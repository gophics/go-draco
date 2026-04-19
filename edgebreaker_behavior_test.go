package draco

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMeshEdgebreakerEncoderReuse(t *testing.T) {
	mesh := newEdgebreakerColorQuadMesh(t)

	opts := []EncodeOption{WithMeshMethod(MeshEdgebreakerEncoding)}
	data0 := encodeMesh(t, mesh, opts...)
	data1 := encodeMesh(t, mesh, opts...)
	requireDeterministicEncode(t, data0, data1)

	decoded := decodeMesh(t, data0)
	requireMeshEquivalent(t, mesh, decoded)
}

func TestMeshEdgebreakerDecoderReuse(t *testing.T) {
	mesh := newEdgebreakerColorQuadMesh(t)
	data := encodeMesh(t, mesh, WithMeshMethod(MeshEdgebreakerEncoding))

	decoded0 := decodeMesh(t, data)
	decoded1 := decodeMesh(t, data)
	requireMeshEquivalent(t, decoded0, decoded1)
	requireMeshEquivalent(t, mesh, decoded0)
}

func TestMeshEdgebreakerNativeTraversalSpeedRoundTrip(t *testing.T) {
	source := decodeMesh(t, readFixture(t, "testdata/mesh_edgebreaker.drc"))
	opts := []EncodeOption{
		WithMeshMethod(MeshEdgebreakerEncoding),
		WithSpeed(10, 10),
	}

	data0 := encodeMesh(t, source, opts...)
	data1 := encodeMesh(t, source, opts...)
	requireDeterministicEncode(t, data0, data1)
	requireMeshEquivalent(t, source, decodeMesh(t, data0))
}

func TestMeshEdgebreakerDefaultCanonicalGoldenBytes(t *testing.T) {
	testCases := []struct {
		name       string
		fixture    string
		wantSHA256 string
		wantLen    int
	}{
		{
			name:       "owned-seam-mesh",
			fixture:    "testdata/mesh_edgebreaker.drc",
			wantSHA256: "370880f264161da9a39d8ceb36e8c684833173951b2104c5c1f062bf15b87801",
			wantLen:    317,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			source := decodeMesh(t, readFixture(t, tc.fixture))
			encoded := encodeMesh(t, source, WithMeshMethod(MeshEdgebreakerEncoding))
			sum := fmt.Sprintf("%x", sha256.Sum256(encoded))
			require.Len(t, encoded, tc.wantLen)
			require.Equal(t, tc.wantSHA256, sum)
			requireDeterministicEncode(t, encoded, encodeMesh(t, decodeMesh(t, encoded), WithMeshMethod(MeshEdgebreakerEncoding)))
		})
	}
}

func TestMeshEdgebreakerSplitMeshOnSeamsModes(t *testing.T) {
	fixture := newSharedPositionFixture(t)

	testCases := []struct {
		name     string
		split    bool
		validate func(*testing.T, sharedPositionFixture, *Mesh)
	}{
		{
			name:  "split-on-seams",
			split: true,
			validate: func(t *testing.T, fixture sharedPositionFixture, decoded *Mesh) {
				t.Helper()

				requireMeshCounts(t, decoded, fixture.mesh.FaceCount(), fixture.sourcePoints, fixture.mesh.AttributeCount())

				splitPos := requireMeshAttribute(t, decoded, AttributePosition, fixture.position.DataType, fixture.position.NumComponents)
				splitTex := requireMeshAttribute(t, decoded, AttributeTexCoord, DataTypeFloat32, 2)
				splitNorm := requireMeshAttribute(t, decoded, AttributeNormal, DataTypeFloat32, 3)

				require.Equal(t, decoded.PointCount(), splitPos.EntryCount())
				require.Greater(t, splitPos.EntryCount(), fixture.sourcePosSize)
				require.Equal(t, decoded.PointCount(), splitTex.EntryCount())
				require.Equal(t, decoded.PointCount(), splitNorm.EntryCount())
			},
		},
		{
			name:  "preserve-shared-positions",
			split: false,
			validate: func(t *testing.T, fixture sharedPositionFixture, decoded *Mesh) {
				t.Helper()

				requireMeshCounts(t, decoded, fixture.mesh.FaceCount(), fixture.mesh.PointCount(), fixture.mesh.AttributeCount())

				position := requireMeshAttribute(t, decoded, AttributePosition, fixture.position.DataType, fixture.position.NumComponents)
				require.Equal(t, fixture.sourcePosSize, position.EntryCount())
				for attrID := 0; attrID < decoded.AttributeCount(); attrID++ {
					attr := decoded.attribute(attrID)
					require.NotNil(t, attr)
					if attr.Type == AttributePosition {
						continue
					}

					require.Equal(t, decoded.PointCount(), attr.EntryCount())
				}

				requireMeshEquivalent(t, fixture.mesh, decoded)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := encodeMesh(t, fixture.mesh,
				WithMeshMethod(MeshEdgebreakerEncoding),
				WithSplitMeshOnSeams(tc.split),
			)
			decoded := decodeMesh(t, data)
			tc.validate(t, fixture, decoded)
		})
	}
}

func TestMeshEdgebreakerWrongAttributeOrder(t *testing.T) {
	var builder triangleSoupMeshBuilder
	builder.Start(1)

	normAttID, err := builder.AddAttribute(AttributeNormal, 3, DataTypeFloat32)
	require.NoError(t, err)
	posAttID, err := builder.AddAttribute(AttributePosition, 3, DataTypeFloat32)
	require.NoError(t, err)

	require.NoError(t, builder.SetAttributeValuesForFace(
		posAttID,
		0,
		[3]float32{0, 0, 0},
		[3]float32{1, 0, 0},
		[3]float32{0, 1, 0},
	))
	require.NoError(t, builder.SetAttributeValuesForFace(
		normAttID,
		0,
		[3]float32{0, 0, 1},
		[3]float32{0, 0, 1},
		[3]float32{0, 0, 1},
	))

	mesh, err := builder.Finalize()
	require.NoError(t, err)

	data := encodeMesh(t, mesh,
		WithMeshMethod(MeshEdgebreakerEncoding),
		WithAttributeQuantization(AttributePosition, 8),
		WithAttributeQuantization(AttributeNormal, 8),
	)
	decoded := decodeMesh(t, data)

	require.Equal(t, 2, decoded.AttributeCount())
	requireAttributeSchema(t, decoded.attribute(0), AttributePosition, decoded.attribute(0).DataType, 3)
	requireAttributeSchema(t, decoded.attribute(1), AttributeNormal, decoded.attribute(1).DataType, 3)
}

func TestMeshEdgebreakerIsolatedPointCleanup(t *testing.T) {
	mesh := newEdgebreakerIsolatedPointMesh(t)
	data := encodeMesh(t, mesh, WithMeshMethod(MeshEdgebreakerEncoding))
	decoded := decodeMesh(t, data)

	cleaned := mesh.Clone()
	require.NoError(t, cleaned.Cleanup(DefaultMeshCleanupOptions()))
	requireMeshEquivalent(t, cleaned, decoded)
}

func TestMeshEdgebreakerDegenerateMeshFails(t *testing.T) {
	mesh := mustNewMesh(3)
	position := mustNewFloat32Attribute(AttributePosition, 3, 3)
	for i, xyz := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}} {
		setFloat32Value(t, position, i, xyz[:]...)
	}

	addMeshAttribute(t, mesh, position)
	addFace(t, mesh, Face{0, 0, 1})

	_, err := Encode(testContext(t), mesh, WithMeshMethod(MeshEdgebreakerEncoding))
	require.Error(t, err)
}

type sharedPositionFixture struct {
	mesh          *Mesh
	position      *Attribute
	sourcePosSize int
	sourcePoints  int
}

func newSharedPositionFixture(t *testing.T) sharedPositionFixture {
	t.Helper()

	position := mustNewFloat32Attribute(AttributePosition, 3, 4)
	for i, xyz := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}} {
		setFloat32Value(t, position, i, xyz[:]...)
	}

	require.NoError(t, position.SetExplicitMapping(6))
	for pointID, entryID := range []uint32{0, 1, 2, 0, 2, 3} {
		require.NoError(t, position.SetPointMapEntry(pointID, entryID))
	}

	texCoord, err := NewFloat32Attribute(AttributeTexCoord, 2, []float32{
		0, 0,
		1, 0,
		1, 1,
		0, 0,
		0.25, 1,
		0, 1,
	})
	require.NoError(t, err)

	normal := mustNewFloat32Attribute(AttributeNormal, 3, 6)
	for i := 0; i < 6; i++ {
		setFloat32Value(t, normal, i, 0, 0, 1)
	}

	mesh, err := NewMesh(6, []Face{{0, 1, 2}, {3, 4, 5}}, position, texCoord, normal)
	require.NoError(t, err)

	return sharedPositionFixture{
		mesh:          mesh,
		position:      position,
		sourcePosSize: position.EntryCount(),
		sourcePoints:  mesh.PointCount(),
	}
}

func newEdgebreakerColorQuadMesh(t *testing.T) *Mesh {
	t.Helper()

	mesh := mustNewMesh(4)
	position := mustNewFloat32Attribute(AttributePosition, 3, 4)
	for i, xyz := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}} {
		setFloat32Value(t, position, i, xyz[:]...)
	}

	addMeshAttribute(t, mesh, position)

	color, err := NewAttribute(AttributeColor, DataTypeUint8, 4, 4)
	require.NoError(t, err)
	for i, rgba := range [][]byte{
		{255, 0, 0, 255},
		{0, 255, 0, 255},
		{0, 0, 255, 255},
		{255, 255, 0, 255},
	} {
		setRawValue(t, color, i, rgba)
	}

	addMeshAttribute(t, mesh, color)
	addFace(t, mesh, Face{0, 1, 2})
	addFace(t, mesh, Face{0, 2, 3})
	return mesh
}

func newEdgebreakerIsolatedPointMesh(t *testing.T) *Mesh {
	t.Helper()

	mesh := mustNewMesh(5)
	position := mustNewFloat32Attribute(AttributePosition, 3, 5)
	for i, xyz := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}, {3, 3, 3}} {
		setFloat32Value(t, position, i, xyz[:]...)
	}

	addMeshAttribute(t, mesh, position)

	color, err := NewAttribute(AttributeColor, DataTypeUint8, 4, 5)
	require.NoError(t, err)
	for i, rgba := range [][]byte{
		{255, 0, 0, 255},
		{0, 255, 0, 255},
		{0, 0, 255, 255},
		{255, 255, 0, 255},
		{64, 64, 64, 255},
	} {
		setRawValue(t, color, i, rgba)
	}

	addMeshAttribute(t, mesh, color)
	addFace(t, mesh, Face{0, 1, 2})
	addFace(t, mesh, Face{0, 2, 3})
	return mesh
}
