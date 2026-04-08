package entropy

import (
	"errors"
	"fmt"
	"math"
	"math/bits"

	"github.com/gophics/go-draco/internal/core"
)

const (
	SymbolCodingAuto = -1

	maxTagSymbolBitLength         = 32
	maxRawEncodingBitLength       = 18
	defaultSymbolCompressionLevel = 7
)

type EncodeOptions struct {
	Method           int
	CompressionLevel int
}

func EncodeSymbols(w *core.Writer, symbols []uint32, numComponents int, options *EncodeOptions) error {
	if len(symbols) == 0 {
		return nil
	}

	if numComponents <= 0 {
		numComponents = 1
	}

	if len(symbols)%numComponents != 0 {
		return fmt.Errorf("draco: symbol count %d is not divisible by component count %d", len(symbols), numComponents)
	}

	bitLengths, maxValue := computeBitLengths(symbols, numComponents)
	taggedSchemeTotalBits := approximateTaggedSchemeBits(bitLengths, numComponents)

	rawSchemeTotalBits := int64(^uint64(0) >> 1)
	numUniqueSymbols := 0
	maxValueBitLength := bits.Len32(maxValue)
	if maxValueBitLength == 0 {
		maxValueBitLength = 1
	}

	if maxValueBitLength <= maxRawEncodingBitLength {
		rawSchemeTotalBits, numUniqueSymbols = approximateRawSchemeBits(symbols, maxValue)
	}

	method := SymbolCodingAuto
	compressionLevel := defaultSymbolCompressionLevel
	if options != nil {
		method = options.Method
		compressionLevel = options.CompressionLevel
	}

	if method == SymbolCodingAuto {
		if maxValueBitLength > maxRawEncodingBitLength || taggedSchemeTotalBits < rawSchemeTotalBits {
			method = SymbolCodingTagged
		} else {
			method = SymbolCodingRaw
		}
	}

	if err := w.WriteUint8(uint8(method)); err != nil {
		return err
	}

	switch method {
	case SymbolCodingTagged:
		return encodeTaggedSymbols(w, symbols, numComponents, bitLengths)
	case SymbolCodingRaw:
		return encodeRawSymbols(w, symbols, maxValue, numUniqueSymbols, compressionLevel)
	default:
		return fmt.Errorf("draco: unsupported symbol coding scheme %d", method)
	}
}

func computeBitLengths(symbols []uint32, numComponents int) ([]uint32, uint32) {
	bitLengths := make([]uint32, 0, (len(symbols)+numComponents-1)/numComponents)
	maxValue := uint32(0)
	for i := 0; i < len(symbols); i += numComponents {
		maxComponentValue := symbols[i]
		for j := 1; j < numComponents; j++ {
			if symbols[i+j] > maxComponentValue {
				maxComponentValue = symbols[i+j]
			}
		}

		bitLength := uint32(bits.Len32(maxComponentValue))
		if bitLength == 0 {
			bitLength = 1
		}

		if maxComponentValue > maxValue {
			maxValue = maxComponentValue
		}

		bitLengths = append(bitLengths, bitLength)
	}

	return bitLengths, maxValue
}

func approximateTaggedSchemeBits(bitLengths []uint32, numComponents int) int64 {
	totalBitLength := uint64(0)
	maxValue := uint32(0)
	for _, bitLength := range bitLengths {
		totalBitLength += uint64(bitLength)
		if bitLength > maxValue {
			maxValue = bitLength
		}
	}

	tagBits, numUniqueSymbols := computeShannonEntropy(bitLengths, maxValue)
	tagTableBits := approximateRAnsFrequencyTableBits(int32(numUniqueSymbols), numUniqueSymbols)
	return tagBits + tagTableBits + int64(totalBitLength)*int64(numComponents)
}

func approximateRawSchemeBits(symbols []uint32, maxValue uint32) (int64, int) {
	dataBits, numUniqueSymbols := computeShannonEntropy(symbols, maxValue)
	tableBits := approximateRAnsFrequencyTableBits(int32(maxValue), numUniqueSymbols)
	return dataBits + tableBits, numUniqueSymbols
}

func computeShannonEntropy(symbols []uint32, maxValue uint32) (int64, int) {
	if len(symbols) == 0 {
		return 0, 0
	}

	frequencies := make([]int, int(maxValue)+1)
	for _, symbol := range symbols {
		frequencies[symbol]++
	}

	totalBits := 0.0
	numUniqueSymbols := 0
	numSymbolsD := float64(len(symbols))
	for _, freq := range frequencies {
		if freq == 0 {
			continue
		}

		numUniqueSymbols++
		totalBits += float64(freq) * math.Log2(float64(freq)/numSymbolsD)
	}

	return int64(-totalBits), numUniqueSymbols
}

func approximateRAnsFrequencyTableBits(maxValue int32, numUniqueSymbols int) int64 {
	tableZeroFrequencyBits := 8 * int64(numUniqueSymbols+(int(maxValue)-numUniqueSymbols)/64)
	return 8*int64(numUniqueSymbols) + tableZeroFrequencyBits
}

func encodeTaggedSymbols(w *core.Writer, symbols []uint32, numComponents int, bitLengths []uint32) error {
	frequencies := make([]uint64, maxTagSymbolBitLength+1)
	for _, bitLength := range bitLengths {
		frequencies[bitLength]++
	}

	precisionBits := ComputeRansPrecisionBits(5)
	probabilityTable, err := buildRansProbabilityTable(frequencies, precisionBits)
	if err != nil {
		return err
	}

	if err := encodeRansProbabilityTable(w, probabilityTable); err != nil {
		return err
	}

	tagEncoder := NewRansEncoder(precisionBits)
	tagEncoder.Reset(len(bitLengths))
	for i := len(bitLengths) - 1; i >= 0; i-- {
		tagEncoder.WriteSymbol(probabilityTable[bitLengths[i]])
	}

	tagPayload := tagEncoder.Bytes()
	if err := core.EncodeVarUint64(w, uint64(len(tagPayload))); err != nil {
		return err
	}

	if err := w.WriteBytes(tagPayload); err != nil {
		return err
	}

	valueBits := 0
	for _, bitLength := range bitLengths {
		valueBits += int(bitLength) * numComponents
	}

	valueWriter := core.NewBitWriter(valueBits)
	for i := 0; i < len(symbols); i += numComponents {
		bitLength := int(bitLengths[i/numComponents])
		for j := 0; j < numComponents; j++ {
			if !valueWriter.WriteBitsLSB(symbols[i+j], bitLength) {
				return errors.New("draco: failed to encode tagged symbol bits")
			}
		}
	}

	return w.WriteBytes(valueWriter.BytesView())
}

func encodeRawSymbols(w *core.Writer, symbols []uint32, maxValue uint32, numUniqueSymbols int, compressionLevel int) error {
	if bits.Len32(maxValue) > maxRawEncodingBitLength {
		return fmt.Errorf("draco: raw symbol encoding exceeds %d bits", maxRawEncodingBitLength)
	}

	uniqueSymbolsBitLength := 1
	if numUniqueSymbols > 0 {
		uniqueSymbolsBitLength = bits.Len32(uint32(numUniqueSymbols))
		if uniqueSymbolsBitLength == 0 {
			uniqueSymbolsBitLength = 1
		}
	}

	switch {
	case compressionLevel < 4:
		uniqueSymbolsBitLength -= 2
	case compressionLevel < 6:
		uniqueSymbolsBitLength--
	case compressionLevel > 9:
		uniqueSymbolsBitLength += 2
	case compressionLevel > 7:
		uniqueSymbolsBitLength++
	}

	if uniqueSymbolsBitLength < 1 {
		uniqueSymbolsBitLength = 1
	}

	if uniqueSymbolsBitLength > maxRawEncodingBitLength {
		uniqueSymbolsBitLength = maxRawEncodingBitLength
	}

	if err := w.WriteUint8(uint8(uniqueSymbolsBitLength)); err != nil {
		return err
	}

	frequencies := make([]uint64, int(maxValue)+1)
	for _, symbol := range symbols {
		frequencies[symbol]++
	}

	probabilityTable, err := buildRansProbabilityTable(frequencies, ComputeRansPrecisionBits(uniqueSymbolsBitLength))
	if err != nil {
		return err
	}

	if err := encodeRansProbabilityTable(w, probabilityTable); err != nil {
		return err
	}

	encoder := NewRansEncoder(ComputeRansPrecisionBits(uniqueSymbolsBitLength))
	encoder.Reset(len(symbols))
	for i := len(symbols) - 1; i >= 0; i-- {
		encoder.WriteSymbol(probabilityTable[symbols[i]])
	}

	payload := encoder.Bytes()
	if err := core.EncodeVarUint64(w, uint64(len(payload))); err != nil {
		return err
	}

	return w.WriteBytes(payload)
}
