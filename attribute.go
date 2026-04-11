package draco

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

type Attribute struct {
	Type          AttributeType
	DataType      DataType
	NumComponents int
	Normalized    bool
	UniqueID      uint32
	Name          string

	data    []byte
	mapping []uint32
}

func NewAttribute(attType AttributeType, dataType DataType, numComponents, numEntries int) (*Attribute, error) {
	if numComponents <= 0 {
		return nil, fmt.Errorf("%w: invalid component count %d", ErrInvalidGeometry, numComponents)
	}

	stride := DataTypeLength(dataType) * numComponents
	if stride == 0 {
		return nil, fmt.Errorf("%w: invalid data type %d", ErrInvalidGeometry, dataType)
	}

	if numEntries < 0 {
		return nil, fmt.Errorf("%w: invalid attribute entry count %d", ErrInvalidGeometry, numEntries)
	}

	if err := guardSliceAllocation(numEntries, uintptr(stride), "attribute data"); err != nil {
		return nil, err
	}

	return &Attribute{
		Type:          attType,
		DataType:      dataType,
		NumComponents: numComponents,
		data:          make([]byte, stride*numEntries),
	}, nil
}

func (a *Attribute) Clone() *Attribute {
	if a == nil {
		return nil
	}

	out := *a
	out.data = append([]byte(nil), a.data...)
	if a.mapping != nil {
		out.mapping = append([]uint32(nil), a.mapping...)
	}

	return &out
}

func cloneAttributeForPoints(attr *Attribute, pointIDs []int) (*Attribute, error) {
	cloned, err := NewAttribute(attr.Type, attr.DataType, attr.NumComponents, len(pointIDs))
	if err != nil {
		return nil, err
	}

	cloned.Normalized = attr.Normalized
	cloned.UniqueID = attr.UniqueID
	cloned.Name = attr.Name
	for newPointID, oldPointID := range pointIDs {
		raw, err := attr.RawValue(int(attr.mappedIndex(oldPointID)))
		if err != nil {
			return nil, err
		}

		if err := cloned.SetRawValue(newPointID, raw); err != nil {
			return nil, err
		}
	}

	return cloned, nil
}

func (a *Attribute) ByteStride() int {
	if a == nil {
		return 0
	}

	return DataTypeLength(a.DataType) * a.NumComponents
}

func (a *Attribute) EntryCount() int {
	stride := a.ByteStride()
	if stride == 0 {
		return 0
	}

	return len(a.data) / stride
}

func (a *Attribute) IsIdentityMapping() bool {
	if a == nil {
		return false
	}

	return a.mapping == nil
}

func (a *Attribute) SetIdentityMapping() error {
	if a == nil {
		return fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	a.mapping = nil
	return nil
}

func (a *Attribute) SetExplicitMapping(numPoints int) error {
	if a == nil {
		return fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	if numPoints < 0 {
		return fmt.Errorf("%w: explicit mapping point count %d is negative", ErrInvalidGeometry, numPoints)
	}

	if err := guardSliceAllocation(numPoints, 4, "attribute mapping"); err != nil {
		return err
	}

	a.mapping = make([]uint32, numPoints)
	return nil
}

func (a *Attribute) MappingSize() int {
	if a == nil {
		return 0
	}

	return len(a.mapping)
}

func (a *Attribute) SetPointMapEntry(point int, entry uint32) error {
	if a == nil {
		return fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	if point < 0 || point >= len(a.mapping) {
		return fmt.Errorf("%w: point index %d out of range for mapping size %d", ErrInvalidGeometry, point, len(a.mapping))
	}

	a.mapping[point] = entry
	return nil
}

func (a *Attribute) MappedIndex(point int) (uint32, error) {
	if a == nil {
		return 0, fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	if point < 0 {
		return 0, fmt.Errorf("%w: point index %d out of range", ErrInvalidGeometry, point)
	}

	if a.mapping == nil {
		if point >= a.EntryCount() {
			return 0, fmt.Errorf("%w: point index %d out of range for %d entries", ErrInvalidGeometry, point, a.EntryCount())
		}

		return uint32(point), nil
	}

	if point >= len(a.mapping) {
		return 0, fmt.Errorf("%w: point index %d out of range for mapping size %d", ErrInvalidGeometry, point, len(a.mapping))
	}

	return a.mapping[point], nil
}

func (a *Attribute) mappedIndex(point int) uint32 {
	if a.mapping == nil {
		return uint32(point)
	}

	return a.mapping[point]
}

func (a *Attribute) Equivalent(other *Attribute) bool {
	if a == nil || other == nil {
		return a == other
	}

	if a.Type != other.Type ||
		a.DataType != other.DataType ||
		a.NumComponents != other.NumComponents ||
		a.Normalized != other.Normalized ||
		a.UniqueID != other.UniqueID ||
		a.Name != other.Name {
		return false
	}

	if !bytes.Equal(a.data, other.data) {
		return false
	}

	if len(a.mapping) != len(other.mapping) {
		return false
	}

	for i := range a.mapping {
		if a.mapping[i] != other.mapping[i] {
			return false
		}
	}

	return true
}

func (a *Attribute) RawValue(entry int) ([]byte, error) {
	raw, err := a.rawEntry(entry)
	if err != nil {
		return nil, err
	}

	return append([]byte(nil), raw...), nil
}

func (a *Attribute) SetRawValue(entry int, raw []byte) error {
	if a == nil {
		return fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	stride := a.ByteStride()
	if len(raw) != stride {
		return fmt.Errorf("%w: raw value size %d does not match stride %d", ErrInvalidGeometry, len(raw), stride)
	}

	if entry < 0 || entry >= a.EntryCount() {
		return fmt.Errorf("%w: attribute entry %d out of range", ErrInvalidGeometry, entry)
	}

	offset := entry * stride
	copy(a.data[offset:offset+stride], raw)
	return nil
}

func (a *Attribute) SetFloat32(entry int, values ...float32) error {
	if a == nil {
		return fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	if a.DataType != DataTypeFloat32 {
		return fmt.Errorf("%w: attribute data type is %s, not FLOAT32", ErrUnsupportedFeature, a.DataType)
	}

	if len(values) != a.NumComponents {
		return fmt.Errorf("%w: expected %d components, got %d", ErrInvalidGeometry, a.NumComponents, len(values))
	}

	buf := make([]byte, a.ByteStride())
	for i, v := range values {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}

	return a.SetRawValue(entry, buf)
}

func (a *Attribute) Float32(entry int) ([]float32, error) {
	raw, err := a.rawEntry(entry)
	if err != nil {
		return nil, err
	}

	out := make([]float32, a.NumComponents)
	return out, decodeRawFloat32(out, raw, a.DataType, a.Normalized)
}

func (a *Attribute) Int32(entry int) ([]int32, error) {
	raw, err := a.rawEntry(entry)
	if err != nil {
		return nil, err
	}

	out := make([]int32, a.NumComponents)
	return out, decodeRawInt32(out, raw, a.DataType)
}

func (a *Attribute) SetInt32(entry int, values []int32) error {
	if a == nil {
		return fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	if len(values) != a.NumComponents {
		return fmt.Errorf("%w: expected %d components, got %d", ErrInvalidGeometry, a.NumComponents, len(values))
	}

	buf := make([]byte, a.ByteStride())
	switch a.DataType {
	case DataTypeInt8:
		for i, v := range values {
			buf[i] = byte(int8(v))
		}
	case DataTypeUint8:
		for i, v := range values {
			buf[i] = byte(v)
		}
	case DataTypeInt16:
		for i, v := range values {
			binary.LittleEndian.PutUint16(buf[i*2:], uint16(int16(v)))
		}
	case DataTypeUint16:
		for i, v := range values {
			binary.LittleEndian.PutUint16(buf[i*2:], uint16(v))
		}
	case DataTypeInt32:
		for i, v := range values {
			binary.LittleEndian.PutUint32(buf[i*4:], uint32(v))
		}
	case DataTypeUint32:
		for i, v := range values {
			binary.LittleEndian.PutUint32(buf[i*4:], uint32(v))
		}
	case DataTypeBool:
		for i, v := range values {
			if v != 0 {
				buf[i] = 1
			}
		}
	default:
		return fmt.Errorf("%w: cannot store int32 values in %s attribute", ErrUnsupportedFeature, a.DataType)
	}

	return a.SetRawValue(entry, buf)
}

func convertSignedToFloat32(value, maxPositive int64, normalized bool) float32 {
	if !normalized {
		return float32(value)
	}

	if value > maxPositive {
		value = maxPositive
	}

	if value < -maxPositive {
		value = -maxPositive
	}

	return float32(float64(value) / float64(maxPositive))
}

func convertUnsignedToFloat32(value, maxValue uint64, normalized bool) float32 {
	if !normalized {
		return float32(value)
	}

	if value > maxValue {
		value = maxValue
	}

	return float32(float64(value) / float64(maxValue))
}
