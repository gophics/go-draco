package draco

import (
	"fmt"
	"math"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAttributeQuantizationPortableRanges(t *testing.T) {
	mesh, posID, tex0ID, tex1ID := newExpertQuantizationMesh(t)

	testCases := []struct {
		name      string
		configure func(*encodeConfig)
		wantMax   map[int]int32
	}{
		{
			name: "by-attribute-id",
			configure: func(cfg *encodeConfig) {
				cfg.meshMethod = MeshSequentialEncoding
				cfg.SetAttributeQuantizationByID(posID, 16)
				cfg.SetAttributeQuantizationByID(tex0ID, 15)
				cfg.SetAttributeQuantizationByID(tex1ID, 14)
			},
			wantMax: map[int]int32{
				posID:  65535,
				tex0ID: 32767,
				tex1ID: 16383,
			},
		},
		{
			name: "by-attribute-type",
			configure: func(cfg *encodeConfig) {
				cfg.meshMethod = MeshSequentialEncoding
				cfg.SetAttributeQuantization(AttributePosition, 16)
				cfg.SetAttributeQuantization(AttributeTexCoord, 15)
			},
			wantMax: map[int]int32{
				posID:  65535,
				tex0ID: 32767,
				tex1ID: 32767,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := encodeConfig{}
			tc.configure(&cfg)

			data := encodeMesh(t, mesh, withEncodeConfig(cfg))
			decoded := decodeMesh(t, data,
				WithSkipAttributeTransform(AttributePosition),
				WithSkipAttributeTransform(AttributeTexCoord),
			)

			for attrID, wantMax := range tc.wantMax {
				require.Equal(t, wantMax, maxInt32AttributeValue(t, decoded.attribute(attrID)))
			}
		})
	}
}

func TestQuantizedInfinityFails(t *testing.T) {
	pc := newInfinityPointCloud(t)

	testCases := []struct {
		name   string
		method EncodingMethod
	}{
		{name: "sequential", method: PointCloudSequentialEncoding},
		{name: "kd-tree", method: PointCloudKDTreeEncoding},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Encode(testContext(t), pc,
				WithPointCloudMethod(tc.method),
				WithAttributeQuantization(AttributePosition, 11),
			)
			require.Error(t, err)
		})
	}
}

func TestSequentialUnquantizedInfinitySucceeds(t *testing.T) {
	pc := newInfinityPointCloud(t)

	data := encodePointCloud(t, pc, WithPointCloudMethod(PointCloudSequentialEncoding))
	decoded := decodePointCloud(t, data)

	position := requirePointCloudAttribute(t, decoded, AttributePosition, DataTypeFloat32, 3)
	value, err := position.Float32(0)
	require.NoError(t, err)
	require.True(t, math.IsInf(float64(value[0]), 1))
}

func TestMixedQuantizedAndUnquantizedFloatAttributes(t *testing.T) {
	pc := mustNewPointCloud(3)
	position := mustNewFloat32Attribute(AttributePosition, 3, 3)
	normal := mustNewFloat32Attribute(AttributeNormal, 3, 3)
	for i, value := range [][3]float32{{0, 0, 1}, {1, 2, 3}, {4, 5, 6}} {
		setFloat32Value(t, position, i, value[:]...)
		setFloat32Value(t, normal, i, 0, 1, 0)
	}

	addPointCloudAttribute(t, pc, position)
	addPointCloudAttribute(t, pc, normal)

	data := encodePointCloud(t, pc,
		WithAttributeQuantization(AttributePosition, 11),
		WithAttributeQuantization(AttributeNormal, 0),
	)
	decoded := decodePointCloud(t, data)

	decodedNormal := requirePointCloudAttribute(t, decoded, AttributeNormal, DataTypeFloat32, 3)
	requireFloat32Entry(t, decodedNormal, 2, []float32{0, 1, 0}, 0)
}

func TestKDTreeEncodingRequiresFloatQuantization(t *testing.T) {
	pc := mustNewPointCloud(2)
	position := mustNewFloat32Attribute(AttributePosition, 3, 2)
	setFloat32Value(t, position, 0, 0, 0, 0)
	setFloat32Value(t, position, 1, 1, 1, 1)
	addPointCloudAttribute(t, pc, position)

	_, err := Encode(testContext(t), pc, WithPointCloudMethod(PointCloudKDTreeEncoding))
	require.Error(t, err)

	_, err = Encode(testContext(t), pc,
		WithPointCloudMethod(PointCloudKDTreeEncoding),
		WithAttributeQuantization(AttributePosition, 16),
	)
	require.NoError(t, err)
}

func TestEncoderTracksEncodedEntries(t *testing.T) {
	mesh := newTrackingMesh(t)
	pc := mesh.PointCloud.Clone()

	sequentialResult := encodeWithStats(t, mesh)
	require.Equal(t, 0, sequentialResult.Stats.Points)
	require.Equal(t, 0, sequentialResult.Stats.Faces)

	meshResult := encodeWithStats(t, mesh, WithTrackStats())
	require.Equal(t, mesh.PointCount(), meshResult.Stats.Points)
	require.Equal(t, mesh.FaceCount(), meshResult.Stats.Faces)

	pointResult := encodeWithStats(t, pc, WithTrackStats())
	require.Equal(t, pc.PointCount(), pointResult.Stats.Points)
	require.Equal(t, 0, pointResult.Stats.Faces)
}

func TestNoPositionQuantizationNormalCoding(t *testing.T) {
	mesh := newNormalCodingMesh(t)

	data := encodeMesh(t, mesh, WithAttributeQuantization(AttributeNormal, 8))
	decoded := decodeMesh(t, data)

	requireMeshAttribute(t, decoded, AttributePosition, DataTypeFloat32, 3)
	requireMeshAttribute(t, decoded, AttributeNormal, DataTypeFloat32, 3)
}

func TestGridQuantizationComputesExpectedParameters(t *testing.T) {
	mesh, posID := newGridQuantizationMesh(t)

	cfg := encodeConfig{}
	cfg.SetAttributeGridQuantizationByID(posID, 0.1)
	transform, err := attributeQuantizationTransform(posID, mesh.attribute(posID), cfg)
	require.NoError(t, err)

	require.Equal(t, uint8(4), transform.quantizationBits)
	requireFloat32SliceInDelta(t, []float32{0, 0, 0}, transform.minValues, 0)
	require.InDelta(t, 1.5, transform.rangeValue, 1e-6)
}

func TestGridQuantizationWithOffsetComputesExpectedParameters(t *testing.T) {
	mesh, posID := newOffsetGridQuantizationMesh(t)

	cfg := encodeConfig{}
	cfg.SetAttributeGridQuantizationByID(posID, 0.0625)
	transform, err := attributeQuantizationTransform(posID, mesh.attribute(posID), cfg)
	require.NoError(t, err)

	require.Equal(t, uint8(5), transform.quantizationBits)
	requireFloat32SliceInDelta(t, []float32{-0.5625, 0.625, 10.75}, transform.minValues, 0)
	require.InDelta(t, 31*0.0625, transform.rangeValue, 1e-6)
}

func TestPointCloudGridQuantizationEncodeOptions(t *testing.T) {
	mesh, _ := newGridQuantizationMesh(t)
	pc := mesh.PointCloud.Clone()

	geometryOptions := encodeConfig{}
	geometryOptions.SetAttributeGridQuantization(AttributePosition, 0.15)

	manualData := encodePointCloud(t, pc, withEncodeConfig(geometryOptions))
	repeatedData := encodePointCloud(t, pc.Clone(), withEncodeConfig(geometryOptions))
	require.Equal(t, manualData, repeatedData)

	transform, err := attributeQuantizationTransform(0, pc.attribute(0), geometryOptions)
	require.NoError(t, err)
	require.Equal(t, uint8(3), transform.quantizationBits)
	require.InDelta(t, 1.05, transform.rangeValue, 1e-6)
}

func TestSkipTransformWithNoQuantization(t *testing.T) {
	decoded := decodePointCloud(t, readFixture(t, "testdata/point_cloud_sequential.drc"), WithSkipAttributeTransform(AttributePosition))

	requirePointCloudAttribute(t, decoded, AttributePosition, DataTypeFloat32, 3)
}

func TestSkipTransformPreservesUniqueIDs(t *testing.T) {
	mesh := newNormalCodingMesh(t)
	mesh.NamedAttribute(AttributePosition).UniqueID = 7
	mesh.NamedAttribute(AttributeNormal).UniqueID = 42

	data := encodeMesh(t, mesh,
		WithAttributeQuantization(AttributePosition, 10),
		WithAttributeQuantization(AttributeNormal, 11),
	)

	meshNoSkip := decodeMesh(t, data)
	meshSkip := decodeMesh(t, data,
		WithSkipAttributeTransform(AttributePosition),
		WithSkipAttributeTransform(AttributeNormal),
	)

	posNoSkip := requireMeshAttribute(t, meshNoSkip, AttributePosition, DataTypeFloat32, 3)
	normNoSkip := requireMeshAttribute(t, meshNoSkip, AttributeNormal, DataTypeFloat32, 3)
	posSkip := requireMeshAttribute(t, meshSkip, AttributePosition, DataTypeInt32, 3)
	normSkip := requireMeshAttribute(t, meshSkip, AttributeNormal, DataTypeInt32, 2)

	require.Equal(t, posNoSkip.UniqueID, posSkip.UniqueID)
	require.Equal(t, normNoSkip.UniqueID, normSkip.UniqueID)
}

func TestKDTreeEncodingMatrix(t *testing.T) {
	testCases := []struct {
		name    string
		build   func(*testing.T) *PointCloud
		config  func(*encodeConfig)
		epsilon float64
	}{
		{
			name:    "float-position",
			build:   newKDTreeFloatPositionFixture,
			config:  configureKDTreePositionQuantization16,
			epsilon: 1e-2,
		},
		{
			name:    "uint32-position",
			build:   newKDTreeUint32PositionFixture,
			config:  func(cfg *encodeConfig) {},
			epsilon: 0,
		},
		{
			name:    "higher-dimension-varied-types",
			build:   newKDTreeVariedTypesFixture,
			config:  func(cfg *encodeConfig) {},
			epsilon: 0,
		},
		{
			name:    "float-generic",
			build:   newKDTreeFloatGenericFixture,
			config:  configureKDTreeGenericQuantization16,
			epsilon: 1e-2,
		},
		{
			name:    "signed-types",
			build:   newKDTreeSignedTypesFixture,
			config:  func(cfg *encodeConfig) {},
			epsilon: 0,
		},
		{
			name:    "high-dimensional",
			build:   newKDTreeHighDimensionalFixture,
			config:  func(cfg *encodeConfig) {},
			epsilon: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pc := tc.build(t)
			for compressionLevel := 0; compressionLevel <= 6; compressionLevel++ {
				t.Run(fmt.Sprintf("compression-level=%d", compressionLevel), func(t *testing.T) {
					cfg := encodeConfig{pointCloudMethod: PointCloudKDTreeEncoding}
					tc.config(&cfg)
					cfg.SetKDTreeCompressionLevel(compressionLevel)
					data := encodePointCloud(t, pc, withEncodeConfig(cfg))
					decoded := decodePointCloud(t, data)
					requirePointCloudApproxEqual(t, pc, decoded, tc.epsilon)
				})
			}
		})
	}
}

func configureKDTreePositionQuantization16(cfg *encodeConfig) {
	cfg.SetAttributeQuantization(AttributePosition, 16)
}

func configureKDTreeGenericQuantization16(cfg *encodeConfig) {
	cfg.SetAttributeQuantization(AttributeGeneric, 16)
}

func newKDTreeFloatPositionFixture(t *testing.T) *PointCloud {
	t.Helper()

	pc := mustNewPointCloud(4)
	position := mustNewFloat32Attribute(AttributePosition, 3, 4)
	for i, value := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}} {
		setFloat32Value(t, position, i, value[:]...)
	}

	addPointCloudAttribute(t, pc, position)
	return pc
}

func newKDTreeUint32PositionFixture(t *testing.T) *PointCloud {
	t.Helper()

	builder := pointCloudBuilder{}
	builder.Start(120)
	attID, err := builder.AddAttribute(AttributePosition, 3, DataTypeUint32)
	require.NoError(t, err)
	for i := 0; i < 120; i++ {
		value := [3]uint32{
			uint32(8 * ((i * 7) % 127)),
			uint32(13 * ((i * 3) % 321)),
			uint32(29 * ((i * 19) % 450)),
		}
		require.NoError(t, builder.SetAttributeValueForPoint(attID, i, value))
	}

	return finalizePointCloudBuilder(t, &builder)
}

func newKDTreeVariedTypesFixture(t *testing.T) *PointCloud {
	t.Helper()

	builder := pointCloudBuilder{}
	builder.Start(120)
	att3, err := builder.AddAttribute(AttributePosition, 3, DataTypeUint32)
	require.NoError(t, err)
	att2, err := builder.AddAttribute(AttributePosition, 2, DataTypeUint16)
	require.NoError(t, err)
	att1, err := builder.AddAttribute(AttributeGeneric, 1, DataTypeUint8)
	require.NoError(t, err)
	for i := 0; i < 120; i++ {
		require.NoError(t, builder.SetAttributeValueForPoint(att3, i, [3]uint32{
			uint32(8 * ((i * 7) % 127)),
			uint32(13 * ((i * 3) % 321)),
			uint32(29 * ((i * 19) % 450)),
		}))
		require.NoError(t, builder.SetAttributeValueForPoint(att2, i, [2]uint16{
			uint16(8*((i*7)%127) + 1),
			uint16(13*((i*3)%321) + 1),
		}))
		require.NoError(t, builder.SetAttributeValueForPoint(att1, i, [1]uint8{
			uint8(8*((i*7)%127) + 11),
		}))
	}

	return finalizePointCloudBuilder(t, &builder)
}

func newKDTreeFloatGenericFixture(t *testing.T) *PointCloud {
	t.Helper()

	builder := pointCloudBuilder{}
	builder.Start(130)
	att3, err := builder.AddAttribute(AttributePosition, 3, DataTypeUint32)
	require.NoError(t, err)
	attFloat, err := builder.AddAttribute(AttributeGeneric, 2, DataTypeFloat32)
	require.NoError(t, err)
	for i := 0; i < 130; i++ {
		require.NoError(t, builder.SetAttributeValueForPoint(att3, i, [3]uint32{
			uint32(8 * ((i * 7) % 125)),
			uint32(13 * ((i * 3) % 334)),
			uint32(29 * ((i * 19) % 470)),
		}))
		require.NoError(t, builder.SetAttributeValueForPoint(attFloat, i, [2]float32{
			float32(8*((i*7)%127)+1) / 2.5,
			float32(13*((i*3)%321)+1) / 3.2,
		}))
	}

	return finalizePointCloudBuilder(t, &builder)
}

func newKDTreeSignedTypesFixture(t *testing.T) *PointCloud {
	t.Helper()

	builder := pointCloudBuilder{}
	builder.Start(120)
	att3, err := builder.AddAttribute(AttributePosition, 3, DataTypeUint32)
	require.NoError(t, err)
	att2, err := builder.AddAttribute(AttributePosition, 2, DataTypeInt32)
	require.NoError(t, err)
	att1, err := builder.AddAttribute(AttributeGeneric, 1, DataTypeInt16)
	require.NoError(t, err)
	for i := 0; i < 120; i++ {
		require.NoError(t, builder.SetAttributeValueForPoint(att3, i, [3]uint32{
			uint32(8 * ((i * 7) % 127)),
			uint32(13 * ((i * 3) % 321)),
			uint32(29 * ((i * 19) % 450)),
		}))
		value2 := [2]int32{
			int32(8*((i*7)%127) + 1),
			int32(13*((i*3)%321) + 1),
		}
		if i%3 == 0 {
			value2[0] = -value2[0]
		}

		require.NoError(t, builder.SetAttributeValueForPoint(att2, i, value2))
		value1 := [1]int16{int16(8*((i*7)%127) + 11)}
		if i%5 == 0 {
			value1[0] = -value1[0]
		}

		require.NoError(t, builder.SetAttributeValueForPoint(att1, i, value1))
	}

	return finalizePointCloudBuilder(t, &builder)
}

func newKDTreeHighDimensionalFixture(t *testing.T) *PointCloud {
	t.Helper()

	builder := pointCloudBuilder{}
	builder.Start(120)
	attID, err := builder.AddAttribute(AttributePosition, 42, DataTypeUint32)
	require.NoError(t, err)
	for i := 0; i < 120; i++ {
		var value [42]uint32
		for d := range value {
			value[d] = uint32(8 * ((i + d) * (7 + (d % 4)) % (127 + d%3)))
		}

		require.NoError(t, builder.SetAttributeValueForPoint(attID, i, value))
	}

	return finalizePointCloudBuilder(t, &builder)
}

func finalizePointCloudBuilder(t *testing.T, builder *pointCloudBuilder) *PointCloud {
	t.Helper()

	pc, err := builder.Finalize(false)
	require.NoError(t, err)
	return pc
}

func newExpertQuantizationMesh(t *testing.T) (*Mesh, int, int, int) {
	t.Helper()

	var builder triangleSoupMeshBuilder
	builder.Start(1)

	posID, err := builder.AddAttribute(AttributePosition, 3, DataTypeFloat32)
	require.NoError(t, err)
	tex0ID, err := builder.AddAttribute(AttributeTexCoord, 2, DataTypeFloat32)
	require.NoError(t, err)
	tex1ID, err := builder.AddAttribute(AttributeTexCoord, 2, DataTypeFloat32)
	require.NoError(t, err)

	require.NoError(t, builder.SetAttributeValuesForFace(
		posID,
		0,
		[3]float32{0, 0, 0},
		[3]float32{1, 0, 0},
		[3]float32{1, 1, 0},
	))
	require.NoError(t, builder.SetAttributeValuesForFace(
		tex0ID,
		0,
		[2]float32{0, 0},
		[2]float32{1, 0},
		[2]float32{1, 1},
	))
	require.NoError(t, builder.SetAttributeValuesForFace(
		tex1ID,
		0,
		[2]float32{0, 0},
		[2]float32{1, 0},
		[2]float32{1, 1},
	))

	mesh, err := builder.Finalize()
	require.NoError(t, err)
	return mesh, posID, tex0ID, tex1ID
}

func newInfinityPointCloud(t *testing.T) *PointCloud {
	t.Helper()

	pc := mustNewPointCloud(2)
	position := mustNewFloat32Attribute(AttributePosition, 3, 2)
	setFloat32Value(t, position, 0, float32(math.Inf(1)), 0, 0)
	setFloat32Value(t, position, 1, 1, 2, 3)
	addPointCloudAttribute(t, pc, position)
	return pc
}

func newTrackingMesh(t *testing.T) *Mesh {
	t.Helper()

	mesh := mustNewMesh(3)
	position := mustNewFloat32Attribute(AttributePosition, 3, 3)
	for i, value := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}} {
		setFloat32Value(t, position, i, value[:]...)
	}

	addMeshAttribute(t, mesh, position)
	addFace(t, mesh, Face{0, 1, 2})
	return mesh
}

func newNormalCodingMesh(t *testing.T) *Mesh {
	t.Helper()

	mesh := mustNewMesh(3)
	position := mustNewFloat32Attribute(AttributePosition, 3, 3)
	normal := mustNewFloat32Attribute(AttributeNormal, 3, 3)
	for i, value := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}} {
		setFloat32Value(t, position, i, value[:]...)
		setFloat32Value(t, normal, i, 0, 0, 1)
	}

	addMeshAttribute(t, mesh, position)
	addMeshAttribute(t, mesh, normal)
	addFace(t, mesh, Face{0, 1, 2})
	return mesh
}

func newGridQuantizationMesh(t *testing.T) (*Mesh, int) {
	t.Helper()

	mesh := mustNewMesh(8)
	position := mustNewFloat32Attribute(AttributePosition, 3, 8)
	values := [][3]float32{
		{0, 0, 0},
		{1, 0, 0},
		{1, 1, 0},
		{0, 1, 0},
		{0, 0, 1},
		{1, 0, 1},
		{1, 1, 1},
		{0, 1, 1},
	}
	for i, value := range values {
		setFloat32Value(t, position, i, value[:]...)
	}

	posID := addMeshAttribute(t, mesh, position)
	for _, face := range []Face{{0, 1, 2}, {0, 2, 3}, {4, 5, 6}, {4, 6, 7}} {
		addFace(t, mesh, face)
	}

	return mesh, posID
}

func newOffsetGridQuantizationMesh(t *testing.T) (*Mesh, int) {
	t.Helper()

	mesh, posID := newGridQuantizationMesh(t)
	position := mesh.attribute(posID)
	for i := 0; i < position.EntryCount(); i++ {
		value, err := position.Float32(i)
		require.NoError(t, err)
		setFloat32Value(t, position, i, value[0]-0.55, value[1]+0.65, value[2]+10.75)
	}

	return mesh, posID
}

func maxInt32AttributeValue(t *testing.T, attr *Attribute) int32 {
	t.Helper()

	var maxValue int32
	for entry := 0; entry < attr.EntryCount(); entry++ {
		values, err := attr.Int32(entry)
		require.NoError(t, err)
		for _, value := range values {
			if value > maxValue {
				maxValue = value
			}
		}
	}

	return maxValue
}

func requirePointCloudApproxEqual(t *testing.T, want, got *PointCloud, epsilon float64) {
	t.Helper()

	require.Equal(t, want.PointCount(), got.PointCount())
	require.Equal(t, want.AttributeCount(), got.AttributeCount())
	for i := 0; i < want.AttributeCount(); i++ {
		wantAttr := want.attribute(i)
		gotAttr := got.attribute(i)
		require.True(t, attributeSchemaEqual(wantAttr, gotAttr))

		switch wantAttr.DataType {
		case DataTypeFloat32, DataTypeFloat64:
			wantValues := sortedFloatValues(t, wantAttr, want.PointCount())
			gotValues := sortedFloatValues(t, gotAttr, got.PointCount())
			require.Len(t, gotValues, len(wantValues))
			for j := range wantValues {
				require.InDelta(t, wantValues[j], gotValues[j], epsilon)
			}
		default:
			wantRaw := sortedRawEntries(t, wantAttr, want.PointCount())
			gotRaw := sortedRawEntries(t, gotAttr, got.PointCount())
			require.Len(t, gotRaw, len(wantRaw))
			for j := range wantRaw {
				require.Equal(t, wantRaw[j], gotRaw[j])
			}
		}
	}
}

func sortedFloatValues(t *testing.T, attr *Attribute, numPoints int) []float64 {
	t.Helper()

	values := make([]float64, 0, numPoints*attr.NumComponents)
	for pointID := 0; pointID < numPoints; pointID++ {
		entry, err := attr.Float32(int(attr.mappedIndex(pointID)))
		require.NoError(t, err)
		for _, value := range entry {
			values = append(values, float64(value))
		}
	}

	sort.Float64s(values)
	return values
}

func sortedRawEntries(t *testing.T, attr *Attribute, numPoints int) []string {
	t.Helper()

	values := make([]string, 0, numPoints)
	for pointID := 0; pointID < numPoints; pointID++ {
		entry, err := attr.RawValue(int(attr.mappedIndex(pointID)))
		require.NoError(t, err)
		values = append(values, string(entry))
	}

	sort.Strings(values)
	return values
}
