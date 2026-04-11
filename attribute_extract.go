package draco

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
)

// AttributeDescriptors reports the schema of all attributes on the point cloud.
func (pc *PointCloud) AttributeDescriptors() []AttributeDescriptor {
	if pc == nil {
		return nil
	}

	out := make([]AttributeDescriptor, len(pc.attributes))
	for i, attr := range pc.attributes {
		out[i] = attr.Descriptor()
	}

	return out
}

// ExtractRaw returns the entry-ordered raw attribute payload.
func (a *Attribute) ExtractRaw() []byte {
	if a == nil {
		return nil
	}

	return append([]byte(nil), a.data...)
}

// AppendRaw appends the entry-ordered raw attribute payload to dst.
func (a *Attribute) AppendRaw(dst []byte) []byte {
	if a == nil {
		return dst
	}

	return append(dst, a.data...)
}

// ExtractFloat32 returns the entry-ordered attribute values converted to float32.
func (a *Attribute) ExtractFloat32() ([]float32, error) {
	return a.AppendFloat32(nil)
}

// AppendFloat32 appends the entry-ordered attribute values converted to float32.
func (a *Attribute) AppendFloat32(dst []float32) ([]float32, error) {
	return appendAttributeFloat32(dst, a, a.EntryCount(), func(entry int) int { return entry })
}

// ExtractInt32 returns the entry-ordered attribute values converted to int32.
func (a *Attribute) ExtractInt32() ([]int32, error) {
	return a.AppendInt32(nil)
}

// AppendInt32 appends the entry-ordered attribute values converted to int32.
func (a *Attribute) AppendInt32(dst []int32) ([]int32, error) {
	return appendAttributeInt32(dst, a, a.EntryCount(), func(entry int) int { return entry })
}

// ExtractMappedRaw returns the point-ordered raw attribute payload for attID.
func (pc *PointCloud) ExtractMappedRaw(attID int) ([]byte, error) {
	return pc.AppendMappedRaw(attID, nil)
}

// AppendMappedRaw appends the point-ordered raw attribute payload for attID.
func (pc *PointCloud) AppendMappedRaw(attID int, dst []byte) ([]byte, error) {
	attr, err := pc.attributeForExtraction(attID)
	if err != nil {
		return nil, err
	}

	return appendAttributeRaw(dst, attr, pc.PointCount(), func(pointID int) int {
		return int(attr.mappedIndex(pointID))
	})
}

// ExtractMappedFloat32 returns the point-ordered attribute values converted to float32.
func (pc *PointCloud) ExtractMappedFloat32(attID int) ([]float32, error) {
	return pc.AppendMappedFloat32(attID, nil)
}

// AppendMappedFloat32 appends the point-ordered attribute values converted to float32.
func (pc *PointCloud) AppendMappedFloat32(attID int, dst []float32) ([]float32, error) {
	attr, err := pc.attributeForExtraction(attID)
	if err != nil {
		return nil, err
	}

	return appendAttributeFloat32(dst, attr, pc.PointCount(), func(pointID int) int {
		return int(attr.mappedIndex(pointID))
	})
}

// ExtractMappedInt32 returns the point-ordered attribute values converted to int32.
func (pc *PointCloud) ExtractMappedInt32(attID int) ([]int32, error) {
	return pc.AppendMappedInt32(attID, nil)
}

// AppendMappedInt32 appends the point-ordered attribute values converted to int32.
func (pc *PointCloud) AppendMappedInt32(attID int, dst []int32) ([]int32, error) {
	attr, err := pc.attributeForExtraction(attID)
	if err != nil {
		return nil, err
	}

	return appendAttributeInt32(dst, attr, pc.PointCount(), func(pointID int) int {
		return int(attr.mappedIndex(pointID))
	})
}

func (pc *PointCloud) attributeForExtraction(attID int) (*Attribute, error) {
	if pc == nil {
		return nil, fmt.Errorf("%w: point cloud is nil", ErrInvalidGeometry)
	}

	if attID < 0 || attID >= len(pc.attributes) {
		return nil, fmt.Errorf("%w: attribute %d out of range", ErrInvalidGeometry, attID)
	}

	return pc.attributes[attID], nil
}

func appendAttributeRaw(dst []byte, attr *Attribute, count int, entryAt func(int) int) ([]byte, error) {
	if attr == nil {
		return nil, fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	stride := attr.ByteStride()
	if stride == 0 {
		return nil, fmt.Errorf("%w: invalid attribute stride", ErrInvalidGeometry)
	}

	dst, out := growBytes(dst, count*stride)
	for i := 0; i < count; i++ {
		raw, err := attr.rawEntry(entryAt(i))
		if err != nil {
			return nil, err
		}

		copy(out[i*stride:], raw)
	}

	return dst, nil
}

func appendAttributeFloat32(dst []float32, attr *Attribute, count int, entryAt func(int) int) ([]float32, error) {
	if attr == nil {
		return nil, fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	dst, out := growFloat32(dst, count*attr.NumComponents)
	for i := 0; i < count; i++ {
		raw, err := attr.rawEntry(entryAt(i))
		if err != nil {
			return nil, err
		}

		start := i * attr.NumComponents
		if err := decodeRawFloat32(out[start:start+attr.NumComponents], raw, attr.DataType, attr.Normalized); err != nil {
			return nil, err
		}
	}

	return dst, nil
}

func appendAttributeInt32(dst []int32, attr *Attribute, count int, entryAt func(int) int) ([]int32, error) {
	if attr == nil {
		return nil, fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	dst, out := growInt32(dst, count*attr.NumComponents)
	for i := 0; i < count; i++ {
		raw, err := attr.rawEntry(entryAt(i))
		if err != nil {
			return nil, err
		}

		start := i * attr.NumComponents
		if err := decodeRawInt32(out[start:start+attr.NumComponents], raw, attr.DataType); err != nil {
			return nil, err
		}
	}

	return dst, nil
}

func (a *Attribute) rawEntry(entry int) ([]byte, error) {
	if a == nil {
		return nil, fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	stride := a.ByteStride()
	if stride == 0 {
		return nil, fmt.Errorf("%w: invalid stride", ErrInvalidGeometry)
	}

	if entry < 0 || entry >= a.EntryCount() {
		return nil, fmt.Errorf("%w: attribute entry %d out of range", ErrInvalidGeometry, entry)
	}

	offset := entry * stride
	return a.data[offset : offset+stride], nil
}

func decodeRawFloat32(dst []float32, raw []byte, dataType DataType, normalized bool) error {
	switch dataType {
	case DataTypeFloat32:
		for i := range dst {
			dst[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
		}
	case DataTypeFloat64:
		for i := range dst {
			dst[i] = float32(math.Float64frombits(binary.LittleEndian.Uint64(raw[i*8:])))
		}
	case DataTypeInt8:
		for i := range dst {
			dst[i] = convertSignedToFloat32(int64(int8(raw[i])), math.MaxInt8, normalized)
		}
	case DataTypeUint8:
		for i := range dst {
			dst[i] = convertUnsignedToFloat32(uint64(raw[i]), math.MaxUint8, normalized)
		}
	case DataTypeInt16:
		for i := range dst {
			dst[i] = convertSignedToFloat32(int64(int16(binary.LittleEndian.Uint16(raw[i*2:]))), math.MaxInt16, normalized)
		}
	case DataTypeUint16:
		for i := range dst {
			dst[i] = convertUnsignedToFloat32(uint64(binary.LittleEndian.Uint16(raw[i*2:])), math.MaxUint16, normalized)
		}
	case DataTypeInt32:
		for i := range dst {
			dst[i] = convertSignedToFloat32(int64(int32(binary.LittleEndian.Uint32(raw[i*4:]))), math.MaxInt32, normalized)
		}
	case DataTypeUint32:
		for i := range dst {
			dst[i] = convertUnsignedToFloat32(uint64(binary.LittleEndian.Uint32(raw[i*4:])), math.MaxUint32, normalized)
		}
	case DataTypeInt64:
		for i := range dst {
			dst[i] = convertSignedToFloat32(int64(binary.LittleEndian.Uint64(raw[i*8:])), math.MaxInt64, normalized)
		}
	case DataTypeUint64:
		for i := range dst {
			dst[i] = convertUnsignedToFloat32(binary.LittleEndian.Uint64(raw[i*8:]), math.MaxUint64, normalized)
		}
	case DataTypeBool:
		for i := range dst {
			if raw[i] != 0 {
				dst[i] = 1
			} else {
				dst[i] = 0
			}
		}
	default:
		return fmt.Errorf("draco: cannot convert %s attribute to float32", dataType)
	}

	return nil
}

func decodeRawInt32(dst []int32, raw []byte, dataType DataType) error {
	switch dataType {
	case DataTypeInt8:
		for i := range dst {
			dst[i] = int32(int8(raw[i]))
		}
	case DataTypeUint8:
		for i := range dst {
			dst[i] = int32(raw[i])
		}
	case DataTypeInt16:
		for i := range dst {
			dst[i] = int32(int16(binary.LittleEndian.Uint16(raw[i*2:])))
		}
	case DataTypeUint16:
		for i := range dst {
			dst[i] = int32(binary.LittleEndian.Uint16(raw[i*2:]))
		}
	case DataTypeInt32:
		for i := range dst {
			dst[i] = int32(binary.LittleEndian.Uint32(raw[i*4:]))
		}
	case DataTypeUint32:
		for i := range dst {
			value := binary.LittleEndian.Uint32(raw[i*4:])
			if value > math.MaxInt32 {
				return fmt.Errorf("draco: UINT32 value %d overflows int32", value)
			}

			dst[i] = int32(value)
		}
	case DataTypeInt64:
		for i := range dst {
			value := int64(binary.LittleEndian.Uint64(raw[i*8:]))
			if value < math.MinInt32 || value > math.MaxInt32 {
				return fmt.Errorf("draco: INT64 value %d overflows int32", value)
			}

			dst[i] = int32(value)
		}
	case DataTypeUint64:
		for i := range dst {
			value := binary.LittleEndian.Uint64(raw[i*8:])
			if value > math.MaxInt32 {
				return fmt.Errorf("draco: UINT64 value %d overflows int32", value)
			}

			dst[i] = int32(value)
		}
	case DataTypeFloat32:
		for i := range dst {
			value := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:])))
			if math.IsNaN(value) || math.IsInf(value, 0) || value < math.MinInt32 || value > math.MaxInt32 {
				return fmt.Errorf("draco: FLOAT32 value %v cannot convert to int32", value)
			}

			dst[i] = int32(value)
		}
	case DataTypeFloat64:
		for i := range dst {
			value := math.Float64frombits(binary.LittleEndian.Uint64(raw[i*8:]))
			if math.IsNaN(value) || math.IsInf(value, 0) || value < math.MinInt32 || value > math.MaxInt32 {
				return fmt.Errorf("draco: FLOAT64 value %v cannot convert to int32", value)
			}

			dst[i] = int32(value)
		}
	case DataTypeBool:
		for i := range dst {
			if raw[i] != 0 {
				dst[i] = 1
			} else {
				dst[i] = 0
			}
		}
	default:
		return fmt.Errorf("draco: cannot convert %s attribute to int32", dataType)
	}

	return nil
}

func decodeInt32AttributeEntry(dst []int32, attr *Attribute, entry int) error {
	raw, err := attr.rawEntry(entry)
	if err != nil {
		return err
	}

	return decodeRawInt32(dst, raw, attr.DataType)
}

func growBytes(dst []byte, n int) ([]byte, []byte) {
	old := len(dst)
	if n <= 0 {
		return dst, dst[old:old]
	}

	dst = slices.Grow(dst, n)
	dst = dst[:old+n]
	return dst, dst[old:]
}

func growFloat32(dst []float32, n int) ([]float32, []float32) {
	old := len(dst)
	if n <= 0 {
		return dst, dst[old:old]
	}

	dst = slices.Grow(dst, n)
	dst = dst[:old+n]
	return dst, dst[old:]
}

func growInt32(dst []int32, n int) ([]int32, []int32) {
	old := len(dst)
	if n <= 0 {
		return dst, dst[old:old]
	}

	dst = slices.Grow(dst, n)
	dst = dst[:old+n]
	return dst, dst[old:]
}
