package transform

import (
	"errors"

	"github.com/gophics/go-draco/internal/core"
)

type Wrap struct {
	numComponents  int
	minValue       int32
	maxValue       int32
	maxDif         int32
	maxCorrection  int32
	minCorrection  int32
	fixedScratch   [16]int32
	clampedScratch []int32
}

func (t *Wrap) Init(numComponents int) {
	t.numComponents = numComponents
	if numComponents <= len(t.fixedScratch) {
		t.clampedScratch = t.fixedScratch[:numComponents]
		return
	}

	if cap(t.clampedScratch) < numComponents {
		t.clampedScratch = make([]int32, numComponents)
	} else {
		t.clampedScratch = t.clampedScratch[:numComponents]
	}
}

func (t *Wrap) InitFromValues(values []int32, numComponents int) error {
	t.Init(numComponents)
	if len(values) == 0 {
		return nil
	}

	minValue := values[0]
	maxValue := values[0]
	for _, value := range values[1:] {
		if value < minValue {
			minValue = value
		} else if value > maxValue {
			maxValue = value
		}
	}

	t.minValue = minValue
	t.maxValue = maxValue
	dif := int64(maxValue) - int64(minValue)
	if dif < 0 || dif >= 1<<31-1 {
		return errors.New("draco: invalid wrap transform range")
	}

	t.maxDif = 1 + int32(dif)
	t.maxCorrection = t.maxDif / 2
	t.minCorrection = -t.maxCorrection
	if (t.maxDif & 1) == 0 {
		t.maxCorrection--
	}

	return nil
}

func (t *Wrap) Decode(r *core.Reader) error {
	minValue, err := r.ReadInt32()
	if err != nil {
		return err
	}

	maxValue, err := r.ReadInt32()
	if err != nil {
		return err
	}

	if minValue > maxValue {
		return errors.New("draco: invalid wrap transform bounds")
	}

	t.minValue = minValue
	t.maxValue = maxValue
	dif := int64(maxValue) - int64(minValue)
	if dif < 0 || dif >= 1<<31-1 {
		return errors.New("draco: invalid wrap transform range")
	}

	t.maxDif = 1 + int32(dif)
	t.maxCorrection = t.maxDif / 2
	t.minCorrection = -t.maxCorrection
	if (t.maxDif & 1) == 0 {
		t.maxCorrection--
	}

	return nil
}

func (t *Wrap) Encode(w *core.Writer) error {
	if err := w.WriteInt32(t.minValue); err != nil {
		return err
	}

	return w.WriteInt32(t.maxValue)
}

func (t *Wrap) ComputeCorrection(original, predicted []int32, out []int32) {
	predicted = t.clampPredictedValue(predicted)
	for i := 0; i < t.numComponents; i++ {
		out[i] = original[i] - predicted[i]
		if out[i] < t.minCorrection {
			out[i] += t.maxDif
		} else if out[i] > t.maxCorrection {
			out[i] -= t.maxDif
		}
	}
}

func (t *Wrap) ComputeOriginalValue(predicted, corr []int32, out []int32) {
	predicted = t.clampPredictedValue(predicted)
	for i := 0; i < t.numComponents; i++ {
		out[i] = int32(uint32(predicted[i]) + uint32(corr[i]))
		if out[i] > t.maxValue {
			out[i] -= t.maxDif
		} else if out[i] < t.minValue {
			out[i] += t.maxDif
		}
	}
}

func (t *Wrap) clampPredictedValue(predicted []int32) []int32 {
	for i := 0; i < t.numComponents; i++ {
		switch {
		case predicted[i] > t.maxValue:
			t.clampedScratch[i] = t.maxValue
		case predicted[i] < t.minValue:
			t.clampedScratch[i] = t.minValue
		default:
			t.clampedScratch[i] = predicted[i]
		}
	}

	return t.clampedScratch
}
