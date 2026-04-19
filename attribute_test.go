package draco

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAttributeConstructorsRejectInvalidInput(t *testing.T) {
	testCases := []struct {
		name  string
		build func() (*Attribute, error)
	}{
		{
			name: "new-attribute-zero-components",
			build: func() (*Attribute, error) {
				return NewAttribute(AttributeGeneric, DataTypeFloat32, 0, 1)
			},
		},
		{
			name: "new-attribute-invalid-datatype",
			build: func() (*Attribute, error) {
				return NewAttribute(AttributeGeneric, DataTypeInvalid, 1, 1)
			},
		},
		{
			name: "new-attribute-negative-entries",
			build: func() (*Attribute, error) {
				return NewAttribute(AttributeGeneric, DataTypeFloat32, 1, -1)
			},
		},
		{
			name: "new-raw-attribute-misaligned-length",
			build: func() (*Attribute, error) {
				return NewRawAttribute(AttributeGeneric, DataTypeUint8, 2, []byte{1, 2, 3})
			},
		},
		{
			name: "new-float32-attribute-misaligned-length",
			build: func() (*Attribute, error) {
				return NewFloat32Attribute(AttributePosition, 3, []float32{1, 2, 3, 4})
			},
		},
		{
			name: "new-bool-attribute-misaligned-length",
			build: func() (*Attribute, error) {
				return NewBoolAttribute(AttributeGeneric, 2, []bool{true})
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.build()
			require.ErrorIs(t, err, ErrInvalidGeometry)
		})
	}
}

func TestNewRawAttributeCopiesIngress(t *testing.T) {
	raw := []byte{1, 2, 3, 4}
	attr, err := NewRawAttribute(AttributeGeneric, DataTypeUint8, 2, raw)
	require.NoError(t, err)

	raw[0] = 9

	got, err := attr.RawValue(0)
	require.NoError(t, err)
	require.Equal(t, []byte{1, 2}, got)
}

func TestTypedAttributeConstructorsCoverIntegerWidths(t *testing.T) {
	tests := []struct {
		name        string
		dataType    DataType
		constructor func() (*Attribute, error)
	}{
		{
			name:     "int8",
			dataType: DataTypeInt8,
			constructor: func() (*Attribute, error) {
				return NewInt8Attribute(AttributeGeneric, 2, []int8{-1, 2, 3, 4})
			},
		},
		{
			name:     "uint8",
			dataType: DataTypeUint8,
			constructor: func() (*Attribute, error) {
				return NewUint8Attribute(AttributeColor, 3, []uint8{1, 2, 3, 4, 5, 6})
			},
		},
		{
			name:     "int16",
			dataType: DataTypeInt16,
			constructor: func() (*Attribute, error) {
				return NewInt16Attribute(AttributeGeneric, 2, []int16{-1, 2, 3, 4})
			},
		},
		{
			name:     "uint16",
			dataType: DataTypeUint16,
			constructor: func() (*Attribute, error) {
				return NewUint16Attribute(AttributeGeneric, 2, []uint16{1, 2, 3, 4})
			},
		},
		{
			name:     "uint32",
			dataType: DataTypeUint32,
			constructor: func() (*Attribute, error) {
				return NewUint32Attribute(AttributeGeneric, 2, []uint32{1, 2, 3, 4})
			},
		},
		{
			name:     "uint64",
			dataType: DataTypeUint64,
			constructor: func() (*Attribute, error) {
				return NewUint64Attribute(AttributeGeneric, 2, []uint64{1, 2, 3, 4})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attr, err := tc.constructor()
			require.NoError(t, err)
			require.Equal(t, tc.dataType, attr.DataType)
			require.Equal(t, 2, attr.EntryCount())
		})
	}
}

func TestAttributeRawValueAndMapping(t *testing.T) {
	attr, err := NewAttribute(AttributeColor, DataTypeUint8, 4, 2)
	require.NoError(t, err)
	require.NoError(t, attr.SetExplicitMapping(2))
	require.NoError(t, attr.SetPointMapEntry(0, 1))
	require.NoError(t, attr.SetPointMapEntry(1, 0))
	setRawValue(t, attr, 0, []byte{1, 2, 3, 4})
	setRawValue(t, attr, 1, []byte{5, 6, 7, 8})
	requireRawEntry(t, attr, int(attr.mappedIndex(0)), []byte{5, 6, 7, 8})
}

func TestAttributeFloat32Conversions(t *testing.T) {
	testCases := []struct {
		name  string
		build func(*testing.T) *Attribute
		want  []float32
	}{
		{
			name: "normalized-uint8",
			build: func(t *testing.T) *Attribute {
				t.Helper()
				attr, err := NewAttribute(AttributeColor, DataTypeUint8, 2, 1)
				require.NoError(t, err)
				attr.Normalized = true
				setRawValue(t, attr, 0, []byte{0, 255})
				return attr
			},
			want: []float32{0, 1},
		},
		{
			name: "normalized-int16",
			build: func(t *testing.T) *Attribute {
				t.Helper()
				attr, err := NewAttribute(AttributeGeneric, DataTypeInt16, 2, 1)
				require.NoError(t, err)
				attr.Normalized = true
				setInt32Value(t, attr, 0, -32768, 32767)
				return attr
			},
			want: []float32{-1, 1},
		},
		{
			name: "bool",
			build: func(t *testing.T) *Attribute {
				t.Helper()
				attr, err := NewAttribute(AttributeGeneric, DataTypeBool, 2, 1)
				require.NoError(t, err)
				setInt32Value(t, attr, 0, 0, 3)
				return attr
			},
			want: []float32{0, 1},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			requireFloat32Entry(t, tc.build(t), 0, tc.want, 0)
		})
	}
}

func TestAttributeNilSafetyAndCloneBoundaries(t *testing.T) {
	var attr *Attribute
	require.False(t, attr.IsIdentityMapping())
	require.Zero(t, attr.ByteStride())
	require.Zero(t, attr.EntryCount())
	require.Zero(t, attr.MappingSize())
	require.Nil(t, attr.Clone())
	require.Nil(t, attr.ExtractRaw())
	require.Nil(t, attr.AppendRaw(nil))

	_, err := attr.RawValue(0)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	_, err = attr.Float32(0)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	_, err = attr.Int32(0)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	err = attr.SetIdentityMapping()
	require.ErrorIs(t, err, ErrInvalidGeometry)
	err = attr.SetExplicitMapping(1)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	err = attr.SetPointMapEntry(0, 0)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	err = attr.SetRawValue(0, []byte{1})
	require.ErrorIs(t, err, ErrInvalidGeometry)
	err = attr.SetFloat32(0, 1)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	err = attr.SetInt32(0, []int32{1})
	require.ErrorIs(t, err, ErrInvalidGeometry)
	_, err = attr.ExtractFloat32()
	require.ErrorIs(t, err, ErrInvalidGeometry)
	_, err = attr.ExtractInt32()
	require.ErrorIs(t, err, ErrInvalidGeometry)
	_, err = attr.AppendFloat32(nil)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	_, err = attr.AppendInt32(nil)
	require.ErrorIs(t, err, ErrInvalidGeometry)

	original := mustNewFloat32Attribute(AttributePosition, 2, 2)
	setFloat32Value(t, original, 0, 1, 2)
	setFloat32Value(t, original, 1, 3, 4)
	require.NoError(t, original.SetExplicitMapping(2))
	require.NoError(t, original.SetPointMapEntry(0, 1))
	require.NoError(t, original.SetPointMapEntry(1, 0))

	clone := original.Clone()
	require.NotSame(t, original, clone)
	require.True(t, original.Equivalent(clone))

	require.NoError(t, clone.SetFloat32(0, 9, 8))
	require.NoError(t, clone.SetPointMapEntry(0, 0))

	originalValue, err := original.Float32(0)
	require.NoError(t, err)
	require.Equal(t, []float32{1, 2}, originalValue)
	require.Equal(t, uint32(1), original.mapping[0])
}

func TestAttributeEquivalent(t *testing.T) {
	left := mustNewFloat32Attribute(AttributePosition, 3, 1)
	setFloat32Value(t, left, 0, 1, 2, 3)
	left.UniqueID = 7

	right := left.Clone()
	require.True(t, left.Equivalent(right))

	setFloat32Value(t, right, 0, 4, 5, 6)
	require.False(t, left.Equivalent(right))
}

func TestAttributeViewConversionsAndRawCopies(t *testing.T) {
	floatAttr := mustNewFloat32Attribute(AttributePosition, 2, 2)
	setFloat32Value(t, floatAttr, 0, 1, 2)
	setFloat32Value(t, floatAttr, 1, 3, 4)

	floatView := AttributeView{
		descriptor: floatAttr.Descriptor(),
		data:       floatAttr.ExtractRaw(),
	}
	raw := floatView.RawData()
	raw[0] = 99
	require.Equal(t, floatAttr.ExtractRaw(), floatView.RawData())

	floatValues, err := floatView.Float32()
	require.NoError(t, err)
	require.Equal(t, []float32{1, 2, 3, 4}, floatValues)

	intAttr, err := NewInt32Attribute(AttributeGeneric, 2, []int32{5, 6, 7, 8})
	require.NoError(t, err)
	intView := AttributeView{
		descriptor: intAttr.Descriptor(),
		data:       intAttr.ExtractRaw(),
	}
	intValues, err := intView.Int32()
	require.NoError(t, err)
	require.Equal(t, []int32{5, 6, 7, 8}, intValues)

	testCases := []struct {
		name string
		view AttributeView
	}{
		{
			name: "zero-components",
			view: AttributeView{
				descriptor: AttributeDescriptor{
					DataType:      DataTypeFloat32,
					NumComponents: 0,
				},
			},
		},
		{
			name: "invalid-datatype",
			view: AttributeView{
				descriptor: AttributeDescriptor{
					DataType:      DataTypeInvalid,
					NumComponents: 2,
				},
				data: []byte{1, 2, 3, 4},
			},
		},
		{
			name: "misaligned-length",
			view: AttributeView{
				descriptor: AttributeDescriptor{
					DataType:      DataTypeFloat32,
					NumComponents: 2,
				},
				data: []byte{1, 2, 3},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.view.Float32()
			require.ErrorIs(t, err, ErrInvalidGeometry)
			_, err = tc.view.Int32()
			require.ErrorIs(t, err, ErrInvalidGeometry)
		})
	}
}

func TestGeometryViewConstructionRejectsNilInputs(t *testing.T) {
	var pc *PointCloud
	_, err := NewPointCloudView(pc)
	require.ErrorIs(t, err, ErrInvalidGeometry)

	var mesh *Mesh
	_, err = NewMeshView(mesh)
	require.ErrorIs(t, err, ErrInvalidGeometry)
}

func TestGeometryViewsExposeSnapshotsByUniqueID(t *testing.T) {
	pc := mustNewPointCloud(2)
	position := mustNewFloat32Attribute(AttributePosition, 3, 2)
	position.UniqueID = 42
	setFloat32Value(t, position, 0, 0, 0, 0)
	setFloat32Value(t, position, 1, 1, 0, 0)
	addPointCloudAttribute(t, pc, position)

	pcView, err := NewPointCloudView(pc)
	require.NoError(t, err)
	require.NotNil(t, pcView.AttributeByUniqueID(42))

	mesh := newMeshFromData(t,
		[][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}},
		[]Face{{0, 1, 2}},
	)
	mesh.attribute(0).UniqueID = 77

	meshView, err := NewMeshView(mesh)
	require.NoError(t, err)
	require.Equal(t, 1, meshView.FaceCount())

	face, err := meshView.Face(0)
	require.NoError(t, err)
	require.Equal(t, Face{0, 1, 2}, face)
	require.Len(t, meshView.Attributes(), 1)
	require.NotNil(t, meshView.AttributeByUniqueID(77))
}
