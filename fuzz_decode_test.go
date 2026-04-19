package draco

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzDecode(f *testing.F) {
	if data, err := Encode(f.Context(), seedFuzzPointCloud()); err == nil {
		f.Add(data)
	}

	if data, err := Encode(f.Context(), seedFuzzMesh()); err == nil {
		f.Add(data)
	}

	for _, path := range []string{
		"testdata/point_cloud_sequential.drc",
		"testdata/point_cloud_quantized.drc",
		"testdata/point_cloud_kd_tree.drc",
		"testdata/mesh_sequential.drc",
		"testdata/mesh_edgebreaker.drc",
	} {
		if data, err := os.ReadFile(filepath.FromSlash(path)); err == nil {
			f.Add(data)
		}
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if _, err := Decode(t.Context(), data); err != nil {
			return
		}
	})
}

func FuzzDetectGeometry(f *testing.F) {
	f.Add([]byte("DRACO"))
	if data, err := Encode(f.Context(), seedFuzzPointCloud()); err == nil {
		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if _, err := DetectGeometry(data); err != nil {
			return
		}
	})
}

func seedFuzzPointCloud() *PointCloud {
	pc := mustNewPointCloud(2)
	pos := mustNewFloat32Attribute(AttributePosition, 3, 2)
	if err := pos.SetFloat32(0, 1, 2, 3); err != nil {
		panic(err)
	}

	if err := pos.SetFloat32(1, 4, 5, 6); err != nil {
		panic(err)
	}

	if _, err := pc.AddAttribute(pos); err != nil {
		panic(err)
	}

	return pc
}

func seedFuzzMesh() *Mesh {
	mesh := mustNewMesh(3)
	pos := mustNewFloat32Attribute(AttributePosition, 3, 3)
	if err := pos.SetFloat32(0, 0, 0, 0); err != nil {
		panic(err)
	}

	if err := pos.SetFloat32(1, 1, 0, 0); err != nil {
		panic(err)
	}

	if err := pos.SetFloat32(2, 0, 1, 0); err != nil {
		panic(err)
	}

	if _, err := mesh.AddAttribute(pos); err != nil {
		panic(err)
	}

	if err := mesh.AddFace(Face{0, 1, 2}); err != nil {
		panic(err)
	}

	return mesh
}
