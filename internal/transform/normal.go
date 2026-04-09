package transform

import (
	"fmt"

	"github.com/gophics/go-draco/internal/core"
)

type NormalPrediction interface {
	Decode(r *core.Reader) error
	ComputeOriginalValue(pred, corr, out []int32)
	CorrectionsPositive() bool
	BaseOctahedron() *Octahedron
}

type NormalOctahedron struct {
	Octahedron
}

func (t *NormalOctahedron) Decode(r *core.Reader) error {
	maxQuantizedValue, err := r.ReadInt32()
	if err != nil {
		return err
	}

	return t.SetMaxQuantizedValue(maxQuantizedValue)
}

func (t *NormalOctahedron) CorrectionsPositive() bool {
	return true
}

func (t *NormalOctahedron) ComputeOriginalValue(pred, corr, out []int32) {
	center := t.centerValue
	predX := pred[0] - center
	predY := pred[1] - center
	predInDiamond := t.IsInDiamond(predX, predY)
	if !predInDiamond {
		t.InvertDiamond(&predX, &predY)
	}

	origX := t.ModMax(addAsUnsigned(predX, corr[0]))
	origY := t.ModMax(addAsUnsigned(predY, corr[1]))
	if !predInDiamond {
		t.InvertDiamond(&origX, &origY)
	}

	out[0], out[1] = origX+center, origY+center
}

func (t *NormalOctahedron) BaseOctahedron() *Octahedron {
	return &t.Octahedron
}

type NormalOctahedronCanonicalized struct {
	Octahedron
}

func (t *NormalOctahedronCanonicalized) Decode(r *core.Reader) error {
	maxQuantizedValue, err := r.ReadInt32()
	if err != nil {
		return err
	}

	centerValue, err := r.ReadInt32()
	if err != nil {
		return err
	}

	_ = centerValue
	if err := t.SetMaxQuantizedValue(maxQuantizedValue); err != nil {
		return err
	}

	if t.quantizationBits < 2 || t.quantizationBits > 30 {
		return fmt.Errorf("draco: invalid canonicalized octahedron quantization bits %d", t.quantizationBits)
	}

	return nil
}

func (t *NormalOctahedronCanonicalized) Encode(w *core.Writer) error {
	if err := w.WriteInt32(t.maxQuantizedValue); err != nil {
		return err
	}

	return w.WriteInt32(t.centerValue)
}

func (t *NormalOctahedronCanonicalized) CorrectionsPositive() bool {
	return true
}

func (t *NormalOctahedronCanonicalized) ComputeCorrection(orig, pred, out []int32) {
	center := octPoint{t.centerValue, t.centerValue}
	origPoint := octPoint{orig[0], orig[1]}.sub(center)
	predPoint := octPoint{pred[0], pred[1]}.sub(center)
	if !t.IsInDiamond(predPoint[0], predPoint[1]) {
		t.InvertDiamond(&origPoint[0], &origPoint[1])
		t.InvertDiamond(&predPoint[0], &predPoint[1])
	}

	if !t.isInBottomLeft(predPoint) {
		rotationCount := t.rotationCount(predPoint)
		origPoint = t.rotatePoint(origPoint, rotationCount)
		predPoint = t.rotatePoint(predPoint, rotationCount)
	}

	out[0] = t.MakePositive(origPoint[0] - predPoint[0])
	out[1] = t.MakePositive(origPoint[1] - predPoint[1])
}

func (t *NormalOctahedronCanonicalized) ComputeOriginalValue(pred, corr, out []int32) {
	center := t.centerValue
	predX := pred[0] - center
	predY := pred[1] - center
	predInDiamond := t.IsInDiamond(predX, predY)
	if !predInDiamond {
		t.InvertDiamond(&predX, &predY)
	}

	predInBottomLeft := (predX == 0 && predY == 0) || (predX < 0 && predY <= 0)
	var rotationCount int32
	if !predInBottomLeft {
		rotationCount = t.rotationCount(octPoint{predX, predY})
		predX, predY = rotateOctPointValues(predX, predY, rotationCount)
	}

	origX := t.ModMax(addAsUnsigned(predX, corr[0]))
	origY := t.ModMax(addAsUnsigned(predY, corr[1]))
	if !predInBottomLeft {
		origX, origY = rotateOctPointValues(origX, origY, (4-rotationCount)%4)
	}

	if !predInDiamond {
		t.InvertDiamond(&origX, &origY)
	}

	out[0], out[1] = origX+center, origY+center
}

func (t *NormalOctahedronCanonicalized) BaseOctahedron() *Octahedron {
	return &t.Octahedron
}

type octPoint [2]int32

func (p octPoint) sub(other octPoint) octPoint {
	return octPoint{p[0] - other[0], p[1] - other[1]}
}

func addAsUnsigned(a, b int32) int32 {
	return int32(uint32(a) + uint32(b))
}

func (t *NormalOctahedronCanonicalized) rotationCount(pred octPoint) int32 {
	signX := pred[0]
	signY := pred[1]
	switch {
	case signX == 0:
		switch {
		case signY == 0:
			return 0
		case signY > 0:
			return 3
		default:
			return 1
		}
	case signX > 0:
		if signY >= 0 {
			return 2
		}

		return 1
	default:
		if signY <= 0 {
			return 0
		}

		return 3
	}
}

func (t *NormalOctahedronCanonicalized) rotatePoint(p octPoint, rotationCount int32) octPoint {
	x, y := rotateOctPointValues(p[0], p[1], rotationCount)
	return octPoint{x, y}
}

func rotateOctPointValues(x, y int32, rotationCount int32) (int32, int32) {
	switch rotationCount {
	case 1:
		return y, -x
	case 2:
		return -x, -y
	case 3:
		return -y, x
	default:
		return x, y
	}
}

func (t *NormalOctahedronCanonicalized) isInBottomLeft(p octPoint) bool {
	if p[0] == 0 && p[1] == 0 {
		return true
	}

	return p[0] < 0 && p[1] <= 0
}
