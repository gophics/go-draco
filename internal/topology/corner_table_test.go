package topology

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildCornerTableQuad(t *testing.T) {
	table, err := BuildCornerTable(4, []Face{
		{0, 1, 2},
		{0, 2, 3},
	})
	require.NoError(t, err)
	require.Equal(t, 2, table.FaceCount())
	require.Equal(t, 6, table.CornerCount())
	require.Equal(t, 4, table.VertexCount())
	require.Equal(t, 5, table.Opposite(1))
	require.Equal(t, 1, table.Opposite(5))
	require.Equal(t, InvalidCorner, table.Opposite(0))
	require.Equal(t, InvalidCorner, table.Opposite(2))
	require.Equal(t, 0, table.Next(2))
	require.Equal(t, 2, table.Previous(0))
	require.Equal(t, 3, table.LeftMostCorner(0))
	require.Equal(t, 3, table.SwingLeft(0))
	require.Equal(t, InvalidCorner, table.SwingLeft(3))
	require.Equal(t, 3, table.Valence(0))
	require.Equal(t, 3, table.Valence(2))
	require.True(t, table.IsBoundaryVertex(0))
	require.True(t, table.IsBoundaryVertex(2))
	require.Equal(t, [3]int{InvalidFace, 1, InvalidFace}, table.FaceNeighbors(0))
}

func TestBuildCornerTableClosedTetrahedron(t *testing.T) {
	table, err := BuildCornerTable(4, []Face{
		{0, 2, 1},
		{0, 1, 3},
		{1, 2, 3},
		{0, 3, 2},
	})
	require.NoError(t, err)
	for vertex := 0; vertex < 4; vertex++ {
		require.Equal(t, 3, table.Valence(vertex))
		require.False(t, table.IsBoundaryVertex(vertex))
	}
}

func TestBuildCornerTableRejectsNonManifoldEdge(t *testing.T) {
	_, err := BuildCornerTable(5, []Face{
		{0, 1, 2},
		{1, 0, 3},
		{0, 1, 4},
	})
	require.Error(t, err)
}

func TestCornerTableAddFaceAndVertex(t *testing.T) {
	table, err := BuildCornerTable(4, []Face{
		{0, 1, 2},
		{0, 2, 3},
	})
	require.NoError(t, err)

	require.Equal(t, 4, table.AddVertex())
	newFace, err := table.AddFace(Face{2, 3, 4})
	require.NoError(t, err)
	require.Equal(t, 2, newFace)
	require.Equal(t, 3, table.FaceCount())
	require.Equal(t, 9, table.CornerCount())
	require.Equal(t, 2, table.Vertex(6))
	require.Equal(t, 3, table.Vertex(7))
	require.Equal(t, 4, table.Vertex(8))
}
