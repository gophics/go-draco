# Test Fixtures

The `.drc` files in this directory are synthetic fixtures generated for this project.

Regenerate them from the repository root with:

```sh
go run ./internal/fixtures/cmd/fixturegen
```

Fixture inventory:

- `point_cloud_sequential.drc`: small point cloud with position, color, and geometry metadata.
- `point_cloud_quantized.drc`: sequential point cloud with quantized positions.
- `point_cloud_kd_tree.drc`: kd-tree point cloud with quantized positions.
- `mesh_sequential.drc`: two-triangle mesh with shared position entries, texture coordinates, normals, generic labels, and geometry metadata.
- `mesh_edgebreaker.drc`: the same mesh encoded with edgebreaker connectivity.
