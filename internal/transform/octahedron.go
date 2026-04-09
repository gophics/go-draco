package transform

import (
	"fmt"
	"math"
	"math/bits"

	"github.com/gophics/go-draco/internal/core"
)

type Octahedron struct {
	quantizationBits  int32
	maxQuantizedValue int32
	maxValue          int32
	centerValue       int32
	dequantScale      float32
}

func (t *Octahedron) SetQuantizationBits(q int) error {
	if q < 2 || q > 30 {
		return fmt.Errorf("draco: invalid octahedron quantization bits %d", q)
	}

	t.quantizationBits = int32(q)
	t.maxQuantizedValue = (1 << t.quantizationBits) - 1
	t.maxValue = t.maxQuantizedValue - 1
	t.centerValue = t.maxValue / 2
	t.dequantScale = 2 / float32(t.maxValue)
	return nil
}

func (t *Octahedron) SetMaxQuantizedValue(maxQuantizedValue int32) error {
	if maxQuantizedValue <= 0 || maxQuantizedValue%2 == 0 {
		return fmt.Errorf("draco: invalid octahedron max quantized value %d", maxQuantizedValue)
	}

	return t.SetQuantizationBits(bits.Len32(uint32(maxQuantizedValue)))
}

func (t *Octahedron) Encode(w *core.Writer) error {
	return w.WriteUint8(uint8(t.quantizationBits))
}

func (t *Octahedron) Decode(r *core.Reader) error {
	q, err := r.ReadUint8()
	if err != nil {
		return err
	}

	return t.SetQuantizationBits(int(q))
}

func (t *Octahedron) CanonicalizeOctahedralCoords(s, tt int32) (int32, int32) {
	if (s == 0 && tt == 0) || (s == 0 && tt == t.maxValue) || (s == t.maxValue && tt == 0) {
		return t.maxValue, t.maxValue
	}

	if s == 0 && tt > t.centerValue {
		return s, t.centerValue - (tt - t.centerValue)
	}

	if s == t.maxValue && tt < t.centerValue {
		return s, t.centerValue + (t.centerValue - tt)
	}

	if tt == t.maxValue && s < t.centerValue {
		return t.centerValue + (t.centerValue - s), tt
	}

	if tt == 0 && s > t.centerValue {
		return t.centerValue - (s - t.centerValue), tt
	}

	return s, tt
}

func (t *Octahedron) IsInDiamond(s, tt int32) bool {
	return absInt32(s)+absInt32(tt) <= t.centerValue
}

func (t *Octahedron) InvertDiamond(s, tt *int32) {
	var signS, signT int32

	switch {
	case *s >= 0 && *tt >= 0:
		signS, signT = 1, 1
	case *s <= 0 && *tt <= 0:
		signS, signT = -1, -1
	default:
		if *s > 0 {
			signS = 1
		} else {
			signS = -1
		}

		if *tt > 0 {
			signT = 1
		} else {
			signT = -1
		}
	}

	cornerPointS := uint32(signS * t.centerValue)
	cornerPointT := uint32(signT * t.centerValue)
	us := uint32(*s)
	ut := uint32(*tt)
	us = us + us - cornerPointS
	ut = ut + ut - cornerPointT
	if signS*signT >= 0 {
		temp := us
		us = uint32(-int32(ut))
		ut = uint32(-int32(temp))
	} else {
		us, ut = ut, us
	}

	us += cornerPointS
	ut += cornerPointT
	*s = int32(us) / 2
	*tt = int32(ut) / 2
}

func (t *Octahedron) ModMax(x int32) int32 {
	if x > t.centerValue {
		return x - t.maxQuantizedValue
	}

	if x < -t.centerValue {
		return x + t.maxQuantizedValue
	}

	return x
}

func (t *Octahedron) MakePositive(x int32) int32 {
	if x < 0 {
		return x + t.maxQuantizedValue
	}

	return x
}

func (t *Octahedron) IntegerVectorToQuantizedOctahedralCoords(intVec [3]int32) (int32, int32) {
	var s, tt int32
	if intVec[0] >= 0 {
		s = intVec[1] + t.centerValue
		tt = intVec[2] + t.centerValue
	} else {
		if intVec[1] < 0 {
			s = absInt32(intVec[2])
		} else {
			s = t.maxValue - absInt32(intVec[2])
		}

		if intVec[2] < 0 {
			tt = absInt32(intVec[1])
		} else {
			tt = t.maxValue - absInt32(intVec[1])
		}
	}

	return t.CanonicalizeOctahedralCoords(s, tt)
}

func (t *Octahedron) FloatVectorToQuantizedOctahedralCoords(vec []float32) (int32, int32) {
	absSum := math.Abs(float64(vec[0])) + math.Abs(float64(vec[1])) + math.Abs(float64(vec[2]))
	scaled := [3]float64{1, 0, 0}
	if absSum > 1e-6 {
		scale := 1.0 / absSum
		scaled[0] = float64(vec[0]) * scale
		scaled[1] = float64(vec[1]) * scale
		scaled[2] = float64(vec[2]) * scale
	}

	intVec := [3]int32{
		int32(math.Floor(scaled[0]*float64(t.centerValue) + 0.5)),
		int32(math.Floor(scaled[1]*float64(t.centerValue) + 0.5)),
	}
	intVec[2] = t.centerValue - absInt32(intVec[0]) - absInt32(intVec[1])
	if intVec[2] < 0 {
		if intVec[1] > 0 {
			intVec[1] += intVec[2]
		} else {
			intVec[1] -= intVec[2]
		}

		intVec[2] = 0
	}

	if scaled[2] < 0 {
		intVec[2] *= -1
	}

	return t.IntegerVectorToQuantizedOctahedralCoords(intVec)
}

func (t *Octahedron) CanonicalizeIntegerVector(vec []int32) {
	if len(vec) < 3 {
		return
	}

	absSum := int64(absInt32(vec[0])) + int64(absInt32(vec[1])) + int64(absInt32(vec[2]))
	if absSum == 0 {
		vec[0] = t.centerValue
		vec[1] = 0
		vec[2] = 0
		return
	}

	vec[0] = int32((int64(vec[0]) * int64(t.centerValue)) / absSum)
	vec[1] = int32((int64(vec[1]) * int64(t.centerValue)) / absSum)
	if vec[2] >= 0 {
		vec[2] = t.centerValue - absInt32(vec[0]) - absInt32(vec[1])
	} else {
		vec[2] = -(t.centerValue - absInt32(vec[0]) - absInt32(vec[1]))
	}
}

func (t *Octahedron) InvertDirection(s, tt *int32) {
	*s *= -1
	*tt *= -1
	t.InvertDiamond(s, tt)
}

func (t *Octahedron) QuantizedOctahedralCoordsToUnitVector(s, tt int32) [3]float32 {
	return t.OctahedralCoordsToUnitVector(float32(s)*t.dequantScale-1, float32(tt)*t.dequantScale-1)
}

func (t *Octahedron) OctahedralCoordsToUnitVector(inS, inT float32) [3]float32 {
	y := inS
	z := inT
	absY := y
	if absY < 0 {
		absY = -absY
	}

	absZ := z
	if absZ < 0 {
		absZ = -absZ
	}

	x := 1 - absY - absZ
	xOffset := -x
	if xOffset < 0 {
		xOffset = 0
	}

	if y < 0 {
		y += xOffset
	} else {
		y -= xOffset
	}

	if z < 0 {
		z += xOffset
	} else {
		z -= xOffset
	}

	normSquared := x*x + y*y + z*z
	if normSquared < 1e-6 {
		return [3]float32{}
	}

	inv := 1 / float32(math.Sqrt(float64(normSquared)))
	return [3]float32{x * inv, y * inv, z * inv}
}

func absInt32(v int32) int32 {
	if v < 0 {
		return -v
	}

	return v
}
