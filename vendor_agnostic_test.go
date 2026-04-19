package draco

import (
	"bytes"
	"testing"

	md "github.com/gophics/go-draco/metadata"
	"github.com/stretchr/testify/require"
)

func TestAttributeTypeExtendedSemantics(t *testing.T) {
	require.Equal(t, "TANGENT", AttributeTangent.String())
	require.Equal(t, "MATERIAL", AttributeMaterial.String())
	require.Equal(t, "JOINTS", AttributeJoints.String())
	require.Equal(t, "WEIGHTS", AttributeWeights.String())

	pc := mustNewPointCloud(1)
	tangent := mustNewFloat32Attribute(AttributeTangent, 4, 1)
	setFloat32Value(t, tangent, 0, 1, 0, 0, 1)
	addPointCloudAttribute(t, pc, tangent)

	require.Equal(t, 1, pc.NamedAttributeCount(AttributeTangent))
	require.NotNil(t, pc.NamedAttribute(AttributeTangent))
}

func TestAttributeBulkExtractionAndViewsRespectExplicitMappings(t *testing.T) {
	pc := mustNewPointCloud(3)

	color, err := NewAttribute(AttributeColor, DataTypeUint8, 2, 2)
	require.NoError(t, err)
	color.Normalized = true
	setRawValue(t, color, 0, []byte{0, 255})
	setRawValue(t, color, 1, []byte{128, 64})
	require.NoError(t, color.SetExplicitMapping(3))
	require.NoError(t, color.SetPointMapEntry(0, 1))
	require.NoError(t, color.SetPointMapEntry(1, 0))
	require.NoError(t, color.SetPointMapEntry(2, 1))
	colorID := addPointCloudAttribute(t, pc, color)

	int64Attr, err := NewInt64Attribute(AttributeMaterial, 1, []int64{-2, 5})
	require.NoError(t, err)
	require.Equal(t, []byte{254, 255, 255, 255, 255, 255, 255, 255}, int64Attr.ExtractRaw()[:8])
	intValues, err := int64Attr.ExtractInt32()
	require.NoError(t, err)
	require.Equal(t, []int32{-2, 5}, intValues)

	raw, err := pc.ExtractMappedRaw(colorID)
	require.NoError(t, err)
	require.Equal(t, []byte{128, 64, 0, 255, 128, 64}, raw)

	floatValues, err := pc.ExtractMappedFloat32(colorID)
	require.NoError(t, err)
	requireFloat64SliceInDelta(t, []float64{
		float64(128) / float64(255),
		float64(64) / float64(255),
		0,
		1,
		float64(128) / float64(255),
		float64(64) / float64(255),
	}, toFloat64Slice(floatValues), 1e-6)

	entryValues, err := color.ExtractInt32()
	require.NoError(t, err)
	require.Equal(t, []int32{0, 255, 128, 64}, entryValues)

	view, err := NewPointCloudView(pc)
	require.NoError(t, err)
	require.Equal(t, 3, view.Info().PointCount)
	require.Equal(t, 1, view.AttributeCount())
	attrView, err := view.Attribute(0)
	require.NoError(t, err)
	require.Equal(t, color.UniqueID, attrView.Descriptor().UniqueID)
	require.Equal(t, raw, attrView.RawData())

	viewFloatValues, err := attrView.Float32()
	require.NoError(t, err)
	require.Equal(t, floatValues, viewFloatValues)
}

func TestMeshViewPreservesFacesAndMappedAttributes(t *testing.T) {
	mesh := newMeshFromData(t, [][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}, []Face{{0, 1, 2}})

	weights, err := NewAttribute(AttributeWeights, DataTypeUint16, 2, 2)
	require.NoError(t, err)
	setRawValue(t, weights, 0, []byte{1, 0, 2, 0})
	setRawValue(t, weights, 1, []byte{3, 0, 4, 0})
	require.NoError(t, weights.SetExplicitMapping(3))
	require.NoError(t, weights.SetPointMapEntry(0, 1))
	require.NoError(t, weights.SetPointMapEntry(1, 0))
	require.NoError(t, weights.SetPointMapEntry(2, 1))
	addMeshAttribute(t, mesh, weights)

	view, err := NewMeshView(mesh)
	require.NoError(t, err)
	require.Equal(t, mesh.Faces(), view.Faces())
	require.Equal(t, 1, view.Info().FaceCount)
	require.Equal(t, mesh.AttributeCount(), view.AttributeCount())
	attrView, err := view.Attribute(1)
	require.NoError(t, err)
	require.Equal(t, AttributeWeights, attrView.Descriptor().Type)

	intValues, err := attrView.Int32()
	require.NoError(t, err)
	require.Equal(t, []int32{3, 4, 1, 2, 3, 4}, intValues)
}

func TestViewsExposeImmutableSnapshots(t *testing.T) {
	pc := mustNewPointCloud(1)
	position := mustNewFloat32Attribute(AttributePosition, 3, 1)
	setFloat32Value(t, position, 0, 0, 1, 2)
	addPointCloudAttribute(t, pc, position)

	view, err := NewPointCloudView(pc)
	require.NoError(t, err)

	info := view.Info()
	info.Attributes[0].UniqueID = 99
	require.NotEqual(t, uint32(99), view.Info().Attributes[0].UniqueID)

	attrs := view.Attributes()
	require.Len(t, attrs, 1)
	attrs[0] = AttributeView{}
	got, err := view.Attribute(0)
	require.NoError(t, err)
	require.Equal(t, AttributePosition, got.Descriptor().Type)

	mesh := newMeshFromData(t, [][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}, []Face{{0, 1, 2}})
	meshView, err := NewMeshView(mesh)
	require.NoError(t, err)
	faces := meshView.Faces()
	faces[0][0] = 99
	require.Equal(t, mesh.Faces(), meshView.Faces())
}

func TestInspectAndDecodeWithStats(t *testing.T) {
	mesh := newMeshFromData(t, [][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}, []Face{{0, 1, 2}})
	require.NoError(t, mesh.SetMetadata(testGeometryMetadata()))
	data := encodeMesh(t, mesh, WithMeshMethod(MeshEdgebreakerEncoding))

	info, err := Inspect(testContext(t), data)
	require.NoError(t, err)
	require.Equal(t, MeshGeometry, info.GeometryType)
	require.Equal(t, MeshEdgebreakerEncoding, info.EncodingMethod)
	require.Equal(t, 3, info.PointCount)
	require.Equal(t, 1, info.FaceCount)
	require.Equal(t, 1, info.AttributeCount)
	require.True(t, info.HasMetadata)
	require.Equal(t, AttributePosition, info.Attributes[0].Type)

	decoder, err := NewDecoder()
	require.NoError(t, err)

	infoFromReader, err := decoder.InspectFrom(testContext(t), bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, info, infoFromReader)

	result, err := decoder.DecodeWithStats(testContext(t), data)
	require.NoError(t, err)
	require.Equal(t, len(data), result.Stats.BytesRead)
	require.Equal(t, info, result.Stats.GeometryInfo)

	decoded, ok := result.Geometry.(*Mesh)
	require.True(t, ok)
	requireMeshEquivalent(t, mesh, decoded)
}

func TestInspectKDTreePointCloud(t *testing.T) {
	pc := mustNewPointCloud(2)
	position := mustNewFloat32Attribute(AttributePosition, 3, 2)
	setFloat32Value(t, position, 0, 0, 0, 0)
	setFloat32Value(t, position, 1, 1, 2, 3)
	addPointCloudAttribute(t, pc, position)

	data := encodePointCloud(t, pc,
		WithPointCloudMethod(PointCloudKDTreeEncoding),
		WithAttributeQuantization(AttributePosition, 12),
	)

	info, err := InspectFrom(testContext(t), bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, PointCloudGeometry, info.GeometryType)
	require.Equal(t, PointCloudKDTreeEncoding, info.EncodingMethod)
	require.Equal(t, 2, info.PointCount)
	require.Zero(t, info.FaceCount)
	require.Len(t, info.Attributes, 1)
	require.Equal(t, DataTypeFloat32, info.Attributes[0].DataType)
}

func TestSequentialEncodeRejectsIgnoredQuantizationAndPrediction(t *testing.T) {
	pc := mustNewPointCloud(2)

	genericFloat64, err := NewFloat64Attribute(AttributeGeneric, 1, []float64{1, 2})
	require.NoError(t, err)
	addPointCloudAttribute(t, pc, genericFloat64)

	_, err = Encode(testContext(t), pc, WithAttributeQuantization(AttributeGeneric, 10))
	require.ErrorIs(t, err, ErrUnsupportedFeature)

	_, err = Encode(testContext(t), pc, WithAttributePrediction(AttributeGeneric, PredictionMethodDifference))
	require.ErrorIs(t, err, ErrUnsupportedFeature)
}

func TestSupportedCapabilitiesIncludesVendorAgnosticSurface(t *testing.T) {
	caps := SupportedCapabilities()
	require.True(t, caps[CapabilityAttributeExtendedSemantics])
	require.True(t, caps[CapabilityInspectGeometry])
	require.True(t, caps[CapabilityDecodeWithStats])
	require.True(t, caps[CapabilityDecodeReusableContext])
	require.True(t, caps[CapabilityAttributeDescriptors])
	require.True(t, caps[CapabilityExtractMappedAttributes])
	require.True(t, caps[CapabilityViewPointCloud])
	require.True(t, caps[CapabilityViewMesh])
}

func testGeometryMetadata() *md.GeometryMetadata {
	metadata := &md.GeometryMetadata{}
	if err := metadata.Root.SetString("name", "vendor-agnostic"); err != nil {
		panic(err)
	}
	return metadata
}

func toFloat64Slice(values []float32) []float64 {
	out := make([]float64, len(values))
	for i, value := range values {
		out[i] = float64(value)
	}
	return out
}

func requireFloat64SliceInDelta(t *testing.T, expected, actual []float64, delta float64) {
	t.Helper()
	require.Len(t, actual, len(expected))
	for i := range expected {
		require.InDeltaf(t, expected[i], actual[i], delta, "component %d", i)
	}
}
