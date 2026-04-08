package entropy

import (
	"fmt"

	"github.com/gophics/go-draco/internal/core"
)

type bitEncoder interface {
	StartEncoding()
	EncodeBit(bool)
	EncodeLeastSignificantBits32(int, uint32)
	EndEncoding(*core.Writer) error
	Clear()
}

type bitDecoder interface {
	StartDecoding(*core.Reader) error
	DecodeNextBit() bool
	DecodeLeastSignificantBits32(int, *uint32) bool
	EndDecoding() bool
	Clear()
}

type FoldedBit32Encoder struct {
	folded [32]bitEncoder
	bits   bitEncoder
	err    error
}

func NewFoldedBit32Encoder(factory func() bitEncoder) *FoldedBit32Encoder {
	e := &FoldedBit32Encoder{}
	for i := range e.folded {
		e.folded[i] = factory()
	}

	e.bits = factory()
	return e
}

func NewFoldedRAnsBit32Encoder() *FoldedBit32Encoder {
	return NewFoldedBit32Encoder(func() bitEncoder { return &RansBitEncoder{} })
}

func (e *FoldedBit32Encoder) StartEncoding() {
	e.err = nil
	for i := range e.folded {
		e.folded[i].StartEncoding()
	}

	e.bits.StartEncoding()
}

func (e *FoldedBit32Encoder) EncodeBit(bit bool) {
	if e.err != nil {
		return
	}

	e.bits.EncodeBit(bit)
}

func (e *FoldedBit32Encoder) EncodeLeastSignificantBits32(nbits int, value uint32) {
	if e.err != nil {
		return
	}

	if nbits <= 0 || nbits > 32 {
		e.err = fmt.Errorf("draco: invalid number of bits for folded bit encoding: %d", nbits)
		return
	}

	selector := uint32(1) << (nbits - 1)
	for i := 0; i < nbits; i++ {
		e.folded[i].EncodeBit((value & selector) != 0)
		selector >>= 1
	}
}

func (e *FoldedBit32Encoder) EndEncoding(w *core.Writer) error {
	if e.err != nil {
		err := e.err
		e.Clear()
		return err
	}

	for i := range e.folded {
		if err := e.folded[i].EndEncoding(w); err != nil {
			return err
		}
	}

	return e.bits.EndEncoding(w)
}

func (e *FoldedBit32Encoder) Clear() {
	e.err = nil
	for i := range e.folded {
		e.folded[i].Clear()
	}

	e.bits.Clear()
}

type FoldedBit32Decoder struct {
	folded [32]bitDecoder
	bits   bitDecoder
	err    error
}

func NewFoldedBit32Decoder(factory func() bitDecoder) *FoldedBit32Decoder {
	d := &FoldedBit32Decoder{}
	for i := range d.folded {
		d.folded[i] = factory()
	}

	d.bits = factory()
	return d
}

type ransBitDecoderAdapter struct {
	RansBitDecoder
}

func (d *ransBitDecoderAdapter) DecodeLeastSignificantBits32(nbits int, value *uint32) bool {
	return d.RansBitDecoder.DecodeLeastSignificantBits32(nbits, value)
}

func NewFoldedRAnsBit32Decoder() *FoldedBit32Decoder {
	return NewFoldedBit32Decoder(func() bitDecoder { return &ransBitDecoderAdapter{} })
}

func (d *FoldedBit32Decoder) StartDecoding(r *core.Reader) error {
	d.err = nil
	for i := range d.folded {
		if err := d.folded[i].StartDecoding(r); err != nil {
			return err
		}
	}

	return d.bits.StartDecoding(r)
}

func (d *FoldedBit32Decoder) DecodeNextBit() bool {
	if d.err != nil {
		return false
	}

	return d.bits.DecodeNextBit()
}

func (d *FoldedBit32Decoder) DecodeLeastSignificantBits32(nbits int, value *uint32) bool {
	if d.err != nil {
		return false
	}

	if nbits <= 0 || nbits > 32 {
		d.err = fmt.Errorf("draco: invalid number of bits for folded bit decoding: %d", nbits)
		return false
	}

	var result uint32
	for i := 0; i < nbits; i++ {
		bit := d.folded[i].DecodeNextBit()
		result = (result << 1) + uint32(boolToBit(bit))
	}

	*value = result
	return true
}

func (d *FoldedBit32Decoder) EndDecoding() bool {
	if d.err != nil {
		return false
	}

	for i := range d.folded {
		if !d.folded[i].EndDecoding() {
			return false
		}
	}

	return d.bits.EndDecoding()
}

func (d *FoldedBit32Decoder) Clear() {
	d.err = nil
	for i := range d.folded {
		d.folded[i].Clear()
	}

	d.bits.Clear()
}

func (d *FoldedBit32Decoder) Err() error {
	return d.err
}
