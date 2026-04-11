// Package draco implements the core Draco geometry container and codec in pure
// Go for meshes and point clouds.
package draco

import (
	"errors"

	"github.com/gophics/go-draco/internal/bitstream"
	xform "github.com/gophics/go-draco/internal/transform"
)

var (
	ErrInvalidArgument     = errors.New("draco: invalid argument")
	ErrInvalidHeader       = errors.New("draco: invalid header")
	ErrUnsupportedFeature  = errors.New("draco: unsupported feature")
	ErrUnsupportedVersion  = errors.New("draco: unsupported version")
	ErrUnsupportedEncoding = errors.New("draco: unsupported encoding method")
	ErrInvalidGeometry     = errors.New("draco: invalid geometry")
)

type octahedronTransform = xform.Octahedron
type wrapTransform = xform.Wrap
type normalPredictionTransform = xform.NormalPrediction
type normalOctahedronPredictionTransform = xform.NormalOctahedron
type normalOctahedronCanonicalizedPredictionTransform = xform.NormalOctahedronCanonicalized

type EncodedGeometryType uint8

const (
	InvalidGeometryType EncodedGeometryType = 255
	PointCloudGeometry  EncodedGeometryType = EncodedGeometryType(bitstream.GeometryTypePointCloud)
	MeshGeometry        EncodedGeometryType = EncodedGeometryType(bitstream.GeometryTypeMesh)
)

func (t EncodedGeometryType) String() string {
	switch t {
	case PointCloudGeometry:
		return "point_cloud"
	case MeshGeometry:
		return "mesh"
	default:
		return "invalid"
	}
}

type EncodingMethod uint8

const (
	PointCloudSequentialEncoding EncodingMethod = EncodingMethod(bitstream.PointCloudSequentialEncoding)
	PointCloudKDTreeEncoding     EncodingMethod = EncodingMethod(bitstream.PointCloudKDTreeEncoding)
	MeshSequentialEncoding       EncodingMethod = EncodingMethod(bitstream.MeshSequentialEncoding)
	MeshEdgebreakerEncoding      EncodingMethod = EncodingMethod(bitstream.MeshEdgebreakerEncoding)
)

type EdgebreakerMethod uint8

const (
	EdgebreakerMethodStandard   EdgebreakerMethod = EdgebreakerMethod(edgebreakerTraversalStandard)
	EdgebreakerMethodPredictive EdgebreakerMethod = EdgebreakerMethod(edgebreakerTraversalPredictive)
	EdgebreakerMethodValence    EdgebreakerMethod = EdgebreakerMethod(edgebreakerTraversalValence)
)

type PredictionMethod int8

const (
	PredictionMethodNone                          PredictionMethod = PredictionMethod(bitstream.PredictionNone)
	PredictionMethodUndefined                     PredictionMethod = PredictionMethod(bitstream.PredictionUndefined)
	PredictionMethodDifference                    PredictionMethod = PredictionMethod(bitstream.PredictionDifference)
	PredictionMethodParallelogram                 PredictionMethod = PredictionMethod(bitstream.PredictionParallelogram)
	PredictionMethodMultiParallelogram            PredictionMethod = PredictionMethod(bitstream.PredictionMultiParallelogram)
	PredictionMethodTexCoordsDeprecated           PredictionMethod = PredictionMethod(bitstream.PredictionTexCoordsDeprecated)
	PredictionMethodConstrainedMultiParallelogram PredictionMethod = PredictionMethod(bitstream.PredictionConstrainedMultiParallelogram)
	PredictionMethodTexCoordsPortable             PredictionMethod = PredictionMethod(bitstream.PredictionTexCoordsPortable)
	PredictionMethodGeometricNormal               PredictionMethod = PredictionMethod(bitstream.PredictionGeometricNormal)
)

type SequentialConnectivityMethod uint8

const (
	SequentialConnectivityCompressed   SequentialConnectivityMethod = 0
	SequentialConnectivityUncompressed SequentialConnectivityMethod = 1
)

type AttributeType uint8

const (
	AttributeInvalid  AttributeType = 255
	AttributePosition AttributeType = 0
	AttributeNormal   AttributeType = 1
	AttributeColor    AttributeType = 2
	AttributeTexCoord AttributeType = 3
	AttributeGeneric  AttributeType = 4
	AttributeTangent  AttributeType = 5
	AttributeMaterial AttributeType = 6
	AttributeJoints   AttributeType = 7
	AttributeWeights  AttributeType = 8
)

func (t AttributeType) String() string {
	switch t {
	case AttributePosition:
		return "POSITION"
	case AttributeNormal:
		return "NORMAL"
	case AttributeColor:
		return "COLOR"
	case AttributeTexCoord:
		return "TEX_COORD"
	case AttributeGeneric:
		return "GENERIC"
	case AttributeTangent:
		return "TANGENT"
	case AttributeMaterial:
		return "MATERIAL"
	case AttributeJoints:
		return "JOINTS"
	case AttributeWeights:
		return "WEIGHTS"
	default:
		return "INVALID"
	}
}

type DataType uint8

const (
	DataTypeInvalid DataType = iota
	DataTypeInt8
	DataTypeUint8
	DataTypeInt16
	DataTypeUint16
	DataTypeInt32
	DataTypeUint32
	DataTypeInt64
	DataTypeUint64
	DataTypeFloat32
	DataTypeFloat64
	DataTypeBool
)

func (dt DataType) String() string {
	switch dt {
	case DataTypeInt8:
		return "INT8"
	case DataTypeUint8:
		return "UINT8"
	case DataTypeInt16:
		return "INT16"
	case DataTypeUint16:
		return "UINT16"
	case DataTypeInt32:
		return "INT32"
	case DataTypeUint32:
		return "UINT32"
	case DataTypeInt64:
		return "INT64"
	case DataTypeUint64:
		return "UINT64"
	case DataTypeFloat32:
		return "FLOAT32"
	case DataTypeFloat64:
		return "FLOAT64"
	case DataTypeBool:
		return "BOOL"
	default:
		return "INVALID"
	}
}

func DataTypeLength(dt DataType) int {
	switch dt {
	case DataTypeInt8, DataTypeUint8, DataTypeBool:
		return 1
	case DataTypeInt16, DataTypeUint16:
		return 2
	case DataTypeInt32, DataTypeUint32, DataTypeFloat32:
		return 4
	case DataTypeInt64, DataTypeUint64, DataTypeFloat64:
		return 8
	default:
		return 0
	}
}

type Geometry interface {
	GeometryType() EncodedGeometryType
	PointCount() int
	AttributeCount() int
}

type Face [3]uint32
