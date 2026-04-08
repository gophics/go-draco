package entropy

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/gophics/go-draco/internal/core"
)

type DirectBitEncoder struct {
	bits         []uint32
	localBits    uint32
	numLocalBits uint32
	err          error
}

func (e *DirectBitEncoder) StartEncoding() {
	e.Clear()
}

func (e *DirectBitEncoder) EncodeBit(bit bool) {
	if e.err != nil {
		return
	}

	if bit {
		e.localBits |= 1 << (31 - e.numLocalBits)
	}

	e.numLocalBits++
	if e.numLocalBits == 32 {
		e.bits = append(e.bits, e.localBits)
		e.localBits = 0
		e.numLocalBits = 0
	}
}

func (e *DirectBitEncoder) EncodeLeastSignificantBits32(nbits int, value uint32) {
	if e.err != nil {
		return
	}

	if nbits <= 0 || nbits > 32 {
		e.err = fmt.Errorf("draco: invalid number of bits for direct bit encoding: %d", nbits)
		return
	}

	remaining := 32 - int(e.numLocalBits)
	value <<= 32 - nbits
	if nbits <= remaining {
		value >>= e.numLocalBits
		e.localBits |= value
		e.numLocalBits += uint32(nbits)
		if e.numLocalBits == 32 {
			e.bits = append(e.bits, e.localBits)
			e.localBits = 0
			e.numLocalBits = 0
		}

		return
	}

	value >>= 32 - nbits
	e.numLocalBits = uint32(nbits - remaining)
	left := value >> e.numLocalBits
	e.localBits |= left
	e.bits = append(e.bits, e.localBits)
	e.localBits = value << (32 - e.numLocalBits)
}

func (e *DirectBitEncoder) EndEncoding(w *core.Writer) error {
	if e.err != nil {
		err := e.err
		e.Clear()
		return err
	}

	e.bits = append(e.bits, e.localBits)
	sizeInBytes := uint32(len(e.bits) * 4)
	if err := w.WriteUint32(sizeInBytes); err != nil {
		return err
	}

	for _, word := range e.bits {
		if err := w.WriteUint32(word); err != nil {
			return err
		}
	}

	e.Clear()
	return nil
}

func (e *DirectBitEncoder) Clear() {
	e.bits = resetUint32Scratch(e.bits)
	e.localBits = 0
	e.numLocalBits = 0
	e.err = nil
}

type DirectBitDecoder struct {
	data        []byte
	pos         int
	numUsedBits uint32
	err         error
}

func (d *DirectBitDecoder) StartDecoding(r *core.Reader) error {
	d.Clear()
	sizeInBytes, err := r.ReadUint32()
	if err != nil {
		return err
	}

	if sizeInBytes == 0 || sizeInBytes&3 != 0 {
		return errors.New("draco: invalid direct bit payload size")
	}

	if int(sizeInBytes) > r.Remaining() {
		return errors.New("draco: direct bit payload exceeds input")
	}

	d.data, err = r.ReadBytesView(int(sizeInBytes))
	if err != nil {
		return err
	}

	d.pos = 0
	d.numUsedBits = 0
	return nil
}

func (d *DirectBitDecoder) DecodeNextBit() bool {
	if d.err != nil {
		return false
	}

	selector := uint32(1) << (31 - d.numUsedBits)
	if d.pos >= len(d.data) {
		return false
	}

	word := binary.LittleEndian.Uint32(d.data[d.pos:])
	bit := word&selector != 0
	d.numUsedBits++
	if d.numUsedBits == 32 {
		d.pos += 4
		d.numUsedBits = 0
	}

	return bit
}

func (d *DirectBitDecoder) DecodeLeastSignificantBits32(nbits int, value *uint32) bool {
	if d.err != nil {
		return false
	}

	if nbits <= 0 || nbits > 32 {
		d.err = fmt.Errorf("draco: invalid number of bits for direct bit decoding: %d", nbits)
		return false
	}

	remaining := 32 - int(d.numUsedBits)
	if nbits <= remaining {
		if d.pos >= len(d.data) {
			return false
		}

		word := binary.LittleEndian.Uint32(d.data[d.pos:])
		*value = (word << d.numUsedBits) >> (32 - nbits)
		d.numUsedBits += uint32(nbits)
		if d.numUsedBits == 32 {
			d.pos += 4
			d.numUsedBits = 0
		}

		return true
	}

	if d.pos+4 >= len(d.data) {
		return false
	}

	current := binary.LittleEndian.Uint32(d.data[d.pos:])
	next := binary.LittleEndian.Uint32(d.data[d.pos+4:])
	valueLeft := current << d.numUsedBits
	d.numUsedBits = uint32(nbits - remaining)
	d.pos += 4
	valueRight := next >> (32 - d.numUsedBits)
	*value = (valueLeft >> (32 - d.numUsedBits - uint32(remaining))) | valueRight
	return true
}

func (d *DirectBitDecoder) EndDecoding() bool {
	return d.err == nil
}

func (d *DirectBitDecoder) Err() error {
	return d.err
}

func (d *DirectBitDecoder) Clear() {
	d.data = nil
	d.pos = 0
	d.numUsedBits = 0
	d.err = nil
}
