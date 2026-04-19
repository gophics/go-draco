package draco

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMeshCleanupRemovesDegenerateFacesByPosition(t *testing.T) {
	mesh := mustNewMesh(4)

	position, err := NewAttribute(AttributePosition, DataTypeFloat32, 3, 3)
	require.NoError(t, err)
	for i, xyz := range [][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}} {
		setFloat32Value(t, position, i, xyz...)
	}

	require.NoError(t, position.SetExplicitMapping(4))
	for pointID, entryID := range []uint32{0, 1, 2, 1} {
		require.NoError(t, position.SetPointMapEntry(pointID, entryID))
	}

	addMeshAttribute(t, mesh, position)
	addFace(t, mesh, Face{0, 1, 2})
	addFace(t, mesh, Face{3, 1, 2})

	options := DefaultMeshCleanupOptions()
	options.RemoveDuplicateFaces = false
	options.RemoveUnusedAttributes = false
	require.NoError(t, mesh.Cleanup(options))
	requireMeshCounts(t, mesh, 1, 4, 1)
	require.Equal(t, Face{0, 1, 2}, mesh.face(0))
}

func TestMeshCleanupRemovesUnusedPointsAndCompactsAttributes(t *testing.T) {
	mesh := mustNewMesh(5)

	position, err := NewAttribute(AttributePosition, DataTypeFloat32, 3, 4)
	require.NoError(t, err)
	for i, xyz := range [][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {10, 1, 0}} {
		setFloat32Value(t, position, i, xyz...)
	}

	require.NoError(t, position.SetExplicitMapping(5))
	for pointID, entryID := range []uint32{0, 1, 2, 3, 3} {
		require.NoError(t, position.SetPointMapEntry(pointID, entryID))
	}

	addMeshAttribute(t, mesh, position)

	genericAttr, err := NewAttribute(AttributeGeneric, DataTypeInt32, 2, 3)
	require.NoError(t, err)
	for i, values := range [][]int32{{0, 0}, {0, 1}, {0, 2}} {
		setInt32Value(t, genericAttr, i, values...)
	}

	require.NoError(t, genericAttr.SetExplicitMapping(5))
	for pointID, entryID := range []uint32{0, 1, 2, 0, 1} {
		require.NoError(t, genericAttr.SetPointMapEntry(pointID, entryID))
	}

	addMeshAttribute(t, mesh, genericAttr)

	identityAttr, err := NewAttribute(AttributeColor, DataTypeUint8, 1, 5)
	require.NoError(t, err)
	for i, value := range []int32{10, 20, 30, 40, 50} {
		setInt32Value(t, identityAttr, i, value)
	}

	addMeshAttribute(t, mesh, identityAttr)

	addFace(t, mesh, Face{0, 1, 2})
	addFace(t, mesh, Face{3, 4, 4})

	require.NoError(t, mesh.Cleanup(DefaultMeshCleanupOptions()))
	requireMeshCounts(t, mesh, 1, 3, 3)

	cleanedGeneric := requireMeshAttribute(t, mesh, AttributeGeneric, DataTypeInt32, 2)
	require.Equal(t, 3, cleanedGeneric.EntryCount())
	require.Equal(t, 3, cleanedGeneric.MappingSize())

	cleanedIdentity := requireMeshAttribute(t, mesh, AttributeColor, DataTypeUint8, 1)
	require.Equal(t, 3, cleanedIdentity.EntryCount())
	require.True(t, cleanedIdentity.IsIdentityMapping())
	requireInt32Entry(t, cleanedIdentity, 2, []int32{30})
}

func TestMeshCleanupRemovesDuplicateFaces(t *testing.T) {
	mesh := newMeshFromData(t,
		[][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {0, 0, 1}},
		[]Face{{0, 1, 2}, {1, 2, 0}, {0, 1, 3}},
	)

	options := DefaultMeshCleanupOptions()
	options.RemoveDegenerateFaces = false
	options.RemoveUnusedAttributes = false
	require.NoError(t, mesh.Cleanup(options))
	requireMeshCounts(t, mesh, 2, 4, 1)
	require.Equal(t, Face{0, 1, 2}, mesh.face(0))
	require.Equal(t, Face{0, 1, 3}, mesh.face(1))
}

func TestMeshCleanupRemovesFacesThatOnlyDifferInNonPositionAttributes(t *testing.T) {
	mesh := mustNewMesh(6)
	position := mustNewFloat32Attribute(AttributePosition, 3, 3)
	for i, xyz := range [][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}} {
		setFloat32Value(t, position, i, xyz...)
	}

	require.NoError(t, position.SetExplicitMapping(6))
	for pointID, entryID := range []uint32{0, 1, 2, 0, 1, 2} {
		require.NoError(t, position.SetPointMapEntry(pointID, entryID))
	}

	addMeshAttribute(t, mesh, position)

	normal := mustNewFloat32Attribute(AttributeNormal, 3, 6)
	for i, xyz := range [][]float32{
		{0, 0, 1}, {0, 0, 1}, {0, 0, 1},
		{0, 1, 0}, {0, 1, 0}, {0, 1, 0},
	} {
		setFloat32Value(t, normal, i, xyz...)
	}

	addMeshAttribute(t, mesh, normal)

	addFace(t, mesh, Face{0, 1, 2})
	addFace(t, mesh, Face{3, 4, 5})

	options := DefaultMeshCleanupOptions()
	options.RemoveDegenerateFaces = false
	options.RemoveUnusedAttributes = false
	require.NoError(t, mesh.Cleanup(options))
	requireMeshCounts(t, mesh, 1, 6, 2)
}

func TestMeshCleanupRejectsUnsupportedManifoldOption(t *testing.T) {
	mesh := newMeshFromData(t, [][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}, []Face{{0, 1, 2}})
	options := DefaultMeshCleanupOptions()
	options.MakeGeometryManifold = true
	before := mesh.Clone()
	require.Error(t, mesh.Cleanup(options))
	requireMeshEquivalent(t, before, mesh)
}
