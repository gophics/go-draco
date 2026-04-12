package draco

import "fmt"

type MeshCleanupOptions struct {
	RemoveDegenerateFaces  bool
	RemoveDuplicateFaces   bool
	RemoveUnusedAttributes bool
	MakeGeometryManifold   bool
}

func DefaultMeshCleanupOptions() MeshCleanupOptions {
	return MeshCleanupOptions{
		RemoveDegenerateFaces:  true,
		RemoveDuplicateFaces:   true,
		RemoveUnusedAttributes: true,
	}
}

func (m *Mesh) Cleanup(options MeshCleanupOptions) error {
	if m == nil {
		return fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if !options.RemoveDegenerateFaces && !options.RemoveDuplicateFaces &&
		!options.RemoveUnusedAttributes && !options.MakeGeometryManifold {
		return nil
	}

	if err := m.Validate(); err != nil {
		return err
	}

	if options.MakeGeometryManifold {
		return fmt.Errorf("%w: make geometry manifold", ErrUnsupportedFeature)
	}

	work := m.Clone()
	position := work.namedAttribute(AttributePosition)
	if position == nil {
		return fmt.Errorf("%w: mesh position attribute missing", ErrInvalidGeometry)
	}

	if options.RemoveDegenerateFaces {
		work.removeDegenerateFaces(position)
	}

	if options.RemoveDuplicateFaces {
		work.removeDuplicateFaces(position)
	}

	if options.RemoveUnusedAttributes {
		if err := work.removeUnusedPointsAndAttributeEntries(); err != nil {
			return err
		}
	}

	if err := work.Validate(); err != nil {
		return err
	}

	*m = *work
	return nil
}

func (m *Mesh) removeDegenerateFaces(position *Attribute) {
	if len(m.faces) == 0 {
		return
	}

	kept := m.faces[:0]
	for _, face := range m.faces {
		if isFaceDegenerateByPosition(position, face) {
			continue
		}

		kept = append(kept, face)
	}

	m.faces = kept
}

func (m *Mesh) removeDuplicateFaces(position *Attribute) {
	if len(m.faces) == 0 {
		return
	}

	seen := make(map[[3]uint32]struct{}, len(m.faces))
	kept := m.faces[:0]
	for _, face := range m.faces {
		normalizedFace, key := canonicalFaceByPosition(position, face)
		if _, ok := seen[key]; ok {
			continue
		}

		seen[key] = struct{}{}
		kept = append(kept, normalizedFace)
	}

	m.faces = kept
}

func (m *Mesh) removeUnusedPointsAndAttributeEntries() error {
	numOriginalPoints := m.PointCount()
	pointMap := make([]int, numOriginalPoints)
	for i := range pointMap {
		pointMap[i] = -1
	}

	numNewPoints := 0
	for _, face := range m.faces {
		for _, pointID := range face {
			if pointMap[pointID] >= 0 {
				continue
			}

			pointMap[pointID] = numNewPoints
			numNewPoints++
		}
	}

	if numOriginalPoints == 0 {
		numNewPoints = 0
	}

	newFaces := append([]Face(nil), m.faces...)
	for faceID := range newFaces {
		for corner, pointID := range newFaces[faceID] {
			newFaces[faceID][corner] = uint32(pointMap[pointID])
		}
	}

	compactedAttributes := make([]*Attribute, len(m.attributes))
	for attrID, attr := range m.attributes {
		compacted, err := compactAttribute(attr, pointMap, numNewPoints)
		if err != nil {
			return fmt.Errorf("draco: compact attribute %d: %w", attrID, err)
		}

		compactedAttributes[attrID] = compacted
	}

	m.faces = newFaces
	m.attributes = compactedAttributes
	m.numPoints = numNewPoints
	return nil
}

func compactAttribute(attr *Attribute, pointMap []int, numNewPoints int) (*Attribute, error) {
	usedEntries := make([]bool, attr.EntryCount())
	numUsedEntries := 0
	for oldPoint, newPoint := range pointMap {
		if newPoint < 0 {
			continue
		}

		entryID := int(attr.mappedIndex(oldPoint))
		if !usedEntries[entryID] {
			usedEntries[entryID] = true
			numUsedEntries++
		}
	}

	compacted, err := NewAttribute(attr.Type, attr.DataType, attr.NumComponents, numUsedEntries)
	if err != nil {
		return nil, err
	}

	compacted.Normalized = attr.Normalized
	compacted.UniqueID = attr.UniqueID

	entryMap := make([]int, attr.EntryCount())
	for i := range entryMap {
		entryMap[i] = -1
	}

	nextEntry := 0
	for oldEntry := 0; oldEntry < attr.EntryCount(); oldEntry++ {
		if !usedEntries[oldEntry] {
			continue
		}

		raw, err := attr.RawValue(oldEntry)
		if err != nil {
			return nil, err
		}

		if err := compacted.SetRawValue(nextEntry, raw); err != nil {
			return nil, err
		}

		entryMap[oldEntry] = nextEntry
		nextEntry++
	}

	if !attr.IsIdentityMapping() {
		if err := compacted.SetExplicitMapping(numNewPoints); err != nil {
			return nil, err
		}

		for oldPoint, newPoint := range pointMap {
			if newPoint < 0 {
				continue
			}

			oldEntry := int(attr.mappedIndex(oldPoint))
			if err := compacted.SetPointMapEntry(newPoint, uint32(entryMap[oldEntry])); err != nil {
				return nil, err
			}
		}

		return compacted, nil
	}

	if numUsedEntries != numNewPoints {
		if err := compacted.SetExplicitMapping(numNewPoints); err != nil {
			return nil, err
		}

		for oldPoint, newPoint := range pointMap {
			if newPoint < 0 {
				continue
			}

			oldEntry := int(attr.mappedIndex(oldPoint))
			if err := compacted.SetPointMapEntry(newPoint, uint32(entryMap[oldEntry])); err != nil {
				return nil, err
			}
		}
	}

	return compacted, nil
}

func isFaceDegenerateByPosition(position *Attribute, face Face) bool {
	a := position.mappedIndex(int(face[0]))
	b := position.mappedIndex(int(face[1]))
	c := position.mappedIndex(int(face[2]))
	return a == b || a == c || b == c
}

func canonicalFaceByPosition(position *Attribute, face Face) (Face, [3]uint32) {
	bestFace := face
	bestKey := [3]uint32{
		position.mappedIndex(int(face[0])),
		position.mappedIndex(int(face[1])),
		position.mappedIndex(int(face[2])),
	}
	for rotation := 1; rotation < 3; rotation++ {
		rotatedFace := rotateFace(face, rotation)
		key := [3]uint32{
			position.mappedIndex(int(rotatedFace[0])),
			position.mappedIndex(int(rotatedFace[1])),
			position.mappedIndex(int(rotatedFace[2])),
		}
		if lessFaceKey(key, bestKey) || (key == bestKey && lessActualFace(rotatedFace, bestFace)) {
			bestFace = rotatedFace
			bestKey = key
		}
	}

	return bestFace, bestKey
}

func rotateFace(face Face, rotation int) Face {
	switch rotation % 3 {
	case 1:
		return Face{face[1], face[2], face[0]}
	case 2:
		return Face{face[2], face[0], face[1]}
	default:
		return face
	}
}

func lessActualFace(a, b Face) bool {
	return compareActualFace(a, b) < 0
}

func compareActualFace(a, b Face) int {
	if a[0] < b[0] {
		return -1
	}

	if a[0] > b[0] {
		return 1
	}

	if a[1] < b[1] {
		return -1
	}

	if a[1] > b[1] {
		return 1
	}

	if a[2] < b[2] {
		return -1
	}

	if a[2] > b[2] {
		return 1
	}

	return 0
}

func lessFaceKey(a, b [3]uint32) bool {
	for i := 0; i < 3; i++ {
		if a[i] < b[i] {
			return true
		}

		if a[i] > b[i] {
			return false
		}
	}

	return false
}
