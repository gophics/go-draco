package draco

import "fmt"

// AttributeView is a point-aligned snapshot of attribute data.
type AttributeView struct {
	descriptor AttributeDescriptor
	data       []byte
}

// Descriptor returns a copy of the attribute schema for this snapshot.
func (v AttributeView) Descriptor() AttributeDescriptor {
	return v.descriptor
}

// RawData returns a copy of the point-aligned raw payload.
func (v AttributeView) RawData() []byte {
	return append([]byte(nil), v.data...)
}

// Float32 returns the point-aligned payload converted to float32.
func (v AttributeView) Float32() ([]float32, error) {
	if v.descriptor.NumComponents <= 0 {
		return nil, fmt.Errorf("%w: invalid attribute view component count %d", ErrInvalidGeometry, v.descriptor.NumComponents)
	}

	componentWidth := DataTypeLength(v.descriptor.DataType)
	if componentWidth <= 0 {
		return nil, fmt.Errorf("%w: invalid attribute view data type %s", ErrInvalidGeometry, v.descriptor.DataType)
	}

	stride := v.descriptor.NumComponents * componentWidth
	if len(v.data)%stride != 0 {
		return nil, fmt.Errorf("%w: attribute view payload length %d is not divisible by stride %d", ErrInvalidGeometry, len(v.data), stride)
	}

	values := len(v.data) / componentWidth
	out := make([]float32, values)
	for entry := 0; entry < len(out)/v.descriptor.NumComponents; entry++ {
		start := entry * v.descriptor.NumComponents
		raw := v.data[entry*stride : entry*stride+stride]
		if err := decodeRawFloat32(out[start:start+v.descriptor.NumComponents], raw, v.descriptor.DataType, v.descriptor.Normalized); err != nil {
			return nil, err
		}
	}

	return out, nil
}

// Int32 returns the point-aligned payload converted to int32.
func (v AttributeView) Int32() ([]int32, error) {
	if v.descriptor.NumComponents <= 0 {
		return nil, fmt.Errorf("%w: invalid attribute view component count %d", ErrInvalidGeometry, v.descriptor.NumComponents)
	}

	componentWidth := DataTypeLength(v.descriptor.DataType)
	if componentWidth <= 0 {
		return nil, fmt.Errorf("%w: invalid attribute view data type %s", ErrInvalidGeometry, v.descriptor.DataType)
	}

	stride := v.descriptor.NumComponents * componentWidth
	if len(v.data)%stride != 0 {
		return nil, fmt.Errorf("%w: attribute view payload length %d is not divisible by stride %d", ErrInvalidGeometry, len(v.data), stride)
	}

	values := len(v.data) / componentWidth
	out := make([]int32, values)
	for entry := 0; entry < len(out)/v.descriptor.NumComponents; entry++ {
		start := entry * v.descriptor.NumComponents
		raw := v.data[entry*stride : entry*stride+stride]
		if err := decodeRawInt32(out[start:start+v.descriptor.NumComponents], raw, v.descriptor.DataType); err != nil {
			return nil, err
		}
	}

	return out, nil
}

func cloneAttributeViews(values []AttributeView) []AttributeView {
	if values == nil {
		return nil
	}

	out := make([]AttributeView, len(values))
	copy(out, values)
	return out
}

// PointCloudView is a read-only point-aligned snapshot of a point cloud.
type PointCloudView struct {
	info       GeometryInfo
	attributes []AttributeView
}

// Info returns a copy of the geometry info for this snapshot.
func (v *PointCloudView) Info() GeometryInfo {
	if v == nil {
		return GeometryInfo{}
	}

	return v.info.Clone()
}

// AttributeCount reports the number of attribute views in the snapshot.
func (v *PointCloudView) AttributeCount() int {
	if v == nil {
		return 0
	}

	return len(v.attributes)
}

// Attribute returns an attribute snapshot by index.
func (v *PointCloudView) Attribute(index int) (AttributeView, error) {
	if v == nil {
		return AttributeView{}, fmt.Errorf("%w: point cloud view is nil", ErrInvalidGeometry)
	}

	if index < 0 || index >= len(v.attributes) {
		return AttributeView{}, fmt.Errorf("%w: point cloud view attribute %d out of range", ErrInvalidGeometry, index)
	}

	return v.attributes[index], nil
}

// Attributes returns a copied slice of attribute snapshots.
func (v *PointCloudView) Attributes() []AttributeView {
	if v == nil {
		return nil
	}

	return cloneAttributeViews(v.attributes)
}

// AttributeByUniqueID returns the first attribute view with the given unique id.
func (v *PointCloudView) AttributeByUniqueID(id uint32) *AttributeView {
	if v == nil {
		return nil
	}

	for i := range v.attributes {
		if v.attributes[i].descriptor.UniqueID == id {
			clone := v.attributes[i]
			return &clone
		}
	}

	return nil
}

// MeshView is a read-only point-aligned snapshot of a mesh.
type MeshView struct {
	info       GeometryInfo
	faces      []Face
	attributes []AttributeView
}

// Info returns a copy of the geometry info for this snapshot.
func (v *MeshView) Info() GeometryInfo {
	if v == nil {
		return GeometryInfo{}
	}

	return v.info.Clone()
}

// FaceCount reports the number of faces in the snapshot.
func (v *MeshView) FaceCount() int {
	if v == nil {
		return 0
	}

	return len(v.faces)
}

// Face returns a face snapshot by index.
func (v *MeshView) Face(index int) (Face, error) {
	if v == nil {
		return Face{}, fmt.Errorf("%w: mesh view is nil", ErrInvalidGeometry)
	}

	if index < 0 || index >= len(v.faces) {
		return Face{}, fmt.Errorf("%w: mesh view face %d out of range", ErrInvalidGeometry, index)
	}

	return v.faces[index], nil
}

// Faces returns a copied slice of face snapshots.
func (v *MeshView) Faces() []Face {
	if v == nil {
		return nil
	}

	out := make([]Face, len(v.faces))
	copy(out, v.faces)
	return out
}

// AttributeCount reports the number of attribute views in the snapshot.
func (v *MeshView) AttributeCount() int {
	if v == nil {
		return 0
	}

	return len(v.attributes)
}

// Attribute returns an attribute snapshot by index.
func (v *MeshView) Attribute(index int) (AttributeView, error) {
	if v == nil {
		return AttributeView{}, fmt.Errorf("%w: mesh view is nil", ErrInvalidGeometry)
	}

	if index < 0 || index >= len(v.attributes) {
		return AttributeView{}, fmt.Errorf("%w: mesh view attribute %d out of range", ErrInvalidGeometry, index)
	}

	return v.attributes[index], nil
}

// Attributes returns a copied slice of attribute snapshots.
func (v *MeshView) Attributes() []AttributeView {
	if v == nil {
		return nil
	}

	return cloneAttributeViews(v.attributes)
}

// AttributeByUniqueID returns the first attribute view with the given unique id.
func (v *MeshView) AttributeByUniqueID(id uint32) *AttributeView {
	if v == nil {
		return nil
	}

	for i := range v.attributes {
		if v.attributes[i].descriptor.UniqueID == id {
			clone := v.attributes[i]
			return &clone
		}
	}

	return nil
}

// NewPointCloudView builds a point-aligned snapshot of a point cloud.
func NewPointCloudView(pc *PointCloud) (*PointCloudView, error) {
	if pc == nil {
		return nil, fmt.Errorf("%w: point cloud is nil", ErrInvalidGeometry)
	}

	if err := pc.Validate(); err != nil {
		return nil, err
	}

	info := describePointCloudGeometry(pc)
	attributes := make([]AttributeView, len(info.Attributes))
	for attID := range info.Attributes {
		data, err := pc.ExtractMappedRaw(attID)
		if err != nil {
			return nil, err
		}

		attributes[attID] = AttributeView{
			descriptor: info.Attributes[attID],
			data:       data,
		}
	}

	return &PointCloudView{
		info:       info,
		attributes: attributes,
	}, nil
}

// NewMeshView builds a point-aligned snapshot of a mesh.
func NewMeshView(mesh *Mesh) (*MeshView, error) {
	if mesh == nil {
		return nil, fmt.Errorf("%w: mesh is nil", ErrInvalidGeometry)
	}

	if err := mesh.Validate(); err != nil {
		return nil, err
	}

	info := describeMeshGeometry(mesh)
	attributes := make([]AttributeView, len(info.Attributes))
	for attID := range info.Attributes {
		data, err := mesh.ExtractMappedRaw(attID)
		if err != nil {
			return nil, err
		}

		attributes[attID] = AttributeView{
			descriptor: info.Attributes[attID],
			data:       data,
		}
	}

	return &MeshView{
		info:       info,
		faces:      mesh.Faces(),
		attributes: attributes,
	}, nil
}
