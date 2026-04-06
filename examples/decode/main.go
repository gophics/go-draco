package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	draco "github.com/gophics/go-draco"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (err error) {
	limit := flag.Int64("limit", 256<<20, "maximum input bytes")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "usage: decode [-limit bytes] <file.drc>\n")
		os.Exit(2)
	}

	file, err := os.Open(flag.Arg(0))
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}()

	result, err := draco.DecodeWithStatsFrom(
		context.Background(),
		file,
		draco.WithInputLimit(*limit),
	)
	if err != nil {
		return err
	}

	printInfo(result.Stats.GeometryInfo)
	return nil
}

func printInfo(info draco.GeometryInfo) {
	fmt.Printf("type: %s\n", info.GeometryType)
	fmt.Printf("encoding: %s\n", encodingMethodName(info.GeometryType, info.EncodingMethod))
	fmt.Printf("version: %d.%d\n", info.VersionMajor, info.VersionMinor)
	fmt.Printf("points: %d\n", info.PointCount)
	if info.GeometryType == draco.MeshGeometry {
		fmt.Printf("faces: %d\n", info.FaceCount)
	}

	fmt.Printf("attributes: %d\n", info.AttributeCount)

	for i, attr := range info.Attributes {
		fmt.Printf(
			"  %d: type=%s data=%s components=%d entries=%d unique_id=%d\n",
			i,
			attr.Type,
			attr.DataType,
			attr.NumComponents,
			attr.EntryCount,
			attr.UniqueID,
		)
	}
}

func encodingMethodName(geometryType draco.EncodedGeometryType, method draco.EncodingMethod) string {
	switch geometryType {
	case draco.PointCloudGeometry:
		switch method {
		case draco.PointCloudSequentialEncoding:
			return "point-cloud-sequential"
		case draco.PointCloudKDTreeEncoding:
			return "point-cloud-kd-tree"
		}
	case draco.MeshGeometry:
		switch method {
		case draco.MeshSequentialEncoding:
			return "mesh-sequential"
		case draco.MeshEdgebreakerEncoding:
			return "mesh-edgebreaker"
		}
	}

	return fmt.Sprintf("unknown(%d)", method)
}
