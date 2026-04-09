package topology

import (
	"errors"
	"fmt"
)

const (
	InvalidCorner = -1
	InvalidFace   = -1
	InvalidVertex = -1
)

type Face [3]uint32

type CornerTable struct {
	faces         []Face
	cornerVertex  []int
	opposite      []int
	vertexCorners []int
}

// NewCornerTableFromConnectivity wraps precomputed connectivity slices.
// The caller owns the slices and must keep them stable while the table is used.
func NewCornerTableFromConnectivity(cornerVertex, opposite, vertexCorners []int) *CornerTable {
	return &CornerTable{
		cornerVertex:  cornerVertex,
		opposite:      opposite,
		vertexCorners: vertexCorners,
	}
}

// ResetCornerTableFromConnectivity reuses an existing table wrapper for
// precomputed connectivity owned by the caller.
func ResetCornerTableFromConnectivity(table *CornerTable, cornerVertex, opposite, vertexCorners []int) *CornerTable {
	if table == nil {
		return NewCornerTableFromConnectivity(cornerVertex, opposite, vertexCorners)
	}

	table.faces = nil
	table.cornerVertex = cornerVertex
	table.opposite = opposite
	table.vertexCorners = vertexCorners
	return table
}

type edgeKey struct {
	a uint32
	b uint32
}

func BuildCornerTable(numVertices int, faces []Face) (*CornerTable, error) {
	if numVertices < 0 {
		return nil, fmt.Errorf("draco: invalid vertex count %d", numVertices)
	}

	table := &CornerTable{
		faces:         append([]Face(nil), faces...),
		cornerVertex:  make([]int, len(faces)*3),
		opposite:      make([]int, len(faces)*3),
		vertexCorners: make([]int, numVertices),
	}
	for i := range table.opposite {
		table.opposite[i] = InvalidCorner
	}

	for i := range table.vertexCorners {
		table.vertexCorners[i] = InvalidCorner
	}

	edges := make(map[edgeKey]int, len(faces)*3)
	for faceID, face := range faces {
		if isDegenerateFace(face) {
			return nil, fmt.Errorf("draco: degenerate face %d", faceID)
		}

		for local, vertex := range face {
			if int(vertex) >= numVertices {
				return nil, fmt.Errorf("draco: face %d corner %d index %d out of range for %d vertices", faceID, local, vertex, numVertices)
			}

			corner := faceID*3 + local
			table.cornerVertex[corner] = int(vertex)
			if table.vertexCorners[vertex] != InvalidCorner {
				continue
			}

			table.vertexCorners[vertex] = corner
		}

		for local := range face {
			corner := faceID*3 + local
			a, b := edgeVertices(face, local)
			key := makeEdgeKey(a, b)
			if other, ok := edges[key]; ok {
				if table.opposite[other] != InvalidCorner {
					return nil, fmt.Errorf("draco: non-manifold edge %d-%d", key.a, key.b)
				}

				table.opposite[corner] = other
				table.opposite[other] = corner
				continue
			}

			edges[key] = corner
		}
	}

	for vertex := range table.vertexCorners {
		best := table.vertexCorners[vertex]
		if best == InvalidCorner {
			continue
		}

		for corner, mappedVertex := range table.cornerVertex {
			if mappedVertex != vertex {
				continue
			}

			if table.SwingLeft(corner) == InvalidCorner {
				best = corner
				break
			}
		}

		table.vertexCorners[vertex] = best
	}

	return table, nil
}

func (t *CornerTable) VertexCount() int {
	if t == nil {
		return 0
	}

	return len(t.vertexCorners)
}

func (t *CornerTable) FaceCount() int {
	if t == nil {
		return 0
	}

	return len(t.faces)
}

func (t *CornerTable) CornerCount() int {
	if t == nil {
		return 0
	}

	return len(t.cornerVertex)
}

func (t *CornerTable) Opposite(corner int) int {
	if t == nil || corner < 0 || corner >= len(t.opposite) {
		return InvalidCorner
	}

	return t.opposite[corner]
}

func (t *CornerTable) Next(corner int) int {
	if corner == InvalidCorner || t == nil || corner < 0 || corner >= len(t.cornerVertex) {
		return InvalidCorner
	}

	if corner%3 == 2 {
		return corner - 2
	}

	return corner + 1
}

func (t *CornerTable) Previous(corner int) int {
	if corner == InvalidCorner || t == nil || corner < 0 || corner >= len(t.cornerVertex) {
		return InvalidCorner
	}

	if corner%3 == 0 {
		return corner + 2
	}

	return corner - 1
}

func (t *CornerTable) Vertex(corner int) int {
	if t == nil || corner < 0 || corner >= len(t.cornerVertex) {
		return InvalidVertex
	}

	return t.cornerVertex[corner]
}

func (t *CornerTable) Face(corner int) int {
	if t == nil || corner < 0 || corner >= len(t.cornerVertex) {
		return InvalidFace
	}

	return corner / 3
}

func (t *CornerTable) FirstCorner(face int) int {
	if t == nil || face < 0 || face >= len(t.faces) {
		return InvalidCorner
	}

	return face * 3
}

func (t *CornerTable) AllCorners(face int) [3]int {
	first := t.FirstCorner(face)
	if first == InvalidCorner {
		return [3]int{InvalidCorner, InvalidCorner, InvalidCorner}
	}

	return [3]int{first, first + 1, first + 2}
}

func (t *CornerTable) LocalIndex(corner int) int {
	if corner == InvalidCorner {
		return -1
	}

	return corner % 3
}

func (t *CornerTable) LeftMostCorner(vertex int) int {
	if t == nil || vertex < 0 || vertex >= len(t.vertexCorners) {
		return InvalidCorner
	}

	return t.vertexCorners[vertex]
}

func (t *CornerTable) SwingRight(corner int) int {
	return t.Previous(t.Opposite(t.Previous(corner)))
}

func (t *CornerTable) SwingLeft(corner int) int {
	return t.Next(t.Opposite(t.Next(corner)))
}

func (t *CornerTable) LeftCorner(corner int) int {
	if corner == InvalidCorner {
		return InvalidCorner
	}

	return t.Opposite(t.Previous(corner))
}

func (t *CornerTable) RightCorner(corner int) int {
	if corner == InvalidCorner {
		return InvalidCorner
	}

	return t.Opposite(t.Next(corner))
}

func (t *CornerTable) IsBoundaryVertex(vertex int) bool {
	corner := t.LeftMostCorner(vertex)
	if corner == InvalidCorner {
		return false
	}

	return t.SwingLeft(corner) == InvalidCorner
}

func (t *CornerTable) Valence(vertex int) int {
	if t == nil || vertex < 0 || vertex >= len(t.vertexCorners) {
		return -1
	}

	neighbors := make(map[int]struct{})
	for _, face := range t.faces {
		for local, faceVertex := range face {
			if int(faceVertex) != vertex {
				continue
			}

			neighbors[int(face[(local+1)%3])] = struct{}{}
			neighbors[int(face[(local+2)%3])] = struct{}{}
		}
	}

	return len(neighbors)
}

func (t *CornerTable) FaceNeighbors(face int) [3]int {
	out := [3]int{InvalidFace, InvalidFace, InvalidFace}
	first := t.FirstCorner(face)
	if first == InvalidCorner {
		return out
	}

	for i := 0; i < 3; i++ {
		opposite := t.Opposite(first + i)
		if opposite != InvalidCorner {
			out[i] = t.Face(opposite)
		}
	}

	return out
}

func (t *CornerTable) AddVertex() int {
	if t == nil {
		return InvalidVertex
	}

	t.vertexCorners = append(t.vertexCorners, InvalidCorner)
	return len(t.vertexCorners) - 1
}

func (t *CornerTable) AddFace(face Face) (int, error) {
	if t == nil {
		return InvalidFace, errors.New("draco: corner table is nil")
	}

	for local, vertex := range face {
		if int(vertex) < 0 || int(vertex) >= len(t.vertexCorners) {
			return InvalidFace, fmt.Errorf("draco: face corner %d index %d out of range for %d vertices", local, vertex, len(t.vertexCorners))
		}
	}

	if isDegenerateFace(face) {
		return InvalidFace, errors.New("draco: degenerate face")
	}

	faceID := len(t.faces)
	t.faces = append(t.faces, face)
	firstCorner := len(t.cornerVertex)
	for local, vertex := range face {
		t.cornerVertex = append(t.cornerVertex, int(vertex))
		t.opposite = append(t.opposite, InvalidCorner)
		corner := firstCorner + local
		if t.vertexCorners[vertex] == InvalidCorner || t.SwingLeft(t.vertexCorners[vertex]) != InvalidCorner {
			t.vertexCorners[vertex] = corner
		}
	}

	for local := range face {
		corner := firstCorner + local
		a, b := edgeVertices(face, local)
		for other := 0; other < firstCorner; other++ {
			otherFace := t.faces[t.Face(other)]
			oa, ob := edgeVertices(otherFace, t.LocalIndex(other))
			if makeEdgeKey(a, b) != makeEdgeKey(oa, ob) {
				continue
			}

			if t.opposite[other] != InvalidCorner {
				return InvalidFace, fmt.Errorf("draco: non-manifold edge %d-%d", minUint32(a, b), maxUint32(a, b))
			}

			t.opposite[corner] = other
			t.opposite[other] = corner
			break
		}
	}

	return faceID, nil
}

func isDegenerateFace(face Face) bool {
	return face[0] == face[1] || face[1] == face[2] || face[0] == face[2]
}

func edgeVertices(face Face, local int) (uint32, uint32) {
	return face[(local+1)%3], face[(local+2)%3]
}

func makeEdgeKey(a, b uint32) edgeKey {
	if a > b {
		a, b = b, a
	}

	return edgeKey{a: a, b: b}
}

func minUint32(a, b uint32) uint32 {
	if a < b {
		return a
	}

	return b
}

func maxUint32(a, b uint32) uint32 {
	if a > b {
		return a
	}

	return b
}
