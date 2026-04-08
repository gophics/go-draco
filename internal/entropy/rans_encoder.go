package entropy

import (
	"errors"
	"math"
	"sort"

	"github.com/gophics/go-draco/internal/core"
)

type RansEncoder struct {
	state     uint32
	precision uint32
	lBase     uint32
	buf       []byte
}

func NewRansEncoder(precisionBits int) *RansEncoder {
	precision := uint32(1) << precisionBits
	return &RansEncoder{
		state:     precision * 4,
		precision: precision,
		lBase:     precision * 4,
	}
}

func (e *RansEncoder) Reset(expectedBytes int) {
	e.state = e.lBase
	if expectedBytes < 0 {
		expectedBytes = 0
	}

	e.buf = make([]byte, 0, expectedBytes+4)
}

func (e *RansEncoder) WriteSymbol(sym ransSym) {
	for e.state >= (e.lBase/e.precision)*256*sym.prob {
		e.buf = append(e.buf, byte(e.state))
		e.state /= 256
	}

	e.state = (e.state/sym.prob)*e.precision + e.state%sym.prob + sym.cumProb
}

func (e *RansEncoder) Bytes() []byte {
	out := append([]byte(nil), e.buf...)
	state := e.state - e.lBase
	switch {
	case state < 1<<6:
		out = append(out, byte(state))
	case state < 1<<14:
		out = append(out, byte(state), byte((1<<6)|((state>>8)&0x3F)))
	case state < 1<<22:
		out = append(out, byte(state), byte(state>>8), byte((2<<6)|((state>>16)&0x3F)))
	case state < 1<<30:
		out = append(out, byte(state), byte(state>>8), byte(state>>16), byte((3<<6)|((state>>24)&0x3F)))
	}

	return out
}

func buildRansProbabilityTable(frequencies []uint64, precisionBits int) ([]ransSym, error) {
	precision := uint32(1) << precisionBits
	var totalFreq uint64
	maxValidSymbol := -1
	for i, freq := range frequencies {
		totalFreq += freq
		if freq > 0 {
			maxValidSymbol = i
		}
	}

	if totalFreq == 0 || maxValidSymbol < 0 {
		return nil, errors.New("draco: empty rANS alphabet")
	}

	probabilityTable := make([]ransSym, maxValidSymbol+1)
	totalFreqD := float64(totalFreq)
	precisionD := float64(precision)

	totalRansProb := uint32(0)
	for i := 0; i <= maxValidSymbol; i++ {
		freq := frequencies[i]
		ransProb := uint32(float64(freq)/totalFreqD*precisionD + 0.5)
		if ransProb == 0 && freq > 0 {
			ransProb = 1
		}

		probabilityTable[i].prob = ransProb
		totalRansProb += ransProb
	}

	if totalRansProb != precision {
		sorted := make([]int, len(probabilityTable))
		for i := range probabilityTable {
			sorted[i] = i
		}

		sort.SliceStable(sorted, func(i, j int) bool {
			return probabilityTable[sorted[i]].prob < probabilityTable[sorted[j]].prob
		})
		if totalRansProb < precision {
			probabilityTable[sorted[len(sorted)-1]].prob += precision - totalRansProb
		} else {
			errorProb := int32(totalRansProb - precision)
			for errorProb > 0 {
				actTotalProbD := float64(totalRansProb)
				actRelErrorD := precisionD / actTotalProbD
				for j := len(sorted) - 1; j > 0; j-- {
					symbolID := sorted[j]
					if probabilityTable[symbolID].prob <= 1 {
						if j == len(sorted)-1 {
							return nil, errors.New("draco: invalid rANS probability normalization")
						}

						break
					}

					newProb := int32(math.Floor(actRelErrorD * float64(probabilityTable[symbolID].prob)))
					fix := int32(probabilityTable[symbolID].prob) - newProb
					if fix == 0 {
						fix = 1
					}

					if fix >= int32(probabilityTable[symbolID].prob) {
						fix = int32(probabilityTable[symbolID].prob) - 1
					}

					if fix > errorProb {
						fix = errorProb
					}

					probabilityTable[symbolID].prob -= uint32(fix)
					totalRansProb -= uint32(fix)
					errorProb -= fix
					if totalRansProb == precision {
						break
					}
				}
			}
		}
	}

	cumProb := uint32(0)
	for i := range probabilityTable {
		probabilityTable[i].cumProb = cumProb
		cumProb += probabilityTable[i].prob
	}

	if cumProb != precision {
		return nil, errors.New("draco: invalid rANS precision sum")
	}

	return probabilityTable, nil
}

func encodeRansProbabilityTable(w *core.Writer, probabilityTable []ransSym) error {
	if err := core.EncodeVarUint32(w, uint32(len(probabilityTable))); err != nil {
		return err
	}

	for i := 0; i < len(probabilityTable); i++ {
		prob := probabilityTable[i].prob
		if prob == 0 {
			offset := 0
			for ; offset < (1<<6)-1; offset++ {
				if probabilityTable[i+offset+1].prob > 0 {
					break
				}
			}

			if err := w.WriteUint8(uint8((offset << 2) | 3)); err != nil {
				return err
			}

			i += offset
			continue
		}

		numExtraBytes := 0
		if prob >= 1<<6 {
			numExtraBytes++
			if prob >= 1<<14 {
				numExtraBytes++
				if prob >= 1<<22 {
					return errors.New("draco: rANS probability too large")
				}
			}
		}

		if err := w.WriteUint8(uint8((prob << 2) | uint32(numExtraBytes&3))); err != nil {
			return err
		}

		for b := 0; b < numExtraBytes; b++ {
			if err := w.WriteUint8(uint8(prob >> (8*(b+1) - 2))); err != nil {
				return err
			}
		}
	}

	return nil
}
