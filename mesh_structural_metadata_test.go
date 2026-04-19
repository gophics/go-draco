package draco

import (
	"testing"

	md "github.com/gophics/go-draco/metadata"
	"github.com/stretchr/testify/require"
)

func TestMeshStructuralMetadataCloneAndEquivalence(t *testing.T) {
	mesh := mustNewMesh(3)
	pos := mustNewFloat32Attribute(AttributePosition, 3, 3)
	for i, xyz := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}} {
		setFloat32Value(t, pos, i, xyz[:]...)
	}

	addMeshAttribute(t, mesh, pos)
	addFace(t, mesh, Face{0, 1, 2})

	sm := &md.StructuralMetadata{}
	schema := md.NewStructuralMetadataSchema()
	schema.JSON = md.NewStructuralMetadataSchemaString("schema", "Data")
	sm.Schema = schema
	_, err := sm.AddPropertyAttribute(&md.PropertyAttribute{Name: "directions", Class: "vectors"})
	require.NoError(t, err)
	_, err = sm.AddPropertyAttribute(&md.PropertyAttribute{Name: "magnitudes", Class: "vectors"})
	require.NoError(t, err)
	mesh.setStructuralMetadata(sm)
	materials := MaterialLibrary{}
	_, err = materials.AddMaterial(NewMaterial())
	require.NoError(t, err)
	_, err = materials.AddMaterial(NewMaterial())
	require.NoError(t, err)
	require.NoError(t, mesh.SetMaterials(materials))

	index, err := mesh.AddPropertyAttributesIndex(0)
	require.NoError(t, err)
	require.Equal(t, 0, index)
	index, err = mesh.AddPropertyAttributesIndex(1)
	require.NoError(t, err)
	require.Equal(t, 1, index)
	require.NoError(t, mesh.AddPropertyAttributesIndexMaterialMask(0, 0))
	require.NoError(t, mesh.AddPropertyAttributesIndexMaterialMask(1, 1))

	clone := mesh.Clone()
	requireMeshEquivalent(t, mesh, clone)
	cloneMetadata := clone.StructuralMetadataClone()
	meshMetadata := mesh.StructuralMetadataClone()
	require.NotSame(t, meshMetadata, cloneMetadata)
	require.Equal(t, "Data", cloneMetadata.Schema.JSON.String)
	require.Equal(t, 2, clone.PropertyAttributeIndexCount())
	value, err := clone.PropertyAttributeIndex(0)
	require.NoError(t, err)
	require.Equal(t, 0, value)
	value, err = clone.PropertyAttributeIndex(1)
	require.NoError(t, err)
	require.Equal(t, 1, value)
	value, err = clone.PropertyAttributeIndexMaterialMask(0, 0)
	require.NoError(t, err)
	require.Equal(t, 0, value)
	value, err = clone.PropertyAttributeIndexMaterialMask(1, 0)
	require.NoError(t, err)
	require.Equal(t, 1, value)

	require.NoError(t, clone.RemovePropertyAttributesIndex(0))
	require.Equal(t, 1, clone.PropertyAttributeIndexCount())
	require.Equal(t, 2, mesh.PropertyAttributeIndexCount())
	require.False(t, mesh.Equivalent(clone))
}

func TestMeshValidateStructuralMetadataIndices(t *testing.T) {
	testCases := []struct {
		name  string
		build func() *Mesh
		check func(error) bool
	}{
		{
			name: "without-structural-metadata",
			build: func() *Mesh {
				mesh := mustNewMesh(0)
				_, err := mesh.AddPropertyAttributesIndex(0)
				require.ErrorIs(t, err, ErrInvalidGeometry)
				return mesh
			},
			check: func(err error) bool { return err == nil },
		},
		{
			name: "out-of-range-index",
			build: func() *Mesh {
				mesh := mustNewMesh(0)
				mesh.setStructuralMetadata(&md.StructuralMetadata{})
				_, err := mesh.AddPropertyAttributesIndex(1)
				require.ErrorIs(t, err, ErrInvalidGeometry)
				return mesh
			},
			check: func(err error) bool { return err == nil },
		},
		{
			name: "valid",
			build: func() *Mesh {
				mesh := mustNewMesh(0)
				structuralMetadata := &md.StructuralMetadata{}
				_, err := structuralMetadata.AddPropertyAttribute(&md.PropertyAttribute{Name: "one"})
				require.NoError(t, err)
				mesh.setStructuralMetadata(structuralMetadata)
				materials := MaterialLibrary{}
				for i := 0; i <= 7; i++ {
					_, err = materials.AddMaterial(NewMaterial())
					require.NoError(t, err)
				}

				require.NoError(t, mesh.SetMaterials(materials))
				_, err = mesh.AddPropertyAttributesIndex(0)
				require.NoError(t, err)
				require.NoError(t, mesh.AddPropertyAttributesIndexMaterialMask(0, 7))
				return mesh
			},
			check: func(err error) bool { return err == nil },
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.True(t, tc.check(tc.build().Validate()))
		})
	}
}

func TestMeshSetStructuralMetadataRejectsInvalidMetadata(t *testing.T) {
	mesh := mustNewMesh(0)
	err := mesh.SetStructuralMetadata(&md.StructuralMetadata{
		PropertyTables: []*md.PropertyTable{nil},
	})
	require.ErrorIs(t, err, md.ErrInvalidMetadata)
	require.Nil(t, mesh.StructuralMetadataClone())
}

func TestMeshSetStructuralMetadataClonesIngress(t *testing.T) {
	mesh := mustNewMesh(0)
	structuralMetadata := &md.StructuralMetadata{}
	_, err := structuralMetadata.AddPropertyTable(&md.PropertyTable{Name: "table"})
	require.NoError(t, err)
	_, err = structuralMetadata.AddPropertyAttribute(&md.PropertyAttribute{Name: "attribute"})
	require.NoError(t, err)

	require.NoError(t, mesh.SetStructuralMetadata(structuralMetadata))

	structuralMetadata.PropertyTables[0].Name = "mutated-table"
	structuralMetadata.PropertyAttributes[0].Name = "mutated-attribute"

	stored := mesh.StructuralMetadataClone()
	require.NotNil(t, stored)
	require.Equal(t, "table", stored.PropertyTables[0].Name)
	require.Equal(t, "attribute", stored.PropertyAttributes[0].Name)
}

func TestMeshStructuralMetadataIngressValidation(t *testing.T) {
	mesh := mustNewMesh(0)

	_, err := mesh.AddPropertyAttributesIndex(0)
	require.ErrorIs(t, err, ErrInvalidGeometry)

	structuralMetadata := &md.StructuralMetadata{}
	_, err = structuralMetadata.AddPropertyAttribute(&md.PropertyAttribute{Name: "one"})
	require.NoError(t, err)
	mesh.setStructuralMetadata(structuralMetadata)

	_, err = mesh.AddPropertyAttributesIndex(1)
	require.ErrorIs(t, err, ErrInvalidGeometry)

	index, err := mesh.AddPropertyAttributesIndex(0)
	require.NoError(t, err)

	err = mesh.AddPropertyAttributesIndexMaterialMask(index, 0)
	require.ErrorIs(t, err, ErrInvalidGeometry)

	materials := MaterialLibrary{}
	_, err = materials.AddMaterial(NewMaterial())
	require.NoError(t, err)
	require.NoError(t, mesh.SetMaterials(materials))
	require.NoError(t, mesh.AddPropertyAttributesIndexMaterialMask(index, 0))
}
