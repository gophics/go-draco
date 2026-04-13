package draco

import "fmt"

func (m *Mesh) AddPropertyAttributesIndex(propertyAttributeIndex int) (int, error) {
	if m == nil {
		return -1, fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if m.structuralMetadata == nil {
		return -1, fmt.Errorf("%w: property attribute indices require structural metadata", ErrInvalidGeometry)
	}

	if propertyAttributeIndex < 0 || propertyAttributeIndex >= m.structuralMetadata.PropertyAttributeCount() {
		return -1, fmt.Errorf("%w: property attribute index %d out of range for %d property attributes", ErrInvalidGeometry, propertyAttributeIndex, m.structuralMetadata.PropertyAttributeCount())
	}

	m.propertyAttributeIndices = append(m.propertyAttributeIndices, propertyAttributeIndex)
	m.propertyAttributeMaterialMask = append(m.propertyAttributeMaterialMask, nil)
	return len(m.propertyAttributeIndices) - 1, nil
}

func (m *Mesh) PropertyAttributeIndexCount() int {
	if m == nil {
		return 0
	}

	return len(m.propertyAttributeIndices)
}

func (m *Mesh) PropertyAttributeIndex(index int) (int, error) {
	if m == nil {
		return 0, fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if index < 0 || index >= len(m.propertyAttributeIndices) {
		return 0, fmt.Errorf("%w: property attribute index %d out of range", ErrInvalidGeometry, index)
	}

	return m.propertyAttributeIndices[index], nil
}

func (m *Mesh) RemovePropertyAttributesIndex(index int) error {
	if m == nil {
		return fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if index < 0 || index >= len(m.propertyAttributeIndices) {
		return fmt.Errorf("%w: property attribute index %d out of range", ErrInvalidGeometry, index)
	}

	m.propertyAttributeIndices = append(m.propertyAttributeIndices[:index], m.propertyAttributeIndices[index+1:]...)
	m.propertyAttributeMaterialMask = append(m.propertyAttributeMaterialMask[:index], m.propertyAttributeMaterialMask[index+1:]...)
	return nil
}

func (m *Mesh) AddPropertyAttributesIndexMaterialMask(index, materialIndex int) error {
	if m == nil {
		return fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if index < 0 || index >= len(m.propertyAttributeMaterialMask) {
		return fmt.Errorf("%w: property attribute material mask index %d out of range", ErrInvalidGeometry, index)
	}

	if materialIndex < 0 || materialIndex >= m.materials.MaterialCount() {
		return fmt.Errorf("%w: property attribute %d material index %d out of range for %d materials", ErrInvalidGeometry, index, materialIndex, m.materials.MaterialCount())
	}

	m.propertyAttributeMaterialMask[index] = append(m.propertyAttributeMaterialMask[index], materialIndex)
	return nil
}

func (m *Mesh) PropertyAttributeIndexMaterialMaskCount(index int) (int, error) {
	if m == nil {
		return 0, fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if index < 0 || index >= len(m.propertyAttributeMaterialMask) {
		return 0, fmt.Errorf("%w: property attribute material mask index %d out of range", ErrInvalidGeometry, index)
	}

	return len(m.propertyAttributeMaterialMask[index]), nil
}

func (m *Mesh) PropertyAttributeIndexMaterialMask(index, maskIndex int) (int, error) {
	if m == nil {
		return 0, fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if index < 0 || index >= len(m.propertyAttributeMaterialMask) {
		return 0, fmt.Errorf("%w: property attribute material mask index %d out of range", ErrInvalidGeometry, index)
	}

	if maskIndex < 0 || maskIndex >= len(m.propertyAttributeMaterialMask[index]) {
		return 0, fmt.Errorf("%w: property attribute material mask value %d out of range", ErrInvalidGeometry, maskIndex)
	}

	return m.propertyAttributeMaterialMask[index][maskIndex], nil
}

func cloneIntMatrix(values [][]int) [][]int {
	if values == nil {
		return nil
	}

	out := make([][]int, len(values))
	for i := range values {
		out[i] = append([]int(nil), values[i]...)
	}

	return out
}
