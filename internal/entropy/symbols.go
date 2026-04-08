package entropy

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/gophics/go-draco/internal/core"
)

const (
	SymbolCodingTagged = 0
	SymbolCodingRaw    = 1
)

const maxRetainedDecodeScratchBytes uint64 = 8 << 20

func ComputeRansPrecisionBits(uniqueSymbolsBitLength int) int {
	bits := (3 * uniqueSymbolsBitLength) / 2
	if bits < 12 {
		return 12
	}

	if bits > 20 {
		return 20
	}

	return bits
}

func DecodeSymbols(r *core.Reader, numValues uint32, numComponents int) ([]uint32, error) {
	return DecodeSymbolsVersionedWithScratch(r, numValues, numComponents, false, nil)
}

type DecodeScratch struct {
	probabilities []uint32
	symbols       []uint32
	int32Buffers  [2][]int32
	decoders      [21]*RansDecoder
}

// Reset clears transient input references while retaining reusable decode tables.
func (s *DecodeScratch) Reset() {
	if s == nil {
		return
	}

	s.probabilities = resetUint32Scratch(s.probabilities)
	s.symbols = resetUint32Scratch(s.symbols)
	for i := range s.int32Buffers {
		s.int32Buffers[i] = resetInt32Scratch(s.int32Buffers[i])
	}

	for _, decoder := range s.decoders {
		if decoder != nil {
			decoder.ClearInput()
		}
	}
}

func resetUint32Scratch(buf []uint32) []uint32 {
	if uint64(cap(buf))*4 > maxRetainedDecodeScratchBytes {
		return nil
	}

	return buf[:0]
}

func resetInt32Scratch(buf []int32) []int32 {
	if uint64(cap(buf))*4 > maxRetainedDecodeScratchBytes {
		return nil
	}

	return buf[:0]
}

func resetByteScratch(buf []byte) []byte {
	if uint64(cap(buf)) > maxRetainedDecodeScratchBytes {
		return nil
	}

	return buf[:0]
}

const maxDecodeScratchBytes uint64 = 512 << 20

func guardProbabilityScratch(n uint32) error {
	const uint32Size = 4
	if uint64(n) > uint64(math.MaxInt) {
		return fmt.Errorf("draco: symbol probability count %d exceeds platform int limit", n)
	}

	if uint64(n)*uint32Size > maxDecodeScratchBytes {
		return fmt.Errorf("draco: symbol probability allocation of %d bytes exceeds %d-byte limit", uint64(n)*uint32Size, maxDecodeScratchBytes)
	}

	return nil
}

func guardSymbolOutputAllocation(n uint32) error {
	if uint64(n) > uint64(math.MaxInt) {
		return fmt.Errorf("draco: symbol output count %d exceeds platform int limit", n)
	}

	if uint64(n)*4 > maxDecodeScratchBytes {
		return fmt.Errorf("draco: symbol output allocation of %d bytes exceeds %d-byte limit", uint64(n)*4, maxDecodeScratchBytes)
	}

	return nil
}

func (s *DecodeScratch) probabilitiesSlice(n uint32) ([]uint32, error) {
	if err := guardProbabilityScratch(n); err != nil {
		return nil, err
	}

	if s == nil {
		return make([]uint32, n), nil
	}

	s.probabilities = slices.Grow(s.probabilities[:0], int(n))
	s.probabilities = s.probabilities[:n]
	return s.probabilities, nil
}

func (s *DecodeScratch) symbolsSlice(n uint32) ([]uint32, error) {
	if err := guardSymbolOutputAllocation(n); err != nil {
		return nil, err
	}

	if s == nil {
		return make([]uint32, n), nil
	}

	s.symbols = slices.Grow(s.symbols[:0], int(n))
	s.symbols = s.symbols[:n]
	return s.symbols, nil
}

// Int32Buffer returns scratch-owned int32 storage for transient decode work.
func (s *DecodeScratch) Int32Buffer(slot, n int) []int32 {
	if s == nil || slot < 0 || slot >= len(s.int32Buffers) {
		return make([]int32, n)
	}

	buf := slices.Grow(s.int32Buffers[slot][:0], n)
	buf = buf[:n]
	s.int32Buffers[slot] = buf
	return buf
}

func (s *DecodeScratch) decoder(precisionBits int) *RansDecoder {
	if s == nil || precisionBits < 0 || precisionBits >= len(s.decoders) {
		return NewRansDecoder(precisionBits)
	}

	decoder := s.decoders[precisionBits]
	if decoder == nil {
		decoder = NewRansDecoder(precisionBits)
		s.decoders[precisionBits] = decoder
	}

	return decoder
}

func DecodeSymbolsWithScratch(r *core.Reader, numValues uint32, numComponents int, scratch *DecodeScratch) ([]uint32, error) {
	return DecodeSymbolsVersionedWithScratch(r, numValues, numComponents, false, scratch)
}

func DecodeSymbolsVersionedWithScratch(r *core.Reader, numValues uint32, numComponents int, legacy bool, scratch *DecodeScratch) ([]uint32, error) {
	return decodeSymbolsVersionedWithScratch(r, numValues, numComponents, legacy, scratch, false)
}

// DecodeSymbolsVersionedTransientWithScratch decodes symbols into scratch-owned
// output. Callers must consume the returned slice before the next scratch use.
func DecodeSymbolsVersionedTransientWithScratch(r *core.Reader, numValues uint32, numComponents int, legacy bool, scratch *DecodeScratch) ([]uint32, error) {
	return decodeSymbolsVersionedWithScratch(r, numValues, numComponents, legacy, scratch, true)
}

func decodeSymbolsVersionedWithScratch(r *core.Reader, numValues uint32, numComponents int, legacy bool, scratch *DecodeScratch, transientOutput bool) ([]uint32, error) {
	if numValues == 0 {
		return nil, nil
	}

	if err := guardSymbolOutputAllocation(numValues); err != nil {
		return nil, err
	}

	scheme, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}

	switch scheme {
	case SymbolCodingTagged:
		return decodeTaggedSymbols(r, numValues, numComponents, legacy, scratch, transientOutput)
	case SymbolCodingRaw:
		return decodeRawSymbols(r, numValues, legacy, scratch, transientOutput)
	default:
		return nil, fmt.Errorf("draco: unsupported symbol coding scheme %d", scheme)
	}
}

func decodeTaggedSymbols(r *core.Reader, numValues uint32, numComponents int, legacy bool, scratch *DecodeScratch, transientOutput bool) ([]uint32, error) {
	tagDecoder, err := createSymbolDecoder(r, 5, legacy, scratch)
	if err != nil {
		return nil, err
	}

	var out []uint32
	if transientOutput {
		out, err = scratch.symbolsSlice(numValues)
		if err != nil {
			return nil, err
		}
	} else {
		out = make([]uint32, numValues)
	}

	bitData := r.RemainingBytes()
	totalBits := len(bitData) * 8
	bitOffset := 0
	valueID := 0
	for i := uint32(0); i < numValues; i += uint32(numComponents) {
		bitLength := int(tagDecoder.ReadSymbol())
		if bitLength < 0 || bitLength > 32 {
			return nil, errors.New("draco: invalid tagged symbol bit length")
		}

		for j := 0; j < numComponents; j++ {
			value, ok := readTaggedBitsLSB(bitData, totalBits, &bitOffset, bitLength)
			if !ok {
				return nil, errors.New("draco: failed to read tagged symbol bits")
			}

			out[valueID] = value
			valueID++
		}
	}

	if err := r.Advance((bitOffset + 7) / 8); err != nil {
		return nil, err
	}

	return out, nil
}

func readTaggedBitsLSB(data []byte, totalBits int, bitOffset *int, n int) (uint32, bool) {
	if n == 0 {
		return 0, true
	}

	offset := *bitOffset
	if offset > totalBits || n > totalBits-offset {
		return 0, false
	}

	byteOffset := offset >> 3
	shift := uint(offset & 0x7)
	if int(shift)+n <= 8 {
		*bitOffset = offset + n
		mask := uint32(1<<uint(n)) - 1
		return (uint32(data[byteOffset]) >> shift) & mask, true
	}

	neededBits := n + int(shift)
	var bits uint64
	for loadedBits := 0; loadedBits < neededBits; loadedBits += 8 {
		bits |= uint64(data[byteOffset]) << loadedBits
		byteOffset++
	}

	*bitOffset = offset + n
	mask := (uint64(1) << uint(n)) - 1
	return uint32((bits >> shift) & mask), true
}

func decodeRawSymbols(r *core.Reader, numValues uint32, legacy bool, scratch *DecodeScratch, transientOutput bool) ([]uint32, error) {
	maxBitLength, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}

	decoder, err := createSymbolDecoder(r, int(maxBitLength), legacy, scratch)
	if err != nil {
		return nil, err
	}

	var out []uint32
	if transientOutput {
		out, err = scratch.symbolsSlice(numValues)
		if err != nil {
			return nil, err
		}
	} else {
		out = make([]uint32, numValues)
	}

	for i := uint32(0); i < numValues; i++ {
		out[i] = decoder.ReadSymbol()
	}

	return out, nil
}

func createSymbolDecoder(r *core.Reader, uniqueSymbolsBitLength int, legacy bool, scratch *DecodeScratch) (*RansDecoder, error) {
	var numSymbols uint32
	var err error
	if legacy {
		numSymbols, err = r.ReadUint32()
		if err != nil {
			return nil, err
		}
	} else {
		numSymbols, err = core.DecodeVarUint32(r)
		if err != nil {
			return nil, err
		}
	}

	probabilities, err := scratch.probabilitiesSlice(numSymbols)
	if err != nil {
		return nil, err
	}

	for i := uint32(0); i < numSymbols; i++ {
		probData, err := r.ReadUint8()
		if err != nil {
			return nil, err
		}

		token := probData & 3
		if token == 3 {
			offset := uint32(probData >> 2)
			if i+offset >= numSymbols {
				return nil, errors.New("draco: invalid zero-run in probability table")
			}

			for j := uint32(0); j <= offset; j++ {
				probabilities[i+j] = 0
			}

			i += offset
			continue
		}

		prob := uint32(probData >> 2)
		for b := 0; b < int(token); b++ {
			extra, err := r.ReadUint8()
			if err != nil {
				return nil, err
			}

			prob |= uint32(extra) << (8*(b+1) - 2)
		}

		probabilities[i] = prob
	}

	var size uint64
	if legacy {
		var readErr error
		size, readErr = r.ReadUint64()
		if readErr != nil {
			return nil, readErr
		}
	} else {
		size, err = core.DecodeVarUint64(r)
		if err != nil {
			return nil, err
		}
	}

	if size > uint64(r.Remaining()) {
		return nil, errors.New("draco: invalid rANS payload size")
	}

	data, err := r.ReadBytesView(int(size))
	if err != nil {
		return nil, err
	}

	decoder := scratch.decoder(ComputeRansPrecisionBits(uniqueSymbolsBitLength))
	if err := decoder.BuildLookup(probabilities); err != nil {
		return nil, err
	}

	if err := decoder.Init(data); err != nil {
		return nil, err
	}

	return decoder, nil
}
