package draco

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTriangleSoupAssemblyDeduplicatesVertices(t *testing.T) {
	var builder triangleSoupMeshBuilder
	builder.Start(2)

	posID, err := builder.AddAttribute(AttributePosition, 3, DataTypeFloat32)
	require.NoError(t, err)
	texID, err := builder.AddAttribute(AttributeTexCoord, 2, DataTypeFloat32)
	require.NoError(t, err)
	require.NoError(t, builder.SetAttributeUniqueID(posID, 77))

	require.NoError(t, builder.SetAttributeValuesForFace(
		posID,
		0,
		[]float32{0, 0, 0},
		[]float32{1, 0, 0},
		[]float32{0, 1, 0},
	))
	require.NoError(t, builder.SetAttributeValuesForFace(
		posID,
		1,
		[]float32{1, 0, 0},
		[]float32{1, 1, 0},
		[]float32{0, 1, 0},
	))
	require.NoError(t, builder.SetAttributeValuesForFace(
		texID,
		0,
		[]float32{0, 0},
		[]float32{1, 0},
		[]float32{0, 1},
	))
	require.NoError(t, builder.SetAttributeValuesForFace(
		texID,
		1,
		[]float32{1, 0},
		[]float32{1, 1},
		[]float32{0, 1},
	))

	mesh, err := builder.Finalize()
	require.NoError(t, err)
	requireMeshCounts(t, mesh, 2, 4, 2)
	got := mesh.AttributeByUniqueID(77)
	require.NotNil(t, got)
	require.Equal(t, AttributePosition, got.Type)
}

func TestTriangleSoupAssemblyPerFaceValues(t *testing.T) {
	var builder triangleSoupMeshBuilder
	builder.Start(1)

	posID, err := builder.AddAttribute(AttributePosition, 3, DataTypeFloat32)
	require.NoError(t, err)
	genID, err := builder.AddAttribute(AttributeGeneric, 1, DataTypeInt32)
	require.NoError(t, err)

	require.NoError(t, builder.SetAttributeValuesForFace(
		posID,
		0,
		[]float32{0, 0, 0},
		[]float32{1, 0, 0},
		[]float32{0, 1, 0},
	))
	require.NoError(t, builder.SetPerFaceAttributeValueForFace(genID, 0, int32(9)))

	mesh, err := builder.Finalize()
	require.NoError(t, err)
	attr := requireMeshAttribute(t, mesh, AttributeGeneric, DataTypeInt32, 1)
	for pointID := 0; pointID < mesh.PointCount(); pointID++ {
		requireInt32Entry(t, attr, pointID, []int32{9})
	}
}

func TestTriangleSoupAssemblyAttributeName(t *testing.T) {
	var builder triangleSoupMeshBuilder
	builder.Start(1)

	posID, err := builder.AddAttribute(AttributePosition, 3, DataTypeFloat32)
	require.NoError(t, err)
	require.NoError(t, builder.SetAttributeName(posID, "Bob"))
	require.NoError(t, builder.SetAttributeValuesForFace(
		posID,
		0,
		[]float32{0, 0, 0},
		[]float32{1, 0, 0},
		[]float32{0, 1, 0},
	))

	mesh, err := builder.Finalize()
	require.NoError(t, err)
	require.Equal(t, "Bob", mesh.attribute(posID).Name)
}

type triangleSoupMeshBuilder struct {
	mesh *Mesh
}

func (b *triangleSoupMeshBuilder) Start(numFaces int) {
	mesh := newMesh(numFaces * 3)
	mesh.faces = make([]Face, numFaces)
	for faceID := 0; faceID < numFaces; faceID++ {
		base := uint32(faceID * 3)
		mesh.faces[faceID] = Face{base, base + 1, base + 2}
	}

	b.mesh = mesh
}

func (b *triangleSoupMeshBuilder) AddAttribute(attType AttributeType, numComponents int, dataType DataType) (int, error) {
	return b.AddAttributeNormalized(attType, numComponents, dataType, false)
}

func (b *triangleSoupMeshBuilder) AddAttributeNormalized(attType AttributeType, numComponents int, dataType DataType, normalized bool) (int, error) {
	mesh, err := b.requireMesh()
	if err != nil {
		return -1, err
	}

	attr, err := NewAttribute(attType, dataType, numComponents, mesh.PointCount())
	if err != nil {
		return -1, err
	}

	attr.Normalized = normalized
	return mesh.AddAttribute(attr)
}

func (b *triangleSoupMeshBuilder) SetAttributeValuesForFace(attID, faceID int, corner0, corner1, corner2 any) error {
	mesh, err := b.requireMesh()
	if err != nil {
		return err
	}

	if faceID < 0 || faceID >= mesh.FaceCount() {
		return fmt.Errorf("draco: face index %d out of range for %d faces", faceID, mesh.FaceCount())
	}

	attr, err := b.requireAttribute(attID)
	if err != nil {
		return err
	}

	start := faceID * 3
	if err := setAttributeValueFromComponents(attr, start, corner0); err != nil {
		return err
	}

	if err := setAttributeValueFromComponents(attr, start+1, corner1); err != nil {
		return err
	}

	return setAttributeValueFromComponents(attr, start+2, corner2)
}

func (b *triangleSoupMeshBuilder) SetPerFaceAttributeValueForFace(attID, faceID int, value any) error {
	return b.SetAttributeValuesForFace(attID, faceID, value, value, value)
}

func (b *triangleSoupMeshBuilder) SetAttributeUniqueID(attID int, uniqueID uint32) error {
	attr, err := b.requireAttribute(attID)
	if err != nil {
		return err
	}

	attr.UniqueID = uniqueID
	return nil
}

func (b *triangleSoupMeshBuilder) SetAttributeName(attID int, name string) error {
	attr, err := b.requireAttribute(attID)
	if err != nil {
		return err
	}

	attr.Name = name
	return nil
}

func (b *triangleSoupMeshBuilder) Finalize() (*Mesh, error) {
	mesh, err := b.requireMesh()
	if err != nil {
		return nil, err
	}

	keptPoints, pointMap, err := deduplicatePointSet(mesh.attributes, mesh.PointCount())
	if err != nil {
		return nil, err
	}

	out := newMesh(len(keptPoints))
	out.setMetadata(mesh.metadataRef().Clone())
	out.Name = mesh.Name
	out.materials = mesh.materials.Clone()
	out.nonMaterialTextures = mesh.nonMaterialTextures.Clone()
	out.meshFeatures = cloneMeshFeaturesSlice(mesh.meshFeatures)
	out.meshFeaturesMaterialMask = cloneIntMatrix(mesh.meshFeaturesMaterialMask)
	out.structuralMetadata = mesh.structuralMetadata.Clone()
	out.propertyAttributeIndices = append([]int(nil), mesh.propertyAttributeIndices...)
	out.propertyAttributeMaterialMask = cloneIntMatrix(mesh.propertyAttributeMaterialMask)
	for _, attr := range mesh.attributes {
		cloned, err := cloneAttributeForPoints(attr, keptPoints)
		if err != nil {
			return nil, err
		}

		if _, err := out.AddAttribute(cloned); err != nil {
			return nil, err
		}
	}

	for _, face := range mesh.faces {
		if err := out.AddFace(Face{
			pointMap[face[0]],
			pointMap[face[1]],
			pointMap[face[2]],
		}); err != nil {
			return nil, err
		}
	}

	b.mesh = nil
	return out, nil
}

func (b *triangleSoupMeshBuilder) requireMesh() (*Mesh, error) {
	if b.mesh == nil {
		return nil, errors.New("draco: triangle soup mesh builder has not been started")
	}

	return b.mesh, nil
}

func (b *triangleSoupMeshBuilder) requireAttribute(attID int) (*Attribute, error) {
	mesh, err := b.requireMesh()
	if err != nil {
		return nil, err
	}

	if attID < 0 || attID >= mesh.AttributeCount() {
		return nil, fmt.Errorf("draco: attribute index %d out of range", attID)
	}

	return mesh.attribute(attID), nil
}
