package draco

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"slices"

	"github.com/gophics/go-draco/internal/bitstream"
	kdtreecodec "github.com/gophics/go-draco/internal/codec/kdtree"
	"github.com/gophics/go-draco/internal/core"
	md "github.com/gophics/go-draco/metadata"
)

type kdTreeAttributeState struct {
	attr         *Attribute
	portable     *Attribute
	quantization *quantizationTransform
	signedMin    []int32
}

type kdTreeAttributeWriter struct {
	attr       *Attribute
	data       []byte
	stride     int
	components int
	dataType   DataType
}

type kdTreeEncodeScratch struct {
	points        [][]uint32
	pointBacking  []uint32
	int32Values   []int32
	uint32Values  []uint32
	float32Values []float32
	codec         kdtreecodec.EncodeScratch
}

func (s *kdTreeEncodeScratch) reset() {
	if s == nil {
		return
	}

	s.points = resetScratchSlice(s.points)
	s.pointBacking = resetScratchSlice(s.pointBacking)
	s.int32Values = resetScratchSlice(s.int32Values)
	s.uint32Values = resetScratchSlice(s.uint32Values)
	s.float32Values = resetScratchSlice(s.float32Values)
	s.codec.Reset()
}

func (s *kdTreeEncodeScratch) pointRows(numPoints, totalComponents int) ([][]uint32, error) {
	totalValues, err := guardIntProductAllocation(numPoints, totalComponents, 4, "kd-tree encoded point values")
	if err != nil {
		return nil, err
	}

	if s == nil {
		points := make([][]uint32, numPoints)
		backing := make([]uint32, totalValues)
		for pointID := 0; pointID < numPoints; pointID++ {
			start := pointID * totalComponents
			points[pointID] = backing[start : start+totalComponents]
		}

		return points, nil
	}

	s.points = slices.Grow(s.points[:0], numPoints)
	s.points = s.points[:numPoints]
	s.pointBacking = slices.Grow(s.pointBacking[:0], totalValues)
	s.pointBacking = s.pointBacking[:totalValues]
	for pointID := 0; pointID < numPoints; pointID++ {
		start := pointID * totalComponents
		s.points[pointID] = s.pointBacking[start : start+totalComponents]
	}

	return s.points, nil
}

func (s *kdTreeEncodeScratch) int32Buffer(size int) []int32 {
	if s == nil {
		return make([]int32, size)
	}

	s.int32Values = slices.Grow(s.int32Values[:0], size)
	s.int32Values = s.int32Values[:size]
	return s.int32Values
}

func (s *kdTreeEncodeScratch) uint32Buffer(size int) []uint32 {
	if s == nil {
		return make([]uint32, size)
	}

	s.uint32Values = slices.Grow(s.uint32Values[:0], size)
	s.uint32Values = s.uint32Values[:size]
	return s.uint32Values
}

func (s *kdTreeEncodeScratch) float32Buffer(size int) []float32 {
	if s == nil {
		return make([]float32, size)
	}

	s.float32Values = slices.Grow(s.float32Values[:0], size)
	s.float32Values = s.float32Values[:size]
	return s.float32Values
}

func encodePointCloudKDTree(ctx context.Context, writer *core.Writer, pc *PointCloud, options encodeConfig, scratch *kdTreeEncodeScratch) error {
	if err := guardEncodeInt32Value(pc.PointCount(), "kd-tree point count"); err != nil {
		return err
	}

	header := bitstream.Header{
		VersionMajor:  bitstream.PointCloudVersionMajor,
		VersionMinor:  bitstream.PointCloudVersionMinor,
		EncoderType:   bitstream.GeometryTypePointCloud,
		EncoderMethod: bitstream.PointCloudKDTreeEncoding,
	}
	if pc.metadataRef() != nil {
		header.Flags |= bitstream.MetadataFlagMask
	}

	if err := bitstream.EncodeHeader(writer, header); err != nil {
		return err
	}

	if pc.metadataRef() != nil {
		if err := md.EncodeGeometryMetadata(writer, pc.metadataRef()); err != nil {
			return err
		}
	}

	if err := writer.WriteInt32(int32(pc.PointCount())); err != nil {
		return err
	}

	return encodeKDTreeAttributes(ctx, writer, pc, options, scratch)
}

func decodePointCloudKDTree(reader *core.Reader, header bitstream.Header, options decodeConfig, ctx context.Context, scratch *kdtreecodec.DecodeScratch) (*PointCloud, error) {
	pc := newPointCloud(0)
	var err error
	if header.Flags&bitstream.MetadataFlagMask != 0 {
		metadata, decodeErr := md.DecodeGeometryMetadata(ctx, reader)
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
	if err := decodeKDTreeAttributes(reader, pc, options, ctx, scratch); err != nil {
		return nil, err
	}

	return pc, nil
}

func encodeKDTreeAttributes(ctx context.Context, writer *core.Writer, pc *PointCloud, options encodeConfig, scratch *kdTreeEncodeScratch) error {
	if pc.AttributeCount() == 0 {
		return fmt.Errorf("%w: no attributes", ErrInvalidGeometry)
	}

	if err := guardEncodeUint32Value(pc.AttributeCount(), "kd-tree attribute count"); err != nil {
		return err
	}

	for i, attr := range pc.attributes {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		if attr.IsIdentityMapping() && attr.EntryCount() != pc.PointCount() {
			return fmt.Errorf("%w: kd-tree attribute %s has %d entries for %d points", ErrInvalidGeometry, attr.Type, attr.EntryCount(), pc.PointCount())
		}
	}

	if err := writer.WriteUint8(1); err != nil {
		return err
	}

	if err := core.EncodeVarUint32(writer, uint32(pc.AttributeCount())); err != nil {
		return err
	}

	states := make([]kdTreeAttributeState, len(pc.attributes))
	totalComponents := 0
	for i, attr := range pc.attributes {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		if err := guardEncodeUint8Value(int(attr.Type), "kd-tree attribute type"); err != nil {
			return err
		}

		if err := guardEncodeUint8Value(int(attr.DataType), "kd-tree attribute data type"); err != nil {
			return err
		}

		if err := guardEncodeUint8Value(attr.NumComponents, "kd-tree attribute component count"); err != nil {
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

		state, err := prepareKDTreeAttributeForEncoding(ctx, i, attr, options, scratch)
		if err != nil {
			return err
		}

		states[i] = state
		totalComponents += state.portable.NumComponents
	}

	compressionLevel := options.normalizedKDTreeCompressionLevel(totalComponents)
	if err := guardEncodeUint8Value(compressionLevel, "kd-tree compression level"); err != nil {
		return err
	}

	if err := writer.WriteUint8(uint8(compressionLevel)); err != nil {
		return err
	}

	points, bitLength, err := buildKDTreePointVector(ctx, states, pc.PointCount(), scratch)
	if err != nil {
		return err
	}

	var codecScratch *kdtreecodec.EncodeScratch
	if scratch != nil {
		codecScratch = &scratch.codec
	}

	if err := kdtreecodec.EncodePointsContext(ctx, writer, points, totalComponents, bitLength, compressionLevel, codecScratch); err != nil {
		return err
	}

	for i, state := range states {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		if state.quantization != nil {
			if err := state.quantization.encode(writer); err != nil {
				return err
			}
		}
	}

	for i, state := range states {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		for j, value := range state.signedMin {
			if err := checkContextEvery(ctx, j); err != nil {
				return err
			}

			if err := core.EncodeVarInt32(writer, value); err != nil {
				return err
			}
		}
	}

	return nil
}

func decodeKDTreeAttributes(reader *core.Reader, pc *PointCloud, options decodeConfig, ctx context.Context, scratch *kdtreecodec.DecodeScratch) error {
	numDecoders, err := reader.ReadUint8()
	if err != nil {
		return err
	}

	if numDecoders != 1 {
		return fmt.Errorf("%w: kd-tree expects exactly one attribute decoder, got %d", ErrUnsupportedFeature, numDecoders)
	}

	numAttrs, err := core.DecodeVarUint32(reader)
	if err != nil {
		return err
	}

	if numAttrs == 0 {
		return fmt.Errorf("%w: kd-tree attribute count is zero", ErrInvalidGeometry)
	}

	if _, err := guardUint32SliceAllocation(numAttrs, 16, "kd-tree attributes"); err != nil {
		return err
	}

	states := make([]kdTreeAttributeState, numAttrs)
	totalComponents := 0
	for i := uint32(0); i < numAttrs; i++ {
		if err := checkContextEvery(ctx, int(i)); err != nil {
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

		uniqueID, err := core.DecodeVarUint32(reader)
		if err != nil {
			return err
		}

		attr, err := NewAttribute(AttributeType(attType), DataType(dataType), int(numComponents), pc.PointCount())
		if err != nil {
			return err
		}

		attr.Normalized = normalized > 0
		attr.UniqueID = uniqueID
		id, err := pc.addAttributeOwned(attr)
		if err != nil {
			return err
		}

		actualAttr := pc.attribute(id)
		state, err := prepareKDTreeAttributeForDecoding(actualAttr, pc.PointCount())
		if err != nil {
			return err
		}

		states[i] = state
		totalComponents += state.portable.NumComponents
	}

	if err := guardSliceAllocation(totalComponents, 4, "kd-tree point components"); err != nil {
		return err
	}

	if _, err := guardIntProductAllocation(pc.PointCount(), totalComponents, 4, "kd-tree decoded point values"); err != nil {
		return err
	}

	compressionLevel, err := reader.ReadUint8()
	if err != nil {
		return err
	}

	writers := make([]kdTreeAttributeWriter, len(states))
	for i := range states {
		writers[i] = newKDTreeAttributeWriter(states[i].portable)
	}

	pointID, err := decodeKDTreeRowsToAttributes(ctx, reader, totalComponents, int(compressionLevel), pc.PointCount(), writers, scratch)
	if err != nil {
		return err
	}

	if pointID != pc.PointCount() {
		return fmt.Errorf("draco: kd-tree decoded %d points, want %d", pointID, pc.PointCount())
	}

	for i := range states {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		if states[i].quantization != nil {
			if err := states[i].quantization.decode(reader, states[i].attr.NumComponents); err != nil {
				return err
			}
		}
	}

	for i := range states {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		for c := range states[i].signedMin {
			value, err := core.DecodeVarInt32(reader)
			if err != nil {
				return err
			}

			states[i].signedMin[c] = value
		}
	}

	for _, state := range states {
		if err := checkContext(ctx); err != nil {
			return err
		}

		if len(state.signedMin) > 0 {
			if err := restoreSignedKDTreeAttribute(ctx, state.attr, state.signedMin); err != nil {
				return err
			}
		}

		if state.quantization != nil {
			if options.SkipTransform(state.attr.Type) {
				replaceAttributeWithPortable(state.attr, state.portable)
				continue
			}

			if err := dequantizeUint32Attribute(ctx, state.quantization, state.portable, state.attr); err != nil {
				return err
			}
		}
	}

	return nil
}

func decodeKDTreeRowsToAttributes(ctx context.Context, reader *core.Reader, totalComponents, compressionLevel, pointCount int, writers []kdTreeAttributeWriter, scratch *kdtreecodec.DecodeScratch) (int, error) {
	if len(writers) == 2 &&
		writers[0].dataType == DataTypeUint32 &&
		writers[1].dataType == DataTypeUint8 {
		return decodeKDTreeRowsUint32Uint8(ctx, reader, totalComponents, compressionLevel, pointCount, writers[0], writers[1], scratch)
	}

	pointID := 0
	err := kdtreecodec.DecodePointsToRowsContext(ctx, reader, totalComponents, compressionLevel, scratch, func(row []uint32) error {
		if pointID >= pointCount {
			return fmt.Errorf("draco: kd-tree decoded more than %d points", pointCount)
		}

		if err := checkContextEvery(ctx, pointID); err != nil {
			return err
		}

		offset := 0
		for i := range writers {
			values := row[offset : offset+writers[i].components]
			if err := writers[i].writeUnchecked(pointID, values); err != nil {
				return err
			}

			offset += writers[i].components
		}

		pointID++
		return nil
	})
	return pointID, err
}

func decodeKDTreeRowsUint32Uint8(ctx context.Context, reader *core.Reader, totalComponents, compressionLevel, pointCount int, first, second kdTreeAttributeWriter, scratch *kdtreecodec.DecodeScratch) (int, error) {
	pointID := 0
	err := kdtreecodec.DecodePointsToRowsContext(ctx, reader, totalComponents, compressionLevel, scratch, func(row []uint32) error {
		if pointID >= pointCount {
			return fmt.Errorf("draco: kd-tree decoded more than %d points", pointCount)
		}

		if err := checkContextEvery(ctx, pointID); err != nil {
			return err
		}

		firstOffset := pointID * first.stride
		firstData := first.data[firstOffset : firstOffset+first.stride]
		for component := 0; component < first.components; component++ {
			binary.LittleEndian.PutUint32(firstData[component*4:], row[component])
		}

		secondOffset := pointID * second.stride
		secondData := second.data[secondOffset : secondOffset+second.stride]
		rowOffset := first.components
		for component := 0; component < second.components; component++ {
			secondData[component] = byte(row[rowOffset+component])
		}

		pointID++
		return nil
	})
	return pointID, err
}

func prepareKDTreeAttributeForEncoding(ctx context.Context, attID int, attr *Attribute, options encodeConfig, scratch *kdTreeEncodeScratch) (kdTreeAttributeState, error) {
	state := kdTreeAttributeState{attr: attr, portable: attr}
	switch attr.DataType {
	case DataTypeUint32, DataTypeUint16, DataTypeUint8:
		return state, nil
	case DataTypeInt32, DataTypeInt16, DataTypeInt8:
		minValues, err := attributeMinInt32(ctx, attr, scratch)
		if err != nil {
			return kdTreeAttributeState{}, err
		}

		portable, err := shiftSignedAttributeToUint32(ctx, attr, minValues, scratch)
		if err != nil {
			return kdTreeAttributeState{}, err
		}

		state.portable = portable
		state.signedMin = minValues
		return state, nil
	case DataTypeFloat32:
		quantizationBits, err := quantizationBitsForAttribute(attID, attr, options)
		if err != nil {
			return kdTreeAttributeState{}, err
		}

		if quantizationBits < 1 {
			return kdTreeAttributeState{}, fmt.Errorf("%w: kd-tree float32 attribute %s requires quantization bits", ErrUnsupportedFeature, attr.Type)
		}

		transform, err := attributeQuantizationTransform(attID, attr, options)
		if err != nil {
			return kdTreeAttributeState{}, err
		}

		portable, err := quantizeAttributeToUint32(ctx, transform, attr, scratch)
		if err != nil {
			return kdTreeAttributeState{}, err
		}

		state.portable = portable
		state.quantization = transform
		return state, nil
	default:
		return kdTreeAttributeState{}, fmt.Errorf("%w: kd-tree does not support %s attributes", ErrUnsupportedFeature, attr.DataType)
	}
}

func prepareKDTreeAttributeForDecoding(attr *Attribute, numPoints int) (kdTreeAttributeState, error) {
	state := kdTreeAttributeState{attr: attr, portable: attr}
	switch attr.DataType {
	case DataTypeUint32, DataTypeUint16, DataTypeUint8:
		return state, nil
	case DataTypeInt32, DataTypeInt16, DataTypeInt8:
		state.signedMin = make([]int32, attr.NumComponents)
		return state, nil
	case DataTypeFloat32:
		portable := aliasKDTreeFloatPortable(attr, numPoints)
		state.portable = portable
		state.quantization = &quantizationTransform{}
		return state, nil
	default:
		return kdTreeAttributeState{}, fmt.Errorf("%w: kd-tree does not support %s attributes", ErrUnsupportedFeature, attr.DataType)
	}
}

func aliasKDTreeFloatPortable(attr *Attribute, numPoints int) *Attribute {
	portable := *attr
	portable.DataType = DataTypeUint32
	portable.mapping = nil
	portable.data = attr.data[:numPoints*attr.NumComponents*DataTypeLength(DataTypeUint32)]
	return &portable
}

func buildKDTreePointVector(ctx context.Context, states []kdTreeAttributeState, numPoints int, scratch *kdTreeEncodeScratch) ([][]uint32, uint32, error) {
	totalComponents := 0
	for _, state := range states {
		totalComponents += state.portable.NumComponents
	}

	points, err := scratch.pointRows(numPoints, totalComponents)
	if err != nil {
		return nil, 0, err
	}

	if bitLength, ok, err := buildKDTreePointVectorUint32Uint8(ctx, points, states, numPoints); err != nil {
		return nil, 0, err
	} else if ok {
		return points, bitLength, nil
	}

	var bitLength uint32
	for pointID := 0; pointID < numPoints; pointID++ {
		if err := checkContextEvery(ctx, pointID); err != nil {
			return nil, 0, err
		}

		values := points[pointID]
		offset := 0
		for _, state := range states {
			attr := state.portable
			components := values[offset : offset+attr.NumComponents]
			entryID := pointID
			if attr.mapping != nil {
				entryID = int(attr.mapping[pointID])
			}

			if err := writeAttributeValueAsUint32(components, attr, entryID); err != nil {
				return nil, 0, err
			}

			for _, component := range components {
				if component == 0 {
					continue
				}

				msb := uint32(bits.Len32(component))
				if msb > bitLength {
					bitLength = msb
				}
			}

			offset += len(components)
		}
	}

	return points, bitLength, nil
}

func buildKDTreePointVectorUint32Uint8(ctx context.Context, points [][]uint32, states []kdTreeAttributeState, numPoints int) (uint32, bool, error) {
	if len(states) != 2 {
		return 0, false, nil
	}

	first := states[0].portable
	second := states[1].portable
	if first == nil || second == nil ||
		first.mapping != nil || second.mapping != nil ||
		first.DataType != DataTypeUint32 || second.DataType != DataTypeUint8 {
		return 0, false, nil
	}

	firstComponents := first.NumComponents
	secondComponents := second.NumComponents
	if firstComponents <= 0 || secondComponents <= 0 {
		return 0, false, nil
	}

	firstStride := firstComponents * 4
	secondStride := secondComponents
	if numPoints > len(first.data)/firstStride || numPoints > len(second.data)/secondStride {
		return 0, false, nil
	}

	var bitLength uint32
	for pointID := 0; pointID < numPoints; pointID++ {
		if err := checkContextEvery(ctx, pointID); err != nil {
			return 0, true, err
		}

		values := points[pointID]
		firstRaw := first.data[pointID*firstStride:]
		for component := 0; component < firstComponents; component++ {
			value := binary.LittleEndian.Uint32(firstRaw[component*4:])
			values[component] = value
			if value == 0 {
				continue
			}

			if msb := uint32(bits.Len32(value)); msb > bitLength {
				bitLength = msb
			}
		}

		secondRaw := second.data[pointID*secondStride:]
		offset := firstComponents
		for component := 0; component < secondComponents; component++ {
			value := uint32(secondRaw[component])
			values[offset+component] = value
			if value == 0 {
				continue
			}

			if msb := uint32(bits.Len32(value)); msb > bitLength {
				bitLength = msb
			}
		}
	}

	return bitLength, true, nil
}

func writeAttributeValueAsUint32(dst []uint32, attr *Attribute, entry int) error {
	if attr == nil {
		return fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	if len(dst) != attr.NumComponents {
		return fmt.Errorf("draco: expected %d kd-tree components, got %d", attr.NumComponents, len(dst))
	}

	raw, err := attr.rawEntry(entry)
	if err != nil {
		return err
	}

	switch attr.DataType {
	case DataTypeUint8:
		for i := range dst {
			dst[i] = uint32(raw[i])
		}
	case DataTypeUint16:
		for i := range dst {
			dst[i] = uint32(binary.LittleEndian.Uint16(raw[i*2:]))
		}
	case DataTypeUint32:
		for i := range dst {
			dst[i] = binary.LittleEndian.Uint32(raw[i*4:])
		}
	default:
		return fmt.Errorf("draco: cannot read %s attribute as uint32", attr.DataType)
	}

	return nil
}

func newKDTreeAttributeWriter(attr *Attribute) kdTreeAttributeWriter {
	return kdTreeAttributeWriter{
		attr:       attr,
		data:       attr.data,
		stride:     attr.ByteStride(),
		components: attr.NumComponents,
		dataType:   attr.DataType,
	}
}

func (w kdTreeAttributeWriter) writeUnchecked(entry int, values []uint32) error {
	offset := entry * w.stride
	buf := w.data[offset : offset+w.stride]
	switch w.dataType {
	case DataTypeUint8, DataTypeInt8:
		for i, value := range values {
			buf[i] = byte(value)
		}
	case DataTypeUint16, DataTypeInt16:
		for i, value := range values {
			binary.LittleEndian.PutUint16(buf[i*2:], uint16(value))
		}
	case DataTypeUint32, DataTypeInt32:
		for i, value := range values {
			binary.LittleEndian.PutUint32(buf[i*4:], value)
		}
	default:
		return fmt.Errorf("draco: cannot write kd-tree values into %s attribute", w.dataType)
	}

	return nil
}

func setAttributeUint32Value(attr *Attribute, entry int, values []uint32) error {
	if attr == nil {
		return fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	if len(values) != attr.NumComponents {
		return fmt.Errorf("draco: expected %d kd-tree components, got %d", attr.NumComponents, len(values))
	}

	stride := attr.ByteStride()
	if stride == 0 {
		return fmt.Errorf("%w: invalid stride", ErrInvalidGeometry)
	}

	if entry < 0 || entry >= attr.EntryCount() {
		return fmt.Errorf("%w: attribute entry %d out of range", ErrInvalidGeometry, entry)
	}

	offset := entry * stride
	buf := attr.data[offset : offset+stride]
	switch attr.DataType {
	case DataTypeUint8, DataTypeInt8:
		for i, value := range values {
			buf[i] = byte(value)
		}
	case DataTypeUint16, DataTypeInt16:
		for i, value := range values {
			binary.LittleEndian.PutUint16(buf[i*2:], uint16(value))
		}
	case DataTypeUint32, DataTypeInt32:
		for i, value := range values {
			binary.LittleEndian.PutUint32(buf[i*4:], value)
		}
	default:
		return fmt.Errorf("draco: cannot write kd-tree values into %s attribute", attr.DataType)
	}

	return nil
}

func setAttributeFloat32Value(attr *Attribute, entry int, values []float32) error {
	if attr == nil {
		return fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	if attr.DataType != DataTypeFloat32 {
		return fmt.Errorf("%w: attribute data type is %s, not FLOAT32", ErrUnsupportedFeature, attr.DataType)
	}

	if len(values) != attr.NumComponents {
		return fmt.Errorf("%w: expected %d components, got %d", ErrInvalidGeometry, attr.NumComponents, len(values))
	}

	stride := attr.ByteStride()
	if entry < 0 || entry >= attr.EntryCount() {
		return fmt.Errorf("%w: attribute entry %d out of range", ErrInvalidGeometry, entry)
	}

	buf := attr.data[entry*stride : entry*stride+stride]
	for i, value := range values {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(value))
	}

	return nil
}

func restoreSignedKDTreeAttributeEntry(attr *Attribute, entry int, values []int32) error {
	if attr == nil {
		return fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	if len(values) != attr.NumComponents {
		return fmt.Errorf("%w: expected %d components, got %d", ErrInvalidGeometry, attr.NumComponents, len(values))
	}

	stride := attr.ByteStride()
	if entry < 0 || entry >= attr.EntryCount() {
		return fmt.Errorf("%w: attribute entry %d out of range", ErrInvalidGeometry, entry)
	}

	buf := attr.data[entry*stride : entry*stride+stride]
	switch attr.DataType {
	case DataTypeInt8:
		for i, value := range values {
			buf[i] = byte(int8(value))
		}
	case DataTypeInt16:
		for i, value := range values {
			binary.LittleEndian.PutUint16(buf[i*2:], uint16(int16(value)))
		}
	case DataTypeInt32:
		for i, value := range values {
			binary.LittleEndian.PutUint32(buf[i*4:], uint32(value))
		}
	default:
		return fmt.Errorf("draco: unsupported signed kd-tree target type %s", attr.DataType)
	}

	return nil
}

func attributeMinInt32(ctx context.Context, attr *Attribute, scratch *kdTreeEncodeScratch) ([]int32, error) {
	if attr.EntryCount() == 0 {
		return nil, errors.New("draco: cannot compute signed minimum for empty attribute")
	}

	minValues := make([]int32, attr.NumComponents)
	raw, err := attr.rawEntry(0)
	if err != nil {
		return nil, err
	}

	if err := decodeRawInt32(minValues, raw, attr.DataType); err != nil {
		return nil, err
	}

	values := scratch.int32Buffer(attr.NumComponents)
	for entry := 1; entry < attr.EntryCount(); entry++ {
		if err := checkContextEvery(ctx, entry); err != nil {
			return nil, err
		}

		raw, err := attr.rawEntry(entry)
		if err != nil {
			return nil, err
		}

		if err := decodeRawInt32(values, raw, attr.DataType); err != nil {
			return nil, err
		}

		for i := range minValues {
			if values[i] < minValues[i] {
				minValues[i] = values[i]
			}
		}
	}

	return minValues, nil
}

func shiftSignedAttributeToUint32(ctx context.Context, attr *Attribute, minValues []int32, scratch *kdTreeEncodeScratch) (*Attribute, error) {
	target, err := NewAttribute(attr.Type, signedAttributePortableType(attr.DataType), attr.NumComponents, attr.EntryCount())
	if err != nil {
		return nil, err
	}

	target.Normalized = attr.Normalized
	target.UniqueID = attr.UniqueID
	target.Name = attr.Name
	if !attr.IsIdentityMapping() {
		target.mapping = append([]uint32(nil), attr.mapping...)
	}

	values := scratch.int32Buffer(attr.NumComponents)
	shifted := scratch.uint32Buffer(attr.NumComponents)
	for entry := 0; entry < attr.EntryCount(); entry++ {
		if err := checkContextEvery(ctx, entry); err != nil {
			return nil, err
		}

		raw, err := attr.rawEntry(entry)
		if err != nil {
			return nil, err
		}

		if err := decodeRawInt32(values, raw, attr.DataType); err != nil {
			return nil, err
		}

		for i, value := range values {
			shifted[i] = uint32(value - minValues[i])
		}

		if err := setAttributeUint32Value(target, entry, shifted); err != nil {
			return nil, err
		}
	}

	return target, nil
}

func signedAttributePortableType(dataType DataType) DataType {
	switch dataType {
	case DataTypeInt8:
		return DataTypeUint8
	case DataTypeInt16:
		return DataTypeUint16
	default:
		return DataTypeUint32
	}
}

func restoreSignedKDTreeAttribute(ctx context.Context, attr *Attribute, minValues []int32) error {
	values := make([]int32, attr.NumComponents)
	for entry := 0; entry < attr.EntryCount(); entry++ {
		if err := checkContextEvery(ctx, entry); err != nil {
			return err
		}

		raw, err := attr.rawEntry(entry)
		if err != nil {
			return err
		}

		switch attr.DataType {
		case DataTypeInt8:
			for i := range values {
				values[i] = int32(raw[i]) + minValues[i]
			}
		case DataTypeInt16:
			for i := range values {
				values[i] = int32(binary.LittleEndian.Uint16(raw[i*2:])) + minValues[i]
			}
		case DataTypeInt32:
			for i := range values {
				unsignedValue := binary.LittleEndian.Uint32(raw[i*4:])
				if unsignedValue > ^uint32(0)>>1 {
					return errors.New("draco: signed kd-tree value overflow")
				}

				values[i] = int32(unsignedValue) + minValues[i]
			}
		default:
			return fmt.Errorf("draco: unsupported signed kd-tree target type %s", attr.DataType)
		}

		if err := restoreSignedKDTreeAttributeEntry(attr, entry, values); err != nil {
			return err
		}
	}

	return nil
}

func quantizeAttributeToUint32(ctx context.Context, transform *quantizationTransform, source *Attribute, scratch *kdTreeEncodeScratch) (*Attribute, error) {
	maxQuantizedValue := (1 << transform.quantizationBits) - 1
	target, err := NewAttribute(source.Type, DataTypeUint32, source.NumComponents, source.EntryCount())
	if err != nil {
		return nil, err
	}

	target.Normalized = source.Normalized
	target.UniqueID = source.UniqueID
	target.Name = source.Name
	if !source.IsIdentityMapping() {
		target.mapping = append([]uint32(nil), source.mapping...)
	}

	inverseDelta := float32(maxQuantizedValue) / transform.rangeValue
	qvals := scratch.uint32Buffer(source.NumComponents)
	values := scratch.float32Buffer(source.NumComponents)
	for entry := 0; entry < source.EntryCount(); entry++ {
		if err := checkContextEvery(ctx, entry); err != nil {
			return nil, err
		}

		if source.DataType == DataTypeFloat32 {
			raw, err := source.rawEntry(entry)
			if err != nil {
				return nil, err
			}

			for i := range qvals {
				value := math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
				qvals[i] = uint32((value-transform.minValues[i])*inverseDelta + 0.5)
			}
		} else {
			entryValues, err := source.Float32(entry)
			if err != nil {
				return nil, err
			}

			copy(values, entryValues)
			for i, value := range values {
				qvals[i] = uint32((value-transform.minValues[i])*inverseDelta + 0.5)
			}
		}

		if err := setAttributeUint32Value(target, entry, qvals); err != nil {
			return nil, err
		}
	}

	return target, nil
}

func dequantizeUint32Attribute(ctx context.Context, transform *quantizationTransform, portable, target *Attribute) error {
	if portable.DataType == DataTypeUint32 && target.DataType == DataTypeFloat32 && portable.NumComponents == target.NumComponents {
		return dequantizeUint32AttributeToFloat32(ctx, transform, portable, target)
	}

	maxQuantizedValue := (1 << transform.quantizationBits) - 1
	delta := transform.rangeValue / float32(maxQuantizedValue)
	qvals := make([]uint32, portable.NumComponents)
	values := make([]float32, portable.NumComponents)
	for entry := 0; entry < portable.EntryCount(); entry++ {
		if err := checkContextEvery(ctx, entry); err != nil {
			return err
		}

		if err := writeAttributeValueAsUint32(qvals, portable, entry); err != nil {
			return err
		}

		for i, q := range qvals {
			values[i] = float32(q)*delta + transform.minValues[i]
		}

		if err := setAttributeFloat32Value(target, entry, values); err != nil {
			return err
		}
	}

	return nil
}

func dequantizeUint32AttributeToFloat32(ctx context.Context, transform *quantizationTransform, portable, target *Attribute) error {
	maxQuantizedValue := (1 << transform.quantizationBits) - 1
	delta := transform.rangeValue / float32(maxQuantizedValue)
	components := portable.NumComponents
	portableStride := portable.ByteStride()
	targetStride := target.ByteStride()
	portableData := portable.data
	targetData := target.data
	minValues := transform.minValues
	for entry := 0; entry < portable.EntryCount(); entry++ {
		if err := checkContextEvery(ctx, entry); err != nil {
			return err
		}

		src := portableData[entry*portableStride:]
		dst := targetData[entry*targetStride:]
		for component := 0; component < components; component++ {
			q := binary.LittleEndian.Uint32(src[component*4:])
			value := float32(q)*delta + minValues[component]
			binary.LittleEndian.PutUint32(dst[component*4:], math.Float32bits(value))
		}
	}

	return nil
}
