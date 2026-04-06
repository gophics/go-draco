# go-draco

`go-draco` is a pure-Go Draco codec for meshes, point clouds, and metadata.

The API uses:

- encode uses free functions, and decode supports both free functions and a reusable `Decoder`
- configuration uses typed functional options
- validating geometry constructors
- typed attribute constructors replace reflection-heavy builder paths
- inspect, extract, and view helpers alongside the raw codec functions

## Install

Requires Go 1.22 or newer.

```bash
go get github.com/gophics/go-draco
```

## Encode A Point Cloud

```go
position, err := draco.NewFloat32Attribute(draco.AttributePosition, 3, []float32{
	0, 0, 0,
	1, 0, 0,
})
if err != nil {
	log.Fatal(err)
}

pc, err := draco.NewPointCloud(2, position)
if err != nil {
	log.Fatal(err)
}

result, err := draco.EncodeWithStats(
	context.Background(),
	pc,
	draco.WithPointCloudMethod(draco.PointCloudSequentialEncoding),
	draco.WithAttributeQuantization(draco.AttributePosition, 10),
	draco.WithTrackStats(),
)
if err != nil {
	log.Fatal(err)
}

fmt.Println(len(result.Data), result.Stats.Points)
```

## Encode A Mesh

```go
position, err := draco.NewFloat32Attribute(draco.AttributePosition, 3, []float32{
	0, 0, 0,
	1, 0, 0,
	0, 1, 0,
})
if err != nil {
	log.Fatal(err)
}

mesh, err := draco.NewMesh(3, []draco.Face{{0, 1, 2}}, position)
if err != nil {
	log.Fatal(err)
}

data, err := draco.Encode(
	context.Background(),
	mesh,
	draco.WithMeshMethod(draco.MeshEdgebreakerEncoding),
	draco.WithAttributeQuantization(draco.AttributePosition, 12),
)
if err != nil {
	log.Fatal(err)
}

decoded, err := draco.DecodeMesh(context.Background(), data)
if err != nil {
	log.Fatal(err)
}

fmt.Println(decoded.FaceCount(), decoded.PointCount())
```

## Decode From A Reader

```go
geom, err := draco.DecodeFrom(
	context.Background(),
	reader,
	draco.WithSkipAttributeTransform(draco.AttributeNormal),
	draco.WithInputLimit(64<<20),
)
if err != nil {
	log.Fatal(err)
}

switch g := geom.(type) {
case *draco.Mesh:
	fmt.Println("mesh", g.FaceCount())
case *draco.PointCloud:
	fmt.Println("point-cloud", g.PointCount())
}
```

## Decode Command Example

```bash
go run ./examples/decode -- testdata/mesh_edgebreaker.drc
```

The example prints geometry counts and attribute schemas for a `.drc` file.

## Inspect Geometry Info

```go
info, err := draco.Inspect(context.Background(), data)
if err != nil {
	log.Fatal(err)
}

fmt.Println(info.GeometryType, info.EncodingMethod, info.PointCount, info.FaceCount)
for _, attr := range info.Attributes {
	fmt.Println(attr.Type, attr.DataType, attr.NumComponents, attr.UniqueID)
}
```

## Reuse A Decoder

`Decoder` keeps reusable scratch buffers. Use one decoder per goroutine, or
protect shared decoder use with external synchronization.

```go
decoder, err := draco.NewDecoder(draco.WithSkipAttributeTransform(draco.AttributeNormal))
if err != nil {
	log.Fatal(err)
}

result, err := decoder.DecodeWithStats(context.Background(), data)
if err != nil {
	log.Fatal(err)
}

fmt.Println(result.Stats.BytesRead, result.Stats.PointCount, result.Stats.AttributeCount)
```

## Extract Point-Aligned Attribute Data

```go
pc, err := draco.DecodePointCloud(context.Background(), data)
if err != nil {
	log.Fatal(err)
}

positions, err := pc.ExtractMappedFloat32(0)
if err != nil {
	log.Fatal(err)
}

fmt.Println(len(positions) / 3)
```

## Build Read-Only Geometry Views

```go
mesh, err := draco.DecodeMesh(context.Background(), data)
if err != nil {
	log.Fatal(err)
}

view, err := draco.NewMeshView(mesh)
if err != nil {
	log.Fatal(err)
}

fmt.Println(view.FaceCount(), view.AttributeCount())
```

## Metadata

```go
position, err := draco.NewFloat32Attribute(draco.AttributePosition, 3, []float32{
	0, 0, 0,
	1, 0, 0,
})
if err != nil {
	log.Fatal(err)
}

pc, err := draco.NewPointCloud(2, position)
if err != nil {
	log.Fatal(err)
}

metadataValue := &metadata.GeometryMetadata{}
if err := metadataValue.Root.SetString("name", "sample"); err != nil {
	log.Fatal(err)
}
if err := pc.SetMetadata(metadataValue); err != nil {
	log.Fatal(err)
}
```

## Notes

- `Encode`, `EncodeTo`, and `EncodeWithStats` are the primary write entrypoints.
- `Decode`, `DecodeFrom`, `DecodeMesh`, `DecodeMeshFrom`, `DecodePointCloud`, and `DecodePointCloudFrom` are the primary read entrypoints, and they all take `context.Context`.
- Reader-based decode entrypoints enforce a default input cap of `256 MiB`; override it per call with `WithInputLimit`.
- `Inspect`, `InspectFrom`, `DecodeWithStats`, and `NewDecoder` cover geometry info and decoder reuse.
- `Attribute` entry-order extractors plus `PointCloud` point-order extractors expose typed bulk reads without per-entry allocation loops in downstream code.
- `NewPointCloudView` and `NewMeshView` return point-aligned, read-only snapshots.
- The public attribute enum includes semantic kinds such as `TANGENT`, `MATERIAL`, `JOINTS`, and `WEIGHTS`.
- Compression settings are passed as encode options instead of being stored on `PointCloud` or `Mesh`.

## License

Apache-2.0.
