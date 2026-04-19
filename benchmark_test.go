package draco

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkDecodePointCloudSequentialFixture(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/point_cloud_sequential.drc")
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	defer b.ReportMetric(float64(len(data)), "encoded_B/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodePointCloud(ctx, data); err != nil {
			b.Fatalf("DecodePointCloud() error = %v", err)
		}
	}
}

func BenchmarkDecodePointCloudSequentialFixtureFromReader(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/point_cloud_sequential.drc")
	reader := bytes.NewReader(data)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	defer b.ReportMetric(float64(len(data)), "encoded_B/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader.Reset(data)
		if _, err := DecodeFrom(ctx, reader); err != nil {
			b.Fatalf("DecodeFrom() error = %v", err)
		}
	}
}

func BenchmarkInspectPointCloudSequentialFixture(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/point_cloud_sequential.drc")
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Inspect(ctx, data); err != nil {
			b.Fatalf("Inspect() error = %v", err)
		}
	}
}

func BenchmarkInspectPointCloudSequentialFixtureFromReader(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/point_cloud_sequential.drc")
	reader := bytes.NewReader(data)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader.Reset(data)
		if _, err := InspectFrom(ctx, reader); err != nil {
			b.Fatalf("InspectFrom() error = %v", err)
		}
	}
}

func BenchmarkDecodePointCloudSequentialFixtureReusableDecoder(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/point_cloud_sequential.drc")
	decoder, err := NewDecoder()
	if err != nil {
		b.Fatalf("NewDecoder() error = %v", err)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	defer b.ReportMetric(float64(len(data)), "encoded_B/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := decoder.DecodePointCloud(ctx, data); err != nil {
			b.Fatalf("Decoder.DecodePointCloud() error = %v", err)
		}
	}
}

func BenchmarkDecodeMeshSequentialFixture(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/mesh_sequential.drc")
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	defer b.ReportMetric(float64(len(data)), "encoded_B/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodeMesh(ctx, data); err != nil {
			b.Fatalf("DecodeMesh() error = %v", err)
		}
	}
}

func BenchmarkDecodeMeshSequentialFixtureFromReader(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/mesh_sequential.drc")
	reader := bytes.NewReader(data)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	defer b.ReportMetric(float64(len(data)), "encoded_B/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader.Reset(data)
		if _, err := DecodeFrom(ctx, reader); err != nil {
			b.Fatalf("DecodeFrom() error = %v", err)
		}
	}
}

func BenchmarkDecodeMeshSequentialFixtureReusableDecoder(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/mesh_sequential.drc")
	decoder, err := NewDecoder()
	if err != nil {
		b.Fatalf("NewDecoder() error = %v", err)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	defer b.ReportMetric(float64(len(data)), "encoded_B/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := decoder.DecodeMesh(ctx, data); err != nil {
			b.Fatalf("Decoder.DecodeMesh() error = %v", err)
		}
	}
}

func BenchmarkDecodeMeshSequentialFixtureReusableDecoderFromReader(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/mesh_sequential.drc")
	reader := bytes.NewReader(data)
	decoder, err := NewDecoder()
	if err != nil {
		b.Fatalf("NewDecoder() error = %v", err)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	defer b.ReportMetric(float64(len(data)), "encoded_B/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader.Reset(data)
		if _, err := decoder.DecodeFrom(ctx, reader); err != nil {
			b.Fatalf("Decoder.DecodeFrom() error = %v", err)
		}
	}
}

func BenchmarkInspectMeshSequentialFixtureFromReader(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/mesh_sequential.drc")
	reader := bytes.NewReader(data)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader.Reset(data)
		if _, err := InspectFrom(ctx, reader); err != nil {
			b.Fatalf("InspectFrom() error = %v", err)
		}
	}
}

func BenchmarkEncodeMeshSequentialFixture(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/mesh_sequential.drc")
	mesh, err := DecodeMesh(ctx, data)
	if err != nil {
		b.Fatalf("DecodeMesh() error = %v", err)
	}

	encoded, err := Encode(ctx, mesh)
	if err != nil {
		b.Fatalf("Encode() preflight error = %v", err)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(encoded)))
	defer b.ReportMetric(float64(len(encoded)), "encoded_B/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Encode(ctx, mesh); err != nil {
			b.Fatalf("EncodeMesh() error = %v", err)
		}
	}
}

func BenchmarkDecodePointCloudKDTreeFixture(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/point_cloud_kd_tree.drc")
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	defer b.ReportMetric(float64(len(data)), "encoded_B/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodePointCloud(ctx, data); err != nil {
			b.Fatalf("DecodePointCloud(kd-tree) error = %v", err)
		}
	}
}

func BenchmarkEncodePointCloudKDTreeFixture(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/point_cloud_kd_tree.drc")
	pc, err := DecodePointCloud(ctx, data)
	if err != nil {
		b.Fatalf("DecodePointCloud() error = %v", err)
	}

	opts := []EncodeOption{
		WithPointCloudMethod(PointCloudKDTreeEncoding),
		WithAttributeQuantization(AttributePosition, 14),
	}
	encoded, err := Encode(ctx, pc, opts...)
	if err != nil {
		b.Fatalf("Encode() preflight error = %v", err)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(encoded)))
	defer b.ReportMetric(float64(len(encoded)), "encoded_B/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Encode(ctx, pc, opts...); err != nil {
			b.Fatalf("EncodePointCloud(kd-tree) error = %v", err)
		}
	}
}

func BenchmarkExtractMappedFloat32(b *testing.B) {
	b.ReportAllocs()
	pc, attrID := benchmarkMappedExtractionFixture(b)
	b.SetBytes(int64(pc.PointCount() * pc.attribute(attrID).NumComponents * 4))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := pc.ExtractMappedFloat32(attrID); err != nil {
			b.Fatalf("ExtractMappedFloat32() error = %v", err)
		}
	}
}

func BenchmarkAppendMappedFloat32Reuse(b *testing.B) {
	b.ReportAllocs()
	pc, attrID := benchmarkMappedExtractionFixture(b)
	attr := pc.attribute(attrID)
	dst := make([]float32, 0, pc.PointCount()*attr.NumComponents)
	b.SetBytes(int64(pc.PointCount() * attr.NumComponents * 4))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst = dst[:0]
		var err error
		dst, err = pc.AppendMappedFloat32(attrID, dst)
		if err != nil {
			b.Fatalf("AppendMappedFloat32() error = %v", err)
		}
	}
}

func BenchmarkAppendMappedRawReuse(b *testing.B) {
	b.ReportAllocs()
	pc, attrID := benchmarkMappedExtractionFixture(b)
	attr := pc.attribute(attrID)
	dst := make([]byte, 0, pc.PointCount()*attr.ByteStride())
	b.SetBytes(int64(pc.PointCount() * attr.ByteStride()))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst = dst[:0]
		var err error
		dst, err = pc.AppendMappedRaw(attrID, dst)
		if err != nil {
			b.Fatalf("AppendMappedRaw() error = %v", err)
		}
	}
}

func BenchmarkAppendMappedInt32Reuse(b *testing.B) {
	pc, attrID := benchmarkMappedExtractionFixture(b)
	attr := pc.attribute(attrID)
	dst := make([]int32, 0, pc.PointCount()*attr.NumComponents)
	b.ReportAllocs()
	b.SetBytes(int64(pc.PointCount() * attr.NumComponents * 4))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst = dst[:0]
		var err error
		dst, err = pc.AppendMappedInt32(attrID, dst)
		if err != nil {
			b.Fatalf("AppendMappedInt32() error = %v", err)
		}
	}
}

func BenchmarkDecodeMeshEdgebreakerFixture(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/mesh_edgebreaker.drc")
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	defer b.ReportMetric(float64(len(data)), "encoded_B/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodeMesh(ctx, data); err != nil {
			b.Fatalf("DecodeMesh(edgebreaker) error = %v", err)
		}
	}
}

func BenchmarkEncodeMeshEdgebreakerFixture(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/mesh_edgebreaker.drc")
	mesh, err := DecodeMesh(ctx, data)
	if err != nil {
		b.Fatalf("DecodeMesh() error = %v", err)
	}

	encoded, err := Encode(ctx, mesh, WithMeshMethod(MeshEdgebreakerEncoding))
	if err != nil {
		b.Fatalf("Encode() preflight error = %v", err)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(encoded)))
	defer b.ReportMetric(float64(len(encoded)), "encoded_B/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Encode(ctx, mesh, WithMeshMethod(MeshEdgebreakerEncoding)); err != nil {
			b.Fatalf("EncodeMesh(edgebreaker) error = %v", err)
		}
	}
}

func BenchmarkEncodeMeshEdgebreakerNativeOrderFixture(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/mesh_edgebreaker.drc")
	mesh, err := DecodeMesh(ctx, data)
	if err != nil {
		b.Fatalf("DecodeMesh() error = %v", err)
	}

	opts := []EncodeOption{
		WithMeshMethod(MeshEdgebreakerEncoding),
		WithSpeed(10, 10),
	}
	encoded, err := Encode(ctx, mesh, opts...)
	if err != nil {
		b.Fatalf("Encode() preflight error = %v", err)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(encoded)))
	defer b.ReportMetric(float64(len(encoded)), "encoded_B/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Encode(ctx, mesh, opts...); err != nil {
			b.Fatalf("EncodeMesh(edgebreaker native order) error = %v", err)
		}
	}
}

func BenchmarkCanonicalizeEdgebreakerWorkingMesh(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/mesh_edgebreaker.drc")
	mesh, err := DecodeMesh(ctx, data)
	if err != nil {
		b.Fatalf("DecodeMesh() error = %v", err)
	}

	scratch := &edgebreakerEncodeScratch{}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, _, err := canonicalizeEdgebreakerWorkingMesh(mesh, true, scratch); err != nil {
			b.Fatalf("canonicalizeEdgebreakerWorkingMesh() error = %v", err)
		}
	}
}

func BenchmarkInspectMeshSequentialFixture(b *testing.B) {
	ctx := testContext(b)
	data := benchmarkReadFixture(b, "testdata/mesh_sequential.drc")
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Inspect(ctx, data); err != nil {
			b.Fatalf("Inspect() error = %v", err)
		}
	}
}

func benchmarkMappedExtractionFixture(b *testing.B) (*PointCloud, int) {
	b.Helper()
	pc := mustNewPointCloud(2048)
	position := mustNewFloat32Attribute(AttributePosition, 3, 1024)
	for entry := 0; entry < position.EntryCount(); entry++ {
		setFloat32Value(b, position, entry, float32(entry), float32(entry+1), float32(entry+2))
	}

	if err := position.SetExplicitMapping(pc.PointCount()); err != nil {
		b.Fatalf("SetExplicitMapping() error = %v", err)
	}

	for pointID := 0; pointID < pc.PointCount(); pointID++ {
		if err := position.SetPointMapEntry(pointID, uint32(pointID%position.EntryCount())); err != nil {
			b.Fatalf("SetPointMapEntry() error = %v", err)
		}
	}

	addPointCloudAttribute(b, pc, position)
	return pc, 0
}

func benchmarkReadFixture(b *testing.B, path string) []byte {
	b.Helper()
	data, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		b.Fatalf("ReadFile(%q) error = %v", path, err)
	}

	return data
}
