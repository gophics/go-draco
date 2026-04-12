package draco

import (
	"fmt"
	"sort"
)

type MeshConnectedComponent struct {
	Vertices      []int
	Faces         []int
	BoundaryEdges [][2]uint32
}

type meshEdgeKey struct {
	a uint32
	b uint32
}

func (m *Mesh) ConnectedComponents() ([]MeshConnectedComponent, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}

	position := m.namedAttribute(AttributePosition)
	if position == nil {
		return nil, fmt.Errorf("%w: mesh position attribute missing", ErrInvalidGeometry)
	}

	adjacency, edgeFaces, faceEdges := meshAdjacency(m.faces, position)
	visited := make([]bool, len(m.faces))
	components := make([]MeshConnectedComponent, 0)
	for faceID := range m.faces {
		if visited[faceID] || IsDegenerateFace(m.faces[faceID]) {
			continue
		}

		stack := []int{faceID}
		visited[faceID] = true
		component := MeshConnectedComponent{}
		vertexSet := make(map[int]struct{})
		boundarySet := make(map[[2]uint32]struct{})
		for len(stack) > 0 {
			current := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			component.Faces = append(component.Faces, current)
			for _, pointID := range m.faces[current] {
				vertexSet[int(pointID)] = struct{}{}
			}

			for _, edge := range faceEdges[current] {
				if len(edgeFaces[edge]) == 1 {
					boundarySet[[2]uint32{edge.a, edge.b}] = struct{}{}
				}
			}

			for _, next := range adjacency[current] {
				if visited[next] {
					continue
				}

				visited[next] = true
				stack = append(stack, next)
			}
		}

		component.Vertices = make([]int, 0, len(vertexSet))
		for vertex := range vertexSet {
			component.Vertices = append(component.Vertices, vertex)
		}

		sort.Ints(component.Vertices)
		sort.Ints(component.Faces)
		component.BoundaryEdges = make([][2]uint32, 0, len(boundarySet))
		for edge := range boundarySet {
			component.BoundaryEdges = append(component.BoundaryEdges, edge)
		}

		sort.Slice(component.BoundaryEdges, func(i, j int) bool {
			if component.BoundaryEdges[i][0] != component.BoundaryEdges[j][0] {
				return component.BoundaryEdges[i][0] < component.BoundaryEdges[j][0]
			}

			return component.BoundaryEdges[i][1] < component.BoundaryEdges[j][1]
		})
		components = append(components, component)
	}

	return components, nil
}

func (m *Mesh) SplitConnectedComponents() ([]*Mesh, error) {
	components, err := m.ConnectedComponents()
	if err != nil {
		return nil, err
	}

	out := make([]*Mesh, len(components))
	for i := range components {
		out[i], err = m.cloneWithFaces(components[i].Faces)
		if err != nil {
			return nil, err
		}
	}

	return out, nil
}

func (m *Mesh) NonManifoldEdges() ([][2]uint32, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}

	position := m.namedAttribute(AttributePosition)
	if position == nil {
		return nil, fmt.Errorf("%w: mesh position attribute missing", ErrInvalidGeometry)
	}

	_, edgeFaces, _ := meshAdjacency(m.faces, position)
	out := make([][2]uint32, 0)
	for edge, faces := range edgeFaces {
		if len(faces) > 2 {
			out = append(out, [2]uint32{edge.a, edge.b})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i][0] != out[j][0] {
			return out[i][0] < out[j][0]
		}

		return out[i][1] < out[j][1]
	})
	return out, nil
}

func (m *Mesh) IsManifold() (bool, error) {
	edges, err := m.NonManifoldEdges()
	if err != nil {
		return false, err
	}

	return len(edges) == 0, nil
}

func (m *Mesh) cloneWithFaces(faceIDs []int) (*Mesh, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}

	out := newMesh(m.PointCount())
	out.setMetadata(m.metadataRef().Clone())
	out.Name = m.Name
	out.materials = m.materials.Clone()
	out.nonMaterialTextures = m.nonMaterialTextures.Clone()
	out.meshFeatures = cloneMeshFeaturesSlice(m.meshFeatures)
	out.meshFeaturesMaterialMask = cloneIntMatrix(m.meshFeaturesMaterialMask)
	out.structuralMetadata = m.structuralMetadata.Clone()
	out.propertyAttributeIndices = append([]int(nil), m.propertyAttributeIndices...)
	out.propertyAttributeMaterialMask = cloneIntMatrix(m.propertyAttributeMaterialMask)
	for _, attr := range m.attributes {
		if _, err := out.AddAttribute(attr.Clone()); err != nil {
			return nil, err
		}
	}

	for _, faceID := range faceIDs {
		if faceID < 0 || faceID >= len(m.faces) {
			return nil, fmt.Errorf("%w: face %d out of range", ErrInvalidGeometry, faceID)
		}

		if err := out.AddFace(m.faces[faceID]); err != nil {
			return nil, err
		}
	}

	cleanup := MeshCleanupOptions{
		RemoveUnusedAttributes: true,
	}
	if err := out.Cleanup(cleanup); err != nil {
		return nil, err
	}

	return out, nil
}

func meshAdjacency(faces []Face, position *Attribute) ([][]int, map[meshEdgeKey][]int, [][]meshEdgeKey) {
	adjacency := make([][]int, len(faces))
	edgeFaces := make(map[meshEdgeKey][]int, len(faces)*3)
	faceEdges := make([][]meshEdgeKey, len(faces))
	for faceID, face := range faces {
		if IsDegenerateFace(face) {
			continue
		}

		edges := make([]meshEdgeKey, 3)
		for local := range face {
			a, b := facePositionEdgeVertices(position, face, local)
			key := makeMeshEdgeKey(a, b)
			edges[local] = key
			edgeFaces[key] = append(edgeFaces[key], faceID)
		}

		faceEdges[faceID] = edges
	}

	for _, incidentFaces := range edgeFaces {
		for i := 0; i < len(incidentFaces); i++ {
			for j := i + 1; j < len(incidentFaces); j++ {
				a := incidentFaces[i]
				b := incidentFaces[j]
				adjacency[a] = append(adjacency[a], b)
				adjacency[b] = append(adjacency[b], a)
			}
		}
	}

	for i := range adjacency {
		sort.Ints(adjacency[i])
		adjacency[i] = compactSortedInts(adjacency[i])
	}

	return adjacency, edgeFaces, faceEdges
}

func makeMeshEdgeKey(a, b uint32) meshEdgeKey {
	if a > b {
		a, b = b, a
	}

	return meshEdgeKey{a: a, b: b}
}

func faceEdgeVertices(face Face, local int) (uint32, uint32) {
	return face[(local+1)%3], face[(local+2)%3]
}

func facePositionEdgeVertices(position *Attribute, face Face, local int) (uint32, uint32) {
	a, b := faceEdgeVertices(face, local)
	return position.mappedIndex(int(a)), position.mappedIndex(int(b))
}

func compactSortedInts(values []int) []int {
	if len(values) < 2 {
		return values
	}

	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}

	return out
}
