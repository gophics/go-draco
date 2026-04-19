package draco

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testContext(tb testing.TB) context.Context {
	tb.Helper()
	if deadlineTB, ok := any(tb).(interface{ Deadline() (time.Time, bool) }); ok {
		if deadline, ok := deadlineTB.Deadline(); ok {
			ctx, cancel := context.WithDeadline(tb.Context(), deadline)
			tb.Cleanup(cancel)
			return ctx
		}
	}

	ctx, cancel := context.WithCancel(tb.Context())
	tb.Cleanup(cancel)
	return ctx
}

func requireFloat32SliceInDelta(t *testing.T, expected, actual []float32, delta float64, msgAndArgs ...any) {
	t.Helper()
	require.Len(t, actual, len(expected), msgAndArgs...)
	for i := range expected {
		require.InDeltaf(t, expected[i], actual[i], delta, "component %d", i)
	}
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.FromSlash(path))
	require.NoError(t, err)
	return data
}

func mustNewFloat32Attribute(attType AttributeType, numComponents, numEntries int) *Attribute {
	attr, err := NewAttribute(attType, DataTypeFloat32, numComponents, numEntries)
	if err != nil {
		panic(err)
	}

	return attr
}

func addPointCloudAttribute(tb testing.TB, pc *PointCloud, attr *Attribute) int {
	tb.Helper()
	id, err := pc.AddAttribute(attr)
	require.NoError(tb, err)
	return id
}

func addMeshAttribute(tb testing.TB, mesh *Mesh, attr *Attribute) int {
	tb.Helper()
	id, err := mesh.AddAttribute(attr)
	require.NoError(tb, err)
	return id
}

func addFace(tb testing.TB, mesh *Mesh, face Face) {
	tb.Helper()
	require.NoError(tb, mesh.AddFace(face))
}

func setFloat32Value(tb testing.TB, attr *Attribute, entry int, values ...float32) {
	tb.Helper()
	require.NoError(tb, attr.SetFloat32(entry, values...))
}

func setInt32Value(tb testing.TB, attr *Attribute, entry int, values ...int32) {
	tb.Helper()
	require.NoError(tb, attr.SetInt32(entry, values))
}

func setRawValue(tb testing.TB, attr *Attribute, entry int, raw []byte) {
	tb.Helper()
	require.NoError(tb, attr.SetRawValue(entry, raw))
}

func requirePointCloudCounts(tb testing.TB, pc *PointCloud, wantPoints, wantAttrs int) {
	tb.Helper()
	require.Equal(tb, wantPoints, pc.PointCount())
	require.Equal(tb, wantAttrs, pc.AttributeCount())
}

func requireMeshCounts(tb testing.TB, mesh *Mesh, wantFaces, wantPoints, wantAttrs int) {
	tb.Helper()
	require.Equal(tb, wantFaces, mesh.FaceCount())
	require.Equal(tb, wantPoints, mesh.PointCount())
	require.Equal(tb, wantAttrs, mesh.AttributeCount())
}

func requireAttributeSchema(tb testing.TB, attr *Attribute, wantType AttributeType, wantDataType DataType, wantComponents int) {
	tb.Helper()
	require.NotNil(tb, attr)
	require.Equal(tb, wantType, attr.Type)
	require.Equal(tb, wantDataType, attr.DataType)
	require.Equal(tb, wantComponents, attr.NumComponents)
}

func requireFloat32Entry(t *testing.T, attr *Attribute, entry int, want []float32, delta float64) {
	t.Helper()
	got, err := attr.Float32(entry)
	require.NoError(t, err)
	requireFloat32SliceInDelta(t, want, got, delta)
}

func requireInt32Entry(tb testing.TB, attr *Attribute, entry int, want []int32) {
	tb.Helper()
	got, err := attr.Int32(entry)
	require.NoError(tb, err)
	require.Equal(tb, want, got)
}

func requireRawEntry(tb testing.TB, attr *Attribute, entry int, want []byte) {
	tb.Helper()
	got, err := attr.RawValue(entry)
	require.NoError(tb, err)
	require.Equal(tb, want, got)
}

func requireDeterministicEncode(tb testing.TB, first, second []byte) {
	tb.Helper()
	require.Equal(tb, first, second, "encoding is not deterministic")
}

func requireMeshEquivalent(tb testing.TB, want, got *Mesh) {
	tb.Helper()
	require.True(tb, want.Equivalent(got))
}

func requirePointCloudEquivalent(tb testing.TB, want, got *PointCloud) {
	tb.Helper()
	require.True(tb, want.Equivalent(got))
}

func requirePointCloudAttribute(tb testing.TB, pc *PointCloud, attrType AttributeType, wantDataType DataType, wantComponents int) *Attribute {
	tb.Helper()
	attr := pc.NamedAttribute(attrType)
	requireAttributeSchema(tb, attr, attrType, wantDataType, wantComponents)
	return attr
}

func requireMeshAttribute(tb testing.TB, mesh *Mesh, attrType AttributeType, wantDataType DataType, wantComponents int) *Attribute {
	tb.Helper()
	attr := mesh.NamedAttribute(attrType)
	requireAttributeSchema(tb, attr, attrType, wantDataType, wantComponents)
	return attr
}

func mustNewPointCloud(numPoints int) *PointCloud {
	return newPointCloud(numPoints)
}

func mustNewMesh(numPoints int) *Mesh {
	return newMesh(numPoints)
}

func withEncodeConfig(cfg encodeConfig) EncodeOption {
	return func(dst *encodeConfig) error {
		*dst = mergeEncodeConfig(*dst, cfg)
		return nil
	}
}

func encodePointCloud(t *testing.T, pc *PointCloud, opts ...EncodeOption) []byte {
	t.Helper()
	data, err := Encode(testContext(t), pc, opts...)
	require.NoError(t, err)
	return data
}

func decodePointCloud(t *testing.T, data []byte, opts ...DecodeOption) *PointCloud {
	t.Helper()
	pc, err := DecodePointCloud(testContext(t), data, opts...)
	require.NoError(t, err)
	return pc
}

func encodeMesh(t *testing.T, mesh *Mesh, opts ...EncodeOption) []byte {
	t.Helper()
	data, err := Encode(testContext(t), mesh, opts...)
	require.NoError(t, err)
	return data
}

func decodeMesh(t *testing.T, data []byte, opts ...DecodeOption) *Mesh {
	t.Helper()
	mesh, err := DecodeMesh(testContext(t), data, opts...)
	require.NoError(t, err)
	return mesh
}

func encodeWithStats(t *testing.T, g Geometry, opts ...EncodeOption) EncodeResult {
	t.Helper()
	result, err := EncodeWithStats(testContext(t), g, opts...)
	require.NoError(t, err)
	return result
}
