package draco

import (
	"errors"
	"fmt"

	"github.com/gophics/go-draco/internal/bitstream"
	"github.com/gophics/go-draco/internal/core"
)

const (
	edgebreakerTraversalStandard   = 0
	edgebreakerTraversalPredictive = 1
	edgebreakerTraversalValence    = 2

	meshVertexAttribute = 0
	meshCornerAttribute = 1

	meshTraversalDepthFirst       = 0
	meshTraversalPredictionDegree = 1

	topologyC        = 0x0
	topologyS        = 0x1
	topologyL        = 0x3
	topologyR        = 0x5
	topologyE        = 0x7
	topologyInitFace = 0x8
	topologyInvalid  = 0x9

	leftFaceEdge  = 0
	rightFaceEdge = 1

	minValence = 2
	maxValence = 7
)

var edgeBreakerSymbolToTopology = [...]uint32{
	topologyC,
	topologyS,
	topologyL,
	topologyR,
	topologyE,
}

func edgeBreakerSymbolID(topology uint32) (uint32, bool) {
	switch topology {
	case topologyC:
		return 0, true
	case topologyS:
		return 1, true
	case topologyL:
		return 2, true
	case topologyR:
		return 3, true
	case topologyE:
		return 4, true
	default:
		return 0, false
	}
}

func readVersionedEdgebreakerVarUint32(reader *core.Reader, header bitstream.Header) (uint32, error) {
	if header.VersionMajor < 2 {
		return reader.ReadUint32()
	}

	return core.DecodeVarUint32(reader)
}

func readSizedEdgebreakerBitPayload(r *core.Reader, header bitstream.Header) ([]byte, error) {
	var size uint64
	var err error
	if header.VersionMajor < 2 || (header.VersionMajor == 2 && header.VersionMinor < 2) {
		size, err = r.ReadUint64()
	} else {
		size, err = core.DecodeVarUint64(r)
	}

	if err != nil {
		return nil, err
	}

	if size > uint64(r.Remaining()) {
		return nil, errors.New("draco: invalid edgebreaker bit payload size")
	}

	return r.ReadBytesView(int(size))
}

func writeEdgebreakerSymbolPayload(w *core.Writer, symbols []uint32, bitPatternLengths []int) error {
	bits := core.NewBitWriter(len(symbols) * 3)
	for i := len(symbols) - 1; i >= 0; i-- {
		symbol := symbols[i]
		if symbol >= uint32(len(bitPatternLengths)) {
			return fmt.Errorf("draco: invalid edgebreaker symbol %d", symbol)
		}

		if !bits.WriteBitsLSB(symbol, bitPatternLengths[symbol]) {
			return errors.New("draco: failed to bit-pack edgebreaker symbol")
		}
	}

	payload := bits.BytesView()
	if err := core.EncodeVarUint64(w, uint64(len(payload))); err != nil {
		return err
	}

	return w.WriteBytes(payload)
}

type edgebreakerTopologySplit struct {
	sourceSymbolID uint32
	splitSymbolID  uint32
	sourceEdge     uint8
}

type edgebreakerMutableCornerTable struct {
	numFaces      int
	maxVertices   int
	numVertices   int
	cornerVertex  []int
	opposite      []int
	vertexCorners []int

	trackVertexCorners bool
	vertexCornerNext   []int
	vertexCornerHead   []int
}

func newEdgebreakerMutableCornerTable(numFaces, maxVertices int) *edgebreakerMutableCornerTable {
	return resetEdgebreakerMutableCornerTable(nil, numFaces, maxVertices)
}

func resetEdgebreakerMutableCornerTable(table *edgebreakerMutableCornerTable, numFaces, maxVertices int) *edgebreakerMutableCornerTable {
	cornerCount := numFaces * 3
	if table == nil {
		table = &edgebreakerMutableCornerTable{}
	}

	table.numFaces = numFaces
	table.maxVertices = maxVertices
	table.numVertices = 0
	table.cornerVertex = growIntSlice(table.cornerVertex, cornerCount, -1)
	table.opposite = growIntSlice(table.opposite, cornerCount, -1)
	table.vertexCorners = growIntSlice(table.vertexCorners, maxVertices, -1)
	table.trackVertexCorners = false
	return table
}

func (t *edgebreakerMutableCornerTable) enableVertexCornerTracking() {
	if t == nil {
		return
	}

	t.vertexCornerNext = growIntSlice(t.vertexCornerNext, len(t.cornerVertex), -1)
	t.vertexCornerHead = growIntSlice(t.vertexCornerHead, t.maxVertices, -1)
	t.trackVertexCorners = true
}

func (t *edgebreakerMutableCornerTable) FaceCount() int {
	if t == nil {
		return 0
	}

	return t.numFaces
}

func (t *edgebreakerMutableCornerTable) CornerCount() int {
	if t == nil {
		return 0
	}

	return len(t.cornerVertex)
}

func (t *edgebreakerMutableCornerTable) VertexCount() int {
	if t == nil {
		return 0
	}

	return t.numVertices
}

func (t *edgebreakerMutableCornerTable) Face(corner int) int {
	if t == nil || corner < 0 || corner >= len(t.cornerVertex) {
		return -1
	}

	return corner / 3
}

func (t *edgebreakerMutableCornerTable) Next(corner int) int {
	if corner < 0 {
		return -1
	}

	return nextCorner(corner)
}

func (t *edgebreakerMutableCornerTable) Previous(corner int) int {
	if corner < 0 {
		return -1
	}

	return previousCorner(corner)
}

func nextCorner(corner int) int {
	if corner%3 == 2 {
		return corner - 2
	}

	return corner + 1
}

func previousCorner(corner int) int {
	if corner%3 == 0 {
		return corner + 2
	}

	return corner - 1
}

func swingRightCorner(opposite []int, corner int) int {
	if corner < 0 {
		return -1
	}

	opp := opposite[previousCorner(corner)]
	if opp < 0 {
		return -1
	}

	return previousCorner(opp)
}

func swingLeftCorner(opposite []int, corner int) int {
	if corner < 0 {
		return -1
	}

	opp := opposite[nextCorner(corner)]
	if opp < 0 {
		return -1
	}

	return nextCorner(opp)
}

func (t *edgebreakerMutableCornerTable) Opposite(corner int) int {
	if t == nil || corner < 0 || corner >= len(t.opposite) {
		return -1
	}

	return t.opposite[corner]
}

func (t *edgebreakerMutableCornerTable) Vertex(corner int) int {
	if t == nil || corner < 0 || corner >= len(t.cornerVertex) {
		return -1
	}

	return t.cornerVertex[corner]
}

func (t *edgebreakerMutableCornerTable) MapCornerToVertex(corner, vertex int) {
	if t == nil || corner < 0 || corner >= len(t.cornerVertex) || vertex < 0 || vertex >= len(t.vertexCorners) {
		return
	}

	t.mapCornerToVertexUnchecked(corner, vertex)
}

func (t *edgebreakerMutableCornerTable) mapCornerToVertexUnchecked(corner, vertex int) {
	if t.trackVertexCorners && t.cornerVertex[corner] < 0 {
		t.vertexCornerNext[corner] = t.vertexCornerHead[vertex]
		t.vertexCornerHead[vertex] = corner
	}

	t.cornerVertex[corner] = vertex
}

func (t *edgebreakerMutableCornerTable) SetOppositeCorners(a, b int) {
	if t == nil || a < 0 || b < 0 || a >= len(t.opposite) || b >= len(t.opposite) {
		return
	}

	t.setOppositeCornersUnchecked(a, b)
}

func (t *edgebreakerMutableCornerTable) setOppositeCornersUnchecked(a, b int) {
	t.opposite[a] = b
	t.opposite[b] = a
}

func (t *edgebreakerMutableCornerTable) LeftMostCorner(vertex int) int {
	if t == nil || vertex < 0 || vertex >= len(t.vertexCorners) {
		return -1
	}

	return t.vertexCorners[vertex]
}

func (t *edgebreakerMutableCornerTable) SetLeftMostCorner(vertex, corner int) {
	if t == nil || vertex < 0 || vertex >= len(t.vertexCorners) {
		return
	}

	t.vertexCorners[vertex] = corner
}

func (t *edgebreakerMutableCornerTable) SwingRight(corner int) int {
	if t == nil || corner < 0 || corner >= len(t.opposite) {
		return -1
	}

	return swingRightCorner(t.opposite, corner)
}

func (t *edgebreakerMutableCornerTable) SwingLeft(corner int) int {
	if t == nil || corner < 0 || corner >= len(t.opposite) {
		return -1
	}

	return swingLeftCorner(t.opposite, corner)
}

func (t *edgebreakerMutableCornerTable) LeftCorner(corner int) int {
	if corner < 0 {
		return -1
	}

	return t.Opposite(t.Previous(corner))
}

func (t *edgebreakerMutableCornerTable) RightCorner(corner int) int {
	if corner < 0 {
		return -1
	}

	return t.Opposite(t.Next(corner))
}

func (t *edgebreakerMutableCornerTable) AddNewVertex() int {
	if t == nil || t.numVertices >= t.maxVertices {
		return -1
	}

	vertex := t.numVertices
	t.numVertices++
	return vertex
}

func (t *edgebreakerMutableCornerTable) MakeVertexIsolated(vertex int) {
	if t == nil || vertex < 0 || vertex >= len(t.vertexCorners) {
		return
	}

	t.vertexCorners[vertex] = -1
	if t.trackVertexCorners {
		t.vertexCornerHead[vertex] = -1
	}
}

func (t *edgebreakerMutableCornerTable) IsBoundaryVertex(vertex int) bool {
	if t == nil || vertex < 0 || vertex >= t.numVertices {
		return false
	}

	corner := t.LeftMostCorner(vertex)
	if corner < 0 {
		return true
	}

	return t.SwingLeft(corner) < 0
}

func (t *edgebreakerMutableCornerTable) UpdateVertexToCornerMap(vertex int) {
	if t == nil || vertex < 0 || vertex >= t.numVertices {
		return
	}

	first := t.vertexCorners[vertex]
	if first < 0 {
		return
	}

	act := t.SwingLeft(first)
	best := first
	for act >= 0 && act != first {
		best = act
		act = t.SwingLeft(act)
	}

	if act != first {
		t.vertexCorners[vertex] = best
	}
}

func (t *edgebreakerMutableCornerTable) Valence(vertex int) int {
	if t == nil || vertex < 0 || vertex >= t.numVertices {
		return 0
	}

	start := t.LeftMostCorner(vertex)
	if start < 0 {
		return 0
	}

	valence := 1
	act := t.SwingRight(start)
	for act >= 0 && act != start {
		valence++
		act = t.SwingRight(act)
	}

	if act == start {
		return valence
	}

	for act = t.SwingLeft(start); act >= 0; act = t.SwingLeft(act) {
		valence++
	}

	return valence
}

func (t *edgebreakerMutableCornerTable) FaceVertexTriplet(corner int, cornerToVertex []int) (int, int, int) {
	if corner < 0 {
		return -1, -1, -1
	}

	local := corner % 3
	if cornerToVertex == nil {
		cornerToVertex = t.cornerVertex
	}

	face := corner / 3
	base := face * 3
	switch local {
	case 0:
		return cornerToVertex[base], cornerToVertex[base+1], cornerToVertex[base+2]
	case 1:
		return cornerToVertex[base+1], cornerToVertex[base+2], cornerToVertex[base]
	default:
		return cornerToVertex[base+2], cornerToVertex[base], cornerToVertex[base+1]
	}
}

type edgebreakerAttributeConnectivity struct {
	seamEdges       []bool
	vertexOnSeam    []bool
	cornerToVertex  []int
	leftMostCorners []int
	numVertices     int
	noInteriorSeams bool
}

func newEdgebreakerAttributeConnectivity(numCorners, numVertices int) *edgebreakerAttributeConnectivity {
	return &edgebreakerAttributeConnectivity{
		seamEdges:       make([]bool, numCorners),
		vertexOnSeam:    make([]bool, numVertices),
		cornerToVertex:  make([]int, numCorners),
		noInteriorSeams: true,
	}
}

func resetEdgebreakerAttributeConnectivity(c *edgebreakerAttributeConnectivity, numCorners, numVertices int) *edgebreakerAttributeConnectivity {
	if c == nil {
		return newEdgebreakerAttributeConnectivity(numCorners, numVertices)
	}

	c.seamEdges = growBoolSlice(c.seamEdges, numCorners, false)
	c.vertexOnSeam = growBoolSlice(c.vertexOnSeam, numVertices, false)
	c.cornerToVertex = growIntSlice(c.cornerToVertex, numCorners, 0)
	c.leftMostCorners = c.leftMostCorners[:0]
	c.numVertices = 0
	c.noInteriorSeams = true
	return c
}

func (c *edgebreakerAttributeConnectivity) resetScratch() {
	if c == nil {
		return
	}

	c.seamEdges = resetScratchSlice(c.seamEdges)
	c.vertexOnSeam = resetScratchSlice(c.vertexOnSeam)
	c.cornerToVertex = resetScratchSlice(c.cornerToVertex)
	c.leftMostCorners = resetScratchSlice(c.leftMostCorners)
	c.numVertices = 0
	c.noInteriorSeams = true
}

func (c *edgebreakerAttributeConnectivity) markSeam(base *edgebreakerMutableCornerTable, corner int) {
	if c == nil || base == nil || corner < 0 || corner >= len(c.seamEdges) {
		return
	}

	c.seamEdges[corner] = true
	next := base.Vertex(base.Next(corner))
	prev := base.Vertex(base.Previous(corner))
	if next >= 0 && next < len(c.vertexOnSeam) {
		c.vertexOnSeam[next] = true
	}

	if prev >= 0 && prev < len(c.vertexOnSeam) {
		c.vertexOnSeam[prev] = true
	}

	opp := base.Opposite(corner)
	if opp >= 0 {
		c.seamEdges[opp] = true
		next = base.Vertex(base.Next(opp))
		prev = base.Vertex(base.Previous(opp))
		if next >= 0 && next < len(c.vertexOnSeam) {
			c.vertexOnSeam[next] = true
		}

		if prev >= 0 && prev < len(c.vertexOnSeam) {
			c.vertexOnSeam[prev] = true
		}

		c.noInteriorSeams = false
	}
}

func (c *edgebreakerAttributeConnectivity) recomputeVertices(base *edgebreakerMutableCornerTable) error {
	if c == nil || base == nil {
		return nil
	}

	for i := range c.cornerToVertex {
		c.cornerToVertex[i] = -1
	}

	c.leftMostCorners = c.leftMostCorners[:0]
	c.numVertices = 0

	for vertex := 0; vertex < base.VertexCount(); vertex++ {
		corner := base.LeftMostCorner(vertex)
		if corner < 0 {
			continue
		}

		currentVertex := c.numVertices
		c.numVertices++
		firstCorner := corner
		if c.vertexOnSeam[vertex] {
			act := c.SwingLeft(base, firstCorner)
			for act >= 0 {
				firstCorner = act
				act = c.SwingLeft(base, act)
				if act == corner {
					return ErrInvalidGeometry
				}
			}
		}

		c.cornerToVertex[firstCorner] = currentVertex
		c.leftMostCorners = append(c.leftMostCorners, firstCorner)
		act := base.SwingRight(firstCorner)
		for act >= 0 && act != firstCorner {
			if c.seamEdges[base.Next(act)] {
				currentVertex = c.numVertices
				c.numVertices++
				c.leftMostCorners = append(c.leftMostCorners, act)
			}

			c.cornerToVertex[act] = currentVertex
			act = base.SwingRight(act)
		}
	}

	// Non-manifold position connectivity can leave disjoint corner fragments after
	// edge breaking. Assign any leftover fragments to fresh attribute vertices so
	// edgebreaker sequencing can still traverse them deterministically.
	for corner := 0; corner < base.CornerCount(); corner++ {
		if c.cornerToVertex[corner] >= 0 {
			continue
		}

		currentVertex := c.numVertices
		c.numVertices++
		c.cornerToVertex[corner] = currentVertex
		c.leftMostCorners = append(c.leftMostCorners, corner)
		act := base.SwingRight(corner)
		for act >= 0 && act != corner && c.cornerToVertex[act] < 0 {
			if c.seamEdges[base.Next(act)] {
				currentVertex = c.numVertices
				c.numVertices++
				c.leftMostCorners = append(c.leftMostCorners, act)
			}

			c.cornerToVertex[act] = currentVertex
			act = base.SwingRight(act)
		}
	}

	return nil
}

func (c *edgebreakerAttributeConnectivity) Opposite(base *edgebreakerMutableCornerTable, corner int) int {
	if c == nil || base == nil || corner < 0 {
		return -1
	}

	if c.seamEdges[corner] {
		return -1
	}

	return base.Opposite(corner)
}

func (c *edgebreakerAttributeConnectivity) SwingRight(base *edgebreakerMutableCornerTable, corner int) int {
	return base.Previous(c.Opposite(base, base.Previous(corner)))
}

func (c *edgebreakerAttributeConnectivity) SwingLeft(base *edgebreakerMutableCornerTable, corner int) int {
	return base.Next(c.Opposite(base, base.Next(corner)))
}
