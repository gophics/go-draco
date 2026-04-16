package kdtree

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"slices"
	"unsafe"

	"github.com/gophics/go-draco/internal/core"
	"github.com/gophics/go-draco/internal/entropy"
)

const maxDecodeAllocationBytes uint64 = 512 << 20
const maxRetainedScratchBytes uint64 = 8 << 20

type kdTreeBitEncoder interface {
	StartEncoding()
	EncodeBit(bool)
	EncodeLeastSignificantBits32(int, uint32)
	EndEncoding(*core.Writer) error
}

type kdTreeDirectBitEncoder struct{ entropy.DirectBitEncoder }
type kdTreeDirectBitDecoder struct{ entropy.DirectBitDecoder }

func (d *kdTreeDirectBitDecoder) DecodeLeastSignificantBits32(nbits int) (uint32, error) {
	var value uint32
	if !d.DirectBitDecoder.DecodeLeastSignificantBits32(nbits, &value) {
		if err := d.Err(); err != nil {
			return 0, err
		}

		return 0, errors.New("draco: failed to decode direct bit payload")
	}

	return value, nil
}

type kdTreeRAnsBitEncoder struct{ entropy.RansBitEncoder }
type kdTreeRAnsBitDecoder struct{ entropy.RansBitDecoder }

func (d *kdTreeRAnsBitDecoder) DecodeLeastSignificantBits32(nbits int) (uint32, error) {
	var value uint32
	if !d.RansBitDecoder.DecodeLeastSignificantBits32(nbits, &value) {
		if err := d.Err(); err != nil {
			return 0, err
		}

		return 0, errors.New("draco: failed to decode rANS bit payload")
	}

	return value, nil
}

type kdTreeFoldedBitEncoder struct{ *entropy.FoldedBit32Encoder }
type kdTreeFoldedBitDecoder struct{ *entropy.FoldedBit32Decoder }

func (d *kdTreeFoldedBitDecoder) DecodeLeastSignificantBits32(nbits int) (uint32, error) {
	var value uint32
	if !d.FoldedBit32Decoder.DecodeLeastSignificantBits32(nbits, &value) {
		if err := d.Err(); err != nil {
			return 0, err
		}

		return 0, errors.New("draco: failed to decode folded bit payload")
	}

	return value, nil
}

type kdTreePolicy struct {
	numbers    kdTreeBitEncoder
	remaining  kdTreeBitEncoder
	axis       kdTreeBitEncoder
	half       kdTreeBitEncoder
	selectAxis bool
}

type kdTreeNumberDecoderKind uint8

const (
	kdTreeNumberDecoderDirect kdTreeNumberDecoderKind = iota
	kdTreeNumberDecoderRAns
	kdTreeNumberDecoderFolded
)

type kdTreeDecodePolicy struct {
	numbersKind   kdTreeNumberDecoderKind
	numbersDirect kdTreeDirectBitDecoder
	numbersRAns   kdTreeRAnsBitDecoder
	numbersFolded *kdTreeFoldedBitDecoder
	remaining     kdTreeDirectBitDecoder
	axis          kdTreeDirectBitDecoder
	half          kdTreeDirectBitDecoder
	selectAxis    bool
	axisBits      int
}

type DecodeScratch struct {
	baseStackBacking   []uint32
	levelsStackBacking []uint32
	baseStack          [][]uint32
	levelsStack        [][]uint32
	axes               []uint32
	row                []uint32
	stack              []kdTreeDecodingStatus
	policy             kdTreeDecodePolicy
}

// Reset clears transient input references while retaining reusable buffers.
func (s *DecodeScratch) Reset() {
	if s == nil {
		return
	}

	s.baseStackBacking = resetScratchSlice(s.baseStackBacking)
	s.levelsStackBacking = resetScratchSlice(s.levelsStackBacking)
	s.baseStack = resetScratchSlice(s.baseStack)
	s.levelsStack = resetScratchSlice(s.levelsStack)
	s.axes = resetScratchSlice(s.axes)
	s.row = resetScratchSlice(s.row)
	s.stack = resetScratchSlice(s.stack)
	s.policy.numbersDirect.Clear()
	s.policy.numbersRAns.Clear()
	if s.policy.numbersFolded != nil {
		s.policy.numbersFolded.Clear()
	}

	s.policy.remaining.Clear()
	s.policy.axis.Clear()
	s.policy.half.Clear()
	s.policy.selectAxis = false
}

func resetScratchSlice[T any](buf []T) []T {
	var zero T
	if uint64(cap(buf))*uint64(unsafe.Sizeof(zero)) > maxRetainedScratchBytes {
		return nil
	}

	return buf[:0]
}

type EncodeScratch struct {
	baseStackBacking   []uint32
	levelsStackBacking []uint32
	baseStack          [][]uint32
	levelsStack        [][]uint32
	axes               []uint32
	stack              []kdTreeEncodingStatus
	policy             kdTreePolicy
	numbersDirect      kdTreeDirectBitEncoder
	numbersRAns        kdTreeRAnsBitEncoder
	numbersFolded      *kdTreeFoldedBitEncoder
	remaining          kdTreeDirectBitEncoder
	axis               kdTreeDirectBitEncoder
	half               kdTreeDirectBitEncoder
}

// Reset drops oversized reusable buffers while keeping common fixture-sized
// scratch available for subsequent encodes.
func (s *EncodeScratch) Reset() {
	if s == nil {
		return
	}

	s.baseStackBacking = resetScratchSlice(s.baseStackBacking)
	s.levelsStackBacking = resetScratchSlice(s.levelsStackBacking)
	s.baseStack = resetScratchSlice(s.baseStack)
	s.levelsStack = resetScratchSlice(s.levelsStack)
	s.axes = resetScratchSlice(s.axes)
	s.stack = resetScratchSlice(s.stack)
	s.numbersDirect.Clear()
	s.numbersRAns.Clear()
	if s.numbersFolded != nil {
		s.numbersFolded.Clear()
	}

	s.remaining.Clear()
	s.axis.Clear()
	s.half.Clear()
}

func (s *EncodeScratch) prepare(dimension int) error {
	if s == nil {
		return nil
	}

	stackLen, valuesLen, err := kdTreeScratchSizes(dimension)
	if err != nil {
		return err
	}

	s.baseStackBacking = slices.Grow(s.baseStackBacking[:0], valuesLen)
	s.baseStackBacking = s.baseStackBacking[:valuesLen]
	clear(s.baseStackBacking)

	s.levelsStackBacking = slices.Grow(s.levelsStackBacking[:0], valuesLen)
	s.levelsStackBacking = s.levelsStackBacking[:valuesLen]
	clear(s.levelsStackBacking)

	s.baseStack = slices.Grow(s.baseStack[:0], stackLen)
	s.baseStack = s.baseStack[:stackLen]
	s.levelsStack = slices.Grow(s.levelsStack[:0], stackLen)
	s.levelsStack = s.levelsStack[:stackLen]
	for i := 0; i < stackLen; i++ {
		start := i * dimension
		end := start + dimension
		s.baseStack[i] = s.baseStackBacking[start:end]
		s.levelsStack[i] = s.levelsStackBacking[start:end]
	}

	s.axes = slices.Grow(s.axes[:0], dimension)
	s.axes = s.axes[:dimension]
	clear(s.axes)

	s.stack = slices.Grow(s.stack[:0], stackLen)
	s.stack = s.stack[:0]
	return nil
}

func newKDTreePolicy(level int, scratch *EncodeScratch) (*kdTreePolicy, error) {
	if level < 0 || level > 6 {
		return nil, fmt.Errorf("draco: unsupported kd-tree compression level %d", level)
	}

	var policy *kdTreePolicy
	if scratch != nil {
		policy = &scratch.policy
		*policy = kdTreePolicy{
			remaining: &scratch.remaining,
			axis:      &scratch.axis,
			half:      &scratch.half,
		}
	} else {
		policy = &kdTreePolicy{
			remaining: &kdTreeDirectBitEncoder{},
			axis:      &kdTreeDirectBitEncoder{},
			half:      &kdTreeDirectBitEncoder{},
		}
	}

	switch {
	case level >= 4:
		if scratch != nil {
			if scratch.numbersFolded == nil {
				scratch.numbersFolded = &kdTreeFoldedBitEncoder{entropy.NewFoldedRAnsBit32Encoder()}
			}

			policy.numbers = scratch.numbersFolded
		} else {
			policy.numbers = &kdTreeFoldedBitEncoder{entropy.NewFoldedRAnsBit32Encoder()}
		}
	case level >= 2:
		if scratch != nil {
			policy.numbers = &scratch.numbersRAns
		} else {
			policy.numbers = &kdTreeRAnsBitEncoder{}
		}
	default:
		if scratch != nil {
			policy.numbers = &scratch.numbersDirect
		} else {
			policy.numbers = &kdTreeDirectBitEncoder{}
		}
	}

	policy.selectAxis = level >= 6
	return policy, nil
}

func newKDTreeDecodePolicy(level int, scratch *DecodeScratch) (*kdTreeDecodePolicy, error) {
	if level < 0 || level > 6 {
		return nil, fmt.Errorf("draco: unsupported kd-tree compression level %d", level)
	}

	var policy *kdTreeDecodePolicy
	if scratch != nil {
		policy = &scratch.policy
		*policy = kdTreeDecodePolicy{
			numbersFolded: policy.numbersFolded,
		}
	} else {
		policy = &kdTreeDecodePolicy{}
	}

	switch {
	case level >= 4:
		policy.numbersKind = kdTreeNumberDecoderFolded
		if policy.numbersFolded == nil {
			policy.numbersFolded = &kdTreeFoldedBitDecoder{entropy.NewFoldedRAnsBit32Decoder()}
		}
	case level >= 2:
		policy.numbersKind = kdTreeNumberDecoderRAns
	default:
		policy.numbersKind = kdTreeNumberDecoderDirect
	}

	policy.selectAxis = level >= 6
	return policy, nil
}

type kdTreeEncodingStatus struct {
	begin    int
	end      int
	lastAxis uint32
	stackPos int
}

type kdTreeDecodingStatus struct {
	numRemainingPoints uint32
	lastAxis           uint32
	stackPos           int
}

func (p *kdTreeDecodePolicy) startDecoding(r *core.Reader) error {
	switch p.numbersKind {
	case kdTreeNumberDecoderFolded:
		if err := p.numbersFolded.StartDecoding(r); err != nil {
			return err
		}
	case kdTreeNumberDecoderRAns:
		if err := p.numbersRAns.StartDecoding(r); err != nil {
			return err
		}
	default:
		if err := p.numbersDirect.StartDecoding(r); err != nil {
			return err
		}
	}

	if err := p.remaining.StartDecoding(r); err != nil {
		return err
	}

	if err := p.axis.StartDecoding(r); err != nil {
		return err
	}

	return p.half.StartDecoding(r)
}

func (p *kdTreeDecodePolicy) endDecoding() bool {
	var numbersOK bool
	switch p.numbersKind {
	case kdTreeNumberDecoderFolded:
		numbersOK = p.numbersFolded.EndDecoding()
	case kdTreeNumberDecoderRAns:
		numbersOK = p.numbersRAns.EndDecoding()
	default:
		numbersOK = p.numbersDirect.EndDecoding()
	}

	return numbersOK && p.remaining.EndDecoding() && p.axis.EndDecoding() && p.half.EndDecoding()
}

func (p *kdTreeDecodePolicy) decodeNumbersBits(nbits int) (uint32, error) {
	switch p.numbersKind {
	case kdTreeNumberDecoderFolded:
		return p.numbersFolded.DecodeLeastSignificantBits32(nbits)
	case kdTreeNumberDecoderRAns:
		return p.numbersRAns.DecodeLeastSignificantBits32(nbits)
	default:
		return p.numbersDirect.DecodeLeastSignificantBits32(nbits)
	}
}

func (p *kdTreeDecodePolicy) decodeRemainingBits(nbits int) (uint32, error) {
	return p.remaining.DecodeLeastSignificantBits32(nbits)
}

func (p *kdTreeDecodePolicy) decodeAxisBits(nbits int) (uint32, error) {
	return p.axis.DecodeLeastSignificantBits32(nbits)
}

func (p *kdTreeDecodePolicy) decodeHalfBit() bool {
	return p.half.DecodeNextBit()
}

func (s *DecodeScratch) prepare(dimension int) error {
	stackLen, stackValues, err := kdTreeScratchSizes(dimension)
	if err != nil {
		return err
	}

	s.baseStackBacking = slices.Grow(s.baseStackBacking[:0], stackValues)
	s.baseStackBacking = s.baseStackBacking[:stackValues]
	clear(s.baseStackBacking)

	s.levelsStackBacking = slices.Grow(s.levelsStackBacking[:0], stackValues)
	s.levelsStackBacking = s.levelsStackBacking[:stackValues]
	clear(s.levelsStackBacking)

	s.baseStack = slices.Grow(s.baseStack[:0], stackLen)
	s.baseStack = s.baseStack[:stackLen]
	s.levelsStack = slices.Grow(s.levelsStack[:0], stackLen)
	s.levelsStack = s.levelsStack[:stackLen]
	for i := 0; i < stackLen; i++ {
		start := i * dimension
		end := start + dimension
		s.baseStack[i] = s.baseStackBacking[start:end]
		s.levelsStack[i] = s.levelsStackBacking[start:end]
	}

	s.axes = slices.Grow(s.axes[:0], dimension)
	s.axes = s.axes[:dimension]
	s.row = slices.Grow(s.row[:0], dimension)
	s.row = s.row[:dimension]

	s.stack = slices.Grow(s.stack[:0], stackLen)
	s.stack = s.stack[:0]
	return nil
}

func kdTreeScratchSizes(dimension int) (int, int, error) {
	if dimension <= 0 {
		return 0, 0, fmt.Errorf("draco: invalid kd-tree dimension %d", dimension)
	}

	stackLen := uint64(dimension)*32 + 1
	if stackLen > uint64(math.MaxInt) {
		return 0, 0, fmt.Errorf("draco: kd-tree stack length %d exceeds platform int limit", stackLen)
	}

	stackValues := stackLen * uint64(dimension)
	if stackValues > uint64(math.MaxInt) {
		return 0, 0, fmt.Errorf("draco: kd-tree scratch size %d exceeds platform int limit", stackValues)
	}

	return int(stackLen), int(stackValues), nil
}

func guardKDTreeScratchAllocation(dimension int, statusSize uintptr, what string) error {
	stackLen, stackValues, err := kdTreeScratchSizes(dimension)
	if err != nil {
		return err
	}

	stackArrayBytes := uint64(stackValues) * 4 * 2
	if stackArrayBytes > maxDecodeAllocationBytes {
		return fmt.Errorf("draco: kd-tree %s stack allocation of %d bytes exceeds %d-byte limit", what, stackArrayBytes, maxDecodeAllocationBytes)
	}

	stackHeaderBytes := uint64(stackLen)*uint64(unsafe.Sizeof([]uint32{}))*2 + uint64(stackLen)*uint64(statusSize)
	if stackHeaderBytes > maxDecodeAllocationBytes {
		return fmt.Errorf("draco: kd-tree %s stack header allocation of %d bytes exceeds %d-byte limit", what, stackHeaderBytes, maxDecodeAllocationBytes)
	}

	return nil
}

func EncodePointsContext(ctx context.Context, w *core.Writer, points [][]uint32, dimension int, bitLength uint32, compressionLevel int, scratch *EncodeScratch) error {
	policy, err := newKDTreePolicy(compressionLevel, scratch)
	if err != nil {
		return err
	}

	if err := w.WriteUint32(bitLength); err != nil {
		return err
	}

	if err := w.WriteUint32(uint32(len(points))); err != nil {
		return err
	}

	if len(points) == 0 {
		return nil
	}

	policy.numbers.StartEncoding()
	policy.remaining.StartEncoding()
	policy.axis.StartEncoding()
	policy.half.StartEncoding()
	if err := encodeDynamicIntegerPointsKDTreeInternal(ctx, policy, points, dimension, bitLength, scratch); err != nil {
		return err
	}

	if err := policy.numbers.EndEncoding(w); err != nil {
		return err
	}

	if err := policy.remaining.EndEncoding(w); err != nil {
		return err
	}

	if err := policy.axis.EndEncoding(w); err != nil {
		return err
	}

	return policy.half.EndEncoding(w)
}

func encodeDynamicIntegerPointsKDTreeInternal(ctx context.Context, policy *kdTreePolicy, points [][]uint32, dimension int, bitLength uint32, scratch *EncodeScratch) error {
	if err := guardKDTreeScratchAllocation(dimension, unsafe.Sizeof(kdTreeEncodingStatus{}), "encode"); err != nil {
		return err
	}

	var baseStack [][]uint32
	var levelsStack [][]uint32
	var axes []uint32
	var stack []kdTreeEncodingStatus
	if scratch != nil {
		if err := scratch.prepare(dimension); err != nil {
			return err
		}

		baseStack = scratch.baseStack
		levelsStack = scratch.levelsStack
		axes = scratch.axes
		stack = scratch.stack[:0]
		stack = append(stack, kdTreeEncodingStatus{begin: 0, end: len(points), lastAxis: 0, stackPos: 0})
	} else {
		stackLen, _, err := kdTreeScratchSizes(dimension)
		if err != nil {
			return err
		}

		baseStack = make([][]uint32, stackLen)
		levelsStack = make([][]uint32, stackLen)
		for i := range baseStack {
			baseStack[i] = make([]uint32, dimension)
			levelsStack[i] = make([]uint32, dimension)
		}

		axes = make([]uint32, dimension)
		stack = []kdTreeEncodingStatus{{begin: 0, end: len(points), lastAxis: 0, stackPos: 0}}
	}

	for len(stack) > 0 {
		if ctx != nil && len(stack)%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}

		status := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		oldBase := baseStack[status.stackPos]
		levels := levelsStack[status.stackPos]
		axis := kdTreeGetAndEncodeAxis(policy, points[status.begin:status.end], oldBase, levels, status.lastAxis, bitLength)
		level := levels[axis]
		numRemainingPoints := status.end - status.begin
		if bitLength-level == 0 {
			continue
		}

		if numRemainingPoints <= 2 {
			axes[0] = axis
			for i := 1; i < dimension; i++ {
				axes[i] = incrementMod(axes[i-1], uint32(dimension))
			}

			for i := 0; i < numRemainingPoints; i++ {
				if ctx != nil && i%1024 == 0 {
					if err := ctx.Err(); err != nil {
						return err
					}
				}

				point := points[status.begin+i]
				for j := 0; j < dimension; j++ {
					numRemainingBits := bitLength - levels[axes[j]]
					if numRemainingBits > 0 {
						policy.remaining.EncodeLeastSignificantBits32(int(numRemainingBits), point[axes[j]])
					}
				}
			}

			continue
		}

		numRemainingBits := bitLength - level
		modifier := uint32(1) << (numRemainingBits - 1)
		copy(baseStack[status.stackPos+1], oldBase)
		newBase := baseStack[status.stackPos+1]
		newBase[axis] += modifier

		split := partitionKDTreePoints(points, status.begin, status.end, axis, newBase[axis])
		requiredBits := mostSignificantBit(uint32(numRemainingPoints))
		firstHalf := uint32(split - status.begin)
		secondHalf := uint32(status.end - split)
		left := firstHalf < secondHalf
		if firstHalf != secondHalf {
			policy.half.EncodeBit(left)
		}

		if left {
			policy.numbers.EncodeLeastSignificantBits32(requiredBits, uint32(numRemainingPoints/2)-firstHalf)
		} else {
			policy.numbers.EncodeLeastSignificantBits32(requiredBits, uint32(numRemainingPoints/2)-secondHalf)
		}

		levelsStack[status.stackPos][axis]++
		copy(levelsStack[status.stackPos+1], levelsStack[status.stackPos])
		if split != status.begin {
			stack = append(stack, kdTreeEncodingStatus{begin: status.begin, end: split, lastAxis: axis, stackPos: status.stackPos})
		}

		if split != status.end {
			stack = append(stack, kdTreeEncodingStatus{begin: split, end: status.end, lastAxis: axis, stackPos: status.stackPos + 1})
		}
	}

	return nil
}

func DecodePointsContext(ctx context.Context, r *core.Reader, dimension int, compressionLevel int) ([][]uint32, error) {
	var points [][]uint32
	err := DecodePointsToRowsContext(ctx, r, dimension, compressionLevel, nil, func(row []uint32) error {
		points = append(points, append([]uint32(nil), row...))
		return nil
	})
	if err != nil {
		return nil, err
	}

	return points, nil
}

func DecodePointsToRowsContext(ctx context.Context, r *core.Reader, dimension int, compressionLevel int, scratch *DecodeScratch, emit func([]uint32) error) error {
	if dimension <= 0 {
		return fmt.Errorf("draco: invalid kd-tree dimension %d", dimension)
	}

	if emit == nil {
		return errors.New("draco: kd-tree row sink is nil")
	}

	policy, err := newKDTreeDecodePolicy(compressionLevel, scratch)
	if err != nil {
		return err
	}

	bitLength, err := r.ReadUint32()
	if err != nil {
		return err
	}

	if bitLength > 32 {
		return fmt.Errorf("draco: invalid kd-tree bit length %d", bitLength)
	}

	numPoints, err := r.ReadUint32()
	if err != nil {
		return err
	}

	if numPoints == 0 {
		return nil
	}

	if err := policy.startDecoding(r); err != nil {
		return err
	}

	policy.axisBits = axisBitCount(uint32(dimension))
	err = decodeDynamicIntegerPointsKDTreeInternal(ctx, policy, dimension, bitLength, numPoints, scratch, emit)
	if err != nil {
		return err
	}

	if !policy.endDecoding() {
		return errors.New("draco: kd-tree bit decoder did not terminate cleanly")
	}

	return nil
}

func decodeDynamicIntegerPointsKDTreeInternal(ctx context.Context, policy *kdTreeDecodePolicy, dimension int, bitLength, numPoints uint32, scratch *DecodeScratch, emit func([]uint32) error) error {
	if err := guardKDTreeScratchAllocation(dimension, unsafe.Sizeof(kdTreeDecodingStatus{}), "decode"); err != nil {
		return err
	}

	pointValueBytes := uint64(numPoints) * uint64(dimension) * 4
	if pointValueBytes > maxDecodeAllocationBytes {
		return fmt.Errorf("draco: kd-tree decoded point value allocation of %d bytes exceeds %d-byte limit", pointValueBytes, maxDecodeAllocationBytes)
	}

	if scratch == nil {
		scratch = &DecodeScratch{}
	}

	if err := scratch.prepare(dimension); err != nil {
		return err
	}

	baseStack := scratch.baseStack
	levelsStack := scratch.levelsStack
	axes := scratch.axes
	row := scratch.row
	stack := scratch.stack[:0]
	stack = append(stack, kdTreeDecodingStatus{numRemainingPoints: numPoints, lastAxis: 0, stackPos: 0})
	iterations := 0
	for len(stack) > 0 {
		if ctx != nil && iterations%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}

		iterations++
		status := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		oldBase := baseStack[status.stackPos]
		levels := levelsStack[status.stackPos]
		if status.numRemainingPoints > numPoints {
			return errors.New("draco: invalid kd-tree point count")
		}

		axis, err := kdTreeGetAxis(policy, status.numRemainingPoints, levels, status.lastAxis, uint32(dimension))
		if err != nil {
			return err
		}

		level := levels[axis]
		if bitLength-level == 0 {
			for i := uint32(0); i < status.numRemainingPoints; i++ {
				if ctx != nil && i%1024 == 0 {
					if err := ctx.Err(); err != nil {
						return err
					}
				}

				copy(row, oldBase)
				if err := emit(row); err != nil {
					return err
				}
			}

			continue
		}

		if status.numRemainingPoints <= 2 {
			axes[0] = axis
			for i := 1; i < dimension; i++ {
				axes[i] = incrementMod(axes[i-1], uint32(dimension))
			}

			for i := uint32(0); i < status.numRemainingPoints; i++ {
				for j := 0; j < dimension; j++ {
					row[axes[j]] = 0
					numRemainingBits := bitLength - levels[axes[j]]
					if numRemainingBits > 0 {
						value, err := policy.decodeRemainingBits(int(numRemainingBits))
						if err != nil {
							return err
						}

						row[axes[j]] = value
					} else {
						row[axes[j]] = 0
					}

					row[axes[j]] = oldBase[axes[j]] | row[axes[j]]
				}

				if err := emit(row); err != nil {
					return err
				}
			}

			continue
		}

		numRemainingBits := bitLength - level
		modifier := uint32(1) << (numRemainingBits - 1)
		copy(baseStack[status.stackPos+1], oldBase)
		baseStack[status.stackPos+1][axis] += modifier

		incomingBits := mostSignificantBit(status.numRemainingPoints)
		number, err := policy.decodeNumbersBits(incomingBits)
		if err != nil {
			return err
		}

		firstHalf := status.numRemainingPoints / 2
		if firstHalf < number {
			return errors.New("draco: invalid kd-tree partition payload")
		}

		firstHalf -= number
		secondHalf := status.numRemainingPoints - firstHalf
		if firstHalf != secondHalf && !policy.decodeHalfBit() {
			firstHalf, secondHalf = secondHalf, firstHalf
		}

		levelsStack[status.stackPos][axis]++
		copy(levelsStack[status.stackPos+1], levelsStack[status.stackPos])
		if firstHalf > 0 {
			stack = append(stack, kdTreeDecodingStatus{numRemainingPoints: firstHalf, lastAxis: axis, stackPos: status.stackPos})
		}

		if secondHalf > 0 {
			stack = append(stack, kdTreeDecodingStatus{numRemainingPoints: secondHalf, lastAxis: axis, stackPos: status.stackPos + 1})
		}
	}

	return nil
}

func kdTreeGetAndEncodeAxis(policy *kdTreePolicy, points [][]uint32, oldBase, levels []uint32, lastAxis, bitLength uint32) uint32 {
	dimension := uint32(len(levels))
	if !policy.selectAxis {
		return incrementMod(lastAxis, dimension)
	}

	var bestAxis uint32
	if len(points) < 64 {
		for axis := uint32(1); axis < dimension; axis++ {
			if levels[bestAxis] > levels[axis] {
				bestAxis = axis
			}
		}

		return bestAxis
	}

	maxValue := uint32(0)
	for axis := uint32(0); axis < dimension; axis++ {
		numRemainingBits := bitLength - levels[axis]
		if numRemainingBits == 0 {
			continue
		}

		split := oldBase[axis] + (uint32(1) << (numRemainingBits - 1))
		var below uint32
		for _, point := range points {
			if point[axis] < split {
				below++
			}
		}

		deviation := below
		if other := uint32(len(points)) - below; other > deviation {
			deviation = other
		}

		if deviation > maxValue {
			maxValue = deviation
			bestAxis = axis
		}
	}

	policy.axis.EncodeLeastSignificantBits32(axisBitCount(dimension), bestAxis)
	return bestAxis
}

func kdTreeGetAxis(policy *kdTreeDecodePolicy, numRemainingPoints uint32, levels []uint32, lastAxis, dimension uint32) (uint32, error) {
	if !policy.selectAxis {
		return incrementMod(lastAxis, dimension), nil
	}

	var bestAxis uint32
	if numRemainingPoints < 64 {
		for axis := uint32(1); axis < dimension; axis++ {
			if levels[bestAxis] > levels[axis] {
				bestAxis = axis
			}
		}

		return bestAxis, nil
	}

	value, err := policy.decodeAxisBits(policy.axisBits)
	if err != nil {
		return 0, err
	}

	bestAxis = value
	if bestAxis >= dimension {
		return 0, fmt.Errorf("draco: invalid kd-tree axis %d", bestAxis)
	}

	return bestAxis, nil
}

func axisBitCount(dimension uint32) int {
	if dimension <= 16 {
		return 4
	}

	return bits.Len32(dimension - 1)
}

func partitionKDTreePoints(points [][]uint32, begin, end int, axis uint32, split uint32) int {
	i := begin
	j := end
	for {
		for i < end && points[i][axis] < split {
			i++
		}

		for j > begin && points[j-1][axis] >= split {
			j--
		}

		if i >= j {
			return i
		}

		points[i], points[j-1] = points[j-1], points[i]
		i++
		j--
	}
}

func incrementMod(value, mod uint32) uint32 {
	value++
	if value >= mod {
		return 0
	}

	return value
}

func mostSignificantBit(value uint32) int {
	if value == 0 {
		return 0
	}

	return bits.Len32(value) - 1
}
