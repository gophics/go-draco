package draco

import "fmt"

func (m *Mesh) CornerCount() int {
	if m == nil {
		return 0
	}

	return len(m.faces) * 3
}

func (m *Mesh) CornerToPointID(corner int) uint32 {
	if m == nil || corner < 0 || corner >= len(m.faces)*3 {
		return 0
	}

	return m.faces[corner/3][corner%3]
}

func (m *Mesh) RemoveIsolatedPoints() error {
	if m == nil {
		return fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if err := m.Validate(); err != nil {
		return err
	}

	return m.removeUnusedPointsAndAttributeEntries()
}

func (m *Mesh) AddPerVertexAttribute(attr *Attribute) (int, error) {
	if m == nil {
		return -1, fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if err := m.Validate(); err != nil {
		return -1, err
	}

	if attr == nil {
		return -1, fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	position := m.namedAttribute(AttributePosition)
	if position == nil {
		return -1, fmt.Errorf("%w: mesh position attribute missing", ErrInvalidGeometry)
	}

	if attr.EntryCount() != position.EntryCount() {
		return -1, fmt.Errorf("%w: per-vertex attribute requires %d entries, got %d", ErrInvalidGeometry, position.EntryCount(), attr.EntryCount())
	}

	cloned := attr.Clone()
	if position.IsIdentityMapping() && cloned.EntryCount() == m.PointCount() {
		if err := cloned.SetIdentityMapping(); err != nil {
			return -1, err
		}

		return m.AddAttribute(cloned)
	}

	if err := cloned.SetExplicitMapping(m.PointCount()); err != nil {
		return -1, err
	}

	for pointID := 0; pointID < m.PointCount(); pointID++ {
		if err := cloned.SetPointMapEntry(pointID, position.mappedIndex(pointID)); err != nil {
			return -1, err
		}
	}

	return m.AddAttribute(cloned)
}

func (m *Mesh) AddAttributeWithConnectivity(attr *Attribute, cornerToValue []uint32) (int, error) {
	if m == nil {
		return -1, fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if err := m.Validate(); err != nil {
		return -1, err
	}

	if attr == nil {
		return -1, fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	if len(cornerToValue) != m.CornerCount() {
		return -1, fmt.Errorf("%w: connectivity mapping has %d entries for %d corners", ErrInvalidGeometry, len(cornerToValue), m.CornerCount())
	}

	for corner, valueID := range cornerToValue {
		if int(valueID) >= attr.EntryCount() {
			return -1, fmt.Errorf("%w: corner %d references attribute entry %d out of %d", ErrInvalidGeometry, corner, valueID, attr.EntryCount())
		}
	}

	oldNumPoints := m.PointCount()
	originalPointForNew := make([]uint32, 0, oldNumPoints)
	assignedEntryForPoint := make([]uint32, 0, oldNumPoints)
	pointAssigned := make([]bool, 0, oldNumPoints)
	for i := 0; i < oldNumPoints; i++ {
		originalPointForNew = append(originalPointForNew, uint32(i))
		assignedEntryForPoint = append(assignedEntryForPoint, 0)
		pointAssigned = append(pointAssigned, false)
	}

	type pointEntryKey struct {
		point uint32
		entry uint32
	}
	assignments := make(map[pointEntryKey]uint32, m.CornerCount())
	newFaces := append([]Face(nil), m.faces...)
	nextPointID := uint32(oldNumPoints)
	for faceID, face := range m.faces {
		for corner := 0; corner < 3; corner++ {
			originalPoint := face[corner]
			entryID := cornerToValue[faceID*3+corner]
			key := pointEntryKey{point: originalPoint, entry: entryID}
			if mappedPoint, ok := assignments[key]; ok {
				newFaces[faceID][corner] = mappedPoint
				continue
			}

			if !pointAssigned[originalPoint] {
				assignments[key] = originalPoint
				assignedEntryForPoint[originalPoint] = entryID
				pointAssigned[originalPoint] = true
				newFaces[faceID][corner] = originalPoint
				continue
			}

			mappedPoint := nextPointID
			nextPointID++
			assignments[key] = mappedPoint
			originalPointForNew = append(originalPointForNew, originalPoint)
			assignedEntryForPoint = append(assignedEntryForPoint, entryID)
			pointAssigned = append(pointAssigned, true)
			newFaces[faceID][corner] = mappedPoint
		}
	}

	expandedAttributes := make([]*Attribute, len(m.attributes))
	for i, existing := range m.attributes {
		expanded, err := expandAttributeForDuplicatedPoints(existing, originalPointForNew)
		if err != nil {
			return -1, err
		}

		expandedAttributes[i] = expanded
	}

	newAttr := attr.Clone()
	if err := newAttr.SetExplicitMapping(len(originalPointForNew)); err != nil {
		return -1, err
	}

	for pointID := range originalPointForNew {
		entryID := uint32(0)
		if pointAssigned[pointID] {
			entryID = assignedEntryForPoint[pointID]
		}

		if err := newAttr.SetPointMapEntry(pointID, entryID); err != nil {
			return -1, err
		}
	}

	temp := newPointCloud(len(originalPointForNew))
	temp.setMetadata(m.metadataRef().Clone())
	for _, existing := range expandedAttributes {
		if _, err := temp.AddAttribute(existing); err != nil {
			return -1, err
		}
	}

	attID, err := temp.AddAttribute(newAttr)
	if err != nil {
		return -1, err
	}

	m.PointCloud = *temp
	m.faces = newFaces
	return attID, nil
}

func expandAttributeForDuplicatedPoints(attr *Attribute, originalPointForNew []uint32) (*Attribute, error) {
	cloned := attr.Clone()
	if cloned.IsIdentityMapping() {
		if len(originalPointForNew) == attr.EntryCount() {
			if err := cloned.SetIdentityMapping(); err != nil {
				return nil, err
			}

			return cloned, nil
		}

		if err := cloned.SetExplicitMapping(len(originalPointForNew)); err != nil {
			return nil, err
		}

		for pointID, originalPoint := range originalPointForNew {
			if err := cloned.SetPointMapEntry(pointID, originalPoint); err != nil {
				return nil, err
			}
		}

		return cloned, nil
	}

	mapping := make([]uint32, len(originalPointForNew))
	for pointID, originalPoint := range originalPointForNew {
		mapping[pointID] = attr.mappedIndex(int(originalPoint))
	}

	cloned.mapping = mapping
	return cloned, nil
}
