package draco

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTextureLibraryRejectsInvalidOperations(t *testing.T) {
	var textures TextureLibrary

	_, err := textures.AddTexture(nil)
	require.ErrorIs(t, err, ErrInvalidGeometry)

	index, err := textures.AddTexture(&Texture{Name: "base"})
	require.NoError(t, err)
	require.Equal(t, 0, index)

	texture, err := textures.Texture(0)
	require.NoError(t, err)
	require.Equal(t, "base", texture.Name)

	_, err = textures.Texture(1)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	require.ErrorIs(t, textures.RemoveTexture(1), ErrInvalidGeometry)
	require.NoError(t, textures.RemoveTexture(0))
}

func TestMaterialLibraryRejectsInvalidOperations(t *testing.T) {
	var materials MaterialLibrary

	_, err := materials.AddMaterial(nil)
	require.ErrorIs(t, err, ErrInvalidGeometry)

	material := NewMaterial()
	material.Name = "default"
	index, err := materials.AddMaterial(material)
	require.NoError(t, err)
	require.Equal(t, 0, index)

	got, err := materials.Material(0)
	require.NoError(t, err)
	require.Equal(t, "default", got.Name)

	_, err = materials.AddVariant("")
	require.ErrorIs(t, err, ErrInvalidGeometry)

	variantIndex, err := materials.AddVariant("draft")
	require.NoError(t, err)
	require.Equal(t, 0, variantIndex)

	name, err := materials.VariantName(0)
	require.NoError(t, err)
	require.Equal(t, "draft", name)

	_, err = materials.VariantName(1)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	require.ErrorIs(t, materials.RemoveMaterial(1), ErrInvalidGeometry)
	require.NoError(t, materials.RemoveMaterial(0))
}

func TestMaterialSetTextureMapRejectsNilReceiver(t *testing.T) {
	var material *Material

	err := material.SetTextureMap(NewTextureMap())
	require.ErrorIs(t, err, ErrInvalidGeometry)
}

func TestMaterialAndTextureLibrariesCloneIngressAndAccessors(t *testing.T) {
	textures := TextureLibrary{}
	textureIndex, err := textures.AddTexture(&Texture{
		Name:     "base",
		URI:      "base.png",
		MIMEType: "image/png",
		Data:     []byte{1, 2, 3},
	})
	require.NoError(t, err)

	textureClone, err := textures.Texture(textureIndex)
	require.NoError(t, err)
	textureClone.Name = "mutated"
	textureClone.Data[0] = 9
	require.Equal(t, "base", textures.textures[0].Name)
	require.Equal(t, []byte{1, 2, 3}, textures.textures[0].Data)

	textures.textures[0].Name = "ingress-mutated"
	texturesCopy := textures.Clone()
	require.NotNil(t, texturesCopy.textures[0])
	texturesCopy.textures[0].Name = "changed"
	require.Equal(t, "ingress-mutated", textures.textures[0].Name)

	materials := MaterialLibrary{}
	material := NewMaterial()
	material.Name = "default"
	material.TextureMaps = []TextureMap{NewTextureMap()}
	_, err = materials.AddMaterial(material)
	require.NoError(t, err)
	material.Name = "mutated"

	materialClone, err := materials.Material(0)
	require.NoError(t, err)
	materialClone.Name = "changed"
	materialClone.TextureMaps[0].TexCoordIndex = 99
	require.Equal(t, "default", materials.materials[0].Name)
	require.Equal(t, -1, materials.materials[0].TextureMaps[0].TexCoordIndex)

	materials.materials[0].Name = "ingress-mutated"
	materialsCopy := materials.Clone()
	require.NotNil(t, materialsCopy.materials[0])
	materialsCopy.materials[0].Name = "changed"
	require.Equal(t, "ingress-mutated", materials.materials[0].Name)
}

func TestMaterialLibraryNilReceiverGuards(t *testing.T) {
	var materials *MaterialLibrary
	_, err := materials.AddMaterial(NewMaterial())
	require.ErrorIs(t, err, ErrInvalidGeometry)
	_, err = materials.AddVariant("draft")
	require.ErrorIs(t, err, ErrInvalidGeometry)
	err = materials.SetTextureLibrary(TextureLibrary{})
	require.ErrorIs(t, err, ErrInvalidGeometry)
}

func TestDefaultTextureTransformUsesIdentityScale(t *testing.T) {
	transform := DefaultTextureTransform()
	require.Equal(t, [2]float64{1, 1}, transform.Scale)

	textureMap := NewTextureMap()
	require.Equal(t, [2]float64{1, 1}, textureMap.Transform.Scale)
}

func TestMaterialLibraryValidateRejectsInvalidTextureReferences(t *testing.T) {
	materials := MaterialLibrary{}
	textures := TextureLibrary{}
	textureIndex, err := textures.AddTexture(&Texture{Name: "base"})
	require.NoError(t, err)
	require.NoError(t, materials.SetTextureLibrary(textures))

	material := NewMaterial()
	require.NoError(t, material.SetTextureMap(TextureMap{
		Type:          TextureMapColor,
		TextureIndex:  textureIndex,
		TexCoordIndex: 0,
		Transform:     DefaultTextureTransform(),
	}))
	_, err = materials.AddMaterial(material)
	require.NoError(t, err)
	require.NoError(t, materials.Validate(1))

	duplicate := materials.Clone()
	require.NoError(t, duplicate.materials[0].SetTextureMap(TextureMap{
		Type:          TextureMapColor,
		TextureIndex:  textureIndex,
		TexCoordIndex: 0,
		Transform:     DefaultTextureTransform(),
	}))
	duplicate.materials[0].TextureMaps = append(duplicate.materials[0].TextureMaps, TextureMap{
		Type:          TextureMapColor,
		TextureIndex:  textureIndex,
		TexCoordIndex: 0,
		Transform:     DefaultTextureTransform(),
	})
	require.ErrorIs(t, duplicate.Validate(1), ErrInvalidGeometry)

	outOfRangeTexture := materials.Clone()
	outOfRangeTexture.materials[0].TextureMaps[0].TextureIndex = 7
	require.ErrorIs(t, outOfRangeTexture.Validate(1), ErrInvalidGeometry)

	outOfRangeTexCoord := materials.Clone()
	outOfRangeTexCoord.materials[0].TextureMaps[0].TexCoordIndex = 2
	require.ErrorIs(t, outOfRangeTexCoord.Validate(1), ErrInvalidGeometry)
}
