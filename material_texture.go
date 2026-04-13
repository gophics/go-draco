package draco

import (
	"bytes"
	"fmt"
	"math"
)

type Texture struct {
	Name     string
	URI      string
	MIMEType string
	Data     []byte
}

func (t *Texture) Clone() *Texture {
	if t == nil {
		return nil
	}

	out := *t
	out.Data = append([]byte(nil), t.Data...)
	return &out
}

func (t *Texture) Equal(other *Texture) bool {
	if t == nil || other == nil {
		return t == other
	}

	return t.Name == other.Name &&
		t.URI == other.URI &&
		t.MIMEType == other.MIMEType &&
		bytes.Equal(t.Data, other.Data)
}

type TextureLibrary struct {
	textures []*Texture
}

func (l *TextureLibrary) AddTexture(texture *Texture) (int, error) {
	if l == nil {
		return -1, fmt.Errorf("%w: texture library is nil", ErrInvalidGeometry)
	}

	if texture == nil {
		return -1, fmt.Errorf("%w: texture is nil", ErrInvalidGeometry)
	}

	l.textures = append(l.textures, texture.Clone())
	return len(l.textures) - 1, nil
}

func (l TextureLibrary) TextureCount() int {
	return len(l.textures)
}

func (l TextureLibrary) Texture(index int) (*Texture, error) {
	if index < 0 || index >= len(l.textures) {
		return nil, fmt.Errorf("%w: texture %d out of range", ErrInvalidGeometry, index)
	}

	return l.textures[index].Clone(), nil
}

func (l *TextureLibrary) RemoveTexture(index int) error {
	if l == nil {
		return fmt.Errorf("%w: texture library is nil", ErrInvalidGeometry)
	}

	if index < 0 || index >= len(l.textures) {
		return fmt.Errorf("%w: texture %d out of range", ErrInvalidGeometry, index)
	}

	l.textures = append(l.textures[:index], l.textures[index+1:]...)
	return nil
}

func (l TextureLibrary) Clone() TextureLibrary {
	out := TextureLibrary{textures: make([]*Texture, len(l.textures))}
	for i, texture := range l.textures {
		out.textures[i] = texture.Clone()
	}

	return out
}

func (l TextureLibrary) Equal(other TextureLibrary) bool {
	if len(l.textures) != len(other.textures) {
		return false
	}

	for i := range l.textures {
		if !l.textures[i].Equal(other.textures[i]) {
			return false
		}
	}

	return true
}

func (l TextureLibrary) Validate() error {
	for i, texture := range l.textures {
		if texture == nil {
			return fmt.Errorf("%w: texture %d is nil", ErrInvalidGeometry, i)
		}
	}

	return nil
}

type TextureMapType uint8

const (
	TextureMapGeneric TextureMapType = iota
	TextureMapColor
	TextureMapOpacity
	TextureMapMetallic
	TextureMapRoughness
	TextureMapMetallicRoughness
	TextureMapNormalObjectSpace
	TextureMapNormalTangentSpace
	TextureMapAmbientOcclusion
	TextureMapEmissive
	TextureMapSheenColor
	TextureMapSheenRoughness
	TextureMapTransmission
	TextureMapClearcoat
	TextureMapClearcoatRoughness
	TextureMapClearcoatNormal
	TextureMapThickness
	TextureMapSpecular
	TextureMapSpecularColor
)

type TextureAxisWrappingMode uint8

const (
	TextureClampToEdge TextureAxisWrappingMode = iota
	TextureMirroredRepeat
	TextureRepeat
)

type TextureWrappingMode struct {
	S TextureAxisWrappingMode
	T TextureAxisWrappingMode
}

type TextureFilterType uint8

const (
	TextureFilterUnspecified TextureFilterType = iota
	TextureFilterNearest
	TextureFilterLinear
	TextureFilterNearestMipmapNearest
	TextureFilterLinearMipmapNearest
	TextureFilterNearestMipmapLinear
	TextureFilterLinearMipmapLinear
)

type TextureTransform struct {
	Offset   [2]float64
	Rotation float64
	Scale    [2]float64
	TexCoord int
}

func DefaultTextureTransform() TextureTransform {
	return TextureTransform{
		Offset:   [2]float64{0, 0},
		Rotation: 0,
		Scale:    [2]float64{1, 1},
		TexCoord: -1,
	}
}

type TextureMap struct {
	Type          TextureMapType
	Wrapping      TextureWrappingMode
	TexCoordIndex int
	MinFilter     TextureFilterType
	MagFilter     TextureFilterType
	TextureIndex  int
	Transform     TextureTransform
}

func NewTextureMap() TextureMap {
	return TextureMap{
		Type:          TextureMapGeneric,
		Wrapping:      TextureWrappingMode{S: TextureClampToEdge, T: TextureClampToEdge},
		TexCoordIndex: -1,
		MinFilter:     TextureFilterUnspecified,
		MagFilter:     TextureFilterUnspecified,
		TextureIndex:  -1,
		Transform:     DefaultTextureTransform(),
	}
}

func (m TextureMap) Equal(other TextureMap) bool {
	return m == other
}

type MaterialTransparencyMode uint8

const (
	TransparencyOpaque MaterialTransparencyMode = iota
	TransparencyMask
	TransparencyBlend
)

type Material struct {
	Name                     string
	ColorFactor              [4]float32
	MetallicFactor           float32
	RoughnessFactor          float32
	EmissiveFactor           [3]float32
	DoubleSided              bool
	TransparencyMode         MaterialTransparencyMode
	AlphaCutoff              float32
	NormalTextureScale       float32
	Unlit                    bool
	HasSheen                 bool
	SheenColorFactor         [3]float32
	SheenRoughnessFactor     float32
	HasTransmission          bool
	TransmissionFactor       float32
	HasClearcoat             bool
	ClearcoatFactor          float32
	ClearcoatRoughnessFactor float32
	HasVolume                bool
	ThicknessFactor          float32
	AttenuationDistance      float32
	AttenuationColor         [3]float32
	HasIor                   bool
	Ior                      float32
	HasSpecular              bool
	SpecularFactor           float32
	SpecularColorFactor      [3]float32
	TextureMaps              []TextureMap
}

func NewMaterial() *Material {
	return &Material{
		ColorFactor:         [4]float32{1, 1, 1, 1},
		MetallicFactor:      1,
		RoughnessFactor:     1,
		TransparencyMode:    TransparencyOpaque,
		AlphaCutoff:         0.5,
		NormalTextureScale:  1,
		AttenuationDistance: math.MaxFloat32,
		AttenuationColor:    [3]float32{1, 1, 1},
		Ior:                 1.5,
		SpecularFactor:      1,
		SpecularColorFactor: [3]float32{1, 1, 1},
	}
}

func (m *Material) Clone() *Material {
	if m == nil {
		return nil
	}

	out := *m
	out.TextureMaps = append([]TextureMap(nil), m.TextureMaps...)
	return &out
}

func (m *Material) Equal(other *Material) bool {
	if m == nil || other == nil {
		return m == other
	}

	if m.Name != other.Name ||
		m.ColorFactor != other.ColorFactor ||
		m.MetallicFactor != other.MetallicFactor ||
		m.RoughnessFactor != other.RoughnessFactor ||
		m.EmissiveFactor != other.EmissiveFactor ||
		m.DoubleSided != other.DoubleSided ||
		m.TransparencyMode != other.TransparencyMode ||
		m.AlphaCutoff != other.AlphaCutoff ||
		m.NormalTextureScale != other.NormalTextureScale ||
		m.Unlit != other.Unlit ||
		m.HasSheen != other.HasSheen ||
		m.SheenColorFactor != other.SheenColorFactor ||
		m.SheenRoughnessFactor != other.SheenRoughnessFactor ||
		m.HasTransmission != other.HasTransmission ||
		m.TransmissionFactor != other.TransmissionFactor ||
		m.HasClearcoat != other.HasClearcoat ||
		m.ClearcoatFactor != other.ClearcoatFactor ||
		m.ClearcoatRoughnessFactor != other.ClearcoatRoughnessFactor ||
		m.HasVolume != other.HasVolume ||
		m.ThicknessFactor != other.ThicknessFactor ||
		m.AttenuationDistance != other.AttenuationDistance ||
		m.AttenuationColor != other.AttenuationColor ||
		m.HasIor != other.HasIor ||
		m.Ior != other.Ior ||
		m.HasSpecular != other.HasSpecular ||
		m.SpecularFactor != other.SpecularFactor ||
		m.SpecularColorFactor != other.SpecularColorFactor {
		return false
	}

	if len(m.TextureMaps) != len(other.TextureMaps) {
		return false
	}

	for i := range m.TextureMaps {
		if !m.TextureMaps[i].Equal(other.TextureMaps[i]) {
			return false
		}
	}

	return true
}

func (m *Material) SetTextureMap(textureMap TextureMap) error {
	if m == nil {
		return fmt.Errorf("%w: material is nil", ErrInvalidGeometry)
	}

	for i := range m.TextureMaps {
		if m.TextureMaps[i].Type == textureMap.Type {
			m.TextureMaps[i] = textureMap
			return nil
		}
	}

	m.TextureMaps = append(m.TextureMaps, textureMap)
	return nil
}

type MaterialLibrary struct {
	materials      []*Material
	variantNames   []string
	textureLibrary TextureLibrary
}

func (l *MaterialLibrary) AddMaterial(material *Material) (int, error) {
	if l == nil {
		return -1, fmt.Errorf("%w: material library is nil", ErrInvalidGeometry)
	}

	if material == nil {
		return -1, fmt.Errorf("%w: material is nil", ErrInvalidGeometry)
	}

	l.materials = append(l.materials, material.Clone())
	return len(l.materials) - 1, nil
}

func (l MaterialLibrary) MaterialCount() int {
	return len(l.materials)
}

func (l MaterialLibrary) TextureLibraryClone() TextureLibrary {
	return l.textureLibrary.Clone()
}

func (l *MaterialLibrary) SetTextureLibrary(textures TextureLibrary) error {
	if l == nil {
		return fmt.Errorf("%w: material library is nil", ErrInvalidGeometry)
	}

	clone := textures.Clone()
	if err := clone.Validate(); err != nil {
		return err
	}

	l.textureLibrary = clone
	return nil
}

func (l MaterialLibrary) Material(index int) (*Material, error) {
	if index < 0 || index >= len(l.materials) {
		return nil, fmt.Errorf("%w: material %d out of range", ErrInvalidGeometry, index)
	}

	return l.materials[index].Clone(), nil
}

func (l *MaterialLibrary) RemoveMaterial(index int) error {
	if l == nil {
		return fmt.Errorf("%w: material library is nil", ErrInvalidGeometry)
	}

	if index < 0 || index >= len(l.materials) {
		return fmt.Errorf("%w: material %d out of range", ErrInvalidGeometry, index)
	}

	l.materials = append(l.materials[:index], l.materials[index+1:]...)
	return nil
}

func (l *MaterialLibrary) AddVariant(name string) (int, error) {
	if l == nil {
		return -1, fmt.Errorf("%w: material library is nil", ErrInvalidGeometry)
	}

	if name == "" {
		return -1, fmt.Errorf("%w: variant name is empty", ErrInvalidGeometry)
	}

	l.variantNames = append(l.variantNames, name)
	return len(l.variantNames) - 1, nil
}

func (l MaterialLibrary) VariantCount() int {
	return len(l.variantNames)
}

func (l MaterialLibrary) VariantName(index int) (string, error) {
	if index < 0 || index >= len(l.variantNames) {
		return "", fmt.Errorf("%w: material variant %d out of range", ErrInvalidGeometry, index)
	}

	return l.variantNames[index], nil
}

func (l MaterialLibrary) Clone() MaterialLibrary {
	out := MaterialLibrary{
		materials:      make([]*Material, len(l.materials)),
		variantNames:   append([]string(nil), l.variantNames...),
		textureLibrary: l.textureLibrary.Clone(),
	}
	for i, material := range l.materials {
		out.materials[i] = material.Clone()
	}

	return out
}

func (l MaterialLibrary) Equal(other MaterialLibrary) bool {
	if len(l.materials) != len(other.materials) ||
		len(l.variantNames) != len(other.variantNames) ||
		!l.textureLibrary.Equal(other.textureLibrary) {
		return false
	}

	for i := range l.variantNames {
		if l.variantNames[i] != other.variantNames[i] {
			return false
		}
	}

	for i := range l.materials {
		if !l.materials[i].Equal(other.materials[i]) {
			return false
		}
	}

	return true
}

func (l MaterialLibrary) Validate(texCoordCount int) error {
	if err := l.textureLibrary.Validate(); err != nil {
		return err
	}

	for materialIndex, material := range l.materials {
		if material == nil {
			return fmt.Errorf("%w: material %d is nil", ErrInvalidGeometry, materialIndex)
		}

		seenTextureTypes := make(map[TextureMapType]struct{}, len(material.TextureMaps))
		for mapIndex, textureMap := range material.TextureMaps {
			if _, ok := seenTextureTypes[textureMap.Type]; ok {
				return fmt.Errorf("%w: material %d texture map type %d duplicated", ErrInvalidGeometry, materialIndex, textureMap.Type)
			}

			seenTextureTypes[textureMap.Type] = struct{}{}
			if textureMap.TextureIndex < 0 || textureMap.TextureIndex >= l.textureLibrary.TextureCount() {
				return fmt.Errorf("%w: material %d texture map %d references texture %d out of range for %d textures", ErrInvalidGeometry, materialIndex, mapIndex, textureMap.TextureIndex, l.textureLibrary.TextureCount())
			}

			if textureMap.TexCoordIndex < 0 || textureMap.TexCoordIndex >= texCoordCount {
				return fmt.Errorf("%w: material %d texture map %d texcoord index %d out of range for %d TEX_COORD attributes", ErrInvalidGeometry, materialIndex, mapIndex, textureMap.TexCoordIndex, texCoordCount)
			}
		}
	}

	return nil
}
