package entropy

import (
	"errors"
	"slices"
)

type ransSym struct {
	prob    uint32
	cumProb uint32
}

type ransLookupCacheEntry struct {
	hash          uint64
	probabilities []uint32
	lutTable      []uint32
	probTable     []ransSym
}

type RansDecoder struct {
	buf         []byte
	bufOffset   int
	state       uint32
	precision   uint32
	shift       uint
	mask        uint32
	lBase       uint32
	lutTable    []uint32
	probTable   []ransSym
	lutWork     []uint32
	probWork    []ransSym
	lookupCache [8]ransLookupCacheEntry
	nextCache   int
}

func NewRansDecoder(precisionBits int) *RansDecoder {
	precision := uint32(1) << precisionBits
	return &RansDecoder{
		precision: precision,
		shift:     uint(precisionBits),
		mask:      precision - 1,
		lBase:     precision * 4,
	}
}

// ClearInput drops the current encoded byte slice without discarding lookup tables.
func (d *RansDecoder) ClearInput() {
	if d == nil {
		return
	}

	d.buf = nil
	d.bufOffset = 0
	d.state = 0
}

func (d *RansDecoder) Init(buf []byte) error {
	offset := len(buf)
	if offset < 1 {
		return errors.New("draco: invalid rANS buffer")
	}

	x := buf[offset-1] >> 6
	d.buf = buf
	switch x {
	case 0:
		d.bufOffset = offset - 1
		d.state = uint32(buf[offset-1] & 0x3F)
	case 1:
		if offset < 2 {
			return errors.New("draco: invalid rANS state size")
		}

		d.bufOffset = offset - 2
		d.state = uint32(buf[offset-2]) | uint32(buf[offset-1]&0x3F)<<8
	case 2:
		if offset < 3 {
			return errors.New("draco: invalid rANS state size")
		}

		d.bufOffset = offset - 3
		d.state = uint32(buf[offset-3]) | uint32(buf[offset-2])<<8 | uint32(buf[offset-1]&0x3F)<<16
	case 3:
		if offset < 4 {
			return errors.New("draco: invalid rANS state size")
		}

		d.bufOffset = offset - 4
		d.state = uint32(buf[offset-4]) | uint32(buf[offset-3])<<8 | uint32(buf[offset-2])<<16 | uint32(buf[offset-1]&0x3F)<<24
	default:
		return errors.New("draco: invalid rANS state tag")
	}

	d.state += d.lBase
	if d.state >= d.lBase*256 {
		return errors.New("draco: invalid rANS state")
	}

	return nil
}

func (d *RansDecoder) BuildLookup(probabilities []uint32) error {
	var hash uint64
	if d.precision <= 4096 {
		hash = hashRansProbabilities(probabilities)
		for i := range d.lookupCache {
			entry := &d.lookupCache[i]
			if entry.hash == hash && slices.Equal(entry.probabilities, probabilities) {
				d.lutTable = entry.lutTable
				d.probTable = entry.probTable
				return nil
			}
		}
	}

	d.lutWork = slices.Grow(d.lutWork[:0], int(d.precision))
	d.lutWork = d.lutWork[:d.precision]
	d.probWork = slices.Grow(d.probWork[:0], len(probabilities))
	d.probWork = d.probWork[:len(probabilities)]
	var cumProb, actProb uint32
	for i, prob := range probabilities {
		d.probWork[i] = ransSym{
			prob:    prob,
			cumProb: cumProb,
		}
		cumProb += prob
		if cumProb > d.precision {
			return errors.New("draco: invalid rANS cumulative probability")
		}

		for j := actProb; j < cumProb; j++ {
			d.lutWork[j] = uint32(i)
		}

		actProb = cumProb
	}

	if cumProb != d.precision {
		return errors.New("draco: invalid rANS precision sum")
	}

	d.lutTable = d.lutWork
	d.probTable = d.probWork
	if d.precision <= 4096 {
		entry := &d.lookupCache[d.nextCache%len(d.lookupCache)]
		entry.hash = hash
		entry.probabilities = append(entry.probabilities[:0], probabilities...)
		entry.lutTable = append(entry.lutTable[:0], d.lutWork...)
		entry.probTable = append(entry.probTable[:0], d.probWork...)
		d.nextCache++
	}

	return nil
}

func hashRansProbabilities(probabilities []uint32) uint64 {
	hash := uint64(1469598103934665603)
	for _, probability := range probabilities {
		hash ^= uint64(probability)
		hash *= 1099511628211
	}

	return hash ^ uint64(len(probabilities))
}

func (d *RansDecoder) ReadSymbol() uint32 {
	for d.state < d.lBase && d.bufOffset > 0 {
		d.state = d.state*256 + uint32(d.buf[d.bufOffset-1])
		d.bufOffset--
	}

	quo := d.state >> d.shift
	rem := d.state & d.mask
	symbol := d.lutTable[rem]
	sym := d.probTable[symbol]
	d.state = quo*sym.prob + rem - sym.cumProb
	return symbol
}
