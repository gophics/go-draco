package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	draco "github.com/gophics/go-draco"
	md "github.com/gophics/go-draco/metadata"
)

type fixtureSpec struct {
	path     string
	geometry draco.Geometry
	options  []draco.EncodeOption
}

func main() {
	fixtures := []fixtureSpec{
		{
			path:     "testdata/point_cloud_sequential.drc",
			geometry: pointCloudFixture("point-cloud-sequential", 6),
			options:  []draco.EncodeOption{draco.WithPointCloudMethod(draco.PointCloudSequentialEncoding)},
		},
		{
			path:     "testdata/point_cloud_quantized.drc",
			geometry: pointCloudFixture("point-cloud-quantized", 6),
			options: []draco.EncodeOption{
				draco.WithPointCloudMethod(draco.PointCloudSequentialEncoding),
				draco.WithAttributeQuantization(draco.AttributePosition, 12),
			},
		},
		{
			path:     "testdata/point_cloud_kd_tree.drc",
			geometry: pointCloudFixture("point-cloud-kd-tree", 16),
			options: []draco.EncodeOption{
				draco.WithPointCloudMethod(draco.PointCloudKDTreeEncoding),
				draco.WithAttributeQuantization(draco.AttributePosition, 12),
				draco.WithKDTreeCompressionLevel(6),
			},
		},
		{
			path:     "testdata/mesh_sequential.drc",
			geometry: seamMeshFixture("mesh-sequential"),
			options:  meshFixtureOptions(draco.MeshSequentialEncoding),
		},
		{
			path:     "testdata/mesh_edgebreaker.drc",
			geometry: seamMeshFixture("mesh-edgebreaker"),
			options:  meshFixtureOptions(draco.MeshEdgebreakerEncoding),
		},
	}

	for _, fixture := range fixtures {
		if err := writeFixture(fixture); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", fixture.path, err)
			os.Exit(1)
		}
	}
}

func writeFixture(fixture fixtureSpec) error {
	data, err := draco.Encode(context.Background(), fixture.geometry, fixture.options...)
	if err != nil {
		return err
	}

	path := filepath.FromSlash(fixture.path)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

func pointCloudFixture(name string, count int) *draco.PointCloud {
	positions := make([]float32, 0, count*3)
	colors := make([]uint8, 0, count*3)
	for i := 0; i < count; i++ {
		positions = append(positions,
			float32(i%4)*0.35,
			float32((i*3)%5)*0.2-0.4,
			float32((i*7)%6)*0.15,
		)
		colors = append(colors,
			uint8(40+i*11),
			uint8(80+i*7),
			uint8(120+i*5),
		)
	}

	position := must(draco.NewFloat32Attribute(draco.AttributePosition, 3, positions))
	color := must(draco.NewUint8Attribute(draco.AttributeColor, 3, colors))
	pc := must(draco.NewPointCloud(count, position, color))
	mustDo(setFixtureMetadata(pc, name))
	return pc
}

func seamMeshFixture(name string) *draco.Mesh {
	positions := must(draco.NewFloat32Attribute(draco.AttributePosition, 3, []float32{
		0, 0, 0,
		1, 0, 0,
		1, 1, 0,
		0, 1, 0,
	}))
	mustDo(positions.SetExplicitMapping(6))
	for pointID, entryID := range []uint32{0, 1, 2, 0, 2, 3} {
		mustDo(positions.SetPointMapEntry(pointID, entryID))
	}

	texCoords := must(draco.NewFloat32Attribute(draco.AttributeTexCoord, 2, []float32{
		0, 0,
		1, 0,
		1, 1,
		0, 0,
		0.25, 1,
		0, 1,
	}))

	normals := must(draco.NewFloat32Attribute(draco.AttributeNormal, 3, []float32{
		0, 0, 1,
		0, 0, 1,
		0, 0, 1,
		0, 0, 1,
		0, 0, 1,
		0, 0, 1,
	}))

	labels := must(draco.NewUint8Attribute(draco.AttributeGeneric, 1, []uint8{1, 2, 3, 4, 5, 6}))
	mesh := must(draco.NewMesh(6, []draco.Face{{0, 1, 2}, {3, 4, 5}}, positions, texCoords, normals, labels))

	mustDo(setFixtureMetadata(mesh, name))
	return mesh
}

func meshFixtureOptions(method draco.EncodingMethod) []draco.EncodeOption {
	return []draco.EncodeOption{
		draco.WithMeshMethod(method),
	}
}

func setFixtureMetadata(geometry interface {
	SetMetadata(*md.GeometryMetadata) error
}, name string) error {
	metadata := &md.GeometryMetadata{}
	if err := metadata.Root.Set("fixture", name); err != nil {
		return err
	}

	return geometry.SetMetadata(metadata)
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}

	return value
}

func mustDo(err error) {
	if err != nil {
		panic(err)
	}
}
