package draco

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/gophics/go-draco/internal/bitstream"
	"github.com/gophics/go-draco/internal/core"
	"github.com/gophics/go-draco/internal/entropy"
	"github.com/gophics/go-draco/internal/topology"
	md "github.com/gophics/go-draco/metadata"
)

type edgebreakerPendingAttribute struct {
	attrType       AttributeType
	dataType       DataType
	numComponents  int
	normalized     bool
	uniqueID       uint32
	sequentialType uint8
}

type edgebreakerAttributeDecoder struct {
	dataID          int
	decoderType     uint8
	traversalMethod uint8
	pending         []edgebreakerPendingAttribute
	sequenceCorners []int
	vertexToEncoded []int
	numValues       int
	states          []*sequentialDecodedAttribute
}

type edgebreakerDecodeScratch struct {
	activeCorners        []int
	topologySplitCorners []int
	topologySplitSet     []bool
	attrConnectivity     []*edgebreakerAttributeConnectivity
	invalidVertices      []int
	remap                []int
	remappedHoles        []bool
	isVertHole           []bool
	cornerToPoint        []int
	pointToCorner        []int
	faces                []Face
	mutableTable         *edgebreakerMutableCornerTable
	predictionCornerVert []int
	predictionVertCorner []int
	groupPtrs            []*edgebreakerAttributeDecoder
	attributeGroups      []edgebreakerAttributeDecoder
	sequenceCorners      []int
	vertexToEncoded      []int
	sequenceVisitedFaces []bool
	sequenceVisitedVerts []bool
	sequenceStack        []int
	predictionDegree     []int
	predictionStacks     [3][]int
	predictionTable      *topology.CornerTable
	portableDataBuffers  [][]byte
	portableMapBuffers   [][]uint32
	portableDataUsed     int
	portableMapUsed      int
}

func (s *edgebreakerDecodeScratch) reset() {
	if s == nil {
		return
	}

	s.activeCorners = resetScratchSlice(s.activeCorners)
	s.topologySplitCorners = resetScratchSlice(s.topologySplitCorners)
	s.topologySplitSet = resetScratchSlice(s.topologySplitSet)
	s.invalidVertices = resetScratchSlice(s.invalidVertices)
	s.remap = resetScratchSlice(s.remap)
	s.remappedHoles = resetScratchSlice(s.remappedHoles)
	s.isVertHole = resetScratchSlice(s.isVertHole)
	s.cornerToPoint = resetScratchSlice(s.cornerToPoint)
	s.pointToCorner = resetScratchSlice(s.pointToCorner)
	s.faces = resetScratchSlice(s.faces)
	s.predictionCornerVert = resetScratchSlice(s.predictionCornerVert)
	s.predictionVertCorner = resetScratchSlice(s.predictionVertCorner)

	// Connectivity objects must be reset before the owner list is shortened.
	for _, connectivity := range s.attrConnectivity {
		if connectivity == nil {
			continue
		}

		connectivity.seamEdges = resetScratchSlice(connectivity.seamEdges)
		connectivity.vertexOnSeam = resetScratchSlice(connectivity.vertexOnSeam)
		connectivity.cornerToVertex = resetScratchSlice(connectivity.cornerToVertex)
		connectivity.leftMostCorners = resetScratchSlice(connectivity.leftMostCorners)
		connectivity.numVertices = 0
		connectivity.noInteriorSeams = true
	}

	s.attrConnectivity = resetScratchSlice(s.attrConnectivity)

	for i := range s.attributeGroups {
		clear(s.attributeGroups[i].states)
		s.attributeGroups[i].pending = resetScratchSlice(s.attributeGroups[i].pending)
		s.attributeGroups[i].states = resetScratchSlice(s.attributeGroups[i].states)
		s.attributeGroups[i].sequenceCorners = resetScratchSlice(s.attributeGroups[i].sequenceCorners)
		s.attributeGroups[i].vertexToEncoded = resetScratchSlice(s.attributeGroups[i].vertexToEncoded)
	}

	s.groupPtrs = resetScratchSliceClear(s.groupPtrs)
	s.attributeGroups = resetScratchSlice(s.attributeGroups)

	s.sequenceCorners = resetScratchSlice(s.sequenceCorners)
	s.vertexToEncoded = resetScratchSlice(s.vertexToEncoded)
	s.sequenceVisitedFaces = resetScratchSlice(s.sequenceVisitedFaces)
	s.sequenceVisitedVerts = resetScratchSlice(s.sequenceVisitedVerts)
	s.sequenceStack = resetScratchSlice(s.sequenceStack)

	s.predictionDegree = resetScratchSlice(s.predictionDegree)
	for i := range s.predictionStacks {
		s.predictionStacks[i] = resetScratchSlice(s.predictionStacks[i])
	}

	for i := range s.portableDataBuffers {
		s.portableDataBuffers[i] = resetScratchSlice(s.portableDataBuffers[i])
	}

	for i := range s.portableMapBuffers {
		s.portableMapBuffers[i] = resetScratchSlice(s.portableMapBuffers[i])
	}

	s.portableDataBuffers = resetScratchSlice(s.portableDataBuffers)
	s.portableMapBuffers = resetScratchSlice(s.portableMapBuffers)
	s.portableDataUsed = 0
	s.portableMapUsed = 0
}

func (s *edgebreakerDecodeScratch) portableDataBuffer(size int) []byte {
	if s == nil {
		return make([]byte, size)
	}

	index := s.portableDataUsed
	s.portableDataUsed++
	s.portableDataBuffers = slices.Grow(s.portableDataBuffers, index+1)
	s.portableDataBuffers = s.portableDataBuffers[:index+1]
	buffer := slices.Grow(s.portableDataBuffers[index][:0], size)
	buffer = buffer[:size]
	clear(buffer)
	s.portableDataBuffers[index] = buffer
	return buffer
}

func (s *edgebreakerDecodeScratch) portableMapBuffer(size int) []uint32 {
	if s == nil {
		return make([]uint32, size)
	}

	index := s.portableMapUsed
	s.portableMapUsed++
	s.portableMapBuffers = slices.Grow(s.portableMapBuffers, index+1)
	s.portableMapBuffers = s.portableMapBuffers[:index+1]
	buffer := slices.Grow(s.portableMapBuffers[index][:0], size)
	buffer = buffer[:size]
	clear(buffer)
	s.portableMapBuffers[index] = buffer
	return buffer
}

type edgebreakerTraversal interface {
	DecodeSymbol() (uint32, error)
	DecodeStartFaceConfiguration() (bool, error)
	DecodeAttributeSeam(attribute int) (bool, error)
	NewActiveCornerReached(corner int)
	NewInteriorFaceReached(corner int)
	MergeVertices(dest, source int)
}

type edgebreakerStandardTraversalDecoder struct {
	header                 bitstream.Header
	symbolBits             core.BitReader
	legacyStartFaceBits    core.BitReader
	hasLegacyStartFaceBits bool
	startFaceDecoder       entropy.RansBitDecoder
	attributeSeamDecoders  []entropy.RansBitDecoder
}

func newEdgebreakerStandardTraversalDecoder(header bitstream.Header, r *core.Reader, numAttributeData int) (*edgebreakerStandardTraversalDecoder, error) {
	symbolPayload, err := readSizedEdgebreakerBitPayload(r, header)
	if err != nil {
		return nil, err
	}

	legacy := header.VersionMajor < 2 || (header.VersionMajor == 2 && header.VersionMinor < 2)
	out := &edgebreakerStandardTraversalDecoder{
		header:     header,
		symbolBits: core.NewBitReaderValue(symbolPayload),
	}
	if legacy {
		startFacePayload, err := readSizedEdgebreakerBitPayload(r, header)
		if err != nil {
			return nil, err
		}

		out.legacyStartFaceBits = core.NewBitReaderValue(startFacePayload)
		out.hasLegacyStartFaceBits = true
	} else {
		if err := out.startFaceDecoder.StartDecoding(r); err != nil {
			return nil, fmt.Errorf("draco: edgebreaker start-face stream: %w", err)
		}
	}

	if numAttributeData > 0 {
		out.attributeSeamDecoders = make([]entropy.RansBitDecoder, numAttributeData)
		for i := range out.attributeSeamDecoders {
			if err := out.attributeSeamDecoders[i].StartDecodingVersioned(r, legacy); err != nil {
				return nil, fmt.Errorf("draco: edgebreaker seam stream %d: %w", i, err)
			}
		}
	}

	return out, nil
}

func (d *edgebreakerStandardTraversalDecoder) DecodeSymbol() (uint32, error) {
	symbol, ok := d.symbolBits.ReadBitLSB()
	if !ok {
		return 0, errors.New("draco: truncated edgebreaker symbol stream")
	}

	if symbol == topologyC {
		return symbol, nil
	}

	suffix, ok := d.symbolBits.ReadBitsLSB(2)
	if !ok {
		return 0, errors.New("draco: truncated edgebreaker symbol suffix")
	}

	return symbol | (suffix << 1), nil
}

func (d *edgebreakerStandardTraversalDecoder) DecodeStartFaceConfiguration() (bool, error) {
	if d.hasLegacyStartFaceBits {
		value, ok := d.legacyStartFaceBits.ReadBitLSB()
		if !ok {
			return false, errors.New("draco: truncated edgebreaker start-face stream")
		}

		return value != 0, nil
	}

	return d.startFaceDecoder.DecodeNextBit(), nil
}

func (d *edgebreakerStandardTraversalDecoder) DecodeAttributeSeam(attribute int) (bool, error) {
	if attribute < 0 || attribute >= len(d.attributeSeamDecoders) {
		return false, nil
	}

	return d.attributeSeamDecoders[attribute].DecodeNextBit(), nil
}

func (d *edgebreakerStandardTraversalDecoder) NewActiveCornerReached(corner int) {}

func (d *edgebreakerStandardTraversalDecoder) NewInteriorFaceReached(corner int) {}

func (d *edgebreakerStandardTraversalDecoder) MergeVertices(dest, source int) {}

type edgebreakerValenceTraversalDecoder struct {
	*edgebreakerStandardTraversalDecoder
	table           *edgebreakerMutableCornerTable
	vertexValences  []int
	contextSymbols  [][]uint32
	contextCounters []int
	activeContext   int
	lastSymbol      uint32
}

func newEdgebreakerValenceTraversalDecoder(header bitstream.Header, r *core.Reader, numAttributeData, numVertices int, scratch *entropy.DecodeScratch) (*edgebreakerValenceTraversalDecoder, error) {
	out := &edgebreakerValenceTraversalDecoder{
		edgebreakerStandardTraversalDecoder: &edgebreakerStandardTraversalDecoder{header: header},
		vertexValences:                      make([]int, numVertices),
		activeContext:                       -1,
		contextSymbols:                      make([][]uint32, maxValence-minValence+1),
		contextCounters:                     make([]int, maxValence-minValence+1),
	}
	legacy := header.VersionMajor < 2 || (header.VersionMajor == 2 && header.VersionMinor < 2)
	if legacy {
		base, err := newEdgebreakerStandardTraversalDecoder(header, r, numAttributeData)
		if err != nil {
			return nil, err
		}

		out.edgebreakerStandardTraversalDecoder = base
		splitCount, err := readVersionedEdgebreakerVarUint32(r, header)
		if err != nil {
			return nil, err
		}

		if int(splitCount) >= numVertices {
			return nil, fmt.Errorf("%w: invalid valence split symbol count %d", ErrInvalidGeometry, splitCount)
		}

		mode, err := r.ReadInt8()
		if err != nil {
			return nil, err
		}

		if mode != 0 {
			return nil, fmt.Errorf("%w: unsupported valence mode %d", ErrUnsupportedFeature, mode)
		}
	} else {
		if err := out.startFaceDecoder.StartDecoding(r); err != nil {
			return nil, fmt.Errorf("draco: valence edgebreaker start-face stream: %w", err)
		}

		if numAttributeData > 0 {
			out.attributeSeamDecoders = make([]entropy.RansBitDecoder, numAttributeData)
			for i := range out.attributeSeamDecoders {
				if err := out.attributeSeamDecoders[i].StartDecoding(r); err != nil {
					return nil, fmt.Errorf("draco: valence edgebreaker seam stream %d: %w", i, err)
				}
			}
		}
	}

	for i := range out.contextSymbols {
		count, err := core.DecodeVarUint32(r)
		if err != nil {
			return nil, err
		}

		if count == 0 {
			continue
		}

		symbols, err := entropy.DecodeSymbolsVersionedWithScratch(r, count, 1, legacy, scratch)
		if err != nil {
			return nil, err
		}

		out.contextSymbols[i] = symbols
		out.contextCounters[i] = len(symbols)
	}

	return out, nil
}

func (d *edgebreakerValenceTraversalDecoder) setCornerTable(table *edgebreakerMutableCornerTable) {
	d.table = table
}

func (d *edgebreakerValenceTraversalDecoder) DecodeSymbol() (uint32, error) {
	if d.activeContext != -1 {
		d.contextCounters[d.activeContext]--
		if d.contextCounters[d.activeContext] < 0 {
			return 0, errors.New("draco: valence edgebreaker context underflow")
		}

		symbolID := d.contextSymbols[d.activeContext][d.contextCounters[d.activeContext]]
		if symbolID >= uint32(len(edgeBreakerSymbolToTopology)) {
			return 0, fmt.Errorf("draco: invalid valence edgebreaker symbol %d", symbolID)
		}

		d.lastSymbol = edgeBreakerSymbolToTopology[symbolID]
		return d.lastSymbol, nil
	}

	if d.header.VersionMajor < 2 || (d.header.VersionMajor == 2 && d.header.VersionMinor < 2) {
		symbol, err := d.edgebreakerStandardTraversalDecoder.DecodeSymbol()
		if err != nil {
			return 0, err
		}

		d.lastSymbol = symbol
		return symbol, nil
	}

	d.lastSymbol = topologyE
	return topologyE, nil
}

func (d *edgebreakerValenceTraversalDecoder) NewActiveCornerReached(corner int) {
	if d.table == nil {
		return
	}

	next := d.table.Next(corner)
	prev := d.table.Previous(corner)
	switch d.lastSymbol {
	case topologyC, topologyS:
		d.vertexValences[d.table.Vertex(next)]++
		d.vertexValences[d.table.Vertex(prev)]++
	case topologyR:
		d.vertexValences[d.table.Vertex(corner)]++
		d.vertexValences[d.table.Vertex(next)]++
		d.vertexValences[d.table.Vertex(prev)] += 2
	case topologyL:
		d.vertexValences[d.table.Vertex(corner)]++
		d.vertexValences[d.table.Vertex(next)] += 2
		d.vertexValences[d.table.Vertex(prev)]++
	case topologyE:
		d.vertexValences[d.table.Vertex(corner)] += 2
		d.vertexValences[d.table.Vertex(next)] += 2
		d.vertexValences[d.table.Vertex(prev)] += 2
	}

	activeValence := d.vertexValences[d.table.Vertex(next)]
	if activeValence < minValence {
		activeValence = minValence
	} else if activeValence > maxValence {
		activeValence = maxValence
	}

	d.activeContext = activeValence - minValence
}

func (d *edgebreakerValenceTraversalDecoder) MergeVertices(dest, source int) {
	if dest < 0 || source < 0 || dest >= len(d.vertexValences) || source >= len(d.vertexValences) {
		return
	}

	d.vertexValences[dest] += d.vertexValences[source]
}

type edgebreakerPredictiveTraversalDecoder struct {
	*edgebreakerStandardTraversalDecoder
	table             *edgebreakerMutableCornerTable
	vertexValences    []int
	predictionDecoder entropy.RansBitDecoder
	lastSymbol        int
	predictedSymbol   int
}

func newEdgebreakerPredictiveTraversalDecoder(header bitstream.Header, r *core.Reader, numAttributeData, numVertices int, table *edgebreakerMutableCornerTable) (*edgebreakerPredictiveTraversalDecoder, error) {
	base, err := newEdgebreakerStandardTraversalDecoder(header, r, numAttributeData)
	if err != nil {
		return nil, err
	}

	numSplitSymbols, err := r.ReadInt32()
	if err != nil {
		return nil, err
	}

	if numSplitSymbols < 0 || numSplitSymbols >= int32(numVertices) {
		return nil, fmt.Errorf("%w: invalid predictive edgebreaker split count %d", ErrInvalidGeometry, numSplitSymbols)
	}

	legacy := header.VersionMajor < 2 || (header.VersionMajor == 2 && header.VersionMinor < 2)
	out := &edgebreakerPredictiveTraversalDecoder{
		edgebreakerStandardTraversalDecoder: base,
		table:                               table,
		vertexValences:                      make([]int, numVertices),
		lastSymbol:                          -1,
		predictedSymbol:                     -1,
	}
	if err := out.predictionDecoder.StartDecodingVersioned(r, legacy); err != nil {
		return nil, fmt.Errorf("draco: predictive edgebreaker predictions: %w", err)
	}

	return out, nil
}

func (d *edgebreakerPredictiveTraversalDecoder) DecodeSymbol() (uint32, error) {
	if d.predictedSymbol != -1 {
		if d.predictionDecoder.DecodeNextBit() {
			d.lastSymbol = d.predictedSymbol
			return uint32(d.predictedSymbol), nil
		}
	}

	symbol, err := d.edgebreakerStandardTraversalDecoder.DecodeSymbol()
	if err != nil {
		return 0, err
	}

	d.lastSymbol = int(symbol)
	return symbol, nil
}

func (d *edgebreakerPredictiveTraversalDecoder) NewActiveCornerReached(corner int) {
	next := d.table.Next(corner)
	prev := d.table.Previous(corner)
	switch d.lastSymbol {
	case topologyC, topologyS:
		d.vertexValences[d.table.Vertex(next)]++
		d.vertexValences[d.table.Vertex(prev)]++
	case topologyR:
		d.vertexValences[d.table.Vertex(corner)]++
		d.vertexValences[d.table.Vertex(next)]++
		d.vertexValences[d.table.Vertex(prev)] += 2
	case topologyL:
		d.vertexValences[d.table.Vertex(corner)]++
		d.vertexValences[d.table.Vertex(next)] += 2
		d.vertexValences[d.table.Vertex(prev)]++
	case topologyE:
		d.vertexValences[d.table.Vertex(corner)] += 2
		d.vertexValences[d.table.Vertex(next)] += 2
		d.vertexValences[d.table.Vertex(prev)] += 2
	}

	if d.lastSymbol == topologyC || d.lastSymbol == topologyR {
		pivot := d.table.Vertex(d.table.Next(corner))
		if pivot >= 0 && pivot < len(d.vertexValences) {
			if d.vertexValences[pivot] < 6 {
				d.predictedSymbol = topologyR
			} else {
				d.predictedSymbol = topologyC
			}

			return
		}
	}

	d.predictedSymbol = -1
}

func (d *edgebreakerPredictiveTraversalDecoder) MergeVertices(dest, source int) {
	if dest < 0 || source < 0 || dest >= len(d.vertexValences) || source >= len(d.vertexValences) {
		return
	}

	d.vertexValences[dest] += d.vertexValences[source]
}

func decodeEdgebreakerSplitEvents(ctx context.Context, reader *core.Reader, header bitstream.Header) ([]edgebreakerTopologySplit, error) {
	numSplits, err := readVersionedEdgebreakerVarUint32(reader, header)
	if err != nil {
		return nil, err
	}

	numSplitsInt, err := guardUint32SliceAllocation(numSplits, 12, "edgebreaker split events")
	if err != nil {
		return nil, err
	}

	events := make([]edgebreakerTopologySplit, numSplitsInt)
	if numSplits == 0 {
		if header.VersionMajor < 2 || (header.VersionMajor == 2 && header.VersionMinor < 1) {
			if _, err := readVersionedEdgebreakerVarUint32(reader, header); err != nil {
				return nil, err
			}
		}

		return events, nil
	}

	if header.VersionMajor < 1 || (header.VersionMajor == 1 && header.VersionMinor < 2) {
		for i := range events {
			if err := checkContextEvery(ctx, i); err != nil {
				return nil, err
			}

			splitID, err := reader.ReadUint32()
			if err != nil {
				return nil, err
			}

			sourceID, err := reader.ReadUint32()
			if err != nil {
				return nil, err
			}

			edge, err := reader.ReadUint8()
			if err != nil {
				return nil, err
			}

			events[i] = edgebreakerTopologySplit{
				sourceSymbolID: sourceID,
				splitSymbolID:  splitID,
				sourceEdge:     edge & 1,
			}
		}
	} else {
		lastSource := uint32(0)
		for i := range events {
			if err := checkContextEvery(ctx, i); err != nil {
				return nil, err
			}

			sourceDelta, err := core.DecodeVarUint32(reader)
			if err != nil {
				return nil, err
			}

			splitDelta, err := core.DecodeVarUint32(reader)
			if err != nil {
				return nil, err
			}

			sourceID := lastSource + sourceDelta
			if splitDelta > sourceID {
				return nil, fmt.Errorf("%w: invalid edgebreaker split delta", ErrInvalidGeometry)
			}

			events[i].sourceSymbolID = sourceID
			events[i].splitSymbolID = sourceID - splitDelta
			lastSource = sourceID
		}

		bitCount := int(numSplits)
		byteCount := (bitCount + 7) / 8
		bits, err := reader.ReadBytesView(byteCount)
		if err != nil {
			return nil, err
		}

		bitReader := core.NewBitReaderValue(bits)
		for i := range events {
			value, ok := bitReader.ReadBitLSB()
			if !ok {
				return nil, errors.New("draco: truncated edgebreaker split bits")
			}

			events[i].sourceEdge = uint8(value)
		}
	}

	if header.VersionMajor < 2 || (header.VersionMajor == 2 && header.VersionMinor < 1) {
		numHoleEvents, err := readVersionedEdgebreakerVarUint32(reader, header)
		if err != nil {
			return nil, err
		}

		if header.VersionMajor < 1 || (header.VersionMajor == 1 && header.VersionMinor < 2) {
			for i := uint32(0); i < numHoleEvents; i++ {
				if err := checkContextEvery(ctx, int(i)); err != nil {
					return nil, err
				}

				if _, err := reader.ReadInt32(); err != nil {
					return nil, err
				}
			}
		} else {
			last := uint32(0)
			for i := uint32(0); i < numHoleEvents; i++ {
				if err := checkContextEvery(ctx, int(i)); err != nil {
					return nil, err
				}

				delta, err := core.DecodeVarUint32(reader)
				if err != nil {
					return nil, err
				}

				last += delta
			}
		}
	}

	return events, nil
}

func decodeEdgebreakerMesh(reader *core.Reader, header bitstream.Header, options decodeConfig, ctx context.Context, scratch *entropy.DecodeScratch, decodeScratch *edgebreakerDecodeScratch) (*Mesh, error) {
	mesh := newMesh(0)
	var err error
	if header.Flags&bitstream.MetadataFlagMask != 0 {
		metadata, decodeErr := md.DecodeGeometryMetadata(ctx, reader)
		if decodeErr != nil {
			return nil, decodeErr
		}

		mesh.setMetadata(metadata)
	}

	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	traversalType, err := reader.ReadUint8()
	if err != nil {
		return nil, err
	}

	if header.VersionMajor < 2 || (header.VersionMajor == 2 && header.VersionMinor < 2) {
		if _, err := readVersionedEdgebreakerVarUint32(reader, header); err != nil {
			return nil, err
		}
	}

	numEncodedVertices, err := readVersionedEdgebreakerVarUint32(reader, header)
	if err != nil {
		return nil, err
	}

	numFaces, err := readVersionedEdgebreakerVarUint32(reader, header)
	if err != nil {
		return nil, err
	}

	numAttributeData, err := reader.ReadUint8()
	if err != nil {
		return nil, err
	}

	numEncodedSymbols, err := readVersionedEdgebreakerVarUint32(reader, header)
	if err != nil {
		return nil, err
	}

	numEncodedSplitSymbols, err := readVersionedEdgebreakerVarUint32(reader, header)
	if err != nil {
		return nil, err
	}

	if numEncodedSplitSymbols > numEncodedSymbols {
		return nil, fmt.Errorf("%w: invalid edgebreaker split count", ErrInvalidGeometry)
	}

	maxVertices64 := uint64(numEncodedVertices) + uint64(numEncodedSplitSymbols)
	if maxVertices64 > uint64(math.MaxInt) {
		return nil, fmt.Errorf("%w: edgebreaker vertex count %d exceeds int range", ErrInvalidGeometry, maxVertices64)
	}

	maxVertices := int(maxVertices64)
	numFacesInt, err := guardUint32SliceAllocation(numFaces, 24, "edgebreaker corner table faces")
	if err != nil {
		return nil, err
	}

	numSymbolsInt, err := guardUint32SliceAllocation(numEncodedSymbols, 9, "edgebreaker topology split state")
	if err != nil {
		return nil, err
	}

	if _, err := guardIntProductAllocation(numFacesInt, 3, 8, "edgebreaker corner vertices"); err != nil {
		return nil, err
	}

	if _, err := guardIntProductAllocation(numFacesInt, 3, 8, "edgebreaker opposite corners"); err != nil {
		return nil, err
	}

	if err := guardSliceAllocation(maxVertices, 8, "edgebreaker vertex corners"); err != nil {
		return nil, err
	}

	if err := guardSliceAllocation(int(numAttributeData), 8, "edgebreaker attribute connectivity"); err != nil {
		return nil, err
	}

	if err := guardSliceAllocation(maxVertices, 1, "edgebreaker hole flags"); err != nil {
		return nil, err
	}

	var baseTable *edgebreakerMutableCornerTable
	if decodeScratch != nil {
		baseTable = resetEdgebreakerMutableCornerTable(decodeScratch.mutableTable, numFacesInt, maxVertices)
		decodeScratch.mutableTable = baseTable
	} else {
		baseTable = newEdgebreakerMutableCornerTable(numFacesInt, maxVertices)
	}

	baseTable.enableVertexCornerTracking()

	var attrConnectivity []*edgebreakerAttributeConnectivity
	if decodeScratch != nil {
		attrConnectivity = slices.Grow(decodeScratch.attrConnectivity[:0], int(numAttributeData))
		attrConnectivity = attrConnectivity[:int(numAttributeData)]
		decodeScratch.attrConnectivity = attrConnectivity
	} else {
		attrConnectivity = make([]*edgebreakerAttributeConnectivity, int(numAttributeData))
	}

	for i := range attrConnectivity {
		attrConnectivity[i] = resetEdgebreakerAttributeConnectivity(attrConnectivity[i], baseTable.CornerCount(), maxVertices)
	}

	var splitEvents []edgebreakerTopologySplit
	var connectivityReader *core.Reader
	if header.VersionMajor < 2 || (header.VersionMajor == 2 && header.VersionMinor < 2) {
		connectivitySize, err := readVersionedEdgebreakerVarUint32(reader, header)
		if err != nil {
			return nil, err
		}

		if _, err := guardUint32SliceAllocation(connectivitySize, 1, "edgebreaker connectivity payload"); err != nil {
			return nil, err
		}

		connectivityPayload, err := reader.ReadBytesView(int(connectivitySize))
		if err != nil {
			return nil, err
		}

		connectivityReader = core.NewReader(connectivityPayload)
		splitEvents, err = decodeEdgebreakerSplitEvents(ctx, reader, header)
		if err != nil {
			return nil, err
		}
	} else {
		splitEvents, err = decodeEdgebreakerSplitEvents(ctx, reader, header)
		if err != nil {
			return nil, err
		}

		connectivityReader = reader
	}

	var traversal edgebreakerTraversal
	switch traversalType {
	case edgebreakerTraversalStandard:
		traversal, err = newEdgebreakerStandardTraversalDecoder(header, connectivityReader, int(numAttributeData))
	case edgebreakerTraversalPredictive:
		traversal, err = newEdgebreakerPredictiveTraversalDecoder(header, connectivityReader, int(numAttributeData), maxVertices, baseTable)
	case edgebreakerTraversalValence:
		valenceDecoder, valenceErr := newEdgebreakerValenceTraversalDecoder(header, connectivityReader, int(numAttributeData), maxVertices, scratch)
		if valenceErr == nil {
			valenceDecoder.setCornerTable(baseTable)
		}

		traversal, err = valenceDecoder, valenceErr
	default:
		err = fmt.Errorf("%w: edgebreaker traversal type %d", ErrUnsupportedFeature, traversalType)
	}

	if err != nil {
		return nil, err
	}

	var isVertHole []bool
	if decodeScratch != nil {
		isVertHole = growBoolSlice(decodeScratch.isVertHole, maxVertices, true)
		decodeScratch.isVertHole = isVertHole
	} else {
		isVertHole = make([]bool, maxVertices)
		for i := range isVertHole {
			isVertHole[i] = true
		}
	}

	numConnectivityVertices, err := decodeEdgebreakerConnectivity(ctx, baseTable, traversal, numFacesInt, numSymbolsInt, splitEvents, attrConnectivity, isVertHole, decodeScratch)
	if err != nil {
		return nil, err
	}

	for vertexID, hole := range isVertHole[:baseTable.VertexCount()] {
		if hole {
			baseTable.UpdateVertexToCornerMap(vertexID)
		}
	}

	for _, connectivity := range attrConnectivity {
		if err := connectivity.recomputeVertices(baseTable); err != nil {
			return nil, err
		}
	}

	cornerToPoint, pointToCorner, faces, err := assignEdgebreakerPointsToCorners(ctx, baseTable, attrConnectivity, isVertHole, decodeScratch)
	if err != nil {
		return nil, err
	}

	mesh.setPointCount(len(pointToCorner))
	mesh.faces = make([]Face, len(faces))
	copy(mesh.faces, faces)

	if mesh.PointCount() == 0 && numConnectivityVertices > 0 {
		mesh.setPointCount(numConnectivityVertices)
	}

	groups, err := parseEdgebreakerAttributeDecoders(ctx, reader, header, int(numAttributeData), decodeScratch)
	if err != nil {
		return nil, err
	}

	for _, group := range groups {
		if err := generateEdgebreakerSequence(ctx, baseTable, attrConnectivity, group, decodeScratch); err != nil {
			return nil, err
		}

		if err := instantiateEdgebreakerAttributes(ctx, mesh, baseTable, attrConnectivity, pointToCorner, group, options, decodeScratch); err != nil {
			return nil, err
		}
	}

	predictionTable := edgebreakerPredictionCornerTable(mesh, baseTable, cornerToPoint, decodeScratch)
	if err := decodeEdgebreakerAttributeValues(ctx, reader, header, mesh, groups, options, predictionTable, scratch); err != nil {
		return nil, err
	}

	return mesh, nil
}

func decodeEdgebreakerConnectivity(ctx context.Context, table *edgebreakerMutableCornerTable, traversal edgebreakerTraversal, numFaces, numSymbols int, splitEvents []edgebreakerTopologySplit, attrConnectivity []*edgebreakerAttributeConnectivity, isVertHole []bool, scratch *edgebreakerDecodeScratch) (int, error) {
	standardDecoder, standardTraversal := traversal.(*edgebreakerStandardTraversalDecoder)
	var activeCorners []int
	var topologySplitCorners []int
	var topologySplitSet []bool
	if scratch != nil {
		activeCorners = slices.Grow(scratch.activeCorners[:0], numFaces)
		scratch.activeCorners = activeCorners
		topologySplitCorners = growIntSlice(scratch.topologySplitCorners, numSymbols, 0)
		scratch.topologySplitCorners = topologySplitCorners
		topologySplitSet = growBoolSlice(scratch.topologySplitSet, numSymbols, false)
		scratch.topologySplitSet = topologySplitSet
	} else {
		activeCorners = make([]int, 0, numFaces)
		topologySplitCorners = make([]int, numSymbols)
		topologySplitSet = make([]bool, numSymbols)
	}

	numTopologySplitCorners := 0
	removeInvalidVertices := len(attrConnectivity) == 0
	var invalidVertices []int
	if scratch != nil {
		invalidVertices = slices.Grow(scratch.invalidVertices[:0], numFaces/8)
		scratch.invalidVertices = invalidVertices
	} else {
		invalidVertices = make([]int, 0, numFaces/8)
	}

	if scratch != nil {
		defer func() {
			scratch.activeCorners = activeCorners[:0]
			scratch.invalidVertices = invalidVertices[:0]
		}()
	}

	maxVertices := len(isVertHole)
	numDecodedFaces := 0
	splitIndex := len(splitEvents) - 1

	for symbolID := 0; symbolID < numSymbols; symbolID++ {
		if err := checkContextEvery(ctx, symbolID); err != nil {
			return 0, err
		}

		faceID := numDecodedFaces
		numDecodedFaces++
		checkTopologySplit := false

		var symbol uint32
		var err error
		if standardTraversal {
			symbol, err = standardDecoder.DecodeSymbol()
		} else {
			symbol, err = traversal.DecodeSymbol()
		}

		if err != nil {
			return 0, err
		}

		switch symbol {
		case topologyC:
			if len(activeCorners) == 0 {
				return 0, fmt.Errorf("%w: missing active corner for C symbol", ErrInvalidGeometry)
			}

			cornerA := activeCorners[len(activeCorners)-1]
			vertexX := table.Vertex(table.Next(cornerA))
			cornerB := table.Next(table.LeftMostCorner(vertexX))
			if cornerA == cornerB || cornerA < 0 || cornerB < 0 {
				return 0, fmt.Errorf("%w: invalid C symbol corner pairing at symbol %d (cornerA=%d cornerB=%d vertexX=%d)", ErrInvalidGeometry, symbolID, cornerA, cornerB, vertexX)
			}

			if table.Opposite(cornerA) >= 0 || table.Opposite(cornerB) >= 0 {
				return 0, fmt.Errorf("%w: edgebreaker opposite corner already assigned at symbol %d (cornerA=%d oppA=%d cornerB=%d oppB=%d)", ErrInvalidGeometry, symbolID, cornerA, table.Opposite(cornerA), cornerB, table.Opposite(cornerB))
			}

			corner := 3 * faceID
			table.SetOppositeCorners(cornerA, corner+1)
			table.SetOppositeCorners(cornerB, corner+2)
			vertAPrev := table.Vertex(table.Previous(cornerA))
			vertBNext := table.Vertex(table.Next(cornerB))
			if vertexX == vertAPrev || vertexX == vertBNext {
				return 0, fmt.Errorf("%w: degenerate C face", ErrInvalidGeometry)
			}

			table.MapCornerToVertex(corner, vertexX)
			table.MapCornerToVertex(corner+1, vertBNext)
			table.MapCornerToVertex(corner+2, vertAPrev)
			table.SetLeftMostCorner(vertAPrev, corner+2)
			isVertHole[vertexX] = false
			activeCorners[len(activeCorners)-1] = corner
		case topologyR, topologyL:
			if len(activeCorners) == 0 {
				return 0, fmt.Errorf("%w: missing active corner for LR symbol", ErrInvalidGeometry)
			}

			cornerA := activeCorners[len(activeCorners)-1]
			if table.Opposite(cornerA) >= 0 {
				return 0, fmt.Errorf("%w: edgebreaker active corner already occupied at symbol %d (cornerA=%d oppA=%d)", ErrInvalidGeometry, symbolID, cornerA, table.Opposite(cornerA))
			}

			corner := 3 * faceID
			var oppCorner, cornerL, cornerR int
			if symbol == topologyR {
				oppCorner = corner + 2
				cornerL = corner + 1
				cornerR = corner
			} else {
				oppCorner = corner + 1
				cornerL = corner
				cornerR = corner + 2
			}

			table.SetOppositeCorners(oppCorner, cornerA)
			newVertex := table.AddNewVertex()
			if newVertex < 0 || table.VertexCount() > maxVertices {
				return 0, fmt.Errorf("%w: too many edgebreaker vertices", ErrInvalidGeometry)
			}

			table.MapCornerToVertex(oppCorner, newVertex)
			table.SetLeftMostCorner(newVertex, oppCorner)
			vertexR := table.Vertex(table.Previous(cornerA))
			table.MapCornerToVertex(cornerR, vertexR)
			table.SetLeftMostCorner(vertexR, cornerR)
			table.MapCornerToVertex(cornerL, table.Vertex(table.Next(cornerA)))
			activeCorners[len(activeCorners)-1] = corner
			checkTopologySplit = true
		case topologyS:
			if len(activeCorners) == 0 {
				return 0, fmt.Errorf("%w: missing active corner for S symbol", ErrInvalidGeometry)
			}

			cornerB := activeCorners[len(activeCorners)-1]
			activeCorners = activeCorners[:len(activeCorners)-1]
			if topologySplitSet[symbolID] {
				activeCorners = append(activeCorners, topologySplitCorners[symbolID])
			}

			if len(activeCorners) == 0 {
				return 0, fmt.Errorf("%w: missing split source corner at symbol %d (remainingActive=%d knownSplits=%d)", ErrInvalidGeometry, symbolID, len(activeCorners), numTopologySplitCorners)
			}

			cornerA := activeCorners[len(activeCorners)-1]
			if cornerA == cornerB || table.Opposite(cornerA) >= 0 || table.Opposite(cornerB) >= 0 {
				return 0, fmt.Errorf("%w: invalid S symbol merge at symbol %d (cornerA=%d oppA=%d cornerB=%d oppB=%d)", ErrInvalidGeometry, symbolID, cornerA, table.Opposite(cornerA), cornerB, table.Opposite(cornerB))
			}

			corner := 3 * faceID
			table.SetOppositeCorners(cornerA, corner+2)
			table.SetOppositeCorners(cornerB, corner+1)
			vertexP := table.Vertex(table.Previous(cornerA))
			table.MapCornerToVertex(corner, vertexP)
			table.MapCornerToVertex(corner+1, table.Vertex(table.Next(cornerA)))
			vertBPrev := table.Vertex(table.Previous(cornerB))
			table.MapCornerToVertex(corner+2, vertBPrev)
			table.SetLeftMostCorner(vertBPrev, corner+2)
			cornerN := table.Next(cornerB)
			vertexN := table.Vertex(cornerN)
			if !standardTraversal {
				traversal.MergeVertices(vertexP, vertexN)
			}

			if err := mergeEdgebreakerVertexFan(table, vertexN, vertexP, cornerN); err != nil {
				return 0, err
			}

			table.SetLeftMostCorner(vertexP, table.LeftMostCorner(vertexN))
			table.MakeVertexIsolated(vertexN)
			if removeInvalidVertices {
				invalidVertices = append(invalidVertices, vertexN)
			}

			activeCorners[len(activeCorners)-1] = corner
		case topologyE:
			corner := 3 * faceID
			firstVertex := table.AddNewVertex()
			secondVertex := table.AddNewVertex()
			thirdVertex := table.AddNewVertex()
			if firstVertex < 0 || secondVertex < 0 || thirdVertex < 0 || table.VertexCount() > maxVertices {
				return 0, fmt.Errorf("%w: too many edgebreaker vertices", ErrInvalidGeometry)
			}

			table.MapCornerToVertex(corner, firstVertex)
			table.MapCornerToVertex(corner+1, secondVertex)
			table.MapCornerToVertex(corner+2, thirdVertex)
			table.SetLeftMostCorner(firstVertex, corner)
			table.SetLeftMostCorner(secondVertex, corner+1)
			table.SetLeftMostCorner(thirdVertex, corner+2)
			activeCorners = append(activeCorners, corner)
			checkTopologySplit = true
		default:
			return 0, fmt.Errorf("%w: invalid edgebreaker topology symbol %d", ErrInvalidGeometry, symbol)
		}

		if !standardTraversal {
			traversal.NewActiveCornerReached(activeCorners[len(activeCorners)-1])
		}

		if checkTopologySplit {
			encoderSymbolID := numSymbols - symbolID - 1
			for splitIndex >= 0 && splitEvents[splitIndex].sourceSymbolID == uint32(encoderSymbolID) {
				event := splitEvents[splitIndex]
				splitIndex--
				actCorner := activeCorners[len(activeCorners)-1]
				newActiveCorner := table.Previous(actCorner)
				if event.sourceEdge == rightFaceEdge {
					newActiveCorner = table.Next(actCorner)
				}

				decoderSplitID := numSymbols - int(event.splitSymbolID) - 1
				if decoderSplitID < 0 || decoderSplitID >= len(topologySplitCorners) {
					return 0, fmt.Errorf("%w: edgebreaker split symbol %d maps outside decoder range %d", ErrInvalidGeometry, event.splitSymbolID, numSymbols)
				}

				if !topologySplitSet[decoderSplitID] {
					numTopologySplitCorners++
				}

				topologySplitCorners[decoderSplitID] = newActiveCorner
				topologySplitSet[decoderSplitID] = true
			}
		}
	}

	if table.VertexCount() > maxVertices {
		return 0, fmt.Errorf("%w: too many decoded edgebreaker vertices", ErrInvalidGeometry)
	}

	for len(activeCorners) > 0 {
		corner := activeCorners[len(activeCorners)-1]
		activeCorners = activeCorners[:len(activeCorners)-1]
		var interiorFace bool
		var err error
		if standardTraversal {
			interiorFace, err = standardDecoder.DecodeStartFaceConfiguration()
		} else {
			interiorFace, err = traversal.DecodeStartFaceConfiguration()
		}

		if err != nil {
			return 0, err
		}

		if !interiorFace {
			continue
		}

		if numDecodedFaces >= table.FaceCount() {
			return 0, fmt.Errorf("%w: too many edgebreaker faces", ErrInvalidGeometry)
		}

		cornerA := corner
		vertN := table.Vertex(table.Next(cornerA))
		cornerB := table.Next(table.LeftMostCorner(vertN))
		vertX := table.Vertex(table.Next(cornerB))
		cornerC := table.Next(table.LeftMostCorner(vertX))
		if corner == cornerB || corner == cornerC || cornerB == cornerC {
			return 0, fmt.Errorf("%w: invalid edgebreaker init face", ErrInvalidGeometry)
		}

		if table.Opposite(corner) >= 0 || table.Opposite(cornerB) >= 0 || table.Opposite(cornerC) >= 0 {
			return 0, fmt.Errorf("%w: invalid occupied edgebreaker init edge", ErrInvalidGeometry)
		}

		vertP := table.Vertex(table.Next(cornerC))
		faceID := numDecodedFaces
		numDecodedFaces++
		newCorner := 3 * faceID
		table.SetOppositeCorners(newCorner, corner)
		table.SetOppositeCorners(newCorner+1, cornerB)
		table.SetOppositeCorners(newCorner+2, cornerC)
		table.MapCornerToVertex(newCorner, vertX)
		table.MapCornerToVertex(newCorner+1, vertP)
		table.MapCornerToVertex(newCorner+2, vertN)
		for i := 0; i < 3; i++ {
			vertex := table.Vertex(newCorner + i)
			if vertex >= 0 && vertex < len(isVertHole) {
				isVertHole[vertex] = false
			}
		}

		if !standardTraversal {
			traversal.NewInteriorFaceReached(newCorner)
		}
	}

	if numDecodedFaces != table.FaceCount() {
		return 0, fmt.Errorf("%w: unexpected edgebreaker face count %d want %d", ErrInvalidGeometry, numDecodedFaces, table.FaceCount())
	}

	if removeInvalidVertices && len(invalidVertices) > 0 {
		var remap []int
		var remappedHoles []bool
		if scratch != nil {
			remap = growIntSlice(scratch.remap, table.maxVertices, -1)
			scratch.remap = remap
			remappedHoles = growBoolSlice(scratch.remappedHoles, len(isVertHole), false)
			scratch.remappedHoles = remappedHoles
		} else {
			remap = make([]int, table.maxVertices)
			for i := range remap {
				remap[i] = -1
			}

			remappedHoles = make([]bool, len(isVertHole))
		}

		numVertices := 0
		for corner := 0; corner < table.CornerCount(); corner++ {
			vertex := table.Vertex(corner)
			if vertex < 0 {
				return 0, fmt.Errorf("%w: edgebreaker corner %d has invalid vertex %d before compaction", ErrInvalidGeometry, corner, vertex)
			}

			if remap[vertex] < 0 {
				remap[vertex] = numVertices
				remappedHoles[numVertices] = isVertHole[vertex]
				numVertices++
			}

			table.cornerVertex[corner] = remap[vertex]
		}

		for i := range table.vertexCorners {
			table.vertexCorners[i] = -1
		}

		for corner := 0; corner < table.CornerCount(); corner++ {
			vertex := table.Vertex(corner)
			if table.vertexCorners[vertex] < 0 {
				table.vertexCorners[vertex] = corner
			}
		}

		table.numVertices = numVertices
		for vertex := 0; vertex < table.VertexCount(); vertex++ {
			isVertHole[vertex] = remappedHoles[vertex]
			table.UpdateVertexToCornerMap(vertex)
		}
	}

	if len(attrConnectivity) > 0 {
		if standardTraversal {
			if err := decodeStandardEdgebreakerAttributeSeams(ctx, table, standardDecoder, attrConnectivity); err != nil {
				return 0, err
			}
		} else {
			if err := decodeEdgebreakerAttributeSeams(ctx, table, traversal, attrConnectivity); err != nil {
				return 0, err
			}
		}
	}

	for corner := 0; corner < table.CornerCount(); corner++ {
		vertex := table.Vertex(corner)
		if vertex < 0 || vertex >= table.VertexCount() {
			return 0, fmt.Errorf("%w: edgebreaker corner %d maps to vertex %d outside compacted range %d", ErrInvalidGeometry, corner, vertex, table.VertexCount())
		}
	}

	return table.VertexCount(), nil
}

func mergeEdgebreakerVertexFan(table *edgebreakerMutableCornerTable, fromVertex, toVertex, seedCorner int) error {
	if table == nil || fromVertex == toVertex {
		return nil
	}

	if seedCorner >= 0 && table.Vertex(seedCorner) != fromVertex {
		return fmt.Errorf("%w: unexpected seed corner %d for merge %d->%d", ErrInvalidGeometry, seedCorner, fromVertex, toVertex)
	}

	if table.trackVertexCorners && fromVertex >= 0 && fromVertex < len(table.vertexCornerHead) && toVertex >= 0 && toVertex < len(table.vertexCornerHead) {
		head := table.vertexCornerHead[fromVertex]
		if head >= 0 {
			tail := -1
			for corner := head; corner >= 0; corner = table.vertexCornerNext[corner] {
				if table.cornerVertex[corner] == fromVertex {
					table.cornerVertex[corner] = toVertex
				}

				tail = corner
			}

			table.vertexCornerNext[tail] = table.vertexCornerHead[toVertex]
			table.vertexCornerHead[toVertex] = head
			table.vertexCornerHead[fromVertex] = -1
			return nil
		}
	}

	for corner, vertex := range table.cornerVertex {
		if vertex != fromVertex {
			continue
		}

		table.cornerVertex[corner] = toVertex
	}

	return nil
}

func decodeEdgebreakerAttributeSeams(ctx context.Context, table *edgebreakerMutableCornerTable, traversal edgebreakerTraversal, attrConnectivity []*edgebreakerAttributeConnectivity) error {
	faceCount := table.FaceCount()
	opposite := table.opposite
	for faceID := 0; faceID < faceCount; faceID++ {
		if err := checkContextEvery(ctx, faceID); err != nil {
			return err
		}

		for local := 0; local < 3; local++ {
			corner := faceID*3 + local
			oppCorner := opposite[corner]
			if oppCorner < 0 {
				for _, connectivity := range attrConnectivity {
					connectivity.markSeam(table, corner)
				}

				continue
			}

			if oppCorner < corner {
				continue
			}

			for attrID, connectivity := range attrConnectivity {
				isSeam, err := traversal.DecodeAttributeSeam(attrID)
				if err != nil {
					return err
				}

				if isSeam {
					connectivity.markSeam(table, corner)
				}
			}
		}
	}

	return nil
}

func decodeStandardEdgebreakerAttributeSeams(ctx context.Context, table *edgebreakerMutableCornerTable, traversal *edgebreakerStandardTraversalDecoder, attrConnectivity []*edgebreakerAttributeConnectivity) error {
	faceCount := table.FaceCount()
	opposite := table.opposite
	for faceID := 0; faceID < faceCount; faceID++ {
		if err := checkContextEvery(ctx, faceID); err != nil {
			return err
		}

		for local := 0; local < 3; local++ {
			corner := faceID*3 + local
			oppCorner := opposite[corner]
			if oppCorner < 0 {
				for _, connectivity := range attrConnectivity {
					connectivity.markSeam(table, corner)
				}

				continue
			}

			if oppCorner < corner {
				continue
			}

			for attrID, connectivity := range attrConnectivity {
				isSeam, err := traversal.DecodeAttributeSeam(attrID)
				if err != nil {
					return err
				}

				if isSeam {
					connectivity.markSeam(table, corner)
				}
			}
		}
	}

	return nil
}

func assignEdgebreakerPointsToCorners(ctx context.Context, table *edgebreakerMutableCornerTable, attrConnectivity []*edgebreakerAttributeConnectivity, isVertHole []bool, scratch *edgebreakerDecodeScratch) ([]int, []int, []Face, error) {
	cornerCount := table.CornerCount()
	vertexCount := table.VertexCount()
	faceCount := table.FaceCount()
	cornerVertex := table.cornerVertex
	vertexCorners := table.vertexCorners
	opposite := table.opposite
	var cornerToPoint []int
	var pointToCorner []int
	if scratch != nil {
		cornerToPoint = growIntSlice(scratch.cornerToPoint, cornerCount, -1)
		scratch.cornerToPoint = cornerToPoint
		pointToCorner = slices.Grow(scratch.pointToCorner[:0], vertexCount)
		scratch.pointToCorner = pointToCorner
	} else {
		cornerToPoint = make([]int, cornerCount)
		for i := range cornerToPoint {
			cornerToPoint[i] = -1
		}

		pointToCorner = make([]int, 0, vertexCount)
	}

	if scratch != nil {
		defer func() {
			scratch.pointToCorner = pointToCorner[:0]
		}()
	}

	for vertex := 0; vertex < vertexCount; vertex++ {
		if err := checkContextEvery(ctx, vertex); err != nil {
			return nil, nil, nil, err
		}

		corner := vertexCorners[vertex]
		if corner < 0 {
			continue
		}

		dedupFirstCorner := corner
		if !isVertHole[vertex] {
			for _, connectivity := range attrConnectivity {
				if !connectivity.vertexOnSeam[cornerVertex[corner]] {
					continue
				}

				vertID := connectivity.cornerToVertex[corner]
				actCorner := swingRightCorner(opposite, corner)
				seamFound := false
				for actCorner >= 0 && actCorner != corner {
					if connectivity.cornerToVertex[actCorner] != vertID {
						dedupFirstCorner = actCorner
						seamFound = true
						break
					}

					actCorner = swingRightCorner(opposite, actCorner)
				}

				if seamFound {
					break
				}
			}
		}

		cornerToPoint[dedupFirstCorner] = len(pointToCorner)
		pointToCorner = append(pointToCorner, dedupFirstCorner)
		prevCorner := dedupFirstCorner
		actCorner := swingRightCorner(opposite, dedupFirstCorner)
		for actCorner >= 0 && actCorner != dedupFirstCorner {
			attributeSeam := false
			for _, connectivity := range attrConnectivity {
				if connectivity.cornerToVertex[actCorner] != connectivity.cornerToVertex[prevCorner] {
					attributeSeam = true
					break
				}
			}

			if attributeSeam {
				cornerToPoint[actCorner] = len(pointToCorner)
				pointToCorner = append(pointToCorner, actCorner)
			} else {
				cornerToPoint[actCorner] = cornerToPoint[prevCorner]
			}

			prevCorner = actCorner
			actCorner = swingRightCorner(opposite, actCorner)
		}
	}

	var faces []Face
	if scratch != nil {
		faces = slices.Grow(scratch.faces[:0], faceCount)
		faces = faces[:faceCount]
		scratch.faces = faces
	} else {
		faces = make([]Face, faceCount)
	}

	for faceID := 0; faceID < faceCount; faceID++ {
		if err := checkContextEvery(ctx, faceID); err != nil {
			return nil, nil, nil, err
		}

		firstCorner := faceID * 3
		var face Face
		for local := 0; local < 3; local++ {
			corner := firstCorner + local
			point := cornerToPoint[corner]
			if point < 0 {
				return nil, nil, nil, fmt.Errorf("%w: missing point mapping for corner %d", ErrInvalidGeometry, corner)
			}

			face[local] = uint32(point)
		}

		faces[faceID] = face
	}

	return cornerToPoint, pointToCorner, faces, nil
}

func parseEdgebreakerAttributeDecoders(ctx context.Context, reader *core.Reader, header bitstream.Header, numAttributeData int, scratch *edgebreakerDecodeScratch) ([]*edgebreakerAttributeDecoder, error) {
	numDecoders, err := reader.ReadUint8()
	if err != nil {
		return nil, err
	}

	if numDecoders == 0 {
		return nil, fmt.Errorf("%w: attribute decoder count is zero", ErrInvalidGeometry)
	}

	var groups []*edgebreakerAttributeDecoder
	var groupStorage []edgebreakerAttributeDecoder
	if scratch != nil {
		groups = slices.Grow(scratch.groupPtrs[:0], int(numDecoders))
		groups = groups[:int(numDecoders)]
		scratch.groupPtrs = groups
		groupStorage = slices.Grow(scratch.attributeGroups[:0], int(numDecoders))
		groupStorage = groupStorage[:int(numDecoders)]
		scratch.attributeGroups = groupStorage
	} else {
		groups = make([]*edgebreakerAttributeDecoder, int(numDecoders))
	}

	for i := 0; i < int(numDecoders); i++ {
		if err := checkContextEvery(ctx, i); err != nil {
			return nil, err
		}

		dataID, err := reader.ReadInt8()
		if err != nil {
			return nil, err
		}

		decoderType, err := reader.ReadUint8()
		if err != nil {
			return nil, err
		}

		var group *edgebreakerAttributeDecoder
		if scratch != nil {
			group = &groupStorage[i]
			pending := group.pending[:0]
			states := group.states[:0]
			*group = edgebreakerAttributeDecoder{
				pending:         pending,
				states:          states,
				dataID:          int(dataID),
				decoderType:     decoderType,
				traversalMethod: meshTraversalDepthFirst,
			}
		} else {
			group = &edgebreakerAttributeDecoder{
				dataID:          int(dataID),
				decoderType:     decoderType,
				traversalMethod: meshTraversalDepthFirst,
			}
		}

		if header.VersionMajor > 1 || (header.VersionMajor == 1 && header.VersionMinor >= 2) {
			traversalMethod, err := reader.ReadUint8()
			if err != nil {
				return nil, err
			}

			group.traversalMethod = traversalMethod
		}

		if group.dataID >= numAttributeData {
			return nil, fmt.Errorf("%w: invalid attribute data id %d", ErrInvalidGeometry, group.dataID)
		}

		groups[i] = group
	}

	for i := range groups {
		group := groups[i]
		numAttrs, err := readVersionedEdgebreakerVarUint32(reader, header)
		if err != nil {
			return nil, err
		}

		if _, err := guardUint32SliceAllocation(numAttrs, 16, "edgebreaker pending attributes"); err != nil {
			return nil, err
		}

		if scratch != nil {
			group.pending = slices.Grow(group.pending[:0], int(numAttrs))
			group.pending = group.pending[:int(numAttrs)]
		} else {
			group.pending = make([]edgebreakerPendingAttribute, int(numAttrs))
		}

		for j := 0; j < int(numAttrs); j++ {
			if err := checkContextEvery(ctx, j); err != nil {
				return nil, err
			}

			attType, err := reader.ReadUint8()
			if err != nil {
				return nil, err
			}

			dataType, err := reader.ReadUint8()
			if err != nil {
				return nil, err
			}

			numComponents, err := reader.ReadUint8()
			if err != nil {
				return nil, err
			}

			normalized, err := reader.ReadUint8()
			if err != nil {
				return nil, err
			}

			var uniqueID uint32
			if header.VersionMajor < 1 || (header.VersionMajor == 1 && header.VersionMinor < 3) {
				legacyID, err := reader.ReadUint16()
				if err != nil {
					return nil, err
				}

				uniqueID = uint32(legacyID)
			} else {
				uniqueID, err = core.DecodeVarUint32(reader)
				if err != nil {
					return nil, err
				}
			}

			group.pending[j] = edgebreakerPendingAttribute{
				attrType:      AttributeType(attType),
				dataType:      DataType(dataType),
				numComponents: int(numComponents),
				normalized:    normalized > 0,
				uniqueID:      uniqueID,
			}
		}

		for j := range group.pending {
			if err := checkContextEvery(ctx, j); err != nil {
				return nil, err
			}

			decoderType, err := reader.ReadUint8()
			if err != nil {
				return nil, err
			}

			group.pending[j].sequentialType = decoderType
		}
	}

	return groups, nil
}

func generateEdgebreakerSequence(ctx context.Context, base *edgebreakerMutableCornerTable, attrConnectivity []*edgebreakerAttributeConnectivity, group *edgebreakerAttributeDecoder, scratch *edgebreakerDecodeScratch) error {
	numVertices := base.VertexCount()
	var connectivity *edgebreakerAttributeConnectivity
	if group.dataID >= 0 {
		connectivity = attrConnectivity[group.dataID]
		if group.decoderType == meshCornerAttribute {
			numVertices = connectivity.numVertices
		}
	}

	if scratch != nil {
		group.sequenceCorners = slices.Grow(scratch.sequenceCorners[:0], numVertices)
		scratch.sequenceCorners = group.sequenceCorners
		group.vertexToEncoded = growIntSlice(scratch.vertexToEncoded, numVertices, -1)
		scratch.vertexToEncoded = group.vertexToEncoded
	} else {
		group.sequenceCorners = group.sequenceCorners[:0]
		group.vertexToEncoded = make([]int, numVertices)
		for i := range group.vertexToEncoded {
			group.vertexToEncoded[i] = -1
		}
	}

	switch group.traversalMethod {
	case meshTraversalDepthFirst:
		if err := generateEdgebreakerDepthFirstSequence(ctx, base, connectivity, group, scratch); err != nil {
			return err
		}
	case meshTraversalPredictionDegree:
		if err := generateEdgebreakerPredictionDegreeSequence(ctx, base, connectivity, group, scratch); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: edgebreaker traversal method %d", ErrUnsupportedFeature, group.traversalMethod)
	}

	assigned := countAssignedEdgebreakerVertices(group.vertexToEncoded)
	if assigned != len(group.sequenceCorners) {
		return fmt.Errorf("%w: edgebreaker sequence assigned %d unique vertices but produced %d sequence entries", ErrInvalidGeometry, assigned, len(group.sequenceCorners))
	}

	return nil
}

func countAssignedEdgebreakerVertices(vertexToEncoded []int) int {
	assigned := 0
	for _, entry := range vertexToEncoded {
		if entry >= 0 {
			assigned++
		}
	}

	return assigned
}

func generateEdgebreakerDepthFirstSequence(ctx context.Context, base *edgebreakerMutableCornerTable, connectivity *edgebreakerAttributeConnectivity, group *edgebreakerAttributeDecoder, scratch *edgebreakerDecodeScratch) error {
	var isFaceVisited []bool
	var isVertexVisited []bool
	var stack []int
	if scratch != nil {
		isFaceVisited = growBoolSlice(scratch.sequenceVisitedFaces, base.FaceCount(), false)
		scratch.sequenceVisitedFaces = isFaceVisited
		isVertexVisited = growBoolSlice(scratch.sequenceVisitedVerts, len(group.vertexToEncoded), false)
		scratch.sequenceVisitedVerts = isVertexVisited
		stack = slices.Grow(scratch.sequenceStack[:0], base.FaceCount())
		scratch.sequenceStack = stack
	} else {
		isFaceVisited = make([]bool, base.FaceCount())
		isVertexVisited = make([]bool, len(group.vertexToEncoded))
		stack = make([]int, 0, base.FaceCount())
	}

	if scratch != nil {
		defer func() {
			scratch.sequenceStack = stack[:0]
		}()
	}

	onNewVertex := func(vertex, corner int) {
		group.vertexToEncoded[vertex] = len(group.sequenceCorners)
		group.sequenceCorners = append(group.sequenceCorners, corner)
	}
	for faceID := 0; faceID < base.FaceCount(); faceID++ {
		if err := checkContextEvery(ctx, faceID); err != nil {
			return err
		}

		startCorner := faceID * 3
		if isFaceVisited[faceID] {
			continue
		}

		stack = append(stack[:0], startCorner)
		_, nextVert, prevVert := edgebreakerCornerToVerts(base, connectivity, group, startCorner)
		if nextVert >= 0 && nextVert < len(isVertexVisited) && !isVertexVisited[nextVert] {
			isVertexVisited[nextVert] = true
			onNewVertex(nextVert, base.Next(startCorner))
		}

		if prevVert >= 0 && prevVert < len(isVertexVisited) && !isVertexVisited[prevVert] {
			isVertexVisited[prevVert] = true
			onNewVertex(prevVert, base.Previous(startCorner))
		}

		for len(stack) > 0 {
			corner := stack[len(stack)-1]
			currentFace := base.Face(corner)
			if corner < 0 || currentFace < 0 || isFaceVisited[currentFace] {
				stack = stack[:len(stack)-1]
				continue
			}

			for {
				currentFace = base.Face(corner)
				isFaceVisited[currentFace] = true
				vertex, _, _ := edgebreakerCornerToVerts(base, connectivity, group, corner)
				if !isVertexVisited[vertex] {
					isVertexVisited[vertex] = true
					onNewVertex(vertex, corner)
					if !edgebreakerIsBoundary(base, connectivity, group, vertex, corner) {
						corner = base.RightCorner(corner)
						continue
					}
				}

				rightCorner, leftCorner := edgebreakerTraversalNeighbors(base, connectivity, group, corner)
				rightFaceVisited := rightCorner < 0 || isFaceVisited[base.Face(rightCorner)]
				leftFaceVisited := leftCorner < 0 || isFaceVisited[base.Face(leftCorner)]
				if rightFaceVisited {
					if leftFaceVisited {
						stack = stack[:len(stack)-1]
						break
					}

					corner = leftCorner
					continue
				}

				if leftFaceVisited {
					corner = rightCorner
					continue
				}

				stack[len(stack)-1] = leftCorner
				stack = append(stack, rightCorner)
				break
			}
		}
	}

	group.numValues = len(group.sequenceCorners)
	return nil
}

func generateEdgebreakerPredictionDegreeSequence(ctx context.Context, base *edgebreakerMutableCornerTable, connectivity *edgebreakerAttributeConnectivity, group *edgebreakerAttributeDecoder, scratch *edgebreakerDecodeScratch) error {
	if group.decoderType == meshCornerAttribute {
		return fmt.Errorf("%w: prediction-degree traversal for corner attributes", ErrUnsupportedFeature)
	}

	var isFaceVisited []bool
	var isVertexVisited []bool
	var predictionDegree []int
	var stacks [3][]int
	if scratch != nil {
		isFaceVisited = growBoolSlice(scratch.sequenceVisitedFaces, base.FaceCount(), false)
		scratch.sequenceVisitedFaces = isFaceVisited
		isVertexVisited = growBoolSlice(scratch.sequenceVisitedVerts, len(group.vertexToEncoded), false)
		scratch.sequenceVisitedVerts = isVertexVisited
		predictionDegree = growIntSlice(scratch.predictionDegree, len(group.vertexToEncoded), 0)
		scratch.predictionDegree = predictionDegree
		for i := range stacks {
			stacks[i] = slices.Grow(scratch.predictionStacks[i][:0], base.FaceCount())
		}
	} else {
		isFaceVisited = make([]bool, base.FaceCount())
		isVertexVisited = make([]bool, len(group.vertexToEncoded))
		predictionDegree = make([]int, len(group.vertexToEncoded))
	}

	if scratch != nil {
		defer func() {
			for i := range stacks {
				scratch.predictionStacks[i] = stacks[i][:0]
			}
		}()
	}

	bestPriority := 0
	onNewVertex := func(vertex, corner int) {
		group.vertexToEncoded[vertex] = len(group.sequenceCorners)
		group.sequenceCorners = append(group.sequenceCorners, corner)
	}
	computePriority := func(corner int) int {
		tip, _, _ := edgebreakerCornerToVerts(base, connectivity, group, corner)
		priority := 0
		if !isVertexVisited[tip] {
			predictionDegree[tip]++
			if predictionDegree[tip] > 1 {
				priority = 1
			} else {
				priority = 2
			}
		}

		if priority >= len(stacks) {
			return len(stacks) - 1
		}

		return priority
	}
	popNextCorner := func() int {
		for priority := bestPriority; priority < len(stacks); priority++ {
			stack := stacks[priority]
			if len(stack) == 0 {
				continue
			}

			corner := stack[len(stack)-1]
			stacks[priority] = stack[:len(stack)-1]
			bestPriority = priority
			return corner
		}

		return -1
	}
	for faceID := 0; faceID < base.FaceCount(); faceID++ {
		if err := checkContextEvery(ctx, faceID); err != nil {
			return err
		}

		startCorner := faceID * 3
		if isFaceVisited[faceID] {
			continue
		}

		for i := range stacks {
			stacks[i] = stacks[i][:0]
		}

		stacks[0] = append(stacks[0], startCorner)
		bestPriority = 0
		tip, nextVert, prevVert := edgebreakerCornerToVerts(base, connectivity, group, startCorner)
		if !isVertexVisited[nextVert] {
			isVertexVisited[nextVert] = true
			onNewVertex(nextVert, base.Next(startCorner))
		}

		if !isVertexVisited[prevVert] {
			isVertexVisited[prevVert] = true
			onNewVertex(prevVert, base.Previous(startCorner))
		}

		if !isVertexVisited[tip] {
			isVertexVisited[tip] = true
			onNewVertex(tip, startCorner)
		}

		for corner := popNextCorner(); corner >= 0; corner = popNextCorner() {
			if currentFace := base.Face(corner); currentFace >= 0 && isFaceVisited[currentFace] {
				continue
			}

			for {
				currentFace := base.Face(corner)
				isFaceVisited[currentFace] = true
				vertex, _, _ := edgebreakerCornerToVerts(base, connectivity, group, corner)
				if !isVertexVisited[vertex] {
					isVertexVisited[vertex] = true
					onNewVertex(vertex, corner)
				}

				rightCorner := base.RightCorner(corner)
				leftCorner := base.LeftCorner(corner)
				rightVisited := rightCorner < 0 || isFaceVisited[base.Face(rightCorner)]
				leftVisited := leftCorner < 0 || isFaceVisited[base.Face(leftCorner)]
				if !leftVisited {
					priority := computePriority(leftCorner)
					if rightVisited && priority <= bestPriority {
						corner = leftCorner
						continue
					}

					stacks[priority] = append(stacks[priority], leftCorner)
					if priority < bestPriority {
						bestPriority = priority
					}
				}

				if !rightVisited {
					priority := computePriority(rightCorner)
					if priority <= bestPriority {
						corner = rightCorner
						continue
					}

					stacks[priority] = append(stacks[priority], rightCorner)
					if priority < bestPriority {
						bestPriority = priority
					}
				}

				break
			}
		}
	}

	group.numValues = len(group.sequenceCorners)
	return nil
}

func edgebreakerCornerToVerts(base *edgebreakerMutableCornerTable, connectivity *edgebreakerAttributeConnectivity, group *edgebreakerAttributeDecoder, corner int) (int, int, int) {
	if group.decoderType == meshCornerAttribute && connectivity != nil {
		return base.FaceVertexTriplet(corner, connectivity.cornerToVertex)
	}

	return base.FaceVertexTriplet(corner, nil)
}

func edgebreakerTraversalNeighbors(base *edgebreakerMutableCornerTable, connectivity *edgebreakerAttributeConnectivity, group *edgebreakerAttributeDecoder, corner int) (int, int) {
	rightCorner := base.RightCorner(corner)
	leftCorner := base.LeftCorner(corner)
	if group.decoderType == meshCornerAttribute && connectivity != nil {
		if connectivity.seamEdges[base.Next(corner)] {
			rightCorner = -1
		}

		if connectivity.seamEdges[base.Previous(corner)] {
			leftCorner = -1
		}
	}

	return rightCorner, leftCorner
}

func edgebreakerIsBoundary(base *edgebreakerMutableCornerTable, connectivity *edgebreakerAttributeConnectivity, group *edgebreakerAttributeDecoder, vertex, corner int) bool {
	baseVertex := base.Vertex(corner)
	onPositionBoundary := base.IsBoundaryVertex(baseVertex)
	if group.decoderType == meshCornerAttribute && connectivity != nil {
		return onPositionBoundary || connectivity.vertexOnSeam[baseVertex]
	}

	if connectivity != nil && group.dataID >= 0 {
		return onPositionBoundary || connectivity.vertexOnSeam[vertex]
	}

	return onPositionBoundary
}

func instantiateEdgebreakerAttributes(ctx context.Context, mesh *Mesh, base *edgebreakerMutableCornerTable, attrConnectivity []*edgebreakerAttributeConnectivity, pointToCorner []int, group *edgebreakerAttributeDecoder, options decodeConfig, scratch *edgebreakerDecodeScratch) error {
	var connectivity *edgebreakerAttributeConnectivity
	if group.dataID >= 0 {
		connectivity = attrConnectivity[group.dataID]
	}

	group.states = slices.Grow(group.states[:0], len(group.pending))
	group.states = group.states[:len(group.pending)]
	clear(group.states)
	for i, pending := range group.pending {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		attr, err := NewAttribute(pending.attrType, pending.dataType, pending.numComponents, group.numValues)
		if err != nil {
			return err
		}

		attr.Normalized = pending.normalized
		attr.UniqueID = pending.uniqueID
		if err := attr.SetExplicitMapping(mesh.PointCount()); err != nil {
			return err
		}

		mapping := attr.mapping
		for pointID, corner := range pointToCorner {
			if err := checkContextEvery(ctx, pointID); err != nil {
				return err
			}

			vertex, _, _ := edgebreakerCornerToVerts(base, connectivity, group, corner)
			entry := group.vertexToEncoded[vertex]
			if entry < 0 {
				return fmt.Errorf("%w: missing edgebreaker attribute sequence entry for point %d (corner=%d vertex=%d assigned=%d vertexSlots=%d dataID=%d decoderType=%d traversal=%d numValues=%d)", ErrInvalidGeometry, pointID, corner, vertex, countAssignedEdgebreakerVertices(group.vertexToEncoded), len(group.vertexToEncoded), group.dataID, group.decoderType, group.traversalMethod, group.numValues)
			}

			mapping[pointID] = uint32(entry)
		}

		attrID, err := mesh.addAttributeOwned(attr)
		if err != nil {
			return err
		}

		attr = mesh.attribute(attrID)
		copyPortableMapping := pending.attrType == AttributePosition || options.SkipTransform(pending.attrType)
		portableData, portableMapping, err := edgebreakerSequentialPortableScratch(scratch, pending, group.numValues, mesh.PointCount(), copyPortableMapping, options)
		if err != nil {
			return err
		}

		state, err := buildSequentialDecodedAttributeStateWithScratch(attr, group.numValues, mesh.PointCount(), pending.sequentialType, copyPortableMapping, portableData, portableMapping)
		if err != nil {
			return err
		}

		group.states[i] = state
	}

	return nil
}

func edgebreakerSequentialPortableScratch(scratch *edgebreakerDecodeScratch, pending edgebreakerPendingAttribute, numValues, pointCount int, copyPointMapping bool, options decodeConfig) ([]byte, []uint32, error) {
	if scratch == nil || options.SkipTransform(pending.attrType) {
		return nil, nil, nil
	}

	dataType, numComponents, ok := sequentialPortableScratchSchema(pending)
	if !ok {
		return nil, nil, nil
	}

	stride := DataTypeLength(dataType) * numComponents
	if stride == 0 {
		return nil, nil, fmt.Errorf("%w: invalid sequential portable type %s", ErrInvalidGeometry, dataType)
	}

	size, err := guardIntProductAllocation(numValues, stride, 1, "edgebreaker sequential portable scratch")
	if err != nil {
		return nil, nil, err
	}

	portableData := scratch.portableDataBuffer(size)
	var portableMapping []uint32
	if copyPointMapping {
		if _, err := guardIntProductAllocation(pointCount, 1, 4, "edgebreaker sequential portable mapping scratch"); err != nil {
			return nil, nil, err
		}

		portableMapping = scratch.portableMapBuffer(pointCount)
	}

	return portableData, portableMapping, nil
}

func sequentialPortableScratchSchema(pending edgebreakerPendingAttribute) (DataType, int, bool) {
	return sequentialPortableAttributeScratchSchema(pending.sequentialType, pending.numComponents)
}

func edgebreakerPredictionCornerTable(mesh *Mesh, base *edgebreakerMutableCornerTable, cornerToPoint []int, scratch *edgebreakerDecodeScratch) *topology.CornerTable {
	if mesh == nil || base == nil {
		return nil
	}

	cornerCount := len(mesh.faces) * 3
	var cornerVertex []int
	var vertexCorners []int
	if len(cornerToPoint) == cornerCount {
		cornerVertex = cornerToPoint
	} else if scratch != nil {
		cornerVertex = growIntSlice(scratch.predictionCornerVert, cornerCount, topology.InvalidCorner)
		scratch.predictionCornerVert = cornerVertex
	} else {
		cornerVertex = growIntSlice(nil, cornerCount, topology.InvalidCorner)
	}

	if scratch != nil {
		vertexCorners = growIntSlice(scratch.predictionVertCorner, mesh.PointCount(), topology.InvalidCorner)
		scratch.predictionVertCorner = vertexCorners
	} else {
		vertexCorners = growIntSlice(nil, mesh.PointCount(), topology.InvalidCorner)
	}

	if len(cornerToPoint) == cornerCount {
		for corner, point := range cornerVertex {
			if point >= 0 && point < len(vertexCorners) && vertexCorners[point] == topology.InvalidCorner {
				vertexCorners[point] = corner
			}
		}
	} else {
		for faceID, face := range mesh.faces {
			for local, pointID := range face {
				corner := faceID*3 + local
				point := int(pointID)
				cornerVertex[corner] = point
				if point >= 0 && point < len(vertexCorners) && vertexCorners[point] == topology.InvalidCorner {
					vertexCorners[point] = corner
				}
			}
		}
	}

	var table *topology.CornerTable
	if scratch != nil {
		table = topology.ResetCornerTableFromConnectivity(scratch.predictionTable, cornerVertex, base.opposite, vertexCorners)
		scratch.predictionTable = table
	} else {
		table = topology.NewCornerTableFromConnectivity(cornerVertex, base.opposite, vertexCorners)
	}

	opposite := base.opposite
	for corner, point := range cornerVertex {
		if point < 0 || point >= len(vertexCorners) {
			continue
		}

		if swingLeftCorner(opposite, corner) == topology.InvalidCorner {
			vertexCorners[point] = corner
		}
	}

	return table
}

func decodeEdgebreakerAttributeValues(ctx context.Context, reader *core.Reader, header bitstream.Header, mesh *Mesh, groups []*edgebreakerAttributeDecoder, options decodeConfig, predictionTable *topology.CornerTable, scratch *entropy.DecodeScratch) error {
	legacyTransformDataInline := header.VersionMajor < 2
	var decodedPortable [256]*Attribute
	predictionTables := &meshPredictionTableCache{mesh: mesh, table: predictionTable, ready: predictionTable != nil}
	for _, group := range groups {
		for stateIndex, state := range group.states {
			if err := checkContextEvery(ctx, stateIndex); err != nil {
				return err
			}

			switch state.decoderType {
			case bitstream.SequentialAttributeEncoderGeneric:
				stride := state.portable.ByteStride()
				for entry := 0; entry < group.numValues; entry++ {
					if err := checkContextEvery(ctx, entry); err != nil {
						return err
					}

					raw, err := reader.ReadBytesView(stride)
					if err != nil {
						return err
					}

					if err := state.portable.SetRawValue(entry, raw); err != nil {
						return fmt.Errorf("draco: edgebreaker generic decode entry=%d numValues=%d stride=%d: %w", entry, group.numValues, stride, err)
					}
				}
			case bitstream.SequentialAttributeEncoderInteger, bitstream.SequentialAttributeEncoderQuantization, bitstream.SequentialAttributeEncoderNormals:
				decodeLegacyTransformData := legacySequentialTransformDecoder(reader, state, legacyTransformDataInline)
				if err := decodeSequentialIntegerAttribute(ctx, reader, state.attr, state.portable, group.numValues, mesh, decodedPortable[AttributePosition], legacyTransformDataInline, decodeLegacyTransformData, predictionTables, scratch); err != nil {
					return fmt.Errorf("draco: edgebreaker attribute decode data_id=%d type=%s unique=%d encoder=%d values=%d: %w", group.dataID, state.attr.Type, state.attr.UniqueID, state.decoderType, group.numValues, err)
				}
			}

			decodedPortable[state.attr.Type] = state.portable
			if legacyTransformDataInline {
				if err := finalizeDecodedSequentialState(ctx, state, options); err != nil {
					return err
				}
			}
		}

		if legacyTransformDataInline {
			continue
		}

		for _, state := range group.states {
			if err := checkContext(ctx); err != nil {
				return err
			}

			if err := decodeSequentialTransformMetadata(reader, state); err != nil {
				return err
			}
		}

		for _, state := range group.states {
			if err := checkContext(ctx); err != nil {
				return err
			}

			if err := finalizeDecodedSequentialState(ctx, state, options); err != nil {
				return err
			}
		}
	}

	return nil
}
