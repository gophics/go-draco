package draco

import (
	"context"
	"fmt"

	md "github.com/gophics/go-draco/metadata"
)

func ExampleEncodeWithStats_pointCloud() {
	position, err := NewFloat32Attribute(AttributePosition, 3, []float32{
		0, 0, 0,
		1, 0, 0,
	})
	if err != nil {
		panic(err)
	}

	pc, err := NewPointCloud(2, position)
	if err != nil {
		panic(err)
	}

	result, err := EncodeWithStats(
		context.Background(),
		pc,
		WithPointCloudMethod(PointCloudSequentialEncoding),
		WithAttributeQuantization(AttributePosition, 10),
		WithTrackStats(),
	)
	if err != nil {
		panic(err)
	}

	decoded, err := DecodePointCloud(context.Background(), result.Data)
	if err != nil {
		panic(err)
	}

	fmt.Println(result.Stats.Points, decoded.PointCount(), decoded.AttributeCount())
	// Output: 2 2 1
}

func ExampleEncode_mesh() {
	position, err := NewFloat32Attribute(AttributePosition, 3, []float32{
		0, 0, 0,
		1, 0, 0,
		0, 1, 0,
	})
	if err != nil {
		panic(err)
	}

	mesh, err := NewMesh(3, []Face{{0, 1, 2}}, position)
	if err != nil {
		panic(err)
	}

	data, err := Encode(context.Background(), mesh, WithMeshMethod(MeshSequentialEncoding))
	if err != nil {
		panic(err)
	}

	decoded, err := DecodeMesh(context.Background(), data)
	if err != nil {
		panic(err)
	}

	fmt.Println(decoded.FaceCount(), decoded.PointCount())
	// Output: 1 3
}

func ExampleNewFloat32Attribute() {
	attr, err := NewFloat32Attribute(AttributePosition, 3, []float32{
		0, 0, 0,
		1, 2, 3,
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(attr.EntryCount(), attr.NumComponents, attr.DataType)
	// Output: 2 3 FLOAT32
}

func Example_metadata() {
	position, err := NewFloat32Attribute(AttributePosition, 3, []float32{
		0, 0, 0,
		1, 0, 0,
	})
	if err != nil {
		panic(err)
	}

	pc, err := NewPointCloud(2, position)
	if err != nil {
		panic(err)
	}

	metadata := &md.GeometryMetadata{}
	if err := metadata.Root.SetString("name", "sample"); err != nil {
		panic(err)
	}

	if err := pc.SetMetadata(metadata); err != nil {
		panic(err)
	}

	value, _ := pc.MetadataClone().Root.String("name")
	fmt.Println(value)
	// Output: sample
}
