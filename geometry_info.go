package draco

import "fmt"

// AttributeDescriptor describes a decoded attribute schema and mapping shape.
type AttributeDescriptor struct {
	UniqueID       uint32
	Type           AttributeType
	Name           string
	DataType       DataType
	NumComponents  int
	Normalized     bool
	EntryCount     int
	IdentityMapped bool
	MappingSize    int
}

// GeometryInfo describes the geometry and attribute schema of a Draco payload.
type GeometryInfo struct {
	GeometryType   EncodedGeometryType
	EncodingMethod EncodingMethod
	VersionMajor   uint8
	VersionMinor   uint8
	Flags          uint16
	HasMetadata    bool
	PointCount     int
	FaceCount      int
	AttributeCount int
	Attributes     []AttributeDescriptor
}

// DecodeStats reports geometry information gathered during decode.
type DecodeStats struct {
	GeometryInfo
	BytesRead int
}

// DecodeResult is returned by DecodeWithStats.
type DecodeResult struct {
	Geometry Geometry
	Stats    DecodeStats
}

// Clone returns a copy of the geometry info and its attribute descriptors.
func (info GeometryInfo) Clone() GeometryInfo {
	out := info
	out.Attributes = append([]AttributeDescriptor(nil), info.Attributes...)
	return out
}

// Descriptor reports the public schema of an attribute.
func (a *Attribute) Descriptor() AttributeDescriptor {
	if a == nil {
		return AttributeDescriptor{}
	}

	return AttributeDescriptor{
		UniqueID:       a.UniqueID,
		Type:           a.Type,
		Name:           a.Name,
		DataType:       a.DataType,
		NumComponents:  a.NumComponents,
		Normalized:     a.Normalized,
		EntryCount:     a.EntryCount(),
		IdentityMapped: a.IsIdentityMapping(),
		MappingSize:    a.MappingSize(),
	}
}

// DescribeGeometry reports counts and attribute descriptors for an in-memory geometry.
func DescribeGeometry(g Geometry) (GeometryInfo, error) {
	switch geom := g.(type) {
	case *PointCloud:
		return describePointCloudGeometry(geom), nil
	case *Mesh:
		return describeMeshGeometry(geom), nil
	case nil:
		return GeometryInfo{}, fmt.Errorf("%w: geometry is nil", ErrInvalidGeometry)
	default:
		return GeometryInfo{}, fmt.Errorf("%w: geometry is %T", ErrInvalidGeometry, g)
	}
}

func describePointCloudGeometry(pc *PointCloud) GeometryInfo {
	descriptors := pc.AttributeDescriptors()
	return GeometryInfo{
		GeometryType:   PointCloudGeometry,
		HasMetadata:    pc.metadataRef() != nil,
		PointCount:     pc.PointCount(),
		AttributeCount: len(descriptors),
		Attributes:     descriptors,
	}
}

func describeMeshGeometry(mesh *Mesh) GeometryInfo {
	descriptors := mesh.AttributeDescriptors()
	return GeometryInfo{
		GeometryType:   MeshGeometry,
		HasMetadata:    mesh.metadataRef() != nil,
		PointCount:     mesh.PointCount(),
		FaceCount:      mesh.FaceCount(),
		AttributeCount: len(descriptors),
		Attributes:     descriptors,
	}
}
