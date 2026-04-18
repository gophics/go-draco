package draco

import (
	"context"
	"fmt"
	"io"

	"github.com/gophics/go-draco/internal/bitstream"
)

// Decoder stores decode state, options, and reusable scratch buffers.
// It is not safe for concurrent use by multiple goroutines.
type Decoder struct {
	engine decoder
}

// DetectGeometry reports the encoded geometry type for a Draco bitstream.
func DetectGeometry(data []byte) (EncodedGeometryType, error) {
	return detectGeometry(data)
}

// NewDecoder returns a decoder with the provided options.
func NewDecoder(opts ...DecodeOption) (*Decoder, error) {
	cfg, err := applyDecodeOptions(opts)
	if err != nil {
		return nil, err
	}

	return &Decoder{engine: decoder{options: cfg, scratch: &decodeScratch{}}}, nil
}

// Encode serializes a mesh or point cloud with the provided options.
func Encode(ctx context.Context, g Geometry, opts ...EncodeOption) ([]byte, error) {
	result, err := EncodeWithStats(ctx, g, opts...)
	if err != nil {
		return nil, err
	}

	return result.Data, nil
}

// EncodeTo serializes a mesh or point cloud to a writer.
func EncodeTo(ctx context.Context, w io.Writer, g Geometry, opts ...EncodeOption) error {
	if err := guardNonNilWriter(w); err != nil {
		return err
	}

	data, err := Encode(ctx, g, opts...)
	if err != nil {
		return err
	}

	return writeFull(w, data)
}

// Decode deserializes a Draco mesh or point cloud.
func Decode(ctx context.Context, data []byte, opts ...DecodeOption) (Geometry, error) {
	cfg, err := applyDecodeOptions(opts)
	if err != nil {
		return nil, err
	}

	scratch := acquireDecodeScratch()
	defer releaseDecodeScratch(scratch)
	decoder := &Decoder{engine: decoder{options: cfg, scratch: scratch}}
	return decoder.Decode(ctx, data)
}

// EncodeWithStats serializes a mesh or point cloud and returns encode counts.
func EncodeWithStats(ctx context.Context, g Geometry, opts ...EncodeOption) (EncodeResult, error) {
	if ctx == nil {
		return EncodeResult{}, fmt.Errorf("%w: context is nil", ErrInvalidArgument)
	}

	if err := ctx.Err(); err != nil {
		return EncodeResult{}, err
	}

	cfg, err := applyEncodeOptions(opts)
	if err != nil {
		return EncodeResult{}, err
	}

	scratch := acquireEncodeScratch()
	defer releaseEncodeScratch(scratch)
	engine := (&encoder{options: cfg, scratch: scratch}).withContext(ctx)
	switch geom := g.(type) {
	case *PointCloud:
		data, err := engine.EncodePointCloud(geom)
		if err != nil {
			return EncodeResult{}, err
		}

		return EncodeResult{
			Data: data,
			Stats: EncodeStats{
				Points: engine.numEncodedPoints,
				Faces:  engine.numEncodedFaces,
			},
		}, nil
	case *Mesh:
		data, err := engine.EncodeMesh(geom)
		if err != nil {
			return EncodeResult{}, err
		}

		return EncodeResult{
			Data: data,
			Stats: EncodeStats{
				Points: engine.numEncodedPoints,
				Faces:  engine.numEncodedFaces,
			},
		}, nil
	default:
		return EncodeResult{}, ErrInvalidGeometry
	}
}

// DecodeFrom deserializes a Draco mesh or point cloud from a reader.
func DecodeFrom(ctx context.Context, r io.Reader, opts ...DecodeOption) (Geometry, error) {
	cfg, err := applyDecodeOptions(opts)
	if err != nil {
		return nil, err
	}

	scratch := acquireDecodeScratch()
	defer releaseDecodeScratch(scratch)
	decoder := &Decoder{engine: decoder{options: cfg, scratch: scratch}}
	return decoder.DecodeFrom(ctx, r)
}

// DecodeMeshFrom deserializes a Draco mesh from a reader.
func DecodeMeshFrom(ctx context.Context, r io.Reader, opts ...DecodeOption) (*Mesh, error) {
	geom, err := DecodeFrom(ctx, r, opts...)
	if err != nil {
		return nil, err
	}

	return geometryAsMesh(geom)
}

// DecodePointCloudFrom deserializes a Draco point cloud from a reader.
func DecodePointCloudFrom(ctx context.Context, r io.Reader, opts ...DecodeOption) (*PointCloud, error) {
	geom, err := DecodeFrom(ctx, r, opts...)
	if err != nil {
		return nil, err
	}

	return geometryAsPointCloud(geom)
}

// DecodeMesh deserializes a Draco mesh.
func DecodeMesh(ctx context.Context, data []byte, opts ...DecodeOption) (*Mesh, error) {
	geom, err := Decode(ctx, data, opts...)
	if err != nil {
		return nil, err
	}

	return geometryAsMesh(geom)
}

// DecodePointCloud deserializes a Draco point cloud.
func DecodePointCloud(ctx context.Context, data []byte, opts ...DecodeOption) (*PointCloud, error) {
	geom, err := Decode(ctx, data, opts...)
	if err != nil {
		return nil, err
	}

	return geometryAsPointCloud(geom)
}

// DecodeWithStats deserializes Draco geometry and returns geometry info.
func DecodeWithStats(ctx context.Context, data []byte, opts ...DecodeOption) (DecodeResult, error) {
	cfg, err := applyDecodeOptions(opts)
	if err != nil {
		return DecodeResult{}, err
	}

	scratch := acquireDecodeScratch()
	defer releaseDecodeScratch(scratch)
	decoder := &Decoder{engine: decoder{options: cfg, scratch: scratch}}
	return decoder.DecodeWithStats(ctx, data)
}

// DecodeWithStatsFrom deserializes Draco geometry from a reader and returns geometry info.
func DecodeWithStatsFrom(ctx context.Context, r io.Reader, opts ...DecodeOption) (DecodeResult, error) {
	cfg, err := applyDecodeOptions(opts)
	if err != nil {
		return DecodeResult{}, err
	}

	scratch := acquireDecodeScratch()
	defer releaseDecodeScratch(scratch)
	decoder := &Decoder{engine: decoder{options: cfg, scratch: scratch}}
	return decoder.DecodeWithStatsFrom(ctx, r)
}

// Inspect returns geometry counts and attribute descriptors without returning
// the decoded geometry.
func Inspect(ctx context.Context, data []byte, opts ...DecodeOption) (GeometryInfo, error) {
	result, err := DecodeWithStats(ctx, data, opts...)
	if err != nil {
		return GeometryInfo{}, err
	}

	return result.Stats.GeometryInfo, nil
}

// InspectFrom returns geometry counts and attribute descriptors from a reader
// without returning the decoded geometry.
func InspectFrom(ctx context.Context, r io.Reader, opts ...DecodeOption) (GeometryInfo, error) {
	result, err := DecodeWithStatsFrom(ctx, r, opts...)
	if err != nil {
		return GeometryInfo{}, err
	}

	return result.Stats.GeometryInfo, nil
}

// Decode deserializes Draco geometry with the decoder.
func (d *Decoder) Decode(ctx context.Context, data []byte) (Geometry, error) {
	if d == nil {
		return nil, errNilDecoder()
	}

	if ctx == nil {
		return nil, fmt.Errorf("%w: context is nil", ErrInvalidArgument)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	engine := d.engine.withContext(ctx)
	return engine.Decode(data)
}

// DecodeFrom deserializes Draco geometry from a reader with the decoder.
func (d *Decoder) DecodeFrom(ctx context.Context, r io.Reader) (Geometry, error) {
	if d == nil {
		return nil, errNilDecoder()
	}

	data, err := readAllContext(ctx, r, d.engine.options.InputLimit())
	if err != nil {
		return nil, err
	}

	return d.Decode(ctx, data)
}

// DecodeMeshFrom deserializes a Draco mesh from a reader with the decoder.
func (d *Decoder) DecodeMeshFrom(ctx context.Context, r io.Reader) (*Mesh, error) {
	if d == nil {
		return nil, errNilDecoder()
	}

	geom, err := d.DecodeFrom(ctx, r)
	if err != nil {
		return nil, err
	}

	return geometryAsMesh(geom)
}

// DecodeMesh deserializes a Draco mesh with the decoder.
func (d *Decoder) DecodeMesh(ctx context.Context, data []byte) (*Mesh, error) {
	if d == nil {
		return nil, errNilDecoder()
	}

	geom, err := d.Decode(ctx, data)
	if err != nil {
		return nil, err
	}

	return geometryAsMesh(geom)
}

// DecodePointCloudFrom deserializes a Draco point cloud from a reader with the decoder.
func (d *Decoder) DecodePointCloudFrom(ctx context.Context, r io.Reader) (*PointCloud, error) {
	if d == nil {
		return nil, errNilDecoder()
	}

	geom, err := d.DecodeFrom(ctx, r)
	if err != nil {
		return nil, err
	}

	return geometryAsPointCloud(geom)
}

// DecodePointCloud deserializes a Draco point cloud with the decoder.
func (d *Decoder) DecodePointCloud(ctx context.Context, data []byte) (*PointCloud, error) {
	if d == nil {
		return nil, errNilDecoder()
	}

	geom, err := d.Decode(ctx, data)
	if err != nil {
		return nil, err
	}

	return geometryAsPointCloud(geom)
}

// DecodeWithStats deserializes Draco geometry and returns geometry info.
func (d *Decoder) DecodeWithStats(ctx context.Context, data []byte) (DecodeResult, error) {
	if d == nil {
		return DecodeResult{}, errNilDecoder()
	}

	if ctx == nil {
		return DecodeResult{}, fmt.Errorf("%w: context is nil", ErrInvalidArgument)
	}

	if err := ctx.Err(); err != nil {
		return DecodeResult{}, err
	}

	engine := d.engine.withContext(ctx)
	geom, header, err := engine.decodeWithHeader(data)
	if err != nil {
		return DecodeResult{}, err
	}

	info, err := DescribeGeometry(geom)
	if err != nil {
		return DecodeResult{}, err
	}

	info.EncodingMethod = EncodingMethod(header.EncoderMethod)
	info.VersionMajor = header.VersionMajor
	info.VersionMinor = header.VersionMinor
	info.Flags = header.Flags
	info.HasMetadata = header.Flags&bitstream.MetadataFlagMask != 0 || info.HasMetadata
	return DecodeResult{
		Geometry: geom,
		Stats: DecodeStats{
			GeometryInfo: info,
			BytesRead:    len(data),
		},
	}, nil
}

// DecodeWithStatsFrom deserializes Draco geometry from a reader and returns geometry info.
func (d *Decoder) DecodeWithStatsFrom(ctx context.Context, r io.Reader) (DecodeResult, error) {
	if d == nil {
		return DecodeResult{}, errNilDecoder()
	}

	data, err := readAllContext(ctx, r, d.engine.options.InputLimit())
	if err != nil {
		return DecodeResult{}, err
	}

	return d.DecodeWithStats(ctx, data)
}

// Inspect returns geometry counts and attribute descriptors without returning
// the decoded geometry.
func (d *Decoder) Inspect(ctx context.Context, data []byte) (GeometryInfo, error) {
	result, err := d.DecodeWithStats(ctx, data)
	if err != nil {
		return GeometryInfo{}, err
	}

	return result.Stats.GeometryInfo, nil
}

// InspectFrom returns geometry counts and attribute descriptors from a reader
// without returning the decoded geometry.
func (d *Decoder) InspectFrom(ctx context.Context, r io.Reader) (GeometryInfo, error) {
	if d == nil {
		return GeometryInfo{}, errNilDecoder()
	}

	result, err := d.DecodeWithStatsFrom(ctx, r)
	if err != nil {
		return GeometryInfo{}, err
	}

	return result.Stats.GeometryInfo, nil
}

func errNilDecoder() error {
	return fmt.Errorf("%w: decoder is nil", ErrInvalidArgument)
}

func geometryAsMesh(geom Geometry) (*Mesh, error) {
	mesh, ok := geom.(*Mesh)
	if !ok {
		return nil, fmt.Errorf("%w: geometry is %T", ErrInvalidGeometry, geom)
	}

	return mesh, nil
}

func geometryAsPointCloud(geom Geometry) (*PointCloud, error) {
	pc, ok := geom.(*PointCloud)
	if !ok {
		return nil, fmt.Errorf("%w: geometry is %T", ErrInvalidGeometry, geom)
	}

	return pc, nil
}

func writeFull(w io.Writer, data []byte) error {
	if err := guardNonNilWriter(w); err != nil {
		return err
	}

	totalWritten := 0
	for totalWritten < len(data) {
		n, err := w.Write(data[totalWritten:])
		totalWritten += n
		if err != nil {
			return err
		}

		if n == 0 {
			return io.ErrShortWrite
		}
	}

	return nil
}
