package draco

import "fmt"

type MeshFeatures struct {
	Label              string
	FeatureCount       int64
	NullFeatureID      int64
	AttributeIndex     int
	PropertyTableIndex int
	TextureMap         *TextureMap
	TextureChannels    []int
}

func NewMeshFeatures() *MeshFeatures {
	return &MeshFeatures{
		NullFeatureID:      -1,
		AttributeIndex:     -1,
		PropertyTableIndex: -1,
	}
}

func (m *MeshFeatures) Clone() *MeshFeatures {
	if m == nil {
		return nil
	}

	out := *m
	if m.TextureMap != nil {
		textureMap := *m.TextureMap
		out.TextureMap = &textureMap
	}

	out.TextureChannels = append([]int(nil), m.TextureChannels...)
	return &out
}

func meshFeaturesEqual(a, b *MeshFeatures) bool {
	if a == nil || b == nil {
		return a == b
	}

	if a.Label != b.Label ||
		a.FeatureCount != b.FeatureCount ||
		a.NullFeatureID != b.NullFeatureID ||
		a.AttributeIndex != b.AttributeIndex ||
		a.PropertyTableIndex != b.PropertyTableIndex ||
		(a.TextureMap == nil) != (b.TextureMap == nil) ||
		len(a.TextureChannels) != len(b.TextureChannels) {
		return false
	}

	if a.TextureMap != nil && !a.TextureMap.Equal(*b.TextureMap) {
		return false
	}

	for i := range a.TextureChannels {
		if a.TextureChannels[i] != b.TextureChannels[i] {
			return false
		}
	}

	return true
}

func cloneMeshFeaturesSlice(values []*MeshFeatures) []*MeshFeatures {
	if values == nil {
		return nil
	}

	out := make([]*MeshFeatures, len(values))
	for i, value := range values {
		out[i] = value.Clone()
	}

	return out
}

func (m *Mesh) AddMeshFeatures(features *MeshFeatures) (int, error) {
	if m == nil {
		return -1, fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if features == nil {
		return -1, fmt.Errorf("%w: mesh features is nil", ErrInvalidGeometry)
	}

	m.meshFeatures = append(m.meshFeatures, features.Clone())
	m.meshFeaturesMaterialMask = append(m.meshFeaturesMaterialMask, nil)
	if err := m.Validate(); err != nil {
		m.meshFeatures = m.meshFeatures[:len(m.meshFeatures)-1]
		m.meshFeaturesMaterialMask = m.meshFeaturesMaterialMask[:len(m.meshFeaturesMaterialMask)-1]
		return -1, err
	}

	return len(m.meshFeatures) - 1, nil
}

func (m *Mesh) MeshFeatureCount() int {
	if m == nil {
		return 0
	}

	return len(m.meshFeatures)
}

func (m *Mesh) MeshFeature(index int) (*MeshFeatures, error) {
	if m == nil {
		return nil, fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if index < 0 || index >= len(m.meshFeatures) {
		return nil, fmt.Errorf("%w: mesh feature %d out of range", ErrInvalidGeometry, index)
	}

	return m.meshFeatures[index].Clone(), nil
}

func (m *Mesh) RemoveMeshFeatures(index int) error {
	if m == nil {
		return fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if index < 0 || index >= len(m.meshFeatures) {
		return fmt.Errorf("%w: mesh feature %d out of range", ErrInvalidGeometry, index)
	}

	m.meshFeatures = append(m.meshFeatures[:index], m.meshFeatures[index+1:]...)
	m.meshFeaturesMaterialMask = append(m.meshFeaturesMaterialMask[:index], m.meshFeaturesMaterialMask[index+1:]...)
	return nil
}

func (m *Mesh) AddMeshFeaturesMaterialMask(index, materialIndex int) error {
	if m == nil {
		return fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if index < 0 || index >= len(m.meshFeaturesMaterialMask) {
		return fmt.Errorf("%w: mesh feature material mask index %d out of range", ErrInvalidGeometry, index)
	}

	if materialIndex < 0 || materialIndex >= m.materials.MaterialCount() {
		return fmt.Errorf("%w: mesh feature %d material index %d out of range for %d materials", ErrInvalidGeometry, index, materialIndex, m.materials.MaterialCount())
	}

	m.meshFeaturesMaterialMask[index] = append(m.meshFeaturesMaterialMask[index], materialIndex)
	return nil
}

func (m *Mesh) MeshFeatureMaterialMaskCount(index int) (int, error) {
	if m == nil {
		return 0, fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if index < 0 || index >= len(m.meshFeaturesMaterialMask) {
		return 0, fmt.Errorf("%w: mesh feature material mask index %d out of range", ErrInvalidGeometry, index)
	}

	return len(m.meshFeaturesMaterialMask[index]), nil
}

func (m *Mesh) MeshFeatureMaterialMask(index, maskIndex int) (int, error) {
	if m == nil {
		return 0, fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if index < 0 || index >= len(m.meshFeaturesMaterialMask) {
		return 0, fmt.Errorf("%w: mesh feature material mask index %d out of range", ErrInvalidGeometry, index)
	}

	if maskIndex < 0 || maskIndex >= len(m.meshFeaturesMaterialMask[index]) {
		return 0, fmt.Errorf("%w: mesh feature material mask value %d out of range", ErrInvalidGeometry, maskIndex)
	}

	return m.meshFeaturesMaterialMask[index][maskIndex], nil
}

func (m *Mesh) IsAttributeUsedByMeshFeatures(attID int) bool {
	if m == nil {
		return false
	}

	for _, feature := range m.meshFeatures {
		if feature != nil && feature.AttributeIndex == attID {
			return true
		}
	}

	return false
}
