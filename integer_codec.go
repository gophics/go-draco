package draco

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"

	"github.com/gophics/go-draco/internal/bitstream"
	"github.com/gophics/go-draco/internal/core"
	"github.com/gophics/go-draco/internal/entropy"
)

func isSequentialIntegerDataType(dt DataType) bool {
	switch dt {
	case DataTypeInt8, DataTypeUint8, DataTypeInt16, DataTypeUint16, DataTypeInt32, DataTypeUint32:
		return true
	default:
		return false
	}
}

func convertSignedIntToSymbol(val int32) uint32 {
	if val >= 0 {
		return uint32(val) << 1
	}

	return (uint32(-(val + 1)) << 1) | 1
}

func convertSymbolToSignedInt(val uint32) int32 {
	isPositive := (val & 1) == 0
	val >>= 1
	if isPositive {
		return int32(val)
	}

	return -int32(val) - 1
}

func encodeRawSymbols(w *core.Writer, symbols []uint32) error {
	maskedValue := uint32(0)
	for _, symbol := range symbols {
		maskedValue |= symbol
	}

	numBytes := 1
	if maskedValue != 0 {
		numBytes = (bits.Len32(maskedValue) + 7) / 8
	}

	if err := w.WriteUint8(uint8(numBytes)); err != nil {
		return err
	}

	var raw [4]byte
	for _, symbol := range symbols {
		for i := 0; i < numBytes; i++ {
			raw[i] = byte(symbol >> (8 * i))
		}

		if err := w.WriteBytes(raw[:numBytes]); err != nil {
			return err
		}
	}

	return nil
}

func decodeSequentialIntegerAttribute(ctx context.Context, r *core.Reader, originalAttr, portableAttr *Attribute, numEntries int, mesh *Mesh, positionPortable *Attribute, legacy bool, decodeLegacyTransformData func() error, predictionTables *meshPredictionTableCache, scratch *entropy.DecodeScratch) error {
	predictionMethod, err := r.ReadInt8()
	if err != nil {
		return err
	}

	predictionTransform := bitstream.PredictionTransformNone
	if predictionMethod != bitstream.PredictionNone {
		predictionTransform, err = r.ReadInt8()
		if err != nil {
			return err
		}
	}

	if legacy && decodeLegacyTransformData != nil {
		if err := decodeLegacyTransformData(); err != nil {
			return err
		}
	}

	var normalTransform normalPredictionTransform
	correctionsPositive := false
	if predictionMethod != bitstream.PredictionNone {
		switch predictionTransform {
		case bitstream.PredictionTransformWrap:
		case bitstream.PredictionTransformNormalOctahedron:
			normalTransform = &normalOctahedronPredictionTransform{}
			correctionsPositive = true
		case bitstream.PredictionTransformNormalOctahedronCanonicalized:
			normalTransform = &normalOctahedronCanonicalizedPredictionTransform{}
			correctionsPositive = true
		default:
			return fmt.Errorf("%w: integer prediction transform %d", ErrUnsupportedFeature, predictionTransform)
		}
	}

	compressed, err := r.ReadUint8()
	if err != nil {
		return err
	}

	numComponents := portableAttr.NumComponents
	numValues, err := guardIntProductAllocation(numEntries, numComponents, 4, "sequential integer symbols")
	if err != nil {
		return err
	}

	var symbols []uint32
	if compressed > 0 {
		symbols, err = entropy.DecodeSymbolsVersionedTransientWithScratch(r, uint32(numValues), numComponents, legacy, scratch)
		if err != nil {
			return err
		}
	} else {
		numBytes, err := r.ReadUint8()
		if err != nil {
			return err
		}

		if numBytes == 0 || numBytes > 4 {
			return fmt.Errorf("%w: sequential integer raw symbol width %d", ErrInvalidGeometry, numBytes)
		}

		if err := guardSliceAllocation(numValues, 4, "sequential integer symbols"); err != nil {
			return err
		}

		symbols = make([]uint32, numValues)
		for i := range symbols {
			if err := checkContextEvery(ctx, i); err != nil {
				return err
			}

			raw, err := r.ReadBytesView(int(numBytes))
			if err != nil {
				return err
			}

			var value uint32
			for b := 0; b < len(raw); b++ {
				value |= uint32(raw[b]) << (8 * b)
			}

			symbols[i] = value
		}
	}

	if err := guardSliceAllocation(len(symbols), 4, "sequential integer corrections"); err != nil {
		return err
	}

	corrections := scratch.Int32Buffer(0, len(symbols))
	for i, symbol := range symbols {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		if correctionsPositive {
			corrections[i] = int32(symbol)
		} else {
			corrections[i] = convertSymbolToSignedInt(symbol)
		}
	}

	if err := guardSliceAllocation(len(corrections), 4, "sequential integer values"); err != nil {
		return err
	}

	values := scratch.Int32Buffer(1, len(corrections))
	switch predictionMethod {
	case bitstream.PredictionNone:
		copy(values, corrections)
	case bitstream.PredictionDifference:
		switch predictionTransform {
		case bitstream.PredictionTransformWrap:
			var transform wrapTransform
			transform.Init(numComponents)
			if err := transform.Decode(r); err != nil {
				return err
			}

			var zeroStorage [16]int32
			zero := zeroStorage[:]
			if numComponents > len(zeroStorage) {
				zero = make([]int32, numComponents)
			} else {
				zero = zero[:numComponents]
			}

			transform.ComputeOriginalValue(zero, corrections[:numComponents], values[:numComponents])
			for i := numComponents; i < len(corrections); i += numComponents {
				transform.ComputeOriginalValue(values[i-numComponents:i], corrections[i:i+numComponents], values[i:i+numComponents])
			}
		case bitstream.PredictionTransformNormalOctahedron, bitstream.PredictionTransformNormalOctahedronCanonicalized:
			if portableAttr.NumComponents != 2 {
				return fmt.Errorf("%w: octahedron prediction requires 2-component portable attribute", ErrUnsupportedFeature)
			}

			if err := normalTransform.Decode(r); err != nil {
				return err
			}

			var zeroStorage [16]int32
			zero := zeroStorage[:]
			if numComponents > len(zeroStorage) {
				zero = make([]int32, numComponents)
			} else {
				zero = zero[:numComponents]
			}

			normalTransform.ComputeOriginalValue(zero, corrections[:numComponents], values[:numComponents])
			for i := numComponents; i < len(corrections); i += numComponents {
				normalTransform.ComputeOriginalValue(values[i-numComponents:i], corrections[i:i+numComponents], values[i:i+numComponents])
			}
		default:
			return fmt.Errorf("%w: integer prediction transform %d", ErrUnsupportedFeature, predictionTransform)
		}
	case bitstream.PredictionParallelogram:
		if predictionTransform != bitstream.PredictionTransformWrap {
			return fmt.Errorf("%w: parallelogram prediction transform %d", ErrUnsupportedFeature, predictionTransform)
		}

		predCtx, err := newMeshPredictionContext(ctx, mesh, originalAttr, numEntries, predictionTables)
		if err != nil {
			return err
		}

		var transform wrapTransform
		transform.Init(numComponents)
		if err := transform.Decode(r); err != nil {
			return err
		}

		values, err = restoreMeshParallelogramValuesInto(ctx, predCtx, corrections, numComponents, &transform, values)
		if err != nil {
			return err
		}
	case bitstream.PredictionMultiParallelogram:
		if predictionTransform != bitstream.PredictionTransformWrap {
			return fmt.Errorf("%w: multi-parallelogram prediction transform %d", ErrUnsupportedFeature, predictionTransform)
		}

		predCtx, err := newMeshPredictionContext(ctx, mesh, originalAttr, numEntries, predictionTables)
		if err != nil {
			return err
		}

		var transform wrapTransform
		transform.Init(numComponents)
		if err := transform.Decode(r); err != nil {
			return err
		}

		values, err = restoreMeshMultiParallelogramValuesInto(ctx, predCtx, corrections, numComponents, &transform, values)
		if err != nil {
			return err
		}
	case bitstream.PredictionConstrainedMultiParallelogram:
		if predictionTransform != bitstream.PredictionTransformWrap {
			return fmt.Errorf("%w: constrained multi-parallelogram prediction transform %d", ErrUnsupportedFeature, predictionTransform)
		}

		predCtx, err := newMeshPredictionContext(ctx, mesh, originalAttr, numEntries, predictionTables)
		if err != nil {
			return err
		}

		predictionData, err := decodeConstrainedMultiPredictionData(r, legacy)
		if err != nil {
			return err
		}

		var transform wrapTransform
		transform.Init(numComponents)
		if err := transform.Decode(r); err != nil {
			return err
		}

		values, err = restoreMeshConstrainedMultiParallelogramValues(ctx, predCtx, corrections, numComponents, &transform, predictionData)
		if err != nil {
			return err
		}
	case bitstream.PredictionTexCoordsPortable:
		if predictionTransform != bitstream.PredictionTransformWrap {
			return fmt.Errorf("%w: texcoord portable prediction transform %d", ErrUnsupportedFeature, predictionTransform)
		}

		predCtx, err := newMeshTexCoordPredictionContext(ctx, mesh, originalAttr, positionPortable, numEntries, predictionTables)
		if err != nil {
			return err
		}

		predictionData, err := decodeTexCoordPredictionData(r, legacy)
		if err != nil {
			return err
		}

		var transform wrapTransform
		transform.Init(numComponents)
		if err := transform.Decode(r); err != nil {
			return err
		}

		values, err = restoreMeshTexCoordPortableValues(ctx, predCtx, corrections, numComponents, &transform, predictionData)
		if err != nil {
			return err
		}
	case bitstream.PredictionGeometricNormal:
		if predictionTransform != bitstream.PredictionTransformNormalOctahedron &&
			predictionTransform != bitstream.PredictionTransformNormalOctahedronCanonicalized {
			return fmt.Errorf("%w: geometric normal prediction transform %d", ErrUnsupportedFeature, predictionTransform)
		}

		predCtx, err := newMeshGeometricNormalPredictionContext(ctx, mesh, originalAttr, positionPortable, numEntries, decodeNormalTransformOctahedron(normalTransform), predictionTables)
		if err != nil {
			return err
		}

		if err := normalTransform.Decode(r); err != nil {
			return err
		}

		predictionData, err := decodeGeometricNormalPredictionData(r, numEntries, legacy)
		if err != nil {
			return err
		}

		values, err = restoreMeshGeometricNormalValues(ctx, predCtx, corrections, numComponents, normalTransform, predictionData)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: integer prediction method %d", ErrUnsupportedFeature, predictionMethod)
	}

	return storeDecodedInt32Values(ctx, portableAttr, values, numEntries, numComponents)
}

func storeDecodedInt32Values(ctx context.Context, attr *Attribute, values []int32, numEntries, numComponents int) error {
	stride := attr.ByteStride()
	data := attr.data
	switch attr.DataType {
	case DataTypeInt8:
		for entry := 0; entry < numEntries; entry++ {
			if err := checkContextEvery(ctx, entry); err != nil {
				return err
			}

			src := values[entry*numComponents:]
			dst := data[entry*stride:]
			for i := 0; i < numComponents; i++ {
				dst[i] = byte(int8(src[i]))
			}
		}
	case DataTypeUint8:
		for entry := 0; entry < numEntries; entry++ {
			if err := checkContextEvery(ctx, entry); err != nil {
				return err
			}

			src := values[entry*numComponents:]
			dst := data[entry*stride:]
			for i := 0; i < numComponents; i++ {
				dst[i] = byte(src[i])
			}
		}
	case DataTypeInt16:
		for entry := 0; entry < numEntries; entry++ {
			if err := checkContextEvery(ctx, entry); err != nil {
				return err
			}

			src := values[entry*numComponents:]
			dst := data[entry*stride:]
			for i := 0; i < numComponents; i++ {
				binary.LittleEndian.PutUint16(dst[i*2:], uint16(int16(src[i])))
			}
		}
	case DataTypeUint16:
		for entry := 0; entry < numEntries; entry++ {
			if err := checkContextEvery(ctx, entry); err != nil {
				return err
			}

			src := values[entry*numComponents:]
			dst := data[entry*stride:]
			for i := 0; i < numComponents; i++ {
				binary.LittleEndian.PutUint16(dst[i*2:], uint16(src[i]))
			}
		}
	case DataTypeInt32:
		for entry := 0; entry < numEntries; entry++ {
			if err := checkContextEvery(ctx, entry); err != nil {
				return err
			}

			src := values[entry*numComponents:]
			dst := data[entry*stride:]
			for i := 0; i < numComponents; i++ {
				binary.LittleEndian.PutUint32(dst[i*4:], uint32(src[i]))
			}
		}
	case DataTypeUint32:
		for entry := 0; entry < numEntries; entry++ {
			if err := checkContextEvery(ctx, entry); err != nil {
				return err
			}

			src := values[entry*numComponents:]
			dst := data[entry*stride:]
			for i := 0; i < numComponents; i++ {
				binary.LittleEndian.PutUint32(dst[i*4:], uint32(src[i]))
			}
		}
	case DataTypeBool:
		for entry := 0; entry < numEntries; entry++ {
			if err := checkContextEvery(ctx, entry); err != nil {
				return err
			}

			src := values[entry*numComponents:]
			dst := data[entry*stride:]
			for i := 0; i < numComponents; i++ {
				if src[i] != 0 {
					dst[i] = 1
				} else {
					dst[i] = 0
				}
			}
		}
	default:
		return fmt.Errorf("%w: cannot store int32 values in %s attribute", ErrUnsupportedFeature, attr.DataType)
	}

	return nil
}

func readAttributeInt32Values(ctx context.Context, attr *Attribute, values []int32, numEntries, numComponents int) error {
	if attr == nil {
		return fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	if numComponents != attr.NumComponents {
		return fmt.Errorf("%w: expected %d components, got %d", ErrInvalidGeometry, attr.NumComponents, numComponents)
	}

	if len(values) < numEntries*numComponents {
		return fmt.Errorf("%w: int32 value buffer too small", ErrInvalidGeometry)
	}

	for entry := 0; entry < numEntries; entry++ {
		if err := checkContextEvery(ctx, entry); err != nil {
			return err
		}

		raw, err := attr.rawEntry(entry)
		if err != nil {
			return err
		}

		start := entry * numComponents
		if err := decodeRawInt32(values[start:start+numComponents], raw, attr.DataType); err != nil {
			return err
		}
	}

	return nil
}

func encodeSequentialNormalAttribute(ctx context.Context, w *core.Writer, attrID int, originalAttr, portableAttr *Attribute, transform *octahedronTransform, numEntries int, mesh *Mesh, positionPortable *Attribute, options encodeConfig, useBuiltInCompression bool, symbolOptions *entropy.EncodeOptions, scratch *sequentialEncodeScratch) error {
	if transform == nil {
		return errors.New("draco: octahedron transform is nil")
	}

	if portableAttr.NumComponents != 2 {
		return errors.New("draco: octahedron portable attribute must have 2 components")
	}

	numValues, err := guardIntProductAllocation(numEntries, portableAttr.NumComponents, 4, "sequential normal values")
	if err != nil {
		return err
	}

	values := scratch.int32Buffer(0, numValues)
	if err := readAttributeInt32Values(ctx, portableAttr, values, numEntries, portableAttr.NumComponents); err != nil {
		return err
	}

	prediction := normalOctahedronCanonicalizedPredictionTransform{
		Octahedron: *transform,
	}
	predictionMethod := bitstream.PredictionDifference
	predictionTransform := bitstream.PredictionTransformNormalOctahedronCanonicalized
	corrections := scratch.int32Buffer(1, len(values))
	var geometricData *geometricNormalPredictionData
	switch selectedMethod := options.predictionMethodForAttribute(attrID, originalAttr.Type); selectedMethod {
	case PredictionMethodUndefined, PredictionMethodDifference:
		var zeroStorage [16]int32
		zero := zeroStorage[:portableAttr.NumComponents]
		if len(values) > 0 {
			prediction.ComputeCorrection(values[:portableAttr.NumComponents], zero, corrections[:portableAttr.NumComponents])
			for i := portableAttr.NumComponents; i < len(values); i += portableAttr.NumComponents {
				prediction.ComputeCorrection(values[i:i+portableAttr.NumComponents], values[i-portableAttr.NumComponents:i], corrections[i:i+portableAttr.NumComponents])
			}
		}
	case PredictionMethodNone:
		predictionMethod = bitstream.PredictionNone
		predictionTransform = bitstream.PredictionTransformNone
		copy(corrections, values)
	case PredictionMethodGeometricNormal:
		predCtx, err := newMeshGeometricNormalPredictionContext(ctx, mesh, originalAttr, positionPortable, numEntries, transform, nil)
		if err != nil {
			return err
		}

		correctionValues, predictionData, err := computeMeshGeometricNormalCorrections(ctx, predCtx, values, portableAttr.NumComponents, &prediction)
		if err != nil {
			return err
		}

		corrections = correctionValues
		geometricData = predictionData
		predictionMethod = bitstream.PredictionGeometricNormal
	default:
		return fmt.Errorf("%w: normal prediction method %d", ErrUnsupportedFeature, selectedMethod)
	}

	if err := w.WriteInt8(predictionMethod); err != nil {
		return err
	}

	if predictionMethod != bitstream.PredictionNone {
		if err := w.WriteInt8(predictionTransform); err != nil {
			return err
		}
	}

	symbols := scratch.uint32Buffer(0, len(corrections))
	for i, value := range corrections {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		if value < 0 {
			return fmt.Errorf("draco: canonicalized normal correction %d is negative", value)
		}

		symbols[i] = uint32(value)
	}

	if useBuiltInCompression {
		if err := w.WriteUint8(1); err != nil {
			return err
		}

		if err := entropy.EncodeSymbols(w, symbols, portableAttr.NumComponents, symbolOptions); err != nil {
			return err
		}
	} else {
		if err := w.WriteUint8(0); err != nil {
			return err
		}

		if err := encodeRawSymbols(w, symbols); err != nil {
			return err
		}
	}

	switch predictionMethod {
	case bitstream.PredictionDifference:
		return prediction.Encode(w)
	case bitstream.PredictionGeometricNormal:
		if err := prediction.Encode(w); err != nil {
			return err
		}

		return encodeGeometricNormalPredictionData(ctx, w, geometricData)
	default:
		return nil
	}
}

func encodeSequentialIntegerAttribute(ctx context.Context, w *core.Writer, attrID int, originalAttr, portableAttr *Attribute, numEntries int, mesh *Mesh, positionPortable *Attribute, options encodeConfig, useBuiltInCompression bool, symbolOptions *entropy.EncodeOptions, scratch *sequentialEncodeScratch) error {
	numValues, err := guardIntProductAllocation(numEntries, portableAttr.NumComponents, 4, "sequential integer values")
	if err != nil {
		return err
	}

	values := scratch.int32Buffer(0, numValues)
	if err := readAttributeInt32Values(ctx, portableAttr, values, numEntries, portableAttr.NumComponents); err != nil {
		return err
	}

	predictionMethod := bitstream.PredictionNone
	predictionTransform := bitstream.PredictionTransformNone
	corrections := values
	var transform wrapTransform
	var constrainedMultiData *constrainedMultiPredictionData
	var texCoordData *texCoordPredictionData
	if err := transform.InitFromValues(values, portableAttr.NumComponents); err == nil && len(values) > 0 {
		selectedMethod := options.predictionMethodForAttribute(attrID, originalAttr.Type)
		switch selectedMethod {
		case PredictionMethodUndefined, PredictionMethodDifference:
			predictionMethod = bitstream.PredictionDifference
			predictionTransform = bitstream.PredictionTransformWrap
			corrections = scratch.int32Buffer(1, len(values))
			var zeroStorage [16]int32
			zero := zeroStorage[:]
			if portableAttr.NumComponents > len(zeroStorage) {
				zero = make([]int32, portableAttr.NumComponents)
			} else {
				zero = zero[:portableAttr.NumComponents]
			}

			transform.ComputeCorrection(values[:portableAttr.NumComponents], zero, corrections[:portableAttr.NumComponents])
			for i := portableAttr.NumComponents; i < len(values); i += portableAttr.NumComponents {
				transform.ComputeCorrection(values[i:i+portableAttr.NumComponents], values[i-portableAttr.NumComponents:i], corrections[i:i+portableAttr.NumComponents])
			}
		case PredictionMethodNone:
		case PredictionMethodParallelogram:
			predCtx, err := newMeshPredictionContext(ctx, mesh, originalAttr, numEntries, nil)
			if err != nil {
				return err
			}

			predictionMethod = bitstream.PredictionParallelogram
			predictionTransform = bitstream.PredictionTransformWrap
			correctionValues, err := computeMeshParallelogramCorrections(ctx, predCtx, values, portableAttr.NumComponents, &transform)
			if err != nil {
				return err
			}

			corrections = correctionValues
		case PredictionMethodMultiParallelogram:
			predCtx, err := newMeshPredictionContext(ctx, mesh, originalAttr, numEntries, nil)
			if err != nil {
				return err
			}

			predictionMethod = bitstream.PredictionMultiParallelogram
			predictionTransform = bitstream.PredictionTransformWrap
			correctionValues, err := computeMeshMultiParallelogramCorrections(ctx, predCtx, values, portableAttr.NumComponents, &transform)
			if err != nil {
				return err
			}

			corrections = correctionValues
		case PredictionMethodConstrainedMultiParallelogram:
			predCtx, err := newMeshPredictionContext(ctx, mesh, originalAttr, numEntries, nil)
			if err != nil {
				return err
			}

			predictionMethod = bitstream.PredictionConstrainedMultiParallelogram
			predictionTransform = bitstream.PredictionTransformWrap
			correctionValues, predictionData, err := computeMeshConstrainedMultiParallelogramCorrections(ctx, predCtx, values, portableAttr.NumComponents, &transform)
			if err != nil {
				return err
			}

			corrections = correctionValues
			constrainedMultiData = predictionData
		case PredictionMethodTexCoordsPortable:
			predCtx, err := newMeshTexCoordPredictionContext(ctx, mesh, originalAttr, positionPortable, numEntries, nil)
			if err != nil {
				return err
			}

			predictionMethod = bitstream.PredictionTexCoordsPortable
			predictionTransform = bitstream.PredictionTransformWrap
			correctionValues, predictionData, err := computeMeshTexCoordPortableCorrections(ctx, predCtx, values, portableAttr.NumComponents, &transform)
			if err != nil {
				return err
			}

			corrections = correctionValues
			texCoordData = predictionData
		default:
			return fmt.Errorf("%w: integer prediction method %d", ErrUnsupportedFeature, selectedMethod)
		}
	}

	if err := w.WriteInt8(predictionMethod); err != nil {
		return err
	}

	if predictionMethod != bitstream.PredictionNone {
		if err := w.WriteInt8(predictionTransform); err != nil {
			return err
		}
	}

	symbols := scratch.uint32Buffer(0, len(corrections))
	for i, value := range corrections {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		symbols[i] = convertSignedIntToSymbol(value)
	}

	if useBuiltInCompression {
		if err := w.WriteUint8(1); err != nil {
			return err
		}

		if err := entropy.EncodeSymbols(w, symbols, portableAttr.NumComponents, symbolOptions); err != nil {
			return err
		}
	} else {
		if err := w.WriteUint8(0); err != nil {
			return err
		}

		if err := encodeRawSymbols(w, symbols); err != nil {
			return err
		}
	}

	switch predictionMethod {
	case bitstream.PredictionDifference, bitstream.PredictionParallelogram, bitstream.PredictionMultiParallelogram:
		if err := transform.Encode(w); err != nil {
			return err
		}
	case bitstream.PredictionConstrainedMultiParallelogram:
		if err := encodeConstrainedMultiPredictionData(w, constrainedMultiData); err != nil {
			return err
		}

		if err := transform.Encode(w); err != nil {
			return err
		}
	case bitstream.PredictionTexCoordsPortable:
		if err := encodeTexCoordPredictionData(ctx, w, texCoordData); err != nil {
			return err
		}

		if err := transform.Encode(w); err != nil {
			return err
		}
	}

	return nil
}

func decodeNormalTransformOctahedron(transform normalPredictionTransform) *octahedronTransform {
	if transform == nil {
		return nil
	}

	return transform.BaseOctahedron()
}
