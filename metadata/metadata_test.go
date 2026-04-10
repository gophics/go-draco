package metadata

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/gophics/go-draco/internal/core"
	"github.com/stretchr/testify/require"
)

func TestElementRemoveEntry(t *testing.T) {
	var element Element
	require.NoError(t, element.SetInt("int", 100))
	require.NoError(t, element.RemoveEntry("int"))
	_, ok := element.Int("int")
	require.False(t, ok)
}

func TestElementTypedEntries(t *testing.T) {
	var element Element

	require.NoError(t, element.SetInt("int", 100))
	gotInt, ok := element.Int("int")
	require.True(t, ok)
	require.Equal(t, int32(100), gotInt)

	require.NoError(t, element.SetFloat64("double", 1.234))
	gotDouble, ok := element.Float64("double")
	require.True(t, ok)
	require.InDelta(t, 1.234, gotDouble, 1e-12)

	require.NoError(t, element.SetIntArray("int_array", []int32{1, 2, 3}))
	gotIntArray, ok := element.IntArray("int_array")
	require.True(t, ok)
	require.Equal(t, []int32{1, 2, 3}, gotIntArray)

	require.NoError(t, element.SetFloat64Array("double_array", []float64{0.1, 0.2, 0.3}))
	gotDoubleArray, ok := element.Float64Array("double_array")
	require.True(t, ok)
	require.Equal(t, []float64{0.1, 0.2, 0.3}, gotDoubleArray)

	require.NoError(t, element.SetString("string", "test string entry"))
	gotString, ok := element.String("string")
	require.True(t, ok)
	require.Equal(t, "test string entry", gotString)

	require.NoError(t, element.SetBinary("binary_data", []byte{0x1, 0x2, 0x3, 0x4}))
	gotBinary, ok := element.Binary("binary_data")
	require.True(t, ok)
	require.Equal(t, []byte{0x1, 0x2, 0x3, 0x4}, gotBinary)
}

func TestElementWriteOverEntry(t *testing.T) {
	var element Element
	require.NoError(t, element.SetInt("int", 100))
	require.NoError(t, element.SetInt("int", 200))
	got, ok := element.Int("int")
	require.True(t, ok)
	require.Equal(t, int32(200), got)
	require.Len(t, element.Entries, 1)
}

func TestElementNestedMetadata(t *testing.T) {
	var element Element
	child := &Element{}
	require.NoError(t, child.SetInt("int", 100))
	require.NoError(t, element.SetChild("sub0", child))
	require.ErrorIs(t, element.SetChild("sub0", child), ErrDuplicateChild)

	sub := element.Child("sub0")
	require.NotNil(t, sub)
	got, ok := sub.Int("int")
	require.True(t, ok)
	require.Equal(t, int32(100), got)

	require.NoError(t, sub.SetInt("new_entry", 20))
	got, ok = sub.Int("new_entry")
	require.True(t, ok)
	require.Equal(t, int32(20), got)
	_, ok = child.Int("new_entry")
	require.False(t, ok)
}

func TestElementClone(t *testing.T) {
	var element Element
	require.NoError(t, element.SetInt("int", 100))
	child := &Element{}
	require.NoError(t, child.SetInt("sub_int", 200))
	require.NoError(t, element.SetChild("sub0", child))

	cloned := element.Clone()
	got, ok := cloned.Int("int")
	require.True(t, ok)
	require.Equal(t, int32(100), got)

	sub := cloned.Child("sub0")
	require.NotNil(t, sub)
	got, ok = sub.Int("sub_int")
	require.True(t, ok)
	require.Equal(t, int32(200), got)

	require.NoError(t, sub.SetInt("new_entry", 20))
	originalSub := element.Child("sub0")
	require.NotNil(t, originalSub)
	_, ok = originalSub.Int("new_entry")
	require.False(t, ok)
}

func TestGeometryMetadataHelpers(t *testing.T) {
	var geometry GeometryMetadata
	attribute := &AttributeMetadata{AttributeUniqueID: 10}
	require.NoError(t, attribute.Element.SetInt("int", 100))
	require.NoError(t, attribute.Element.SetString("name", "pos"))

	require.ErrorIs(t, geometry.AddAttributeMetadata(nil), ErrNilAttributeMetadata)
	require.NoError(t, geometry.AddAttributeMetadata(attribute))
	require.ErrorIs(t, geometry.AddAttributeMetadata(attribute), ErrDuplicateAttributeMeta)

	require.NotNil(t, geometry.AttributeMetadataByUniqueID(10))
	require.Nil(t, geometry.AttributeMetadataByUniqueID(1))
	require.NotNil(t, geometry.AttributeMetadataByStringEntry("name", "pos"))
	require.Nil(t, geometry.AttributeMetadataByStringEntry("name", "not_exists"))

	require.NoError(t, geometry.DeleteAttributeMetadataByUniqueID(10))
	require.Nil(t, geometry.AttributeMetadataByUniqueID(10))
}

func TestGeometryMetadataAccessorsReturnClones(t *testing.T) {
	geometry := &GeometryMetadata{}
	attribute := &AttributeMetadata{AttributeUniqueID: 10}
	require.NoError(t, attribute.Element.SetString("name", "pos"))
	require.NoError(t, geometry.AddAttributeMetadata(attribute))

	byID := geometry.AttributeMetadataByUniqueID(10)
	require.NotNil(t, byID)
	require.NoError(t, byID.Element.SetString("name", "changed"))

	byString := geometry.AttributeMetadataByStringEntry("name", "pos")
	require.NotNil(t, byString)
	require.NoError(t, byString.Element.SetString("name", "other"))

	stored := geometry.attributeMetadataByUniqueIDRef(10)
	require.NotNil(t, stored)
	value, ok := stored.Element.String("name")
	require.True(t, ok)
	require.Equal(t, "pos", value)
}

func TestStructuralMetadataAccessorsReturnClones(t *testing.T) {
	structural := &StructuralMetadata{}
	_, err := structural.AddPropertyTable(&PropertyTable{
		Name: "table",
		Properties: []*PropertyTableProperty{
			{Name: "prop"},
		},
	})
	require.NoError(t, err)
	_, err = structural.AddPropertyAttribute(&PropertyAttribute{
		Name: "attribute",
		Properties: []*PropertyAttributeProperty{
			{Name: "prop", AttributeName: "_PROP"},
		},
	})
	require.NoError(t, err)

	table, err := structural.PropertyTable(0)
	require.NoError(t, err)
	table.Name = "changed"
	property, err := table.Property(0)
	require.NoError(t, err)
	property.Name = "changed-prop"

	attribute, err := structural.PropertyAttribute(0)
	require.NoError(t, err)
	attribute.Name = "changed-attr"
	attrProperty, err := attribute.Property(0)
	require.NoError(t, err)
	attrProperty.AttributeName = "_CHANGED"

	storedTable := structural.PropertyTables[0]
	require.Equal(t, "table", storedTable.Name)
	require.Equal(t, "prop", storedTable.Properties[0].Name)
	storedAttribute := structural.PropertyAttributes[0]
	require.Equal(t, "attribute", storedAttribute.Name)
	require.Equal(t, "_PROP", storedAttribute.Properties[0].AttributeName)
}

func TestGeometryMetadataCloneAndEqual(t *testing.T) {
	gm := &GeometryMetadata{}
	attr := &AttributeMetadata{AttributeUniqueID: 3}
	require.NoError(t, attr.Element.SetString("semantic", "normal"))
	require.NoError(t, gm.AddAttributeMetadata(attr))
	require.NoError(t, gm.Root.SetInt("version", 1))
	child := &Element{}
	require.NoError(t, child.SetString("kind", "material"))
	require.NoError(t, gm.Root.SetChild("child", child))

	clone := gm.Clone()
	require.True(t, gm.Equal(clone))

	require.NoError(t, clone.Root.SetInt("version", 2))
	require.False(t, gm.Equal(clone))
	got, ok := gm.Root.Int("version")
	require.True(t, ok)
	require.Equal(t, int32(1), got)
}

func TestGeometryMetadataRoundTrip(t *testing.T) {
	gm := &GeometryMetadata{}
	attribute := &AttributeMetadata{AttributeUniqueID: 7}
	require.NoError(t, attribute.Element.SetString("semantic", "texcoord0"))
	require.NoError(t, attribute.Element.SetBinary("raw", []byte{0x2, 0x4, 0x8}))
	require.NoError(t, gm.AddAttributeMetadata(attribute))
	require.NoError(t, gm.Root.SetString("name", "fixture"))
	require.NoError(t, gm.Root.SetInt("id", 42))
	material := &Element{}
	require.NoError(t, material.SetString("kind", "lambert"))
	require.NoError(t, gm.Root.SetChild("material", material))

	writer := core.NewWriter(0)
	require.NoError(t, EncodeGeometryMetadata(writer, gm))
	decoded, err := DecodeGeometryMetadata(t.Context(), core.NewReader(writer.Bytes()))
	require.NoError(t, err)

	require.Len(t, decoded.Attributes, 1)
	gotName, ok := decoded.Root.String("name")
	require.True(t, ok)
	require.Equal(t, "fixture", gotName)

	gotID, ok := decoded.Root.Int("id")
	require.True(t, ok)
	require.Equal(t, int32(42), gotID)

	gotSemantic, ok := decoded.Attributes[0].Element.String("semantic")
	require.True(t, ok)
	require.Equal(t, "texcoord0", gotSemantic)

	gotRaw, ok := decoded.Attributes[0].Element.Binary("raw")
	require.True(t, ok)
	require.Equal(t, []byte{0x2, 0x4, 0x8}, gotRaw)

	sub := decoded.Root.Child("material")
	require.NotNil(t, sub)
	gotKind, ok := sub.String("kind")
	require.True(t, ok)
	require.Equal(t, "lambert", gotKind)
}

func TestGeometryMetadataEncodeRejectsInvalidInputs(t *testing.T) {
	testCases := []struct {
		name string
		gm   *GeometryMetadata
	}{
		{
			name: "oversized-key",
			gm: &GeometryMetadata{
				Root: Element{
					Entries: []Entry{{
						Key:   string(bytes.Repeat([]byte{'a'}, 256)),
						Value: []byte("value"),
					}},
				},
			},
		},
		{
			name: "empty-entry",
			gm: &GeometryMetadata{
				Root: Element{
					Entries: []Entry{{Key: "empty", Value: nil}},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, EncodeGeometryMetadata(core.NewWriter(0), tc.gm))
		})
	}
}

func TestGeometryMetadataDecodeRejectsInvalidInputs(t *testing.T) {
	t.Run("truncated", func(t *testing.T) {
		_, err := DecodeGeometryMetadata(t.Context(), core.NewReader([]byte{0x01}))
		require.Error(t, err)
	})

	t.Run("zero-sized-value", func(t *testing.T) {
		writer := core.NewWriter(0)
		require.NoError(t, core.EncodeVarUint32(writer, 0))
		require.NoError(t, core.EncodeVarUint32(writer, 1))
		require.NoError(t, writer.WriteUint8(1))
		require.NoError(t, writer.WriteBytes([]byte("a")))
		require.NoError(t, core.EncodeVarUint32(writer, 0))
		require.NoError(t, core.EncodeVarUint32(writer, 0))

		_, err := DecodeGeometryMetadata(t.Context(), core.NewReader(writer.Bytes()))
		require.Error(t, err)
	})
}

func TestNilMetadataQueryHelpers(t *testing.T) {
	var element *Element
	require.Nil(t, element.Child("child"))
	gotInt, ok := element.Int("int")
	require.False(t, ok)
	require.Zero(t, gotInt)
	gotIntArray, ok := element.IntArray("ints")
	require.False(t, ok)
	require.Nil(t, gotIntArray)
	gotFloat64, ok := element.Float64("float")
	require.False(t, ok)
	require.Zero(t, gotFloat64)
	gotFloat64Array, ok := element.Float64Array("floats")
	require.False(t, ok)
	require.Nil(t, gotFloat64Array)
	gotString, ok := element.String("string")
	require.False(t, ok)
	require.Empty(t, gotString)
	gotBinary, ok := element.Binary("binary")
	require.False(t, ok)
	require.Nil(t, gotBinary)
	cloned := element.Clone()
	require.Empty(t, cloned.Entries)
	require.Empty(t, cloned.SubMetadata)

	var geometry *GeometryMetadata
	require.Nil(t, geometry.AttributeMetadataByUniqueID(1))
	require.Nil(t, geometry.AttributeMetadataByStringEntry("name", "value"))
	require.Nil(t, geometry.Clone())

	var propertyTable *PropertyTable
	require.Zero(t, propertyTable.PropertyCount())
	property, err := propertyTable.Property(0)
	require.ErrorIs(t, err, ErrInvalidArgument)
	require.Nil(t, property)
	require.ErrorIs(t, propertyTable.RemoveProperty(0), ErrInvalidArgument)
	require.Nil(t, propertyTable.Clone())

	var propertyAttribute *PropertyAttribute
	require.Zero(t, propertyAttribute.PropertyCount())
	propertyAttr, err := propertyAttribute.Property(0)
	require.ErrorIs(t, err, ErrInvalidArgument)
	require.Nil(t, propertyAttr)
	require.ErrorIs(t, propertyAttribute.RemoveProperty(0), ErrInvalidArgument)
	require.Nil(t, propertyAttribute.Clone())

	var structural *StructuralMetadata
	require.Zero(t, structural.PropertyTableCount())
	require.Zero(t, structural.PropertyAttributeCount())
	table, err := structural.PropertyTable(0)
	require.ErrorIs(t, err, ErrInvalidArgument)
	require.Nil(t, table)
	attribute, err := structural.PropertyAttribute(0)
	require.ErrorIs(t, err, ErrInvalidArgument)
	require.Nil(t, attribute)
	require.ErrorIs(t, structural.RemovePropertyTable(0), ErrInvalidArgument)
	require.ErrorIs(t, structural.RemovePropertyAttribute(0), ErrInvalidArgument)
	require.Nil(t, structural.Clone())
}

func TestGeometryMetadataUniqueIDRoundTripAboveMaxInt32(t *testing.T) {
	uniqueID := uint32(math.MaxInt32) + 7
	geometry := &GeometryMetadata{}
	attribute := &AttributeMetadata{AttributeUniqueID: uniqueID}
	require.NoError(t, attribute.Element.SetString("semantic", "position"))
	require.NoError(t, geometry.AddAttributeMetadata(attribute))

	require.NotNil(t, geometry.AttributeMetadataByUniqueID(uniqueID))
	require.NoError(t, geometry.DeleteAttributeMetadataByUniqueID(uniqueID))
	require.Nil(t, geometry.AttributeMetadataByUniqueID(uniqueID))

	require.NoError(t, geometry.AddAttributeMetadata(attribute))
	writer := core.NewWriter(0)
	require.NoError(t, EncodeGeometryMetadata(writer, geometry))
	decoded, err := DecodeGeometryMetadata(t.Context(), core.NewReader(writer.Bytes()))
	require.NoError(t, err)
	require.NotNil(t, decoded.AttributeMetadataByUniqueID(uniqueID))
}

func TestGeometryMetadataEncodeRejectsMalformedState(t *testing.T) {
	geometry := &GeometryMetadata{
		Attributes: []AttributeMetadata{
			{AttributeUniqueID: 9},
			{AttributeUniqueID: 9},
		},
	}
	writer := core.NewWriter(0)
	err := EncodeGeometryMetadata(writer, geometry)
	require.ErrorIs(t, err, ErrInvalidMetadata)
}

func TestGeometryMetadataDecodeRejectsDuplicateAttributeIDs(t *testing.T) {
	writer := core.NewWriter(0)
	require.NoError(t, core.EncodeVarUint32(writer, 2))
	require.NoError(t, core.EncodeVarUint32(writer, 9))
	require.NoError(t, core.EncodeVarUint32(writer, 0))
	require.NoError(t, core.EncodeVarUint32(writer, 0))
	require.NoError(t, core.EncodeVarUint32(writer, 9))
	require.NoError(t, core.EncodeVarUint32(writer, 0))
	require.NoError(t, core.EncodeVarUint32(writer, 0))
	_, err := DecodeGeometryMetadata(t.Context(), core.NewReader(writer.Bytes()))
	require.ErrorIs(t, err, ErrInvalidMetadata)
}

func TestGeometryMetadataDecodeRejectsDuplicateEntryKeys(t *testing.T) {
	writer := core.NewWriter(0)
	require.NoError(t, core.EncodeVarUint32(writer, 0))
	require.NoError(t, core.EncodeVarUint32(writer, 2))
	require.NoError(t, writer.WriteUint8(1))
	require.NoError(t, writer.WriteBytes([]byte("a")))
	require.NoError(t, core.EncodeVarUint32(writer, 1))
	require.NoError(t, writer.WriteBytes([]byte("x")))
	require.NoError(t, writer.WriteUint8(1))
	require.NoError(t, writer.WriteBytes([]byte("a")))
	require.NoError(t, core.EncodeVarUint32(writer, 1))
	require.NoError(t, writer.WriteBytes([]byte("y")))
	require.NoError(t, core.EncodeVarUint32(writer, 0))
	_, err := DecodeGeometryMetadata(t.Context(), core.NewReader(writer.Bytes()))
	require.ErrorIs(t, err, ErrInvalidMetadata)
}

func TestGeometryMetadataDecodeRejectsDuplicateChildKeys(t *testing.T) {
	writer := core.NewWriter(0)
	require.NoError(t, core.EncodeVarUint32(writer, 0))
	require.NoError(t, core.EncodeVarUint32(writer, 0))
	require.NoError(t, core.EncodeVarUint32(writer, 2))
	require.NoError(t, writer.WriteUint8(1))
	require.NoError(t, writer.WriteBytes([]byte("a")))
	require.NoError(t, core.EncodeVarUint32(writer, 0))
	require.NoError(t, core.EncodeVarUint32(writer, 0))
	require.NoError(t, writer.WriteUint8(1))
	require.NoError(t, writer.WriteBytes([]byte("a")))
	require.NoError(t, core.EncodeVarUint32(writer, 0))
	require.NoError(t, core.EncodeVarUint32(writer, 0))
	_, err := DecodeGeometryMetadata(t.Context(), core.NewReader(writer.Bytes()))
	require.ErrorIs(t, err, ErrInvalidMetadata)
}

func TestGeometryMetadataDecodeRejectsOversizedCounts(t *testing.T) {
	writer := core.NewWriter(0)
	require.NoError(t, core.EncodeVarUint32(writer, math.MaxUint32))
	_, err := DecodeGeometryMetadata(t.Context(), core.NewReader(writer.Bytes()))
	require.ErrorIs(t, err, ErrInvalidMetadata)
}

func TestGeometryMetadataDecodeHonorsCanceledContext(t *testing.T) {
	gm := &GeometryMetadata{}
	require.NoError(t, gm.Root.SetString("name", "fixture"))
	writer := core.NewWriter(0)
	require.NoError(t, EncodeGeometryMetadata(writer, gm))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := DecodeGeometryMetadata(ctx, core.NewReader(writer.Bytes()))
	require.ErrorIs(t, err, context.Canceled)
}

func TestGeometryMetadataDecodeRejectsExcessiveNestingDepth(t *testing.T) {
	root := Element{}
	current := &root
	for depth := 0; depth <= maxDecodedMetadataDepth; depth++ {
		current.SubMetadata = []NamedElement{{
			Key: fmt.Sprintf("child-%d", depth),
		}}
		current = &current.SubMetadata[0].Element
	}

	writer := core.NewWriter(0)
	require.NoError(t, EncodeGeometryMetadata(writer, &GeometryMetadata{Root: root}))

	_, err := DecodeGeometryMetadata(t.Context(), core.NewReader(writer.Bytes()))
	require.ErrorIs(t, err, ErrInvalidMetadata)
}

func TestGeometryMetadataEncodeRejectsOversizedKeys(t *testing.T) {
	geometry := &GeometryMetadata{}
	geometry.Root.Entries = []Entry{{
		Key:   string(bytes.Repeat([]byte{'k'}, math.MaxUint8+1)),
		Value: []byte("x"),
	}}

	writer := core.NewWriter(0)
	err := EncodeGeometryMetadata(writer, geometry)
	require.ErrorIs(t, err, ErrInvalidMetadata)
}

func FuzzDecodeGeometryMetadata(f *testing.F) {
	gm := &GeometryMetadata{}
	attr := &AttributeMetadata{AttributeUniqueID: 7}
	if err := attr.Element.SetString("semantic", "texcoord0"); err != nil {
		panic(err)
	}

	if err := gm.AddAttributeMetadata(attr); err != nil {
		panic(err)
	}

	if err := gm.Root.SetString("name", "fixture"); err != nil {
		panic(err)
	}

	if err := gm.Root.SetInt("id", 42); err != nil {
		panic(err)
	}

	writer := core.NewWriter(0)
	if err := EncodeGeometryMetadata(writer, gm); err == nil {
		f.Add(writer.Bytes())
	}

	f.Add([]byte{0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		if _, err := DecodeGeometryMetadata(t.Context(), core.NewReader(data)); err != nil {
			return
		}
	})
}
