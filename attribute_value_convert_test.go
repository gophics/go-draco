package draco

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"reflect"
)

func setAttributeValueFromComponents(attr *Attribute, entry int, values any) error {
	raw, err := rawAttributeValueFromComponents(attr.DataType, attr.NumComponents, values)
	if err != nil {
		return err
	}

	return attr.SetRawValue(entry, raw)
}

func setAttributeValuesFromFlatComponents(attr *Attribute, startEntry, numEntries int, values any, offset int) error {
	if attr == nil {
		return errors.New("draco: attribute is nil")
	}

	rv, err := flatComponentValue(values)
	if err != nil {
		return err
	}

	required := offset + numEntries*attr.NumComponents
	if rv.Len() < required {
		return fmt.Errorf("draco: flat value length %d too small for %d entries with %d components at offset %d", rv.Len(), numEntries, attr.NumComponents, offset)
	}

	for entry := 0; entry < numEntries; entry++ {
		raw := make([]byte, attr.ByteStride())
		for component := 0; component < attr.NumComponents; component++ {
			if err := writeAttributeComponent(raw, attr.DataType, component, rv.Index(offset+entry*attr.NumComponents+component)); err != nil {
				return err
			}
		}

		if err := attr.SetRawValue(startEntry+entry, raw); err != nil {
			return err
		}
	}

	return nil
}

func rawAttributeValueFromComponents(dataType DataType, numComponents int, values any) ([]byte, error) {
	components, err := componentValues(values, numComponents)
	if err != nil {
		return nil, err
	}

	raw := make([]byte, DataTypeLength(dataType)*numComponents)
	for i, value := range components {
		if err := writeAttributeComponent(raw, dataType, i, value); err != nil {
			return nil, err
		}
	}

	return raw, nil
}

func componentValues(values any, expected int) ([]reflect.Value, error) {
	rv := reflect.ValueOf(values)
	if !rv.IsValid() {
		return nil, errors.New("draco: attribute value is nil")
	}

	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, errors.New("draco: attribute value is nil")
		}

		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Array, reflect.Slice:
		if rv.Len() != expected {
			return nil, fmt.Errorf("draco: expected %d components, got %d", expected, rv.Len())
		}

		out := make([]reflect.Value, expected)
		for i := range out {
			out[i] = rv.Index(i)
		}

		return out, nil
	default:
		if expected != 1 {
			return nil, fmt.Errorf("draco: expected %d components, got scalar value", expected)
		}

		return []reflect.Value{rv}, nil
	}
}

func flatComponentValue(values any) (reflect.Value, error) {
	rv := reflect.ValueOf(values)
	if !rv.IsValid() {
		return reflect.Value{}, errors.New("draco: flat attribute values are nil")
	}

	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return reflect.Value{}, errors.New("draco: flat attribute values are nil")
		}

		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Array && rv.Kind() != reflect.Slice {
		return reflect.Value{}, errors.New("draco: flat attribute values must be a slice or array")
	}

	return rv, nil
}

func writeAttributeComponent(raw []byte, dataType DataType, component int, value reflect.Value) error {
	offset := component * DataTypeLength(dataType)
	switch dataType {
	case DataTypeInt8:
		v, err := int64Value(value)
		if err != nil {
			return err
		}

		raw[offset] = byte(int8(v))
	case DataTypeUint8:
		v, err := uint64Value(value)
		if err != nil {
			return err
		}

		raw[offset] = byte(v)
	case DataTypeInt16:
		v, err := int64Value(value)
		if err != nil {
			return err
		}

		binary.LittleEndian.PutUint16(raw[offset:], uint16(int16(v)))
	case DataTypeUint16:
		v, err := uint64Value(value)
		if err != nil {
			return err
		}

		binary.LittleEndian.PutUint16(raw[offset:], uint16(v))
	case DataTypeInt32:
		v, err := int64Value(value)
		if err != nil {
			return err
		}

		binary.LittleEndian.PutUint32(raw[offset:], uint32(int32(v)))
	case DataTypeUint32:
		v, err := uint64Value(value)
		if err != nil {
			return err
		}

		binary.LittleEndian.PutUint32(raw[offset:], uint32(v))
	case DataTypeInt64:
		v, err := int64Value(value)
		if err != nil {
			return err
		}

		binary.LittleEndian.PutUint64(raw[offset:], uint64(v))
	case DataTypeUint64:
		v, err := uint64Value(value)
		if err != nil {
			return err
		}

		binary.LittleEndian.PutUint64(raw[offset:], v)
	case DataTypeFloat32:
		v, err := float64Value(value)
		if err != nil {
			return err
		}

		binary.LittleEndian.PutUint32(raw[offset:], math.Float32bits(float32(v)))
	case DataTypeFloat64:
		v, err := float64Value(value)
		if err != nil {
			return err
		}

		binary.LittleEndian.PutUint64(raw[offset:], math.Float64bits(v))
	case DataTypeBool:
		v, err := boolValue(value)
		if err != nil {
			return err
		}

		if v {
			raw[offset] = 1
		} else {
			raw[offset] = 0
		}
	default:
		return fmt.Errorf("draco: unsupported attribute data type %s", dataType)
	}

	return nil
}

func int64Value(value reflect.Value) (int64, error) {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0, errors.New("draco: value is nil")
		}

		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return int64(value.Uint()), nil
	case reflect.Float32, reflect.Float64:
		return int64(value.Float()), nil
	case reflect.Bool:
		if value.Bool() {
			return 1, nil
		}

		return 0, nil
	default:
		return 0, fmt.Errorf("draco: cannot convert %s to int64", value.Kind())
	}
}

func uint64Value(value reflect.Value) (uint64, error) {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0, errors.New("draco: value is nil")
		}

		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return uint64(value.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint(), nil
	case reflect.Float32, reflect.Float64:
		return uint64(value.Float()), nil
	case reflect.Bool:
		if value.Bool() {
			return 1, nil
		}

		return 0, nil
	default:
		return 0, fmt.Errorf("draco: cannot convert %s to uint64", value.Kind())
	}
}

func float64Value(value reflect.Value) (float64, error) {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0, errors.New("draco: value is nil")
		}

		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(value.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(value.Uint()), nil
	case reflect.Float32, reflect.Float64:
		return value.Float(), nil
	case reflect.Bool:
		if value.Bool() {
			return 1, nil
		}

		return 0, nil
	default:
		return 0, fmt.Errorf("draco: cannot convert %s to float64", value.Kind())
	}
}

func boolValue(value reflect.Value) (bool, error) {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return false, errors.New("draco: value is nil")
		}

		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.Bool:
		return value.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int() != 0, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint() != 0, nil
	case reflect.Float32, reflect.Float64:
		return value.Float() != 0, nil
	default:
		return false, fmt.Errorf("draco: cannot convert %s to bool", value.Kind())
	}
}
