package draco

const (
	CapabilityDecodeSequentialPointCloudGeneric = "decode:point-cloud:sequential:generic"
	CapabilityEncodeSequentialPointCloudGeneric = "encode:point-cloud:sequential:generic"
	CapabilityDecodeKDTreePointCloud            = "decode:point-cloud:kd-tree"
	CapabilityEncodeKDTreePointCloud            = "encode:point-cloud:kd-tree"
	CapabilityDecodeSequentialMeshGeneric       = "decode:mesh:sequential:generic"
	CapabilityEncodeSequentialMeshGeneric       = "encode:mesh:sequential:generic"
	CapabilityDecodeMeshEdgebreaker             = "decode:mesh:edgebreaker"
	CapabilityEncodeMeshEdgebreaker             = "encode:mesh:edgebreaker"
	CapabilityDecodeSequentialMeshConnectivity  = "decode:mesh:sequential:connectivity"
	CapabilityEncodeSequentialMeshConnectivity  = "encode:mesh:sequential:connectivity"
	CapabilityDecodeLegacyBitstream             = "decode:legacy-bitstream"
	CapabilityDecodeAttributeInteger            = "decode:attribute:integer"
	CapabilityEncodeAttributeInteger            = "encode:attribute:integer"
	CapabilityDecodeTransformQuantization       = "decode:transform:quantization"
	CapabilityEncodeTransformQuantization       = "encode:transform:quantization"
	CapabilityDecodeTransformOctahedron         = "decode:transform:octahedron"
	CapabilityEncodeTransformOctahedron         = "encode:transform:octahedron"
	CapabilityPredictionNormalOctahedron        = "prediction:normal-octahedron"
	CapabilityPredictionNormalOctahedronCan     = "prediction:normal-octahedron-canonicalized"
	CapabilityPredictionParallelogram           = "prediction:parallelogram"
	CapabilityPredictionMultiParallelogram      = "prediction:multi-parallelogram"
	CapabilityPredictionConstrainedMultiPar     = "prediction:constrained-multi-parallelogram"
	CapabilityPredictionTexCoordPortable        = "prediction:texcoord-portable"
	CapabilityPredictionGeometricNormal         = "prediction:geometric-normal"
	CapabilityEntropyTaggedRANS                 = "entropy:tagged-rans"
	CapabilityPredictionDeltaWrap               = "prediction:delta-wrap"
	CapabilityMetadata                          = "metadata"
	CapabilityAttributeExtendedSemantics        = "attribute:semantics:extended"
	CapabilityAttributeDescriptors              = "attribute:descriptors"
	CapabilityInspectGeometry                   = "inspect:geometry"
	CapabilityDecodeWithStats                   = "decode:stats"
	CapabilityDecodeReusableContext             = "decode:context"
	CapabilityExtractMappedAttributes           = "extract:attributes:mapped"
	CapabilityViewPointCloud                    = "view:point-cloud"
	CapabilityViewMesh                          = "view:mesh"
)

func SupportedCapabilities() map[string]bool {
	return map[string]bool{
		CapabilityDecodeSequentialPointCloudGeneric: true,
		CapabilityEncodeSequentialPointCloudGeneric: true,
		CapabilityDecodeKDTreePointCloud:            true,
		CapabilityEncodeKDTreePointCloud:            true,
		CapabilityDecodeSequentialMeshGeneric:       true,
		CapabilityEncodeSequentialMeshGeneric:       true,
		CapabilityDecodeMeshEdgebreaker:             true,
		CapabilityEncodeMeshEdgebreaker:             true,
		CapabilityDecodeSequentialMeshConnectivity:  true,
		CapabilityEncodeSequentialMeshConnectivity:  true,
		CapabilityDecodeLegacyBitstream:             true,
		CapabilityDecodeAttributeInteger:            true,
		CapabilityEncodeAttributeInteger:            true,
		CapabilityDecodeTransformQuantization:       true,
		CapabilityEncodeTransformQuantization:       true,
		CapabilityDecodeTransformOctahedron:         true,
		CapabilityEncodeTransformOctahedron:         true,
		CapabilityPredictionNormalOctahedron:        true,
		CapabilityPredictionNormalOctahedronCan:     true,
		CapabilityPredictionParallelogram:           true,
		CapabilityPredictionMultiParallelogram:      true,
		CapabilityPredictionConstrainedMultiPar:     true,
		CapabilityPredictionTexCoordPortable:        true,
		CapabilityPredictionGeometricNormal:         true,
		CapabilityEntropyTaggedRANS:                 true,
		CapabilityPredictionDeltaWrap:               true,
		CapabilityMetadata:                          true,
		CapabilityAttributeExtendedSemantics:        true,
		CapabilityAttributeDescriptors:              true,
		CapabilityInspectGeometry:                   true,
		CapabilityDecodeWithStats:                   true,
		CapabilityDecodeReusableContext:             true,
		CapabilityExtractMappedAttributes:           true,
		CapabilityViewPointCloud:                    true,
		CapabilityViewMesh:                          true,
	}
}
