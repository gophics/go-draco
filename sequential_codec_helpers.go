package draco

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/gophics/go-draco/internal/bitstream"
	"github.com/gophics/go-draco/internal/core"
	"github.com/gophics/go-draco/internal/entropy"
)

type sequentialFloatQuantizer func(context.Context, *Attribute, int) (*quantizationTransform, *Attribute, error)

type sequentialEncodeScratch struct {
	int32Buffers  [3][]int32
	uint32Buffers [1][]uint32
}

func (s *sequentialEncodeScratch) reset() {
	if s == nil {
		return
	}

	for i := range s.int32Buffers {
		s.int32Buffers[i] = resetScratchSlice(s.int32Buffers[i])
	}

	for i := range s.uint32Buffers {
		s.uint32Buffers[i] = resetScratchSlice(s.uint32Buffers[i])
	}
}

func (s *sequentialEncodeScratch) int32Buffer(slot, size int) []int32 {
	if s == nil || slot < 0 || slot >= len(s.int32Buffers) {
		return make([]int32, size)
	}

	buf := slices.Grow(s.int32Buffers[slot][:0], size)
	buf = buf[:size]
	s.int32Buffers[slot] = buf
	return buf
}

func (s *sequentialEncodeScratch) uint32Buffer(slot, size int) []uint32 {
	if s == nil || slot < 0 || slot >= len(s.uint32Buffers) {
		return make([]uint32, size)
	}

	buf := slices.Grow(s.uint32Buffers[slot][:0], size)
	buf = buf[:size]
	s.uint32Buffers[slot] = buf
	return buf
}

func buildSequentialEncodedAttributeState(ctx context.Context, attr, encoded *Attribute, quantizationBits int, quantizeFloat sequentialFloatQuantizer) (sequentialEncodedAttribute, error) {
	state := sequentialEncodedAttribute{
		attr:        encoded,
		portable:    encoded,
		encoderType: bitstream.SequentialAttributeEncoderGeneric,
	}
	switch {
	case attr.Type == AttributeNormal && quantizationBits > 0:
		if encoded.DataType != DataTypeFloat32 || encoded.NumComponents != 3 {
			return state, fmt.Errorf("%w: normal quantization requires FLOAT32x3 attribute", ErrUnsupportedFeature)
		}

		transform := &octahedronTransform{}
		if err := transform.SetQuantizationBits(quantizationBits); err != nil {
			return state, err
		}

		portable, err := quantizeOctahedronAttribute(ctx, transform, encoded)
		if err != nil {
			return state, err
		}

		state.portable = portable
		state.encoderType = bitstream.SequentialAttributeEncoderNormals
		state.octahedron = transform
	case encoded.DataType == DataTypeFloat32 && quantizationBits > 0:
		if quantizeFloat == nil {
			return state, errors.New("draco: missing sequential float quantizer")
		}

		transform, portable, err := quantizeFloat(ctx, encoded, quantizationBits)
		if err != nil {
			return state, err
		}

		state.portable = portable
		state.encoderType = bitstream.SequentialAttributeEncoderQuantization
		state.quantization = transform
	case isSequentialIntegerDataType(encoded.DataType):
		state.encoderType = bitstream.SequentialAttributeEncoderInteger
	}

	return state, nil
}

func defaultSequentialFloatQuantizer(ctx context.Context, attr *Attribute, quantizationBits int) (*quantizationTransform, *Attribute, error) {
	transform := &quantizationTransform{}
	if err := transform.compute(attr, quantizationBits); err != nil {
		return nil, nil, err
	}

	portable, err := transform.quantizeAttribute(ctx, attr)
	if err != nil {
		return nil, nil, err
	}

	return transform, portable, nil
}

func buildSequentialDecodedAttributeStateWithScratch(attr *Attribute, numValues, pointCount int, decoderType uint8, copyPointMapping bool, portableData []byte, portableMapping []uint32) (*sequentialDecodedAttribute, error) {
	state := &sequentialDecodedAttribute{
		attr:        attr,
		decoderType: decoderType,
	}
	switch decoderType {
	case bitstream.SequentialAttributeEncoderGeneric, bitstream.SequentialAttributeEncoderInteger:
		state.portable = attr
	case bitstream.SequentialAttributeEncoderQuantization:
		if attr.DataType != DataTypeFloat32 {
			return nil, fmt.Errorf("%w: quantized attribute target type %s", ErrUnsupportedFeature, attr.DataType)
		}

		portable, err := newSequentialPortableAttribute(attr, DataTypeInt32, attr.NumComponents, numValues, portableData)
		if err != nil {
			return nil, err
		}

		portable.Normalized = attr.Normalized
		portable.UniqueID = attr.UniqueID
		if copyPointMapping {
			if err := cloneSequentialPortableMappingInto(portable, attr, pointCount, portableMapping); err != nil {
				return nil, err
			}
		}

		state.portable = portable
		state.quantization = &quantizationTransform{}
	case bitstream.SequentialAttributeEncoderNormals:
		if attr.Type != AttributeNormal || attr.DataType != DataTypeFloat32 || attr.NumComponents != 3 {
			return nil, fmt.Errorf("%w: octahedron normals require FLOAT32x3 normal attribute", ErrUnsupportedFeature)
		}

		portable, err := newSequentialPortableAttribute(attr, DataTypeInt32, 2, numValues, portableData)
		if err != nil {
			return nil, err
		}

		portable.UniqueID = attr.UniqueID
		if copyPointMapping {
			if err := cloneSequentialPortableMappingInto(portable, attr, pointCount, portableMapping); err != nil {
				return nil, err
			}
		}

		state.portable = portable
		state.octahedron = &octahedronTransform{}
	default:
		return nil, fmt.Errorf("%w: sequential decoder type %d", ErrUnsupportedFeature, decoderType)
	}

	return state, nil
}

func newSequentialPortableAttribute(source *Attribute, dataType DataType, numComponents, numValues int, data []byte) (*Attribute, error) {
	if data == nil {
		return NewAttribute(source.Type, dataType, numComponents, numValues)
	}

	if numComponents <= 0 {
		return nil, fmt.Errorf("%w: invalid component count %d", ErrInvalidGeometry, numComponents)
	}

	stride := DataTypeLength(dataType) * numComponents
	if stride == 0 {
		return nil, fmt.Errorf("%w: invalid data type %d", ErrInvalidGeometry, dataType)
	}

	size, err := guardIntProductAllocation(numValues, stride, 1, "sequential portable attribute data")
	if err != nil {
		return nil, err
	}

	if len(data) != size {
		return nil, fmt.Errorf("draco: sequential portable scratch size %d does not match %d", len(data), size)
	}

	return &Attribute{
		Type:          source.Type,
		DataType:      dataType,
		NumComponents: numComponents,
		data:          data,
	}, nil
}

func sequentialPortableAttributeScratchSchema(decoderType uint8, numComponents int) (DataType, int, bool) {
	switch decoderType {
	case bitstream.SequentialAttributeEncoderQuantization:
		return DataTypeInt32, numComponents, true
	case bitstream.SequentialAttributeEncoderNormals:
		return DataTypeInt32, 2, true
	default:
		return DataTypeInvalid, 0, false
	}
}

func validateSequentialEncodingSelection(attID int, attr *Attribute, state sequentialEncodedAttribute, quantizationBits int, options encodeConfig) error {
	if quantizationBits > 0 && state.encoderType == bitstream.SequentialAttributeEncoderGeneric {
		return fmt.Errorf("%w: quantization for %s requires FLOAT32 attribute, got %s", ErrUnsupportedFeature, attr.Type, attr.DataType)
	}

	selectedMethod := options.predictionMethodForAttribute(attID, attr.Type)
	switch selectedMethod {
	case PredictionMethodUndefined, PredictionMethodNone:
		return nil
	case PredictionMethodDifference:
		if state.encoderType == bitstream.SequentialAttributeEncoderGeneric {
			return fmt.Errorf("%w: prediction method %d requires integer, quantized, or quantized normal attribute", ErrUnsupportedFeature, selectedMethod)
		}

		return nil
	case PredictionMethodParallelogram, PredictionMethodMultiParallelogram, PredictionMethodConstrainedMultiParallelogram, PredictionMethodTexCoordsPortable:
		if state.encoderType != bitstream.SequentialAttributeEncoderInteger && state.encoderType != bitstream.SequentialAttributeEncoderQuantization {
			return fmt.Errorf("%w: prediction method %d requires integer or quantized attribute", ErrUnsupportedFeature, selectedMethod)
		}

		return nil
	case PredictionMethodGeometricNormal:
		if state.encoderType != bitstream.SequentialAttributeEncoderNormals {
			return fmt.Errorf("%w: prediction method %d requires quantized normal attribute", ErrUnsupportedFeature, selectedMethod)
		}

		return nil
	default:
		return fmt.Errorf("%w: integer prediction method %d", ErrUnsupportedFeature, selectedMethod)
	}
}

func cloneSequentialPortableMappingInto(portable, source *Attribute, pointCount int, mapping []uint32) error {
	if mapping != nil {
		if len(mapping) != pointCount {
			return fmt.Errorf("draco: sequential portable mapping scratch size %d does not match %d", len(mapping), pointCount)
		}

		portable.mapping = mapping
	} else {
		if err := portable.SetExplicitMapping(pointCount); err != nil {
			return err
		}
	}

	out := portable.mapping
	for pointID := 0; pointID < pointCount; pointID++ {
		out[pointID] = source.mappedIndex(pointID)
	}

	return nil
}

func symbolEncodingOptions(options encodeConfig) *entropy.EncodeOptions {
	return &entropy.EncodeOptions{CompressionLevel: options.normalizedSymbolCompressionLevel()}
}

func legacySequentialTransformDecoder(reader *core.Reader, state *sequentialDecodedAttribute, legacyTransformDataInline bool) func() error {
	if !legacyTransformDataInline {
		return nil
	}

	switch state.decoderType {
	case bitstream.SequentialAttributeEncoderQuantization:
		return func() error {
			return state.quantization.decode(reader, state.attr.NumComponents)
		}
	case bitstream.SequentialAttributeEncoderNormals:
		return func() error {
			return state.octahedron.Decode(reader)
		}
	default:
		return nil
	}
}

func decodeSequentialTransformMetadata(reader *core.Reader, state *sequentialDecodedAttribute) error {
	switch state.decoderType {
	case bitstream.SequentialAttributeEncoderQuantization:
		return state.quantization.decode(reader, state.attr.NumComponents)
	case bitstream.SequentialAttributeEncoderNormals:
		return state.octahedron.Decode(reader)
	default:
		return nil
	}
}

func finalizeDecodedSequentialState(ctx context.Context, state *sequentialDecodedAttribute, options decodeConfig) error {
	switch state.decoderType {
	case bitstream.SequentialAttributeEncoderQuantization:
		if options.SkipTransform(state.attr.Type) {
			replaceAttributeWithPortable(state.attr, state.portable)
			return nil
		}

		return state.quantization.dequantizeAttribute(ctx, state.portable, state.attr)
	case bitstream.SequentialAttributeEncoderNormals:
		if options.SkipTransform(state.attr.Type) {
			replaceAttributeWithPortable(state.attr, state.portable)
			return nil
		}

		return dequantizeOctahedronAttribute(ctx, state.octahedron, state.portable, state.attr)
	default:
		return nil
	}
}
