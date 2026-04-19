package draco

import (
	"errors"
	"math"
	"testing"

	"github.com/gophics/go-draco/internal/bitstream"
	"github.com/gophics/go-draco/internal/core"
	"github.com/stretchr/testify/require"
)

func TestEncodeToRejectsNilWriter(t *testing.T) {
	mesh := newMeshFromData(t,
		[][]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}},
		[]Face{{0, 1, 2}},
	)

	err := EncodeTo(testContext(t), nil, mesh)
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestEncodeRejectsPointCountsThatDoNotFitTheWireFormat(t *testing.T) {
	pc := newPointCloud(math.MaxInt32 + 1)
	_, err := Encode(testContext(t), pc)
	require.ErrorIs(t, err, ErrInvalidGeometry)

	mesh := newMesh(math.MaxUint32 + 1)
	_, err = Encode(testContext(t), mesh, WithMeshMethod(MeshSequentialEncoding))
	require.ErrorIs(t, err, ErrInvalidGeometry)
}

func TestEncodeRejectsAttributeDescriptorsThatDoNotFitTheWireFormat(t *testing.T) {
	pc := mustNewPointCloud(1)
	attr, err := NewAttribute(AttributePosition, DataTypeFloat32, math.MaxUint8+1, 1)
	require.NoError(t, err)
	addPointCloudAttribute(t, pc, attr)

	_, err = Encode(t.Context(), pc)
	require.ErrorIs(t, err, ErrInvalidGeometry)
}

func TestResetScratchSliceDropsOversizedBackingArray(t *testing.T) {
	oversized := make([]byte, 0, maxPooledScratchRetainedBytes+1)
	require.Nil(t, resetScratchSlice(oversized))

	reusable := make([]byte, 16, 32)
	reset := resetScratchSlice(reusable)
	require.Empty(t, reset)
	require.Equal(t, 32, cap(reset))
}

func TestResetScratchSliceClearDropsReferences(t *testing.T) {
	values := []*int{new(int)}
	reset := resetScratchSliceClear(values)
	require.Empty(t, reset)
	require.Nil(t, values[0])
}

func TestEdgebreakerDecodeScratchResetsAttributeConnectivityBeforeDroppingList(t *testing.T) {
	connectivity := &edgebreakerAttributeConnectivity{
		seamEdges: make([]bool, 0, maxPooledScratchRetainedBytes+1),
	}
	scratch := &edgebreakerDecodeScratch{
		attrConnectivity: []*edgebreakerAttributeConnectivity{connectivity},
	}

	scratch.reset()

	require.Empty(t, scratch.attrConnectivity)
	require.Nil(t, connectivity.seamEdges)
}

func TestDecodeRejectsTruncatedStreams(t *testing.T) {
	testCases := []struct {
		name string
		data []byte
	}{
		{
			name: "point-cloud",
			data: encodePointCloud(t, minimalRobustnessPointCloud(t)),
		},
		{
			name: "mesh",
			data: encodeMesh(t, minimalRobustnessMesh(t)),
		},
		{
			name: "predictive-edgebreaker-mesh",
			data: func() []byte {
				return encodeMesh(t, minimalRobustnessMesh(t),
					WithMeshMethod(MeshEdgebreakerEncoding),
					WithEdgebreakerMethod(EdgebreakerMethodPredictive),
				)
			}(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for i := 0; i < len(tc.data); i++ {
				_, err := Decode(testContext(t), tc.data[:i])
				require.Error(t, err)
			}
		})
	}
}

func TestDecodeRejectsCorruptSequentialAttributeState(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func([]byte) error
		check  func(error) bool
	}{
		{
			name: "zero-attribute-encoder-groups",
			mutate: func(data []byte) error {
				numEncodersPos, _, err := sequentialPointCloudAttributeOffsets(data)
				if err != nil {
					return err
				}

				data[numEncodersPos] = 0
				return nil
			},
			check: func(err error) bool { return errors.Is(err, ErrInvalidGeometry) },
		},
		{
			name: "unknown-sequential-attribute-encoder",
			mutate: func(data []byte) error {
				_, decoderTypePos, err := sequentialPointCloudAttributeOffsets(data)
				if err != nil {
					return err
				}

				data[decoderTypePos] = 255
				return nil
			},
			check: func(err error) bool { return errors.Is(err, ErrUnsupportedFeature) },
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := encodePointCloud(t, minimalRobustnessPointCloud(t))
			require.NoError(t, tc.mutate(data))
			_, err := Decode(testContext(t), data)
			require.True(t, tc.check(err))
		})
	}
}

func TestDecodeSequentialIntegerAttributeRejectsInvalidRawWidth(t *testing.T) {
	predictionNone := bitstream.PredictionNone
	reader := core.NewReader([]byte{
		byte(predictionNone),
		0,
		5,
	})

	original, err := NewAttribute(AttributePosition, DataTypeInt32, 1, 1)
	require.NoError(t, err)
	portable, err := NewAttribute(AttributePosition, DataTypeInt32, 1, 1)
	require.NoError(t, err)

	err = decodeSequentialIntegerAttribute(testContext(t), reader, original, portable, 1, nil, nil, false, nil, nil, nil)
	require.ErrorIs(t, err, ErrInvalidGeometry)
}

func TestDecodeRejectsOversizedSequentialAndEdgebreakerFaceCounts(t *testing.T) {
	t.Run("sequential", func(t *testing.T) {
		writer := core.NewWriter(0)
		require.NoError(t, bitstream.EncodeHeader(writer, bitstream.Header{
			VersionMajor:  bitstream.MeshVersionMajor,
			VersionMinor:  bitstream.MeshVersionMinor,
			EncoderType:   bitstream.GeometryTypeMesh,
			EncoderMethod: bitstream.MeshSequentialEncoding,
		}))
		require.NoError(t, core.EncodeVarUint32(writer, math.MaxUint32))
		require.NoError(t, core.EncodeVarUint32(writer, 3))
		require.NoError(t, writer.WriteUint8(uint8(SequentialConnectivityCompressed)))

		_, err := Decode(testContext(t), writer.Bytes())
		require.ErrorIs(t, err, ErrInvalidGeometry)
	})

	t.Run("edgebreaker", func(t *testing.T) {
		writer := core.NewWriter(0)
		require.NoError(t, bitstream.EncodeHeader(writer, bitstream.Header{
			VersionMajor:  bitstream.MeshVersionMajor,
			VersionMinor:  bitstream.MeshVersionMinor,
			EncoderType:   bitstream.GeometryTypeMesh,
			EncoderMethod: bitstream.MeshEdgebreakerEncoding,
		}))
		require.NoError(t, writer.WriteUint8(edgebreakerTraversalStandard))
		require.NoError(t, core.EncodeVarUint32(writer, 3))
		require.NoError(t, core.EncodeVarUint32(writer, math.MaxUint32))
		require.NoError(t, writer.WriteUint8(0))
		require.NoError(t, core.EncodeVarUint32(writer, 0))
		require.NoError(t, core.EncodeVarUint32(writer, 0))

		_, err := Decode(testContext(t), writer.Bytes())
		require.ErrorIs(t, err, ErrInvalidGeometry)
	})
}

func sequentialPointCloudAttributeOffsets(data []byte) (numEncodersPos, decoderTypePos int, err error) {
	reader := core.NewReader(data)
	header, err := bitstream.DecodeHeader(reader)
	if err != nil {
		return 0, 0, err
	}

	if header.EncoderType != bitstream.GeometryTypePointCloud || header.EncoderMethod != bitstream.PointCloudSequentialEncoding {
		return 0, 0, errors.New("not a sequential point cloud stream")
	}

	if header.Flags&bitstream.MetadataFlagMask != 0 {
		return 0, 0, errors.New("metadata streams not supported in offset helper")
	}

	if _, err := reader.ReadInt32(); err != nil {
		return 0, 0, err
	}

	numEncodersPos = reader.Pos()
	if _, err := reader.ReadUint8(); err != nil {
		return 0, 0, err
	}

	if _, err := core.DecodeVarUint32(reader); err != nil {
		return 0, 0, err
	}

	for i := 0; i < 4; i++ {
		if _, err := reader.ReadUint8(); err != nil {
			return 0, 0, err
		}
	}

	if _, err := core.DecodeVarUint32(reader); err != nil {
		return 0, 0, err
	}

	decoderTypePos = reader.Pos()
	if _, err := reader.ReadUint8(); err != nil {
		return 0, 0, err
	}

	return numEncodersPos, decoderTypePos, nil
}

func minimalRobustnessPointCloud(t *testing.T) *PointCloud {
	t.Helper()

	pc := mustNewPointCloud(2)
	pos := mustNewFloat32Attribute(AttributePosition, 3, 2)
	setFloat32Value(t, pos, 0, 1, 2, 3)
	setFloat32Value(t, pos, 1, 4, 5, 6)
	addPointCloudAttribute(t, pc, pos)
	return pc
}

func minimalRobustnessMesh(t *testing.T) *Mesh {
	t.Helper()

	mesh := mustNewMesh(3)
	pos := mustNewFloat32Attribute(AttributePosition, 3, 3)
	for i, xyz := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}} {
		setFloat32Value(t, pos, i, xyz[:]...)
	}

	addMeshAttribute(t, mesh, pos)
	addFace(t, mesh, Face{0, 1, 2})
	return mesh
}

func TestHeaderRoundTrip(t *testing.T) {
	writer := core.NewWriter(0)
	header := bitstream.Header{
		VersionMajor:  bitstream.PointCloudVersionMajor,
		VersionMinor:  bitstream.PointCloudVersionMinor,
		EncoderType:   bitstream.GeometryTypePointCloud,
		EncoderMethod: bitstream.PointCloudSequentialEncoding,
		Flags:         bitstream.MetadataFlagMask,
	}
	require.NoError(t, bitstream.EncodeHeader(writer, header))

	got, err := bitstream.DecodeHeader(core.NewReader(writer.Bytes()))
	require.NoError(t, err)
	require.Equal(t, header, got)
}

func TestDetectGeometry(t *testing.T) {
	pc := mustNewPointCloud(1)
	attr := mustNewFloat32Attribute(AttributePosition, 3, 1)
	setFloat32Value(t, attr, 0, 1, 2, 3)
	addPointCloudAttribute(t, pc, attr)

	data := encodePointCloud(t, pc)
	got, err := DetectGeometry(data)
	require.NoError(t, err)
	require.Equal(t, PointCloudGeometry, got)
}

func TestHeaderRelatedErrors(t *testing.T) {
	testCases := []struct {
		name  string
		run   func() error
		check func(error) bool
	}{
		{
			name:  "geometry-type-invalid-header",
			run:   func() error { _, err := DetectGeometry([]byte("NOPE")); return err },
			check: func(err error) bool { return errors.Is(err, ErrInvalidHeader) },
		},
		{
			name:  "decode-invalid-header",
			run:   func() error { _, err := Decode(testContext(t), []byte("bad")); return err },
			check: func(err error) bool { return errors.Is(err, ErrInvalidHeader) },
		},
		{
			name:  "geometry-type-truncated-header",
			run:   func() error { _, err := DetectGeometry([]byte("DRACO")); return err },
			check: func(err error) bool { return errors.Is(err, ErrInvalidHeader) },
		},
		{
			name: "decode-unsupported-version",
			run: func() error {
				writer := core.NewWriter(0)
				header := bitstream.Header{
					VersionMajor:  bitstream.MeshVersionMajor,
					VersionMinor:  bitstream.MeshVersionMinor + 1,
					EncoderType:   bitstream.GeometryTypeMesh,
					EncoderMethod: bitstream.MeshSequentialEncoding,
				}
				require.NoError(t, bitstream.EncodeHeader(writer, header))
				_, err := Decode(testContext(t), writer.Bytes())
				return err
			},
			check: func(err error) bool { return errors.Is(err, ErrUnsupportedVersion) },
		},
		{
			name: "decode-unknown-encoder-type",
			run: func() error {
				writer := core.NewWriter(0)
				header := bitstream.Header{
					VersionMajor:  bitstream.MeshVersionMajor,
					VersionMinor:  bitstream.MeshVersionMinor,
					EncoderType:   99,
					EncoderMethod: bitstream.MeshSequentialEncoding,
				}
				require.NoError(t, bitstream.EncodeHeader(writer, header))
				_, err := Decode(testContext(t), writer.Bytes())
				return err
			},
			check: func(err error) bool { return errors.Is(err, ErrUnsupportedVersion) },
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.True(t, tc.check(tc.run()))
		})
	}
}

func TestValidateVersionLegacyMatrix(t *testing.T) {
	tests := []bitstream.Header{
		{VersionMajor: 1, VersionMinor: 0, EncoderType: bitstream.GeometryTypeMesh},
		{VersionMajor: 1, VersionMinor: 1, EncoderType: bitstream.GeometryTypePointCloud},
	}
	for _, header := range tests {
		require.NoError(t, bitstream.ValidateVersion(header))
	}
}
