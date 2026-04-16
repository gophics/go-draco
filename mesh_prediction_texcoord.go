package draco

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/gophics/go-draco/internal/core"
	"github.com/gophics/go-draco/internal/entropy"
	"github.com/gophics/go-draco/internal/topology"
)

type texCoordPredictionData struct {
	orientations []bool
}

type meshTexCoordPredictionContext struct {
	attrCtx  *meshPredictionContext
	position *Attribute
}

func newMeshTexCoordPredictionContext(encodeCtx context.Context, mesh *Mesh, attr, position *Attribute, numEntries int, tableCache *meshPredictionTableCache) (*meshTexCoordPredictionContext, error) {
	if position == nil {
		return nil, fmt.Errorf("%w: texcoord prediction requires position attribute", ErrUnsupportedFeature)
	}

	if position.NumComponents != 3 {
		return nil, fmt.Errorf("%w: texcoord prediction requires 3-component positions", ErrUnsupportedFeature)
	}

	attrCtx, err := newMeshPredictionContext(encodeCtx, mesh, attr, numEntries, tableCache)
	if err != nil {
		return nil, err
	}

	return &meshTexCoordPredictionContext{
		attrCtx:  attrCtx,
		position: position,
	}, nil
}

func computeMeshTexCoordPortableCorrections(encodeCtx context.Context, ctx *meshTexCoordPredictionContext, values []int32, numComponents int, transform *wrapTransform) ([]int32, *texCoordPredictionData, error) {
	if numComponents != 2 {
		return nil, nil, fmt.Errorf("%w: texcoord portable prediction requires 2 components", ErrUnsupportedFeature)
	}

	corrections := make([]int32, len(values))
	data := &texCoordPredictionData{}
	predicted := make([]int32, numComponents)
	for entryID := len(values)/numComponents - 1; entryID >= 0; entryID-- {
		if err := checkContextEvery(encodeCtx, entryID); err != nil {
			return nil, nil, err
		}

		offset := entryID * numComponents
		if err := ctx.computePredictedValue(values, entryID, data, predicted, true); err != nil {
			return nil, nil, err
		}

		transform.ComputeCorrection(values[offset:offset+numComponents], predicted, corrections[offset:offset+numComponents])
	}

	return corrections, data, nil
}

func restoreMeshTexCoordPortableValues(decodeCtx context.Context, ctx *meshTexCoordPredictionContext, corrections []int32, numComponents int, transform *wrapTransform, data *texCoordPredictionData) ([]int32, error) {
	if numComponents != 2 {
		return nil, fmt.Errorf("%w: texcoord portable prediction requires 2 components", ErrUnsupportedFeature)
	}

	if data == nil {
		return nil, errors.New("draco: texcoord prediction data is nil")
	}

	values := make([]int32, len(corrections))
	predicted := make([]int32, numComponents)
	for entryID := 0; entryID < len(values)/numComponents; entryID++ {
		if err := checkContextEvery(decodeCtx, entryID); err != nil {
			return nil, err
		}

		offset := entryID * numComponents
		if err := ctx.computePredictedValue(values, entryID, data, predicted, false); err != nil {
			return nil, err
		}

		transform.ComputeOriginalValue(predicted, corrections[offset:offset+numComponents], values[offset:offset+numComponents])
	}

	return values, nil
}

func encodeTexCoordPredictionData(ctx context.Context, w *core.Writer, data *texCoordPredictionData) error {
	if data == nil {
		data = &texCoordPredictionData{}
	}

	if err := guardEncodeInt32Value(len(data.orientations), "texcoord orientation count"); err != nil {
		return err
	}

	if err := w.WriteInt32(int32(len(data.orientations))); err != nil {
		return err
	}

	encoder := &entropy.RansBitEncoder{}
	encoder.StartEncoding()
	lastOrientation := true
	for i, orientation := range data.orientations {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		encoder.EncodeBit(orientation == lastOrientation)
		lastOrientation = orientation
	}

	return encoder.EndEncoding(w)
}

func decodeTexCoordPredictionData(r *core.Reader, legacy bool) (*texCoordPredictionData, error) {
	numOrientations, err := r.ReadInt32()
	if err != nil {
		return nil, err
	}

	if numOrientations < 0 {
		return nil, fmt.Errorf("draco: invalid texcoord orientation count %d", numOrientations)
	}

	data := &texCoordPredictionData{
		orientations: make([]bool, numOrientations),
	}
	decoder := &entropy.RansBitDecoder{}
	if err := decoder.StartDecodingVersioned(r, legacy); err != nil {
		return nil, fmt.Errorf("draco: texcoord prediction orientations: %w", err)
	}

	lastOrientation := true
	for i := 0; i < int(numOrientations); i++ {
		if !decoder.DecodeNextBit() {
			lastOrientation = !lastOrientation
		}

		data.orientations[i] = lastOrientation
	}

	return data, nil
}

func (ctx *meshTexCoordPredictionContext) computePredictedValue(data []int32, dataID int, predictionData *texCoordPredictionData, out []int32, encoder bool) error {
	if ctx == nil || ctx.attrCtx == nil {
		return fmt.Errorf("%w: texcoord prediction context missing", ErrUnsupportedFeature)
	}

	if dataID < 0 || dataID >= len(ctx.attrCtx.dataToCorner) {
		return fmt.Errorf("draco: texcoord data id %d out of range", dataID)
	}

	corner := ctx.attrCtx.dataToCorner[dataID]
	if corner == topology.InvalidCorner {
		return fmt.Errorf("draco: texcoord data id %d has no representative corner", dataID)
	}

	nextCorner := ctx.attrCtx.table.Next(corner)
	prevCorner := ctx.attrCtx.table.Previous(corner)
	nextDataID := int(ctx.attrCtx.vertexToData[ctx.attrCtx.table.Vertex(nextCorner)])
	prevDataID := int(ctx.attrCtx.vertexToData[ctx.attrCtx.table.Vertex(prevCorner)])

	if prevDataID < dataID && nextDataID < dataID {
		nextUV := texCoordForEntryID(nextDataID, data)
		prevUV := texCoordForEntryID(prevDataID, data)
		if nextUV == prevUV {
			out[0] = int32(prevUV[0])
			out[1] = int32(prevUV[1])
			return nil
		}

		tipPos, err := ctx.positionForEntryID(dataID)
		if err != nil {
			return err
		}

		nextPos, err := ctx.positionForEntryID(nextDataID)
		if err != nil {
			return err
		}

		prevPos, err := ctx.positionForEntryID(prevDataID)
		if err != nil {
			return err
		}

		pn := subVec3(prevPos, nextPos)
		pnNormSquared := squaredNormVec3(pn)
		if pnNormSquared != 0 {
			cn := subVec3(tipPos, nextPos)
			cnDotPn := dotVec3Signed(cn, pn)
			pnUV := subVec2(prevUV, nextUV)
			xUV := addVec2(scaleVec2(nextUV, int64(pnNormSquared)), scaleVec2(pnUV, cnDotPn))
			xPos := addVec3(nextPos, divVec3(scaleVec3(pn, cnDotPn), int64(pnNormSquared)))
			cxNormSquared := squaredNormVec3(subVec3(tipPos, xPos))
			cxUV := [2]int64{pnUV[1], -pnUV[0]}
			normSquared := intSqrt(cxNormSquared * pnNormSquared)
			cxUV = scaleVec2(cxUV, int64(normSquared))

			divisor := int64(pnNormSquared)
			if encoder {
				actual := texCoordForEntryID(dataID, data)
				pred0 := divVec2(addVec2(xUV, cxUV), divisor)
				pred1 := divVec2(subVec2(xUV, cxUV), divisor)
				if squaredNormVec2(subVec2(actual, pred0)) < squaredNormVec2(subVec2(actual, pred1)) {
					predictionData.orientations = append(predictionData.orientations, true)
					out[0] = int32(pred0[0])
					out[1] = int32(pred0[1])
				} else {
					predictionData.orientations = append(predictionData.orientations, false)
					out[0] = int32(pred1[0])
					out[1] = int32(pred1[1])
				}

				return nil
			}

			if len(predictionData.orientations) == 0 {
				return errors.New("draco: texcoord prediction orientations truncated")
			}

			orientation := predictionData.orientations[len(predictionData.orientations)-1]
			predictionData.orientations = predictionData.orientations[:len(predictionData.orientations)-1]
			predicted := subVec2(xUV, cxUV)
			if orientation {
				predicted = addVec2(xUV, cxUV)
			}

			predicted = divVec2(predicted, divisor)
			out[0] = int32(predicted[0])
			out[1] = int32(predicted[1])
			return nil
		}
	}

	dataOffset := -1
	if prevDataID < dataID {
		dataOffset = prevDataID * 2
	}

	if nextDataID < dataID {
		dataOffset = nextDataID * 2
	} else if dataOffset < 0 {
		if dataID > 0 {
			dataOffset = (dataID - 1) * 2
		} else {
			out[0], out[1] = 0, 0
			return nil
		}
	}

	out[0] = data[dataOffset]
	out[1] = data[dataOffset+1]
	return nil
}

func (ctx *meshTexCoordPredictionContext) positionForEntryID(entryID int) ([3]int64, error) {
	var out [3]int64
	if entryID < 0 || entryID >= len(ctx.attrCtx.dataToCorner) {
		return out, fmt.Errorf("draco: position entry %d out of range", entryID)
	}

	corner := ctx.attrCtx.dataToCorner[entryID]
	if corner == topology.InvalidCorner {
		return out, fmt.Errorf("draco: position entry %d has no representative corner", entryID)
	}

	pointID := ctx.attrCtx.table.Vertex(corner)
	mappedEntry := int(ctx.position.mappedIndex(pointID))
	var values [3]int32
	if err := decodeInt32AttributeEntry(values[:], ctx.position, mappedEntry); err != nil {
		return out, fmt.Errorf("draco: texcoord prediction position lookup entry=%d point=%d mappedEntry=%d positionEntries=%d: %w", entryID, pointID, mappedEntry, ctx.position.EntryCount(), err)
	}

	out[0], out[1], out[2] = int64(values[0]), int64(values[1]), int64(values[2])
	return out, nil
}

func texCoordForEntryID(entryID int, data []int32) [2]int64 {
	offset := entryID * 2
	return [2]int64{int64(data[offset]), int64(data[offset+1])}
}

func intSqrt(value uint64) uint64 {
	if value == 0 {
		return 0
	}

	sqrt := uint64(1)
	act := value
	for act >= 2 {
		sqrt *= 2
		act /= 4
	}

	for {
		next := (sqrt + value/sqrt) / 2
		if next >= sqrt {
			break
		}

		sqrt = next
	}

	for sqrt*sqrt > value {
		sqrt--
	}

	return sqrt
}

func addVec2(a, b [2]int64) [2]int64 {
	return [2]int64{a[0] + b[0], a[1] + b[1]}
}

func subVec2(a, b [2]int64) [2]int64 {
	return [2]int64{a[0] - b[0], a[1] - b[1]}
}

func scaleVec2(a [2]int64, scalar int64) [2]int64 {
	return [2]int64{a[0] * scalar, a[1] * scalar}
}

func divVec2(a [2]int64, scalar int64) [2]int64 {
	if scalar == 0 {
		return [2]int64{}
	}

	return [2]int64{a[0] / scalar, a[1] / scalar}
}

func squaredNormVec2(a [2]int64) float64 {
	return math.Pow(float64(a[0]), 2) + math.Pow(float64(a[1]), 2)
}

func addVec3(a, b [3]int64) [3]int64 {
	return [3]int64{a[0] + b[0], a[1] + b[1], a[2] + b[2]}
}

func subVec3(a, b [3]int64) [3]int64 {
	return [3]int64{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
}

func scaleVec3(a [3]int64, scalar int64) [3]int64 {
	return [3]int64{a[0] * scalar, a[1] * scalar, a[2] * scalar}
}

func divVec3(a [3]int64, scalar int64) [3]int64 {
	if scalar == 0 {
		return [3]int64{}
	}

	return [3]int64{a[0] / scalar, a[1] / scalar, a[2] / scalar}
}

func dotVec3Signed(a, b [3]int64) int64 {
	return a[0]*b[0] + a[1]*b[1] + a[2]*b[2]
}

func squaredNormVec3(a [3]int64) uint64 {
	return uint64(a[0]*a[0] + a[1]*a[1] + a[2]*a[2])
}
