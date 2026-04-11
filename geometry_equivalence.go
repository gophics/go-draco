package draco

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

func attributeSchemaEqual(a, b *Attribute) bool {
	if a == nil || b == nil {
		return a == b
	}

	return a.Type == b.Type &&
		a.DataType == b.DataType &&
		a.NumComponents == b.NumComponents &&
		a.Normalized == b.Normalized &&
		a.UniqueID == b.UniqueID &&
		a.Name == b.Name
}

func appendLengthPrefixed(dst []byte, value []byte) []byte {
	var length [4]byte
	binary.LittleEndian.PutUint32(length[:], uint32(len(value)))
	dst = append(dst, length[:]...)
	return append(dst, value...)
}

func pointSignature(pc *PointCloud, pointID int) ([]byte, error) {
	total := len(pc.attributes) * 4
	for _, attr := range pc.attributes {
		total += attr.ByteStride()
	}

	signature := make([]byte, 0, total)
	for attrID, attr := range pc.attributes {
		entryID := int(attr.mappedIndex(pointID))
		raw, err := attr.rawEntry(entryID)
		if err != nil {
			return nil, fmt.Errorf("draco: point %d attribute %d: %w", pointID, attrID, err)
		}

		signature = appendLengthPrefixed(signature, raw)
	}

	return signature, nil
}

func pointSignatures(pc *PointCloud) ([][]byte, error) {
	signatures := make([][]byte, pc.PointCount())
	for pointID := 0; pointID < pc.PointCount(); pointID++ {
		signature, err := pointSignature(pc, pointID)
		if err != nil {
			return nil, err
		}

		signatures[pointID] = signature
	}

	return signatures, nil
}

func pointSignatureMultiset(pc *PointCloud) ([]string, error) {
	signatures, err := pointSignatures(pc)
	if err != nil {
		return nil, err
	}

	out := make([]string, len(signatures))
	for i := range signatures {
		out[i] = string(signatures[i])
	}

	sort.Strings(out)
	return out, nil
}

func pointCloudEquivalent(pc, other *PointCloud) bool {
	if pc.numPoints != other.numPoints || len(pc.attributes) != len(other.attributes) {
		return false
	}

	for i := range pc.attributes {
		if !attributeSchemaEqual(pc.attributes[i], other.attributes[i]) {
			return false
		}
	}

	if !pc.metadataRef().Equal(other.metadataRef()) {
		return false
	}

	left, err := pointSignatureMultiset(pc)
	if err != nil {
		return false
	}

	right, err := pointSignatureMultiset(other)
	if err != nil {
		return false
	}

	if len(left) != len(right) {
		return false
	}

	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}

	return true
}

func buildUndirectedFaceSignature(signatures [][]byte, face Face) string {
	permutations := [6]Face{
		{face[0], face[1], face[2]},
		{face[1], face[2], face[0]},
		{face[2], face[0], face[1]},
		{face[0], face[2], face[1]},
		{face[2], face[1], face[0]},
		{face[1], face[0], face[2]},
	}
	bestKey := canonicalFaceSignatureForRotation(signatures, permutations[0])
	for _, permutation := range permutations[1:] {
		key := canonicalFaceSignatureForRotation(signatures, permutation)
		if strings.Compare(key, bestKey) < 0 {
			bestKey = key
		}
	}

	return bestKey
}

func canonicalFaceSignatureForRotation(signatures [][]byte, face Face) string {
	total := 12
	for _, pointID := range face {
		total += len(signatures[pointID])
	}

	key := make([]byte, total)
	offset := 0
	for _, pointID := range face {
		signature := signatures[pointID]
		binary.LittleEndian.PutUint32(key[offset:], uint32(len(signature)))
		offset += 4
		copy(key[offset:], signature)
		offset += len(signature)
	}

	return string(key)
}

func meshEquivalent(m, other *Mesh) bool {
	if len(m.faces) != len(other.faces) || !pointCloudEquivalent(&m.PointCloud, &other.PointCloud) {
		return false
	}

	if m.Name != other.Name {
		return false
	}

	if !m.materials.Equal(other.materials) || !m.nonMaterialTextures.Equal(other.nonMaterialTextures) {
		return false
	}

	if !m.structuralMetadata.Equal(other.structuralMetadata) {
		return false
	}

	if len(m.meshFeatures) != len(other.meshFeatures) ||
		len(m.meshFeaturesMaterialMask) != len(other.meshFeaturesMaterialMask) {
		return false
	}

	for i := range m.meshFeatures {
		if !meshFeaturesEqual(m.meshFeatures[i], other.meshFeatures[i]) {
			return false
		}

		if len(m.meshFeaturesMaterialMask[i]) != len(other.meshFeaturesMaterialMask[i]) {
			return false
		}

		for j := range m.meshFeaturesMaterialMask[i] {
			if m.meshFeaturesMaterialMask[i][j] != other.meshFeaturesMaterialMask[i][j] {
				return false
			}
		}
	}

	if len(m.propertyAttributeIndices) != len(other.propertyAttributeIndices) ||
		len(m.propertyAttributeMaterialMask) != len(other.propertyAttributeMaterialMask) {
		return false
	}

	for i := range m.propertyAttributeIndices {
		if m.propertyAttributeIndices[i] != other.propertyAttributeIndices[i] {
			return false
		}

		if len(m.propertyAttributeMaterialMask[i]) != len(other.propertyAttributeMaterialMask[i]) {
			return false
		}

		for j := range m.propertyAttributeMaterialMask[i] {
			if m.propertyAttributeMaterialMask[i][j] != other.propertyAttributeMaterialMask[i][j] {
				return false
			}
		}
	}

	leftSigs, err := pointSignatures(&m.PointCloud)
	if err != nil {
		return false
	}

	rightSigs, err := pointSignatures(&other.PointCloud)
	if err != nil {
		return false
	}

	leftFaces := make([]string, len(m.faces))
	rightFaces := make([]string, len(other.faces))
	for i, face := range m.faces {
		leftFaces[i] = buildUndirectedFaceSignature(leftSigs, face)
		rightFaces[i] = buildUndirectedFaceSignature(rightSigs, other.faces[i])
	}

	sort.Strings(leftFaces)
	sort.Strings(rightFaces)
	for i := range leftFaces {
		if leftFaces[i] != rightFaces[i] {
			return false
		}
	}

	return true
}
