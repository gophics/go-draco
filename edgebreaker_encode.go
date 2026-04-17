package draco

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"

	"github.com/gophics/go-draco/internal/bitstream"
	"github.com/gophics/go-draco/internal/core"
	"github.com/gophics/go-draco/internal/entropy"
	"github.com/gophics/go-draco/internal/topology"
	md "github.com/gophics/go-draco/metadata"
)

type edgebreakerTraversalEncoder interface {
	SetAttributeDataCount(int)
	Start()
	EncodeStartFaceConfiguration(bool)
	NewCornerReached(int)
	EncodeSymbol(uint32)
	EncodeAttributeSeam(int, bool)
	EncodedSymbolCount() int
	TraversalType() uint8
	RecordedSymbols() []uint32
	RecordedStartFaces() []bool
	RecordedSeams() [][]bool
	Write(context.Context, *core.Writer) error
}

type edgebreakerStandardTraversalEncoder struct {
	numAttributeData int
	startFaces       entropy.RansBitEncoder
	attributeSeams   []entropy.RansBitEncoder
	symbols          []uint32
	startFaceBits    []bool
	seamBits         [][]bool
}

func (e *edgebreakerStandardTraversalEncoder) SetAttributeDataCount(numData int) {
	e.numAttributeData = numData
}

func (e *edgebreakerStandardTraversalEncoder) Start() {
	e.startFaces.StartEncoding()
	e.attributeSeams = slices.Grow(e.attributeSeams[:0], e.numAttributeData)
	e.attributeSeams = e.attributeSeams[:e.numAttributeData]
	e.seamBits = slices.Grow(e.seamBits[:0], e.numAttributeData)
	e.seamBits = e.seamBits[:e.numAttributeData]
	for i := range e.seamBits {
		e.seamBits[i] = e.seamBits[i][:0]
	}

	for i := range e.attributeSeams {
		e.attributeSeams[i].StartEncoding()
	}

	e.symbols = e.symbols[:0]
	e.startFaceBits = e.startFaceBits[:0]
}

func (e *edgebreakerStandardTraversalEncoder) EncodeStartFaceConfiguration(interior bool) {
	e.startFaces.EncodeBit(interior)
	e.startFaceBits = append(e.startFaceBits, interior)
}

func (e *edgebreakerStandardTraversalEncoder) NewCornerReached(int) {}

func (e *edgebreakerStandardTraversalEncoder) EncodeSymbol(symbol uint32) {
	e.symbols = append(e.symbols, symbol)
}

func (e *edgebreakerStandardTraversalEncoder) EncodeAttributeSeam(attribute int, isSeam bool) {
	if attribute < 0 || attribute >= len(e.attributeSeams) {
		return
	}

	e.attributeSeams[attribute].EncodeBit(isSeam)
	e.seamBits[attribute] = append(e.seamBits[attribute], isSeam)
}

func (e *edgebreakerStandardTraversalEncoder) EncodedSymbolCount() int {
	return len(e.symbols)
}

func (e *edgebreakerStandardTraversalEncoder) TraversalType() uint8 {
	return edgebreakerTraversalStandard
}

func (e *edgebreakerStandardTraversalEncoder) RecordedSymbols() []uint32 {
	return e.symbols
}

func (e *edgebreakerStandardTraversalEncoder) RecordedStartFaces() []bool {
	return e.startFaceBits
}

func (e *edgebreakerStandardTraversalEncoder) RecordedSeams() [][]bool {
	return e.seamBits
}

func (e *edgebreakerStandardTraversalEncoder) Write(ctx context.Context, w *core.Writer) error {
	if err := writeEdgebreakerSymbolPayload(w, e.symbols, edgeBreakerBitPatternLength[:]); err != nil {
		return err
	}

	if err := e.startFaces.EndEncoding(w); err != nil {
		return err
	}

	for i := range e.attributeSeams {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		if err := e.attributeSeams[i].EndEncoding(w); err != nil {
			return err
		}
	}

	return nil
}

type edgebreakerValenceTraversalEncoder struct {
	base    edgebreakerStandardTraversalEncoder
	encoder *edgebreakerMeshEncoder
}

type edgebreakerPredictiveTraversalEncoder struct {
	table            *edgebreakerMutableCornerTable
	numAttributeData int
	startFaces       entropy.RansBitEncoder
	attributeSeams   []entropy.RansBitEncoder
	predictionBits   []bool
	startFaceBits    []bool
	seamBits         [][]bool
	explicitSymbols  []uint32
	fullSymbols      []uint32
	vertexValences   []int
	prevSymbol       int
	numSplitSymbols  int32
	lastCorner       int
	numSymbols       int
}

func newEdgebreakerPredictiveTraversalEncoder(table *edgebreakerMutableCornerTable) *edgebreakerPredictiveTraversalEncoder {
	return &edgebreakerPredictiveTraversalEncoder{
		table:      table,
		prevSymbol: -1,
		lastCorner: -1,
	}
}

func (e *edgebreakerPredictiveTraversalEncoder) SetAttributeDataCount(numData int) {
	e.numAttributeData = numData
}

func (e *edgebreakerPredictiveTraversalEncoder) Start() {
	e.startFaces.StartEncoding()
	e.attributeSeams = slices.Grow(e.attributeSeams[:0], e.numAttributeData)
	e.attributeSeams = e.attributeSeams[:e.numAttributeData]
	e.seamBits = slices.Grow(e.seamBits[:0], e.numAttributeData)
	e.seamBits = e.seamBits[:e.numAttributeData]
	for i := range e.seamBits {
		e.seamBits[i] = e.seamBits[i][:0]
	}

	for i := range e.attributeSeams {
		e.attributeSeams[i].StartEncoding()
	}

	e.startFaceBits = e.startFaceBits[:0]
	e.predictionBits = e.predictionBits[:0]
	e.explicitSymbols = e.explicitSymbols[:0]
	e.fullSymbols = e.fullSymbols[:0]
	e.numSplitSymbols = 0
	e.prevSymbol = -1
	e.lastCorner = -1
	e.numSymbols = 0
	e.vertexValences = slices.Grow(e.vertexValences[:0], e.table.VertexCount())
	e.vertexValences = e.vertexValences[:e.table.VertexCount()]
	clear(e.vertexValences)
	for vertex := 0; vertex < e.table.VertexCount(); vertex++ {
		e.vertexValences[vertex] = e.table.Valence(vertex)
	}
}

func (e *edgebreakerPredictiveTraversalEncoder) EncodeStartFaceConfiguration(interior bool) {
	e.startFaces.EncodeBit(interior)
	e.startFaceBits = append(e.startFaceBits, interior)
}

func (e *edgebreakerPredictiveTraversalEncoder) NewCornerReached(corner int) {
	e.lastCorner = corner
}

func (e *edgebreakerPredictiveTraversalEncoder) EncodeSymbol(symbol uint32) {
	e.fullSymbols = append(e.fullSymbols, symbol)
	e.numSymbols++

	predictedSymbol := -1
	if e.lastCorner >= 0 {
		next := e.table.Next(e.lastCorner)
		prev := e.table.Previous(e.lastCorner)
		switch symbol {
		case topologyC:
			predictedSymbol = e.computePredictedSymbol(e.table.Vertex(next))
			fallthrough
		case topologyS:
			e.vertexValences[e.table.Vertex(next)]--
			e.vertexValences[e.table.Vertex(prev)]--
			if symbol == topologyS {
				e.vertexValences[e.table.Vertex(e.lastCorner)] = -1
				e.numSplitSymbols++
			}
		case topologyR:
			predictedSymbol = e.computePredictedSymbol(e.table.Vertex(next))
			e.vertexValences[e.table.Vertex(e.lastCorner)]--
			e.vertexValences[e.table.Vertex(next)]--
			e.vertexValences[e.table.Vertex(prev)] -= 2
		case topologyL:
			e.vertexValences[e.table.Vertex(e.lastCorner)]--
			e.vertexValences[e.table.Vertex(next)] -= 2
			e.vertexValences[e.table.Vertex(prev)]--
		case topologyE:
			e.vertexValences[e.table.Vertex(e.lastCorner)] -= 2
			e.vertexValences[e.table.Vertex(next)] -= 2
			e.vertexValences[e.table.Vertex(prev)] -= 2
		}
	}

	storePrevSymbol := true
	if predictedSymbol != -1 {
		if predictedSymbol == e.prevSymbol {
			e.predictionBits = append(e.predictionBits, true)
			storePrevSymbol = false
		} else if e.prevSymbol != -1 {
			e.predictionBits = append(e.predictionBits, false)
		}
	}

	if storePrevSymbol && e.prevSymbol != -1 {
		e.explicitSymbols = append(e.explicitSymbols, uint32(e.prevSymbol))
	}

	e.prevSymbol = int(symbol)
}

func (e *edgebreakerPredictiveTraversalEncoder) computePredictedSymbol(pivot int) int {
	if pivot < 0 || pivot >= len(e.vertexValences) {
		return topologyInvalid
	}

	valence := e.vertexValences[pivot]
	if valence < 0 {
		return topologyInvalid
	}

	if valence < 6 {
		return topologyR
	}

	return topologyC
}

func (e *edgebreakerPredictiveTraversalEncoder) EncodeAttributeSeam(attribute int, isSeam bool) {
	if attribute < 0 || attribute >= len(e.attributeSeams) {
		return
	}

	e.attributeSeams[attribute].EncodeBit(isSeam)
	e.seamBits[attribute] = append(e.seamBits[attribute], isSeam)
}

func (e *edgebreakerPredictiveTraversalEncoder) EncodedSymbolCount() int {
	return e.numSymbols
}

func (e *edgebreakerPredictiveTraversalEncoder) TraversalType() uint8 {
	return edgebreakerTraversalPredictive
}

func (e *edgebreakerPredictiveTraversalEncoder) RecordedSymbols() []uint32 {
	return e.fullSymbols
}

func (e *edgebreakerPredictiveTraversalEncoder) RecordedStartFaces() []bool {
	return e.startFaceBits
}

func (e *edgebreakerPredictiveTraversalEncoder) RecordedSeams() [][]bool {
	return e.seamBits
}

func (e *edgebreakerPredictiveTraversalEncoder) Write(ctx context.Context, w *core.Writer) error {
	if e.prevSymbol != -1 {
		e.explicitSymbols = append(e.explicitSymbols, uint32(e.prevSymbol))
		e.prevSymbol = -1
	}

	if err := writeEdgebreakerSymbolPayload(w, e.explicitSymbols, edgeBreakerBitPatternLength[:]); err != nil {
		return err
	}

	if err := e.startFaces.EndEncoding(w); err != nil {
		return err
	}

	for i := range e.attributeSeams {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		if err := e.attributeSeams[i].EndEncoding(w); err != nil {
			return err
		}
	}

	if err := w.WriteInt32(e.numSplitSymbols); err != nil {
		return err
	}

	encoder := &entropy.RansBitEncoder{}
	encoder.StartEncoding()
	for i := len(e.predictionBits) - 1; i >= 0; i-- {
		if err := checkContextEvery(ctx, len(e.predictionBits)-1-i); err != nil {
			return err
		}

		encoder.EncodeBit(e.predictionBits[i])
	}

	return encoder.EndEncoding(w)
}

func newEdgebreakerValenceTraversalEncoder(encoder *edgebreakerMeshEncoder) *edgebreakerValenceTraversalEncoder {
	return &edgebreakerValenceTraversalEncoder{
		encoder: encoder,
	}
}

func (e *edgebreakerValenceTraversalEncoder) SetAttributeDataCount(numData int) {
	e.base.SetAttributeDataCount(numData)
}

func (e *edgebreakerValenceTraversalEncoder) Start() {
	e.base.Start()
}

func (e *edgebreakerValenceTraversalEncoder) EncodeStartFaceConfiguration(interior bool) {
	e.base.EncodeStartFaceConfiguration(interior)
}

func (e *edgebreakerValenceTraversalEncoder) NewCornerReached(int) {}

func (e *edgebreakerValenceTraversalEncoder) EncodeSymbol(symbol uint32) {
	e.base.EncodeSymbol(symbol)
}

func (e *edgebreakerValenceTraversalEncoder) EncodeAttributeSeam(attribute int, isSeam bool) {
	e.base.EncodeAttributeSeam(attribute, isSeam)
}

func (e *edgebreakerValenceTraversalEncoder) EncodedSymbolCount() int {
	return len(e.base.symbols)
}

func (e *edgebreakerValenceTraversalEncoder) TraversalType() uint8 {
	return edgebreakerTraversalValence
}

func (e *edgebreakerValenceTraversalEncoder) RecordedSymbols() []uint32 {
	return e.base.symbols
}

func (e *edgebreakerValenceTraversalEncoder) RecordedStartFaces() []bool {
	return e.base.startFaceBits
}

func (e *edgebreakerValenceTraversalEncoder) RecordedSeams() [][]bool {
	return e.base.seamBits
}

func (e *edgebreakerValenceTraversalEncoder) Write(ctx context.Context, w *core.Writer) error {
	if err := e.base.startFaces.EndEncoding(w); err != nil {
		return err
	}

	for i := range e.base.attributeSeams {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		if err := e.base.attributeSeams[i].EndEncoding(w); err != nil {
			return err
		}
	}

	contextSymbols, err := buildValenceContextSymbols(e.encoder, e.base.symbols, e.base.startFaceBits, e.base.seamBits)
	if err != nil {
		return err
	}

	for i := range contextSymbols {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		if err := guardEncodeUint32Value(len(contextSymbols[i]), "edgebreaker valence context symbol count"); err != nil {
			return err
		}

		if err := core.EncodeVarUint32(w, uint32(len(contextSymbols[i]))); err != nil {
			return err
		}

		if len(contextSymbols[i]) == 0 {
			continue
		}

		if err := entropy.EncodeSymbols(w, contextSymbols[i], 1, nil); err != nil {
			return err
		}
	}

	return nil
}

type edgebreakerAttributeData struct {
	attr         *Attribute
	dataID       int
	decoderType  uint8
	traversal    uint8
	connectivity *edgebreakerAttributeConnectivity
	sequence     []int
	vertexToCode []int
	state        sequentialEncodedAttribute
	encodedAttr  *Attribute
}

type edgebreakerMeshEncoder struct {
	ctx                       context.Context
	sourceMesh                *Mesh
	mesh                      *Mesh
	options                   encodeConfig
	scratch                   *edgebreakerEncodeScratch
	table                     *edgebreakerMutableCornerTable
	inputPointCorner          []int
	pointToCorner             []int
	pointToSourcePoint        []int
	decodedFaces              []Face
	decodedTable              *edgebreakerMutableCornerTable
	decodedConnectivity       []*edgebreakerAttributeConnectivity
	decodedCornerSource       []int
	connectivitySourceCorners []int
	canonicalFaces            []Face
	traversalSeedOrder        []int
	traversalSeedCursor       int
	nativeTraversalOrder      bool
	initFaceCorners           []int
	nonIsolatedVertices       int
	inputPointSignatures      [][]byte
	inputPointSignatureRanks  []int
	vertexHoleID              []int
	visitedHoles              []bool
	visitedFaces              []bool
	visitedVertices           []bool
	cornerTraversalStack      []int
	processedCorners          []int
	lastEncodedSymbolID       int
	numSplitSymbols           uint32
	splitEvents               []edgebreakerTopologySplit
	faceToSplitSymbol         []int
	attributeData             []edgebreakerAttributeData
	posGroup                  *edgebreakerAttributeData
	facesCanonical            bool
	splitOnSeams              bool
}

type edgebreakerEncodeScratch struct {
	faces                     []Face
	signatures                [][]byte
	signatureBytes            []byte
	signatureRanks            []int
	signatureOrder            []int
	inputPointCorner          []int
	vertexHoleID              []int
	visitedHoles              []bool
	visitedFaces              []bool
	visitedVertices           []bool
	cornerTraversalStack      []int
	processedCorners          []int
	initFaceCorners           []int
	connectivitySourceCorners []int
	splitEvents               []edgebreakerTopologySplit
	attributeData             []edgebreakerAttributeData
	faceToSplitSymbol         []int
	orientationEdges          []edgebreakerFaceEdge
	orientationVisited        []bool
	orientationFlipped        []bool
	orientationComponent      []int
	orientationQueue          []int
	orientationCurrent        []Face
	orientationInverted       []Face
	orientationNeighbors      []edgebreakerOrientationNeighbor
	orientationDenseEdges     []edgebreakerFaceEdge
	orientationDenseCounts    []uint8
	orientationDenseTouched   []int
	pointToSourcePoint        []int
	attrConnectivity          []*edgebreakerAttributeConnectivity
	connectivityPool          []*edgebreakerAttributeConnectivity
	decodedConnectivityPool   []*edgebreakerAttributeConnectivity
	baseTable                 *edgebreakerMutableCornerTable
	baseFaces                 []topology.Face
	baseCornerCounts          []int
	baseVertexOffsets         []int
	baseVertexEdges           []edgebreakerHalfEdge
	baseVisitedVertices       []bool
	baseVisitedCorners        []bool
	decodedTable              *edgebreakerMutableCornerTable
	canonicalFaces            []Face
	traversalSeedOrder        []int
	sourceToDecodedCorner     []int
	sourceVertexToDecoded     []int
	decodedSourcePoints       []int
	isVertHole                []bool
	decodedCornerSource       []int
	recordedSeamPos           []int
	canonicalCache            edgebreakerCanonicalMeshCache
	baseCache                 edgebreakerBaseTableCache
	decodedMappingCache       edgebreakerDecodedMappingCache
	sequenceScratch           sequentialEncodeScratch
	decodeScratch             edgebreakerDecodeScratch
	standardTraversal         edgebreakerStandardTraversalEncoder
	predictiveTraversal       edgebreakerPredictiveTraversalEncoder
	valenceTraversal          edgebreakerValenceTraversalEncoder
}

type edgebreakerCanonicalMeshCache struct {
	valid        bool
	mesh         *Mesh
	splitOnSeams bool
	pointCount   int
	faceCount    int
	attrs        []*Attribute
	faces        []Face
	ranks        []int
}

func (c *edgebreakerCanonicalMeshCache) match(mesh *Mesh, splitOnSeams bool) bool {
	if c == nil || !c.valid || c.mesh != mesh || c.splitOnSeams != splitOnSeams {
		return false
	}

	if mesh == nil || c.pointCount != mesh.PointCount() || c.faceCount != mesh.FaceCount() || len(c.attrs) != len(mesh.attributes) {
		return false
	}

	for i, attr := range mesh.attributes {
		if c.attrs[i] != attr {
			return false
		}
	}

	return true
}

func (c *edgebreakerCanonicalMeshCache) store(mesh *Mesh, splitOnSeams bool, faces []Face, ranks []int) {
	if c == nil || mesh == nil {
		return
	}

	c.valid = true
	c.mesh = mesh
	c.splitOnSeams = splitOnSeams
	c.pointCount = mesh.PointCount()
	c.faceCount = mesh.FaceCount()
	c.attrs = slices.Grow(c.attrs[:0], len(mesh.attributes))
	c.attrs = c.attrs[:len(mesh.attributes)]
	copy(c.attrs, mesh.attributes)
	c.faces = slices.Grow(c.faces[:0], len(faces))
	c.faces = c.faces[:len(faces)]
	copy(c.faces, faces)
	c.ranks = slices.Grow(c.ranks[:0], len(ranks))
	c.ranks = c.ranks[:len(ranks)]
	copy(c.ranks, ranks)
}

type edgebreakerBaseTableCache struct {
	valid            bool
	mesh             *Mesh
	splitOnSeams     bool
	canonicalInput   bool
	pointCount       int
	faceCount        int
	attrs            []*Attribute
	inputPointCorner []int
	table            *edgebreakerMutableCornerTable
	vertexHoleID     []int
	holeCount        int
}

func (c *edgebreakerBaseTableCache) match(mesh *Mesh, splitOnSeams, canonicalInput bool) bool {
	if c == nil || !c.valid || c.mesh != mesh || c.splitOnSeams != splitOnSeams || c.canonicalInput != canonicalInput {
		return false
	}

	if mesh == nil || c.pointCount != mesh.PointCount() || c.faceCount != mesh.FaceCount() || len(c.attrs) != len(mesh.attributes) {
		return false
	}

	for i, attr := range mesh.attributes {
		if c.attrs[i] != attr {
			return false
		}
	}

	return c.table != nil
}

func (c *edgebreakerBaseTableCache) store(mesh *Mesh, splitOnSeams, canonicalInput bool, inputPointCorner []int, table *edgebreakerMutableCornerTable, vertexHoleID []int, holeCount int) {
	if c == nil || mesh == nil || table == nil {
		return
	}

	c.valid = true
	c.mesh = mesh
	c.splitOnSeams = splitOnSeams
	c.canonicalInput = canonicalInput
	c.pointCount = mesh.PointCount()
	c.faceCount = mesh.FaceCount()
	c.attrs = slices.Grow(c.attrs[:0], len(mesh.attributes))
	c.attrs = c.attrs[:len(mesh.attributes)]
	copy(c.attrs, mesh.attributes)
	c.inputPointCorner = slices.Grow(c.inputPointCorner[:0], len(inputPointCorner))
	c.inputPointCorner = c.inputPointCorner[:len(inputPointCorner)]
	copy(c.inputPointCorner, inputPointCorner)
	c.table = cloneEdgebreakerMutableCornerTable(c.table, table)
	c.vertexHoleID = slices.Grow(c.vertexHoleID[:0], len(vertexHoleID))
	c.vertexHoleID = c.vertexHoleID[:len(vertexHoleID)]
	copy(c.vertexHoleID, vertexHoleID)
	c.holeCount = holeCount
}

func cloneEdgebreakerMutableCornerTable(dst, src *edgebreakerMutableCornerTable) *edgebreakerMutableCornerTable {
	if src == nil {
		return nil
	}

	if dst == nil {
		dst = &edgebreakerMutableCornerTable{}
	}

	dst.numFaces = src.numFaces
	dst.maxVertices = src.maxVertices
	dst.numVertices = src.numVertices
	dst.trackVertexCorners = src.trackVertexCorners
	dst.cornerVertex = slices.Grow(dst.cornerVertex[:0], len(src.cornerVertex))
	dst.cornerVertex = dst.cornerVertex[:len(src.cornerVertex)]
	copy(dst.cornerVertex, src.cornerVertex)
	dst.opposite = slices.Grow(dst.opposite[:0], len(src.opposite))
	dst.opposite = dst.opposite[:len(src.opposite)]
	copy(dst.opposite, src.opposite)
	dst.vertexCorners = slices.Grow(dst.vertexCorners[:0], len(src.vertexCorners))
	dst.vertexCorners = dst.vertexCorners[:len(src.vertexCorners)]
	copy(dst.vertexCorners, src.vertexCorners)
	dst.vertexCornerNext = slices.Grow(dst.vertexCornerNext[:0], len(src.vertexCornerNext))
	dst.vertexCornerNext = dst.vertexCornerNext[:len(src.vertexCornerNext)]
	copy(dst.vertexCornerNext, src.vertexCornerNext)
	dst.vertexCornerHead = slices.Grow(dst.vertexCornerHead[:0], len(src.vertexCornerHead))
	dst.vertexCornerHead = dst.vertexCornerHead[:len(src.vertexCornerHead)]
	copy(dst.vertexCornerHead, src.vertexCornerHead)
	return dst
}

type edgebreakerDecodedMappingCache struct {
	valid               bool
	mesh                *Mesh
	splitOnSeams        bool
	canonicalInput      bool
	traversalMethod     EdgebreakerMethod
	pointCount          int
	faceCount           int
	attrs               []*Attribute
	pointToCorner       []int
	pointToSourcePoint  []int
	decodedFaces        []Face
	decodedTable        *edgebreakerMutableCornerTable
	decodedConnectivity []*edgebreakerAttributeConnectivity
	decodedCornerSource []int
}

func (c *edgebreakerDecodedMappingCache) match(mesh *Mesh, splitOnSeams, canonicalInput bool, traversalMethod EdgebreakerMethod) bool {
	if c == nil || !c.valid || c.mesh != mesh || c.splitOnSeams != splitOnSeams || c.canonicalInput != canonicalInput || c.traversalMethod != traversalMethod {
		return false
	}

	if mesh == nil || c.pointCount != mesh.PointCount() || c.faceCount != mesh.FaceCount() || len(c.attrs) != len(mesh.attributes) {
		return false
	}

	for i, attr := range mesh.attributes {
		if c.attrs[i] != attr {
			return false
		}
	}

	return c.decodedTable != nil
}

func (c *edgebreakerDecodedMappingCache) store(mesh *Mesh, splitOnSeams, canonicalInput bool, traversalMethod EdgebreakerMethod, encoder *edgebreakerMeshEncoder) {
	if c == nil || mesh == nil || encoder == nil || encoder.decodedTable == nil {
		return
	}

	c.valid = true
	c.mesh = mesh
	c.splitOnSeams = splitOnSeams
	c.canonicalInput = canonicalInput
	c.traversalMethod = traversalMethod
	c.pointCount = mesh.PointCount()
	c.faceCount = mesh.FaceCount()
	c.attrs = slices.Grow(c.attrs[:0], len(mesh.attributes))
	c.attrs = c.attrs[:len(mesh.attributes)]
	copy(c.attrs, mesh.attributes)
	c.pointToCorner = cloneIntSlice(c.pointToCorner, encoder.pointToCorner)
	c.pointToSourcePoint = cloneIntSlice(c.pointToSourcePoint, encoder.pointToSourcePoint)
	c.decodedFaces = cloneFaceSlice(c.decodedFaces, encoder.decodedFaces)
	c.decodedTable = cloneEdgebreakerMutableCornerTable(c.decodedTable, encoder.decodedTable)
	c.decodedConnectivity = cloneEdgebreakerAttributeConnectivitySlice(c.decodedConnectivity, encoder.decodedConnectivity)
	c.decodedCornerSource = cloneIntSlice(c.decodedCornerSource, encoder.decodedCornerSource)
}

func cloneIntSlice(dst, src []int) []int {
	dst = slices.Grow(dst[:0], len(src))
	dst = dst[:len(src)]
	copy(dst, src)
	return dst
}

func cloneEdgebreakerAttributeConnectivitySlice(dst []*edgebreakerAttributeConnectivity, src []*edgebreakerAttributeConnectivity) []*edgebreakerAttributeConnectivity {
	dst = slices.Grow(dst[:0], len(src))
	dst = dst[:len(src)]
	for i, connectivity := range src {
		dst[i] = cloneEdgebreakerAttributeConnectivity(dst[i], connectivity)
	}

	return dst
}

func cloneEdgebreakerAttributeConnectivity(dst, src *edgebreakerAttributeConnectivity) *edgebreakerAttributeConnectivity {
	if src == nil {
		return nil
	}

	if dst == nil {
		dst = &edgebreakerAttributeConnectivity{}
	}

	dst.seamEdges = growBoolSlice(dst.seamEdges, len(src.seamEdges), false)
	copy(dst.seamEdges, src.seamEdges)
	dst.vertexOnSeam = growBoolSlice(dst.vertexOnSeam, len(src.vertexOnSeam), false)
	copy(dst.vertexOnSeam, src.vertexOnSeam)
	dst.cornerToVertex = cloneIntSlice(dst.cornerToVertex, src.cornerToVertex)
	dst.leftMostCorners = cloneIntSlice(dst.leftMostCorners, src.leftMostCorners)
	dst.numVertices = src.numVertices
	dst.noInteriorSeams = src.noInteriorSeams
	return dst
}

func (s *edgebreakerEncodeScratch) reset() {
	if s == nil {
		return
	}

	clear(s.signatures)
	s.signatures = resetScratchSlice(s.signatures)
	s.signatureBytes = resetScratchSlice(s.signatureBytes)
	s.signatureRanks = resetScratchSlice(s.signatureRanks)
	s.signatureOrder = resetScratchSlice(s.signatureOrder)
	s.faces = resetScratchSlice(s.faces)
	s.inputPointCorner = resetScratchSlice(s.inputPointCorner)
	s.vertexHoleID = resetScratchSlice(s.vertexHoleID)
	s.visitedHoles = resetScratchSlice(s.visitedHoles)

	s.visitedFaces = resetScratchSlice(s.visitedFaces)
	s.visitedVertices = resetScratchSlice(s.visitedVertices)
	s.cornerTraversalStack = resetScratchSlice(s.cornerTraversalStack)
	s.processedCorners = resetScratchSlice(s.processedCorners)
	s.initFaceCorners = resetScratchSlice(s.initFaceCorners)
	s.connectivitySourceCorners = resetScratchSlice(s.connectivitySourceCorners)
	s.splitEvents = resetScratchSlice(s.splitEvents)
	clear(s.attributeData)
	s.attributeData = resetScratchSlice(s.attributeData)
	s.pointToSourcePoint = resetScratchSlice(s.pointToSourcePoint)

	for _, connectivity := range s.attrConnectivity {
		connectivity.resetScratch()
	}

	for _, connectivity := range s.connectivityPool {
		connectivity.resetScratch()
	}

	for _, connectivity := range s.decodedConnectivityPool {
		connectivity.resetScratch()
	}

	s.attrConnectivity = resetScratchSlice(s.attrConnectivity)
	s.connectivityPool = resetScratchSlice(s.connectivityPool)
	s.decodedConnectivityPool = resetScratchSlice(s.decodedConnectivityPool)

	s.baseFaces = resetScratchSlice(s.baseFaces)
	s.baseCornerCounts = resetScratchSlice(s.baseCornerCounts)
	s.baseVertexOffsets = resetScratchSlice(s.baseVertexOffsets)
	s.baseVertexEdges = resetScratchSlice(s.baseVertexEdges)
	s.baseVisitedVertices = resetScratchSlice(s.baseVisitedVertices)
	s.baseVisitedCorners = resetScratchSlice(s.baseVisitedCorners)
	s.canonicalFaces = resetScratchSlice(s.canonicalFaces)
	s.traversalSeedOrder = resetScratchSlice(s.traversalSeedOrder)

	s.sourceToDecodedCorner = resetScratchSlice(s.sourceToDecodedCorner)
	s.sourceVertexToDecoded = resetScratchSlice(s.sourceVertexToDecoded)
	s.decodedSourcePoints = resetScratchSlice(s.decodedSourcePoints)
	s.isVertHole = resetScratchSlice(s.isVertHole)
	s.decodedCornerSource = resetScratchSlice(s.decodedCornerSource)
	s.recordedSeamPos = resetScratchSlice(s.recordedSeamPos)

	s.sequenceScratch.reset()
	s.decodeScratch.reset()
	s.faceToSplitSymbol = resetScratchSlice(s.faceToSplitSymbol)

	s.orientationEdges = resetScratchSlice(s.orientationEdges)
	s.orientationVisited = resetScratchSlice(s.orientationVisited)
	s.orientationFlipped = resetScratchSlice(s.orientationFlipped)
	s.orientationComponent = resetScratchSlice(s.orientationComponent)
	s.orientationQueue = resetScratchSlice(s.orientationQueue)
	s.orientationCurrent = resetScratchSlice(s.orientationCurrent)
	s.orientationInverted = resetScratchSlice(s.orientationInverted)
	s.orientationNeighbors = resetScratchSlice(s.orientationNeighbors)
	s.orientationDenseEdges = resetScratchSlice(s.orientationDenseEdges)
	s.orientationDenseCounts = resetScratchSlice(s.orientationDenseCounts)
	s.orientationDenseTouched = resetScratchSlice(s.orientationDenseTouched)

	s.predictiveTraversal.table = nil
	s.valenceTraversal.encoder = nil
}

func growIntSlice(buf []int, size int, fill int) []int {
	buf = slices.Grow(buf[:0], size)
	buf = buf[:size]
	if fill == 0 {
		clear(buf)
		return buf
	}

	for i := range buf {
		buf[i] = fill
	}

	return buf
}

func growBoolSlice(buf []bool, size int, value bool) []bool {
	buf = slices.Grow(buf[:0], size)
	buf = buf[:size]
	if value {
		for i := range buf {
			buf[i] = true
		}

		return buf
	}

	clear(buf)
	return buf
}

func growEdgebreakerHalfEdges(buf []edgebreakerHalfEdge, size int) []edgebreakerHalfEdge {
	buf = slices.Grow(buf[:0], size)
	buf = buf[:size]
	for i := range buf {
		buf[i].sinkVertex = -1
		buf[i].edgeCorner = -1
	}

	return buf
}

func cloneFaceSlice(buf []Face, src []Face) []Face {
	buf = slices.Grow(buf[:0], len(src))
	buf = buf[:len(src)]
	copy(buf, src)
	return buf
}

func encodeEdgebreakerMesh(ctx context.Context, w *core.Writer, mesh *Mesh, options encodeConfig, scratch *edgebreakerEncodeScratch) error {
	if mesh.namedAttribute(AttributePosition) == nil {
		return fmt.Errorf("%w: edgebreaker mesh encoding requires position attribute", ErrUnsupportedFeature)
	}

	impl, err := newEdgebreakerMeshEncoder(ctx, mesh, options, scratch)
	if err != nil {
		return err
	}

	return impl.encode(w)
}

func newEdgebreakerMeshEncoder(ctx context.Context, mesh *Mesh, options encodeConfig, scratch *edgebreakerEncodeScratch) (*edgebreakerMeshEncoder, error) {
	splitOnSeams := shouldSplitEdgebreakerMeshOnSeams(mesh, options)
	nativeTraversalOrder := shouldUseNativeEdgebreakerTraversalOrder(options)
	sourceMesh := mesh
	canonicalMesh, signatures, signatureRanks, facesCanonical, err := canonicalizeEdgebreakerWorkingMesh(mesh, splitOnSeams && !nativeTraversalOrder, scratch)
	if err != nil {
		return nil, err
	}

	mesh = canonicalMesh
	inputPointCorner, baseTable, vertexHoleID, visitedHoles, err := prepareEdgebreakerBaseEncodingState(sourceMesh, mesh, splitOnSeams, facesCanonical, scratch)
	if err != nil {
		return nil, err
	}

	var visitedFaces []bool
	var visitedVertices []bool
	var faceToSplitSymbol []int
	var splitEvents []edgebreakerTopologySplit
	var attributeData []edgebreakerAttributeData
	var processedCorners []int
	var cornerTraversalStack []int
	var initFaceCorners []int
	var connectivitySourceCorners []int
	faceCount := baseTable.FaceCount()
	if scratch != nil {
		visitedFaces = growBoolSlice(scratch.visitedFaces, faceCount, false)
		scratch.visitedFaces = visitedFaces
		visitedVertices = growBoolSlice(scratch.visitedVertices, baseTable.VertexCount(), false)
		scratch.visitedVertices = visitedVertices
		scratch.faceToSplitSymbol = growIntSlice(scratch.faceToSplitSymbol, faceCount, -1)
		faceToSplitSymbol = scratch.faceToSplitSymbol
		splitEvents = slices.Grow(scratch.splitEvents[:0], faceCount/8)
		scratch.splitEvents = splitEvents
		clear(scratch.attributeData)
		attributeData = scratch.attributeData[:0]
		scratch.attributeData = attributeData
		processedCorners = slices.Grow(scratch.processedCorners[:0], faceCount)
		scratch.processedCorners = processedCorners
		cornerTraversalStack = slices.Grow(scratch.cornerTraversalStack[:0], faceCount)
		scratch.cornerTraversalStack = cornerTraversalStack
		initFaceCorners = slices.Grow(scratch.initFaceCorners[:0], faceCount/8)
		scratch.initFaceCorners = initFaceCorners
		connectivitySourceCorners = slices.Grow(scratch.connectivitySourceCorners[:0], faceCount)
		scratch.connectivitySourceCorners = connectivitySourceCorners
	} else {
		visitedFaces = make([]bool, faceCount)
		visitedVertices = make([]bool, baseTable.VertexCount())
		faceToSplitSymbol = growIntSlice(nil, faceCount, -1)
		splitEvents = make([]edgebreakerTopologySplit, 0, faceCount/8)
		processedCorners = make([]int, 0, faceCount)
		cornerTraversalStack = make([]int, 0, faceCount)
		initFaceCorners = make([]int, 0, faceCount/8)
		connectivitySourceCorners = make([]int, 0, faceCount)
	}

	out := &edgebreakerMeshEncoder{
		ctx:                       ctx,
		sourceMesh:                sourceMesh,
		mesh:                      mesh,
		options:                   options,
		scratch:                   scratch,
		table:                     baseTable,
		inputPointCorner:          inputPointCorner,
		vertexHoleID:              vertexHoleID,
		visitedHoles:              visitedHoles,
		visitedFaces:              visitedFaces,
		visitedVertices:           visitedVertices,
		lastEncodedSymbolID:       -1,
		faceToSplitSymbol:         faceToSplitSymbol,
		splitEvents:               splitEvents,
		attributeData:             attributeData,
		processedCorners:          processedCorners,
		cornerTraversalStack:      cornerTraversalStack,
		initFaceCorners:           initFaceCorners,
		connectivitySourceCorners: connectivitySourceCorners,
		facesCanonical:            facesCanonical,
		nativeTraversalOrder:      nativeTraversalOrder,
		splitOnSeams:              splitOnSeams,
	}
	if !nativeTraversalOrder && !facesCanonical {
		signatures, err = edgebreakerPointSignatures(&mesh.PointCloud, scratch)
		if err != nil {
			return nil, err
		}

		signatureRanks = edgebreakerPointSignatureRanks(signatures, scratch)
	}

	out.inputPointSignatures = signatures
	out.inputPointSignatureRanks = signatureRanks
	if !nativeTraversalOrder && !facesCanonical {
		if scratch != nil {
			out.canonicalFaces = slices.Grow(scratch.canonicalFaces[:0], len(mesh.faces))
			out.canonicalFaces = out.canonicalFaces[:len(mesh.faces)]
			scratch.canonicalFaces = out.canonicalFaces
		} else {
			out.canonicalFaces = make([]Face, len(mesh.faces))
		}

		for faceID, face := range mesh.faces {
			out.canonicalFaces[faceID] = canonicalizeFaceBySignatureRanks(signatureRanks, face)
		}
	}

	if !nativeTraversalOrder {
		out.traversalSeedOrder = out.buildTraversalSeedOrder()
	}

	if err := out.initAttributeData(); err != nil {
		return nil, err
	}

	return out, nil
}

func prepareEdgebreakerBaseEncodingState(sourceMesh, mesh *Mesh, splitOnSeams, canonicalInput bool, scratch *edgebreakerEncodeScratch) ([]int, *edgebreakerMutableCornerTable, []int, []bool, error) {
	if scratch != nil && scratch.baseCache.match(sourceMesh, splitOnSeams, canonicalInput) {
		visitedHoles := growBoolSlice(scratch.visitedHoles, scratch.baseCache.holeCount, false)
		scratch.visitedHoles = visitedHoles
		return scratch.baseCache.inputPointCorner, scratch.baseCache.table, scratch.baseCache.vertexHoleID, visitedHoles, nil
	}

	var inputPointCorner []int
	if scratch != nil {
		inputPointCorner = growIntSlice(scratch.inputPointCorner, mesh.PointCount(), -1)
		scratch.inputPointCorner = inputPointCorner
	} else {
		inputPointCorner = growIntSlice(nil, mesh.PointCount(), -1)
	}

	for faceID, face := range mesh.faces {
		for local, pointID := range face {
			if inputPointCorner[pointID] < 0 {
				inputPointCorner[pointID] = faceID*3 + local
			}
		}
	}

	baseTable, err := buildEdgebreakerBaseTable(mesh, splitOnSeams, scratch)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	vertexHoleID, visitedHoles, err := findEdgebreakerHoles(baseTable, scratch)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	if scratch != nil {
		scratch.baseCache.store(sourceMesh, splitOnSeams, canonicalInput, inputPointCorner, baseTable, vertexHoleID, len(visitedHoles))
	}

	return inputPointCorner, baseTable, vertexHoleID, visitedHoles, nil
}

func shouldSplitEdgebreakerMeshOnSeams(mesh *Mesh, options encodeConfig) bool {
	if enabled, ok := options.splitMeshOnSeamsEnabled(); ok {
		return enabled
	}

	position := mesh.namedAttribute(AttributePosition)
	if position != nil && (!position.IsIdentityMapping() || position.EntryCount() != mesh.PointCount()) {
		// Preserve meshes that already encode seam-split points instead of
		// collapsing them back to shared position entries.
		return true
	}

	return options.Speed() >= 6
}

func shouldUseNativeEdgebreakerTraversalOrder(options encodeConfig) bool {
	return options.IsSpeedSet() && options.Speed() >= 10
}

func canonicalizeEdgebreakerWorkingMesh(mesh *Mesh, splitOnSeams bool, scratch *edgebreakerEncodeScratch) (*Mesh, [][]byte, []int, bool, error) {
	if mesh == nil {
		return nil, nil, nil, false, fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if !splitOnSeams {
		return mesh, nil, nil, false, nil
	}

	if scratch != nil && scratch.canonicalCache.match(mesh, splitOnSeams) {
		out := *mesh
		out.faces = scratch.canonicalCache.faces
		return &out, nil, scratch.canonicalCache.ranks, true, nil
	}

	out := shallowCloneMeshForEdgebreaker(mesh, scratch)
	signatures, err := edgebreakerCanonicalPointSignatures(&out.PointCloud, splitOnSeams, scratch)
	if err != nil {
		return nil, nil, nil, false, err
	}

	signatureRanks := edgebreakerPointSignatureRanks(signatures, scratch)
	if err := canonicalizeEdgebreakerFaceOrientations(out.faces, signatureRanks, scratch); err != nil {
		return nil, nil, nil, false, err
	}

	for i, face := range out.faces {
		out.faces[i] = canonicalizeFaceBySignatureRanks(signatureRanks, face)
	}

	slices.SortFunc(out.faces, func(a, b Face) int {
		if cmp := compareCanonicalFaceSequenceRanks(signatureRanks, a, b); cmp != 0 {
			return cmp
		}

		return compareActualFace(a, b)
	})
	if scratch != nil {
		scratch.canonicalCache.store(mesh, splitOnSeams, out.faces, signatureRanks)
	}

	return out, signatures, signatureRanks, true, nil
}

func shallowCloneMeshForEdgebreaker(mesh *Mesh, scratch *edgebreakerEncodeScratch) *Mesh {
	if mesh == nil {
		return nil
	}

	out := *mesh
	if scratch != nil {
		out.faces = cloneFaceSlice(scratch.faces, mesh.faces)
		scratch.faces = out.faces
	} else {
		out.faces = append([]Face(nil), mesh.faces...)
	}

	return &out
}

func edgebreakerCanonicalPointSignatures(pc *PointCloud, splitOnSeams bool, scratch *edgebreakerEncodeScratch) ([][]byte, error) {
	if splitOnSeams {
		return edgebreakerPointSignatures(pc, scratch)
	}

	position := pc.namedAttribute(AttributePosition)
	if position == nil {
		return nil, fmt.Errorf("%w: mesh position attribute missing", ErrInvalidGeometry)
	}

	var signatures [][]byte
	if scratch != nil {
		signatures = slices.Grow(scratch.signatures[:0], pc.PointCount())
		signatures = signatures[:pc.PointCount()]
		clear(signatures)
		scratch.signatures = signatures
	} else {
		signatures = make([][]byte, pc.PointCount())
	}

	if scratch != nil {
		signatureBytes := slices.Grow(scratch.signatureBytes[:0], pc.PointCount()*4)
		signatureBytes = signatureBytes[:0]
		for pointID := 0; pointID < pc.PointCount(); pointID++ {
			start := len(signatureBytes)
			var mapped [4]byte
			binary.LittleEndian.PutUint32(mapped[:], position.mappedIndex(pointID))
			signatureBytes = append(signatureBytes, mapped[:]...)
			signatures[pointID] = signatureBytes[start:len(signatureBytes):len(signatureBytes)]
		}

		scratch.signatureBytes = signatureBytes
		return signatures, nil
	}

	for pointID := 0; pointID < pc.PointCount(); pointID++ {
		var mapped [4]byte
		binary.LittleEndian.PutUint32(mapped[:], position.mappedIndex(pointID))
		signatures[pointID] = append([]byte(nil), mapped[:]...)
	}

	return signatures, nil
}

func edgebreakerPointSignatures(pc *PointCloud, scratch *edgebreakerEncodeScratch) ([][]byte, error) {
	if scratch == nil {
		return pointSignatures(pc)
	}

	signatures := slices.Grow(scratch.signatures[:0], pc.PointCount())
	signatures = signatures[:pc.PointCount()]
	clear(signatures)
	scratch.signatures = signatures
	bytesPerPoint := len(pc.attributes) * 4
	for _, attr := range pc.attributes {
		bytesPerPoint += attr.ByteStride()
	}

	signatureBytes := slices.Grow(scratch.signatureBytes[:0], pc.PointCount()*bytesPerPoint)
	signatureBytes = signatureBytes[:0]
	for pointID := 0; pointID < pc.PointCount(); pointID++ {
		start := len(signatureBytes)
		for attrID, attr := range pc.attributes {
			entryID := int(attr.mappedIndex(pointID))
			raw, err := attr.rawEntry(entryID)
			if err != nil {
				return nil, fmt.Errorf("draco: point %d attribute %d: %w", pointID, attrID, err)
			}

			signatureBytes = appendLengthPrefixed(signatureBytes, raw)
		}

		signatures[pointID] = signatureBytes[start:len(signatureBytes):len(signatureBytes)]
	}

	scratch.signatureBytes = signatureBytes
	return signatures, nil
}

func compareLengthPrefixedSignature(a, b []byte) int {
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}

		return 1
	}

	return bytes.Compare(a, b)
}

func edgebreakerPointSignatureRanks(signatures [][]byte, scratch *edgebreakerEncodeScratch) []int {
	var order []int
	var ranks []int
	if scratch != nil {
		order = slices.Grow(scratch.signatureOrder[:0], len(signatures))
		order = order[:len(signatures)]
		scratch.signatureOrder = order
		ranks = slices.Grow(scratch.signatureRanks[:0], len(signatures))
		ranks = ranks[:len(signatures)]
		scratch.signatureRanks = ranks
	} else {
		order = make([]int, len(signatures))
		ranks = make([]int, len(signatures))
	}

	for i := range order {
		order[i] = i
	}

	slices.SortFunc(order, func(a, b int) int {
		return compareLengthPrefixedSignature(signatures[a], signatures[b])
	})

	rank := 0
	for i, pointID := range order {
		if i > 0 && compareLengthPrefixedSignature(signatures[pointID], signatures[order[i-1]]) != 0 {
			rank++
		}

		ranks[pointID] = rank
	}

	return ranks
}

func compareCanonicalFaceSequenceRanks(ranks []int, a, b Face) int {
	aRank := ranks[a[0]]
	bRank := ranks[b[0]]
	if aRank < bRank {
		return -1
	}

	if aRank > bRank {
		return 1
	}

	aRank = ranks[a[1]]
	bRank = ranks[b[1]]
	if aRank < bRank {
		return -1
	}

	if aRank > bRank {
		return 1
	}

	aRank = ranks[a[2]]
	bRank = ranks[b[2]]
	if aRank < bRank {
		return -1
	}

	if aRank > bRank {
		return 1
	}

	return 0
}

func canonicalizeFaceBySignatureRanks(ranks []int, face Face) Face {
	bestFace := face
	rotatedFace := Face{face[1], face[2], face[0]}
	if cmp := compareCanonicalFaceSequenceRanks(ranks, rotatedFace, bestFace); cmp < 0 || (cmp == 0 && lessActualFace(rotatedFace, bestFace)) {
		bestFace = rotatedFace
	}

	rotatedFace = Face{face[2], face[0], face[1]}
	if cmp := compareCanonicalFaceSequenceRanks(ranks, rotatedFace, bestFace); cmp < 0 || (cmp == 0 && lessActualFace(rotatedFace, bestFace)) {
		bestFace = rotatedFace
	}

	return bestFace
}

type edgebreakerFaceEdge struct {
	key     edgePair
	faceID  int
	edgePos int
	forward bool
}

type edgebreakerOrientationNeighbor struct {
	faceID      int
	sameForward bool
}

type edgePair struct {
	a uint32
	b uint32
}

func growEdgebreakerOrientationNeighbors(buf []edgebreakerOrientationNeighbor, size int) []edgebreakerOrientationNeighbor {
	buf = slices.Grow(buf[:0], size)
	buf = buf[:size]
	for i := range buf {
		buf[i].faceID = -1
		buf[i].sameForward = false
	}

	return buf
}

func buildEdgebreakerOrientationNeighborsDense(faces []Face, signatureRanks []int, neighbors []edgebreakerOrientationNeighbor, scratch *edgebreakerEncodeScratch) (bool, error) {
	numVertices := len(signatureRanks)
	maxIntValue := int(^uint(0) >> 1)
	if numVertices <= 0 || numVertices > 2048 || numVertices > maxIntValue/numVertices {
		return false, nil
	}

	keySpace := numVertices * numVertices
	limit := len(faces) * 96
	if limit < 4096 {
		limit = 4096
	}

	if keySpace > limit {
		return false, nil
	}

	var denseEdges []edgebreakerFaceEdge
	var denseCounts []uint8
	var touched []int
	if scratch != nil {
		denseEdges = slices.Grow(scratch.orientationDenseEdges[:0], keySpace)
		denseEdges = denseEdges[:keySpace]
		scratch.orientationDenseEdges = denseEdges
		denseCounts = slices.Grow(scratch.orientationDenseCounts[:0], keySpace)
		denseCounts = denseCounts[:keySpace]
		scratch.orientationDenseCounts = denseCounts
		touched = slices.Grow(scratch.orientationDenseTouched[:0], len(faces)*3)
		touched = touched[:0]
	} else {
		denseEdges = make([]edgebreakerFaceEdge, keySpace)
		denseCounts = make([]uint8, keySpace)
		touched = make([]int, 0, len(faces)*3)
	}

	for faceID, face := range faces {
		faceEdges := [3][2]uint32{
			{face[0], face[1]},
			{face[1], face[2]},
			{face[2], face[0]},
		}
		for edgePos, edge := range faceEdges {
			key := normalizeEdgePair(edgePair{a: edge[0], b: edge[1]})
			if key.a >= uint32(numVertices) || key.b >= uint32(numVertices) {
				clearEdgebreakerOrientationDenseCounts(denseCounts, touched, scratch)
				return false, nil
			}

			index := int(key.a)*numVertices + int(key.b)
			count := denseCounts[index]
			current := edgebreakerFaceEdge{
				key:     key,
				faceID:  faceID,
				edgePos: edgePos,
				forward: edge[0] == key.a && edge[1] == key.b,
			}
			switch count {
			case 0:
				denseEdges[index] = current
				denseCounts[index] = 1
				touched = append(touched, index)
			case 1:
				first := denseEdges[index]
				sameForward := first.forward == current.forward
				neighbors[first.faceID*3+first.edgePos] = edgebreakerOrientationNeighbor{faceID: current.faceID, sameForward: sameForward}
				neighbors[current.faceID*3+current.edgePos] = edgebreakerOrientationNeighbor{faceID: first.faceID, sameForward: sameForward}
				denseCounts[index] = 2
			default:
				clearEdgebreakerOrientationDenseCounts(denseCounts, touched, scratch)
				return true, fmt.Errorf("%w: non-manifold edge %d-%d", ErrInvalidGeometry, key.a, key.b)
			}
		}
	}

	clearEdgebreakerOrientationDenseCounts(denseCounts, touched, scratch)
	return true, nil
}

func clearEdgebreakerOrientationDenseCounts(counts []uint8, touched []int, scratch *edgebreakerEncodeScratch) {
	for _, index := range touched {
		counts[index] = 0
	}

	if scratch != nil {
		scratch.orientationDenseTouched = touched[:0]
	}
}

func canonicalizeEdgebreakerFaceOrientations(faces []Face, signatureRanks []int, scratch *edgebreakerEncodeScratch) error {
	if len(faces) == 0 {
		return nil
	}

	var neighbors []edgebreakerOrientationNeighbor
	if scratch != nil {
		neighbors = growEdgebreakerOrientationNeighbors(scratch.orientationNeighbors, len(faces)*3)
		scratch.orientationNeighbors = neighbors
	} else {
		neighbors = growEdgebreakerOrientationNeighbors(nil, len(faces)*3)
	}

	usedDense, err := buildEdgebreakerOrientationNeighborsDense(faces, signatureRanks, neighbors, scratch)
	if err != nil {
		return err
	}

	if !usedDense {
		var edges []edgebreakerFaceEdge
		if scratch != nil {
			edges = slices.Grow(scratch.orientationEdges[:0], len(faces)*3)
			scratch.orientationEdges = edges
		} else {
			edges = make([]edgebreakerFaceEdge, 0, len(faces)*3)
		}

		for faceID, face := range faces {
			faceEdges := [3][2]uint32{
				{face[0], face[1]},
				{face[1], face[2]},
				{face[2], face[0]},
			}
			for edgePos, edge := range faceEdges {
				key := normalizeEdgePair(edgePair{a: edge[0], b: edge[1]})
				edges = append(edges, edgebreakerFaceEdge{
					key:     key,
					faceID:  faceID,
					edgePos: edgePos,
					forward: edge[0] == key.a && edge[1] == key.b,
				})
			}
		}

		if scratch != nil {
			scratch.orientationEdges = edges
		}

		slices.SortFunc(edges, func(a, b edgebreakerFaceEdge) int {
			if a.key.a < b.key.a {
				return -1
			}

			if a.key.a > b.key.a {
				return 1
			}

			if a.key.b < b.key.b {
				return -1
			}

			if a.key.b > b.key.b {
				return 1
			}

			return 0
		})
		for i := 0; i < len(edges); {
			key := edges[i].key
			j := i + 1
			for j < len(edges) && edges[j].key == key {
				j++
			}

			count := j - i
			if count > 2 {
				return fmt.Errorf("%w: non-manifold edge %d-%d", ErrInvalidGeometry, key.a, key.b)
			}

			if count != 2 {
				i = j
				continue
			}

			first := edges[i]
			second := edges[i+1]
			sameForward := first.forward == second.forward
			neighbors[first.faceID*3+first.edgePos] = edgebreakerOrientationNeighbor{faceID: second.faceID, sameForward: sameForward}
			neighbors[second.faceID*3+second.edgePos] = edgebreakerOrientationNeighbor{faceID: first.faceID, sameForward: sameForward}
			i = j
		}
	}

	var visited []bool
	var flipped []bool
	var component []int
	var queue []int
	if scratch != nil {
		visited = growBoolSlice(scratch.orientationVisited, len(faces), false)
		scratch.orientationVisited = visited
		flipped = growBoolSlice(scratch.orientationFlipped, len(faces), false)
		scratch.orientationFlipped = flipped
		component = slices.Grow(scratch.orientationComponent[:0], len(faces))
		queue = slices.Grow(scratch.orientationQueue[:0], len(faces))
	} else {
		visited = make([]bool, len(faces))
		flipped = make([]bool, len(faces))
		component = make([]int, 0, len(faces))
		queue = make([]int, 0, len(faces))
	}

	if scratch != nil {
		defer func() {
			scratch.orientationComponent = component[:0]
			scratch.orientationQueue = queue[:0]
		}()
	}

	for seedFace := range faces {
		if visited[seedFace] {
			continue
		}

		component = append(component[:0], seedFace)
		queue = append(queue[:0], seedFace)
		visited[seedFace] = true
		for head := 0; head < len(queue); head++ {
			faceID := queue[head]
			for edgePos := 0; edgePos < 3; edgePos++ {
				neighbor := neighbors[faceID*3+edgePos]
				if neighbor.faceID < 0 {
					continue
				}

				requiredFlip := neighbor.sameForward != flipped[faceID]
				if !visited[neighbor.faceID] {
					visited[neighbor.faceID] = true
					flipped[neighbor.faceID] = requiredFlip
					component = append(component, neighbor.faceID)
					queue = append(queue, neighbor.faceID)
					continue
				}

				if flipped[neighbor.faceID] != requiredFlip {
					return fmt.Errorf("%w: inconsistent face orientation across face %d edge %d", ErrInvalidGeometry, faceID, edgePos)
				}
			}
		}

		if edgebreakerInvertedOrientationBetter(faces, signatureRanks, component, flipped, scratch) {
			for _, faceID := range component {
				flipped[faceID] = !flipped[faceID]
			}
		}
	}

	for faceID := range faces {
		if flipped[faceID] {
			faces[faceID] = flipFaceWinding(faces[faceID])
		}
	}

	return nil
}

func edgebreakerInvertedOrientationBetter(faces []Face, signatureRanks []int, component []int, flipped []bool, scratch *edgebreakerEncodeScratch) bool {
	currentMin, currentOK := edgebreakerMinComponentOrientationFace(faces, signatureRanks, component, flipped, false)
	invertedMin, invertedOK := edgebreakerMinComponentOrientationFace(faces, signatureRanks, component, flipped, true)
	if currentOK && invertedOK {
		if cmp := compareCanonicalFaceSequenceRanks(signatureRanks, invertedMin, currentMin); cmp < 0 {
			return true
		} else if cmp > 0 {
			return false
		}
	}

	var current []Face
	var inverted []Face
	if scratch != nil {
		current = slices.Grow(scratch.orientationCurrent[:0], len(component))
		current = current[:len(component)]
		inverted = slices.Grow(scratch.orientationInverted[:0], len(component))
		inverted = inverted[:len(component)]
		scratch.orientationCurrent = current
		scratch.orientationInverted = inverted
	} else {
		current = make([]Face, len(component))
		inverted = make([]Face, len(component))
	}

	current = edgebreakerComponentOrientationFaces(current, faces, signatureRanks, component, flipped, false)
	inverted = edgebreakerComponentOrientationFaces(inverted, faces, signatureRanks, component, flipped, true)
	for i := range current {
		cmp := compareCanonicalFaceSequenceRanks(signatureRanks, inverted[i], current[i])
		if cmp < 0 {
			return true
		}

		if cmp > 0 {
			return false
		}
	}

	return false
}

func edgebreakerMinComponentOrientationFace(faces []Face, signatureRanks []int, component []int, flipped []bool, invert bool) (Face, bool) {
	var best Face
	hasBest := false
	for _, faceID := range component {
		face := faces[faceID]
		if flipped[faceID] != invert {
			face = flipFaceWinding(face)
		}

		face = canonicalizeFaceBySignatureRanks(signatureRanks, face)
		if !hasBest {
			best = face
			hasBest = true
			continue
		}

		if cmp := compareCanonicalFaceSequenceRanks(signatureRanks, face, best); cmp < 0 || (cmp == 0 && lessActualFace(face, best)) {
			best = face
		}
	}

	return best, hasBest
}

func edgebreakerComponentOrientationFaces(keys []Face, faces []Face, signatureRanks []int, component []int, flipped []bool, invert bool) []Face {
	for i, faceID := range component {
		face := faces[faceID]
		if flipped[faceID] != invert {
			face = flipFaceWinding(face)
		}

		keys[i] = canonicalizeFaceBySignatureRanks(signatureRanks, face)
	}

	slices.SortFunc(keys, func(a, b Face) int {
		if cmp := compareCanonicalFaceSequenceRanks(signatureRanks, a, b); cmp != 0 {
			return cmp
		}

		return compareActualFace(a, b)
	})
	return keys
}

func flipFaceWinding(face Face) Face {
	return Face{face[0], face[2], face[1]}
}

func (e *edgebreakerMeshEncoder) buildTraversalSeedOrder() []int {
	faceCount := e.table.FaceCount()
	var order []int
	if e.scratch != nil {
		order = slices.Grow(e.scratch.traversalSeedOrder[:0], faceCount)
		order = order[:faceCount]
		e.scratch.traversalSeedOrder = order
	} else {
		order = make([]int, faceCount)
	}

	for faceID := range order {
		order[faceID] = faceID
	}

	slices.SortFunc(order, e.compareTraversalSeedFaces)
	return order
}

func (e *edgebreakerMeshEncoder) compareTraversalSeedFaces(a, b int) int {
	if e.facesCanonical {
		if cmp := compareCanonicalFaceSequenceRanks(e.inputPointSignatureRanks, e.mesh.faces[a], e.mesh.faces[b]); cmp != 0 {
			return cmp
		}
	} else if cmp := compareCanonicalFaceSequenceRanks(e.inputPointSignatureRanks, e.canonicalFaces[a], e.canonicalFaces[b]); cmp != 0 {
		return cmp
	}

	if cmp := compareActualFace(e.mesh.faces[a], e.mesh.faces[b]); cmp != 0 {
		return cmp
	}

	return cmp.Compare(a, b)
}

func (e *edgebreakerMeshEncoder) encode(w *core.Writer) error {
	header := bitstream.Header{
		VersionMajor:  bitstream.MeshVersionMajor,
		VersionMinor:  bitstream.MeshVersionMinor,
		EncoderType:   bitstream.GeometryTypeMesh,
		EncoderMethod: bitstream.MeshEdgebreakerEncoding,
	}
	if e.mesh.metadataRef() != nil {
		header.Flags |= bitstream.MetadataFlagMask
	}

	if err := bitstream.EncodeHeader(w, header); err != nil {
		return err
	}

	if e.mesh.metadataRef() != nil {
		if err := md.EncodeGeometryMetadata(w, e.mesh.metadataRef()); err != nil {
			return err
		}
	}

	traversalMethod := e.options.edgebreakerMethodOr(EdgebreakerMethodStandard)
	var traversal edgebreakerTraversalEncoder
	switch traversalMethod {
	case EdgebreakerMethodStandard:
		if e.scratch != nil {
			traversal = &e.scratch.standardTraversal
		} else {
			traversal = &edgebreakerStandardTraversalEncoder{}
		}
	case EdgebreakerMethodPredictive:
		if e.scratch != nil {
			e.scratch.predictiveTraversal.table = e.table
			traversal = &e.scratch.predictiveTraversal
		} else {
			traversal = newEdgebreakerPredictiveTraversalEncoder(e.table)
		}
	case EdgebreakerMethodValence:
		if e.scratch != nil {
			e.scratch.valenceTraversal.encoder = e
			traversal = &e.scratch.valenceTraversal
		} else {
			traversal = newEdgebreakerValenceTraversalEncoder(e)
		}
	default:
		return fmt.Errorf("%w: edgebreaker traversal type %d", ErrUnsupportedFeature, traversalMethod)
	}

	traversal.SetAttributeDataCount(len(e.attributeData))
	traversal.Start()

	initFaceCorners := e.initFaceCorners[:0]
	for {
		faceID, interior, startCorner, ok := e.nextTraversalSeed()
		if !ok {
			break
		}

		traversal.EncodeStartFaceConfiguration(interior)
		if interior {
			tip := e.table.Vertex(startCorner)
			next := e.table.Vertex(e.table.Next(startCorner))
			prev := e.table.Vertex(e.table.Previous(startCorner))
			e.visitedVertices[tip] = true
			e.visitedVertices[next] = true
			e.visitedVertices[prev] = true
			e.visitedFaces[faceID] = true
			initFaceCorners = append(initFaceCorners, e.table.Next(startCorner))
			opp := e.table.Opposite(e.table.Next(startCorner))
			if opp >= 0 && !e.visitedFaces[e.table.Face(opp)] {
				if err := e.encodeConnectivityFromCorner(opp, traversal); err != nil {
					return err
				}
			}

			continue
		}

		e.encodeHole(e.table.Next(startCorner), true)
		if err := e.encodeConnectivityFromCorner(startCorner, traversal); err != nil {
			return err
		}
	}

	slices.Reverse(e.processedCorners)
	e.connectivitySourceCorners = append(e.connectivitySourceCorners[:0], e.processedCorners...)
	e.initFaceCorners = initFaceCorners
	e.processedCorners = append(e.processedCorners, initFaceCorners...)

	if len(e.attributeData) > 0 {
		clear(e.visitedFaces)
		for _, corner := range e.processedCorners {
			e.encodeAttributeConnectivitiesOnFace(corner, traversal)
		}
	}

	e.nonIsolatedVertices = countNonIsolatedEdgebreakerVertices(e.table)
	if e.nativeTraversalOrder && traversalMethod == EdgebreakerMethodStandard {
		if err := e.computeNativeOrderDecodedPointMapping(); err != nil {
			return err
		}
	} else if !e.applyDecodedMappingCache(traversalMethod) {
		if err := e.computeDecodedPointMapping(traversal); err != nil {
			return err
		}

		e.storeDecodedMappingCache(traversalMethod)
	}

	if err := e.prepareAttributeGroups(); err != nil {
		return err
	}

	if err := guardEncodeUint32Value(e.nonIsolatedVertices, "edgebreaker vertex count"); err != nil {
		return err
	}

	if err := guardEncodeUint32Value(e.table.FaceCount(), "edgebreaker face count"); err != nil {
		return err
	}

	if err := guardEncodeUint8Value(len(e.attributeData), "edgebreaker attribute data count"); err != nil {
		return err
	}

	if err := guardEncodeUint32Value(traversal.EncodedSymbolCount(), "edgebreaker encoded symbol count"); err != nil {
		return err
	}

	if err := w.WriteUint8(traversal.TraversalType()); err != nil {
		return err
	}

	if err := core.EncodeVarUint32(w, uint32(e.nonIsolatedVertices)); err != nil {
		return err
	}

	if err := core.EncodeVarUint32(w, uint32(e.table.FaceCount())); err != nil {
		return err
	}

	if err := w.WriteUint8(uint8(len(e.attributeData))); err != nil {
		return err
	}

	if err := core.EncodeVarUint32(w, uint32(traversal.EncodedSymbolCount())); err != nil {
		return err
	}

	if err := core.EncodeVarUint32(w, e.numSplitSymbols); err != nil {
		return err
	}

	if err := encodeEdgebreakerSplitEvents(e.ctx, w, e.splitEvents); err != nil {
		return err
	}

	if err := traversal.Write(e.ctx, w); err != nil {
		return err
	}

	if err := e.encodeAttributeGroups(w); err != nil {
		return err
	}

	return e.encodeAttributeValues(w)
}

func (e *edgebreakerMeshEncoder) applyDecodedMappingCache(traversalMethod EdgebreakerMethod) bool {
	if e == nil || e.scratch == nil || e.nativeTraversalOrder || traversalMethod != EdgebreakerMethodStandard {
		return false
	}

	cache := &e.scratch.decodedMappingCache
	if !cache.match(e.sourceMesh, e.splitOnSeams, e.facesCanonical, traversalMethod) {
		return false
	}

	e.pointToCorner = cache.pointToCorner
	e.pointToSourcePoint = cache.pointToSourcePoint
	e.decodedFaces = cache.decodedFaces
	e.decodedTable = cache.decodedTable
	e.decodedConnectivity = cache.decodedConnectivity
	e.decodedCornerSource = cache.decodedCornerSource
	return true
}

func (e *edgebreakerMeshEncoder) storeDecodedMappingCache(traversalMethod EdgebreakerMethod) {
	if e == nil || e.scratch == nil || e.nativeTraversalOrder || traversalMethod != EdgebreakerMethodStandard {
		return
	}

	e.scratch.decodedMappingCache.store(e.sourceMesh, e.splitOnSeams, e.facesCanonical, traversalMethod, e)
}

func (e *edgebreakerMeshEncoder) nextTraversalSeed() (int, bool, int, bool) {
	if e.nativeTraversalOrder {
		for e.traversalSeedCursor < e.table.FaceCount() {
			faceID := e.traversalSeedCursor
			e.traversalSeedCursor++
			if e.visitedFaces[faceID] {
				continue
			}

			interior, startCorner := findEdgebreakerInitFaceConfiguration(e.table, faceID, e.vertexHoleID)
			return faceID, interior, startCorner, true
		}

		return 0, false, 0, false
	}

	for e.traversalSeedCursor < len(e.traversalSeedOrder) {
		faceID := e.traversalSeedOrder[e.traversalSeedCursor]
		e.traversalSeedCursor++
		if e.visitedFaces[faceID] {
			continue
		}

		interior, startCorner := findEdgebreakerInitFaceConfiguration(e.table, faceID, e.vertexHoleID)
		return faceID, interior, startCorner, true
	}

	return 0, false, 0, false
}

func (e *edgebreakerMeshEncoder) computeDecodedPointMapping(traversal edgebreakerTraversalEncoder) error {
	maxVertices := e.nonIsolatedVertices + int(e.numSplitSymbols)
	var decodedTable *edgebreakerMutableCornerTable
	if e.scratch != nil {
		decodedTable = resetEdgebreakerMutableCornerTable(e.scratch.decodedTable, e.table.FaceCount(), maxVertices)
		e.scratch.decodedTable = decodedTable
	} else {
		decodedTable = newEdgebreakerMutableCornerTable(e.table.FaceCount(), maxVertices)
	}

	decodedTable.enableVertexCornerTracking()

	var decodedConnectivity []*edgebreakerAttributeConnectivity
	if e.scratch != nil {
		e.scratch.decodedConnectivityPool = slices.Grow(e.scratch.decodedConnectivityPool[:0], len(e.attributeData))
		e.scratch.decodedConnectivityPool = e.scratch.decodedConnectivityPool[:len(e.attributeData)]
		decodedConnectivity = e.scratch.decodedConnectivityPool
	} else {
		decodedConnectivity = make([]*edgebreakerAttributeConnectivity, len(e.attributeData))
	}

	for i := range decodedConnectivity {
		decodedConnectivity[i] = resetEdgebreakerAttributeConnectivity(decodedConnectivity[i], decodedTable.CornerCount(), maxVertices)
	}

	var isVertHole []bool
	if e.scratch != nil {
		isVertHole = growBoolSlice(e.scratch.isVertHole, maxVertices, true)
		e.scratch.isVertHole = isVertHole
	} else {
		isVertHole = growBoolSlice(nil, maxVertices, true)
	}

	var decodedCornerSource []int
	if e.scratch != nil {
		decodedCornerSource = growIntSlice(e.scratch.decodedCornerSource, decodedTable.CornerCount(), -1)
		e.scratch.decodedCornerSource = decodedCornerSource
	} else {
		decodedCornerSource = growIntSlice(nil, decodedTable.CornerCount(), -1)
	}

	var seamPos []int
	if e.scratch != nil {
		seamPos = growIntSlice(e.scratch.recordedSeamPos, len(traversal.RecordedSeams()), 0)
		e.scratch.recordedSeamPos = seamPos
	}

	mock := &recordedEdgebreakerTraversal{
		symbols:                   traversal.RecordedSymbols(),
		startFaces:                traversal.RecordedStartFaces(),
		seams:                     traversal.RecordedSeams(),
		sourceConnectivityCorners: e.connectivitySourceCorners,
		sourceInitFaceCorners:     e.initFaceCorners,
		sourceTable:               e.table,
		decodedCornerSource:       decodedCornerSource,
		seamPos:                   seamPos,
	}
	var decodeScratch *edgebreakerDecodeScratch
	if e.scratch != nil {
		decodeScratch = &e.scratch.decodeScratch
	}

	if _, err := decodeEdgebreakerConnectivity(e.ctx, decodedTable, mock, e.table.FaceCount(), traversal.EncodedSymbolCount(), e.splitEvents, decodedConnectivity, isVertHole, decodeScratch); err != nil {
		return err
	}

	for i := range decodedConnectivity {
		if err := decodedConnectivity[i].recomputeVertices(decodedTable); err != nil {
			return err
		}
	}

	_, pointToCorner, decodedFaces, err := assignEdgebreakerPointsToCorners(e.ctx, decodedTable, decodedConnectivity, isVertHole, decodeScratch)
	if err != nil {
		return err
	}

	var pointToSourcePoint []int
	if e.scratch != nil {
		pointToSourcePoint = growIntSlice(e.scratch.pointToSourcePoint, len(pointToCorner), 0)
		e.scratch.pointToSourcePoint = pointToSourcePoint
	} else {
		pointToSourcePoint = growIntSlice(nil, len(pointToCorner), 0)
	}

	for pointID, corner := range pointToCorner {
		if corner < 0 || corner >= len(mock.decodedCornerSource) {
			return fmt.Errorf("%w: missing decoded corner for point %d", ErrInvalidGeometry, pointID)
		}

		sourceCorner := mock.decodedCornerSource[corner]
		if sourceCorner < 0 {
			return fmt.Errorf("%w: missing source corner for decoded point %d", ErrInvalidGeometry, pointID)
		}

		pointToSourcePoint[pointID] = pointAtMeshCorner(e.mesh, sourceCorner)
	}

	e.pointToCorner = pointToCorner
	e.pointToSourcePoint = pointToSourcePoint
	e.decodedFaces = decodedFaces
	e.decodedTable = decodedTable
	e.decodedConnectivity = decodedConnectivity
	e.decodedCornerSource = mock.decodedCornerSource
	for i := range e.decodedCornerSource {
		e.decodedCornerSource[i] = -1
	}

	for faceID, face := range decodedFaces {
		for local, pointID := range face {
			corner := faceID*3 + local
			e.decodedCornerSource[corner] = pointToSourcePoint[pointID]
		}
	}

	return nil
}

func (e *edgebreakerMeshEncoder) computeNativeOrderDecodedPointMapping() error {
	faceCount := e.table.FaceCount()
	if len(e.processedCorners) != faceCount {
		return fmt.Errorf("%w: native edgebreaker source corner count %d want %d", ErrInvalidGeometry, len(e.processedCorners), faceCount)
	}

	sourceCornerCount := e.table.CornerCount()
	sourceCornerVertex := e.table.cornerVertex
	sourceOpposite := e.table.opposite
	decodedTable := e.resetNativeDecodedTable(faceCount)
	sourceToDecoded := e.nativeSourceToDecodedCorner()
	sourceVertexToDecoded := e.nativeSourceVertexToDecoded()
	decodedSourcePoints := e.nativeDecodedSourcePoints(faceCount)

	for faceID, sourceCorner := range e.processedCorners {
		if sourceCorner < 0 || sourceCorner >= sourceCornerCount {
			return fmt.Errorf("%w: native edgebreaker source corner %d out of range", ErrInvalidGeometry, sourceCorner)
		}

		decodedCorner := faceID * 3
		sourceCorners := [3]int{sourceCorner, nextCorner(sourceCorner), previousCorner(sourceCorner)}
		for local, mappedSourceCorner := range sourceCorners {
			if mappedSourceCorner < 0 || mappedSourceCorner >= len(sourceToDecoded) {
				return fmt.Errorf("%w: native edgebreaker mapped source corner %d out of range", ErrInvalidGeometry, mappedSourceCorner)
			}

			sourceToDecoded[mappedSourceCorner] = decodedCorner + local
			decodedSourcePoints[decodedCorner+local] = pointAtMeshCorner(e.mesh, mappedSourceCorner)

			sourceVertex := sourceCornerVertex[mappedSourceCorner]
			if sourceVertex < 0 || sourceVertex >= len(sourceVertexToDecoded) {
				return fmt.Errorf("%w: native edgebreaker source vertex %d out of range", ErrInvalidGeometry, sourceVertex)
			}

			decodedVertex := sourceVertexToDecoded[sourceVertex]
			if decodedVertex < 0 {
				if decodedTable.numVertices >= decodedTable.maxVertices {
					return fmt.Errorf("%w: too many native edgebreaker decoded vertices", ErrInvalidGeometry)
				}

				decodedVertex = decodedTable.numVertices
				decodedTable.numVertices++
				sourceVertexToDecoded[sourceVertex] = decodedVertex
			}

			decodedTable.cornerVertex[decodedCorner+local] = decodedVertex
		}
	}

	for sourceCorner, decodedCorner := range sourceToDecoded {
		if decodedCorner < 0 {
			continue
		}

		oppositeSourceCorner := sourceOpposite[sourceCorner]
		if oppositeSourceCorner < 0 {
			continue
		}

		if oppositeSourceCorner >= len(sourceToDecoded) || sourceToDecoded[oppositeSourceCorner] < 0 {
			return fmt.Errorf("%w: missing native edgebreaker decoded opposite for source corner %d", ErrInvalidGeometry, sourceCorner)
		}

		oppositeDecodedCorner := sourceToDecoded[oppositeSourceCorner]
		if decodedCorner < oppositeDecodedCorner {
			decodedTable.opposite[decodedCorner] = oppositeDecodedCorner
			decodedTable.opposite[oppositeDecodedCorner] = decodedCorner
		}
	}

	if err := computeEdgebreakerBaseVertexCorners(decodedTable, decodedTable.VertexCount(), e.scratch); err != nil {
		return err
	}

	decodedConnectivity, err := e.nativeDecodedConnectivity(decodedTable, sourceToDecoded)
	if err != nil {
		return err
	}

	isVertHole := e.nativeDecodedHoleFlags(decodedTable.VertexCount(), sourceVertexToDecoded)
	_, pointToCorner, decodedFaces, err := assignEdgebreakerPointsToCorners(e.ctx, decodedTable, decodedConnectivity, isVertHole, e.nativeDecodeScratch())
	if err != nil {
		return err
	}

	pointToSourcePoint := e.nativePointToSourcePoint(pointToCorner, decodedSourcePoints)
	e.pointToCorner = pointToCorner
	e.pointToSourcePoint = pointToSourcePoint
	e.decodedFaces = decodedFaces
	e.decodedTable = decodedTable
	e.decodedConnectivity = decodedConnectivity
	e.decodedCornerSource = decodedSourcePoints
	return nil
}

func (e *edgebreakerMeshEncoder) resetNativeDecodedTable(faceCount int) *edgebreakerMutableCornerTable {
	maxVertices := maxInt(e.table.VertexCount(), e.nonIsolatedVertices)
	if e.scratch != nil {
		e.scratch.decodedTable = resetEdgebreakerMutableCornerTable(e.scratch.decodedTable, faceCount, maxVertices)
		return e.scratch.decodedTable
	}

	return newEdgebreakerMutableCornerTable(faceCount, maxVertices)
}

func (e *edgebreakerMeshEncoder) nativeSourceToDecodedCorner() []int {
	if e.scratch != nil {
		e.scratch.sourceToDecodedCorner = growIntSlice(e.scratch.sourceToDecodedCorner, e.table.CornerCount(), -1)
		return e.scratch.sourceToDecodedCorner
	}

	return growIntSlice(nil, e.table.CornerCount(), -1)
}

func (e *edgebreakerMeshEncoder) nativeSourceVertexToDecoded() []int {
	if e.scratch != nil {
		e.scratch.sourceVertexToDecoded = growIntSlice(e.scratch.sourceVertexToDecoded, e.table.VertexCount(), -1)
		return e.scratch.sourceVertexToDecoded
	}

	return growIntSlice(nil, e.table.VertexCount(), -1)
}

func (e *edgebreakerMeshEncoder) nativeDecodedSourcePoints(faceCount int) []int {
	cornerCount := faceCount * 3
	if e.scratch != nil {
		e.scratch.decodedSourcePoints = growIntSlice(e.scratch.decodedSourcePoints, cornerCount, -1)
		return e.scratch.decodedSourcePoints
	}

	return growIntSlice(nil, cornerCount, -1)
}

func (e *edgebreakerMeshEncoder) nativeDecodedConnectivity(decodedTable *edgebreakerMutableCornerTable, sourceToDecoded []int) ([]*edgebreakerAttributeConnectivity, error) {
	var decodedConnectivity []*edgebreakerAttributeConnectivity
	if e.scratch != nil {
		e.scratch.decodedConnectivityPool = slices.Grow(e.scratch.decodedConnectivityPool[:0], len(e.attributeData))
		e.scratch.decodedConnectivityPool = e.scratch.decodedConnectivityPool[:len(e.attributeData)]
		decodedConnectivity = e.scratch.decodedConnectivityPool
	} else {
		decodedConnectivity = make([]*edgebreakerAttributeConnectivity, len(e.attributeData))
	}

	for dataID := range decodedConnectivity {
		decoded := resetEdgebreakerAttributeConnectivity(decodedConnectivity[dataID], decodedTable.CornerCount(), decodedTable.VertexCount())
		source := e.attributeData[dataID].connectivity
		if source != nil {
			for sourceCorner, seam := range source.seamEdges {
				if !seam {
					continue
				}

				if sourceCorner >= len(sourceToDecoded) || sourceToDecoded[sourceCorner] < 0 {
					return nil, fmt.Errorf("%w: missing decoded corner for native seam source corner %d", ErrInvalidGeometry, sourceCorner)
				}

				decoded.markSeam(decodedTable, sourceToDecoded[sourceCorner])
			}
		}

		if err := decoded.recomputeVertices(decodedTable); err != nil {
			return nil, err
		}

		decodedConnectivity[dataID] = decoded
	}

	return decodedConnectivity, nil
}

func (e *edgebreakerMeshEncoder) nativeDecodedHoleFlags(numVertices int, sourceVertexToDecoded []int) []bool {
	var isVertHole []bool
	if e.scratch != nil {
		isVertHole = growBoolSlice(e.scratch.isVertHole, numVertices, false)
		e.scratch.isVertHole = isVertHole
	} else {
		isVertHole = growBoolSlice(nil, numVertices, false)
	}

	for sourceVertex, decodedVertex := range sourceVertexToDecoded {
		if decodedVertex < 0 || decodedVertex >= len(isVertHole) {
			continue
		}

		isVertHole[decodedVertex] = sourceVertex < len(e.vertexHoleID) && e.vertexHoleID[sourceVertex] != -1
	}

	return isVertHole
}

func (e *edgebreakerMeshEncoder) nativePointToSourcePoint(pointToCorner, decodedSourcePoints []int) []int {
	var pointToSourcePoint []int
	if e.scratch != nil {
		pointToSourcePoint = growIntSlice(e.scratch.pointToSourcePoint, len(pointToCorner), 0)
		e.scratch.pointToSourcePoint = pointToSourcePoint
	} else {
		pointToSourcePoint = growIntSlice(nil, len(pointToCorner), 0)
	}

	for pointID, corner := range pointToCorner {
		if corner >= 0 && corner < len(decodedSourcePoints) {
			pointToSourcePoint[pointID] = decodedSourcePoints[corner]
		}
	}

	return pointToSourcePoint
}

func (e *edgebreakerMeshEncoder) nativeDecodeScratch() *edgebreakerDecodeScratch {
	if e.scratch == nil {
		return nil
	}

	return &e.scratch.decodeScratch
}

func (e *edgebreakerMeshEncoder) initAttributeData() error {
	nonPositionCount := e.mesh.AttributeCount() - 1
	if nonPositionCount < 0 {
		nonPositionCount = 0
	}

	if e.scratch != nil {
		clear(e.scratch.attributeData)
		e.scratch.attributeData = slices.Grow(e.scratch.attributeData[:0], nonPositionCount)
		e.attributeData = e.scratch.attributeData[:0]
		e.scratch.connectivityPool = slices.Grow(e.scratch.connectivityPool[:0], nonPositionCount)
		e.scratch.connectivityPool = e.scratch.connectivityPool[:nonPositionCount]
	} else {
		e.attributeData = make([]edgebreakerAttributeData, 0, nonPositionCount)
	}

	connectivityIndex := 0
	for _, attr := range e.mesh.attributes {
		if attr.Type == AttributePosition {
			continue
		}

		var reusable *edgebreakerAttributeConnectivity
		if e.scratch != nil {
			reusable = e.scratch.connectivityPool[connectivityIndex]
		}

		connectivity, err := buildEdgebreakerAttributeConnectivity(e.table, e.mesh, attr, reusable)
		if err != nil {
			return err
		}

		if e.scratch != nil {
			e.scratch.connectivityPool[connectivityIndex] = connectivity
		}

		connectivityIndex++
		group := edgebreakerAttributeData{
			attr:         attr,
			dataID:       len(e.attributeData),
			decoderType:  meshVertexAttribute,
			traversal:    meshTraversalDepthFirst,
			connectivity: connectivity,
		}
		if !connectivity.noInteriorSeams {
			group.decoderType = meshCornerAttribute
		}

		e.attributeData = append(e.attributeData, group)
	}

	return nil
}

func (e *edgebreakerMeshEncoder) prepareAttributeGroups() error {
	position := e.mesh.namedAttribute(AttributePosition)
	posGroup, err := e.prepareEdgebreakerAttributeGroup(position, -1, meshVertexAttribute, meshTraversalDepthFirst, nil)
	if err != nil {
		return err
	}

	e.posGroup = posGroup
	for i := range e.attributeData {
		group, err := e.prepareEdgebreakerAttributeGroup(
			e.attributeData[i].attr,
			e.attributeData[i].dataID,
			e.attributeData[i].decoderType,
			e.attributeData[i].traversal,
			e.attributeData[i].connectivity,
		)
		if err != nil {
			return err
		}

		e.attributeData[i] = *group
	}

	return nil
}

func (e *edgebreakerMeshEncoder) prepareEdgebreakerAttributeGroup(attr *Attribute, dataID int, decoderType, traversal uint8, connectivity *edgebreakerAttributeConnectivity) (*edgebreakerAttributeData, error) {
	group := &edgebreakerAttributeData{
		attr:         attr,
		dataID:       dataID,
		decoderType:  decoderType,
		traversal:    traversal,
		connectivity: connectivity,
	}
	decodeGroup := edgebreakerAttributeDecoder{
		dataID:          dataID,
		decoderType:     decoderType,
		traversalMethod: traversal,
	}
	attrConnectivity := e.decodedConnectivity
	if dataID >= 0 && dataID < len(attrConnectivity) {
		connectivity = attrConnectivity[dataID]
		group.connectivity = connectivity
	}

	var decodeScratch *edgebreakerDecodeScratch
	if e.scratch != nil {
		decodeScratch = &e.scratch.decodeScratch
	}

	if err := generateEdgebreakerSequence(e.ctx, e.decodedTable, attrConnectivity, &decodeGroup, decodeScratch); err != nil {
		return nil, err
	}

	group.sequence = decodeGroup.sequenceCorners
	group.vertexToCode = decodeGroup.vertexToEncoded
	encoded, err := e.buildEncodedEdgebreakerAttribute(group)
	if err != nil {
		return nil, err
	}

	group.encodedAttr = encoded
	quantizationBits := e.options.quantizationBits(attr.Type)
	state, err := buildSequentialEncodedAttributeState(e.ctx, attr, encoded, quantizationBits, defaultSequentialFloatQuantizer)
	if err != nil {
		return nil, err
	}

	group.state = state
	group.sequence = nil
	group.vertexToCode = nil
	return group, nil
}

func (e *edgebreakerMeshEncoder) buildEncodedEdgebreakerAttribute(group *edgebreakerAttributeData) (*Attribute, error) {
	numValues := len(group.sequence)
	out, err := NewAttribute(group.attr.Type, group.attr.DataType, group.attr.NumComponents, numValues)
	if err != nil {
		return nil, err
	}

	out.UniqueID = group.attr.UniqueID
	out.Normalized = group.attr.Normalized
	if err := out.SetExplicitMapping(len(e.pointToSourcePoint)); err != nil {
		return nil, err
	}

	for pointID := 0; pointID < len(e.pointToSourcePoint); pointID++ {
		if err := checkContextEvery(e.ctx, pointID); err != nil {
			return nil, err
		}

		corner := e.pointToCorner[pointID]
		if corner < 0 {
			return nil, fmt.Errorf("%w: missing decoded corner for point %d", ErrInvalidGeometry, pointID)
		}

		vertex := e.groupVertexForCorner(group, corner)
		if vertex < 0 || vertex >= len(group.vertexToCode) || group.vertexToCode[vertex] < 0 {
			return nil, fmt.Errorf("%w: missing edgebreaker encoded vertex for point %d", ErrInvalidGeometry, pointID)
		}

		out.mapping[pointID] = uint32(group.vertexToCode[vertex])
	}

	stride := group.attr.ByteStride()
	sourceEntries := group.attr.EntryCount()
	sourceData := group.attr.data
	for encodedEntry, corner := range group.sequence {
		if err := checkContextEvery(e.ctx, encodedEntry); err != nil {
			return nil, err
		}

		sourcePoint := e.decodedCornerSource[corner]
		if sourcePoint < 0 || sourcePoint >= e.mesh.PointCount() {
			return nil, fmt.Errorf("%w: missing source point for decoded corner %d", ErrInvalidGeometry, corner)
		}

		sourceEntry := int(group.attr.mappedIndex(sourcePoint))
		if sourceEntry < 0 || sourceEntry >= sourceEntries {
			return nil, fmt.Errorf("%w: attribute entry %d out of range", ErrInvalidGeometry, sourceEntry)
		}

		offset := encodedEntry * stride
		sourceOffset := sourceEntry * stride
		copy(out.data[offset:offset+stride], sourceData[sourceOffset:sourceOffset+stride])
	}

	return out, nil
}

func (e *edgebreakerMeshEncoder) groupVertexForCorner(group *edgebreakerAttributeData, corner int) int {
	if group.decoderType == meshCornerAttribute && group.connectivity != nil {
		return group.connectivity.cornerToVertex[corner]
	}

	return e.decodedTable.Vertex(corner)
}

func (e *edgebreakerMeshEncoder) encodeConnectivityFromCorner(corner int, traversal edgebreakerTraversalEncoder) error {
	e.cornerTraversalStack = append(e.cornerTraversalStack[:0], corner)
	maxSteps := e.table.FaceCount()*4 + 1
	steps := 0
	for len(e.cornerTraversalStack) > 0 {
		corner = e.cornerTraversalStack[len(e.cornerTraversalStack)-1]
		if corner < 0 || e.visitedFaces[e.table.Face(corner)] {
			e.cornerTraversalStack = e.cornerTraversalStack[:len(e.cornerTraversalStack)-1]
			continue
		}

		for {
			steps++
			if steps > maxSteps*maxInt(1, e.table.FaceCount()) {
				return fmt.Errorf("%w: edgebreaker traversal exceeded safety limit", ErrInvalidGeometry)
			}

			e.lastEncodedSymbolID++
			faceID := e.table.Face(corner)
			e.visitedFaces[faceID] = true
			e.processedCorners = append(e.processedCorners, corner)
			traversal.NewCornerReached(corner)

			vertex := e.table.Vertex(corner)
			onBoundary := e.vertexHoleID[vertex] != -1
			if !e.visitedVertices[vertex] {
				e.visitedVertices[vertex] = true
				if !onBoundary {
					traversal.EncodeSymbol(topologyC)
					corner = e.table.RightCorner(corner)
					continue
				}
			}

			rightCorner := e.table.RightCorner(corner)
			leftCorner := e.table.LeftCorner(corner)
			rightVisited := rightCorner < 0 || e.visitedFaces[e.table.Face(rightCorner)]
			leftVisited := leftCorner < 0 || e.visitedFaces[e.table.Face(leftCorner)]
			if rightVisited {
				if rightCorner >= 0 {
					e.checkAndStoreTopologySplitEvent(e.lastEncodedSymbolID, rightFaceEdge, e.table.Face(rightCorner))
				}

				if leftVisited {
					if leftCorner >= 0 {
						e.checkAndStoreTopologySplitEvent(e.lastEncodedSymbolID, leftFaceEdge, e.table.Face(leftCorner))
					}

					traversal.EncodeSymbol(topologyE)
					e.cornerTraversalStack = e.cornerTraversalStack[:len(e.cornerTraversalStack)-1]
					break
				}

				traversal.EncodeSymbol(topologyR)
				corner = leftCorner
				continue
			}

			if leftVisited {
				if leftCorner >= 0 {
					e.checkAndStoreTopologySplitEvent(e.lastEncodedSymbolID, leftFaceEdge, e.table.Face(leftCorner))
				}

				traversal.EncodeSymbol(topologyL)
				corner = rightCorner
				continue
			}

			traversal.EncodeSymbol(topologyS)
			e.numSplitSymbols++
			if onBoundary {
				holeID := e.vertexHoleID[vertex]
				if holeID >= 0 && !e.visitedHoles[holeID] {
					e.encodeHole(corner, false)
				}
			}

			if faceID >= 0 && faceID < len(e.faceToSplitSymbol) {
				e.faceToSplitSymbol[faceID] = e.lastEncodedSymbolID
			}

			e.cornerTraversalStack[len(e.cornerTraversalStack)-1] = leftCorner
			e.cornerTraversalStack = append(e.cornerTraversalStack, rightCorner)
			break
		}
	}

	return nil
}

func (e *edgebreakerMeshEncoder) encodeHole(startCorner int, encodeFirstVertex bool) {
	corner := e.table.Previous(startCorner)
	for e.table.Opposite(corner) >= 0 {
		corner = e.table.Opposite(corner)
		corner = e.table.Next(corner)
	}

	startVertex := e.table.Vertex(startCorner)
	if encodeFirstVertex {
		e.visitedVertices[startVertex] = true
	}

	holeID := e.vertexHoleID[startVertex]
	if holeID >= 0 {
		e.visitedHoles[holeID] = true
	}

	currentVertex := e.table.Vertex(e.table.Previous(corner))
	for currentVertex != startVertex {
		e.visitedVertices[currentVertex] = true
		corner = e.table.Next(corner)
		for e.table.Opposite(corner) >= 0 {
			corner = e.table.Opposite(corner)
			corner = e.table.Next(corner)
		}

		currentVertex = e.table.Vertex(e.table.Previous(corner))
	}
}

func (e *edgebreakerMeshEncoder) checkAndStoreTopologySplitEvent(sourceSymbolID int, sourceEdge uint8, neighborFaceID int) {
	if neighborFaceID < 0 || neighborFaceID >= len(e.faceToSplitSymbol) {
		return
	}

	splitSymbolID := e.faceToSplitSymbol[neighborFaceID]
	if splitSymbolID < 0 {
		return
	}

	e.splitEvents = append(e.splitEvents, edgebreakerTopologySplit{
		sourceSymbolID: uint32(sourceSymbolID),
		splitSymbolID:  uint32(splitSymbolID),
		sourceEdge:     sourceEdge,
	})
}

func (e *edgebreakerMeshEncoder) encodeAttributeConnectivitiesOnFace(corner int, traversal edgebreakerTraversalEncoder) {
	corners := [3]int{corner, e.table.Next(corner), e.table.Previous(corner)}
	faceID := e.table.Face(corner)
	e.visitedFaces[faceID] = true
	for i := 0; i < 3; i++ {
		opp := e.table.Opposite(corners[i])
		if opp < 0 {
			continue
		}

		oppFace := e.table.Face(opp)
		if e.visitedFaces[oppFace] {
			continue
		}

		for attrID := range e.attributeData {
			traversal.EncodeAttributeSeam(attrID, e.attributeData[attrID].connectivity.seamEdges[corners[i]])
		}
	}
}

func (e *edgebreakerMeshEncoder) encodeAttributeGroups(w *core.Writer) error {
	numGroups := 1 + len(e.attributeData)
	if err := guardEncodeUint8Value(numGroups, "edgebreaker attribute group count"); err != nil {
		return err
	}

	if err := w.WriteUint8(uint8(numGroups)); err != nil {
		return err
	}

	if err := encodeEdgebreakerAttributeGroupHeader(w, e.posGroup); err != nil {
		return err
	}

	for i := range e.attributeData {
		if err := checkContextEvery(e.ctx, i); err != nil {
			return err
		}

		if err := encodeEdgebreakerAttributeGroupHeader(w, &e.attributeData[i]); err != nil {
			return err
		}
	}

	if err := encodeEdgebreakerAttributeGroupDescriptor(w, e.posGroup); err != nil {
		return err
	}

	for i := range e.attributeData {
		if err := checkContextEvery(e.ctx, i); err != nil {
			return err
		}

		if err := encodeEdgebreakerAttributeGroupDescriptor(w, &e.attributeData[i]); err != nil {
			return err
		}
	}

	return nil
}

func encodeEdgebreakerAttributeGroupHeader(w *core.Writer, group *edgebreakerAttributeData) error {
	if err := guardEncodeInt8Value(group.dataID, "edgebreaker attribute group data id"); err != nil {
		return err
	}

	if err := w.WriteInt8(int8(group.dataID)); err != nil {
		return err
	}

	if err := w.WriteUint8(group.decoderType); err != nil {
		return err
	}

	return w.WriteUint8(group.traversal)
}

func encodeEdgebreakerAttributeGroupDescriptor(w *core.Writer, group *edgebreakerAttributeData) error {
	if err := validateAttributeWireDescriptor(group.attr, "edgebreaker attribute descriptor"); err != nil {
		return err
	}

	if err := core.EncodeVarUint32(w, 1); err != nil {
		return err
	}

	if err := w.WriteUint8(uint8(group.attr.Type)); err != nil {
		return err
	}

	if err := w.WriteUint8(uint8(group.attr.DataType)); err != nil {
		return err
	}

	if err := w.WriteUint8(uint8(group.attr.NumComponents)); err != nil {
		return err
	}

	normalized := uint8(0)
	if group.attr.Normalized {
		normalized = 1
	}

	if err := w.WriteUint8(normalized); err != nil {
		return err
	}

	if err := core.EncodeVarUint32(w, group.attr.UniqueID); err != nil {
		return err
	}

	return w.WriteUint8(group.state.encoderType)
}

func (e *edgebreakerMeshEncoder) encodeAttributeValues(w *core.Writer) error {
	positionPortable := e.posGroup.state.portable
	sequenceScratch := e.sequenceScratch()
	if err := encodeSingleEdgebreakerAttributeValue(e.ctx, w, e.posGroup, e.options, positionPortable, sequenceScratch); err != nil {
		return err
	}

	for i := range e.attributeData {
		options := e.options
		if isMeshPredictionMethod(options.predictionMethod(e.attributeData[i].attr.Type)) {
			options.SetAttributePrediction(e.attributeData[i].attr.Type, PredictionMethodDifference)
		}

		if err := encodeSingleEdgebreakerAttributeValue(e.ctx, w, &e.attributeData[i], options, positionPortable, sequenceScratch); err != nil {
			return err
		}
	}

	return nil
}

func (e *edgebreakerMeshEncoder) sequenceScratch() *sequentialEncodeScratch {
	if e == nil || e.scratch == nil {
		return nil
	}

	return &e.scratch.sequenceScratch
}

func encodeSingleEdgebreakerAttributeValue(ctx context.Context, w *core.Writer, group *edgebreakerAttributeData, options encodeConfig, positionPortable *Attribute, scratch *sequentialEncodeScratch) error {
	state := group.state
	numValues := group.encodedAttr.EntryCount()
	switch state.encoderType {
	case bitstream.SequentialAttributeEncoderGeneric:
		for entry := 0; entry < numValues; entry++ {
			if err := checkContextEvery(ctx, entry); err != nil {
				return err
			}

			raw, err := state.portable.rawEntry(entry)
			if err != nil {
				return err
			}

			if err := w.WriteBytes(raw); err != nil {
				return err
			}
		}
	case bitstream.SequentialAttributeEncoderInteger, bitstream.SequentialAttributeEncoderQuantization:
		if err := encodeSequentialIntegerAttribute(ctx, w, group.dataID, state.attr, state.portable, numValues, nil, positionPortable, options, options.useBuiltInAttributeCompression(), symbolEncodingOptions(options), scratch); err != nil {
			return err
		}
	case bitstream.SequentialAttributeEncoderNormals:
		if err := encodeSequentialNormalAttribute(ctx, w, group.dataID, state.attr, state.portable, state.octahedron, numValues, nil, positionPortable, options, options.useBuiltInAttributeCompression(), symbolEncodingOptions(options), scratch); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: sequential encoder type %d", ErrUnsupportedFeature, state.encoderType)
	}

	switch state.encoderType {
	case bitstream.SequentialAttributeEncoderQuantization:
		return state.quantization.encode(w)
	case bitstream.SequentialAttributeEncoderNormals:
		return state.octahedron.Encode(w)
	default:
		return nil
	}
}

func encodeEdgebreakerSplitEvents(ctx context.Context, w *core.Writer, events []edgebreakerTopologySplit) error {
	if err := guardEncodeUint32Value(len(events), "edgebreaker split event count"); err != nil {
		return err
	}

	if err := core.EncodeVarUint32(w, uint32(len(events))); err != nil {
		return err
	}

	if len(events) == 0 {
		return nil
	}

	lastSource := uint32(0)
	for i, event := range events {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		if event.sourceSymbolID < lastSource {
			return fmt.Errorf("%w: edgebreaker split source order", ErrInvalidGeometry)
		}

		if event.splitSymbolID > event.sourceSymbolID {
			return fmt.Errorf("%w: edgebreaker split source/symbol mismatch", ErrInvalidGeometry)
		}

		if err := core.EncodeVarUint32(w, event.sourceSymbolID-lastSource); err != nil {
			return err
		}

		if err := core.EncodeVarUint32(w, event.sourceSymbolID-event.splitSymbolID); err != nil {
			return err
		}

		lastSource = event.sourceSymbolID
	}

	bits := core.NewBitWriter(len(events))
	for i, event := range events {
		if err := checkContextEvery(ctx, i); err != nil {
			return err
		}

		if !bits.WriteBitsLSB(uint32(event.sourceEdge&1), 1) {
			return errors.New("draco: failed to bit-pack edgebreaker split edge")
		}
	}

	return w.WriteBytes(bits.BytesView())
}

func validateAttributeWireDescriptor(attr *Attribute, what string) error {
	if attr == nil {
		return fmt.Errorf("%w: %s is nil", ErrInvalidGeometry, what)
	}

	if attr.NumComponents < 0 {
		return fmt.Errorf("%w: %s component count %d is negative", ErrInvalidGeometry, what, attr.NumComponents)
	}

	if attr.NumComponents > 255 {
		return fmt.Errorf("%w: %s component count %d exceeds uint8 range", ErrInvalidGeometry, what, attr.NumComponents)
	}

	return nil
}

func buildEdgebreakerBaseTable(mesh *Mesh, splitOnSeams bool, scratch *edgebreakerEncodeScratch) (*edgebreakerMutableCornerTable, error) {
	position := mesh.namedAttribute(AttributePosition)
	if position == nil {
		return nil, fmt.Errorf("%w: mesh position attribute missing", ErrInvalidGeometry)
	}

	var faces []topology.Face
	if scratch != nil {
		faces = slices.Grow(scratch.baseFaces[:0], mesh.FaceCount())
		faces = faces[:mesh.FaceCount()]
		scratch.baseFaces = faces
	} else {
		faces = make([]topology.Face, mesh.FaceCount())
	}

	for faceID, face := range mesh.faces {
		var mapped topology.Face
		for local, pointID := range face {
			vertex := pointID
			if !splitOnSeams {
				vertex = position.mappedIndex(int(pointID))
			}

			mapped[local] = vertex
		}

		faces[faceID] = mapped
	}

	if !splitOnSeams {
		return buildNonManifoldEdgebreakerBaseTable(position.EntryCount(), faces, scratch)
	}

	return buildNonManifoldEdgebreakerBaseTable(mesh.PointCount(), faces, scratch)
}

type edgebreakerHalfEdge struct {
	sinkVertex int
	edgeCorner int
}

func buildNonManifoldEdgebreakerBaseTable(numVertices int, faces []topology.Face, scratch *edgebreakerEncodeScratch) (*edgebreakerMutableCornerTable, error) {
	if numVertices < 0 {
		return nil, fmt.Errorf("%w: invalid position vertex count %d", ErrInvalidGeometry, numVertices)
	}

	maxVertices := maxInt(numVertices, len(faces)*3)
	var table *edgebreakerMutableCornerTable
	if scratch != nil {
		table = resetEdgebreakerMutableCornerTable(scratch.baseTable, len(faces), maxVertices)
		scratch.baseTable = table
	} else {
		table = newEdgebreakerMutableCornerTable(len(faces), maxVertices)
	}

	table.numVertices = numVertices
	for faceID, face := range faces {
		if face[0] == face[1] || face[1] == face[2] || face[0] == face[2] {
			return nil, fmt.Errorf("%w: degenerate face %d", ErrInvalidGeometry, faceID)
		}

		for local, vertex := range face {
			if int(vertex) < 0 || int(vertex) >= numVertices {
				return nil, fmt.Errorf("%w: face %d corner %d index %d out of range for %d vertices", ErrInvalidGeometry, faceID, local, vertex, numVertices)
			}

			corner := faceID*3 + local
			table.cornerVertex[corner] = int(vertex)
			table.opposite[corner] = -1
		}
	}

	if err := computeEdgebreakerBaseOpposites(table, scratch); err != nil {
		return nil, err
	}

	if err := computeEdgebreakerBaseVertexCorners(table, numVertices, scratch); err != nil {
		return nil, err
	}

	return table, nil
}

func computeEdgebreakerBaseOpposites(table *edgebreakerMutableCornerTable, scratch *edgebreakerEncodeScratch) error {
	cornerCount := table.CornerCount()
	cornerVertex := table.cornerVertex
	var numCornersOnVertices []int
	var vertexEdges []edgebreakerHalfEdge
	var vertexOffsets []int
	if scratch != nil {
		numCornersOnVertices = growIntSlice(scratch.baseCornerCounts, table.numVertices, 0)
		scratch.baseCornerCounts = numCornersOnVertices
		vertexEdges = growEdgebreakerHalfEdges(scratch.baseVertexEdges, cornerCount)
		scratch.baseVertexEdges = vertexEdges
		vertexOffsets = growIntSlice(scratch.baseVertexOffsets, len(numCornersOnVertices), 0)
		scratch.baseVertexOffsets = vertexOffsets
	} else {
		numCornersOnVertices = make([]int, table.numVertices)
		vertexEdges = growEdgebreakerHalfEdges(nil, cornerCount)
		vertexOffsets = make([]int, len(numCornersOnVertices))
	}

	for corner := 0; corner < cornerCount; corner++ {
		vertex := cornerVertex[corner]
		if vertex < 0 || vertex >= len(numCornersOnVertices) {
			return fmt.Errorf("%w: invalid edgebreaker vertex %d", ErrInvalidGeometry, vertex)
		}

		numCornersOnVertices[vertex]++
	}

	offset := 0
	for i, count := range numCornersOnVertices {
		vertexOffsets[i] = offset
		offset += count
	}

	for corner := 0; corner < cornerCount; corner++ {
		tipVertex := cornerVertex[corner]
		sourceVertex := cornerVertex[nextCorner(corner)]
		sinkVertex := cornerVertex[previousCorner(corner)]
		oppositeCorner := -1
		numSinkCorners := numCornersOnVertices[sinkVertex]
		offset = vertexOffsets[sinkVertex]
		for i := 0; i < numSinkCorners; i, offset = i+1, offset+1 {
			otherVertex := vertexEdges[offset].sinkVertex
			if otherVertex == -1 {
				break
			}

			if otherVertex != sourceVertex {
				continue
			}

			if tipVertex == cornerVertex[vertexEdges[offset].edgeCorner] {
				continue
			}

			oppositeCorner = vertexEdges[offset].edgeCorner
			for j := i + 1; j < numSinkCorners; j, offset = j+1, offset+1 {
				vertexEdges[offset] = vertexEdges[offset+1]
				if vertexEdges[offset].sinkVertex == -1 {
					break
				}
			}

			vertexEdges[offset].sinkVertex = -1
			vertexEdges[offset].edgeCorner = -1
			break
		}

		if oppositeCorner == -1 {
			numSourceCorners := numCornersOnVertices[sourceVertex]
			offset = vertexOffsets[sourceVertex]
			inserted := false
			for i := 0; i < numSourceCorners; i, offset = i+1, offset+1 {
				if vertexEdges[offset].sinkVertex != -1 {
					continue
				}

				vertexEdges[offset].sinkVertex = sinkVertex
				vertexEdges[offset].edgeCorner = corner
				inserted = true
				break
			}

			if !inserted {
				return fmt.Errorf("%w: failed to insert edgebreaker half-edge", ErrInvalidGeometry)
			}

			continue
		}

		table.SetOppositeCorners(corner, oppositeCorner)
	}

	return nil
}

func computeEdgebreakerBaseVertexCorners(table *edgebreakerMutableCornerTable, numVertices int, scratch *edgebreakerEncodeScratch) error {
	vertexCorners := table.vertexCorners
	for i := range vertexCorners {
		vertexCorners[i] = -1
	}

	table.numVertices = numVertices
	cornerCount := table.CornerCount()
	cornerVertex := table.cornerVertex
	opposite := table.opposite
	var visitedVertices []bool
	var visitedCorners []bool
	if scratch != nil {
		visitedVertices = growBoolSlice(scratch.baseVisitedVertices, table.maxVertices, false)
		scratch.baseVisitedVertices = visitedVertices
		visitedCorners = growBoolSlice(scratch.baseVisitedCorners, cornerCount, false)
		scratch.baseVisitedCorners = visitedCorners
	} else {
		visitedVertices = make([]bool, table.maxVertices)
		visitedCorners = make([]bool, cornerCount)
	}

	for faceID := 0; faceID < table.numFaces; faceID++ {
		firstCorner := faceID * 3
		for local := 0; local < 3; local++ {
			corner := firstCorner + local
			if visitedCorners[corner] {
				continue
			}

			vertex := table.cornerVertex[corner]
			if vertex < 0 || vertex >= table.numVertices {
				return fmt.Errorf("%w: invalid edgebreaker vertex %d", ErrInvalidGeometry, vertex)
			}

			isNonManifoldVertex := false
			if visitedVertices[vertex] {
				if table.numVertices >= table.maxVertices {
					return fmt.Errorf("%w: failed to split non-manifold edgebreaker vertex", ErrInvalidGeometry)
				}

				vertex = table.numVertices
				table.numVertices++
				isNonManifoldVertex = true
			}

			visitedVertices[vertex] = true
			activeCorner := corner
			for activeCorner >= 0 {
				visitedCorners[activeCorner] = true
				vertexCorners[vertex] = activeCorner
				if isNonManifoldVertex {
					cornerVertex[activeCorner] = vertex
				}

				activeCorner = swingLeftCorner(opposite, activeCorner)
				if activeCorner == corner {
					break
				}
			}

			if activeCorner >= 0 {
				continue
			}

			activeCorner = swingRightCorner(opposite, corner)
			for activeCorner >= 0 {
				visitedCorners[activeCorner] = true
				if isNonManifoldVertex {
					cornerVertex[activeCorner] = vertex
				}

				activeCorner = swingRightCorner(opposite, activeCorner)
			}
		}
	}

	return nil
}

func buildEdgebreakerAttributeConnectivity(base *edgebreakerMutableCornerTable, mesh *Mesh, attr *Attribute, reusable *edgebreakerAttributeConnectivity) (*edgebreakerAttributeConnectivity, error) {
	connectivity := resetEdgebreakerAttributeConnectivity(reusable, base.CornerCount(), base.VertexCount())
	for corner := 0; corner < base.CornerCount(); corner++ {
		opp := base.Opposite(corner)
		if opp < 0 {
			connectivity.markSeam(base, corner)
			continue
		}

		if opp < corner {
			continue
		}

		act := corner
		sibling := opp
		for i := 0; i < 2; i++ {
			act = base.Next(act)
			sibling = base.Previous(sibling)
			pointID := pointAtMeshCorner(mesh, act)
			siblingPointID := pointAtMeshCorner(mesh, sibling)
			if attr.mappedIndex(pointID) == attr.mappedIndex(siblingPointID) {
				continue
			}

			connectivity.markSeam(base, corner)
			break
		}
	}

	err := connectivity.recomputeVertices(base)
	return connectivity, err
}

func findEdgebreakerHoles(table *edgebreakerMutableCornerTable, scratch *edgebreakerEncodeScratch) ([]int, []bool, error) {
	var vertexHoleID []int
	var visitedHoles []bool
	if scratch != nil {
		vertexHoleID = growIntSlice(scratch.vertexHoleID, table.VertexCount(), -1)
		scratch.vertexHoleID = vertexHoleID
		visitedHoles = scratch.visitedHoles[:0]
		scratch.visitedHoles = visitedHoles
	} else {
		vertexHoleID = growIntSlice(nil, table.VertexCount(), -1)
	}

	for corner := 0; corner < table.CornerCount(); corner++ {
		if table.Opposite(corner) >= 0 {
			continue
		}

		boundaryVertex := table.Vertex(table.Next(corner))
		if boundaryVertex < 0 || boundaryVertex >= len(vertexHoleID) {
			return nil, nil, fmt.Errorf("%w: invalid edgebreaker boundary vertex %d", ErrInvalidGeometry, boundaryVertex)
		}

		if vertexHoleID[boundaryVertex] != -1 {
			continue
		}

		holeID := len(visitedHoles)
		visitedHoles = append(visitedHoles, false)
		currentCorner := corner
		for vertexHoleID[boundaryVertex] == -1 {
			vertexHoleID[boundaryVertex] = holeID
			currentCorner = table.Next(currentCorner)
			for table.Opposite(currentCorner) >= 0 {
				currentCorner = table.Opposite(currentCorner)
				currentCorner = table.Next(currentCorner)
			}

			boundaryVertex = table.Vertex(table.Next(currentCorner))
			if boundaryVertex < 0 || boundaryVertex >= len(vertexHoleID) {
				return nil, nil, fmt.Errorf("%w: invalid edgebreaker boundary vertex %d", ErrInvalidGeometry, boundaryVertex)
			}
		}
	}

	if scratch != nil {
		scratch.visitedHoles = visitedHoles
	}

	return vertexHoleID, visitedHoles, nil
}

func findEdgebreakerInitFaceConfiguration(table *edgebreakerMutableCornerTable, faceID int, vertexHoleID []int) (bool, int) {
	corner := faceID * 3
	for i := 0; i < 3; i++ {
		if table.Opposite(corner) < 0 {
			return false, corner
		}

		if vertexHoleID[table.Vertex(corner)] != -1 {
			rightCorner := corner
			for rightCorner >= 0 {
				corner = rightCorner
				rightCorner = table.SwingRight(rightCorner)
			}

			return false, table.Previous(corner)
		}

		corner = table.Next(corner)
	}

	return true, corner
}

func pointAtMeshCorner(mesh *Mesh, corner int) int {
	face := mesh.faces[corner/3]
	return int(face[corner%3])
}

func countNonIsolatedEdgebreakerVertices(table *edgebreakerMutableCornerTable) int {
	count := 0
	for vertex := 0; vertex < table.VertexCount(); vertex++ {
		if table.LeftMostCorner(vertex) >= 0 {
			count++
		}
	}

	return count
}

func isMeshPredictionMethod(method PredictionMethod) bool {
	switch method {
	case PredictionMethodParallelogram,
		PredictionMethodMultiParallelogram,
		PredictionMethodConstrainedMultiParallelogram,
		PredictionMethodTexCoordsPortable,
		PredictionMethodGeometricNormal:
		return true
	default:
		return false
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}

	return b
}

type recordedEdgebreakerTraversal struct {
	symbols                   []uint32
	startFaces                []bool
	seams                     [][]bool
	sourceConnectivityCorners []int
	sourceInitFaceCorners     []int
	sourceTable               *edgebreakerMutableCornerTable
	decodedCornerSource       []int
	symbolPos                 int
	startPos                  int
	seamPos                   []int
	sourceConnectivityPos     int
	sourceInitPos             int
}

func (t *recordedEdgebreakerTraversal) DecodeSymbol() (uint32, error) {
	index := len(t.symbols) - 1 - t.symbolPos
	if index < 0 {
		return 0, errors.New("draco: truncated recorded edgebreaker symbols")
	}

	symbol := t.symbols[index]
	t.symbolPos++
	return symbol, nil
}

func (t *recordedEdgebreakerTraversal) DecodeStartFaceConfiguration() (bool, error) {
	if t.startPos >= len(t.startFaces) {
		return false, errors.New("draco: truncated recorded edgebreaker start-face bits")
	}

	value := t.startFaces[t.startPos]
	t.startPos++
	return value, nil
}

func (t *recordedEdgebreakerTraversal) DecodeAttributeSeam(attribute int) (bool, error) {
	if attribute < 0 || attribute >= len(t.seams) {
		return false, nil
	}

	if t.seamPos == nil {
		t.seamPos = make([]int, len(t.seams))
	}

	if t.seamPos[attribute] >= len(t.seams[attribute]) {
		return false, errors.New("draco: truncated recorded edgebreaker seam bits")
	}

	value := t.seams[attribute][t.seamPos[attribute]]
	t.seamPos[attribute]++
	return value, nil
}

func (t *recordedEdgebreakerTraversal) NewActiveCornerReached(corner int) {
	if t.sourceConnectivityPos >= len(t.sourceConnectivityCorners) {
		return
	}

	t.recordFaceSource(corner, t.sourceConnectivityCorners[t.sourceConnectivityPos])
	t.sourceConnectivityPos++
}

func (t *recordedEdgebreakerTraversal) NewInteriorFaceReached(corner int) {
	if t.sourceInitPos >= len(t.sourceInitFaceCorners) {
		return
	}

	t.recordFaceSource(corner, t.sourceInitFaceCorners[t.sourceInitPos])
	t.sourceInitPos++
}

func (t *recordedEdgebreakerTraversal) MergeVertices(int, int) {}

func (t *recordedEdgebreakerTraversal) recordFaceSource(decodedCorner, sourceCorner int) {
	if t.sourceTable == nil || decodedCorner < 0 || decodedCorner+2 >= len(t.decodedCornerSource) {
		return
	}

	t.decodedCornerSource[decodedCorner] = sourceCorner
	t.decodedCornerSource[decodedCorner+1] = t.sourceTable.Next(sourceCorner)
	t.decodedCornerSource[decodedCorner+2] = t.sourceTable.Previous(sourceCorner)
}

func reverseUint32s(values []uint32) []uint32 {
	out := append([]uint32(nil), values...)
	slices.Reverse(out)
	return out
}

func normalizeEdgePair(edge edgePair) edgePair {
	if edge.a > edge.b {
		edge.a, edge.b = edge.b, edge.a
	}

	return edge
}

func cloneBoolMatrix(values [][]bool) [][]bool {
	out := make([][]bool, len(values))
	for i := range values {
		out[i] = append([]bool(nil), values[i]...)
	}

	return out
}

func buildValenceContextSymbols(encoder *edgebreakerMeshEncoder, rawSymbols []uint32, startFaces []bool, seams [][]bool) ([][]uint32, error) {
	if len(rawSymbols) == 0 {
		return make([][]uint32, maxValence-minValence+1), nil
	}

	decodedSymbols := reverseUint32s(rawSymbols)
	if decodedSymbols[0] != topologyE {
		return nil, fmt.Errorf("%w: valence edgebreaker requires final topology E symbol", ErrUnsupportedFeature)
	}

	maxVertices := countNonIsolatedEdgebreakerVertices(encoder.table) + int(encoder.numSplitSymbols)
	decodedTable := newEdgebreakerMutableCornerTable(encoder.table.FaceCount(), maxVertices)
	attrConnectivity := make([]*edgebreakerAttributeConnectivity, len(encoder.attributeData))
	for i := range attrConnectivity {
		attrConnectivity[i] = newEdgebreakerAttributeConnectivity(decodedTable.CornerCount(), maxVertices)
	}

	isVertHole := make([]bool, maxVertices)
	for i := range isVertHole {
		isVertHole[i] = true
	}

	recorder := newValenceContextRecorder(decodedSymbols, startFaces, seams, maxVertices)
	recorder.table = decodedTable
	if _, err := decodeEdgebreakerConnectivity(encoder.ctx, decodedTable, recorder, encoder.table.FaceCount(), len(decodedSymbols), encoder.splitEvents, attrConnectivity, isVertHole, nil); err != nil {
		return nil, err
	}

	for i := range recorder.contextSymbols {
		slices.Reverse(recorder.contextSymbols[i])
	}

	return recorder.contextSymbols, nil
}

type valenceContextRecorder struct {
	decodedSymbols []uint32
	startFaces     []bool
	seams          [][]bool
	vertexValences []int
	contextSymbols [][]uint32
	table          *edgebreakerMutableCornerTable
	lastSymbol     uint32
	activeContext  int
	symbolPos      int
	startPos       int
	seamPos        []int
}

func newValenceContextRecorder(decodedSymbols []uint32, startFaces []bool, seams [][]bool, maxVertices int) *valenceContextRecorder {
	return &valenceContextRecorder{
		decodedSymbols: decodedSymbols,
		startFaces:     append([]bool(nil), startFaces...),
		seams:          cloneBoolMatrix(seams),
		vertexValences: make([]int, maxVertices),
		contextSymbols: make([][]uint32, maxValence-minValence+1),
		activeContext:  -1,
	}
}

func (r *valenceContextRecorder) DecodeSymbol() (uint32, error) {
	if r.symbolPos >= len(r.decodedSymbols) {
		return 0, errors.New("draco: truncated recorded valence symbols")
	}

	symbol := r.decodedSymbols[r.symbolPos]
	r.symbolPos++
	r.lastSymbol = symbol
	return symbol, nil
}

func (r *valenceContextRecorder) DecodeStartFaceConfiguration() (bool, error) {
	if r.startPos >= len(r.startFaces) {
		return false, errors.New("draco: truncated recorded valence start-face bits")
	}

	value := r.startFaces[r.startPos]
	r.startPos++
	return value, nil
}

func (r *valenceContextRecorder) DecodeAttributeSeam(attribute int) (bool, error) {
	if attribute < 0 || attribute >= len(r.seams) {
		return false, nil
	}

	if r.seamPos == nil {
		r.seamPos = make([]int, len(r.seams))
	}

	if r.seamPos[attribute] >= len(r.seams[attribute]) {
		return false, errors.New("draco: truncated recorded valence seam bits")
	}

	value := r.seams[attribute][r.seamPos[attribute]]
	r.seamPos[attribute]++
	return value, nil
}

func (r *valenceContextRecorder) NewActiveCornerReached(corner int) {
	if r.table == nil {
		return
	}

	next := r.table.Next(corner)
	prev := r.table.Previous(corner)
	switch r.lastSymbol {
	case topologyC, topologyS:
		r.vertexValences[r.table.Vertex(next)]++
		r.vertexValences[r.table.Vertex(prev)]++
	case topologyR:
		r.vertexValences[r.table.Vertex(corner)]++
		r.vertexValences[r.table.Vertex(next)]++
		r.vertexValences[r.table.Vertex(prev)] += 2
	case topologyL:
		r.vertexValences[r.table.Vertex(corner)]++
		r.vertexValences[r.table.Vertex(next)] += 2
		r.vertexValences[r.table.Vertex(prev)]++
	case topologyE:
		r.vertexValences[r.table.Vertex(corner)] += 2
		r.vertexValences[r.table.Vertex(next)] += 2
		r.vertexValences[r.table.Vertex(prev)] += 2
	}

	activeValence := r.vertexValences[r.table.Vertex(next)]
	if activeValence < minValence {
		activeValence = minValence
	} else if activeValence > maxValence {
		activeValence = maxValence
	}

	r.activeContext = activeValence - minValence
	if r.symbolPos >= len(r.decodedSymbols) {
		return
	}

	symbolID, ok := edgeBreakerSymbolID(r.decodedSymbols[r.symbolPos])
	if !ok {
		return
	}

	r.contextSymbols[r.activeContext] = append(r.contextSymbols[r.activeContext], symbolID)
}

func (r *valenceContextRecorder) NewInteriorFaceReached(corner int) {}

func (r *valenceContextRecorder) MergeVertices(dest, source int) {
	if dest < 0 || source < 0 || dest >= len(r.vertexValences) || source >= len(r.vertexValences) {
		return
	}

	r.vertexValences[dest] += r.vertexValences[source]
}

var edgeBreakerBitPatternLength = [...]int{
	topologyC: 1,
	topologyS: 3,
	topologyL: 3,
	topologyR: 3,
	topologyE: 3,
}
