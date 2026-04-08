package entropy

import (
	"errors"
	"fmt"
	"math/bits"
	"slices"

	"github.com/gophics/go-draco/internal/core"
)

const (
	ransBitPrecision uint32 = 256
	ransBitLBase     uint32 = 4096
	ransBitIOBase    uint32 = 256
)

type RansBitEncoder struct {
	bitCounts    [2]uint64
	bits         []uint32
	buffer       []byte
	localBits    uint32
	numLocalBits uint32
	err          error
}

func (e *RansBitEncoder) StartEncoding() {
	e.Clear()
}

func (e *RansBitEncoder) EncodeBit(bit bool) {
	if e.err != nil {
		return
	}

	if bit {
		e.bitCounts[1]++
		e.localBits |= 1 << e.numLocalBits
	} else {
		e.bitCounts[0]++
	}

	e.numLocalBits++
	if e.numLocalBits == 32 {
		e.bits = append(e.bits, e.localBits)
		e.localBits = 0
		e.numLocalBits = 0
	}
}

func (e *RansBitEncoder) EncodeLeastSignificantBits32(nbits int, value uint32) {
	if e.err != nil {
		return
	}

	if nbits <= 0 || nbits > 32 {
		e.err = fmt.Errorf("draco: invalid number of bits for rANS bit encoding: %d", nbits)
		return
	}

	reversed := bits.Reverse32(value) >> (32 - nbits)
	ones := bits.OnesCount32(reversed)
	e.bitCounts[0] += uint64(nbits - ones)
	e.bitCounts[1] += uint64(ones)

	remaining := 32 - int(e.numLocalBits)
	if nbits <= remaining {
		copyBits32(&e.localBits, int(e.numLocalBits), reversed, 0, nbits)
		e.numLocalBits += uint32(nbits)
		if e.numLocalBits == 32 {
			e.bits = append(e.bits, e.localBits)
			e.localBits = 0
			e.numLocalBits = 0
		}

		return
	}

	copyBits32(&e.localBits, int(e.numLocalBits), reversed, 0, remaining)
	e.bits = append(e.bits, e.localBits)
	e.localBits = 0
	copyBits32(&e.localBits, 0, reversed, remaining, nbits-remaining)
	e.numLocalBits = uint32(nbits - remaining)
}

func (e *RansBitEncoder) EndEncoding(w *core.Writer) error {
	if e.err != nil {
		err := e.err
		e.Clear()
		return err
	}

	total := e.bitCounts[0] + e.bitCounts[1]
	if total == 0 {
		total = 1
	}

	zeroProbRaw := uint32((float64(e.bitCounts[0]) / float64(total) * 256.0) + 0.5)
	zeroProb := uint8(255)
	if zeroProbRaw < 255 {
		zeroProb = uint8(zeroProbRaw)
	}

	if zeroProb == 0 {
		zeroProb = 1
	}

	e.buffer = slices.Grow(e.buffer[:0], (len(e.bits)+8)*8)
	buffer := e.buffer[:(len(e.bits)+8)*8]
	ans := ransBitWriter{buf: buffer, state: ransBitLBase}
	for i := int(e.numLocalBits) - 1; i >= 0; i-- {
		bit := (e.localBits >> i) & 1
		ans.write(int(bit), zeroProb)
	}

	for idx := len(e.bits) - 1; idx >= 0; idx-- {
		word := e.bits[idx]
		for i := 31; i >= 0; i-- {
			bit := (word >> i) & 1
			ans.write(int(bit), zeroProb)
		}
	}

	size, err := ans.end()
	if err != nil {
		return err
	}

	if err := w.WriteUint8(zeroProb); err != nil {
		return err
	}

	if err := core.EncodeVarUint32(w, uint32(size)); err != nil {
		return err
	}

	if err := w.WriteBytes(buffer[:size]); err != nil {
		return err
	}

	e.Clear()
	return nil
}

func (e *RansBitEncoder) Clear() {
	e.bitCounts = [2]uint64{}
	e.bits = resetUint32Scratch(e.bits)
	e.buffer = resetByteScratch(e.buffer)
	e.localBits = 0
	e.numLocalBits = 0
	e.err = nil
}

type RansBitDecoder struct {
	probZero  uint8
	buf       []byte
	bufOffset int
	state     uint32
	err       error
}

func (d *RansBitDecoder) StartDecoding(r *core.Reader) error {
	return d.StartDecodingVersioned(r, false)
}

func (d *RansBitDecoder) StartDecodingVersioned(r *core.Reader, legacy bool) error {
	d.Clear()
	probZero, err := r.ReadUint8()
	if err != nil {
		return err
	}

	d.probZero = probZero

	var size uint32
	if legacy {
		size, err = r.ReadUint32()
	} else {
		size, err = core.DecodeVarUint32(r)
	}

	if err != nil {
		return err
	}

	if size > uint32(r.Remaining()) {
		return errors.New("draco: invalid rANS bit payload size")
	}

	data, err := r.ReadBytesView(int(size))
	if err != nil {
		return err
	}

	if err := d.init(data); err != nil {
		return err
	}

	return nil
}

func (d *RansBitDecoder) DecodeNextBit() bool {
	if d.err != nil {
		return false
	}

	return d.read(d.probZero) > 0
}

func (d *RansBitDecoder) DecodeLeastSignificantBits32(nbits int, value *uint32) bool {
	if d.err != nil {
		return false
	}

	if nbits <= 0 || nbits > 32 {
		d.err = fmt.Errorf("draco: invalid number of bits for rANS bit decoding: %d", nbits)
		return false
	}

	var result uint32
	for nbits > 0 {
		result = (result << 1) + uint32(boolToBit(d.DecodeNextBit()))
		nbits--
	}

	*value = result
	return true
}

func (d *RansBitDecoder) EndDecoding() bool {
	return d.err == nil && d.state == ransBitLBase
}

func (d *RansBitDecoder) Err() error {
	return d.err
}

func (d *RansBitDecoder) Clear() {
	d.probZero = 0
	d.buf = nil
	d.bufOffset = 0
	d.state = 0
	d.err = nil
}

type ransBitWriter struct {
	buf       []byte
	bufOffset int
	state     uint32
}

func (w *ransBitWriter) write(val int, p0 uint8) {
	p := ransBitPrecision - uint32(p0)
	l := uint32(p0)
	if val != 0 {
		l = p
	}

	threshold := (ransBitLBase / ransBitPrecision) * ransBitIOBase * l
	if w.state >= threshold {
		w.buf[w.bufOffset] = byte(w.state % ransBitIOBase)
		w.bufOffset++
		w.state /= ransBitIOBase
	}

	quot := w.state / l
	rem := w.state % l
	if val != 0 {
		w.state = quot*ransBitPrecision + rem
		return
	}

	w.state = quot*ransBitPrecision + rem + p
}

func (w *ransBitWriter) end() (int, error) {
	state := w.state - ransBitLBase
	switch {
	case state < 1<<6:
		w.buf[w.bufOffset] = byte(state)
		return w.bufOffset + 1, nil
	case state < 1<<14:
		putLE16(w.buf[w.bufOffset:], (0x01<<14)+state)
		return w.bufOffset + 2, nil
	case state < 1<<22:
		putLE24(w.buf[w.bufOffset:], (0x02<<22)+state)
		return w.bufOffset + 3, nil
	default:
		return 0, errors.New("draco: rANS bit state is too large to serialize")
	}
}

func (d *RansBitDecoder) init(buf []byte) error {
	offset := len(buf)
	if offset < 1 {
		return errors.New("draco: invalid rANS bit buffer")
	}

	tag := buf[offset-1] >> 6
	d.buf = buf
	switch tag {
	case 0:
		d.bufOffset = offset - 1
		d.state = uint32(buf[offset-1] & 0x3F)
	case 1:
		if offset < 2 {
			return errors.New("draco: invalid rANS bit state size")
		}

		d.bufOffset = offset - 2
		d.state = getLE16(buf[offset-2:]) & 0x3FFF
	case 2:
		if offset < 3 {
			return errors.New("draco: invalid rANS bit state size")
		}

		d.bufOffset = offset - 3
		d.state = getLE24(buf[offset-3:]) & 0x3FFFFF
	default:
		return errors.New("draco: invalid rANS bit state tag")
	}

	d.state += ransBitLBase
	if d.state >= ransBitLBase*ransBitIOBase {
		return errors.New("draco: invalid rANS bit state")
	}

	return nil
}

func (d *RansBitDecoder) read(p0 uint8) int {
	p := ransBitPrecision - uint32(p0)
	if d.state < ransBitLBase && d.bufOffset > 0 {
		d.state = d.state*ransBitIOBase + uint32(d.buf[d.bufOffset-1])
		d.bufOffset--
	}

	x := d.state
	quot := x / ransBitPrecision
	rem := x % ransBitPrecision
	xn := quot * p
	if rem < p {
		d.state = xn + rem
		return 1
	}

	d.state = x - xn - p
	return 0
}

func copyBits32(dst *uint32, dstOffset int, src uint32, srcOffset, numBits int) {
	if numBits == 0 {
		return
	}

	mask := ^uint32(0)
	if numBits < 32 {
		mask = uint32(1<<numBits) - 1
	}

	*dst |= ((src >> srcOffset) & mask) << dstOffset
}

func getLE16(buf []byte) uint32 {
	return uint32(buf[0]) | uint32(buf[1])<<8
}

func getLE24(buf []byte) uint32 {
	return uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16
}

func putLE16(buf []byte, value uint32) {
	buf[0] = byte(value)
	buf[1] = byte(value >> 8)
}

func putLE24(buf []byte, value uint32) {
	buf[0] = byte(value)
	buf[1] = byte(value >> 8)
	buf[2] = byte(value >> 16)
}

func boolToBit(v bool) int {
	if v {
		return 1
	}

	return 0
}
