package draco

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMeshPredictionRoundTrips(t *testing.T) {
	testCases := []struct {
		name       string
		build      func(*testing.T) *Mesh
		prediction PredictionMethod
	}{
		{name: "parallelogram", build: newPredictionQuadMesh, prediction: PredictionMethodParallelogram},
		{name: "multi-parallelogram", build: newPredictionTetrahedronMesh, prediction: PredictionMethodMultiParallelogram},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mesh := tc.build(t)
			defaultData := encodeMesh(t, mesh)

			cfg := encodeConfig{}
			cfg.SetAttributePrediction(AttributeGeneric, tc.prediction)
			data := encodeMesh(t, mesh, withEncodeConfig(cfg))

			require.False(t, bytes.Equal(data, defaultData), "%s encoding unexpectedly matched default encoding", tc.name)
			decoded := decodeMesh(t, data)
			requireMeshEquivalent(t, mesh, decoded)
			requireDeterministicEncode(t, data, encodeMesh(t, decoded, withEncodeConfig(cfg)))
		})
	}
}

func TestPointCloudRejectsMeshPredictionScheme(t *testing.T) {
	pc := mustNewPointCloud(2)
	attr, err := NewAttribute(AttributeGeneric, DataTypeInt32, 1, 2)
	require.NoError(t, err)
	setInt32Value(t, attr, 0, 10)
	setInt32Value(t, attr, 1, 20)
	addPointCloudAttribute(t, pc, attr)

	cfg := encodeConfig{}
	cfg.SetAttributePrediction(AttributeGeneric, PredictionMethodParallelogram)
	_, err = Encode(testContext(t), pc, withEncodeConfig(cfg))
	require.ErrorIs(t, err, ErrUnsupportedFeature)
}

func newPredictionQuadMesh(t *testing.T) *Mesh {
	t.Helper()
	mesh := newMeshFromData(t,
		[][]float32{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}},
		[]Face{{0, 1, 2}, {0, 2, 3}},
	)
	attr, err := NewAttribute(AttributeGeneric, DataTypeInt32, 2, 4)
	require.NoError(t, err)
	for i, value := range [][]int32{{0, 0}, {5, 7}, {11, 13}, {17, 19}} {
		setInt32Value(t, attr, i, value...)
	}

	addMeshAttribute(t, mesh, attr)
	return mesh
}

func newPredictionTetrahedronMesh(t *testing.T) *Mesh {
	t.Helper()
	mesh := newMeshFromData(t,
		[][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {0, 0, 1}},
		[]Face{{0, 1, 2}, {0, 3, 1}, {1, 3, 2}, {0, 2, 3}},
	)
	attr, err := NewAttribute(AttributeGeneric, DataTypeInt32, 1, 4)
	require.NoError(t, err)
	for i, value := range []int32{2, 5, 11, 19} {
		setInt32Value(t, attr, i, value)
	}

	addMeshAttribute(t, mesh, attr)
	return mesh
}

func TestMeshExtendedPredictionRoundTrips(t *testing.T) {
	testCases := []struct {
		name       string
		build      func(*testing.T) *Mesh
		baseConfig encodeConfig
		attrType   AttributeType
		prediction PredictionMethod
		validate   func(*testing.T, *Mesh)
	}{
		{
			name:       "constrained-multi-parallelogram",
			build:      newPredictionTetrahedronMesh,
			attrType:   AttributeGeneric,
			prediction: PredictionMethodConstrainedMultiParallelogram,
			validate: func(t *testing.T, decoded *Mesh) {
				t.Helper()

				requireMeshEquivalent(t, newPredictionTetrahedronMesh(t), decoded)
			},
		},
		{
			name:  "texcoord-portable",
			build: newTexCoordPredictionMesh,
			baseConfig: func() encodeConfig {
				cfg := encodeConfig{}
				cfg.SetAttributeQuantization(AttributePosition, 12)
				cfg.SetAttributeQuantization(AttributeTexCoord, 12)
				return cfg
			}(),
			attrType:   AttributeTexCoord,
			prediction: PredictionMethodTexCoordsPortable,
			validate: func(t *testing.T, decoded *Mesh) {
				t.Helper()

				tex := requireMeshAttribute(t, decoded, AttributeTexCoord, DataTypeFloat32, 2)
				for i, want := range [][]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
					requireFloat32Entry(t, tex, i, want, 0.02)
				}
			},
		},
		{
			name:  "geometric-normal",
			build: newGeometricNormalPredictionMesh,
			baseConfig: func() encodeConfig {
				cfg := encodeConfig{}
				cfg.SetAttributeQuantization(AttributePosition, 12)
				cfg.SetAttributeQuantization(AttributeNormal, 12)
				return cfg
			}(),
			attrType:   AttributeNormal,
			prediction: PredictionMethodGeometricNormal,
			validate: func(t *testing.T, decoded *Mesh) {
				t.Helper()

				normals := requireMeshAttribute(t, decoded, AttributeNormal, DataTypeFloat32, 3)
				for i := 0; i < 4; i++ {
					requireFloat32Entry(t, normals, i, []float32{0, 0, 1}, 0.05)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mesh := tc.build(t)
			defaultData := encodeMesh(t, mesh, withEncodeConfig(tc.baseConfig))

			cfg := cloneEncodeConfig(tc.baseConfig)
			cfg.SetAttributePrediction(tc.attrType, tc.prediction)
			data := encodeMesh(t, mesh, withEncodeConfig(cfg))

			require.False(t, bytes.Equal(data, defaultData), "%s encoding unexpectedly matched default encoding", tc.name)
			decoded := decodeMesh(t, data)
			tc.validate(t, decoded)
			requireDeterministicEncode(t, data, encodeMesh(t, decoded, withEncodeConfig(cfg)))
		})
	}
}

func newTexCoordPredictionMesh(t *testing.T) *Mesh {
	t.Helper()
	mesh := newMeshFromData(t,
		[][]float32{{0, 0, 0}, {2, 0, 0}, {2, 2, 0}, {0, 2, 0}},
		[]Face{{0, 1, 2}, {0, 2, 3}},
	)
	tex := mustNewFloat32Attribute(AttributeTexCoord, 2, 4)
	for i, value := range [][]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
		setFloat32Value(t, tex, i, value...)
	}

	addMeshAttribute(t, mesh, tex)
	return mesh
}

func newGeometricNormalPredictionMesh(t *testing.T) *Mesh {
	t.Helper()
	mesh := newMeshFromData(t,
		[][]float32{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}},
		[]Face{{0, 1, 2}, {0, 2, 3}},
	)
	normals := mustNewFloat32Attribute(AttributeNormal, 3, 4)
	for i := 0; i < 4; i++ {
		setFloat32Value(t, normals, i, 0, 0, 1)
	}

	addMeshAttribute(t, mesh, normals)
	return mesh
}
