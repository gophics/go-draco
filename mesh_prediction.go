package draco

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"slices"

	"github.com/gophics/go-draco/internal/core"
	"github.com/gophics/go-draco/internal/entropy"
	"github.com/gophics/go-draco/internal/topology"
)

const maxConstrainedMultiParallelograms = 4

type meshPredictionContext struct {
	table        *topology.CornerTable
	vertexToData []int32
	dataToCorner []int
}

type meshPredictionTableCache struct {
	mesh         *Mesh
	table        *topology.CornerTable
	err          error
	ready        bool
	meshCtx      meshPredictionContext
	vertexToData []int32
	dataToCorner []int
}

func (c *meshPredictionTableCache) cornerTable(mesh *Mesh) (*topology.CornerTable, error) {
	if c == nil {
		return mesh.cornerTable()
	}

	if c.ready && c.mesh == mesh {
		return c.table, c.err
	}

	c.mesh = mesh
	c.table, c.err = mesh.cornerTable()
	c.ready = true
	return c.table, c.err
}

type constrainedMultiPredictionData struct {
	creaseEdges [maxConstrainedMultiParallelograms][]bool
}

type predictionError struct {
	numBits       int
	residualError int64
}

type parallelogramPredictor func(ctx *meshPredictionContext, dataEntryID int, values []int32, numComponents int, out []int32) bool

func (e predictionError) betterThan(other predictionError) bool {
	if e.numBits != other.numBits {
		return e.numBits < other.numBits
	}

	return e.residualError < other.residualError
}

func newMeshPredictionContext(encodeCtx context.Context, mesh *Mesh, attr *Attribute, numEntries int, tableCache *meshPredictionTableCache) (*meshPredictionContext, error) {
	if mesh == nil {
		return nil, fmt.Errorf("%w: mesh prediction requires mesh context", ErrUnsupportedFeature)
	}

	if attr == nil {
		return nil, fmt.Errorf("%w: mesh prediction requires attribute", ErrUnsupportedFeature)
	}

	table, err := tableCache.cornerTable(mesh)
	if err != nil {
		return nil, err
	}

	ctx := &meshPredictionContext{table: table}
	if tableCache != nil {
		tableCache.vertexToData = slices.Grow(tableCache.vertexToData[:0], mesh.PointCount())
		tableCache.vertexToData = tableCache.vertexToData[:mesh.PointCount()]
		tableCache.dataToCorner = slices.Grow(tableCache.dataToCorner[:0], numEntries)
		tableCache.dataToCorner = tableCache.dataToCorner[:numEntries]
		ctx = &tableCache.meshCtx
		ctx.table = table
		ctx.vertexToData = tableCache.vertexToData
		ctx.dataToCorner = tableCache.dataToCorner
	} else {
		ctx.vertexToData = make([]int32, mesh.PointCount())
		ctx.dataToCorner = make([]int, numEntries)
	}

	for i := range ctx.dataToCorner {
		ctx.dataToCorner[i] = topology.InvalidCorner
	}

	for pointID := 0; pointID < mesh.PointCount(); pointID++ {
		if err := checkContextEvery(encodeCtx, pointID); err != nil {
			return nil, err
		}

		ctx.vertexToData[pointID] = int32(attr.mappedIndex(pointID))
	}

	for pointID := 0; pointID < mesh.PointCount(); pointID++ {
		if err := checkContextEvery(encodeCtx, pointID); err != nil {
			return nil, err
		}

		dataID := int(ctx.vertexToData[pointID])
		if dataID < 0 || dataID >= numEntries || ctx.dataToCorner[dataID] != topology.InvalidCorner {
			continue
		}

		corner := table.LeftMostCorner(pointID)
		if corner != topology.InvalidCorner {
			ctx.dataToCorner[dataID] = corner
		}
	}

	return ctx, nil
}

func computeParallelogramPrediction(ctx *meshPredictionContext, dataEntryID int, corner int, values []int32, numComponents int, out []int32) bool {
	if ctx == nil || corner == topology.InvalidCorner {
		return false
	}

	oppositeCorner := ctx.table.Opposite(corner)
	if oppositeCorner == topology.InvalidCorner {
		return false
	}

	oppositeEntry := int(ctx.vertexToData[ctx.table.Vertex(oppositeCorner)])
	nextEntry := int(ctx.vertexToData[ctx.table.Vertex(ctx.table.Next(oppositeCorner))])
	prevEntry := int(ctx.vertexToData[ctx.table.Vertex(ctx.table.Previous(oppositeCorner))])
	if oppositeEntry < 0 || nextEntry < 0 || prevEntry < 0 {
		return false
	}

	if oppositeEntry >= dataEntryID || nextEntry >= dataEntryID || prevEntry >= dataEntryID {
		return false
	}

	oppositeOffset := oppositeEntry * numComponents
	nextOffset := nextEntry * numComponents
	prevOffset := prevEntry * numComponents
	for component := 0; component < numComponents; component++ {
		out[component] = values[nextOffset+component] + values[prevOffset+component] - values[oppositeOffset+component]
	}

	return true
}

func visitPredictionCorners(ctx *meshPredictionContext, dataEntryID int, visit func(corner int) bool) {
	if ctx == nil || dataEntryID < 0 || dataEntryID >= len(ctx.dataToCorner) {
		return
	}

	startCorner := ctx.dataToCorner[dataEntryID]
	if startCorner == topology.InvalidCorner {
		return
	}

	corner := startCorner
	firstPass := true
	for corner != topology.InvalidCorner {
		if !visit(corner) {
			return
		}

		if firstPass {
			corner = ctx.table.SwingLeft(corner)
		} else {
			corner = ctx.table.SwingRight(corner)
		}

		if corner == startCorner {
			break
		}

		if corner == topology.InvalidCorner && firstPass {
			firstPass = false
			corner = ctx.table.SwingRight(startCorner)
		}
	}
}

func collectParallelogramPredictions(ctx *meshPredictionContext, dataEntryID int, values []int32, numComponents int) [][]int32 {
	predictions := make([][]int32, 0, maxConstrainedMultiParallelograms)
	scratch := make([]int32, numComponents)
	visitPredictionCorners(ctx, dataEntryID, func(corner int) bool {
		if !computeParallelogramPrediction(ctx, dataEntryID, corner, values, numComponents, scratch) {
			return true
		}

		predicted := make([]int32, numComponents)
		copy(predicted, scratch)
		predictions = append(predictions, predicted)
		return len(predictions) < maxConstrainedMultiParallelograms
	})
	return predictions
}

func firstParallelogramPrediction(ctx *meshPredictionContext, dataEntryID int, values []int32, numComponents int, out []int32) bool {
	if ctx == nil || dataEntryID < 0 || dataEntryID >= len(ctx.dataToCorner) {
		return false
	}

	startCorner := ctx.dataToCorner[dataEntryID]
	if startCorner == topology.InvalidCorner {
		return false
	}

	corner := startCorner
	firstPass := true
	for corner != topology.InvalidCorner {
		if computeParallelogramPrediction(ctx, dataEntryID, corner, values, numComponents, out) {
			return true
		}

		if firstPass {
			corner = ctx.table.SwingLeft(corner)
		} else {
			corner = ctx.table.SwingRight(corner)
		}

		if corner == startCorner {
			break
		}

		if corner == topology.InvalidCorner && firstPass {
			firstPass = false
			corner = ctx.table.SwingRight(startCorner)
		}
	}

	return false
}

func averageParallelogramPrediction(ctx *meshPredictionContext, dataEntryID int, values []int32, numComponents int, out []int32) bool {
	var scratchStorage [16]int32
	scratch := scratchStorage[:]
	if numComponents > len(scratchStorage) {
		scratch = make([]int32, numComponents)
	} else {
		scratch = scratch[:numComponents]
	}

	clear(out)
	count := 0
	visitPredictionCorners(ctx, dataEntryID, func(corner int) bool {
		if !computeParallelogramPrediction(ctx, dataEntryID, corner, values, numComponents, scratch) {
			return true
		}

		for component := range out {
			out[component] += scratch[component]
		}

		count++
		return count < maxConstrainedMultiParallelograms
	})
	if count == 0 {
		return false
	}

	for component := range out {
		out[component] /= int32(count)
	}

	return true
}

func computeMeshParallelogramCorrections(encodeCtx context.Context, ctx *meshPredictionContext, values []int32, numComponents int, transform *wrapTransform) ([]int32, error) {
	return computeMeshParallelogramCorrectionsWith(encodeCtx, ctx, values, numComponents, transform, firstParallelogramPrediction)
}

func restoreMeshParallelogramValuesInto(decodeCtx context.Context, ctx *meshPredictionContext, corrections []int32, numComponents int, transform *wrapTransform, values []int32) ([]int32, error) {
	return restoreMeshParallelogramValuesWithInto(decodeCtx, ctx, corrections, numComponents, transform, firstParallelogramPrediction, values)
}

func computeMeshMultiParallelogramCorrections(encodeCtx context.Context, ctx *meshPredictionContext, values []int32, numComponents int, transform *wrapTransform) ([]int32, error) {
	return computeMeshParallelogramCorrectionsWith(encodeCtx, ctx, values, numComponents, transform, averageParallelogramPrediction)
}

func restoreMeshMultiParallelogramValuesInto(decodeCtx context.Context, ctx *meshPredictionContext, corrections []int32, numComponents int, transform *wrapTransform, values []int32) ([]int32, error) {
	return restoreMeshParallelogramValuesWithInto(decodeCtx, ctx, corrections, numComponents, transform, averageParallelogramPrediction, values)
}

func computeMeshParallelogramCorrectionsWith(encodeCtx context.Context, ctx *meshPredictionContext, values []int32, numComponents int, transform *wrapTransform, predict parallelogramPredictor) ([]int32, error) {
	corrections := make([]int32, len(values))
	zero := make([]int32, numComponents)
	predicted := make([]int32, numComponents)
	if len(values) == 0 {
		return corrections, nil
	}

	transform.ComputeCorrection(values[:numComponents], zero, corrections[:numComponents])
	for entryID := len(values)/numComponents - 1; entryID > 0; entryID-- {
		if err := checkContextEvery(encodeCtx, entryID); err != nil {
			return nil, err
		}

		offset := entryID * numComponents
		if !predict(ctx, entryID, values, numComponents, predicted) {
			copy(predicted, values[offset-numComponents:offset])
		}

		transform.ComputeCorrection(values[offset:offset+numComponents], predicted, corrections[offset:offset+numComponents])
	}

	return corrections, nil
}

func restoreMeshParallelogramValuesWithInto(decodeCtx context.Context, ctx *meshPredictionContext, corrections []int32, numComponents int, transform *wrapTransform, predict parallelogramPredictor, values []int32) ([]int32, error) {
	if len(values) < len(corrections) {
		values = make([]int32, len(corrections))
	} else {
		values = values[:len(corrections)]
	}

	var zeroStorage [16]int32
	zero := zeroStorage[:]
	var predictedStorage [16]int32
	predicted := predictedStorage[:]
	if numComponents > len(zeroStorage) {
		zero = make([]int32, numComponents)
		predicted = make([]int32, numComponents)
	} else {
		zero = zero[:numComponents]
		predicted = predicted[:numComponents]
	}

	if len(values) == 0 {
		return values, nil
	}

	transform.ComputeOriginalValue(zero, corrections[:numComponents], values[:numComponents])
	for entryID := 1; entryID < len(values)/numComponents; entryID++ {
		if err := checkContextEvery(decodeCtx, entryID); err != nil {
			return nil, err
		}

		offset := entryID * numComponents
		if !predict(ctx, entryID, values, numComponents, predicted) {
			copy(predicted, values[offset-numComponents:offset])
		}

		transform.ComputeOriginalValue(predicted, corrections[offset:offset+numComponents], values[offset:offset+numComponents])
	}

	return values, nil
}

func computeMeshConstrainedMultiParallelogramCorrections(encodeCtx context.Context, ctx *meshPredictionContext, values []int32, numComponents int, transform *wrapTransform) ([]int32, *constrainedMultiPredictionData, error) {
	corrections := make([]int32, len(values))
	data := &constrainedMultiPredictionData{}
	zero := make([]int32, numComponents)
	residuals := make([]int32, numComponents)
	if len(values) == 0 {
		return corrections, data, nil
	}

	transform.ComputeCorrection(values[:numComponents], zero, corrections[:numComponents])

	var totalUsed [maxConstrainedMultiParallelograms]int64
	var totalAvailable [maxConstrainedMultiParallelograms]int64

	for entryID := len(values)/numComponents - 1; entryID > 0; entryID-- {
		if err := checkContextEvery(encodeCtx, entryID); err != nil {
			return nil, nil, err
		}

		offset := entryID * numComponents
		predictions := collectParallelogramPredictions(ctx, entryID, values, numComponents)
		numPredictions := len(predictions)

		bestPredicted := make([]int32, numComponents)
		copy(bestPredicted, values[offset-numComponents:offset])
		bestMask := uint8(0)
		bestUsed := 0
		bestError := estimatePredictionError(bestPredicted, values[offset:offset+numComponents], residuals)

		if numPredictions > 0 {
			totalAvailable[numPredictions-1] += int64(numPredictions)
			bestError.numBits += computePredictionOverheadBits(totalUsed[numPredictions-1], totalAvailable[numPredictions-1])

			accumulated := make([]int32, numComponents)
			candidatePredicted := make([]int32, numComponents)
			for mask := 1; mask < (1 << numPredictions); mask++ {
				used := bits.OnesCount8(uint8(mask))
				for component := range accumulated {
					accumulated[component] = 0
				}

				for i := 0; i < numPredictions; i++ {
					if mask&(1<<i) == 0 {
						continue
					}

					for component := 0; component < numComponents; component++ {
						accumulated[component] += predictions[i][component]
					}
				}

				for component := 0; component < numComponents; component++ {
					candidatePredicted[component] = accumulated[component] / int32(used)
				}

				err := estimatePredictionError(candidatePredicted, values[offset:offset+numComponents], residuals)
				err.numBits += computePredictionOverheadBits(totalUsed[numPredictions-1]+int64(used), totalAvailable[numPredictions-1])
				if err.betterThan(bestError) {
					bestError = err
					bestMask = uint8(mask)
					bestUsed = used
					copy(bestPredicted, candidatePredicted)
				}
			}

			totalUsed[numPredictions-1] += int64(bestUsed)
			for i := 0; i < numPredictions; i++ {
				data.creaseEdges[numPredictions-1] = append(data.creaseEdges[numPredictions-1], bestMask&(1<<i) == 0)
			}
		}

		transform.ComputeCorrection(values[offset:offset+numComponents], bestPredicted, corrections[offset:offset+numComponents])
	}

	return corrections, data, nil
}

func restoreMeshConstrainedMultiParallelogramValues(decodeCtx context.Context, ctx *meshPredictionContext, corrections []int32, numComponents int, transform *wrapTransform, data *constrainedMultiPredictionData) ([]int32, error) {
	if data == nil {
		return nil, errors.New("draco: constrained multi-parallelogram data is nil")
	}

	values := make([]int32, len(corrections))
	zero := make([]int32, numComponents)
	predicted := make([]int32, numComponents)
	var flagPositions [maxConstrainedMultiParallelograms]int
	if len(values) == 0 {
		return values, nil
	}

	transform.ComputeOriginalValue(zero, corrections[:numComponents], values[:numComponents])
	for entryID := 1; entryID < len(values)/numComponents; entryID++ {
		if err := checkContextEvery(decodeCtx, entryID); err != nil {
			return nil, err
		}

		offset := entryID * numComponents
		predictions := collectParallelogramPredictions(ctx, entryID, values, numComponents)
		if len(predictions) == 0 {
			copy(predicted, values[offset-numComponents:offset])
			transform.ComputeOriginalValue(predicted, corrections[offset:offset+numComponents], values[offset:offset+numComponents])
			continue
		}

		context := len(predictions) - 1
		if context >= maxConstrainedMultiParallelograms {
			return nil, fmt.Errorf("draco: invalid constrained multi context %d", context)
		}

		for component := 0; component < numComponents; component++ {
			predicted[component] = 0
		}

		numUsed := 0
		for i := 0; i < len(predictions); i++ {
			flagPos := flagPositions[context]
			if flagPos >= len(data.creaseEdges[context]) {
				return nil, errors.New("draco: constrained multi-parallelogram flags truncated")
			}

			isCrease := data.creaseEdges[context][flagPos]
			flagPositions[context]++
			if isCrease {
				continue
			}

			numUsed++
			for component := 0; component < numComponents; component++ {
				predicted[component] += predictions[i][component]
			}
		}

		if numUsed == 0 {
			copy(predicted, values[offset-numComponents:offset])
		} else {
			for component := 0; component < numComponents; component++ {
				predicted[component] /= int32(numUsed)
			}
		}

		transform.ComputeOriginalValue(predicted, corrections[offset:offset+numComponents], values[offset:offset+numComponents])
	}

	return values, nil
}

func encodeConstrainedMultiPredictionData(w *core.Writer, data *constrainedMultiPredictionData) error {
	if data == nil {
		data = &constrainedMultiPredictionData{}
	}

	for context := 0; context < maxConstrainedMultiParallelograms; context++ {
		flags := data.creaseEdges[context]
		if err := core.EncodeVarUint32(w, uint32(len(flags))); err != nil {
			return err
		}

		if len(flags) == 0 {
			continue
		}

		encoder := &entropy.RansBitEncoder{}
		encoder.StartEncoding()
		numUsedParallelograms := context + 1
		for start := len(flags) - numUsedParallelograms; start >= 0; start -= numUsedParallelograms {
			for i := 0; i < numUsedParallelograms; i++ {
				encoder.EncodeBit(flags[start+i])
			}
		}

		if err := encoder.EndEncoding(w); err != nil {
			return err
		}
	}

	return nil
}

func decodeConstrainedMultiPredictionData(r *core.Reader, legacy bool) (*constrainedMultiPredictionData, error) {
	data := &constrainedMultiPredictionData{}
	for context := 0; context < maxConstrainedMultiParallelograms; context++ {
		numFlags, err := core.DecodeVarUint32(r)
		if err != nil {
			return nil, err
		}

		if numFlags == 0 {
			continue
		}

		flags := make([]bool, numFlags)
		decoder := &entropy.RansBitDecoder{}
		if err := decoder.StartDecodingVersioned(r, legacy); err != nil {
			return nil, fmt.Errorf("draco: constrained multi-parallelogram context %d flags: %w", context, err)
		}

		for i := range flags {
			flags[i] = decoder.DecodeNextBit()
		}

		data.creaseEdges[context] = flags
	}

	return data, nil
}

func estimatePredictionError(predicted, actual []int32, residuals []int32) predictionError {
	var err predictionError
	for i := range predicted {
		residual := predicted[i] - actual[i]
		residuals[i] = residual
		err.residualError += int64(absInt32(residual))
		bitsNeeded := bits.Len32(convertSignedIntToSymbol(residual))
		if bitsNeeded == 0 {
			bitsNeeded = 1
		}

		err.numBits += bitsNeeded
	}

	return err
}

func computePredictionOverheadBits(totalUsed, totalAvailable int64) int {
	if totalAvailable == 0 || totalUsed == 0 || totalUsed == totalAvailable {
		return 0
	}

	p := float64(totalUsed) / float64(totalAvailable)
	q := 1.0 - p
	entropyBits := 0.0
	if p > 0 {
		entropyBits -= p * math.Log2(p)
	}

	if q > 0 {
		entropyBits -= q * math.Log2(q)
	}

	return int(math.Ceil(float64(totalAvailable) * entropyBits))
}
