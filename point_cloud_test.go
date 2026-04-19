package draco

import (
	"bytes"
	"testing"

	"github.com/gophics/go-draco/internal/bitstream"
	"github.com/gophics/go-draco/internal/core"
	md "github.com/gophics/go-draco/metadata"
	"github.com/stretchr/testify/require"
)

func TestPointCloudNamedAttributesAndDeletion(t *testing.T) {
	pc := mustNewPointCloud(0)
	attrs := []*Attribute{
		newNamedEmptyAttribute(t, AttributePosition, "Zero"),
		newNamedEmptyAttribute(t, AttributeGeneric, "Zero"),
		newNamedEmptyAttribute(t, AttributeNormal, ""),
		newNamedEmptyAttribute(t, AttributeGeneric, "One"),
		newNamedEmptyAttribute(t, AttributeNormal, ""),
	}
	for _, attr := range attrs {
		addPointCloudAttribute(t, pc, attr)
	}

	require.Equal(t, pc.attribute(0), pc.NamedAttributeByName(AttributePosition, "Zero"))
	require.Equal(t, pc.attribute(3), pc.NamedAttributeByName(AttributeGeneric, "One"))
	require.Equal(t, 4, pc.NamedAttributeID(AttributeNormal, 1))

	require.NoError(t, pc.DeleteAttribute(1))
	require.Equal(t, 4, pc.AttributeCount())
	require.Equal(t, 3, pc.NamedAttributeID(AttributeNormal, 1))

	require.NoError(t, pc.DeleteAttribute(1))
	require.Equal(t, 3, pc.AttributeCount())
	require.Equal(t, 2, pc.NamedAttributeID(AttributeNormal, 0))
}

func TestPointCloudAttributeMetadataHelpers(t *testing.T) {
	pc := mustNewPointCloud(1)
	pos := mustNewFloat32Attribute(AttributePosition, 3, 1)
	setFloat32Value(t, pos, 0, 1, 2, 3)
	posID := addPointCloudAttribute(t, pc, pos)

	generic, err := NewAttribute(AttributeGeneric, DataTypeFloat32, 3, 1)
	require.NoError(t, err)
	setRawValue(t, generic, 0, make([]byte, generic.ByteStride()))
	genID := addPointCloudAttribute(t, pc, generic)

	posMetadata := &md.AttributeMetadata{}
	require.NoError(t, posMetadata.Element.SetString("name", "position"))
	require.NoError(t, pc.AddAttributeMetadata(posID, posMetadata))

	genMetadata := &md.AttributeMetadata{}
	require.NoError(t, genMetadata.Element.SetString("name", "material"))
	require.NoError(t, pc.AddAttributeMetadata(genID, genMetadata))

	require.NotNil(t, pc.AttributeMetadataByStringEntry("name", "position"))
	requested := pc.AttributeMetadataByStringEntry("name", "material")
	require.NotNil(t, requested)
	require.Equal(t, 1, pc.AttributeIDByUniqueID(requested.AttributeUniqueID))
	require.NotNil(t, pc.AttributeMetadata(1))

	require.NoError(t, pc.DeleteAttribute(posID))
	require.Nil(t, pc.AttributeMetadataByStringEntry("name", "position"))
	requested = pc.AttributeMetadataByStringEntry("name", "material")
	require.NotNil(t, requested)
	require.Equal(t, 0, pc.AttributeIDByUniqueID(requested.AttributeUniqueID))
	require.NotNil(t, pc.AttributeMetadata(0))
}

func TestPointCloudAddAttributeMetadataIsAtomic(t *testing.T) {
	pc := mustNewPointCloud(1)
	position := mustNewFloat32Attribute(AttributePosition, 3, 1)
	setFloat32Value(t, position, 0, 1, 2, 3)
	posID := addPointCloudAttribute(t, pc, position)

	require.Nil(t, pc.MetadataClone())

	err := pc.AddAttributeMetadata(posID, nil)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	require.ErrorIs(t, err, md.ErrNilAttributeMetadata)
	require.Nil(t, pc.MetadataClone())
}

func TestPointCloudClone(t *testing.T) {
	pc := newSquarePointCloud(t)
	metadata := &md.GeometryMetadata{}
	require.NoError(t, metadata.Root.SetString("name", "square"))
	require.NoError(t, pc.SetMetadata(metadata))

	clone := pc.Clone()
	requirePointCloudEquivalent(t, pc, clone)
	cloneMetadata := clone.MetadataClone()
	pcMetadata := pc.MetadataClone()
	require.NotSame(t, pcMetadata, cloneMetadata)
	gotName, ok := cloneMetadata.Root.String("name")
	require.True(t, ok)
	require.Equal(t, "square", gotName)
}

func TestPointCloudAccessorsReturnClones(t *testing.T) {
	pc := newSquarePointCloud(t)
	metadata := &md.GeometryMetadata{}
	require.NoError(t, metadata.Root.SetString("name", "square"))
	attrMetadata := &md.AttributeMetadata{AttributeUniqueID: pc.attribute(0).UniqueID}
	require.NoError(t, attrMetadata.Element.SetString("name", "square"))
	require.NoError(t, metadata.AddAttributeMetadata(attrMetadata))
	require.NoError(t, pc.SetMetadata(metadata))

	attr, err := pc.Attribute(0)
	require.NoError(t, err)
	require.NoError(t, attr.SetFloat32(0, 9, 8, 7))
	originalValue, err := pc.attribute(0).Float32(0)
	require.NoError(t, err)
	require.Equal(t, []float32{0, 0, 0}, originalValue)

	attrs := pc.Attributes()
	require.Len(t, attrs, 1)
	attrs[0].Name = "mutated"
	require.Empty(t, pc.attribute(0).Name)

	named := pc.NamedAttribute(AttributePosition)
	require.NotNil(t, named)
	named.Name = "named"
	require.Empty(t, pc.attribute(0).Name)

	namedByName := pc.NamedAttributeByName(AttributePosition, "")
	require.NotNil(t, namedByName)
	namedByName.Name = "named-by-name"
	require.Empty(t, pc.attribute(0).Name)

	byUniqueID := pc.AttributeByUniqueID(pc.attribute(0).UniqueID)
	require.NotNil(t, byUniqueID)
	byUniqueID.Name = "by-unique-id"
	require.Empty(t, pc.attribute(0).Name)

	metadataByID := pc.AttributeMetadata(0)
	require.NotNil(t, metadataByID)
	metadataByID.AttributeUniqueID++
	require.Equal(t, pc.attribute(0).UniqueID, pc.AttributeMetadata(0).AttributeUniqueID)

	metadataByString := pc.AttributeMetadataByStringEntry("name", "square")
	require.NotNil(t, metadataByString)
	metadataByString.AttributeUniqueID++
	require.Equal(t, pc.attribute(0).UniqueID, pc.AttributeMetadataByStringEntry("name", "square").AttributeUniqueID)

	meta := pc.MetadataClone()
	require.NotNil(t, meta)
	require.NoError(t, meta.Root.SetString("name", "mutated"))
	value, ok := pc.MetadataClone().Root.String("name")
	require.True(t, ok)
	require.Equal(t, "square", value)
}

func TestPointCloudSetMetadataRejectsInvalidMetadata(t *testing.T) {
	pc := mustNewPointCloud(1)
	metadata := &md.GeometryMetadata{
		Root: md.Element{
			Entries: []md.Entry{
				{Key: "dup", Value: []byte("a")},
				{Key: "dup", Value: []byte("b")},
			},
		},
	}
	err := pc.SetMetadata(metadata)
	require.ErrorIs(t, err, md.ErrInvalidMetadata)
	require.Nil(t, pc.MetadataClone())
}

func TestPointCloudSetMetadataRejectsUnknownAttributeMetadataReferences(t *testing.T) {
	pc := mustNewPointCloud(1)
	position := mustNewFloat32Attribute(AttributePosition, 3, 1)
	setFloat32Value(t, position, 0, 0, 0, 0)
	addPointCloudAttribute(t, pc, position)

	metadata := &md.GeometryMetadata{}
	require.NoError(t, metadata.AddAttributeMetadata(&md.AttributeMetadata{
		AttributeUniqueID: position.UniqueID + 100,
	}))

	err := pc.SetMetadata(metadata)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	require.Nil(t, pc.MetadataClone())
}

func TestPointCloudSetMetadataClonesIngress(t *testing.T) {
	pc := mustNewPointCloud(1)
	position := mustNewFloat32Attribute(AttributePosition, 3, 1)
	setFloat32Value(t, position, 0, 0, 0, 0)
	addPointCloudAttribute(t, pc, position)

	metadata := &md.GeometryMetadata{}
	require.NoError(t, metadata.Root.SetString("name", "original"))
	require.NoError(t, metadata.AddAttributeMetadata(&md.AttributeMetadata{
		AttributeUniqueID: position.UniqueID,
	}))

	require.NoError(t, pc.SetMetadata(metadata))

	require.NoError(t, metadata.Root.SetString("name", "mutated"))
	metadata.Attributes[0].AttributeUniqueID++

	stored := pc.MetadataClone()
	require.NotNil(t, stored)
	value, ok := stored.Root.String("name")
	require.True(t, ok)
	require.Equal(t, "original", value)
	require.Equal(t, position.UniqueID, stored.Attributes[0].AttributeUniqueID)
}

func TestPointCloudAddAttributeOwnedStillClonesOnRead(t *testing.T) {
	pc := mustNewPointCloud(1)
	position := mustNewFloat32Attribute(AttributePosition, 3, 1)
	setFloat32Value(t, position, 0, 1, 2, 3)

	attrID, err := pc.addAttributeOwned(position)
	require.NoError(t, err)

	readAttr, err := pc.Attribute(attrID)
	require.NoError(t, err)
	require.NoError(t, readAttr.SetFloat32(0, 9, 8, 7))

	original, err := position.Float32(0)
	require.NoError(t, err)
	require.Equal(t, []float32{1, 2, 3}, original)

	uniqueRead := pc.AttributeByUniqueID(position.UniqueID)
	require.NotNil(t, uniqueRead)
	require.NoError(t, uniqueRead.SetFloat32(0, 4, 5, 6))

	stored, err := pc.attribute(attrID).Float32(0)
	require.NoError(t, err)
	require.Equal(t, []float32{1, 2, 3}, stored)
}

func TestDecodeRejectsUnknownAttributeMetadataReferences(t *testing.T) {
	writer := core.NewWriter(0)
	header := bitstream.Header{
		VersionMajor:  bitstream.PointCloudVersionMajor,
		VersionMinor:  bitstream.PointCloudVersionMinor,
		EncoderType:   bitstream.GeometryTypePointCloud,
		EncoderMethod: bitstream.PointCloudSequentialEncoding,
		Flags:         bitstream.MetadataFlagMask,
	}
	require.NoError(t, bitstream.EncodeHeader(writer, header))

	metadata := &md.GeometryMetadata{}
	require.NoError(t, metadata.AddAttributeMetadata(&md.AttributeMetadata{
		AttributeUniqueID: 99,
	}))
	require.NoError(t, md.EncodeGeometryMetadata(writer, metadata))

	require.NoError(t, writer.WriteInt32(1))
	require.NoError(t, writer.WriteUint8(1))
	require.NoError(t, core.EncodeVarUint32(writer, 1))
	require.NoError(t, writer.WriteUint8(uint8(AttributePosition)))
	require.NoError(t, writer.WriteUint8(uint8(DataTypeFloat32)))
	require.NoError(t, writer.WriteUint8(3))
	require.NoError(t, writer.WriteUint8(0))
	require.NoError(t, core.EncodeVarUint32(writer, 1))
	require.NoError(t, writer.WriteUint8(bitstream.SequentialAttributeEncoderGeneric))
	require.NoError(t, writer.WriteBytes(make([]byte, 12)))

	_, err := DecodePointCloud(testContext(t), writer.Bytes())
	require.ErrorIs(t, err, ErrInvalidGeometry)
}

func TestPointCloudEncodeOptionsDriveEncoder(t *testing.T) {
	pc := newSquarePointCloud(t)

	geometryOptions := encodeConfig{}
	geometryOptions.SetAttributeQuantization(AttributePosition, 8)
	geometryOptions.SetCompressionLevel(6)

	manualData := encodePointCloud(t, pc, withEncodeConfig(geometryOptions))
	repeatedData := encodePointCloud(t, pc.Clone(), withEncodeConfig(geometryOptions))
	require.Equal(t, manualData, repeatedData)

	overrideOptions := cloneEncodeConfig(geometryOptions)
	overrideOptions.SetAttributeQuantization(AttributePosition, 10)
	overrideData := encodePointCloud(t, pc, withEncodeConfig(overrideOptions))
	manualOverride := encodePointCloud(t, pc.Clone(), withEncodeConfig(overrideOptions))
	require.Equal(t, overrideData, manualOverride)
	require.False(t, bytes.Equal(manualData, overrideData))
}

func TestPointCloudExtractMappedValidation(t *testing.T) {
	var nilCloud *PointCloud
	_, err := nilCloud.ExtractMappedRaw(0)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	_, err = nilCloud.AppendMappedFloat32(0, nil)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	_, err = nilCloud.AppendMappedInt32(0, nil)
	require.ErrorIs(t, err, ErrInvalidGeometry)

	pc := mustNewPointCloud(1)
	_, err = pc.ExtractMappedRaw(0)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	_, err = pc.ExtractMappedFloat32(0)
	require.ErrorIs(t, err, ErrInvalidGeometry)
	_, err = pc.ExtractMappedInt32(0)
	require.ErrorIs(t, err, ErrInvalidGeometry)
}

func newNamedEmptyAttribute(t *testing.T, attrType AttributeType, name string) *Attribute {
	t.Helper()
	attr, err := NewAttribute(attrType, DataTypeFloat32, 3, 0)
	require.NoError(t, err)
	attr.Name = name
	return attr
}

func newSquarePointCloud(t *testing.T) *PointCloud {
	t.Helper()
	pc := mustNewPointCloud(4)
	position := mustNewFloat32Attribute(AttributePosition, 3, 4)
	for i, xyz := range [][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {1, 1, 0}} {
		setFloat32Value(t, position, i, xyz...)
	}

	addPointCloudAttribute(t, pc, position)
	return pc
}

func TestPointCloudAddAttributeRejectsOutOfRangeMapping(t *testing.T) {
	pc := mustNewPointCloud(2)
	attr, err := NewAttribute(AttributeColor, DataTypeUint8, 3, 1)
	require.NoError(t, err)
	require.NoError(t, attr.SetExplicitMapping(2))
	require.NoError(t, attr.SetPointMapEntry(0, 0))
	require.NoError(t, attr.SetPointMapEntry(1, 1))

	_, err = pc.AddAttribute(attr)
	require.Error(t, err)
}

func TestPointCloudUniqueIDLookupAndEquivalence(t *testing.T) {
	pc := mustNewPointCloud(1)
	pos := mustNewFloat32Attribute(AttributePosition, 3, 1)
	setFloat32Value(t, pos, 0, 1, 2, 3)
	addPointCloudAttribute(t, pc, pos)

	color, err := NewAttribute(AttributeColor, DataTypeUint8, 3, 1)
	require.NoError(t, err)
	setRawValue(t, color, 0, []byte{1, 2, 3})
	addPointCloudAttribute(t, pc, color)

	first := pc.attribute(0)
	second := pc.attribute(1)
	require.NotEqual(t, first.UniqueID, second.UniqueID)
	require.Equal(t, second, pc.AttributeByUniqueID(second.UniqueID))

	other := mustNewPointCloud(1)
	addPointCloudAttribute(t, other, first.Clone())
	addPointCloudAttribute(t, other, second.Clone())
	requirePointCloudEquivalent(t, pc, other)
}

func TestEncodePointCloudRejectsInvalidGeometry(t *testing.T) {
	pc := mustNewPointCloud(1)
	attr := mustNewFloat32Attribute(AttributePosition, 3, 1)
	setFloat32Value(t, attr, 0, 1, 2, 3)
	addPointCloudAttribute(t, pc, attr)
	pc.setPointCount(3)

	_, err := Encode(testContext(t), pc)
	require.ErrorIs(t, err, ErrInvalidGeometry)
}
