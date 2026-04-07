package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

type Reader struct {
	data []byte
	pos  int
}

func NewReader(data []byte) *Reader {
	return &Reader{data: data}
}

func (r *Reader) Pos() int { return r.pos }

func (r *Reader) Remaining() int { return len(r.data) - r.pos }

func (r *Reader) Advance(n int) error {
	if n < 0 || r.pos+n > len(r.data) {
		return fmt.Errorf("draco: invalid advance size %d", n)
	}

	r.pos += n
	return nil
}

func (r *Reader) RemainingBytes() []byte {
	return r.data[r.pos:]
}

func (r *Reader) ReadBytesView(n int) ([]byte, error) {
	if n < 0 {
		return nil, fmt.Errorf("draco: invalid read size %d", n)
	}

	if r.pos+n > len(r.data) {
		return nil, errors.New("draco: unexpected EOF")
	}

	out := r.data[r.pos : r.pos+n]
	r.pos += n
	return out, nil
}

func (r *Reader) ReadBytes(n int) ([]byte, error) {
	out, err := r.ReadBytesView(n)
	if err != nil {
		return nil, err
	}

	out = append([]byte(nil), out...)
	return out, nil
}

func (r *Reader) ReadInt8() (int8, error) {
	v, err := r.ReadUint8()
	return int8(v), err
}

func (r *Reader) ReadUint8() (uint8, error) {
	if r.pos+1 > len(r.data) {
		return 0, errors.New("draco: unexpected EOF")
	}

	v := r.data[r.pos]
	r.pos++
	return v, nil
}

func (r *Reader) ReadUint16() (uint16, error) {
	if r.pos+2 > len(r.data) {
		return 0, errors.New("draco: unexpected EOF")
	}

	v := binary.LittleEndian.Uint16(r.data[r.pos:])
	r.pos += 2
	return v, nil
}

func (r *Reader) ReadUint32() (uint32, error) {
	if r.pos+4 > len(r.data) {
		return 0, errors.New("draco: unexpected EOF")
	}

	v := binary.LittleEndian.Uint32(r.data[r.pos:])
	r.pos += 4
	return v, nil
}

func (r *Reader) ReadUint64() (uint64, error) {
	if r.pos+8 > len(r.data) {
		return 0, errors.New("draco: unexpected EOF")
	}

	v := binary.LittleEndian.Uint64(r.data[r.pos:])
	r.pos += 8
	return v, nil
}

func (r *Reader) ReadInt32() (int32, error) {
	if r.pos+4 > len(r.data) {
		return 0, errors.New("draco: unexpected EOF")
	}

	v := int32(binary.LittleEndian.Uint32(r.data[r.pos:]))
	r.pos += 4
	return v, nil
}

func (r *Reader) ReadFloat32() (float32, error) {
	v, err := r.ReadUint32()
	if err != nil {
		return 0, err
	}

	return math.Float32frombits(v), nil
}

type BitReader struct {
	data      []byte
	bitOffset int
}

func NewBitReader(data []byte) *BitReader {
	return &BitReader{data: data}
}

func NewBitReaderValue(data []byte) BitReader {
	return BitReader{data: data}
}

func (r *BitReader) ReadBitLSB() (uint32, bool) {
	totalBits := len(r.data) * 8
	if r.bitOffset >= totalBits {
		return 0, false
	}

	offset := r.bitOffset
	r.bitOffset++
	return uint32((r.data[offset>>3] >> uint(offset&0x7)) & 1), true
}

func (r *BitReader) ReadBitsLSB(n int) (uint32, bool) {
	if n < 0 || n > 32 {
		return 0, false
	}

	if n == 0 {
		return 0, true
	}

	totalBits := len(r.data) * 8
	if r.bitOffset > totalBits || n > totalBits-r.bitOffset {
		return 0, false
	}

	byteOffset := r.bitOffset >> 3
	shift := uint(r.bitOffset & 0x7)
	if int(shift)+n <= 8 {
		r.bitOffset += n
		mask := uint32(1<<uint(n)) - 1
		return (uint32(r.data[byteOffset]) >> shift) & mask, true
	}

	neededBits := n + int(shift)
	var bits uint64
	for loadedBits := 0; loadedBits < neededBits; loadedBits += 8 {
		bits |= uint64(r.data[byteOffset]) << loadedBits
		byteOffset++
	}

	r.bitOffset += n
	mask := (uint64(1) << uint(n)) - 1
	return uint32((bits >> shift) & mask), true
}

func (r *BitReader) BytesRead() int {
	return (r.bitOffset + 7) / 8
}
