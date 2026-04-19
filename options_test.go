package draco

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodeOptionsNormalization(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		var cfg encodeConfig
		require.Equal(t, PointCloudSequentialEncoding, cfg.normalizedPointCloudMethod())
		require.Equal(t, MeshSequentialEncoding, cfg.normalizedMeshMethod())
		require.Equal(t, 7, cfg.normalizedSymbolCompressionLevel())
		require.True(t, cfg.useBuiltInAttributeCompression())
		require.False(t, cfg.compressConnectivity())
	})

	t.Run("configured", func(t *testing.T) {
		var cfg encodeConfig
		cfg.SetMeshMethod(MeshEdgebreakerEncoding)
		cfg.SetCompressionLevel(9)
		cfg.SetConnectivityCompression(true)
		cfg.SetUseBuiltInAttributeCompression(false)

		require.Equal(t, MeshEdgebreakerEncoding, cfg.normalizedMeshMethod())
		require.Equal(t, 9, cfg.normalizedSymbolCompressionLevel())
		require.True(t, cfg.compressConnectivity())
		require.False(t, cfg.useBuiltInAttributeCompression())
	})
}

func TestDecodeOptionsAccessors(t *testing.T) {
	var cfg decodeConfig
	require.False(t, cfg.SkipTransform(AttributePosition))

	cfg.SetSkipAttributeTransform(AttributePosition, true)
	require.True(t, cfg.SkipTransform(AttributePosition))
	require.False(t, cfg.SkipTransform(AttributeNormal))
}

func TestEncodeOptionsSpeedAccessors(t *testing.T) {
	var cfg encodeConfig
	require.Equal(t, 5, cfg.EncodingSpeed())
	require.Equal(t, 5, cfg.DecodingSpeed())
	require.Equal(t, 5, cfg.Speed())
	require.False(t, cfg.IsSpeedSet())

	cfg.SetSpeed(2, 7)
	require.Equal(t, 2, cfg.EncodingSpeed())
	require.Equal(t, 7, cfg.DecodingSpeed())
	require.Equal(t, 7, cfg.Speed())
	require.True(t, cfg.IsSpeedSet())
}

func TestSpatialQuantizationOptions(t *testing.T) {
	opts := NewSpatialQuantizationOptions(10)
	require.True(t, opts.AreQuantizationBitsDefined())
	require.Equal(t, 10, opts.QuantizationBits())

	opts.SetQuantizationBits(9)
	require.True(t, opts.AreQuantizationBitsDefined())
	require.Equal(t, 9, opts.QuantizationBits())

	opts.SetGrid(0.25)
	require.False(t, opts.AreQuantizationBitsDefined())
	require.InDelta(t, 0.25, opts.Spacing(), 1e-6)
}

func TestEncodeOptionsQuantizationAndPredictionAccessors(t *testing.T) {
	var cfg encodeConfig
	cfg.SetAttributeQuantization(AttributeTexCoord, 12)
	cfg.SetAttributeQuantizationByID(3, 15)
	cfg.SetAttributePrediction(AttributeGeneric, PredictionMethodParallelogram)
	cfg.SetAttributePredictionByID(7, PredictionMethodTexCoordsPortable)
	cfg.SetAttributeGridQuantization(AttributePosition, 0.25)
	cfg.SetAttributeGridQuantizationByID(2, 0.125)
	cfg.SetAttributeExplicitQuantization(AttributePosition, 11, []float32{-1, -2, -3}, 8)

	require.Equal(t, 12, cfg.quantizationBitsForAttribute(2, AttributeTexCoord))
	require.Equal(t, 15, cfg.quantizationBitsForAttribute(3, AttributeTexCoord))
	require.Equal(t, PredictionMethodParallelogram, cfg.predictionMethodForAttribute(4, AttributeGeneric))
	require.Equal(t, PredictionMethodTexCoordsPortable, cfg.predictionMethodForAttribute(7, AttributeGeneric))

	origin, ok := cfg.quantizationOriginForAttribute(1, AttributePosition)
	require.True(t, ok)
	require.Equal(t, []float32{-1, -2, -3}, origin)

	rangeValue, ok := cfg.quantizationRangeForAttribute(1, AttributePosition)
	require.True(t, ok)
	require.InDelta(t, 8, rangeValue, 1e-6)

	spacing, ok := cfg.quantizationSpacingForAttribute(1, AttributePosition)
	require.True(t, ok)
	require.InDelta(t, 0.25, spacing, 1e-6)

	spacing, ok = cfg.quantizationSpacingForAttribute(2, AttributePosition)
	require.True(t, ok)
	require.InDelta(t, 0.125, spacing, 1e-6)
}

func TestEncodeOptionsTrackEncodedProperties(t *testing.T) {
	var cfg encodeConfig
	require.False(t, cfg.TrackEncodedProperties())
	cfg.SetTrackEncodedProperties(true)
	require.True(t, cfg.TrackEncodedProperties())
}

func TestCloneAndMergeEncodeConfig(t *testing.T) {
	var base encodeConfig
	base.SetMeshMethod(MeshEdgebreakerEncoding)
	base.SetCompressionLevel(6)
	base.SetConnectivityCompression(true)
	base.SetAttributeQuantization(AttributePosition, 8)

	override := encodeConfig{}
	override.SetConnectivityCompression(false)
	override.SetAttributePrediction(AttributeNormal, PredictionMethodGeometricNormal)

	merged := mergeEncodeConfig(base, override)
	require.Equal(t, MeshEdgebreakerEncoding, merged.normalizedMeshMethod())
	require.False(t, merged.compressConnectivity())
	require.Equal(t, 8, merged.quantizationBits(AttributePosition))
	require.Equal(t, PredictionMethodGeometricNormal, merged.predictionMethod(AttributeNormal))
}

func TestEncodeOptionFunctionsApplyPublicConfiguration(t *testing.T) {
	spatial := NewSpatialQuantizationOptions(10)
	cfg, err := applyEncodeOptions([]EncodeOption{
		WithCompressionLevel(4),
		WithAttributeQuantizationID(2, 9),
		WithAttributeExplicitQuantizationID(3, 8, []float32{1, 2, 3}, 6),
		WithAttributePredictionID(4, PredictionMethodDifference),
		WithSpatialQuantization(AttributePosition, spatial),
		WithKDTreeCompressionLevel(5),
	})
	require.NoError(t, err)

	require.Equal(t, 4, cfg.normalizedSymbolCompressionLevel())
	require.Equal(t, 9, cfg.quantizationBitsForAttribute(2, AttributeGeneric))
	require.Equal(t, 10, cfg.quantizationBits(AttributePosition))
	require.Equal(t, PredictionMethodDifference, cfg.predictionMethodForAttribute(4, AttributeGeneric))
	require.Equal(t, 5, cfg.normalizedKDTreeCompressionLevel(3))

	origin, ok := cfg.quantizationOriginForAttribute(3, AttributeGeneric)
	require.True(t, ok)
	require.Equal(t, []float32{1, 2, 3}, origin)

	rangeValue, ok := cfg.quantizationRangeForAttribute(3, AttributeGeneric)
	require.True(t, ok)
	require.InDelta(t, 6, rangeValue, 1e-6)
}
