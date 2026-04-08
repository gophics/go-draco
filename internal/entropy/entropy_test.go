package entropy

import (
	"bytes"
	"testing"

	"github.com/gophics/go-draco/internal/core"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeSymbols(t *testing.T) {
	testCases := []struct {
		name          string
		symbols       []uint32
		numComponents int
		options       *EncodeOptions
	}{
		{
			name:          "auto-large-values",
			symbols:       []uint32{12345678, 1223333, 111, 5},
			numComponents: 1,
		},
		{
			name:          "forced-raw",
			symbols:       repeatedSymbols([]uint32{12, 1025, 7, 9, 0}, []int{1500, 3100, 1, 5, 643}),
			numComponents: 1,
			options:       &EncodeOptions{Method: SymbolCodingRaw},
		},
		{
			name:          "forced-tagged-grouped",
			symbols:       []uint32{1, 2, 3, 255, 0, 17, 65535, 12, 9, 1 << 20, 5, 7},
			numComponents: 3,
			options:       &EncodeOptions{Method: SymbolCodingTagged},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			writer := core.NewWriter(0)
			require.NoError(t, EncodeSymbols(writer, tc.symbols, tc.numComponents, tc.options))

			reader := core.NewReader(writer.Bytes())
			got, err := DecodeSymbols(reader, uint32(len(tc.symbols)), tc.numComponents)
			require.NoError(t, err)
			require.Equal(t, tc.symbols, got)
		})
	}
}

func TestEncodeDecodeEmptySymbols(t *testing.T) {
	writer := core.NewWriter(0)
	require.NoError(t, EncodeSymbols(writer, nil, 1, nil))

	reader := core.NewReader(writer.Bytes())
	got, err := DecodeSymbols(reader, 0, 1)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestEncodeDecodeRawSymbolsCompressionLevels(t *testing.T) {
	symbols := repeatedSymbols([]uint32{1, 5, 9, 17, 33, 65}, []int{180, 120, 90, 60, 45, 30})
	var encoded [][]byte

	for _, level := range []int{1, 10} {
		writer := core.NewWriter(0)
		require.NoError(t, EncodeSymbols(writer, symbols, 1, &EncodeOptions{
			Method:           SymbolCodingRaw,
			CompressionLevel: level,
		}))

		data := append([]byte(nil), writer.Bytes()...)
		encoded = append(encoded, data)

		reader := core.NewReader(data)
		got, err := DecodeSymbols(reader, uint32(len(symbols)), 1)
		require.NoError(t, err)
		require.Equal(t, symbols, got)
	}

	require.False(t, bytes.Equal(encoded[0], encoded[1]))
}

func TestDecodeSymbolsWithScratchReusesDecoderState(t *testing.T) {
	writer := core.NewWriter(0)
	symbols := repeatedSymbols([]uint32{3, 7, 11}, []int{16, 8, 4})
	require.NoError(t, EncodeSymbols(writer, symbols, 1, &EncodeOptions{Method: SymbolCodingRaw}))
	data := writer.Bytes()

	scratch := &DecodeScratch{}
	for i := 0; i < 2; i++ {
		reader := core.NewReader(data)
		got, err := DecodeSymbolsWithScratch(reader, uint32(len(symbols)), 1, scratch)
		require.NoError(t, err)
		require.Equal(t, symbols, got)
	}
}

func TestDecodeSymbolsWithScratchKeepsReturnedOutputStable(t *testing.T) {
	firstSymbols := repeatedSymbols([]uint32{3, 7, 11}, []int{16, 8, 4})
	secondSymbols := repeatedSymbols([]uint32{1, 2, 5}, []int{12, 10, 6})

	firstWriter := core.NewWriter(0)
	require.NoError(t, EncodeSymbols(firstWriter, firstSymbols, 1, &EncodeOptions{Method: SymbolCodingRaw}))
	secondWriter := core.NewWriter(0)
	require.NoError(t, EncodeSymbols(secondWriter, secondSymbols, 1, &EncodeOptions{Method: SymbolCodingRaw}))

	scratch := &DecodeScratch{}
	first, err := DecodeSymbolsWithScratch(core.NewReader(firstWriter.Bytes()), uint32(len(firstSymbols)), 1, scratch)
	require.NoError(t, err)
	second, err := DecodeSymbolsWithScratch(core.NewReader(secondWriter.Bytes()), uint32(len(secondSymbols)), 1, scratch)
	require.NoError(t, err)

	require.Equal(t, firstSymbols, first)
	require.Equal(t, secondSymbols, second)
}

func TestDecodeSymbolsRejectsInvalidScheme(t *testing.T) {
	_, err := DecodeSymbols(core.NewReader([]byte{99}), 1, 1)
	require.Error(t, err)
}

func TestDecodeSymbolsRejectsTruncatedPayloads(t *testing.T) {
	testCases := []struct {
		name          string
		symbols       []uint32
		numComponents int
		options       *EncodeOptions
	}{
		{
			name:          "tagged",
			symbols:       []uint32{1, 3, 7, 15, 31, 63},
			numComponents: 2,
			options:       &EncodeOptions{Method: SymbolCodingTagged},
		},
		{
			name:          "raw",
			symbols:       repeatedSymbols([]uint32{5, 7, 9}, []int{20, 10, 5}),
			numComponents: 1,
			options:       &EncodeOptions{Method: SymbolCodingRaw},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			writer := core.NewWriter(0)
			require.NoError(t, EncodeSymbols(writer, tc.symbols, tc.numComponents, tc.options))
			data := writer.Bytes()

			for truncate := 1; truncate <= 2 && truncate < len(data); truncate++ {
				reader := core.NewReader(data[:len(data)-truncate])
				_, err := DecodeSymbols(reader, uint32(len(tc.symbols)), tc.numComponents)
				require.Error(t, err)
			}
		})
	}
}

func TestRansDecoderRejectsInvalidLookupAndState(t *testing.T) {
	decoder := NewRansDecoder(12)
	require.Error(t, decoder.BuildLookup([]uint32{1, 2, 3}))
	require.Error(t, decoder.Init([]byte{0xff}))
}

func TestDecodeSymbolsRejectsOversizedProbabilityTable(t *testing.T) {
	writer := core.NewWriter(0)
	require.NoError(t, writer.WriteUint8(SymbolCodingRaw))
	require.NoError(t, writer.WriteUint8(1))
	require.NoError(t, core.EncodeVarUint32(writer, uint32(maxDecodeScratchBytes/4)+1))

	_, err := DecodeSymbols(core.NewReader(writer.Bytes()), 1, 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "probability allocation")
}

func TestDecodeSymbolsRejectsOversizedOutputAllocation(t *testing.T) {
	_, err := DecodeSymbolsVersionedWithScratch(core.NewReader(nil), uint32(maxDecodeScratchBytes/4)+1, 1, false, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "symbol output allocation")
}

func repeatedSymbols(values []uint32, counts []int) []uint32 {
	out := make([]uint32, 0)
	for i, value := range values {
		for j := 0; j < counts[i]; j++ {
			out = append(out, value)
		}
	}

	return out
}

func TestDirectBitRoundTrip(t *testing.T) {
	writer := core.NewWriter(0)
	encoder := &DirectBitEncoder{}
	encoder.StartEncoding()
	for _, bit := range []bool{true, false, true, true, false} {
		encoder.EncodeBit(bit)
	}

	encoder.EncodeLeastSignificantBits32(5, 0b10110)
	require.NoError(t, encoder.EndEncoding(writer))

	reader := core.NewReader(writer.Bytes())
	decoder := &DirectBitDecoder{}
	require.NoError(t, decoder.StartDecoding(reader))
	for i, want := range []bool{true, false, true, true, false} {
		require.Equal(t, want, decoder.DecodeNextBit(), "DecodeNextBit(%d)", i)
	}

	var value uint32
	require.True(t, decoder.DecodeLeastSignificantBits32(5, &value))
	require.Equal(t, uint32(0b10110), value)
	require.True(t, decoder.EndDecoding())
}

func TestFoldedBitRoundTrip(t *testing.T) {
	writer := core.NewWriter(0)
	encoder := NewFoldedBit32Encoder(func() bitEncoder { return &DirectBitEncoder{} })
	encoder.StartEncoding()
	encoder.EncodeLeastSignificantBits32(4, 0b1101)
	encoder.EncodeLeastSignificantBits32(4, 0b0011)
	encoder.EncodeBit(true)
	encoder.EncodeBit(false)
	require.NoError(t, encoder.EndEncoding(writer))

	reader := core.NewReader(writer.Bytes())
	decoder := NewFoldedBit32Decoder(func() bitDecoder { return &DirectBitDecoder{} })
	require.NoError(t, decoder.StartDecoding(reader))

	var first uint32
	require.True(t, decoder.DecodeLeastSignificantBits32(4, &first))
	require.Equal(t, uint32(0b1101), first)

	var second uint32
	require.True(t, decoder.DecodeLeastSignificantBits32(4, &second))
	require.Equal(t, uint32(0b0011), second)

	require.True(t, decoder.DecodeNextBit())
	require.False(t, decoder.DecodeNextBit())
	require.True(t, decoder.EndDecoding())
}

func TestDirectBitDecoderRejectsInvalidPayload(t *testing.T) {
	testCases := []struct {
		name string
		data []byte
	}{
		{name: "zero-size", data: []byte{0, 0, 0, 0}},
		{name: "size-not-word-aligned", data: []byte{1, 0, 0, 0, 0}},
		{name: "size-exceeds-input", data: []byte{8, 0, 0, 0, 0, 0, 0, 0}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			decoder := &DirectBitDecoder{}
			require.Error(t, decoder.StartDecoding(core.NewReader(tc.data)))
		})
	}
}

func TestDirectBitDecoderRejectsShortBitReads(t *testing.T) {
	writer := core.NewWriter(0)
	encoder := &DirectBitEncoder{}
	encoder.StartEncoding()
	encoder.EncodeLeastSignificantBits32(8, 0xaa)
	require.NoError(t, encoder.EndEncoding(writer))

	decoder := &DirectBitDecoder{}
	require.NoError(t, decoder.StartDecoding(core.NewReader(writer.Bytes())))

	var value uint32
	require.True(t, decoder.DecodeLeastSignificantBits32(8, &value))
	require.False(t, decoder.DecodeLeastSignificantBits32(32, &value))
}

func TestDirectBitEncoderRejectsInvalidBitCount(t *testing.T) {
	writer := core.NewWriter(0)
	encoder := &DirectBitEncoder{}
	encoder.StartEncoding()
	encoder.EncodeLeastSignificantBits32(0, 0)
	require.Error(t, encoder.EndEncoding(writer))
}

func TestDirectBitDecoderRejectsInvalidBitCount(t *testing.T) {
	writer := core.NewWriter(0)
	encoder := &DirectBitEncoder{}
	encoder.StartEncoding()
	encoder.EncodeLeastSignificantBits32(4, 0xf)
	require.NoError(t, encoder.EndEncoding(writer))

	decoder := &DirectBitDecoder{}
	require.NoError(t, decoder.StartDecoding(core.NewReader(writer.Bytes())))

	var value uint32
	require.False(t, decoder.DecodeLeastSignificantBits32(0, &value))
	require.Error(t, decoder.Err())
}

func TestFoldedBitEncoderRejectsInvalidBitCount(t *testing.T) {
	writer := core.NewWriter(0)
	encoder := NewFoldedBit32Encoder(func() bitEncoder { return &DirectBitEncoder{} })
	encoder.StartEncoding()
	encoder.EncodeLeastSignificantBits32(33, 0)
	require.Error(t, encoder.EndEncoding(writer))
}

func TestRansBitRoundTrip(t *testing.T) {
	input := []bool{true, false, true, true, false, false, true, false, true, false, true, true, true, false}

	writer := core.NewWriter(0)
	encoder := &RansBitEncoder{}
	encoder.StartEncoding()
	for _, bit := range input {
		encoder.EncodeBit(bit)
	}

	require.NoError(t, encoder.EndEncoding(writer))

	decoder := &RansBitDecoder{}
	require.NoError(t, decoder.StartDecoding(core.NewReader(writer.Bytes())))
	for i, want := range input {
		require.Equal(t, want, decoder.DecodeNextBit(), "DecodeNextBit(%d)", i)
	}

	require.True(t, decoder.EndDecoding())
}

func TestRansBitDecoderRejectsInvalidPayload(t *testing.T) {
	decoder := &RansBitDecoder{}
	require.Error(t, decoder.StartDecoding(core.NewReader(nil)))
}

func TestRansBitEncoderRejectsInvalidBitCount(t *testing.T) {
	writer := core.NewWriter(0)
	encoder := &RansBitEncoder{}
	encoder.StartEncoding()
	encoder.EncodeLeastSignificantBits32(0, 0)
	require.Error(t, encoder.EndEncoding(writer))
}

func TestRansBitDecoderRejectsInvalidBitCount(t *testing.T) {
	writer := core.NewWriter(0)
	encoder := &RansBitEncoder{}
	encoder.StartEncoding()
	encoder.EncodeLeastSignificantBits32(4, 0xf)
	require.NoError(t, encoder.EndEncoding(writer))

	decoder := &RansBitDecoder{}
	require.NoError(t, decoder.StartDecoding(core.NewReader(writer.Bytes())))

	var value uint32
	require.False(t, decoder.DecodeLeastSignificantBits32(0, &value))
	require.Error(t, decoder.Err())
}
