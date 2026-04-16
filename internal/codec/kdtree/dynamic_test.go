package kdtree

import (
	"cmp"
	"math"
	"slices"
	"testing"

	"github.com/gophics/go-draco/internal/core"
	"github.com/gophics/go-draco/internal/entropy"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeDynamicIntegerPointsKDTreeCompressionLevels(t *testing.T) {
	ctx := t.Context()
	const (
		count     = 96
		dimension = 5
		bitLength = 10
	)
	points := benchmarkKDTreePoints(count, dimension, bitLength)

	for compressionLevel := 0; compressionLevel <= 6; compressionLevel++ {
		t.Run("cl"+string(rune('0'+compressionLevel)), func(t *testing.T) {
			writer := core.NewWriter(0)
			encodeScratch := &EncodeScratch{}
			err := EncodePointsContext(ctx, writer, cloneKDTreePoints(points), dimension, bitLength, compressionLevel, encodeScratch)
			require.NoError(t, err)

			decoded, err := DecodePointsContext(ctx, core.NewReader(writer.Bytes()), dimension, compressionLevel)
			require.NoError(t, err)
			requireKDTreePointSetEqual(t, points, decoded)
		})
	}
}

func TestDecodeDynamicIntegerPointsKDTreeRows(t *testing.T) {
	ctx := t.Context()
	const (
		count            = 40
		dimension        = 4
		bitLength        = 12
		compressionLevel = 6
	)
	points := benchmarkKDTreePoints(count, dimension, bitLength)
	writer := core.NewWriter(0)
	require.NoError(t, EncodePointsContext(ctx, writer, cloneKDTreePoints(points), dimension, bitLength, compressionLevel, &EncodeScratch{}))

	var rows [][]uint32
	scratch := &DecodeScratch{}
	err := DecodePointsToRowsContext(ctx, core.NewReader(writer.Bytes()), dimension, compressionLevel, scratch, func(row []uint32) error {
		rows = append(rows, append([]uint32(nil), row...))
		return nil
	})
	require.NoError(t, err)
	requireKDTreePointSetEqual(t, points, rows)
}

func TestKDTreeCodecRejectsInvalidInputs(t *testing.T) {
	ctx := t.Context()

	require.Error(t, EncodePointsContext(ctx, core.NewWriter(0), nil, 3, 1, -1, nil))
	require.Error(t, DecodePointsToRowsContext(ctx, core.NewReader(nil), 0, 1, nil, func([]uint32) error { return nil }))
	require.Error(t, DecodePointsToRowsContext(ctx, core.NewReader(nil), 3, 7, nil, func([]uint32) error { return nil }))
	require.Error(t, DecodePointsToRowsContext(ctx, core.NewReader(nil), 3, 1, nil, nil))

	writer := core.NewWriter(0)
	require.NoError(t, writer.WriteUint32(33))
	require.NoError(t, writer.WriteUint32(0))
	require.Error(t, DecodePointsToRowsContext(ctx, core.NewReader(writer.Bytes()), 3, 1, nil, func([]uint32) error { return nil }))

	policy := &kdTreeDecodePolicy{}
	tooManyPoints := uint32(maxDecodeAllocationBytes/4 + 1)
	err := decodeDynamicIntegerPointsKDTreeInternal(ctx, policy, 1, 1, tooManyPoints, nil, func([]uint32) error { return nil })
	require.Error(t, err)
}

func TestKDTreeScratchResetDropsOversizedBuffers(t *testing.T) {
	oversizedUint32s := int(maxRetainedScratchBytes/4 + 1)
	encodeScratch := &EncodeScratch{
		baseStackBacking: make([]uint32, 0, oversizedUint32s),
		numbersFolded:    &kdTreeFoldedBitEncoder{entropy.NewFoldedRAnsBit32Encoder()},
	}
	encodeScratch.Reset()
	require.Nil(t, encodeScratch.baseStackBacking)

	decodeScratch := &DecodeScratch{
		baseStackBacking: make([]uint32, 0, oversizedUint32s),
	}
	decodeScratch.policy.numbersFolded = &kdTreeFoldedBitDecoder{entropy.NewFoldedRAnsBit32Decoder()}
	decodeScratch.Reset()
	require.Nil(t, decodeScratch.baseStackBacking)
	require.NotNil(t, decodeScratch.policy.numbersFolded)
}

func BenchmarkKDTreeBitDecode(b *testing.B) {
	values := benchmarkKDTreeValues(4096, 13)
	for _, tc := range []struct {
		name string
		data []byte
		run  func(*testing.B, []byte, []uint32, int)
	}{
		{
			name: "direct",
			data: benchmarkEncodeKDTreeBits(b, 13, values, func() kdTreeBitEncoder {
				return &kdTreeDirectBitEncoder{}
			}),
			run: benchmarkDirectKDTreeBitDecode,
		},
		{
			name: "rans",
			data: benchmarkEncodeKDTreeBits(b, 13, values, func() kdTreeBitEncoder {
				return &kdTreeRAnsBitEncoder{}
			}),
			run: benchmarkRAnsKDTreeBitDecode,
		},
		{
			name: "folded",
			data: benchmarkEncodeKDTreeBits(b, 13, values, func() kdTreeBitEncoder {
				return &kdTreeFoldedBitEncoder{entropy.NewFoldedRAnsBit32Encoder()}
			}),
			run: benchmarkFoldedKDTreeBitDecode,
		},
	} {
		b.Run(tc.name, func(b *testing.B) {
			tc.run(b, tc.data, values, 13)
		})
	}
}

func BenchmarkDecodeDynamicIntegerPointsKDTreeRows(b *testing.B) {
	ctx := b.Context()
	const (
		dimension = 4
		bitLength = 12
		count     = 4096
	)
	points := benchmarkKDTreePoints(count, dimension, bitLength)
	for _, compressionLevel := range []int{1, 3, 5} {
		b.Run("cl"+string(rune('0'+compressionLevel)), func(b *testing.B) {
			writer := core.NewWriter(0)
			scratch := &EncodeScratch{}
			if err := EncodePointsContext(ctx, writer, cloneKDTreePoints(points), dimension, bitLength, compressionLevel, scratch); err != nil {
				b.Fatalf("EncodePointsContext() error = %v", err)
			}

			data := writer.Bytes()
			decodeScratch := &DecodeScratch{}
			var checksum uint32
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				reader := core.NewReader(data)
				if err := DecodePointsToRowsContext(ctx, reader, dimension, compressionLevel, decodeScratch, func(row []uint32) error {
					checksum += row[0]
					return nil
				}); err != nil {
					b.Fatalf("DecodePointsToRowsContext() error = %v", err)
				}
			}

			if checksum == 0 {
				b.Fatal("unexpected zero checksum")
			}
		})
	}
}

func TestKDTreeScratchAllocationRejectsOversizedDimension(t *testing.T) {
	_, _, err := kdTreeScratchSizes(math.MaxInt/32 + 1)
	require.Error(t, err)

	err = guardKDTreeScratchAllocation(1<<26, 1, "decode")
	require.Error(t, err)
}

func benchmarkDirectKDTreeBitDecode(b *testing.B, data []byte, values []uint32, bitCount int) {
	b.Helper()

	var checksum uint32
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := core.NewReader(data)
		var decoder kdTreeDirectBitDecoder
		if err := decoder.StartDecoding(reader); err != nil {
			b.Fatalf("StartDecoding() error = %v", err)
		}

		for range values {
			value, err := decoder.DecodeLeastSignificantBits32(bitCount)
			if err != nil {
				b.Fatalf("DecodeLeastSignificantBits32() error = %v", err)
			}

			checksum += value
		}

		if !decoder.EndDecoding() {
			b.Fatal("EndDecoding() = false")
		}
	}

	if checksum == 0 {
		b.Fatal("unexpected zero checksum")
	}
}

func benchmarkRAnsKDTreeBitDecode(b *testing.B, data []byte, values []uint32, bitCount int) {
	b.Helper()

	var checksum uint32
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := core.NewReader(data)
		var decoder kdTreeRAnsBitDecoder
		if err := decoder.StartDecoding(reader); err != nil {
			b.Fatalf("StartDecoding() error = %v", err)
		}

		for range values {
			value, err := decoder.DecodeLeastSignificantBits32(bitCount)
			if err != nil {
				b.Fatalf("DecodeLeastSignificantBits32() error = %v", err)
			}

			checksum += value
		}

		if !decoder.EndDecoding() {
			b.Fatal("EndDecoding() = false")
		}
	}

	if checksum == 0 {
		b.Fatal("unexpected zero checksum")
	}
}

func benchmarkFoldedKDTreeBitDecode(b *testing.B, data []byte, values []uint32, bitCount int) {
	b.Helper()

	var checksum uint32
	decoder := kdTreeFoldedBitDecoder{entropy.NewFoldedRAnsBit32Decoder()}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := core.NewReader(data)
		if err := decoder.StartDecoding(reader); err != nil {
			b.Fatalf("StartDecoding() error = %v", err)
		}

		for range values {
			value, err := decoder.DecodeLeastSignificantBits32(bitCount)
			if err != nil {
				b.Fatalf("DecodeLeastSignificantBits32() error = %v", err)
			}

			checksum += value
		}

		if !decoder.EndDecoding() {
			b.Fatal("EndDecoding() = false")
		}
	}

	if checksum == 0 {
		b.Fatal("unexpected zero checksum")
	}
}

func benchmarkEncodeKDTreeBits(b *testing.B, bitCount int, values []uint32, newEncoder func() kdTreeBitEncoder) []byte {
	b.Helper()
	writer := core.NewWriter(0)
	encoder := newEncoder()
	encoder.StartEncoding()
	for _, value := range values {
		encoder.EncodeLeastSignificantBits32(bitCount, value)
	}

	if err := encoder.EndEncoding(writer); err != nil {
		b.Fatalf("EndEncoding() error = %v", err)
	}

	return writer.Bytes()
}

func benchmarkKDTreeValues(count int, bitCount int) []uint32 {
	values := make([]uint32, count)
	mask := ^uint32(0)
	if bitCount < 32 {
		mask = (uint32(1) << bitCount) - 1
	}

	value := uint32(0x9e3779b9)
	for i := range values {
		value = value*1664525 + 1013904223
		values[i] = value & mask
	}

	return values
}

func benchmarkKDTreePoints(count, dimension int, bitLength uint32) [][]uint32 {
	points := make([][]uint32, count)
	backing := make([]uint32, count*dimension)
	mask := (uint32(1) << bitLength) - 1
	value := uint32(0x243f6a88)
	for i := range points {
		start := i * dimension
		row := backing[start : start+dimension]
		for j := range row {
			value = value*1103515245 + 12345
			row[j] = value & mask
		}

		points[i] = row
	}

	return points
}

func cloneKDTreePoints(points [][]uint32) [][]uint32 {
	if len(points) == 0 {
		return nil
	}

	out := make([][]uint32, len(points))
	backing := make([]uint32, 0, len(points)*len(points[0]))
	for i, point := range points {
		start := len(backing)
		backing = append(backing, point...)
		out[i] = backing[start : start+len(point)]
	}

	return out
}

func requireKDTreePointSetEqual(t *testing.T, want, got [][]uint32) {
	t.Helper()

	wantCopy := cloneKDTreePoints(want)
	gotCopy := cloneKDTreePoints(got)
	sortKDTreePoints(wantCopy)
	sortKDTreePoints(gotCopy)
	require.Equal(t, wantCopy, gotCopy)
}

func sortKDTreePoints(points [][]uint32) {
	slices.SortFunc(points, compareKDTreePointRows)
}

func compareKDTreePointRows(a, b []uint32) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := cmp.Compare(a[i], b[i]); c != 0 {
			return c
		}
	}

	return cmp.Compare(len(a), len(b))
}
