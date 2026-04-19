package draco

import (
	"testing"

	md "github.com/gophics/go-draco/metadata"
	"github.com/stretchr/testify/require"
)

func TestPointCloudEquivalentIgnoresPointOrder(t *testing.T) {
	left := mustNewPointCloud(2)
	right := mustNewPointCloud(2)

	leftPos := mustNewFloat32Attribute(AttributePosition, 3, 2)
	setFloat32Value(t, leftPos, 0, 1, 2, 3)
	setFloat32Value(t, leftPos, 1, 4, 5, 6)

	rightPos := mustNewFloat32Attribute(AttributePosition, 3, 2)
	setFloat32Value(t, rightPos, 0, 4, 5, 6)
	setFloat32Value(t, rightPos, 1, 1, 2, 3)

	addPointCloudAttribute(t, left, leftPos)
	addPointCloudAttribute(t, right, rightPos)
	requirePointCloudEquivalent(t, left, right)
}

func TestMeshEquivalentIgnoresFaceAndPointOrder(t *testing.T) {
	left := mustNewMesh(4)
	right := mustNewMesh(4)

	leftPos := mustNewFloat32Attribute(AttributePosition, 3, 4)
	rightPos := mustNewFloat32Attribute(AttributePosition, 3, 4)
	for i, xyz := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {0, 0, 1}} {
		setFloat32Value(t, leftPos, i, xyz[:]...)
	}

	for i, xyz := range [][3]float32{{0, 0, 1}, {0, 1, 0}, {0, 0, 0}, {1, 0, 0}} {
		setFloat32Value(t, rightPos, i, xyz[:]...)
	}

	addMeshAttribute(t, left, leftPos)
	addMeshAttribute(t, right, rightPos)
	for _, face := range []Face{{0, 1, 2}, {0, 2, 3}} {
		addFace(t, left, face)
	}

	for _, face := range []Face{{1, 2, 3}, {2, 1, 0}} {
		addFace(t, right, face)
	}

	requireMeshEquivalent(t, left, right)
}

func TestMeshConnectedComponentsAndSplit(t *testing.T) {
	mesh := newDisconnectedComponentMesh(t)

	components, err := mesh.ConnectedComponents()
	require.NoError(t, err)
	require.Len(t, components, 2)
	require.Len(t, components[0].Faces, 1)
	require.Len(t, components[0].Vertices, 3)
	require.Len(t, components[0].BoundaryEdges, 3)

	split, err := mesh.SplitConnectedComponents()
	require.NoError(t, err)
	require.Len(t, split, 2)
	for _, submesh := range split {
		requireMeshCounts(t, submesh, 1, 3, 1)
	}
}

func TestMeshConnectedComponentsUsePositionConnectivity(t *testing.T) {
	mesh := mustNewMesh(6)
	position, err := NewAttribute(AttributePosition, DataTypeFloat32, 3, 3)
	require.NoError(t, err)
	for i, xyz := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}} {
		setFloat32Value(t, position, i, xyz[:]...)
	}

	require.NoError(t, position.SetExplicitMapping(6))
	for pointID, entryID := range []uint32{0, 1, 2, 0, 1, 2} {
		require.NoError(t, position.SetPointMapEntry(pointID, entryID))
	}

	addMeshAttribute(t, mesh, position)
	for _, face := range []Face{{0, 1, 2}, {3, 4, 5}} {
		addFace(t, mesh, face)
	}

	components, err := mesh.ConnectedComponents()
	require.NoError(t, err)
	require.Len(t, components, 1)
	require.Len(t, components[0].Faces, 2)
}

func TestMeshNonManifoldEdges(t *testing.T) {
	mesh := mustNewMesh(5)
	position := mustNewFloat32Attribute(AttributePosition, 3, 5)
	for i, xyz := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}} {
		setFloat32Value(t, position, i, xyz[:]...)
	}

	addMeshAttribute(t, mesh, position)
	for _, face := range []Face{{0, 1, 2}, {1, 0, 3}, {0, 1, 4}} {
		addFace(t, mesh, face)
	}

	manifold, err := mesh.IsManifold()
	require.NoError(t, err)
	require.False(t, manifold)

	edges, err := mesh.NonManifoldEdges()
	require.NoError(t, err)
	require.Equal(t, [][2]uint32{{0, 1}}, edges)
}

func TestSplitConnectedComponentsPreservesStructuralMetadata(t *testing.T) {
	mesh := newDisconnectedComponentMesh(t)
	structuralMetadata := &md.StructuralMetadata{}
	_, err := structuralMetadata.AddPropertyAttribute(&md.PropertyAttribute{Name: "set"})
	require.NoError(t, err)
	mesh.setStructuralMetadata(structuralMetadata)
	materials := MaterialLibrary{}
	for i := 0; i <= 2; i++ {
		_, err = materials.AddMaterial(NewMaterial())
		require.NoError(t, err)
	}

	require.NoError(t, mesh.SetMaterials(materials))
	_, err = mesh.AddPropertyAttributesIndex(0)
	require.NoError(t, err)
	require.NoError(t, mesh.AddPropertyAttributesIndexMaterialMask(0, 2))

	split, err := mesh.SplitConnectedComponents()
	require.NoError(t, err)
	require.Len(t, split, 2)

	for _, submesh := range split {
		subStructuralMetadata := submesh.StructuralMetadataClone()
		require.NotNil(t, subStructuralMetadata)
		require.Equal(t, 1, subStructuralMetadata.PropertyAttributeCount())
		require.Equal(t, 1, submesh.PropertyAttributeIndexCount())
		index, err := submesh.PropertyAttributeIndex(0)
		require.NoError(t, err)
		require.Equal(t, 0, index)
		count, err := submesh.PropertyAttributeIndexMaterialMaskCount(0)
		require.NoError(t, err)
		require.Equal(t, 1, count)
		maskValue, err := submesh.PropertyAttributeIndexMaterialMask(0, 0)
		require.NoError(t, err)
		require.Equal(t, 2, maskValue)
	}
}

func newDisconnectedComponentMesh(t *testing.T) *Mesh {
	t.Helper()

	mesh := mustNewMesh(6)
	position := mustNewFloat32Attribute(AttributePosition, 3, 6)
	for i, xyz := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {10, 0, 0}, {11, 0, 0}, {10, 1, 0}} {
		setFloat32Value(t, position, i, xyz[:]...)
	}

	addMeshAttribute(t, mesh, position)
	for _, face := range []Face{{0, 1, 2}, {3, 4, 5}} {
		addFace(t, mesh, face)
	}

	return mesh
}
