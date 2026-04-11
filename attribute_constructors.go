package draco

import (
	"encoding/binary"
	"fmt"
	"math"
)

// NewRawAttribute constructs an attribute from raw encoded element bytes.
func NewRawAttribute(attType AttributeType, dataType DataType, numComponents int, raw []byte) (*Attribute, error) {
	stride := DataTypeLength(dataType) * numComponents
	if stride <= 0 {
		return nil, fmt.Errorf("%w: invalid raw attribute schema", ErrInvalidGeometry)
	}

	if len(raw)%stride != 0 {
		return nil, fmt.Errorf("%w: raw attribute length %d is not divisible by stride %d", ErrInvalidGeometry, len(raw), stride)
	}

	attr, err := NewAttribute(attType, dataType, numComponents, len(raw)/stride)
	if err != nil {
		return nil, err
	}

	copy(attr.data, raw)
	return attr, nil
}

// NewFloat32Attribute constructs a FLOAT32 attribute from flat component values.
func NewFloat32Attribute(attType AttributeType, numComponents int, values []float32) (*Attribute, error) {
	return newAttributeFromScalars(attType, DataTypeFloat32, numComponents, values, func(raw []byte, offset int, value float32) {
		binary.LittleEndian.PutUint32(raw[offset:], math.Float32bits(value))
	})
}

// NewFloat64Attribute constructs a FLOAT64 attribute from flat component values.
func NewFloat64Attribute(attType AttributeType, numComponents int, values []float64) (*Attribute, error) {
	return newAttributeFromScalars(attType, DataTypeFloat64, numComponents, values, func(raw []byte, offset int, value float64) {
		binary.LittleEndian.PutUint64(raw[offset:], math.Float64bits(value))
	})
}

// NewInt8Attribute constructs an INT8 attribute from flat component values.
func NewInt8Attribute(attType AttributeType, numComponents int, values []int8) (*Attribute, error) {
	return newAttributeFromScalars(attType, DataTypeInt8, numComponents, values, func(raw []byte, offset int, value int8) {
		raw[offset] = byte(value)
	})
}

// NewUint8Attribute constructs a UINT8 attribute from flat component values.
func NewUint8Attribute(attType AttributeType, numComponents int, values []uint8) (*Attribute, error) {
	return newAttributeFromScalars(attType, DataTypeUint8, numComponents, values, func(raw []byte, offset int, value uint8) {
		raw[offset] = value
	})
}

// NewInt16Attribute constructs an INT16 attribute from flat component values.
func NewInt16Attribute(attType AttributeType, numComponents int, values []int16) (*Attribute, error) {
	return newAttributeFromScalars(attType, DataTypeInt16, numComponents, values, func(raw []byte, offset int, value int16) {
		binary.LittleEndian.PutUint16(raw[offset:], uint16(value))
	})
}

// NewUint16Attribute constructs a UINT16 attribute from flat component values.
func NewUint16Attribute(attType AttributeType, numComponents int, values []uint16) (*Attribute, error) {
	return newAttributeFromScalars(attType, DataTypeUint16, numComponents, values, func(raw []byte, offset int, value uint16) {
		binary.LittleEndian.PutUint16(raw[offset:], value)
	})
}

// NewInt32Attribute constructs an INT32 attribute from flat component values.
func NewInt32Attribute(attType AttributeType, numComponents int, values []int32) (*Attribute, error) {
	return newAttributeFromScalars(attType, DataTypeInt32, numComponents, values, func(raw []byte, offset int, value int32) {
		binary.LittleEndian.PutUint32(raw[offset:], uint32(value))
	})
}

// NewUint32Attribute constructs a UINT32 attribute from flat component values.
func NewUint32Attribute(attType AttributeType, numComponents int, values []uint32) (*Attribute, error) {
	return newAttributeFromScalars(attType, DataTypeUint32, numComponents, values, func(raw []byte, offset int, value uint32) {
		binary.LittleEndian.PutUint32(raw[offset:], value)
	})
}

// NewInt64Attribute constructs an INT64 attribute from flat component values.
func NewInt64Attribute(attType AttributeType, numComponents int, values []int64) (*Attribute, error) {
	return newAttributeFromScalars(attType, DataTypeInt64, numComponents, values, func(raw []byte, offset int, value int64) {
		binary.LittleEndian.PutUint64(raw[offset:], uint64(value))
	})
}

// NewUint64Attribute constructs a UINT64 attribute from flat component values.
func NewUint64Attribute(attType AttributeType, numComponents int, values []uint64) (*Attribute, error) {
	return newAttributeFromScalars(attType, DataTypeUint64, numComponents, values, func(raw []byte, offset int, value uint64) {
		binary.LittleEndian.PutUint64(raw[offset:], value)
	})
}

// NewBoolAttribute constructs a BOOL attribute from flat component values.
func NewBoolAttribute(attType AttributeType, numComponents int, values []bool) (*Attribute, error) {
	return newAttributeFromScalars(attType, DataTypeBool, numComponents, values, func(raw []byte, offset int, value bool) {
		if value {
			raw[offset] = 1
		} else {
			raw[offset] = 0
		}
	})
}

func newAttributeFromScalars[T any](attType AttributeType, dataType DataType, numComponents int, values []T, write func([]byte, int, T)) (*Attribute, error) {
	if numComponents <= 0 {
		return nil, fmt.Errorf("%w: invalid component count %d", ErrInvalidGeometry, numComponents)
	}

	if len(values)%numComponents != 0 {
		return nil, fmt.Errorf("%w: flat value count %d is not divisible by %d components", ErrInvalidGeometry, len(values), numComponents)
	}

	attr, err := NewAttribute(attType, dataType, numComponents, len(values)/numComponents)
	if err != nil {
		return nil, err
	}

	componentWidth := DataTypeLength(dataType)
	stride := attr.ByteStride()
	for entry := 0; entry < attr.EntryCount(); entry++ {
		raw := make([]byte, stride)
		base := entry * numComponents
		for component := 0; component < numComponents; component++ {
			write(raw, component*componentWidth, values[base+component])
		}

		if err := attr.SetRawValue(entry, raw); err != nil {
			return nil, err
		}
	}

	return attr, nil
}
