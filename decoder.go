package draco

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"unsafe"

	"github.com/gophics/go-draco/internal/bitstream"
	kdtreecodec "github.com/gophics/go-draco/internal/codec/kdtree"
	"github.com/gophics/go-draco/internal/core"
	"github.com/gophics/go-draco/internal/entropy"
	md "github.com/gophics/go-draco/metadata"
)

type sequentialDecodedAttribute struct {
	attr         *Attribute
	portable     *Attribute
	decoderType  uint8
	quantization *quantizationTransform
	octahedron   *octahedronTransform
}

type decodeScratch struct {
	kdTree                kdtreecodec.DecodeScratch
	symbols               entropy.DecodeScratch
	edgebreaker           edgebreakerDecodeScratch
	sequentialDataBuffers [][]byte
	sequentialDataUsed    int
}

type decoder struct {
	options decodeConfig
	ctx     context.Context
	scratch *decodeScratch
}

var decodeScratchPool = sync.Pool{
	New: func() any {
		return &decodeScratch{}
	},
}

func acquireDecodeScratch() *decodeScratch {
	scratch, ok := decodeScratchPool.Get().(*decodeScratch)
	if !ok || scratch == nil {
		return &decodeScratch{}
	}

	return scratch
}

func releaseDecodeScratch(scratch *decodeScratch) {
	if scratch == nil {
		return
	}

	scratch.reset()
	decodeScratchPool.Put(scratch)
}

func (s *decodeScratch) reset() {
	if s == nil {
		return
	}

	s.kdTree.Reset()
	s.symbols.Reset()
	s.edgebreaker.reset()
	for i := range s.sequentialDataBuffers {
		s.sequentialDataBuffers[i] = resetScratchSlice(s.sequentialDataBuffers[i])
	}

	s.sequentialDataBuffers = resetScratchSlice(s.sequentialDataBuffers)
	s.sequentialDataUsed = 0
}

func (s *decodeScratch) sequentialDataBuffer(size int) []byte {
	if s == nil {
		return make([]byte, size)
	}

	index := s.sequentialDataUsed
	s.sequentialDataUsed++
	s.sequentialDataBuffers = slices.Grow(s.sequentialDataBuffers, index+1)
	s.sequentialDataBuffers = s.sequentialDataBuffers[:index+1]
	buffer := slices.Grow(s.sequentialDataBuffers[index][:0], size)
	buffer = buffer[:size]
	clear(buffer)
	s.sequentialDataBuffers[index] = buffer
	return buffer
}

func (d decoder) withContext(ctx context.Context) decoder {
	d.ctx = ctx
	return d
}

func (d *decoder) checkContext() error {
	if d == nil || d.ctx == nil {
		return nil
	}

	return d.ctx.Err()
}

func (d *decoder) symbolScratch() *entropy.DecodeScratch {
	if d == nil || d.scratch == nil {
		return nil
	}

	return &d.scratch.symbols
}

func (d *decoder) edgebreakerScratch() *edgebreakerDecodeScratch {
	if d == nil || d.scratch == nil {
		return nil
	}

	return &d.scratch.edgebreaker
}

func (d *decoder) sequentialPortableData(decoderType uint8, attr *Attribute, numValues int) ([]byte, error) {
	if d == nil || d.scratch == nil || attr == nil || d.options.SkipTransform(attr.Type) {
		return nil, nil
	}

	dataType, numComponents, ok := sequentialPortableAttributeScratchSchema(decoderType, attr.NumComponents)
	if !ok {
		return nil, nil
	}

	stride := DataTypeLength(dataType) * numComponents
	if stride == 0 {
		return nil, fmt.Errorf("%w: invalid sequential portable type %s", ErrInvalidGeometry, dataType)
	}

	size, err := guardIntProductAllocation(numValues, stride, 1, "sequential portable scratch")
	if err != nil {
		return nil, err
	}

	return d.scratch.sequentialDataBuffer(size), nil
}

func detectGeometry(data []byte) (EncodedGeometryType, error) {
	reader := core.NewReader(data)
	header, err := bitstream.DecodeHeader(reader)
	if err != nil {
		return InvalidGeometryType, fmt.Errorf("%w: %w", ErrInvalidHeader, err)
	}

	switch header.EncoderType {
	case bitstream.GeometryTypePointCloud:
		return PointCloudGeometry, nil
	case bitstream.GeometryTypeMesh:
		return MeshGeometry, nil
	default:
		return InvalidGeometryType, fmt.Errorf("%w: geometry type %d", ErrInvalidGeometry, header.EncoderType)
	}
}

func (d *decoder) Decode(data []byte) (Geometry, error) {
	if err := d.checkContext(); err != nil {
		return nil, err
	}

	reader := core.NewReader(data)
	header, err := bitstream.DecodeHeader(reader)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidHeader, err)
	}

	if err := bitstream.ValidateVersion(header); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnsupportedVersion, err)
	}

	geom, err := d.decodeFromHeader(reader, header)
	if err != nil {
		return nil, err
	}

	if err := validateDecodedGeometry(geom); err != nil {
		return nil, err
	}

	return geom, d.checkContext()
}

func readAllContext(ctx context.Context, r io.Reader, maxBytes int64) ([]byte, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: context is nil", ErrInvalidArgument)
	}

	if r == nil {
		return nil, fmt.Errorf("%w: reader is nil", ErrInvalidArgument)
	}

	if maxBytes < 0 {
		return nil, fmt.Errorf("%w: maxBytes must be >= 0", ErrInvalidArgument)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	const chunkSize = 32 << 10
	buf := make([]byte, chunkSize)
	var out []byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		n, err := r.Read(buf)
		if n > 0 {
			if int64(len(out))+int64(n) > maxBytes {
				return nil, fmt.Errorf("%w: input exceeds %d-byte limit", ErrInvalidArgument, maxBytes)
			}

			out = append(out, buf[:n]...)
		}

		switch err {
		case nil:
			if n == 0 {
				return nil, io.ErrNoProgress
			}
		case io.EOF:
			return out, nil
		default:
			return nil, err
		}
	}
}

func (d *decoder) decodeWithHeader(data []byte) (Geometry, bitstream.Header, error) {
	if err := d.checkContext(); err != nil {
		return nil, bitstream.Header{}, err
	}

	reader := core.NewReader(data)
	header, err := bitstream.DecodeHeader(reader)
	if err != nil {
		return nil, bitstream.Header{}, fmt.Errorf("%w: %w", ErrInvalidHeader, err)
	}

	if err := bitstream.ValidateVersion(header); err != nil {
		return nil, bitstream.Header{}, fmt.Errorf("%w: %w", ErrUnsupportedVersion, err)
	}

	geom, err := d.decodeFromHeader(reader, header)
	if err != nil {
		return nil, bitstream.Header{}, err
	}

	if err := validateDecodedGeometry(geom); err != nil {
		return nil, bitstream.Header{}, err
	}

	return geom, header, d.checkContext()
}

func (d *decoder) decodeFromHeader(reader *core.Reader, header bitstream.Header) (Geometry, error) {
	switch header.EncoderType {
	case bitstream.GeometryTypePointCloud:
		return d.decodePointCloud(reader, header)
	case bitstream.GeometryTypeMesh:
		return d.decodeMesh(reader, header)
	default:
		return nil, fmt.Errorf("%w: geometry type %d", ErrInvalidGeometry, header.EncoderType)
	}
}

func (d *decoder) decodePointCloud(reader *core.Reader, header bitstream.Header) (*PointCloud, error) {
	switch header.EncoderMethod {
	case bitstream.PointCloudSequentialEncoding:
		pc := newPointCloud(0)
		var err error
		if header.Flags&bitstream.MetadataFlagMask != 0 {
			metadata, decodeErr := md.DecodeGeometryMetadata(d.ctx, reader)
			if decodeErr != nil {
				return nil, decodeErr
			}

			pc.setMetadata(metadata)
		}

		numPoints, err := reader.ReadInt32()
		if err != nil {
			return nil, err
		}

		if numPoints < 0 {
			return nil, fmt.Errorf("%w: invalid point count %d", ErrInvalidGeometry, numPoints)
		}

		pc.setPointCount(int(numPoints))
		if err := d.decodeSequentialAttributes(reader, pc, header, nil); err != nil {
			return nil, err
		}

		return pc, nil
	case bitstream.PointCloudKDTreeEncoding:
		var kdScratch *kdtreecodec.DecodeScratch
		if d.scratch != nil {
			kdScratch = &d.scratch.kdTree
		}

		return decodePointCloudKDTree(reader, header, d.options, d.ctx, kdScratch)
	default:
		return nil, fmt.Errorf("%w: point cloud encoding method %d", ErrUnsupportedEncoding, header.EncoderMethod)
	}
}

func (d *decoder) decodeMesh(reader *core.Reader, header bitstream.Header) (*Mesh, error) {
	switch header.EncoderMethod {
	case bitstream.MeshSequentialEncoding:
		mesh := newMesh(0)
		var err error
		if header.Flags&bitstream.MetadataFlagMask != 0 {
			metadata, decodeErr := md.DecodeGeometryMetadata(d.ctx, reader)
			if decodeErr != nil {
				return nil, decodeErr
			}

			mesh.setMetadata(metadata)
		}

		var numFaces uint32
		var numPoints uint32
		if header.VersionMajor < 2 || (header.VersionMajor == 2 && header.VersionMinor < 2) {
			numFaces, err = reader.ReadUint32()
			if err != nil {
				return nil, err
			}

			numPoints, err = reader.ReadUint32()
			if err != nil {
				return nil, err
			}
		} else {
			numFaces, err = core.DecodeVarUint32(reader)
			if err != nil {
				return nil, err
			}

			numPoints, err = core.DecodeVarUint32(reader)
			if err != nil {
				return nil, err
			}
		}

		numFacesInt, err := guardUint32SliceAllocation(numFaces, unsafe.Sizeof(Face{}), "sequential mesh faces")
		if err != nil {
			return nil, err
		}

		mesh.setPointCount(int(numPoints))
		connectivityMethod, err := reader.ReadUint8()
		if err != nil {
			return nil, err
		}

		switch SequentialConnectivityMethod(connectivityMethod) {
		case SequentialConnectivityCompressed:
			if err := decodeSequentialCompressedIndices(d.ctx, reader, mesh, numFaces, d.symbolScratch()); err != nil {
				return nil, err
			}
		case SequentialConnectivityUncompressed:
			mesh.faces = make([]Face, numFacesInt)
			for i := uint32(0); i < numFaces; i++ {
				if err := checkContextEvery(d.ctx, int(i)); err != nil {
					return nil, err
				}

				var face Face
				switch {
				case numPoints < 256:
					for j := range face {
						v, err := reader.ReadUint8()
						if err != nil {
							return nil, err
						}

						face[j] = uint32(v)
					}
				case numPoints < 1<<16:
					for j := range face {
						v, err := reader.ReadUint16()
						if err != nil {
							return nil, err
						}

						face[j] = uint32(v)
					}
				case numPoints < 1<<21:
					for j := range face {
						v, err := core.DecodeVarUint32(reader)
						if err != nil {
							return nil, err
						}

						face[j] = v
					}
				default:
					for j := range face {
						v, err := reader.ReadUint32()
						if err != nil {
							return nil, err
						}

						face[j] = v
					}
				}

				for _, idx := range face {
					if idx >= numPoints {
						return nil, fmt.Errorf("%w: face index %d out of range for %d points", ErrInvalidGeometry, idx, numPoints)
					}
				}

				mesh.faces[i] = face
			}
		default:
			return nil, fmt.Errorf("%w: sequential mesh connectivity method %d", ErrUnsupportedFeature, connectivityMethod)
		}

		if err := d.decodeSequentialAttributes(reader, &mesh.PointCloud, header, mesh); err != nil {
			return nil, err
		}

		return mesh, nil
	case bitstream.MeshEdgebreakerEncoding:
		return decodeEdgebreakerMesh(reader, header, d.options, d.ctx, d.symbolScratch(), d.edgebreakerScratch())
	default:
		return nil, fmt.Errorf("%w: mesh encoding method %d", ErrUnsupportedEncoding, header.EncoderMethod)
	}
}

func (d *decoder) decodeSequentialAttributes(reader *core.Reader, pc *PointCloud, header bitstream.Header, mesh *Mesh) error {
	legacyTransformDataInline := header.VersionMajor < 2
	legacyAttributeCount := header.VersionMajor < 2
	legacyAttributeUniqueID := header.VersionMajor < 1 || (header.VersionMajor == 1 && header.VersionMinor < 3)
	numEncoders, err := reader.ReadUint8()
	if err != nil {
		return err
	}

	if numEncoders == 0 {
		return fmt.Errorf("%w: attribute encoder count is zero", ErrInvalidGeometry)
	}

	if err := guardSliceAllocation(int(numEncoders), 8, "sequential attribute groups"); err != nil {
		return err
	}

	groups := make([][]*sequentialDecodedAttribute, numEncoders)
	for i := 0; i < int(numEncoders); i++ {
		if err := checkContextEvery(d.ctx, i); err != nil {
			return err
		}

		var numAttrs uint32
		if legacyAttributeCount {
			numAttrs, err = reader.ReadUint32()
			if err != nil {
				return err
			}
		} else {
			numAttrs, err = core.DecodeVarUint32(reader)
			if err != nil {
				return err
			}
		}

		if numAttrs == 0 {
			return fmt.Errorf("%w: attribute group %d has no attributes", ErrInvalidGeometry, i)
		}

		if _, err := guardUint32SliceAllocation(numAttrs, 8, "sequential attributes"); err != nil {
			return err
		}

		group := make([]*Attribute, 0, numAttrs)
		for j := uint32(0); j < numAttrs; j++ {
			if err := checkContextEvery(d.ctx, int(j)); err != nil {
				return err
			}

			attType, err := reader.ReadUint8()
			if err != nil {
				return err
			}

			dataType, err := reader.ReadUint8()
			if err != nil {
				return err
			}

			numComponents, err := reader.ReadUint8()
			if err != nil {
				return err
			}

			normalized, err := reader.ReadUint8()
			if err != nil {
				return err
			}

			var uniqueID uint32
			if legacyAttributeUniqueID {
				legacyID, err := reader.ReadUint16()
				if err != nil {
					return err
				}

				uniqueID = uint32(legacyID)
			} else {
				uniqueID, err = core.DecodeVarUint32(reader)
				if err != nil {
					return err
				}
			}

			attr, err := NewAttribute(AttributeType(attType), DataType(dataType), int(numComponents), pc.PointCount())
			if err != nil {
				return err
			}

			attr.Normalized = normalized > 0
			attr.UniqueID = uniqueID
			group = append(group, attr)
		}

		states := make([]*sequentialDecodedAttribute, len(group))
		for idx, attr := range group {
			if err := checkContextEvery(d.ctx, idx); err != nil {
				return err
			}

			decoderType, err := reader.ReadUint8()
			if err != nil {
				return err
			}

			id, err := pc.addAttributeOwned(attr)
			if err != nil {
				return err
			}

			attr = pc.attribute(id)
			portableData, err := d.sequentialPortableData(decoderType, attr, pc.PointCount())
			if err != nil {
				return err
			}

			state, err := buildSequentialDecodedAttributeStateWithScratch(attr, pc.PointCount(), 0, decoderType, false, portableData, nil)
			if err != nil {
				return err
			}

			states[idx] = state
		}

		groups[i] = states
	}

	var predictionTables *meshPredictionTableCache
	if mesh != nil {
		predictionTables = &meshPredictionTableCache{mesh: mesh}
	}

	for _, group := range groups {
		var decodedPortable [256]*Attribute
		for stateIndex, state := range group {
			if err := checkContextEvery(d.ctx, stateIndex); err != nil {
				return err
			}

			switch state.decoderType {
			case bitstream.SequentialAttributeEncoderGeneric:
				if err := decodeSequentialGenericAttribute(d.ctx, reader, state.portable, pc.PointCount()); err != nil {
					return err
				}
			case bitstream.SequentialAttributeEncoderInteger, bitstream.SequentialAttributeEncoderQuantization, bitstream.SequentialAttributeEncoderNormals:
				decodeLegacyTransformData := legacySequentialTransformDecoder(reader, state, legacyTransformDataInline)
				if err := decodeSequentialIntegerAttribute(d.ctx, reader, state.attr, state.portable, pc.PointCount(), mesh, decodedPortable[AttributePosition], legacyTransformDataInline, decodeLegacyTransformData, predictionTables, d.symbolScratch()); err != nil {
					return fmt.Errorf("draco: sequential attribute decode type=%s unique=%d encoder=%d: %w", state.attr.Type, state.attr.UniqueID, state.decoderType, err)
				}
			}

			decodedPortable[state.attr.Type] = state.portable
			if legacyTransformDataInline {
				if err := finalizeDecodedSequentialState(d.ctx, state, d.options); err != nil {
					return err
				}
			}
		}

		if legacyTransformDataInline {
			continue
		}

		for _, state := range group {
			if err := d.checkContext(); err != nil {
				return err
			}

			if err := decodeSequentialTransformMetadata(reader, state); err != nil {
				return err
			}
		}

		for _, state := range group {
			if err := d.checkContext(); err != nil {
				return err
			}

			if err := finalizeDecodedSequentialState(d.ctx, state, d.options); err != nil {
				return err
			}
		}
	}

	return nil
}

func decodeSequentialGenericAttribute(ctx context.Context, reader *core.Reader, attr *Attribute, pointCount int) error {
	if err := checkContext(ctx); err != nil {
		return err
	}

	stride := attr.ByteStride()
	if attr.IsIdentityMapping() {
		size, err := guardIntProductAllocation(pointCount, stride, 1, "sequential generic attribute")
		if err != nil {
			return err
		}

		raw, err := reader.ReadBytesView(size)
		if err != nil {
			return err
		}

		copy(attr.data[:size], raw)
		return nil
	}

	for pointID := 0; pointID < pointCount; pointID++ {
		if err := checkContextEvery(ctx, pointID); err != nil {
			return err
		}

		raw, err := reader.ReadBytesView(stride)
		if err != nil {
			return err
		}

		entry := int(attr.mappedIndex(pointID))
		offset := entry * stride
		copy(attr.data[offset:offset+stride], raw)
	}

	return nil
}

func validateDecodedGeometry(geom Geometry) error {
	switch value := geom.(type) {
	case *PointCloud:
		return value.Validate()
	case *Mesh:
		return value.Validate()
	case nil:
		return fmt.Errorf("%w: geometry is nil", ErrInvalidGeometry)
	default:
		return fmt.Errorf("%w: geometry is %T", ErrInvalidGeometry, geom)
	}
}

func replaceAttributeWithPortable(dst, src *Attribute) {
	clone := src.Clone()
	*dst = *clone
}

func decodeSequentialCompressedIndices(ctx context.Context, reader *core.Reader, mesh *Mesh, numFaces uint32, scratch *entropy.DecodeScratch) error {
	numSymbols, err := guardIntProductAllocation(int(numFaces), 3, 4, "sequential mesh indices")
	if err != nil {
		return err
	}

	if uint64(numSymbols) > uint64(^uint32(0)) {
		return fmt.Errorf("%w: sequential mesh index count %d exceeds uint32 range", ErrInvalidGeometry, numSymbols)
	}

	symbols, err := entropy.DecodeSymbolsVersionedTransientWithScratch(reader, uint32(numSymbols), 1, false, scratch)
	if err != nil {
		return err
	}

	mesh.faces = make([]Face, int(numFaces))
	pointCount := uint32(mesh.PointCount())
	lastIndexValue := int32(0)
	vertexIndex := 0
	for i := uint32(0); i < numFaces; i++ {
		if err := checkContextEvery(ctx, int(i)); err != nil {
			return err
		}

		var face Face
		for j := range face {
			encodedValue := symbols[vertexIndex]
			vertexIndex++
			indexDiff := int32(encodedValue >> 1)
			if encodedValue&1 != 0 {
				if indexDiff > lastIndexValue {
					return errors.New("draco: invalid compressed sequential index diff")
				}

				indexDiff = -indexDiff
			}

			indexValue := lastIndexValue + indexDiff
			if indexValue < 0 {
				return errors.New("draco: invalid negative mesh index")
			}

			index := uint32(indexValue)
			if index >= pointCount {
				return fmt.Errorf("%w: face index %d out of range for %d points", ErrInvalidGeometry, index, pointCount)
			}

			face[j] = index
			lastIndexValue = indexValue
		}

		mesh.faces[i] = face
	}

	return nil
}
