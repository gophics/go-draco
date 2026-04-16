package draco

import (
	"context"
	"errors"
	"fmt"

	"github.com/gophics/go-draco/internal/core"
	"github.com/gophics/go-draco/internal/entropy"
	"github.com/gophics/go-draco/internal/topology"
)

type geometricNormalPredictionData struct {
	flips []bool
}

type meshGeometricNormalPredictionContext struct {
	attrCtx  *meshPredictionContext
	position *Attribute
	octa     *octahedronTransform
}

func newMeshGeometricNormalPredictionContext(encodeCtx context.Context, mesh *Mesh, attr, position *Attribute, numEntries int, octa *octahedronTransform, tableCache *meshPredictionTableCache) (*meshGeometricNormalPredictionContext, error) {
	if position == nil {
		return nil, fmt.Errorf("%w: geometric normal prediction requires position attribute", ErrUnsupportedFeature)
	}

	if position.NumComponents < 3 {
		return nil, fmt.Errorf("%w: geometric normal prediction requires 3-component positions", ErrUnsupportedFeature)
	}

	if octa == nil {
		return nil, fmt.Errorf("%w: geometric normal prediction requires octahedron transform", ErrUnsupportedFeature)
	}

	attrCtx, err := newMeshPredictionContext(encodeCtx, mesh, attr, numEntries, tableCache)
	if err != nil {
		return nil, err
	}

	return &meshGeometricNormalPredictionContext{
		attrCtx:  attrCtx,
		position: position,
		octa:     octa,
	}, nil
}

func computeMeshGeometricNormalCorrections(encodeCtx context.Context, ctx *meshGeometricNormalPredictionContext, values []int32, numComponents int, transform *normalOctahedronCanonicalizedPredictionTransform) ([]int32, *geometricNormalPredictionData, error) {
	if numComponents != 2 {
		return nil, nil, fmt.Errorf("%w: geometric normal prediction requires 2-component portable normals", ErrUnsupportedFeature)
	}

	corrections := make([]int32, len(values))
	data := &geometricNormalPredictionData{
		flips: make([]bool, len(values)/numComponents),
	}
	posPredicted := make([]int32, 2)
	negPredicted := make([]int32, 2)
	posCorrection := make([]int32, 2)
	negCorrection := make([]int32, 2)
	vector := make([]int32, 3)
	for entryID := 0; entryID < len(values)/numComponents; entryID++ {
		if err := checkContextEvery(encodeCtx, entryID); err != nil {
			return nil, nil, err
		}

		corner := ctx.attrCtx.dataToCorner[entryID]
		if corner == topology.InvalidCorner {
			return nil, nil, fmt.Errorf("draco: geometric normal entry %d has no representative corner", entryID)
		}

		if err := ctx.computePredictedNormal(corner, vector); err != nil {
			return nil, nil, err
		}

		ctx.octa.CanonicalizeIntegerVector(vector)
		posPredicted[0], posPredicted[1] = ctx.octa.IntegerVectorToQuantizedOctahedralCoords([3]int32{vector[0], vector[1], vector[2]})
		vector[0], vector[1], vector[2] = -vector[0], -vector[1], -vector[2]
		negPredicted[0], negPredicted[1] = ctx.octa.IntegerVectorToQuantizedOctahedralCoords([3]int32{vector[0], vector[1], vector[2]})

		offset := entryID * numComponents
		transform.ComputeCorrection(values[offset:offset+numComponents], posPredicted, posCorrection)
		transform.ComputeCorrection(values[offset:offset+numComponents], negPredicted, negCorrection)
		posCorrection[0] = ctx.octa.ModMax(posCorrection[0])
		posCorrection[1] = ctx.octa.ModMax(posCorrection[1])
		negCorrection[0] = ctx.octa.ModMax(negCorrection[0])
		negCorrection[1] = ctx.octa.ModMax(negCorrection[1])
		if absInt32(posCorrection[0])+absInt32(posCorrection[1]) < absInt32(negCorrection[0])+absInt32(negCorrection[1]) {
			corrections[offset] = ctx.octa.MakePositive(posCorrection[0])
			corrections[offset+1] = ctx.octa.MakePositive(posCorrection[1])
			data.flips[entryID] = false
			continue
		}

		corrections[offset] = ctx.octa.MakePositive(negCorrection[0])
		corrections[offset+1] = ctx.octa.MakePositive(negCorrection[1])
		data.flips[entryID] = true
	}

	return corrections, data, nil
}

func restoreMeshGeometricNormalValues(decodeCtx context.Context, ctx *meshGeometricNormalPredictionContext, corrections []int32, numComponents int, transform normalPredictionTransform, data *geometricNormalPredictionData) ([]int32, error) {
	if numComponents != 2 {
		return nil, fmt.Errorf("%w: geometric normal prediction requires 2-component portable normals", ErrUnsupportedFeature)
	}

	if data == nil {
		return nil, errors.New("draco: geometric normal prediction data is nil")
	}

	if len(data.flips) != len(corrections)/numComponents {
		return nil, errors.New("draco: geometric normal flip count mismatch")
	}

	values := make([]int32, len(corrections))
	predicted := make([]int32, 2)
	vector := make([]int32, 3)
	for entryID := 0; entryID < len(values)/numComponents; entryID++ {
		if err := checkContextEvery(decodeCtx, entryID); err != nil {
			return nil, err
		}

		corner := ctx.attrCtx.dataToCorner[entryID]
		if corner == topology.InvalidCorner {
			return nil, fmt.Errorf("draco: geometric normal entry %d has no representative corner", entryID)
		}

		if err := ctx.computePredictedNormal(corner, vector); err != nil {
			return nil, err
		}

		ctx.octa.CanonicalizeIntegerVector(vector)
		if data.flips[entryID] {
			vector[0], vector[1], vector[2] = -vector[0], -vector[1], -vector[2]
		}

		predicted[0], predicted[1] = ctx.octa.IntegerVectorToQuantizedOctahedralCoords([3]int32{vector[0], vector[1], vector[2]})
		offset := entryID * numComponents
		transform.ComputeOriginalValue(predicted, corrections[offset:offset+numComponents], values[offset:offset+numComponents])
	}

	return values, nil
}

func encodeGeometricNormalPredictionData(ctx context.Context, w *core.Writer, data *geometricNormalPredictionData) error {
	if data == nil {
		data = &geometricNormalPredictionData{}
	}

	encoder := &entropy.RansBitEncoder{}
	encoder.StartEncoding()
	for i, flip := range data.flips {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		encoder.EncodeBit(flip)
	}

	return encoder.EndEncoding(w)
}

func decodeGeometricNormalPredictionData(r *core.Reader, numEntries int, legacy bool) (*geometricNormalPredictionData, error) {
	data := &geometricNormalPredictionData{
		flips: make([]bool, numEntries),
	}
	decoder := &entropy.RansBitDecoder{}
	if err := decoder.StartDecodingVersioned(r, legacy); err != nil {
		return nil, fmt.Errorf("draco: geometric normal flips: %w", err)
	}

	for i := range data.flips {
		data.flips[i] = decoder.DecodeNextBit()
	}

	return data, nil
}

func (ctx *meshGeometricNormalPredictionContext) computePredictedNormal(startCorner int, out []int32) error {
	if len(out) < 3 {
		return errors.New("draco: predicted normal buffer too small")
	}

	center, err := ctx.positionForCorner(startCorner)
	if err != nil {
		return err
	}

	var normal [3]int64
	corner := startCorner
	firstPass := true
	for corner != topology.InvalidCorner {
		nextPos, err := ctx.positionForCorner(ctx.attrCtx.table.Next(corner))
		if err != nil {
			return err
		}

		prevPos, err := ctx.positionForCorner(ctx.attrCtx.table.Previous(corner))
		if err != nil {
			return err
		}

		deltaNext := subVec3(nextPos, center)
		deltaPrev := subVec3(prevPos, center)
		cross := crossVec3(deltaNext, deltaPrev)
		normal = addVec3(normal, cross)

		if firstPass {
			corner = ctx.attrCtx.table.SwingLeft(corner)
		} else {
			corner = ctx.attrCtx.table.SwingRight(corner)
		}

		if corner == startCorner {
			break
		}

		if corner == topology.InvalidCorner && firstPass {
			firstPass = false
			corner = ctx.attrCtx.table.SwingRight(startCorner)
		}
	}

	const upperBound = 1 << 29
	absSum := absInt64(normal[0]) + absInt64(normal[1]) + absInt64(normal[2])
	if absSum > upperBound {
		quotient := absSum / upperBound
		if quotient != 0 {
			normal[0] /= quotient
			normal[1] /= quotient
			normal[2] /= quotient
		}
	}

	out[0] = int32(normal[0])
	out[1] = int32(normal[1])
	out[2] = int32(normal[2])
	return nil
}

func (ctx *meshGeometricNormalPredictionContext) positionForCorner(corner int) ([3]int64, error) {
	var out [3]int64
	if corner == topology.InvalidCorner {
		return out, errors.New("draco: invalid prediction corner")
	}

	pointID := ctx.attrCtx.table.Vertex(corner)
	mappedEntry := int(ctx.position.mappedIndex(pointID))
	var values [3]int32
	if err := decodeInt32AttributeEntry(values[:], ctx.position, mappedEntry); err != nil {
		return out, fmt.Errorf("draco: geometric normal position lookup corner=%d point=%d mappedEntry=%d positionEntries=%d: %w", corner, pointID, mappedEntry, ctx.position.EntryCount(), err)
	}

	out[0], out[1], out[2] = int64(values[0]), int64(values[1]), int64(values[2])
	return out, nil
}

func crossVec3(a, b [3]int64) [3]int64 {
	return [3]int64{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}

	return v
}
