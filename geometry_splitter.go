package draco

import (
	"bytes"
	"fmt"
)

func (pc *PointCloud) SplitByAttribute(attID int) ([]*PointCloud, error) {
	if err := pc.Validate(); err != nil {
		return nil, err
	}

	if attID < 0 || attID >= pc.AttributeCount() {
		return nil, fmt.Errorf("%w: attribute %d out of range", ErrInvalidGeometry, attID)
	}

	splitAttr := pc.attribute(attID)
	groups, order, err := splitPointGroups(pc, splitAttr)
	if err != nil {
		return nil, err
	}

	out := make([]*PointCloud, 0, len(order))
	for _, key := range order {
		pointIDs := groups[key]
		group := newPointCloud(len(pointIDs))
		group.setMetadata(pc.metadataRef().Clone())
		for _, attr := range pc.attributes {
			cloned, err := cloneAttributeForPoints(attr, pointIDs)
			if err != nil {
				return nil, err
			}

			if _, err := group.AddAttribute(cloned); err != nil {
				return nil, err
			}
		}

		out = append(out, group)
	}

	return out, nil
}

func (m *Mesh) SplitByAttribute(attID int) ([]*Mesh, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}

	if attID < 0 || attID >= m.AttributeCount() {
		return nil, fmt.Errorf("%w: attribute %d out of range", ErrInvalidGeometry, attID)
	}

	splitAttr := m.attribute(attID)
	groups := make(map[string][]int)
	order := make([]string, 0)
	for faceID, face := range m.faces {
		key, err := meshFaceSplitKey(splitAttr, face)
		if err != nil {
			return nil, fmt.Errorf("draco: split face %d: %w", faceID, err)
		}

		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}

		groups[key] = append(groups[key], faceID)
	}

	out := make([]*Mesh, 0, len(order))
	for _, key := range order {
		split, err := m.cloneWithFaces(groups[key])
		if err != nil {
			return nil, err
		}

		out = append(out, split)
	}

	return out, nil
}

func splitPointGroups(pc *PointCloud, splitAttr *Attribute) (map[string][]int, []string, error) {
	groups := make(map[string][]int)
	order := make([]string, 0)
	for pointID := 0; pointID < pc.PointCount(); pointID++ {
		key, err := attributePointGroupKey(splitAttr, pointID)
		if err != nil {
			return nil, nil, err
		}

		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}

		groups[key] = append(groups[key], pointID)
	}

	return groups, order, nil
}

func attributePointGroupKey(attr *Attribute, pointID int) (string, error) {
	raw, err := attr.RawValue(int(attr.mappedIndex(pointID)))
	if err != nil {
		return "", err
	}

	var key []byte
	key = appendLengthPrefixed(key, raw)
	return string(key), nil
}

func meshFaceSplitKey(attr *Attribute, face Face) (string, error) {
	first, err := attr.RawValue(int(attr.mappedIndex(int(face[0]))))
	if err != nil {
		return "", err
	}

	for corner := 1; corner < 3; corner++ {
		raw, err := attr.RawValue(int(attr.mappedIndex(int(face[corner]))))
		if err != nil {
			return "", err
		}

		if !bytes.Equal(first, raw) {
			return "", fmt.Errorf("%w: split attribute %s must be constant per face", ErrInvalidGeometry, attr.Type)
		}
	}

	var key []byte
	key = appendLengthPrefixed(key, first)
	return string(key), nil
}
