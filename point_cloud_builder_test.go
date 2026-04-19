package draco

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPointCloudAssemblyFinalizeWithoutDedup(t *testing.T) {
	var builder pointCloudBuilder
	builder.Start(3)

	posID, err := builder.AddAttribute(AttributePosition, 3, DataTypeFloat32)
	require.NoError(t, err)
	intensityID, err := builder.AddAttribute(AttributeGeneric, 1, DataTypeInt16)
	require.NoError(t, err)

	for pointID, xyz := range [][3]float32{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}} {
		require.NoError(t, builder.SetAttributeValueForPoint(posID, pointID, xyz))
	}

	require.NoError(t, builder.SetAttributeValuesForAllPoints(intensityID, []int16{10, 20, 30}, 0))
	require.NoError(t, builder.SetAttributeUniqueID(posID, 1234))

	pc, err := builder.Finalize(false)
	require.NoError(t, err)
	requirePointCloudCounts(t, pc, 3, 2)
	got := pc.AttributeByUniqueID(1234)
	require.NotNil(t, got)
	require.Equal(t, AttributePosition, got.Type)
}

func TestPointCloudAssemblyDeduplicatesPoints(t *testing.T) {
	var builder pointCloudBuilder
	builder.Start(4)

	posID, err := builder.AddAttribute(AttributePosition, 3, DataTypeFloat32)
	require.NoError(t, err)
	colorID, err := builder.AddAttribute(AttributeColor, 3, DataTypeUint8)
	require.NoError(t, err)

	values := [][3]float32{{1, 2, 3}, {4, 5, 6}, {1, 2, 3}, {4, 5, 6}}
	colors := [][3]uint8{{1, 2, 3}, {4, 5, 6}, {1, 2, 3}, {4, 5, 6}}
	for pointID := range values {
		require.NoError(t, builder.SetAttributeValueForPoint(posID, pointID, values[pointID]))
		require.NoError(t, builder.SetAttributeValueForPoint(colorID, pointID, colors[pointID]))
	}

	pc, err := builder.Finalize(true)
	require.NoError(t, err)
	requirePointCloudCounts(t, pc, 2, 2)
}

func TestPointCloudAssemblyCanBeReused(t *testing.T) {
	var builder pointCloudBuilder

	builder.Start(2)
	posID, err := builder.AddAttribute(AttributePosition, 3, DataTypeFloat32)
	require.NoError(t, err)
	require.NoError(t, builder.SetAttributeValuesForAllPoints(posID, []float32{1, 2, 3, 4, 5, 6}, 0))
	first, err := builder.Finalize(false)
	require.NoError(t, err)
	require.Equal(t, 2, first.PointCount())

	builder.Start(1)
	posID, err = builder.AddAttribute(AttributePosition, 3, DataTypeFloat32)
	require.NoError(t, err)
	require.NoError(t, builder.SetAttributeValuesForAllPoints(posID, []float32{7, 8, 9}, 0))
	second, err := builder.Finalize(false)
	require.NoError(t, err)
	require.Equal(t, 1, second.PointCount())
}

func TestPointCloudAssemblyAttributeName(t *testing.T) {
	var builder pointCloudBuilder
	builder.Start(1)

	posID, err := builder.AddAttribute(AttributePosition, 3, DataTypeFloat32)
	require.NoError(t, err)
	require.NoError(t, builder.SetAttributeName(posID, "Bob"))
	require.NoError(t, builder.SetAttributeValuesForAllPoints(posID, []float32{1, 2, 3}, 0))

	pc, err := builder.Finalize(false)
	require.NoError(t, err)
	require.Equal(t, "Bob", pc.attribute(posID).Name)
}

type pointCloudBuilder struct {
	pc *PointCloud
}

func (b *pointCloudBuilder) Start(numPoints int) {
	b.pc = newPointCloud(numPoints)
}

func (b *pointCloudBuilder) AddAttribute(attType AttributeType, numComponents int, dataType DataType) (int, error) {
	return b.AddAttributeNormalized(attType, numComponents, dataType, false)
}

func (b *pointCloudBuilder) AddAttributeNormalized(attType AttributeType, numComponents int, dataType DataType, normalized bool) (int, error) {
	pc, err := b.requirePointCloud()
	if err != nil {
		return -1, err
	}

	attr, err := NewAttribute(attType, dataType, numComponents, pc.PointCount())
	if err != nil {
		return -1, err
	}

	attr.Normalized = normalized
	return pc.AddAttribute(attr)
}

func (b *pointCloudBuilder) SetAttributeValueForPoint(attID, pointID int, values any) error {
	pc, err := b.requirePointCloud()
	if err != nil {
		return err
	}

	if pointID < 0 || pointID >= pc.PointCount() {
		return fmt.Errorf("draco: point index %d out of range for %d points", pointID, pc.PointCount())
	}

	attr, err := b.requireAttribute(attID)
	if err != nil {
		return err
	}

	return setAttributeValueFromComponents(attr, pointID, values)
}

func (b *pointCloudBuilder) SetAttributeValuesForAllPoints(attID int, values any, offset int) error {
	pc, err := b.requirePointCloud()
	if err != nil {
		return err
	}

	attr, err := b.requireAttribute(attID)
	if err != nil {
		return err
	}

	return setAttributeValuesFromFlatComponents(attr, 0, pc.PointCount(), values, offset)
}

func (b *pointCloudBuilder) SetAttributeUniqueID(attID int, uniqueID uint32) error {
	attr, err := b.requireAttribute(attID)
	if err != nil {
		return err
	}

	attr.UniqueID = uniqueID
	return nil
}

func (b *pointCloudBuilder) SetAttributeName(attID int, name string) error {
	attr, err := b.requireAttribute(attID)
	if err != nil {
		return err
	}

	attr.Name = name
	return nil
}

func (b *pointCloudBuilder) Finalize(deduplicate bool) (*PointCloud, error) {
	pc, err := b.requirePointCloud()
	if err != nil {
		return nil, err
	}

	out := pc
	if deduplicate {
		out, err = deduplicatePointCloud(pc)
		if err != nil {
			return nil, err
		}
	}

	b.pc = nil
	return out, nil
}

func (b *pointCloudBuilder) requirePointCloud() (*PointCloud, error) {
	if b.pc == nil {
		return nil, errors.New("draco: point cloud builder has not been started")
	}

	return b.pc, nil
}

func (b *pointCloudBuilder) requireAttribute(attID int) (*Attribute, error) {
	pc, err := b.requirePointCloud()
	if err != nil {
		return nil, err
	}

	if attID < 0 || attID >= pc.AttributeCount() {
		return nil, fmt.Errorf("draco: attribute index %d out of range", attID)
	}

	return pc.attribute(attID), nil
}

func deduplicatePointCloud(pc *PointCloud) (*PointCloud, error) {
	keptPoints, _, err := deduplicatePointSet(pc.attributes, pc.PointCount())
	if err != nil {
		return nil, err
	}

	out := newPointCloud(len(keptPoints))
	out.setMetadata(pc.metadataRef().Clone())
	for _, attr := range pc.attributes {
		cloned, err := cloneAttributeForPoints(attr, keptPoints)
		if err != nil {
			return nil, err
		}

		if _, err := out.AddAttribute(cloned); err != nil {
			return nil, err
		}
	}

	return out, nil
}

func deduplicatePointSet(attrs []*Attribute, numPoints int) ([]int, []uint32, error) {
	keptPoints := make([]int, 0, numPoints)
	pointMap := make([]uint32, numPoints)
	signatures := make(map[string]uint32, numPoints)
	for pointID := 0; pointID < numPoints; pointID++ {
		signature, err := pointValueSignature(attrs, pointID)
		if err != nil {
			return nil, nil, err
		}

		if existing, ok := signatures[signature]; ok {
			pointMap[pointID] = existing
			continue
		}

		index := uint32(len(keptPoints))
		signatures[signature] = index
		pointMap[pointID] = index
		keptPoints = append(keptPoints, pointID)
	}

	return keptPoints, pointMap, nil
}

func pointValueSignature(attrs []*Attribute, pointID int) (string, error) {
	signature := make([]byte, 0, len(attrs)*8)
	for _, attr := range attrs {
		raw, err := attr.RawValue(int(attr.mappedIndex(pointID)))
		if err != nil {
			return "", err
		}

		signature = appendLengthPrefixed(signature, raw)
	}

	return string(signature), nil
}
