package core

import (
	"bytes"
	"encoding/binary"
	"math"
)

type Writer struct {
	buf bytes.Buffer
}

func NewWriter(capacity int) *Writer {
	w := &Writer{}
	if capacity > 0 {
		w.buf.Grow(capacity)
	}

	return w
}

func (w *Writer) Bytes() []byte {
	return append([]byte(nil), w.buf.Bytes()...)
}

// BytesView returns the buffered bytes without copying.
// Callers must not write to the writer after taking the view.
func (w *Writer) BytesView() []byte {
	return w.buf.Bytes()
}

func (w *Writer) WriteBytes(data []byte) error {
	_, err := w.buf.Write(data)
	return err
}

func (w *Writer) WriteUint8(v uint8) error {
	return w.buf.WriteByte(v)
}

func (w *Writer) WriteInt8(v int8) error {
	return w.buf.WriteByte(byte(v))
}

func (w *Writer) WriteUint16(v uint16) error {
	var raw [2]byte
	binary.LittleEndian.PutUint16(raw[:], v)
	_, err := w.buf.Write(raw[:])
	return err
}

func (w *Writer) WriteUint32(v uint32) error {
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], v)
	_, err := w.buf.Write(raw[:])
	return err
}

func (w *Writer) WriteInt32(v int32) error {
	return w.WriteUint32(uint32(v))
}

func (w *Writer) WriteFloat32(v float32) error {
	return w.WriteUint32(math.Float32bits(v))
}

type BitWriter struct {
	data      []byte
	bitOffset int
}

func NewBitWriter(expectedBits int) *BitWriter {
	capacity := 0
	if expectedBits > 0 {
		capacity = (expectedBits + 7) / 8
	}

	return &BitWriter{
		data: make([]byte, 0, capacity),
	}
}

func (w *BitWriter) WriteBitsLSB(value uint32, n int) bool {
	if n < 0 || n > 32 {
		return false
	}

	for bit := 0; bit < n; bit++ {
		if (w.bitOffset >> 3) >= len(w.data) {
			w.data = append(w.data, 0)
		}

		if (value>>bit)&1 != 0 {
			w.data[w.bitOffset>>3] |= 1 << (w.bitOffset & 0x7)
		}

		w.bitOffset++
	}

	return true
}

func (w *BitWriter) Bytes() []byte {
	return append([]byte(nil), w.data...)
}

// BytesView returns the bit-packed bytes without copying.
// Callers must not write more bits after taking the view.
func (w *BitWriter) BytesView() []byte {
	return w.data
}

func (w *BitWriter) BytesWritten() int {
	return len(w.data)
}
