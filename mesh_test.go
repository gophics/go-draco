package draco

import (
	"bytes"
	"testing"

	md "github.com/gophics/go-draco/metadata"
	"github.com/stretchr/testify/require"
)

func testMeshWithPositionAndGeneric(t *testing.T) *Mesh {
	t.Helper()
	mesh := newMeshFromData(t, [][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}, []Face{{0, 1, 2}})

	attr, err := NewAttribute(AttributeGeneric, DataTypeUint8, 1, 3)
	require.NoError(t, err)
	for i, value := range []byte{1, 2, 3} {
		setRawValue(t, attr, i, []byte{value})
	}

	addMeshAttribute(t, mesh, attr)
	return mesh
}

func TestMeshNameAndClone(t *testing.T) {
	mesh := testMeshWithPositionAndGeneric(t)
	require.Empty(t, mesh.Name)
	mesh.Name = "Bob"
	require.Equal(t, "Bob", mesh.Name)

	clone := mesh.Clone()
	require.Equal(t, "Bob", clone.Name)
	requireMeshEquivalent(t, mesh, clone)
}

func TestMeshAccessorsReturnClonesAndCopyIngress(t *testing.T) {
	mesh := testMeshWithPositionAndGeneric(t)

	faces := mesh.Faces()
	require.Len(t, faces, 1)
	faces[0][0] = 99
	face, err := mesh.Face(0)
	require.NoError(t, err)
	require.Equal(t, Face{0, 1, 2}, face)

	feature := NewMeshFeatures()
	feature.Label = "feature"
	feature.FeatureCount = 2
	index, err := mesh.AddMeshFeatures(feature)
	require.NoError(t, err)
	feature.Label = "mutated"
	feature.FeatureCount = 7
	gotFeature, err := mesh.MeshFeature(index)
	require.NoError(t, err)
	require.Equal(t, "feature", gotFeature.Label)
	require.Equal(t, int64(2), gotFeature.FeatureCount)
	gotFeature.Label = "changed"
	gotFeature.FeatureCount = 9
	require.Equal(t, "feature", mesh.meshFeatures[index].Label)
	require.Equal(t, int64(2), mesh.meshFeatures[index].FeatureCount)

	textures := TextureLibrary{}
	textureIndex, err := textures.AddTexture(&Texture{Name: "texture", Data: []byte{1, 2, 3}})
	require.NoError(t, err)
	require.NoError(t, mesh.SetNonMaterialTextures(textures))
	textures.textures[textureIndex].Name = "mutated"
	textureCopy := mesh.NonMaterialTexturesClone()
	require.NotNil(t, textureCopy)
	textureCopy.textures[0].Name = "changed"
	require.Equal(t, "texture", mesh.nonMaterialTextures.textures[0].Name)

	materials := MaterialLibrary{}
	material := NewMaterial()
	material.Name = "material"
	_, err = materials.AddMaterial(material)
	require.NoError(t, err)
	_, err = materials.AddVariant("variant")
	require.NoError(t, err)
	require.NoError(t, mesh.SetMaterials(materials))
	materials.materials[0].Name = "mutated"
	materialCopy := mesh.MaterialsClone()
	require.NotNil(t, materialCopy)
	materialCopy.materials[0].Name = "changed"
	require.Equal(t, "material", mesh.materials.materials[0].Name)

	structuralMetadata := &md.StructuralMetadata{}
	_, err = structuralMetadata.AddPropertyAttribute(&md.PropertyAttribute{Name: "attribute"})
	require.NoError(t, err)
	require.NoError(t, mesh.SetStructuralMetadata(structuralMetadata))
	structuralMetadata.PropertyAttributes[0].Name = "mutated"
	structuralCopy := mesh.StructuralMetadataClone()
	require.NotNil(t, structuralCopy)
	structuralCopy.PropertyAttributes[0].Name = "changed"
	require.Equal(t, "attribute", mesh.structuralMetadata.PropertyAttributes[0].Name)
}

func TestMeshNilReceiverMutationGuards(t *testing.T) {
	var mesh *Mesh
	require.ErrorIs(t, mesh.AddFace(Face{0, 1, 2}), ErrInvalidGeometry)
	require.ErrorIs(t, mesh.SetMaterials(MaterialLibrary{}), ErrInvalidGeometry)
	require.ErrorIs(t, mesh.SetNonMaterialTextures(TextureLibrary{}), ErrInvalidGeometry)
	require.ErrorIs(t, mesh.SetStructuralMetadata(&md.StructuralMetadata{}), ErrInvalidGeometry)
	_, err := mesh.AddMeshFeatures(NewMeshFeatures())
	require.ErrorIs(t, err, ErrInvalidGeometry)
	require.ErrorIs(t, mesh.AddMeshFeaturesMaterialMask(0, 1), ErrInvalidGeometry)
	_, err = mesh.MeshFeature(0)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	_, err = mesh.MeshFeatureMaterialMaskCount(0)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	_, err = mesh.MeshFeatureMaterialMask(0, 0)
	require.ErrorIs(t, err, ErrInvalidGeometry)
}

func TestMeshFeaturesDefaultsAndBookkeeping(t *testing.T) {
	features := NewMeshFeatures()
	require.Empty(t, features.Label)
	require.Equal(t, int64(0), features.FeatureCount)
	require.Equal(t, int64(-1), features.NullFeatureID)
	require.Equal(t, -1, features.AttributeIndex)
	require.Equal(t, -1, features.PropertyTableIndex)

	mesh := testMeshWithPositionAndGeneric(t)
	structuralMetadata := &md.StructuralMetadata{}
	_, err := structuralMetadata.AddPropertyTable(&md.PropertyTable{Name: "continents"})
	require.NoError(t, err)
	mesh.setStructuralMetadata(structuralMetadata)
	materials := MaterialLibrary{}
	for range 8 {
		_, err = materials.AddMaterial(NewMaterial())
		require.NoError(t, err)
	}

	require.NoError(t, mesh.SetMaterials(materials))

	oceans := NewMeshFeatures()
	oceans.Label = "oceans"
	oceans.FeatureCount = 8
	oceans.NullFeatureID = 0
	oceans.AttributeIndex = 1
	oceans.PropertyTableIndex = 0

	continents := NewMeshFeatures()
	continents.Label = "continents"

	index0, err := mesh.AddMeshFeatures(oceans)
	require.NoError(t, err)
	index1, err := mesh.AddMeshFeatures(continents)
	require.NoError(t, err)
	require.NoError(t, mesh.AddMeshFeaturesMaterialMask(index0, 7))
	require.NoError(t, mesh.AddMeshFeaturesMaterialMask(index1, 3))
	require.NoError(t, mesh.Validate())

	require.Equal(t, 2, mesh.MeshFeatureCount())
	feature, err := mesh.MeshFeature(index0)
	require.NoError(t, err)
	require.Equal(t, "oceans", feature.Label)
	maskValue, err := mesh.MeshFeatureMaterialMask(index0, 0)
	require.NoError(t, err)
	require.Equal(t, 7, maskValue)
	require.True(t, mesh.IsAttributeUsedByMeshFeatures(1))
	require.False(t, mesh.IsAttributeUsedByMeshFeatures(0))

	clone := mesh.Clone()
	requireMeshEquivalent(t, mesh, clone)

	require.NoError(t, mesh.DeleteAttribute(0))
	feature, err = mesh.MeshFeature(index0)
	require.NoError(t, err)
	require.Equal(t, 0, feature.AttributeIndex)
	require.NoError(t, mesh.DeleteAttribute(0))
	feature, err = mesh.MeshFeature(index0)
	require.NoError(t, err)
	require.Equal(t, -1, feature.AttributeIndex)
	require.False(t, mesh.IsAttributeUsedByMeshFeatures(0))

	require.NoError(t, mesh.RemoveMeshFeatures(1))
	require.Equal(t, 1, mesh.MeshFeatureCount())
	feature, err = mesh.MeshFeature(0)
	require.NoError(t, err)
	require.Equal(t, "oceans", feature.Label)
}

func TestMeshFeatureMutatorsRejectInvalidInputWithoutMutation(t *testing.T) {
	mesh := testMeshWithPositionAndGeneric(t)

	invalidFeature := NewMeshFeatures()
	invalidFeature.AttributeIndex = mesh.AttributeCount()
	_, err := mesh.AddMeshFeatures(invalidFeature)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	require.Equal(t, 0, mesh.MeshFeatureCount())
	require.NoError(t, mesh.Validate())

	validFeature := NewMeshFeatures()
	featureIndex, err := mesh.AddMeshFeatures(validFeature)
	require.NoError(t, err)
	require.Equal(t, 1, mesh.MeshFeatureCount())

	err = mesh.AddMeshFeaturesMaterialMask(featureIndex, 0)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	maskCount, countErr := mesh.MeshFeatureMaterialMaskCount(featureIndex)
	require.NoError(t, countErr)
	require.Equal(t, 0, maskCount)

	materials := MaterialLibrary{}
	_, err = materials.AddMaterial(NewMaterial())
	require.NoError(t, err)
	require.NoError(t, mesh.SetMaterials(materials))
	require.NoError(t, mesh.AddMeshFeaturesMaterialMask(featureIndex, 0))
	require.NoError(t, mesh.Validate())
	maskCount, countErr = mesh.MeshFeatureMaterialMaskCount(featureIndex)
	require.NoError(t, countErr)
	require.Equal(t, 1, maskCount)
}

func TestMeshEncodeOptionsDriveEncoder(t *testing.T) {
	mesh := testMeshWithPositionAndGeneric(t)

	geometryOptions := encodeConfig{}
	geometryOptions.SetAttributeQuantization(AttributePosition, 8)
	geometryOptions.SetCompressionLevel(6)
	geometryOptions.SetConnectivityCompression(true)

	manualData := encodeMesh(t, mesh, withEncodeConfig(geometryOptions))
	repeatedData := encodeMesh(t, mesh.Clone(), withEncodeConfig(geometryOptions))
	require.Equal(t, manualData, repeatedData)

	overrideOptions := cloneEncodeConfig(geometryOptions)
	overrideOptions.SetAttributeQuantization(AttributePosition, 10)
	overrideOptions.SetConnectivityCompression(false)
	overrideData := encodeMesh(t, mesh, withEncodeConfig(overrideOptions))
	manualOverride := encodeMesh(t, mesh.Clone(), withEncodeConfig(overrideOptions))
	require.Equal(t, overrideData, manualOverride)
	require.False(t, bytes.Equal(manualData, overrideData))
}

func TestMeshMaterialAndTextureStateCloneAndValidate(t *testing.T) {
	mesh := testMeshWithPositionAndGeneric(t)

	texCoord, err := NewAttribute(AttributeTexCoord, DataTypeFloat32, 2, 3)
	require.NoError(t, err)
	setFloat32Value(t, texCoord, 0, 0, 0)
	setFloat32Value(t, texCoord, 1, 1, 0)
	setFloat32Value(t, texCoord, 2, 0, 1)
	addMeshAttribute(t, mesh, texCoord)

	structuralMetadata := &md.StructuralMetadata{}
	_, err = structuralMetadata.AddPropertyAttribute(&md.PropertyAttribute{Name: "regions"})
	require.NoError(t, err)
	mesh.setStructuralMetadata(structuralMetadata)

	featureTexture, err := mesh.nonMaterialTexturesRef().AddTexture(&Texture{
		Name:     "feature-ids",
		MIMEType: "image/png",
		Data:     []byte{1, 2, 3},
	})
	require.NoError(t, err)
	materialTexture, err := mesh.materialsRef().textureLibrary.AddTexture(&Texture{
		Name:     "base-color",
		MIMEType: "image/png",
		Data:     []byte{4, 5, 6},
	})
	require.NoError(t, err)

	material := NewMaterial()
	material.Name = "mat-0"
	textureMap := NewTextureMap()
	textureMap.Type = TextureMapColor
	textureMap.TextureIndex = materialTexture
	textureMap.TexCoordIndex = 0
	require.NoError(t, material.SetTextureMap(textureMap))
	materialIndex, err := mesh.materialsRef().AddMaterial(material)
	require.NoError(t, err)
	_, err = mesh.materialsRef().AddVariant("default")
	require.NoError(t, err)

	feature := NewMeshFeatures()
	feature.Label = "districts"
	feature.PropertyTableIndex = -1
	feature.TextureMap = &TextureMap{
		Type:          TextureMapGeneric,
		Wrapping:      TextureWrappingMode{S: TextureClampToEdge, T: TextureClampToEdge},
		TexCoordIndex: 0,
		TextureIndex:  featureTexture,
		Transform:     DefaultTextureTransform(),
	}
	feature.TextureChannels = []int{0}
	featureIndex, err := mesh.AddMeshFeatures(feature)
	require.NoError(t, err)
	require.NoError(t, mesh.AddMeshFeaturesMaterialMask(featureIndex, materialIndex))

	propertyIndex, err := mesh.AddPropertyAttributesIndex(0)
	require.NoError(t, err)
	require.NoError(t, mesh.AddPropertyAttributesIndexMaterialMask(propertyIndex, materialIndex))

	require.NoError(t, mesh.Validate())

	clone := mesh.Clone()
	requireMeshEquivalent(t, mesh, clone)
	require.Equal(t, 1, clone.MaterialsClone().MaterialCount())
	require.Equal(t, 1, clone.NonMaterialTexturesClone().TextureCount())
	copyMaterial, err := clone.MaterialsClone().Material(0)
	require.NoError(t, err)
	require.Equal(t, TextureMapColor, copyMaterial.TextureMaps[0].Type)
	copyFeature, err := clone.MeshFeature(0)
	require.NoError(t, err)
	require.NotNil(t, copyFeature.TextureMap)

	materials := clone.MaterialsClone()
	materials.materials[0].Name = "changed"
	require.NoError(t, clone.SetMaterials(materials))
	require.False(t, mesh.Equivalent(clone))
}

func TestMeshAddPerVertexAttribute(t *testing.T) {
	mesh := mustNewMesh(4)
	position, err := NewAttribute(AttributePosition, DataTypeFloat32, 3, 3)
	require.NoError(t, err)
	for i, xyz := range [][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}} {
		setFloat32Value(t, position, i, xyz...)
	}

	require.NoError(t, position.SetExplicitMapping(4))
	for pointID, entryID := range []uint32{0, 1, 1, 2} {
		require.NoError(t, position.SetPointMapEntry(pointID, entryID))
	}

	addMeshAttribute(t, mesh, position)
	addFace(t, mesh, Face{0, 1, 2})
	addFace(t, mesh, Face{2, 1, 3})

	attr, err := NewAttribute(AttributeGeneric, DataTypeFloat32, 1, 3)
	require.NoError(t, err)
	for entryID := 0; entryID < 3; entryID++ {
		setFloat32Value(t, attr, entryID, float32(entryID))
	}

	attID, err := mesh.AddPerVertexAttribute(attr)
	require.NoError(t, err)

	added := mesh.attribute(attID)
	for pointID, wantEntry := range []uint32{0, 1, 1, 2} {
		got := added.mappedIndex(pointID)
		require.Equal(t, wantEntry, got)
		requireFloat32Entry(t, added, int(got), []float32{float32(wantEntry)}, 0)
	}
}

func TestMeshAddAttributeWithConnectivity(t *testing.T) {
	mesh := mustNewMesh(4)
	position := mustNewFloat32Attribute(AttributePosition, 3, 4)
	for i, xyz := range [][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {1, 1, 0}} {
		setFloat32Value(t, position, i, xyz...)
	}

	addMeshAttribute(t, mesh, position)

	baseAttr, err := NewAttribute(AttributeGeneric, DataTypeUint8, 1, 1)
	require.NoError(t, err)
	setRawValue(t, baseAttr, 0, []byte{10})
	require.NoError(t, baseAttr.SetExplicitMapping(4))
	for pointID := 0; pointID < 4; pointID++ {
		require.NoError(t, baseAttr.SetPointMapEntry(pointID, 0))
	}

	baseID, err := mesh.AddAttribute(baseAttr)
	require.NoError(t, err)

	addFace(t, mesh, Face{0, 1, 2})
	addFace(t, mesh, Face{2, 1, 3})

	attr, err := NewAttribute(AttributeGeneric, DataTypeUint8, 1, 2)
	require.NoError(t, err)
	setRawValue(t, attr, 0, []byte{11})
	setRawValue(t, attr, 1, []byte{12})

	attID, err := mesh.AddAttributeWithConnectivity(attr, []uint32{0, 1, 0, 0, 0, 0})
	require.NoError(t, err)
	require.Equal(t, 5, mesh.PointCount())
	require.Equal(t, uint32(1), mesh.CornerToPointID(1))
	require.Equal(t, uint32(4), mesh.CornerToPointID(4))

	added := mesh.attribute(attID)
	requireInt32Entry(t, added, int(added.mappedIndex(int(mesh.CornerToPointID(1)))), []int32{12})
	requireInt32Entry(t, added, int(added.mappedIndex(int(mesh.CornerToPointID(4)))), []int32{11})

	base := mesh.attribute(baseID)
	requireInt32Entry(t, base, int(base.mappedIndex(int(mesh.CornerToPointID(4)))), []int32{10})
}

func TestMeshAddAttributeWithConnectivityKeepsIsolatedPointsMapped(t *testing.T) {
	mesh := mustNewMesh(5)
	position := mustNewFloat32Attribute(AttributePosition, 3, 5)
	for i, xyz := range [][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {1, 1, 0}, {2, 2, 0}} {
		setFloat32Value(t, position, i, xyz...)
	}

	addMeshAttribute(t, mesh, position)
	addFace(t, mesh, Face{0, 1, 2})
	addFace(t, mesh, Face{2, 1, 3})

	attr, err := NewAttribute(AttributeGeneric, DataTypeUint8, 1, 2)
	require.NoError(t, err)
	setRawValue(t, attr, 0, []byte{11})
	setRawValue(t, attr, 1, []byte{12})
	_, err = mesh.AddAttributeWithConnectivity(attr, []uint32{0, 0, 0, 1, 1, 1})
	require.NoError(t, err)

	added := mesh.attribute(mesh.AttributeCount() - 1)
	require.Less(t, added.mappedIndex(4), uint32(added.EntryCount()))
}

func TestMeshRemoveIsolatedPoints(t *testing.T) {
	mesh := mustNewMesh(5)
	position := mustNewFloat32Attribute(AttributePosition, 3, 5)
	for i, xyz := range [][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {1, 1, 0}, {2, 2, 0}} {
		setFloat32Value(t, position, i, xyz...)
	}

	addMeshAttribute(t, mesh, position)
	addFace(t, mesh, Face{0, 1, 2})
	addFace(t, mesh, Face{2, 1, 3})

	require.NoError(t, mesh.RemoveIsolatedPoints())
	require.Equal(t, 4, mesh.PointCount())
}

func TestMeshTopologyHelpersBoundaryQuad(t *testing.T) {
	mesh := mustNewMesh(4)
	pos := mustNewFloat32Attribute(AttributePosition, 3, 4)
	for i, xyz := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}} {
		setFloat32Value(t, pos, i, xyz[:]...)
	}

	addMeshAttribute(t, mesh, pos)
	addFace(t, mesh, Face{0, 1, 2})
	addFace(t, mesh, Face{0, 2, 3})

	valence, err := mesh.VertexValence(0)
	require.NoError(t, err)
	require.Equal(t, 3, valence)

	boundary, err := mesh.IsBoundaryVertex(0)
	require.NoError(t, err)
	require.True(t, boundary)

	neighbors, err := mesh.FaceNeighbors(0)
	require.NoError(t, err)
	require.Equal(t, [3]int{-1, 1, -1}, neighbors)
}

func TestMeshTopologyHelpersClosedMesh(t *testing.T) {
	mesh := mustNewMesh(4)
	pos := mustNewFloat32Attribute(AttributePosition, 3, 4)
	for i, xyz := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {0, 0, 1}} {
		setFloat32Value(t, pos, i, xyz[:]...)
	}

	addMeshAttribute(t, mesh, pos)
	for _, face := range []Face{{0, 2, 1}, {0, 1, 3}, {1, 2, 3}, {0, 3, 2}} {
		addFace(t, mesh, face)
	}

	for vertex := 0; vertex < 4; vertex++ {
		valence, err := mesh.VertexValence(vertex)
		require.NoError(t, err)
		require.Equal(t, 3, valence)

		boundary, err := mesh.IsBoundaryVertex(vertex)
		require.NoError(t, err)
		require.False(t, boundary)
	}
}

func TestMeshTopologyRejectsDegenerateFace(t *testing.T) {
	mesh := mustNewMesh(3)
	pos := mustNewFloat32Attribute(AttributePosition, 3, 3)
	for i, xyz := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}} {
		setFloat32Value(t, pos, i, xyz[:]...)
	}

	addMeshAttribute(t, mesh, pos)
	addFace(t, mesh, Face{0, 1, 1})

	_, err := mesh.FaceNeighbors(0)
	require.Error(t, err)
}

func TestMeshHelpers(t *testing.T) {
	mesh := mustNewMesh(4)
	pos := mustNewFloat32Attribute(AttributePosition, 3, 4)
	for i, xyz := range [][]float32{{0, 0, 0}, {1, 2, 3}, {2, 1, -1}, {-3, 5, 4}} {
		setFloat32Value(t, pos, i, xyz...)
	}

	addMeshAttribute(t, mesh, pos)
	addFace(t, mesh, Face{0, 1, 2})
	addFace(t, mesh, Face{2, 2, 3})

	require.True(t, mesh.HasDegenerateFaces())
	require.Equal(t, 1, mesh.DegenerateFaceCount())

	minBounds, maxBounds, err := mesh.PositionBounds()
	require.NoError(t, err)
	require.Equal(t, [3]float32{-3, 0, -1}, minBounds)
	require.Equal(t, [3]float32{2, 5, 4}, maxBounds)

	copyMesh := mustNewMesh(4)
	addMeshAttribute(t, copyMesh, pos.Clone())
	addFace(t, copyMesh, Face{0, 1, 2})
	addFace(t, copyMesh, Face{2, 2, 3})
	requireMeshEquivalent(t, mesh, copyMesh)
}

func TestMeshEquivalentIgnoresFaceWinding(t *testing.T) {
	mesh := newMeshFromData(t, [][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}, []Face{{0, 1, 2}})
	reversed := newMeshFromData(t, [][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}, []Face{{0, 2, 1}})
	requireMeshEquivalent(t, mesh, reversed)
}

func TestEncodeMeshRejectsInvalidGeometry(t *testing.T) {
	mesh := newMeshFromData(t, [][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}, []Face{{0, 1, 2}})
	mesh.setPointCount(2)

	_, err := Encode(testContext(t), mesh)
	require.ErrorIs(t, err, ErrInvalidGeometry)
}
