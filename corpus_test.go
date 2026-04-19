package draco

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type corpusFixture struct {
	Name                 string
	Path                 string
	Group                string
	RequiredCapabilities []string
}

type corpusAttributeExpectation struct {
	attrType      AttributeType
	dataType      DataType
	numComponents int
}

type corpusValidator func(*testing.T, Geometry)

func TestFixtureManifest(t *testing.T) {
	validators := corpusValidators()

	for _, fixture := range allCorpusFixtures() {
		validator, ok := validators[fixture.Name]
		require.True(t, ok, "missing corpus validator for %s", fixture.Name)

		t.Run(fixture.Name, func(t *testing.T) {
			require.NotEmpty(t, fixture.Group)
			require.True(t, hasCapabilities(fixture.RequiredCapabilities), "fixture manifest drift: missing declared capabilities %v", fixture.RequiredCapabilities)
			geom, err := Decode(testContext(t), readFixture(t, fixture.Path))
			require.NoError(t, err)
			validator(t, geom)
		})
	}
}

func allCorpusFixtures() []corpusFixture {
	return []corpusFixture{
		{
			Name:                 "point_cloud_sequential",
			Path:                 "testdata/point_cloud_sequential.drc",
			Group:                "owned-sequential-point-cloud",
			RequiredCapabilities: []string{"decode:point-cloud:sequential:generic", "decode:attribute:integer", "entropy:tagged-rans", "prediction:delta-wrap"},
		},
		{
			Name:                 "point_cloud_quantized",
			Path:                 "testdata/point_cloud_quantized.drc",
			Group:                "owned-quantized-point-cloud",
			RequiredCapabilities: []string{"decode:point-cloud:sequential:generic", "decode:transform:quantization"},
		},
		{
			Name:                 "point_cloud_kd_tree",
			Path:                 "testdata/point_cloud_kd_tree.drc",
			Group:                "owned-kd-tree-point-cloud",
			RequiredCapabilities: []string{"decode:point-cloud:kd-tree", "decode:transform:quantization"},
		},
		{
			Name:                 "mesh_sequential",
			Path:                 "testdata/mesh_sequential.drc",
			Group:                "owned-sequential-mesh",
			RequiredCapabilities: []string{"decode:mesh:sequential:generic", "decode:attribute:integer", "entropy:tagged-rans", "prediction:delta-wrap"},
		},
		{
			Name:                 "mesh_edgebreaker",
			Path:                 "testdata/mesh_edgebreaker.drc",
			Group:                "owned-edgebreaker-mesh",
			RequiredCapabilities: []string{"decode:mesh:edgebreaker", "entropy:tagged-rans"},
		},
	}
}

func corpusValidators() map[string]corpusValidator {
	return map[string]corpusValidator{
		"point_cloud_sequential": validatePointCloudSequentialFixture,
		"point_cloud_quantized":  validatePointCloudQuantizedFixture,
		"point_cloud_kd_tree":    validatePointCloudKDTreeFixture,
		"mesh_sequential":        validateSequentialMeshFixture,
		"mesh_edgebreaker":       validateEdgebreakerMeshFixture,
	}
}

func validatePointCloudSequentialFixture(t *testing.T, geom Geometry) {
	t.Helper()

	pc := requirePointCloudFixture(t, geom, 6, 2,
		corpusAttributeExpectation{AttributePosition, DataTypeFloat32, 3},
		corpusAttributeExpectation{AttributeColor, DataTypeUint8, 3},
	)

	wantPositions := [][3]float32{
		{0, -0.4, 0},
		{0.35, 0.2, 0.15},
		{0.7, -0.2, 0.3},
	}
	wantColors := [][]int32{
		{40, 80, 120},
		{51, 87, 125},
		{62, 94, 130},
	}

	position := pc.NamedAttribute(AttributePosition)
	color := pc.NamedAttribute(AttributeColor)
	for i := range wantPositions {
		requireFloat32Entry(t, position, i, wantPositions[i][:], 1e-5)
		requireInt32Entry(t, color, i, wantColors[i])
	}
}

func validatePointCloudQuantizedFixture(t *testing.T, geom Geometry) {
	t.Helper()

	requirePointCloudFixture(t, geom, 6, 2,
		corpusAttributeExpectation{AttributePosition, DataTypeFloat32, 3},
		corpusAttributeExpectation{AttributeColor, DataTypeUint8, 3},
	)
}

func validatePointCloudKDTreeFixture(t *testing.T, geom Geometry) {
	t.Helper()

	requirePointCloudFixture(t, geom, 16, 2,
		corpusAttributeExpectation{AttributePosition, DataTypeFloat32, 3},
		corpusAttributeExpectation{AttributeColor, DataTypeUint8, 3},
	)
}

func validateSequentialMeshFixture(t *testing.T, geom Geometry) {
	t.Helper()

	requireMeshFixture(t, geom, 2, 6, 4,
		corpusAttributeExpectation{AttributePosition, DataTypeFloat32, 3},
		corpusAttributeExpectation{AttributeTexCoord, DataTypeFloat32, 2},
		corpusAttributeExpectation{AttributeNormal, DataTypeFloat32, 3},
		corpusAttributeExpectation{AttributeGeneric, DataTypeUint8, 1},
	)
}

func validateEdgebreakerMeshFixture(t *testing.T, geom Geometry) {
	t.Helper()

	requireMeshFixture(t, geom, 2, 6, 4,
		corpusAttributeExpectation{AttributePosition, DataTypeFloat32, 3},
		corpusAttributeExpectation{AttributeTexCoord, DataTypeFloat32, 2},
		corpusAttributeExpectation{AttributeNormal, DataTypeFloat32, 3},
		corpusAttributeExpectation{AttributeGeneric, DataTypeUint8, 1},
	)
}

func requirePointCloudFixture(t *testing.T, geom Geometry, wantPoints, wantAttrs int, attrs ...corpusAttributeExpectation) *PointCloud {
	t.Helper()
	pc, ok := geom.(*PointCloud)
	require.True(t, ok, "fixture decoded to %T, want *PointCloud", geom)
	if wantPoints >= 0 {
		require.Equal(t, wantPoints, pc.PointCount())
	}

	if wantAttrs >= 0 {
		require.Equal(t, wantAttrs, pc.AttributeCount())
	}

	for _, attr := range attrs {
		requirePointCloudAttribute(t, pc, attr.attrType, attr.dataType, attr.numComponents)
	}

	return pc
}

func requireMeshFixture(t *testing.T, geom Geometry, wantFaces, wantPoints, wantAttrs int, attrs ...corpusAttributeExpectation) *Mesh {
	t.Helper()
	mesh, ok := geom.(*Mesh)
	require.True(t, ok, "fixture decoded to %T, want *Mesh", geom)
	if wantFaces >= 0 {
		require.Equal(t, wantFaces, mesh.FaceCount())
	}

	if wantPoints >= 0 {
		require.Equal(t, wantPoints, mesh.PointCount())
	}

	if wantAttrs >= 0 {
		require.Equal(t, wantAttrs, mesh.AttributeCount())
	}

	for _, attr := range attrs {
		requireMeshAttribute(t, mesh, attr.attrType, attr.dataType, attr.numComponents)
	}

	return mesh
}

func hasCapabilities(required []string) bool {
	supported := SupportedCapabilities()
	for _, capability := range required {
		if !supported[capability] {
			return false
		}
	}

	return true
}
