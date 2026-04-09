package draco

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

func quantizeOctahedronAttribute(ctx context.Context, transform *octahedronTransform, source *Attribute) (*Attribute, error) {
	if source == nil {
		return nil, errors.New("draco: octahedron source attribute is nil")
	}

	if source.DataType != DataTypeFloat32 {
		return nil, fmt.Errorf("draco: octahedron source type is %s, want FLOAT32", source.DataType)
	}

	if source.NumComponents != 3 {
		return nil, fmt.Errorf("draco: octahedron source components = %d, want 3", source.NumComponents)
	}

	target, err := NewAttribute(source.Type, DataTypeInt32, 2, source.EntryCount())
	if err != nil {
		return nil, err
	}

	target.UniqueID = source.UniqueID
	target.Normalized = false
	if !source.IsIdentityMapping() {
		target.mapping = append([]uint32(nil), source.mapping...)
	}

	for entry := 0; entry < source.EntryCount(); entry++ {
		if err := checkContextEvery(ctx, entry); err != nil {
			return nil, err
		}

		values, err := source.Float32(entry)
		if err != nil {
			return nil, err
		}

		s, tt := transform.FloatVectorToQuantizedOctahedralCoords(values)
		if err := target.SetInt32(entry, []int32{s, tt}); err != nil {
			return nil, err
		}
	}

	return target, nil
}

func dequantizeOctahedronAttribute(ctx context.Context, transform *octahedronTransform, portable, target *Attribute) error {
	if portable == nil || target == nil {
		return errors.New("draco: octahedron transform requires source and target attributes")
	}

	if portable.DataType != DataTypeInt32 && portable.DataType != DataTypeUint32 {
		return fmt.Errorf("draco: octahedron source type is %s, want INT32/UINT32", portable.DataType)
	}

	if portable.NumComponents != 2 {
		return fmt.Errorf("draco: octahedron portable components = %d, want 2", portable.NumComponents)
	}

	if target.DataType != DataTypeFloat32 || target.NumComponents != 3 {
		return errors.New("draco: octahedron target must be FLOAT32x3")
	}

	return dequantizeOctahedronAttributeToFloat32(ctx, transform, portable, target)
}

func dequantizeOctahedronAttributeToFloat32(ctx context.Context, transform *octahedronTransform, portable, target *Attribute) error {
	portableStride := portable.ByteStride()
	targetStride := target.ByteStride()
	portableData := portable.data
	targetData := target.data
	switch portable.DataType {
	case DataTypeInt32:
		for entry := 0; entry < portable.EntryCount(); entry++ {
			if err := checkContextEvery(ctx, entry); err != nil {
				return err
			}

			src := portableData[entry*portableStride:]
			s := int32(binary.LittleEndian.Uint32(src))
			tt := int32(binary.LittleEndian.Uint32(src[4:]))
			vec := transform.QuantizedOctahedralCoordsToUnitVector(s, tt)
			dst := targetData[entry*targetStride:]
			binary.LittleEndian.PutUint32(dst, math.Float32bits(vec[0]))
			binary.LittleEndian.PutUint32(dst[4:], math.Float32bits(vec[1]))
			binary.LittleEndian.PutUint32(dst[8:], math.Float32bits(vec[2]))
		}

		return nil
	case DataTypeUint32:
		for entry := 0; entry < portable.EntryCount(); entry++ {
			if err := checkContextEvery(ctx, entry); err != nil {
				return err
			}

			src := portableData[entry*portableStride:]
			sValue := binary.LittleEndian.Uint32(src)
			ttValue := binary.LittleEndian.Uint32(src[4:])
			if sValue > math.MaxInt32 || ttValue > math.MaxInt32 {
				return errors.New("draco: UINT32 octahedron value overflows int32")
			}

			vec := transform.QuantizedOctahedralCoordsToUnitVector(int32(sValue), int32(ttValue))
			dst := targetData[entry*targetStride:]
			binary.LittleEndian.PutUint32(dst, math.Float32bits(vec[0]))
			binary.LittleEndian.PutUint32(dst[4:], math.Float32bits(vec[1]))
			binary.LittleEndian.PutUint32(dst[8:], math.Float32bits(vec[2]))
		}

		return nil
	}

	var st [2]int32
	var vecValues [3]float32
	for entry := 0; entry < portable.EntryCount(); entry++ {
		if err := checkContextEvery(ctx, entry); err != nil {
			return err
		}

		if err := decodeInt32AttributeEntry(st[:], portable, entry); err != nil {
			return fmt.Errorf("draco: octahedron dequantize portable entry=%d numEntries=%d: %w", entry, portable.EntryCount(), err)
		}

		vec := transform.QuantizedOctahedralCoordsToUnitVector(st[0], st[1])
		vecValues[0], vecValues[1], vecValues[2] = vec[0], vec[1], vec[2]
		if err := setAttributeFloat32Value(target, entry, vecValues[:]); err != nil {
			return fmt.Errorf("draco: octahedron dequantize target entry=%d targetEntries=%d: %w", entry, target.EntryCount(), err)
		}
	}

	return nil
}
