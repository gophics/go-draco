package draco

import (
	"fmt"

	"github.com/gophics/go-draco/internal/topology"
	md "github.com/gophics/go-draco/metadata"
)

type Mesh struct {
	PointCloud
	Name                          string
	materials                     MaterialLibrary
	nonMaterialTextures           TextureLibrary
	faces                         []Face
	meshFeatures                  []*MeshFeatures
	meshFeaturesMaterialMask      [][]int
	structuralMetadata            *md.StructuralMetadata
	propertyAttributeIndices      []int
	propertyAttributeMaterialMask [][]int
}

// NewMesh constructs a validating mesh from a point count, faces, and attributes.
func NewMesh(numPoints int, faces []Face, attrs ...*Attribute) (*Mesh, error) {
	mesh := newMesh(numPoints)
	for _, face := range faces {
		if err := mesh.AddFace(face); err != nil {
			return nil, err
		}
	}

	for _, attr := range attrs {
		if _, err := mesh.AddAttribute(attr); err != nil {
			return nil, err
		}
	}

	if err := mesh.Validate(); err != nil {
		return nil, err
	}

	return mesh, nil
}

func newMesh(numPoints int) *Mesh {
	return &Mesh{PointCloud: *newPointCloud(numPoints)}
}

func (m *Mesh) GeometryType() EncodedGeometryType { return MeshGeometry }

func (m *Mesh) Faces() []Face {
	if m == nil {
		return nil
	}

	out := make([]Face, len(m.faces))
	copy(out, m.faces)
	return out
}

func (m *Mesh) FaceCount() int {
	if m == nil {
		return 0
	}

	return len(m.faces)
}

func (m *Mesh) Face(i int) (Face, error) {
	if m == nil {
		return Face{}, fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if i < 0 || i >= len(m.faces) {
		return Face{}, fmt.Errorf("%w: face %d out of range", ErrInvalidGeometry, i)
	}

	return m.faces[i], nil
}

func (m *Mesh) face(i int) Face { return m.faces[i] }

func (m *Mesh) MaterialsClone() MaterialLibrary {
	if m == nil {
		return MaterialLibrary{}
	}

	return m.materials.Clone()
}

func (m *Mesh) SetMaterials(materials MaterialLibrary) error {
	if m == nil {
		return fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	previous := m.materials
	m.materials = materials.Clone()
	if err := m.Validate(); err != nil {
		m.materials = previous
		return err
	}

	return nil
}

func (m *Mesh) NonMaterialTexturesClone() TextureLibrary {
	if m == nil {
		return TextureLibrary{}
	}

	return m.nonMaterialTextures.Clone()
}

func (m *Mesh) SetNonMaterialTextures(textures TextureLibrary) error {
	if m == nil {
		return fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	previous := m.nonMaterialTextures
	m.nonMaterialTextures = textures.Clone()
	if err := m.Validate(); err != nil {
		m.nonMaterialTextures = previous
		return err
	}

	return nil
}

func (m *Mesh) StructuralMetadataClone() *md.StructuralMetadata {
	if m == nil {
		return nil
	}

	return m.structuralMetadata.Clone()
}

func (m *Mesh) SetStructuralMetadata(metadata *md.StructuralMetadata) error {
	if m == nil {
		return fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	previous := m.structuralMetadata
	m.structuralMetadata = metadata.Clone()
	if m.structuralMetadata != nil {
		if err := m.structuralMetadata.Validate(); err != nil {
			m.structuralMetadata = previous
			return err
		}
	}

	if err := m.Validate(); err != nil {
		m.structuralMetadata = previous
		return err
	}

	return nil
}

func (m *Mesh) AddFace(face Face) error {
	if m == nil {
		return fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	for _, idx := range face {
		if int(idx) >= m.PointCount() {
			return fmt.Errorf("%w: face index %d out of range for %d points", ErrInvalidGeometry, idx, m.PointCount())
		}
	}

	m.faces = append(m.faces, face)
	return nil
}

func IsDegenerateFace(face Face) bool {
	return face[0] == face[1] || face[1] == face[2] || face[0] == face[2]
}

func (m *Mesh) DegenerateFaceCount() int {
	if m == nil {
		return 0
	}

	count := 0
	for _, face := range m.faces {
		if IsDegenerateFace(face) {
			count++
		}
	}

	return count
}

func (m *Mesh) HasDegenerateFaces() bool {
	return m.DegenerateFaceCount() > 0
}

func (m *Mesh) Validate() error {
	if m == nil {
		return fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if err := m.PointCloud.Validate(); err != nil {
		return err
	}

	for faceID, face := range m.faces {
		for corner, idx := range face {
			if int(idx) >= m.PointCount() {
				return fmt.Errorf("%w: face %d corner %d index %d out of range for %d points", ErrInvalidGeometry, faceID, corner, idx, m.PointCount())
			}
		}
	}

	texCoordCount := m.NamedAttributeCount(AttributeTexCoord)
	if err := m.materials.Validate(texCoordCount); err != nil {
		return err
	}

	if err := m.nonMaterialTextures.Validate(); err != nil {
		return err
	}

	if m.structuralMetadata != nil {
		if err := m.structuralMetadata.Validate(); err != nil {
			return err
		}

		for index, propertyAttributeIndex := range m.propertyAttributeIndices {
			if propertyAttributeIndex < 0 || propertyAttributeIndex >= m.structuralMetadata.PropertyAttributeCount() {
				return fmt.Errorf("%w: property attribute index %d at slot %d out of range for %d property attributes", ErrInvalidGeometry, propertyAttributeIndex, index, m.structuralMetadata.PropertyAttributeCount())
			}
		}
	} else if len(m.propertyAttributeIndices) > 0 {
		return fmt.Errorf("%w: property attribute indices require structural metadata", ErrInvalidGeometry)
	}

	if len(m.propertyAttributeMaterialMask) != len(m.propertyAttributeIndices) {
		return fmt.Errorf("%w: property attribute material masks (%d) do not match property attribute indices (%d)", ErrInvalidGeometry, len(m.propertyAttributeMaterialMask), len(m.propertyAttributeIndices))
	}

	for index, materialMask := range m.propertyAttributeMaterialMask {
		for maskIndex, materialIndex := range materialMask {
			if materialIndex < 0 || materialIndex >= m.materials.MaterialCount() {
				return fmt.Errorf("%w: property attribute %d material mask %d references material %d out of range for %d materials", ErrInvalidGeometry, index, maskIndex, materialIndex, m.materials.MaterialCount())
			}
		}
	}

	if len(m.meshFeaturesMaterialMask) != len(m.meshFeatures) {
		return fmt.Errorf("%w: mesh feature material masks (%d) do not match mesh features (%d)", ErrInvalidGeometry, len(m.meshFeaturesMaterialMask), len(m.meshFeatures))
	}

	for index, feature := range m.meshFeatures {
		if feature == nil {
			continue
		}

		if feature.AttributeIndex < -1 {
			return fmt.Errorf("%w: mesh feature %d has invalid attribute index %d", ErrInvalidGeometry, index, feature.AttributeIndex)
		}

		if feature.AttributeIndex >= m.AttributeCount() {
			return fmt.Errorf("%w: mesh feature %d attribute index %d out of range for %d attributes", ErrInvalidGeometry, index, feature.AttributeIndex, m.AttributeCount())
		}

		if feature.PropertyTableIndex < -1 {
			return fmt.Errorf("%w: mesh feature %d has invalid property table index %d", ErrInvalidGeometry, index, feature.PropertyTableIndex)
		}

		if feature.PropertyTableIndex >= 0 {
			if m.structuralMetadata == nil || feature.PropertyTableIndex >= m.structuralMetadata.PropertyTableCount() {
				count := 0
				if m.structuralMetadata != nil {
					count = m.structuralMetadata.PropertyTableCount()
				}

				return fmt.Errorf("%w: mesh feature %d property table index %d out of range for %d property tables", ErrInvalidGeometry, index, feature.PropertyTableIndex, count)
			}
		}

		if feature.TextureMap != nil {
			if feature.AttributeIndex >= 0 {
				return fmt.Errorf("%w: mesh feature %d cannot reference both an attribute and a texture map", ErrInvalidGeometry, index)
			}

			if feature.TextureMap.TextureIndex < 0 || feature.TextureMap.TextureIndex >= m.nonMaterialTextures.TextureCount() {
				return fmt.Errorf("%w: mesh feature %d texture index %d out of range for %d non-material textures", ErrInvalidGeometry, index, feature.TextureMap.TextureIndex, m.nonMaterialTextures.TextureCount())
			}

			if feature.TextureMap.TexCoordIndex < 0 || feature.TextureMap.TexCoordIndex >= texCoordCount {
				return fmt.Errorf("%w: mesh feature %d texcoord index %d out of range for %d TEX_COORD attributes", ErrInvalidGeometry, index, feature.TextureMap.TexCoordIndex, texCoordCount)
			}
		} else if len(feature.TextureChannels) > 0 {
			return fmt.Errorf("%w: mesh feature %d texture channels require a texture map", ErrInvalidGeometry, index)
		}

		for channelIndex, channel := range feature.TextureChannels {
			if channel < 0 || channel > 3 {
				return fmt.Errorf("%w: mesh feature %d texture channel %d has invalid channel %d", ErrInvalidGeometry, index, channelIndex, channel)
			}
		}
	}

	for index, materialMask := range m.meshFeaturesMaterialMask {
		for maskIndex, materialIndex := range materialMask {
			if materialIndex < 0 || materialIndex >= m.materials.MaterialCount() {
				return fmt.Errorf("%w: mesh feature %d material mask %d references material %d out of range for %d materials", ErrInvalidGeometry, index, maskIndex, materialIndex, m.materials.MaterialCount())
			}
		}
	}

	return nil
}

func (m *Mesh) Equivalent(other *Mesh) bool {
	if m == nil || other == nil {
		return m == other
	}

	return meshEquivalent(m, other)
}

func (m *Mesh) PositionBounds() ([3]float32, [3]float32, error) {
	var minBounds [3]float32
	var maxBounds [3]float32
	if m == nil {
		return minBounds, maxBounds, fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	position := m.namedAttribute(AttributePosition)
	if position == nil {
		return minBounds, maxBounds, fmt.Errorf("%w: mesh position attribute missing", ErrInvalidGeometry)
	}

	if m.PointCount() == 0 {
		return minBounds, maxBounds, fmt.Errorf("%w: mesh has no points", ErrInvalidGeometry)
	}

	first, err := position.Float32(int(position.mappedIndex(0)))
	if err != nil {
		return minBounds, maxBounds, err
	}

	if len(first) < 3 {
		return minBounds, maxBounds, fmt.Errorf("%w: mesh position attribute requires 3 components", ErrInvalidGeometry)
	}

	copy(minBounds[:], first[:3])
	copy(maxBounds[:], first[:3])
	for pointID := 1; pointID < m.PointCount(); pointID++ {
		value, err := position.Float32(int(position.mappedIndex(pointID)))
		if err != nil {
			return minBounds, maxBounds, err
		}

		if len(value) < 3 {
			return minBounds, maxBounds, fmt.Errorf("%w: mesh position attribute requires 3 components", ErrInvalidGeometry)
		}

		for axis := 0; axis < 3; axis++ {
			if value[axis] < minBounds[axis] {
				minBounds[axis] = value[axis]
			}

			if value[axis] > maxBounds[axis] {
				maxBounds[axis] = value[axis]
			}
		}
	}

	return minBounds, maxBounds, nil
}

func (m *Mesh) Clone() *Mesh {
	if m == nil {
		return nil
	}

	out := &Mesh{
		PointCloud:          *m.PointCloud.Clone(),
		Name:                m.Name,
		materials:           m.materials.Clone(),
		nonMaterialTextures: m.nonMaterialTextures.Clone(),
		faces:               append([]Face(nil), m.faces...),
		meshFeatures:        cloneMeshFeaturesSlice(m.meshFeatures),
		meshFeaturesMaterialMask: cloneIntMatrix(
			m.meshFeaturesMaterialMask,
		),
		structuralMetadata:       m.structuralMetadata.Clone(),
		propertyAttributeIndices: append([]int(nil), m.propertyAttributeIndices...),
		propertyAttributeMaterialMask: cloneIntMatrix(
			m.propertyAttributeMaterialMask,
		),
	}
	return out
}

func (m *Mesh) materialsRef() *MaterialLibrary {
	if m == nil {
		return nil
	}

	return &m.materials
}

func (m *Mesh) nonMaterialTexturesRef() *TextureLibrary {
	if m == nil {
		return nil
	}

	return &m.nonMaterialTextures
}

func (m *Mesh) setStructuralMetadata(metadata *md.StructuralMetadata) {
	if m == nil {
		return
	}

	m.structuralMetadata = metadata
}

func (m *Mesh) DeleteAttribute(attID int) error {
	if err := m.PointCloud.DeleteAttribute(attID); err != nil {
		return err
	}

	for _, feature := range m.meshFeatures {
		if feature == nil {
			continue
		}

		switch {
		case feature.AttributeIndex == attID:
			feature.AttributeIndex = -1
		case feature.AttributeIndex > attID:
			feature.AttributeIndex--
		}
	}

	return nil
}

func (m *Mesh) VertexValence(vertexID int) (int, error) {
	table, err := m.cornerTable()
	if err != nil {
		return 0, err
	}

	if vertexID < 0 || vertexID >= table.VertexCount() {
		return 0, fmt.Errorf("%w: vertex %d out of range", ErrInvalidGeometry, vertexID)
	}

	return table.Valence(vertexID), nil
}

func (m *Mesh) IsBoundaryVertex(vertexID int) (bool, error) {
	table, err := m.cornerTable()
	if err != nil {
		return false, err
	}

	if vertexID < 0 || vertexID >= table.VertexCount() {
		return false, fmt.Errorf("%w: vertex %d out of range", ErrInvalidGeometry, vertexID)
	}

	return table.IsBoundaryVertex(vertexID), nil
}

func (m *Mesh) FaceNeighbors(faceID int) ([3]int, error) {
	table, err := m.cornerTable()
	if err != nil {
		return [3]int{}, err
	}

	if faceID < 0 || faceID >= table.FaceCount() {
		return [3]int{}, fmt.Errorf("%w: face %d out of range", ErrInvalidGeometry, faceID)
	}

	return table.FaceNeighbors(faceID), nil
}

func (m *Mesh) cornerTable() (*topology.CornerTable, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}

	faces := make([]topology.Face, len(m.faces))
	for i, face := range m.faces {
		if IsDegenerateFace(face) {
			return nil, fmt.Errorf("%w: face %d is degenerate", ErrInvalidGeometry, i)
		}

		faces[i] = topology.Face(face)
	}

	return topology.BuildCornerTable(m.PointCount(), faces)
}
