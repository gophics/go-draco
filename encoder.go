package draco

import (
	"context"
	"fmt"
	"sync"

	"github.com/gophics/go-draco/internal/bitstream"
	"github.com/gophics/go-draco/internal/core"
	"github.com/gophics/go-draco/internal/entropy"
	md "github.com/gophics/go-draco/metadata"
)

type sequentialEncodedAttribute struct {
	attr         *Attribute
	portable     *Attribute
	encoderType  uint8
	quantization *quantizationTransform
	octahedron   *octahedronTransform
}

type encoder struct {
	options          encodeConfig
	numEncodedPoints int
	numEncodedFaces  int
	ctx              context.Context
	scratch          *encodeScratch
}

type encodeScratch struct {
	sequential  sequentialEncodeScratch
	kdTree      kdTreeEncodeScratch
	edgebreaker edgebreakerEncodeScratch
}

const maxEncodeWriterCapacityHint = 1 << 20

var encodeScratchPool = sync.Pool{
	New: func() any {
		return &encodeScratch{}
	},
}

func acquireEncodeScratch() *encodeScratch {
	scratch, ok := encodeScratchPool.Get().(*encodeScratch)
	if !ok || scratch == nil {
		return &encodeScratch{}
	}

	return scratch
}

func releaseEncodeScratch(scratch *encodeScratch) {
	if scratch == nil {
		return
	}

	scratch.sequential.reset()
	scratch.kdTree.reset()
	scratch.edgebreaker.reset()
	encodeScratchPool.Put(scratch)
}

func newPointCloudEncodeWriter(pc *PointCloud) *core.Writer {
	return core.NewWriter(encodePointCloudCapacityHint(pc))
}

func newMeshEncodeWriter(mesh *Mesh, options encodeConfig) *core.Writer {
	return core.NewWriter(encodeMeshCapacityHint(mesh, options))
}

func encodePointCloudCapacityHint(pc *PointCloud) int {
	if pc == nil {
		return 0
	}

	hint := addEncodeCapacityHint(16, pc.AttributeCount()*16)
	for _, attr := range pc.attributes {
		if attr == nil {
			continue
		}

		rawBytes := scaledEncodeCapacityHint(attr.EntryCount(), attr.ByteStride(), 2)
		hint = addEncodeCapacityHint(hint, rawBytes)
		hint = addEncodeCapacityHint(hint, attr.EntryCount()/2)
		if !attr.IsIdentityMapping() {
			hint = addEncodeCapacityHint(hint, pc.PointCount()/2)
		}
	}

	return hint
}

func encodeMeshCapacityHint(mesh *Mesh, options encodeConfig) int {
	if mesh == nil {
		return 0
	}

	hint := addEncodeCapacityHint(encodePointCloudCapacityHint(&mesh.PointCloud), 32)
	switch options.normalizedMeshMethod() {
	case MeshSequentialEncoding:
		indexBytes := 4
		switch {
		case mesh.PointCount() < 256:
			indexBytes = 1
		case mesh.PointCount() < 1<<16:
			indexBytes = 2
		}

		hint = addEncodeCapacityHint(hint, mesh.FaceCount()*3*indexBytes)
	case MeshEdgebreakerEncoding:
		hint = addEncodeCapacityHint(hint, mesh.FaceCount()*8)
		hint = addEncodeCapacityHint(hint, mesh.PointCount()*2)
	}

	return hint
}

func scaledEncodeCapacityHint(count, width, divisor int) int {
	if count <= 0 || width <= 0 {
		return 0
	}

	if count > maxEncodeWriterCapacityHint/width {
		return maxEncodeWriterCapacityHint
	}

	value := count * width
	if divisor > 1 {
		value /= divisor
	}

	return value
}

func addEncodeCapacityHint(hint, add int) int {
	if add <= 0 {
		return hint
	}

	if hint >= maxEncodeWriterCapacityHint || add >= maxEncodeWriterCapacityHint-hint {
		return maxEncodeWriterCapacityHint
	}

	return hint + add
}

func (e *encoder) withContext(ctx context.Context) *encoder {
	e.ctx = ctx
	return e
}

func (e *encoder) checkContext() error {
	if e == nil || e.ctx == nil {
		return nil
	}

	return e.ctx.Err()
}

func (e *encoder) sequentialScratch() *sequentialEncodeScratch {
	if e == nil || e.scratch == nil {
		return nil
	}

	return &e.scratch.sequential
}

func (e *encoder) EncodePointCloud(pc *PointCloud) ([]byte, error) {
	e.numEncodedPoints = 0
	e.numEncodedFaces = 0
	if err := e.checkContext(); err != nil {
		return nil, err
	}

	if pc == nil {
		return nil, fmt.Errorf("%w: point cloud is nil", ErrInvalidGeometry)
	}

	if err := pc.Validate(); err != nil {
		return nil, err
	}

	options := mergeEncodeConfig(encodeConfig{}, e.options)
	writer := newPointCloudEncodeWriter(pc)
	switch options.normalizedPointCloudMethod() {
	case PointCloudSequentialEncoding:
		if err := guardEncodeInt32Value(pc.PointCount(), "point cloud point count"); err != nil {
			return nil, err
		}

		header := bitstream.Header{
			VersionMajor:  bitstream.PointCloudVersionMajor,
			VersionMinor:  bitstream.PointCloudVersionMinor,
			EncoderType:   bitstream.GeometryTypePointCloud,
			EncoderMethod: bitstream.PointCloudSequentialEncoding,
		}
		if pc.metadataRef() != nil {
			header.Flags |= bitstream.MetadataFlagMask
		}

		if err := bitstream.EncodeHeader(writer, header); err != nil {
			return nil, err
		}

		if pc.metadataRef() != nil {
			if err := md.EncodeGeometryMetadata(writer, pc.metadataRef()); err != nil {
				return nil, err
			}
		}

		if err := writer.WriteInt32(int32(pc.PointCount())); err != nil {
			return nil, err
		}

		if err := e.encodeSequentialAttributes(writer, pc, nil, options); err != nil {
			return nil, err
		}
	case PointCloudKDTreeEncoding:
		var kdScratch *kdTreeEncodeScratch
		if e.scratch != nil {
			kdScratch = &e.scratch.kdTree
		}

		if err := encodePointCloudKDTree(e.ctx, writer, pc, options, kdScratch); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%w: point cloud method %d", ErrUnsupportedEncoding, options.normalizedPointCloudMethod())
	}

	if options.TrackEncodedProperties() {
		e.numEncodedPoints = pc.PointCount()
	}

	if err := e.checkContext(); err != nil {
		return nil, err
	}

	return writer.BytesView(), nil
}

func (e *encoder) EncodeMesh(mesh *Mesh) ([]byte, error) {
	e.numEncodedPoints = 0
	e.numEncodedFaces = 0
	if err := e.checkContext(); err != nil {
		return nil, err
	}

	if mesh == nil {
		return nil, fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if err := mesh.Validate(); err != nil {
		return nil, err
	}

	options := mergeEncodeConfig(encodeConfig{}, e.options)
	writer := newMeshEncodeWriter(mesh, options)
	switch options.normalizedMeshMethod() {
	case MeshSequentialEncoding:
		if err := guardEncodeUint32Value(mesh.FaceCount(), "mesh face count"); err != nil {
			return nil, err
		}

		if err := guardEncodeUint32Value(mesh.PointCount(), "mesh point count"); err != nil {
			return nil, err
		}

		header := bitstream.Header{
			VersionMajor:  bitstream.MeshVersionMajor,
			VersionMinor:  bitstream.MeshVersionMinor,
			EncoderType:   bitstream.GeometryTypeMesh,
			EncoderMethod: bitstream.MeshSequentialEncoding,
		}
		if mesh.metadataRef() != nil {
			header.Flags |= bitstream.MetadataFlagMask
		}

		if err := bitstream.EncodeHeader(writer, header); err != nil {
			return nil, err
		}

		if mesh.metadataRef() != nil {
			if err := md.EncodeGeometryMetadata(writer, mesh.metadataRef()); err != nil {
				return nil, err
			}
		}

		if err := core.EncodeVarUint32(writer, uint32(mesh.FaceCount())); err != nil {
			return nil, err
		}

		if err := core.EncodeVarUint32(writer, uint32(mesh.PointCount())); err != nil {
			return nil, err
		}

		connectivityMethod := SequentialConnectivityUncompressed
		if options.compressConnectivity() {
			connectivityMethod = SequentialConnectivityCompressed
		}

		if err := writer.WriteUint8(uint8(connectivityMethod)); err != nil {
			return nil, err
		}

		if connectivityMethod == SequentialConnectivityCompressed {
			if err := encodeSequentialCompressedIndices(e.ctx, writer, mesh, symbolEncodingOptions(options)); err != nil {
				return nil, err
			}
		} else {
			for faceID, face := range mesh.faces {
				if err := checkContextEvery(e.ctx, faceID); err != nil {
					return nil, err
				}

				switch {
				case mesh.PointCount() < 256:
					for _, idx := range face {
						if err := writer.WriteUint8(uint8(idx)); err != nil {
							return nil, err
						}
					}
				case mesh.PointCount() < 1<<16:
					for _, idx := range face {
						if err := writer.WriteUint16(uint16(idx)); err != nil {
							return nil, err
						}
					}
				case mesh.PointCount() < 1<<21:
					for _, idx := range face {
						if err := core.EncodeVarUint32(writer, idx); err != nil {
							return nil, err
						}
					}
				default:
					for _, idx := range face {
						if err := writer.WriteUint32(idx); err != nil {
							return nil, err
						}
					}
				}
			}
		}

		if err := e.encodeSequentialAttributes(writer, &mesh.PointCloud, mesh, options); err != nil {
			return nil, err
		}
	case MeshEdgebreakerEncoding:
		var ebScratch *edgebreakerEncodeScratch
		if e.scratch != nil {
			ebScratch = &e.scratch.edgebreaker
		}

		if err := encodeEdgebreakerMesh(e.ctx, writer, mesh, options, ebScratch); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%w: mesh method %d", ErrUnsupportedEncoding, options.normalizedMeshMethod())
	}

	if options.TrackEncodedProperties() {
		e.numEncodedPoints = mesh.PointCount()
		e.numEncodedFaces = mesh.FaceCount()
	}

	if err := e.checkContext(); err != nil {
		return nil, err
	}

	return writer.BytesView(), nil
}

func (e *encoder) encodeSequentialAttributes(writer *core.Writer, pc *PointCloud, mesh *Mesh, options encodeConfig) error {
	if err := e.checkContext(); err != nil {
		return err
	}

	if pc.AttributeCount() == 0 {
		return fmt.Errorf("%w: no attributes", ErrInvalidGeometry)
	}

	if err := guardEncodeUint32Value(pc.AttributeCount(), "attribute count"); err != nil {
		return err
	}

	if err := writer.WriteUint8(1); err != nil {
		return err
	}

	if err := core.EncodeVarUint32(writer, uint32(pc.AttributeCount())); err != nil {
		return err
	}

	for i, attr := range pc.attributes {
		if err := checkContextEvery(e.ctx, i); err != nil {
			return err
		}

		if err := guardEncodeUint8Value(int(attr.Type), "attribute type"); err != nil {
			return err
		}

		if err := guardEncodeUint8Value(int(attr.DataType), "attribute data type"); err != nil {
			return err
		}

		if err := guardEncodeUint8Value(attr.NumComponents, "attribute component count"); err != nil {
			return err
		}

		if err := writer.WriteUint8(uint8(attr.Type)); err != nil {
			return err
		}

		if err := writer.WriteUint8(uint8(attr.DataType)); err != nil {
			return err
		}

		if err := writer.WriteUint8(uint8(attr.NumComponents)); err != nil {
			return err
		}

		normalized := uint8(0)
		if attr.Normalized {
			normalized = 1
		}

		if err := writer.WriteUint8(normalized); err != nil {
			return err
		}

		if err := core.EncodeVarUint32(writer, attr.UniqueID); err != nil {
			return err
		}
	}

	states := make([]sequentialEncodedAttribute, len(pc.attributes))
	for i, attr := range pc.attributes {
		if err := checkContextEvery(e.ctx, i); err != nil {
			return err
		}

		quantizationBits, err := quantizationBitsForAttribute(i, attr, options)
		if err != nil {
			return err
		}

		state, err := buildSequentialEncodedAttributeState(e.ctx, attr, attr, quantizationBits, func(ctx context.Context, source *Attribute, _ int) (*quantizationTransform, *Attribute, error) {
			transform, err := attributeQuantizationTransform(i, source, options)
			if err != nil {
				return nil, nil, err
			}

			portable, err := transform.quantizeAttribute(ctx, source)
			if err != nil {
				return nil, nil, err
			}

			return transform, portable, nil
		})
		if err != nil {
			return err
		}

		if err := validateSequentialEncodingSelection(i, attr, state, quantizationBits, options); err != nil {
			return err
		}

		states[i] = state
		if err := writer.WriteUint8(state.encoderType); err != nil {
			return err
		}
	}

	positionPortable := findEncodedPortableAttribute(states, AttributePosition)
	for i, state := range states {
		if err := checkContextEvery(e.ctx, i); err != nil {
			return err
		}

		switch state.encoderType {
		case bitstream.SequentialAttributeEncoderGeneric:
			if state.portable.IsIdentityMapping() {
				if err := checkContext(e.ctx); err != nil {
					return err
				}

				if err := writer.WriteBytes(state.portable.data); err != nil {
					return err
				}

				continue
			}

			for pointID := 0; pointID < pc.PointCount(); pointID++ {
				if err := checkContextEvery(e.ctx, pointID); err != nil {
					return err
				}

				raw, err := state.portable.rawEntry(int(state.portable.mappedIndex(pointID)))
				if err != nil {
					return err
				}

				if err := writer.WriteBytes(raw); err != nil {
					return err
				}
			}
		case bitstream.SequentialAttributeEncoderInteger, bitstream.SequentialAttributeEncoderQuantization:
			if err := encodeSequentialIntegerAttribute(e.ctx, writer, i, state.attr, state.portable, pc.PointCount(), mesh, positionPortable, options, options.useBuiltInAttributeCompression(), symbolEncodingOptions(options), e.sequentialScratch()); err != nil {
				return err
			}
		case bitstream.SequentialAttributeEncoderNormals:
			if err := encodeSequentialNormalAttribute(e.ctx, writer, i, state.attr, state.portable, state.octahedron, pc.PointCount(), mesh, positionPortable, options, options.useBuiltInAttributeCompression(), symbolEncodingOptions(options), e.sequentialScratch()); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: sequential encoder type %d", ErrUnsupportedFeature, state.encoderType)
		}
	}

	for i, state := range states {
		if err := checkContextEvery(e.ctx, i); err != nil {
			return err
		}

		switch state.encoderType {
		case bitstream.SequentialAttributeEncoderQuantization:
			if err := state.quantization.encode(writer); err != nil {
				return err
			}
		case bitstream.SequentialAttributeEncoderNormals:
			if err := state.octahedron.Encode(writer); err != nil {
				return err
			}
		}
	}

	return nil
}

func encodeSequentialCompressedIndices(ctx context.Context, writer *core.Writer, mesh *Mesh, options *entropy.EncodeOptions) error {
	symbols := make([]uint32, 0, mesh.FaceCount()*3)
	lastIndexValue := int32(0)
	for faceID, face := range mesh.faces {
		if err := checkContextEvery(ctx, faceID); err != nil {
			return err
		}

		for _, idx := range face {
			indexValue := int32(idx)
			indexDiff := indexValue - lastIndexValue
			encodedValue := uint32(absInt32(indexDiff)) << 1
			if indexDiff < 0 {
				encodedValue |= 1
			}

			symbols = append(symbols, encodedValue)
			lastIndexValue = indexValue
		}
	}

	return entropy.EncodeSymbols(writer, symbols, 1, options)
}

func absInt32(v int32) int32 {
	if v < 0 {
		return -v
	}

	return v
}

func findEncodedPortableAttribute(states []sequentialEncodedAttribute, attType AttributeType) *Attribute {
	for _, state := range states {
		if state.attr != nil && state.attr.Type == attType {
			return state.portable
		}
	}

	return nil
}
