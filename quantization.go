package draco

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"

	"github.com/gophics/go-draco/internal/core"
)

type quantizationTransform struct {
	minValues        []float32
	rangeValue       float32
	quantizationBits uint8
}

func requireFloat32QuantizationSource(attr *Attribute) error {
	if attr == nil {
		return errors.New("draco: quantization source attribute is nil")
	}

	if attr.DataType != DataTypeFloat32 {
		return fmt.Errorf("draco: quantization source type is %s, want FLOAT32", attr.DataType)
	}

	if attr.EntryCount() == 0 {
		return errors.New("draco: cannot quantize empty attribute")
	}

	return nil
}

func decodeFloat32AttributeEntry(dst []float32, attr *Attribute, entry int) error {
	raw, err := attr.rawEntry(entry)
	if err != nil {
		return err
	}

	return decodeRawFloat32(dst, raw, attr.DataType, attr.Normalized)
}

func scanFloat32AttributeExtents(attr *Attribute) ([]float32, []float32, error) {
	minValues := make([]float32, attr.NumComponents)
	if err := decodeFloat32AttributeEntry(minValues, attr, 0); err != nil {
		return nil, nil, err
	}

	maxValues := append([]float32(nil), minValues...)
	values := make([]float32, attr.NumComponents)
	for _, value := range minValues {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, nil, errors.New("draco: quantization source contains non-finite values")
		}
	}

	for entry := 1; entry < attr.EntryCount(); entry++ {
		if err := decodeFloat32AttributeEntry(values, attr, entry); err != nil {
			return nil, nil, err
		}

		for i, value := range values {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return nil, nil, errors.New("draco: quantization source contains non-finite values")
			}

			if value < minValues[i] {
				minValues[i] = value
			}

			if value > maxValues[i] {
				maxValues[i] = value
			}
		}
	}

	return minValues, maxValues, nil
}

func (t *quantizationTransform) compute(attr *Attribute, quantizationBits int) error {
	if err := requireFloat32QuantizationSource(attr); err != nil {
		return err
	}

	if quantizationBits < 1 || quantizationBits > 30 {
		return fmt.Errorf("draco: invalid quantization bits %d", quantizationBits)
	}

	minValues, maxValues, err := scanFloat32AttributeExtents(attr)
	if err != nil {
		return err
	}

	rangeValue := float32(0)
	for i := range minValues {
		dif := maxValues[i] - minValues[i]
		if dif > rangeValue {
			rangeValue = dif
		}
	}

	if rangeValue == 0 {
		rangeValue = 1
	}

	t.minValues = minValues
	t.rangeValue = rangeValue
	t.quantizationBits = uint8(quantizationBits)
	return nil
}

func (t *quantizationTransform) computeExplicit(attr *Attribute, quantizationBits int, minValues []float32, rangeValue float32) error {
	if err := requireFloat32QuantizationSource(attr); err != nil {
		return err
	}

	if quantizationBits < 1 || quantizationBits > 30 {
		return fmt.Errorf("draco: invalid quantization bits %d", quantizationBits)
	}

	if len(minValues) != attr.NumComponents {
		return fmt.Errorf("draco: explicit quantization origin has %d components, want %d", len(minValues), attr.NumComponents)
	}

	if math.IsNaN(float64(rangeValue)) || math.IsInf(float64(rangeValue), 0) || rangeValue <= 0 {
		return fmt.Errorf("draco: invalid explicit quantization range %v", rangeValue)
	}

	for _, value := range minValues {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return errors.New("draco: explicit quantization origin contains non-finite values")
		}
	}

	t.minValues = append([]float32(nil), minValues...)
	t.rangeValue = rangeValue
	t.quantizationBits = uint8(quantizationBits)
	return nil
}

func (t *quantizationTransform) computeGrid(attr *Attribute, spacing float32) error {
	if err := requireFloat32QuantizationSource(attr); err != nil {
		return err
	}

	if math.IsNaN(float64(spacing)) || math.IsInf(float64(spacing), 0) || spacing <= 0 {
		return fmt.Errorf("draco: invalid grid quantization spacing %v", spacing)
	}

	minValues, maxValues, err := scanFloat32AttributeExtents(attr)
	if err != nil {
		return err
	}

	origin := make([]float32, len(minValues))
	maxSteps := 0
	spacing64 := float64(spacing)
	for i := range minValues {
		originValue := math.Floor(float64(minValues[i])/spacing64) * spacing64
		origin[i] = float32(originValue)
		steps := int(math.Ceil((float64(maxValues[i]) - originValue) / spacing64))
		if steps > maxSteps {
			maxSteps = steps
		}
	}

	if maxSteps <= 0 {
		maxSteps = 1
	}

	quantizationBits := bitsRequired(uint32(maxSteps))
	if quantizationBits < 1 || quantizationBits > 30 {
		return fmt.Errorf("draco: invalid grid quantization bits %d", quantizationBits)
	}

	t.minValues = origin
	maxQuantizedValue := (uint32(1) << uint(quantizationBits)) - 1
	t.rangeValue = float32(maxQuantizedValue) * spacing
	t.quantizationBits = uint8(quantizationBits)
	return nil
}

func (t *quantizationTransform) decode(r *core.Reader, numComponents int) error {
	if numComponents <= 0 {
		return fmt.Errorf("draco: invalid quantization component count %d", numComponents)
	}

	t.minValues = make([]float32, numComponents)
	for i := 0; i < numComponents; i++ {
		value, err := r.ReadFloat32()
		if err != nil {
			return err
		}

		t.minValues[i] = value
	}

	rangeValue, err := r.ReadFloat32()
	if err != nil {
		return err
	}

	quantizationBits, err := r.ReadUint8()
	if err != nil {
		return err
	}

	if quantizationBits < 1 || quantizationBits > 30 {
		return fmt.Errorf("draco: invalid quantization bits %d", quantizationBits)
	}

	t.rangeValue = rangeValue
	t.quantizationBits = quantizationBits
	return nil
}

func (t *quantizationTransform) encode(w *core.Writer) error {
	for _, minValue := range t.minValues {
		if err := w.WriteFloat32(minValue); err != nil {
			return err
		}
	}

	if err := w.WriteFloat32(t.rangeValue); err != nil {
		return err
	}

	return w.WriteUint8(t.quantizationBits)
}

func (t *quantizationTransform) quantizeAttribute(ctx context.Context, source *Attribute) (*Attribute, error) {
	if err := requireFloat32QuantizationSource(source); err != nil {
		return nil, err
	}

	if len(t.minValues) != source.NumComponents {
		return nil, errors.New("draco: quantization transform component mismatch")
	}

	maxQuantizedValue := (1 << t.quantizationBits) - 1
	if maxQuantizedValue <= 0 {
		return nil, errors.New("draco: invalid quantization max value")
	}

	inverseDelta := float32(maxQuantizedValue) / t.rangeValue
	target, err := NewAttribute(source.Type, DataTypeInt32, source.NumComponents, source.EntryCount())
	if err != nil {
		return nil, err
	}

	target.Normalized = source.Normalized
	target.UniqueID = source.UniqueID
	if !source.IsIdentityMapping() {
		target.mapping = append([]uint32(nil), source.mapping...)
	}

	values := make([]float32, source.NumComponents)
	qvals := make([]int32, source.NumComponents)
	for entry := 0; entry < source.EntryCount(); entry++ {
		if err := checkContextEvery(ctx, entry); err != nil {
			return nil, err
		}

		if err := decodeFloat32AttributeEntry(values, source, entry); err != nil {
			return nil, err
		}

		for i, value := range values {
			qvals[i] = int32(math.Floor(float64((value-t.minValues[i])*inverseDelta) + 0.5))
		}

		if err := restoreSignedKDTreeAttributeEntry(target, entry, qvals); err != nil {
			return nil, err
		}
	}

	return target, nil
}

func (t *quantizationTransform) dequantizeAttribute(ctx context.Context, portable, target *Attribute) error {
	if portable == nil || target == nil {
		return errors.New("draco: quantization transform requires source and target attributes")
	}

	if portable.DataType != DataTypeInt32 {
		return fmt.Errorf("draco: quantization source type is %s, want INT32", portable.DataType)
	}

	if target.DataType != DataTypeFloat32 {
		return fmt.Errorf("draco: quantization target type is %s, want FLOAT32", target.DataType)
	}

	if portable.NumComponents == target.NumComponents {
		return t.dequantizeInt32ToFloat32Attribute(ctx, portable, target)
	}

	maxQuantizedValue := (1 << t.quantizationBits) - 1
	if maxQuantizedValue <= 0 {
		return errors.New("draco: invalid quantization max value")
	}

	delta := t.rangeValue / float32(maxQuantizedValue)
	qvals := make([]int32, portable.NumComponents)
	out := make([]float32, portable.NumComponents)
	for entry := 0; entry < portable.EntryCount(); entry++ {
		if err := checkContextEvery(ctx, entry); err != nil {
			return err
		}

		raw, err := portable.rawEntry(entry)
		if err != nil {
			return fmt.Errorf("draco: quantization dequantize portable entry=%d numEntries=%d: %w", entry, portable.EntryCount(), err)
		}

		if err := decodeRawInt32(qvals, raw, portable.DataType); err != nil {
			return fmt.Errorf("draco: quantization dequantize portable entry=%d numEntries=%d: %w", entry, portable.EntryCount(), err)
		}

		for i, q := range qvals {
			out[i] = float32(q)*delta + t.minValues[i]
		}

		if err := setAttributeFloat32Value(target, entry, out); err != nil {
			return fmt.Errorf("draco: quantization dequantize target entry=%d targetEntries=%d: %w", entry, target.EntryCount(), err)
		}
	}

	return nil
}

func (t *quantizationTransform) dequantizeInt32ToFloat32Attribute(ctx context.Context, portable, target *Attribute) error {
	maxQuantizedValue := (1 << t.quantizationBits) - 1
	if maxQuantizedValue <= 0 {
		return errors.New("draco: invalid quantization max value")
	}

	if len(t.minValues) < portable.NumComponents {
		return errors.New("draco: quantization transform component mismatch")
	}

	delta := t.rangeValue / float32(maxQuantizedValue)
	components := portable.NumComponents
	portableStride := portable.ByteStride()
	targetStride := target.ByteStride()
	portableData := portable.data
	targetData := target.data
	minValues := t.minValues
	for entry := 0; entry < portable.EntryCount(); entry++ {
		if err := checkContextEvery(ctx, entry); err != nil {
			return err
		}

		src := portableData[entry*portableStride:]
		dst := targetData[entry*targetStride:]
		for component := 0; component < components; component++ {
			q := int32(binary.LittleEndian.Uint32(src[component*4:]))
			value := float32(q)*delta + minValues[component]
			binary.LittleEndian.PutUint32(dst[component*4:], math.Float32bits(value))
		}
	}

	return nil
}

func quantizationBitsForAttribute(attID int, attr *Attribute, options encodeConfig) (int, error) {
	quantizationBits := options.quantizationBitsForAttribute(attID, attr.Type)
	if quantizationBits > 0 {
		return quantizationBits, nil
	}

	spacing, hasSpacing := options.quantizationSpacingForAttribute(attID, attr.Type)
	if !hasSpacing {
		return 0, nil
	}

	transform := &quantizationTransform{}
	if err := transform.computeGrid(attr, spacing); err != nil {
		return 0, err
	}

	return int(transform.quantizationBits), nil
}

func attributeQuantizationTransform(attID int, attr *Attribute, options encodeConfig) (*quantizationTransform, error) {
	transform := &quantizationTransform{}
	quantizationBits, err := quantizationBitsForAttribute(attID, attr, options)
	if err != nil {
		return nil, err
	}

	if quantizationBits <= 0 {
		return nil, fmt.Errorf("draco: quantization for %s requires quantization bits or grid spacing", attr.Type)
	}

	origin, hasOrigin := options.quantizationOriginForAttribute(attID, attr.Type)
	rangeValue, hasRange := options.quantizationRangeForAttribute(attID, attr.Type)
	spacing, hasSpacing := options.quantizationSpacingForAttribute(attID, attr.Type)
	switch {
	case hasOrigin != hasRange:
		return nil, fmt.Errorf("draco: explicit quantization for %s requires both quantization_origin and quantization_range", attr.Type)
	case hasOrigin:
		if err := transform.computeExplicit(
			attr,
			quantizationBits,
			origin,
			rangeValue,
		); err != nil {
			return nil, err
		}
	case hasSpacing:
		if err := transform.computeGrid(
			attr,
			spacing,
		); err != nil {
			return nil, err
		}
	default:
		if err := transform.compute(attr, quantizationBits); err != nil {
			return nil, err
		}
	}

	return transform, nil
}

func bitsRequired(value uint32) int {
	if value == 0 {
		return 0
	}

	return bits.Len32(value)
}
