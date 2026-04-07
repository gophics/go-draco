package core

import "errors"

func EncodeVarUint32(w *Writer, v uint32) error {
	for {
		out := uint8(v & 0x7f)
		v >>= 7
		if v != 0 {
			out |= 0x80
		}

		if err := w.WriteUint8(out); err != nil {
			return err
		}

		if v == 0 {
			return nil
		}
	}
}

func DecodeVarUint32(r *Reader) (uint32, error) {
	var (
		value uint32
		shift uint
	)
	for i := 0; i < 5; i++ {
		b, err := r.ReadUint8()
		if err != nil {
			return 0, err
		}

		value |= uint32(b&0x7f) << shift
		if b&0x80 == 0 {
			return value, nil
		}

		shift += 7
	}

	return 0, errors.New("draco: invalid uint32 varint")
}

func EncodeVarUint64(w *Writer, v uint64) error {
	for {
		out := uint8(v & 0x7f)
		v >>= 7
		if v != 0 {
			out |= 0x80
		}

		if err := w.WriteUint8(out); err != nil {
			return err
		}

		if v == 0 {
			return nil
		}
	}
}

func DecodeVarUint64(r *Reader) (uint64, error) {
	var (
		value uint64
		shift uint
	)
	for i := 0; i < 10; i++ {
		b, err := r.ReadUint8()
		if err != nil {
			return 0, err
		}

		value |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return value, nil
		}

		shift += 7
	}

	return 0, errors.New("draco: invalid uint64 varint")
}

func EncodeVarInt32(w *Writer, v int32) error {
	return EncodeVarUint32(w, convertSignedInt32ToSymbol(v))
}

func DecodeVarInt32(r *Reader) (int32, error) {
	value, err := DecodeVarUint32(r)
	if err != nil {
		return 0, err
	}

	return convertSymbolToSignedInt32(value), nil
}

func convertSignedInt32ToSymbol(v int32) uint32 {
	if v >= 0 {
		return uint32(v) << 1
	}

	return (uint32(-(v + 1)) << 1) | 1
}

func convertSymbolToSignedInt32(v uint32) int32 {
	if v&1 == 0 {
		return int32(v >> 1)
	}

	return -int32(v>>1) - 1
}
