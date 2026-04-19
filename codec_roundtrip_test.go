package draco

import (
	"bytes"
	"encoding/binary"
	"testing"

	md "github.com/gophics/go-draco/metadata"
	"github.com/stretchr/testify/require"
)

func TestPointCloudRoundTrips(t *testing.T) {
	testCases := []struct {
		name          string
		build         func(*testing.T) *PointCloud
		opts          []EncodeOption
		validate      func(*testing.T, *PointCloud, []byte)
		checkRepeated bool
		matchOriginal bool
	}{
		{
			name:          "sequential-basic",
			build:         newBasicPointCloud,
			matchOriginal: true,
			validate: func(t *testing.T, decoded *PointCloud, data []byte) {
				t.Helper()
				requirePointCloudCounts(t, decoded, 2, 2)
				metadata := decoded.MetadataClone()
				require.NotNil(t, metadata)
				gotName, ok := metadata.Root.String("name")
				require.True(t, ok)
				require.Equal(t, "minimal-pc", gotName)
				requireFloat32Entry(t, requirePointCloudAttribute(t, decoded, AttributePosition, DataTypeFloat32, 3), 1, []float32{4, 5, 6}, 0)
				requireRawEntry(t, requirePointCloudAttribute(t, decoded, AttributeColor, DataTypeUint8, 4), 1, []byte{5, 6, 7, 8})
			},
		},
		{
			name:          "sequential-integer-tagged",
			build:         newTaggedIntegerPointCloud,
			matchOriginal: true,
			validate: func(t *testing.T, decoded *PointCloud, data []byte) {
				t.Helper()
				wantValues := [][]int32{
					{1 << 20, -(1 << 19), 7},
					{(1 << 20) + 9, -(1 << 19) + 3, 9},
					{(1 << 20) + 17, -(1 << 19) + 8, 12},
				}
				attr := decoded.attribute(0)
				for i, want := range wantValues {
					requireInt32Entry(t, attr, i, want)
				}
			},
		},
		{
			name:          "sequential-quantized-mixed",
			build:         newQuantizedMixedPointCloud,
			opts:          []EncodeOption{WithAttributeQuantization(AttributePosition, 11)},
			checkRepeated: true,
			validate: func(t *testing.T, decoded *PointCloud, data []byte) {
				t.Helper()
				position := requirePointCloudAttribute(t, decoded, AttributePosition, DataTypeFloat32, 3)
				for i, want := range [][]float32{
					{0.1, 1.2, -3.5},
					{0.15, 1.25, -3.45},
					{2.9, -0.8, 0.4},
				} {
					requireFloat32Entry(t, position, i, want, 0.01)
				}

				color := requirePointCloudAttribute(t, decoded, AttributeColor, DataTypeUint8, 3)
				for i, want := range [][]int32{
					{10, 20, 30},
					{40, 50, 60},
					{70, 80, 90},
				} {
					requireInt32Entry(t, color, i, want)
				}

				skipped := decodePointCloud(t, data, WithSkipAttributeTransform(AttributePosition))
				requirePointCloudAttribute(t, skipped, AttributePosition, DataTypeInt32, 3)
			},
		},
		{
			name:          "kd-tree",
			build:         newKDTreePointCloud,
			opts:          newKDTreeEncodeOptions(11),
			checkRepeated: true,
			validate: func(t *testing.T, decoded *PointCloud, data []byte) {
				t.Helper()
				position := requirePointCloudAttribute(t, decoded, AttributePosition, DataTypeFloat32, 3)
				color := requirePointCloudAttribute(t, decoded, AttributeColor, DataTypeUint8, 3)
				expectedByColor := map[[3]int32][]float32{
					{10, 20, 30}: {0.1, 1.2, -3.5},
					{40, 50, 60}: {0.15, 1.25, -3.45},
					{70, 80, 90}: {2.9, -0.8, 0.4},
					{15, 25, 35}: {2.5, -0.7, 0.35},
				}
				for i := 0; i < decoded.PointCount(); i++ {
					gotColor, err := color.Int32(i)
					require.NoError(t, err)
					want, ok := expectedByColor[[3]int32{gotColor[0], gotColor[1], gotColor[2]}]
					require.True(t, ok, "unexpected kd-tree color %v", gotColor)
					requireFloat32Entry(t, position, i, want, 0.01)
				}

				reencoded := encodePointCloud(t, decoded, newKDTreeEncodeOptions(11)...)
				reencoded2 := encodePointCloud(t, decoded, newKDTreeEncodeOptions(11)...)
				requireDeterministicEncode(t, reencoded, reencoded2)
				requirePointCloudEquivalent(t, decoded, decodePointCloud(t, reencoded))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			source := tc.build(t)
			data := encodePointCloud(t, source, tc.opts...)
			if tc.checkRepeated {
				requireDeterministicEncode(t, data, encodePointCloud(t, source, tc.opts...))
			}

			decoded := decodePointCloud(t, data)
			tc.validate(t, decoded, data)

			reencoded := encodePointCloud(t, decoded, tc.opts...)
			if tc.matchOriginal {
				requireDeterministicEncode(t, data, reencoded)
			}
		})
	}
}

func TestPointCloudSequentialIntegerRawRoundTrip(t *testing.T) {
	pc := newRawIntegerPointCloud(t)
	compressed := encodePointCloud(t, pc)

	rawOpts := []EncodeOption{WithRawAttributeCompression()}
	rawData := encodePointCloud(t, pc, rawOpts...)
	require.False(t, bytes.Equal(rawData, compressed), "raw and compressed point cloud encodings unexpectedly match")

	decoded := decodePointCloud(t, rawData)
	attr := decoded.attribute(0)
	for i, want := range [][]int32{{1024, -12}, {1027, -11}, {1032, -9}, {1045, -4}} {
		requireInt32Entry(t, attr, i, want)
	}

	reencoded := encodePointCloud(t, decoded, rawOpts...)
	requireDeterministicEncode(t, rawData, reencoded)
}

func TestMeshRoundTrips(t *testing.T) {
	testCases := []struct {
		name          string
		build         func(*testing.T) *Mesh
		opts          []EncodeOption
		validate      func(*testing.T, *Mesh, []byte)
		checkRepeated bool
	}{
		{
			name:  "sequential-basic",
			build: newBasicMesh,
			validate: func(t *testing.T, decoded *Mesh, _ []byte) {
				t.Helper()
				requireMeshCounts(t, decoded, 1, 3, 1)
				require.Equal(t, Face{0, 1, 2}, decoded.face(0))
			},
		},
		{
			name:  "sequential-quantized",
			build: newQuantizedMesh,
			opts:  []EncodeOption{WithAttributeQuantization(AttributePosition, 10)},
			validate: func(t *testing.T, decoded *Mesh, _ []byte) {
				t.Helper()
				requireMeshCounts(t, decoded, 1, 3, 1)
				position := requireMeshAttribute(t, decoded, AttributePosition, DataTypeFloat32, 3)
				for i, want := range [][]float32{
					{0, 0, 0},
					{1.2, 0.5, 0.25},
					{0.25, 1.8, 0.75},
				} {
					requireFloat32Entry(t, position, i, want, 0.01)
				}
			},
		},
		{
			name:          "sequential-compressed-connectivity",
			build:         newQuadMesh,
			opts:          []EncodeOption{WithConnectivityCompression(true)},
			checkRepeated: true,
			validate: func(t *testing.T, decoded *Mesh, _ []byte) {
				t.Helper()
				requireMeshCounts(t, decoded, 2, 4, 1)
				require.Equal(t, Face{0, 1, 2}, decoded.face(0))
				require.Equal(t, Face{0, 2, 3}, decoded.face(1))
			},
		},
		{
			name:          "edgebreaker-standard",
			build:         newQuadMesh,
			opts:          []EncodeOption{WithMeshMethod(MeshEdgebreakerEncoding)},
			checkRepeated: true,
			validate: func(t *testing.T, decoded *Mesh, _ []byte) {
				t.Helper()
				requireMeshEquivalent(t, newQuadMesh(t), decoded)
			},
		},
		{
			name:          "edgebreaker-valence",
			build:         newQuadMesh,
			opts:          newEdgebreakerEncodeOptions(EdgebreakerMethodValence),
			checkRepeated: true,
			validate: func(t *testing.T, decoded *Mesh, _ []byte) {
				t.Helper()
				requireMeshEquivalent(t, newQuadMesh(t), decoded)
			},
		},
		{
			name:          "edgebreaker-predictive",
			build:         newQuadMesh,
			opts:          newEdgebreakerEncodeOptions(EdgebreakerMethodPredictive),
			checkRepeated: true,
			validate: func(t *testing.T, decoded *Mesh, _ []byte) {
				t.Helper()
				requireMeshEquivalent(t, newQuadMesh(t), decoded)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			source := tc.build(t)
			data := encodeMesh(t, source, tc.opts...)
			if tc.checkRepeated {
				requireDeterministicEncode(t, data, encodeMesh(t, source, tc.opts...))
			}

			decoded := decodeMesh(t, data)
			tc.validate(t, decoded, data)

			reencoded := encodeMesh(t, decoded, tc.opts...)
			reencoded2 := encodeMesh(t, decoded, tc.opts...)
			requireDeterministicEncode(t, reencoded, reencoded2)
			requireMeshEquivalent(t, decoded, decodeMesh(t, reencoded))
		})
	}
}

func TestMeshOctahedronNormalsRoundTrips(t *testing.T) {
	testCases := []struct {
		name    string
		opts    []EncodeOption
		tol     float64
		rawMode bool
	}{
		{
			name: "compressed",
			opts: []EncodeOption{WithAttributeQuantization(AttributeNormal, 8)},
			tol:  0.03,
		},
		{
			name:    "raw",
			opts:    []EncodeOption{WithAttributeQuantization(AttributeNormal, 8), WithRawAttributeCompression()},
			tol:     0.03,
			rawMode: true,
		},
	}

	wantNormals := [][]float32{
		{0, 0, 1},
		{0.57735026, 0.57735026, 0.57735026},
		{-0.4082483, 0.8164966, -0.4082483},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mesh := newMeshWithNormals(t)
			data := encodeMesh(t, mesh, tc.opts...)
			requireDeterministicEncode(t, data, encodeMesh(t, mesh, tc.opts...))

			decoded := decodeMesh(t, data)
			normal := requireMeshAttribute(t, decoded, AttributeNormal, DataTypeFloat32, 3)
			for i, want := range wantNormals {
				requireFloat32Entry(t, normal, i, want, tc.tol)
			}

			if !tc.rawMode {
				skipped := decodeMesh(t, data, WithSkipAttributeTransform(AttributeNormal))
				skippedNormal := requireMeshAttribute(t, skipped, AttributeNormal, DataTypeInt32, 2)
				require.Equal(t, 2, skippedNormal.NumComponents)
			}

			requireDeterministicEncode(t, data, encodeMesh(t, decoded, tc.opts...))
		})
	}
}

func TestFixtureMeshRoundTrips(t *testing.T) {
	testCases := []struct {
		name string
		path string
		opts []EncodeOption
	}{
		{name: "sequential-owned", path: "testdata/mesh_sequential.drc"},
		{name: "edgebreaker-owned", path: "testdata/mesh_edgebreaker.drc", opts: []EncodeOption{WithMeshMethod(MeshEdgebreakerEncoding)}},
		{name: "valence-owned", path: "testdata/mesh_edgebreaker.drc", opts: newEdgebreakerEncodeOptions(EdgebreakerMethodValence)},
		{name: "predictive-owned", path: "testdata/mesh_edgebreaker.drc", opts: newEdgebreakerEncodeOptions(EdgebreakerMethodPredictive)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			decoded := decodeMesh(t, readFixture(t, tc.path))
			reencoded := encodeMesh(t, decoded, tc.opts...)
			redecoded := decodeMesh(t, reencoded)
			requireMeshEquivalent(t, decoded, redecoded)
			requireDeterministicEncode(t, reencoded, encodeMesh(t, redecoded, tc.opts...))
		})
	}
}

func newBasicPointCloud(t *testing.T) *PointCloud {
	t.Helper()
	pc := mustNewPointCloud(2)
	position := mustNewFloat32Attribute(AttributePosition, 3, 2)
	for i, value := range [][]float32{{1, 2, 3}, {4, 5, 6}} {
		setFloat32Value(t, position, i, value...)
	}

	addPointCloudAttribute(t, pc, position)

	color, err := NewAttribute(AttributeColor, DataTypeUint8, 4, 2)
	require.NoError(t, err)
	for i, raw := range [][]byte{{1, 2, 3, 4}, {5, 6, 7, 8}} {
		setRawValue(t, color, i, raw)
	}

	addPointCloudAttribute(t, pc, color)

	metadata := &md.GeometryMetadata{}
	require.NoError(t, metadata.Root.Set("name", "minimal-pc"))
	require.NoError(t, pc.SetMetadata(metadata))
	return pc
}

func newTaggedIntegerPointCloud(t *testing.T) *PointCloud {
	t.Helper()
	pc := mustNewPointCloud(3)
	attr, err := NewAttribute(AttributeGeneric, DataTypeInt32, 3, 3)
	require.NoError(t, err)
	for i, value := range [][]int32{
		{1 << 20, -(1 << 19), 7},
		{(1 << 20) + 9, -(1 << 19) + 3, 9},
		{(1 << 20) + 17, -(1 << 19) + 8, 12},
	} {
		setInt32Value(t, attr, i, value...)
	}

	addPointCloudAttribute(t, pc, attr)
	return pc
}

func newRawIntegerPointCloud(t *testing.T) *PointCloud {
	t.Helper()
	pc := mustNewPointCloud(4)
	attr, err := NewAttribute(AttributeGeneric, DataTypeInt32, 2, 4)
	require.NoError(t, err)
	for i, value := range [][]int32{
		{1024, -12},
		{1027, -11},
		{1032, -9},
		{1045, -4},
	} {
		setInt32Value(t, attr, i, value...)
	}

	addPointCloudAttribute(t, pc, attr)
	return pc
}

func newQuantizedMixedPointCloud(t *testing.T) *PointCloud {
	t.Helper()
	pc := mustNewPointCloud(3)
	position := mustNewFloat32Attribute(AttributePosition, 3, 3)
	for i, value := range [][]float32{
		{0.1, 1.2, -3.5},
		{0.15, 1.25, -3.45},
		{2.9, -0.8, 0.4},
	} {
		setFloat32Value(t, position, i, value...)
	}

	addPointCloudAttribute(t, pc, position)

	color, err := NewAttribute(AttributeColor, DataTypeUint8, 3, 3)
	require.NoError(t, err)
	for i, value := range [][]int32{{10, 20, 30}, {40, 50, 60}, {70, 80, 90}} {
		setInt32Value(t, color, i, value...)
	}

	addPointCloudAttribute(t, pc, color)
	return pc
}

func newKDTreePointCloud(t *testing.T) *PointCloud {
	t.Helper()
	pc := mustNewPointCloud(4)
	position := mustNewFloat32Attribute(AttributePosition, 3, 4)
	for i, value := range [][]float32{
		{0.1, 1.2, -3.5},
		{0.15, 1.25, -3.45},
		{2.9, -0.8, 0.4},
		{2.5, -0.7, 0.35},
	} {
		setFloat32Value(t, position, i, value...)
	}

	addPointCloudAttribute(t, pc, position)

	color, err := NewAttribute(AttributeColor, DataTypeUint8, 3, 4)
	require.NoError(t, err)
	for i, value := range [][]int32{{10, 20, 30}, {40, 50, 60}, {70, 80, 90}, {15, 25, 35}} {
		setInt32Value(t, color, i, value...)
	}

	addPointCloudAttribute(t, pc, color)
	return pc
}

func newBasicMesh(t *testing.T) *Mesh {
	t.Helper()
	return newMeshFromData(t,
		[][]float32{{0.123, 0.6514, 0.000001}, {0.342, 0.1234, 0.000002}, {0.156, 0.8422, 0.000003}},
		[]Face{{0, 1, 2}},
	)
}

func newQuantizedMesh(t *testing.T) *Mesh {
	t.Helper()
	return newMeshFromData(t,
		[][]float32{{0, 0, 0}, {1.2, 0.5, 0.25}, {0.25, 1.8, 0.75}},
		[]Face{{0, 1, 2}},
	)
}

func newQuadMesh(t *testing.T) *Mesh {
	t.Helper()
	return newMeshFromData(t,
		[][]float32{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}},
		[]Face{{0, 1, 2}, {0, 2, 3}},
	)
}

func newMeshWithNormals(t *testing.T) *Mesh {
	t.Helper()
	mesh := newMeshFromData(t,
		[][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}},
		[]Face{{0, 1, 2}},
	)
	normals := mustNewFloat32Attribute(AttributeNormal, 3, 3)
	for i, value := range [][]float32{
		{0, 0, 1},
		{0.57735026, 0.57735026, 0.57735026},
		{-0.4082483, 0.8164966, -0.4082483},
	} {
		setFloat32Value(t, normals, i, value...)
	}

	addMeshAttribute(t, mesh, normals)
	return mesh
}

func newMeshFromData(t *testing.T, positions [][]float32, faces []Face) *Mesh {
	t.Helper()
	mesh := mustNewMesh(len(positions))
	position := mustNewFloat32Attribute(AttributePosition, 3, len(positions))
	for i, value := range positions {
		setFloat32Value(t, position, i, value...)
	}

	addMeshAttribute(t, mesh, position)
	for _, face := range faces {
		addFace(t, mesh, face)
	}

	return mesh
}

func newKDTreeEncodeOptions(bits int) []EncodeOption {
	return []EncodeOption{
		WithPointCloudMethod(PointCloudKDTreeEncoding),
		WithAttributeQuantization(AttributePosition, bits),
	}
}

func newEdgebreakerEncodeOptions(method EdgebreakerMethod) []EncodeOption {
	return []EncodeOption{
		WithMeshMethod(MeshEdgebreakerEncoding),
		WithEdgebreakerMethod(method),
	}
}

func TestKDTreeEncodesExplicitMappingsForUint32Attributes(t *testing.T) {
	pc := mustNewPointCloud(4)

	position, err := NewAttribute(AttributePosition, DataTypeUint32, 3, 2)
	require.NoError(t, err)
	setUint32Entry(t, position, 0, 1, 2, 3)
	setUint32Entry(t, position, 1, 4, 5, 6)
	require.NoError(t, position.SetExplicitMapping(4))
	for pointID, entryID := range []uint32{0, 1, 0, 1} {
		require.NoError(t, position.SetPointMapEntry(pointID, entryID))
	}

	addPointCloudAttribute(t, pc, position)

	data := encodePointCloud(t, pc, WithPointCloudMethod(PointCloudKDTreeEncoding))
	decoded := decodePointCloud(t, data)
	requirePointCloudEquivalent(t, pc, decoded)
}

func TestKDTreeEncodesExplicitMappingsForQuantizedFloatAttributes(t *testing.T) {
	pc := mustNewPointCloud(4)

	position := mustNewFloat32Attribute(AttributePosition, 3, 2)
	setFloat32Value(t, position, 0, 0, 0, 0)
	setFloat32Value(t, position, 1, 10, 5, 2)
	require.NoError(t, position.SetExplicitMapping(4))
	for pointID, entryID := range []uint32{0, 1, 0, 1} {
		require.NoError(t, position.SetPointMapEntry(pointID, entryID))
	}

	addPointCloudAttribute(t, pc, position)

	data := encodePointCloud(t, pc,
		WithPointCloudMethod(PointCloudKDTreeEncoding),
		WithAttributeQuantization(AttributePosition, 16),
	)
	decoded := decodePointCloud(t, data)
	requirePointCloudApproxEqual(t, pc, decoded, 1e-3)
}

func setUint32Entry(tb testing.TB, attr *Attribute, entry int, values ...uint32) {
	tb.Helper()
	raw := make([]byte, len(values)*4)
	for i, value := range values {
		binary.LittleEndian.PutUint32(raw[i*4:], value)
	}

	require.NoError(tb, attr.SetRawValue(entry, raw))
}

func TestSkipTransformFixtures(t *testing.T) {
	testCases := []struct {
		name            string
		path            string
		wantType        DataType
		checkColorBytes bool
	}{
		{
			name:            "sequential-point-cloud",
			path:            "testdata/point_cloud_quantized.drc",
			wantType:        DataTypeInt32,
			checkColorBytes: true,
		},
		{
			name:            "kd-tree-point-cloud",
			path:            "testdata/point_cloud_kd_tree.drc",
			wantType:        DataTypeUint32,
			checkColorBytes: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := readFixture(t, tc.path)
			decoded := decodePointCloud(t, data)
			skipped := decodePointCloud(t, data, WithSkipAttributeTransform(AttributePosition))

			requirePointCloudAttribute(t, decoded, AttributePosition, DataTypeFloat32, 3)
			requirePointCloudAttribute(t, skipped, AttributePosition, tc.wantType, 3)

			if !tc.checkColorBytes {
				return
			}

			color := requirePointCloudAttribute(t, decoded, AttributeColor, DataTypeUint8, 3)
			colorSkipped := requirePointCloudAttribute(t, skipped, AttributeColor, DataTypeUint8, 3)
			for i := 0; i < decoded.PointCount(); i++ {
				got, err := color.RawValue(i)
				require.NoError(t, err)
				gotSkipped, err := colorSkipped.RawValue(i)
				require.NoError(t, err)
				require.True(t, bytes.Equal(got, gotSkipped), "color value %d differs after skipping transform", i)
			}
		})
	}
}

func TestExplicitQuantizationRoundTrips(t *testing.T) {
	testCases := []struct {
		name string
		opts []EncodeOption
	}{
		{
			name: "sequential",
			opts: explicitQuantizationOptions(PointCloudSequentialEncoding),
		},
		{
			name: "kd-tree",
			opts: explicitQuantizationOptions(PointCloudKDTreeEncoding),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pc := newExplicitQuantizationPointCloud(t)
			decoded := decodePointCloud(t, encodePointCloud(t, pc, tc.opts...))
			position := requirePointCloudAttribute(t, decoded, AttributePosition, DataTypeFloat32, 3)
			for i, want := range [][]float32{{1, 2, 3}, {4, 5, 6}} {
				requireFloat32Entry(t, position, i, want, 0)
			}
		})
	}
}

func TestExplicitQuantizationRequiresOriginAndRange(t *testing.T) {
	pc := mustNewPointCloud(1)
	position := mustNewFloat32Attribute(AttributePosition, 3, 1)
	setFloat32Value(t, position, 0, 1, 2, 3)
	addPointCloudAttribute(t, pc, position)

	cfg := encodeConfig{}
	cfg.SetAttributeQuantization(AttributePosition, 8)
	cfg.setAttributeQuantizationOrigin(AttributePosition, []float32{0, 0, 0})

	_, err := Encode(testContext(t), pc, withEncodeConfig(cfg))
	require.Error(t, err)
}

func explicitQuantizationOptions(method EncodingMethod) []EncodeOption {
	return []EncodeOption{
		WithPointCloudMethod(method),
		WithAttributeExplicitQuantization(AttributePosition, 4, []float32{0, 0, 0}, 15),
	}
}

func newExplicitQuantizationPointCloud(t *testing.T) *PointCloud {
	t.Helper()
	pc := mustNewPointCloud(2)
	position := mustNewFloat32Attribute(AttributePosition, 3, 2)
	setFloat32Value(t, position, 0, 1, 2, 3)
	setFloat32Value(t, position, 1, 4, 5, 6)
	addPointCloudAttribute(t, pc, position)
	return pc
}
