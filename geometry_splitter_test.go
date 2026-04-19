package draco

import (
	"testing"

	md "github.com/gophics/go-draco/metadata"
	"github.com/stretchr/testify/require"
)

func TestPointCloudSplitByAttribute(t *testing.T) {
	pc := mustNewPointCloud(4)
	position := mustNewFloat32Attribute(AttributePosition, 3, 4)
	groupAttr, err := NewAttribute(AttributeGeneric, DataTypeInt32, 1, 4)
	require.NoError(t, err)

	for i, xyz := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {1, 1, 0}} {
		setFloat32Value(t, position, i, xyz[:]...)
	}

	for i, value := range []int32{7, 9, 7, 9} {
		setInt32Value(t, groupAttr, i, value)
	}

	addPointCloudAttribute(t, pc, position)
	groupID := addPointCloudAttribute(t, pc, groupAttr)
	metadata := &md.GeometryMetadata{}
	require.NoError(t, metadata.Root.SetInt("tag", 1))
	require.NoError(t, pc.SetMetadata(metadata))

	split, err := pc.SplitByAttribute(groupID)
	require.NoError(t, err)
	require.Len(t, split, 2)
	for _, sub := range split {
		requirePointCloudCounts(t, sub, 2, 2)
		subMetadata := sub.MetadataClone()
		require.NotNil(t, subMetadata)
		require.Len(t, subMetadata.Root.Entries, 1)
	}
}

func TestMeshSplitByAttributePreservesStructuralMetadata(t *testing.T) {
	mesh := mustNewMesh(6)
	position := mustNewFloat32Attribute(AttributePosition, 3, 6)
	groupAttr, err := NewAttribute(AttributeGeneric, DataTypeInt32, 1, 6)
	require.NoError(t, err)

	for i, xyz := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {10, 0, 0}, {11, 0, 0}, {10, 1, 0}} {
		setFloat32Value(t, position, i, xyz[:]...)
	}

	for i, value := range []int32{1, 1, 1, 2, 2, 2} {
		setInt32Value(t, groupAttr, i, value)
	}

	addMeshAttribute(t, mesh, position)
	groupID := addMeshAttribute(t, mesh, groupAttr)
	for _, face := range []Face{{0, 1, 2}, {3, 4, 5}} {
		addFace(t, mesh, face)
	}

	structuralMetadata := &md.StructuralMetadata{}
	_, err = structuralMetadata.AddPropertyAttribute(&md.PropertyAttribute{Name: "set"})
	require.NoError(t, err)
	mesh.setStructuralMetadata(structuralMetadata)
	materials := MaterialLibrary{}
	for i := 0; i <= 9; i++ {
		_, err = materials.AddMaterial(NewMaterial())
		require.NoError(t, err)
	}

	require.NoError(t, mesh.SetMaterials(materials))
	_, err = mesh.AddPropertyAttributesIndex(0)
	require.NoError(t, err)
	require.NoError(t, mesh.AddPropertyAttributesIndexMaterialMask(0, 5))

	features := NewMeshFeatures()
	features.Label = "zones"
	features.FeatureCount = 2
	features.AttributeIndex = groupID
	featureIndex, err := mesh.AddMeshFeatures(features)
	require.NoError(t, err)
	require.NoError(t, mesh.AddMeshFeaturesMaterialMask(featureIndex, 9))

	split, err := mesh.SplitByAttribute(groupID)
	require.NoError(t, err)
	require.Len(t, split, 2)
	for _, sub := range split {
		require.Equal(t, 1, sub.FaceCount())
		subStructuralMetadata := sub.StructuralMetadataClone()
		require.NotNil(t, subStructuralMetadata)
		require.Equal(t, 1, subStructuralMetadata.PropertyAttributeCount())
		require.Equal(t, 1, sub.PropertyAttributeIndexCount())
		index, err := sub.PropertyAttributeIndex(0)
		require.NoError(t, err)
		require.Equal(t, 0, index)
		count, err := sub.PropertyAttributeIndexMaterialMaskCount(0)
		require.NoError(t, err)
		require.Equal(t, 1, count)
		maskValue, err := sub.PropertyAttributeIndexMaterialMask(0, 0)
		require.NoError(t, err)
		require.Equal(t, 5, maskValue)
		require.Equal(t, 1, sub.MeshFeatureCount())
		feature, err := sub.MeshFeature(0)
		require.NoError(t, err)
		require.Equal(t, "zones", feature.Label)
		count, err = sub.MeshFeatureMaterialMaskCount(0)
		require.NoError(t, err)
		require.Equal(t, 1, count)
		maskValue, err = sub.MeshFeatureMaterialMask(0, 0)
		require.NoError(t, err)
		require.Equal(t, 9, maskValue)
	}
}

func TestMeshSplitByAttributeRejectsNonConstantFaceValues(t *testing.T) {
	mesh := mustNewMesh(3)
	position := mustNewFloat32Attribute(AttributePosition, 3, 3)
	groupAttr, err := NewAttribute(AttributeGeneric, DataTypeInt32, 1, 3)
	require.NoError(t, err)

	for i, xyz := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}} {
		setFloat32Value(t, position, i, xyz[:]...)
	}

	for i, value := range []int32{1, 2, 1} {
		setInt32Value(t, groupAttr, i, value)
	}

	addMeshAttribute(t, mesh, position)
	groupID := addMeshAttribute(t, mesh, groupAttr)
	addFace(t, mesh, Face{0, 1, 2})

	_, err = mesh.SplitByAttribute(groupID)
	require.ErrorIs(t, err, ErrInvalidGeometry)
}
