package core

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBitWriterReaderRoundTrip(t *testing.T) {
	writer := NewBitWriter(16)
	require.True(t, writer.WriteBitsLSB(0b1011, 4))
	require.True(t, writer.WriteBitsLSB(0b110010, 6))
	require.Equal(t, 2, writer.BytesWritten())
	require.Equal(t, []byte{0x2b, 0x03}, writer.Bytes())

	reader := NewBitReader(writer.Bytes())
	first, ok := reader.ReadBitsLSB(4)
	require.True(t, ok)
	require.Equal(t, uint32(0b1011), first)
	second, ok := reader.ReadBitsLSB(6)
	require.True(t, ok)
	require.Equal(t, uint32(0b110010), second)
	require.Equal(t, 2, reader.BytesRead())
}

func TestBitWriterRejectsInvalidWidth(t *testing.T) {
	writer := NewBitWriter(0)
	require.False(t, writer.WriteBitsLSB(1, 33))
}

func TestBitReaderRejectsUnexpectedEOF(t *testing.T) {
	reader := NewBitReader([]byte{0x01})
	_, ok := reader.ReadBitsLSB(9)
	require.False(t, ok)
	require.Equal(t, 0, reader.BytesRead())
}

func TestReaderAdvanceAndEOF(t *testing.T) {
	reader := NewReader([]byte{1, 2, 3})
	require.NoError(t, reader.Advance(2))
	require.Equal(t, 2, reader.Pos())
	require.Error(t, reader.Advance(-1))
	_, err := reader.ReadBytes(2)
	require.Error(t, err)
}

func TestReaderReadBytesCopiesData(t *testing.T) {
	source := []byte{1, 2, 3, 4}
	reader := NewReader(source)

	value, err := reader.ReadBytes(2)
	require.NoError(t, err)
	require.Equal(t, 2, reader.Pos())

	value[0] = 9
	require.Equal(t, []byte{1, 2, 3, 4}, source)
}

func TestReaderReadBytesViewSharesBacking(t *testing.T) {
	source := []byte{1, 2, 3, 4}
	reader := NewReader(source)

	value, err := reader.ReadBytesView(2)
	require.NoError(t, err)
	require.Equal(t, 2, reader.Pos())

	value[0] = 9
	require.Equal(t, []byte{9, 2, 3, 4}, source)
}

func TestDecodeVarUintRejectsInvalidEncoding(t *testing.T) {
	testCases := []struct {
		name   string
		decode func(*Reader) (any, error)
		data   []byte
	}{
		{name: "uint32-truncated", decode: decodeVarUint32Any, data: []byte{0x80}},
		{name: "uint32-overflow", decode: decodeVarUint32Any, data: []byte{0x80, 0x80, 0x80, 0x80, 0x80}},
		{name: "uint64-truncated", decode: decodeVarUint64Any, data: []byte{0x80}},
		{name: "uint64-overflow", decode: decodeVarUint64Any, data: []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.decode(NewReader(tc.data))
			require.Error(t, err)
		})
	}
}

func TestBitWriterReaderBoundaryWidths(t *testing.T) {
	writer := NewBitWriter(32)
	require.True(t, writer.WriteBitsLSB(0xffffffff, 32))

	reader := NewBitReader(writer.Bytes())
	value, ok := reader.ReadBitsLSB(32)
	require.True(t, ok)
	require.Equal(t, uint32(0xffffffff), value)

	zero, ok := reader.ReadBitsLSB(0)
	require.True(t, ok)
	require.Equal(t, uint32(0), zero)
}

func TestWriterReaderPrimitiveRoundTrip(t *testing.T) {
	writer := NewWriter(0)
	require.NoError(t, writer.WriteInt8(-7))
	require.NoError(t, writer.WriteUint16(0x1234))
	require.NoError(t, writer.WriteUint32(0x89abcdef))
	require.NoError(t, writer.WriteInt32(-55))
	require.NoError(t, writer.WriteFloat32(1.25))

	data := writer.Bytes()
	data[0] = 0

	reader := NewReader(writer.Bytes())
	gotInt8, err := reader.ReadInt8()
	require.NoError(t, err)
	require.Equal(t, int8(-7), gotInt8)
	gotUint16, err := reader.ReadUint16()
	require.NoError(t, err)
	require.Equal(t, uint16(0x1234), gotUint16)
	gotUint32, err := reader.ReadUint32()
	require.NoError(t, err)
	require.Equal(t, uint32(0x89abcdef), gotUint32)
	gotInt32, err := reader.ReadInt32()
	require.NoError(t, err)
	require.Equal(t, int32(-55), gotInt32)
	gotFloat32, err := reader.ReadFloat32()
	require.NoError(t, err)
	require.InDelta(t, 1.25, gotFloat32, 1e-6)
}

func decodeVarUint32Any(reader *Reader) (any, error) {
	value, err := DecodeVarUint32(reader)
	return value, err
}

func decodeVarUint64Any(reader *Reader) (any, error) {
	value, err := DecodeVarUint64(reader)
	return value, err
}

func TestVarUint32RoundTrip(t *testing.T) {
	testCases := []uint32{0, 1, 2, 127, 128, 255, 300, 16384, 1<<21 - 1, 1 << 28}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("value=%d", tc), func(t *testing.T) {
			writer := NewWriter(0)
			require.NoError(t, EncodeVarUint32(writer, tc))
			got, err := DecodeVarUint32(NewReader(writer.Bytes()))
			require.NoError(t, err)
			require.Equal(t, tc, got)
		})
	}
}

func TestVarUint64RoundTrip(t *testing.T) {
	testCases := []uint64{0, 1, 127, 128, 255, 300, 16384, 1<<21 - 1, 1 << 40, 1<<63 - 1}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("value=%d", tc), func(t *testing.T) {
			writer := NewWriter(0)
			require.NoError(t, EncodeVarUint64(writer, tc))
			got, err := DecodeVarUint64(NewReader(writer.Bytes()))
			require.NoError(t, err)
			require.Equal(t, tc, got)
		})
	}
}

func TestVarInt32RoundTrip(t *testing.T) {
	testCases := []int32{0, 1, -1, 63, -63, 127, -128, 16384, -16384, 1<<28 - 1, -(1 << 28)}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("value=%d", tc), func(t *testing.T) {
			writer := NewWriter(0)
			require.NoError(t, EncodeVarInt32(writer, tc))
			got, err := DecodeVarInt32(NewReader(writer.Bytes()))
			require.NoError(t, err)
			require.Equal(t, tc, got)
		})
	}
}
