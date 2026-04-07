package bitstream

import (
	"fmt"

	"github.com/gophics/go-draco/internal/core"
)

const (
	Magic = "DRACO"

	PointCloudVersionMajor = 2
	PointCloudVersionMinor = 3
	MeshVersionMajor       = 2
	MeshVersionMinor       = 2

	GeometryTypePointCloud = 0
	GeometryTypeMesh       = 1

	PointCloudSequentialEncoding = 0
	PointCloudKDTreeEncoding     = 1
	MeshSequentialEncoding       = 0
	MeshEdgebreakerEncoding      = 1

	MetadataFlagMask = 0x8000

	SequentialAttributeEncoderGeneric      = 0
	SequentialAttributeEncoderInteger      = 1
	SequentialAttributeEncoderQuantization = 2
	SequentialAttributeEncoderNormals      = 3

	PredictionNone                          = int8(-2)
	PredictionUndefined                     = int8(-1)
	PredictionDifference                    = int8(0)
	PredictionParallelogram                 = int8(1)
	PredictionMultiParallelogram            = int8(2)
	PredictionTexCoordsDeprecated           = int8(3)
	PredictionConstrainedMultiParallelogram = int8(4)
	PredictionTexCoordsPortable             = int8(5)
	PredictionGeometricNormal               = int8(6)

	PredictionTransformNone                          = int8(-1)
	PredictionTransformDelta                         = int8(0)
	PredictionTransformWrap                          = int8(1)
	PredictionTransformNormalOctahedron              = int8(2)
	PredictionTransformNormalOctahedronCanonicalized = int8(3)
)

type Header struct {
	VersionMajor  uint8
	VersionMinor  uint8
	EncoderType   uint8
	EncoderMethod uint8
	Flags         uint16
}

func DecodeHeader(r *core.Reader) (Header, error) {
	var h Header
	magic, err := r.ReadBytesView(len(Magic))
	if err != nil {
		return Header{}, err
	}

	if string(magic) != Magic {
		return Header{}, fmt.Errorf("draco: invalid magic %q", string(magic))
	}

	if h.VersionMajor, err = r.ReadUint8(); err != nil {
		return Header{}, err
	}

	if h.VersionMinor, err = r.ReadUint8(); err != nil {
		return Header{}, err
	}

	if h.EncoderType, err = r.ReadUint8(); err != nil {
		return Header{}, err
	}

	if h.EncoderMethod, err = r.ReadUint8(); err != nil {
		return Header{}, err
	}

	if h.Flags, err = r.ReadUint16(); err != nil {
		return Header{}, err
	}

	return h, nil
}

func EncodeHeader(w *core.Writer, h Header) error {
	if err := w.WriteBytes([]byte(Magic)); err != nil {
		return err
	}

	if err := w.WriteUint8(h.VersionMajor); err != nil {
		return err
	}

	if err := w.WriteUint8(h.VersionMinor); err != nil {
		return err
	}

	if err := w.WriteUint8(h.EncoderType); err != nil {
		return err
	}

	if err := w.WriteUint8(h.EncoderMethod); err != nil {
		return err
	}

	return w.WriteUint16(h.Flags)
}

func ValidateVersion(h Header) error {
	switch h.EncoderType {
	case GeometryTypePointCloud:
		if h.VersionMajor >= 1 && h.VersionMajor < PointCloudVersionMajor {
			return nil
		}

		if h.VersionMajor != PointCloudVersionMajor || h.VersionMinor > PointCloudVersionMinor {
			return fmt.Errorf("draco: unsupported point cloud bitstream version %d.%d", h.VersionMajor, h.VersionMinor)
		}
	case GeometryTypeMesh:
		if h.VersionMajor >= 1 && h.VersionMajor < MeshVersionMajor {
			return nil
		}

		if h.VersionMajor != MeshVersionMajor || h.VersionMinor > MeshVersionMinor {
			return fmt.Errorf("draco: unsupported mesh bitstream version %d.%d", h.VersionMajor, h.VersionMinor)
		}
	default:
		return fmt.Errorf("draco: unknown encoder type %d", h.EncoderType)
	}

	return nil
}
