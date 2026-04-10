package metadata

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStructuralMetadataCopyAndCompare(t *testing.T) {
	sm := &StructuralMetadata{}
	schema := NewStructuralMetadataSchema()
	schema.JSON = NewStructuralMetadataSchemaString("schema", "Culture")
	sm.Schema = schema

	table := &PropertyTable{Name: "Just Read The Instructions", Class: "General Contact Unit", Count: 456}
	_, err := table.AddProperty(&PropertyTableProperty{Name: "Determinist"})
	require.NoError(t, err)
	_, err = table.AddProperty(&PropertyTableProperty{Name: "Revisionist"})
	require.NoError(t, err)
	index, err := sm.AddPropertyTable(table)
	require.NoError(t, err)
	require.Equal(t, 0, index)

	attribute := &PropertyAttribute{Name: "movement", Class: "movement"}
	_, err = attribute.AddProperty(&PropertyAttributeProperty{Name: "magnitude", AttributeName: "_MAGNITUDE"})
	require.NoError(t, err)
	index, err = sm.AddPropertyAttribute(attribute)
	require.NoError(t, err)
	require.Equal(t, 0, index)

	clone := sm.Clone()
	require.True(t, sm.Equal(clone))
	propertyTable, err := clone.PropertyTable(0)
	require.NoError(t, err)
	property, err := propertyTable.Property(1)
	require.NoError(t, err)
	require.Equal(t, "Revisionist", property.Name)
	propertyAttribute, err := clone.PropertyAttribute(0)
	require.NoError(t, err)
	attributeProperty, err := propertyAttribute.Property(0)
	require.NoError(t, err)
	require.Equal(t, "_MAGNITUDE", attributeProperty.AttributeName)

	require.NoError(t, clone.RemovePropertyTable(0))
	require.Equal(t, 0, clone.PropertyTableCount())
	require.Equal(t, 1, sm.PropertyTableCount())

	clone = sm.Clone()
	require.NoError(t, clone.RemovePropertyAttribute(0))
	require.Equal(t, 0, clone.PropertyAttributeCount())
	require.Equal(t, 1, sm.PropertyAttributeCount())
}

func TestStructuralMetadataCompare(t *testing.T) {
	a := &StructuralMetadata{}
	b := &StructuralMetadata{}
	require.True(t, a.Equal(b))

	schema := NewStructuralMetadataSchema()
	schema.JSON = NewStructuralMetadataSchemaString("schema", "one")
	a.Schema = schema
	require.False(t, a.Equal(b))

	a = &StructuralMetadata{}
	b = &StructuralMetadata{}
	_, err := a.AddPropertyTable(&PropertyTable{Name: "one"})
	require.NoError(t, err)
	_, err = b.AddPropertyTable(&PropertyTable{Name: "two"})
	require.NoError(t, err)
	require.False(t, a.Equal(b))

	a = &StructuralMetadata{}
	b = &StructuralMetadata{}
	_, err = a.AddPropertyAttribute(&PropertyAttribute{Name: "one"})
	require.NoError(t, err)
	_, err = b.AddPropertyAttribute(&PropertyAttribute{Name: "two"})
	require.NoError(t, err)
	require.False(t, a.Equal(b))
}

func TestStructuralMetadataAddPropertyRejectsInvalidPayloads(t *testing.T) {
	t.Run("table", func(t *testing.T) {
		sm := &StructuralMetadata{}
		index, err := sm.AddPropertyTable(&PropertyTable{
			Name: "table",
			Properties: []*PropertyTableProperty{
				nil,
			},
		})
		require.ErrorIs(t, err, ErrInvalidMetadata)
		require.Equal(t, -1, index)
		require.Zero(t, sm.PropertyTableCount())
	})

	t.Run("attribute", func(t *testing.T) {
		sm := &StructuralMetadata{}
		index, err := sm.AddPropertyAttribute(&PropertyAttribute{
			Name: "attribute",
			Properties: []*PropertyAttributeProperty{
				nil,
			},
		})
		require.ErrorIs(t, err, ErrInvalidMetadata)
		require.Equal(t, -1, index)
		require.Zero(t, sm.PropertyAttributeCount())
	})
}

func TestStructuralMetadataSchemaDefaults(t *testing.T) {
	schema := NewStructuralMetadataSchema()
	require.True(t, schema.Empty())
	require.Equal(t, "schema", schema.JSON.Name)
	require.Equal(t, StructuralMetadataSchemaObjectTypeObject, schema.JSON.Type)
}

func TestStructuralMetadataSchemaObjectSettersAndLookup(t *testing.T) {
	object := NewStructuralMetadataSchemaArray("", []StructuralMetadataSchemaObject{NewStructuralMetadataSchemaInteger("entry", 12)})
	require.Equal(t, StructuralMetadataSchemaObjectTypeArray, object.Type)
	require.Len(t, object.Array, 1)
	require.Equal(t, 12, object.Array[0].Integer)

	object = NewStructuralMetadataSchemaObjects("", []StructuralMetadataSchemaObject{
		NewStructuralMetadataSchemaInteger("object1", 1),
		NewStructuralMetadataSchemaString("object2", "two"),
	})
	child := NewStructuralMetadataSchemaObjects("object3", []StructuralMetadataSchemaObject{
		NewStructuralMetadataSchemaString("child_object", "child"),
	})
	object.Objects = append(object.Objects, child)

	require.Nil(t, object.ObjectByName("child_object"))
	got := object.ObjectByName("object2")
	require.NotNil(t, got)
	require.Equal(t, "two", got.String)

	nested := object.ObjectByName("object3")
	require.NotNil(t, nested)
	require.NotNil(t, nested.ObjectByName("child_object"))
	require.Equal(t, "child", nested.ObjectByName("child_object").String)
}

func TestStructuralMetadataSchemaCompare(t *testing.T) {
	a := NewStructuralMetadataSchema()
	b := NewStructuralMetadataSchema()
	require.True(t, a.Equal(b))
	a.JSON = NewStructuralMetadataSchemaBoolean("schema", true)
	require.False(t, a.Equal(b))
}

func TestPropertyAttributeDefaults(t *testing.T) {
	var property PropertyAttributeProperty
	require.Empty(t, property.Name)
	require.Empty(t, property.AttributeName)

	var attribute PropertyAttribute
	require.Empty(t, attribute.Name)
	require.Empty(t, attribute.Class)
	require.Equal(t, 0, attribute.PropertyCount())
}

func TestPropertyAttributeSettersCloneAndRemove(t *testing.T) {
	attribute := &PropertyAttribute{Name: "movement", Class: "movement"}
	index, err := attribute.AddProperty(&PropertyAttributeProperty{Name: "magnitude", AttributeName: "_MAGNITUDE"})
	require.NoError(t, err)
	require.Equal(t, 0, index)
	index, err = attribute.AddProperty(&PropertyAttributeProperty{Name: "direction", AttributeName: "_DIRECTION"})
	require.NoError(t, err)
	require.Equal(t, 1, index)
	require.Equal(t, 2, attribute.PropertyCount())
	property, err := attribute.Property(1)
	require.NoError(t, err)
	require.Equal(t, "_DIRECTION", property.AttributeName)

	clone := attribute.Clone()
	require.True(t, attribute.Equal(clone))
	require.NoError(t, clone.RemoveProperty(0))
	require.Equal(t, 1, clone.PropertyCount())
	property, err = clone.Property(0)
	require.NoError(t, err)
	require.Equal(t, "direction", property.Name)
	require.Equal(t, 2, attribute.PropertyCount())
}

func TestPropertyAttributeCompare(t *testing.T) {
	a := &PropertyAttribute{Name: "one", Class: "class"}
	b := &PropertyAttribute{Name: "one", Class: "class"}
	_, err := a.AddProperty(&PropertyAttributeProperty{Name: "prop", AttributeName: "_PROP"})
	require.NoError(t, err)
	_, err = b.AddProperty(&PropertyAttributeProperty{Name: "prop", AttributeName: "_PROP"})
	require.NoError(t, err)
	require.True(t, a.Equal(b))

	property := b.Properties[0]
	property.AttributeName = "_OTHER"
	require.False(t, a.Equal(b))
}

func TestPropertyAttributeRejectsInvalidPropertyPayloads(t *testing.T) {
	attribute := &PropertyAttribute{Name: "movement", Class: "movement"}
	_, err := attribute.AddProperty(&PropertyAttributeProperty{Name: "magnitude"})
	require.ErrorIs(t, err, ErrInvalidMetadata)

	attribute.Properties = []*PropertyAttributeProperty{{Name: "", AttributeName: "_MAGNITUDE"}}
	require.ErrorIs(t, attribute.Validate(), ErrInvalidMetadata)
}

func TestPropertyTableDefaults(t *testing.T) {
	var data PropertyTablePropertyData
	require.Empty(t, data.Data)
	require.Equal(t, 0, data.Target)

	var property PropertyTableProperty
	require.Empty(t, property.Name)
	require.Empty(t, property.Data.Data)
	require.Empty(t, property.ArrayOffsets.Type)
	require.Empty(t, property.StringOffsets.Type)

	var table PropertyTable
	require.Empty(t, table.Name)
	require.Empty(t, table.Class)
	require.Equal(t, 0, table.Count)
	require.Equal(t, 0, table.PropertyCount())
}

func TestPropertyTableSettersCloneAndRemove(t *testing.T) {
	table := &PropertyTable{Name: "table", Class: "class", Count: 2}
	index, err := table.AddProperty(&PropertyTableProperty{Name: "one"})
	require.NoError(t, err)
	require.Equal(t, 0, index)
	index, err = table.AddProperty(&PropertyTableProperty{Name: "two"})
	require.NoError(t, err)
	require.Equal(t, 1, index)

	clone := table.Clone()
	require.True(t, table.Equal(clone))
	require.NoError(t, clone.RemoveProperty(0))
	require.Equal(t, 1, clone.PropertyCount())
	property, err := clone.Property(0)
	require.NoError(t, err)
	require.Equal(t, "two", property.Name)
	require.Equal(t, 2, table.PropertyCount())
}

func TestPropertyTableCompare(t *testing.T) {
	a := &PropertyTable{Name: "one", Class: "class", Count: 1}
	b := &PropertyTable{Name: "one", Class: "class", Count: 1}
	_, err := a.AddProperty(&PropertyTableProperty{Name: "prop"})
	require.NoError(t, err)
	_, err = b.AddProperty(&PropertyTableProperty{Name: "prop"})
	require.NoError(t, err)
	require.True(t, a.Equal(b))

	property := b.Properties[0]
	property.Data.Target = 1
	require.False(t, a.Equal(b))
}

func TestPropertyTableOffsetsEncodeDecode(t *testing.T) {
	testCases := []struct {
		name     string
		input    []uint64
		wantType string
	}{
		{name: "uint8", input: []uint64{0x5, 0x21, 0xff}, wantType: "UINT8"},
		{name: "uint16", input: []uint64{0x5, 0x21, 0xffff}, wantType: "UINT16"},
		{name: "uint32", input: []uint64{0x5, 0x21, 0xffffffff}, wantType: "UINT32"},
		{name: "uint64", input: []uint64{0x5, 0x21, 0x100000000}, wantType: "UINT64"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			offsets := MakePropertyTablePropertyOffsetsFromInts(tc.input)
			require.Equal(t, tc.wantType, offsets.Type)

			decoded, err := offsets.ParseToInts()
			require.NoError(t, err)
			require.Equal(t, tc.input, decoded)
		})
	}
}

func TestPropertyTableRejectsInvalidPropertyPayloads(t *testing.T) {
	table := &PropertyTable{Name: "table"}
	_, err := table.AddProperty(nil)
	require.ErrorIs(t, err, ErrNilProperty)

	table.Properties = []*PropertyTableProperty{nil}
	require.ErrorIs(t, table.Validate(), ErrInvalidMetadata)
}
