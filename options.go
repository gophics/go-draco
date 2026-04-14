package draco

import "fmt"

// EncodeOption configures Encode, EncodeTo, and EncodeWithStats.
type EncodeOption func(*encodeConfig) error

// DecodeOption configures the decode entrypoints and Decoder.
type DecodeOption func(*decodeConfig) error

// EncodeStats reports geometry counts gathered during encoding.
type EncodeStats struct {
	Points int
	Faces  int
}

// EncodeResult is returned by EncodeWithStats.
type EncodeResult struct {
	Data  []byte
	Stats EncodeStats
}

// WithPointCloudMethod selects the point-cloud encoding method.
func WithPointCloudMethod(method EncodingMethod) EncodeOption {
	return func(cfg *encodeConfig) error {
		cfg.SetPointCloudMethod(method)
		return nil
	}
}

// WithMeshMethod selects the mesh encoding method.
func WithMeshMethod(method EncodingMethod) EncodeOption {
	return func(cfg *encodeConfig) error {
		cfg.SetMeshMethod(method)
		return nil
	}
}

// WithCompressionLevel sets the global compression level.
func WithCompressionLevel(level int) EncodeOption {
	return func(cfg *encodeConfig) error {
		cfg.SetCompressionLevel(level)
		return nil
	}
}

// WithConnectivityCompression enables or disables sequential mesh connectivity compression.
func WithConnectivityCompression(enabled bool) EncodeOption {
	return func(cfg *encodeConfig) error {
		cfg.SetConnectivityCompression(enabled)
		return nil
	}
}

// WithAttributeQuantization sets quantization bits for all attributes of a given type.
func WithAttributeQuantization(attType AttributeType, quantizationBits int) EncodeOption {
	return func(cfg *encodeConfig) error {
		cfg.SetAttributeQuantization(attType, quantizationBits)
		return nil
	}
}

// WithAttributeQuantizationID sets quantization bits for a single attribute id.
func WithAttributeQuantizationID(attID int, quantizationBits int) EncodeOption {
	return func(cfg *encodeConfig) error {
		cfg.SetAttributeQuantizationByID(attID, quantizationBits)
		return nil
	}
}

// WithAttributeExplicitQuantization sets explicit origin/range quantization for a type.
func WithAttributeExplicitQuantization(attType AttributeType, quantizationBits int, origin []float32, rangeValue float32) EncodeOption {
	return func(cfg *encodeConfig) error {
		cfg.SetAttributeExplicitQuantization(attType, quantizationBits, origin, rangeValue)
		return nil
	}
}

// WithAttributeExplicitQuantizationID sets explicit origin/range quantization for a single attribute id.
func WithAttributeExplicitQuantizationID(attID int, quantizationBits int, origin []float32, rangeValue float32) EncodeOption {
	return func(cfg *encodeConfig) error {
		cfg.SetAttributeExplicitQuantizationByID(attID, quantizationBits, origin, rangeValue)
		return nil
	}
}

// WithAttributePrediction sets the prediction method for all attributes of a given type.
func WithAttributePrediction(attType AttributeType, method PredictionMethod) EncodeOption {
	return func(cfg *encodeConfig) error {
		cfg.SetAttributePrediction(attType, method)
		return nil
	}
}

// WithAttributePredictionID sets the prediction method for a single attribute id.
func WithAttributePredictionID(attID int, method PredictionMethod) EncodeOption {
	return func(cfg *encodeConfig) error {
		cfg.SetAttributePredictionByID(attID, method)
		return nil
	}
}

// WithSpatialQuantization applies bit- or grid-based spatial quantization to an attribute type.
func WithSpatialQuantization(attType AttributeType, options SpatialQuantizationOptions) EncodeOption {
	return func(cfg *encodeConfig) error {
		if options.AreQuantizationBitsDefined() {
			cfg.SetAttributeQuantization(attType, options.QuantizationBits())
			return nil
		}

		cfg.SetAttributeGridQuantization(attType, options.Spacing())
		return nil
	}
}

// WithSkipAttributeTransform skips decode-time transform restoration for the given attribute type.
func WithSkipAttributeTransform(attType AttributeType) DecodeOption {
	return func(cfg *decodeConfig) error {
		cfg.SetSkipAttributeTransform(attType, true)
		return nil
	}
}

// WithInputLimit overrides the maximum number of bytes read by reader-based decode entrypoints.
func WithInputLimit(maxBytes int64) DecodeOption {
	return func(cfg *decodeConfig) error {
		return cfg.SetInputLimit(maxBytes)
	}
}

// WithRawAttributeCompression disables built-in integer attribute compression.
func WithRawAttributeCompression() EncodeOption {
	return func(cfg *encodeConfig) error {
		cfg.SetUseBuiltInAttributeCompression(false)
		return nil
	}
}

// WithTrackStats enables encode-time point/face statistics.
func WithTrackStats() EncodeOption {
	return func(cfg *encodeConfig) error {
		cfg.SetTrackEncodedProperties(true)
		return nil
	}
}

// WithSpeed sets encoder and decoder speed hints used by the codec.
// With edgebreaker mesh encoding, speed 10 uses native traversal order for
// throughput; output remains deterministic for the same input and decodes to
// equivalent geometry, but decode/re-encode bytes are not canonicalized.
func WithSpeed(encodingSpeed, decodingSpeed int) EncodeOption {
	return func(cfg *encodeConfig) error {
		cfg.SetSpeed(encodingSpeed, decodingSpeed)
		return nil
	}
}

// WithKDTreeCompressionLevel overrides the kd-tree compression level.
func WithKDTreeCompressionLevel(level int) EncodeOption {
	return func(cfg *encodeConfig) error {
		cfg.SetKDTreeCompressionLevel(level)
		return nil
	}
}

// WithSplitMeshOnSeams controls edgebreaker seam splitting.
func WithSplitMeshOnSeams(enabled bool) EncodeOption {
	return func(cfg *encodeConfig) error {
		cfg.SetSplitMeshOnSeams(enabled)
		return nil
	}
}

// WithEdgebreakerMethod overrides the edgebreaker traversal mode.
func WithEdgebreakerMethod(method EdgebreakerMethod) EncodeOption {
	return func(cfg *encodeConfig) error {
		cfg.SetEdgebreakerMethod(method)
		return nil
	}
}

func applyEncodeOptions(opts []EncodeOption) (encodeConfig, error) {
	cfg := encodeConfig{}
	for _, opt := range opts {
		if opt == nil {
			continue
		}

		if err := opt(&cfg); err != nil {
			return encodeConfig{}, err
		}
	}

	return cfg, nil
}

func applyDecodeOptions(opts []DecodeOption) (decodeConfig, error) {
	cfg := decodeConfig{}
	for _, opt := range opts {
		if opt == nil {
			continue
		}

		if err := opt(&cfg); err != nil {
			return decodeConfig{}, err
		}
	}

	return cfg, nil
}

type decodeConfig struct {
	SkipAttributeTransform map[AttributeType]bool
	inputLimit             configValue[int64]
}

func (o *decodeConfig) SetSkipAttributeTransform(attType AttributeType, enabled bool) {
	if o.SkipAttributeTransform == nil {
		o.SkipAttributeTransform = make(map[AttributeType]bool)
	}

	o.SkipAttributeTransform[attType] = enabled
}

func (o decodeConfig) SkipTransform(attType AttributeType) bool {
	if o.SkipAttributeTransform == nil {
		return false
	}

	return o.SkipAttributeTransform[attType]
}

func (o *decodeConfig) SetInputLimit(maxBytes int64) error {
	if maxBytes < 0 {
		return fmt.Errorf("%w: input limit must be >= 0", ErrInvalidArgument)
	}

	o.inputLimit = configValue[int64]{set: true, value: maxBytes}
	return nil
}

func (o decodeConfig) InputLimit() int64 {
	if !o.inputLimit.set {
		return defaultReaderLimitBytes
	}

	return o.inputLimit.value
}

type AttributeCompressionMode uint8

const (
	AttributeCompressionStandard AttributeCompressionMode = iota
	AttributeCompressionRaw
)

type configValue[T any] struct {
	set   bool
	value T
}

type attributeEncodeOptions struct {
	quantizationBits   configValue[int]
	prediction         configValue[PredictionMethod]
	quantizationRange  configValue[float32]
	quantizationSpace  configValue[float32]
	quantizationOrigin configValue[[]float32]
}

func (o attributeEncodeOptions) clone() attributeEncodeOptions {
	out := o
	if o.quantizationOrigin.set {
		out.quantizationOrigin.value = cloneFloat32Slice(o.quantizationOrigin.value)
	}

	return out
}

type encodeConfig struct {
	pointCloudMethod EncodingMethod
	meshMethod       EncodingMethod

	pointCloudMethodSet bool
	meshMethodSet       bool

	CompressConnectivity    bool
	compressConnectivitySet bool

	AttributeCompressionMode    AttributeCompressionMode
	attributeCompressionModeSet bool

	SymbolCompressionLevel int

	CompressionLevel    int
	compressionLevelSet bool

	trackEncodedProperties    bool
	trackEncodedPropertiesSet bool

	encodingSpeed    int
	encodingSpeedSet bool

	decodingSpeed    int
	decodingSpeedSet bool

	kdTreeCompressionLevel    int
	kdTreeCompressionLevelSet bool

	splitMeshOnSeams    bool
	splitMeshOnSeamsSet bool

	edgebreakerMethod    EdgebreakerMethod
	edgebreakerMethodSet bool

	attributeDefaults  map[AttributeType]*attributeEncodeOptions
	attributeOverrides map[int]*attributeEncodeOptions
}

func (o *encodeConfig) SetPointCloudMethod(method EncodingMethod) {
	o.pointCloudMethod = method
	o.pointCloudMethodSet = true
}

func (o *encodeConfig) SetMeshMethod(method EncodingMethod) {
	o.meshMethod = method
	o.meshMethodSet = true
}

func (o encodeConfig) normalizedPointCloudMethod() EncodingMethod {
	if o.pointCloudMethodSet || o.pointCloudMethod != 0 {
		return o.pointCloudMethod
	}

	return PointCloudSequentialEncoding
}

func (o encodeConfig) normalizedMeshMethod() EncodingMethod {
	if o.meshMethodSet || o.meshMethod != 0 {
		return o.meshMethod
	}

	return MeshSequentialEncoding
}

func (o *encodeConfig) SetCompressionLevel(level int) {
	o.CompressionLevel = level
	o.compressionLevelSet = true
}

func (o *encodeConfig) SetConnectivityCompression(enabled bool) {
	o.CompressConnectivity = enabled
	o.compressConnectivitySet = true
}

func (o *encodeConfig) SetUseBuiltInAttributeCompression(enabled bool) {
	if enabled {
		o.AttributeCompressionMode = AttributeCompressionStandard
	} else {
		o.AttributeCompressionMode = AttributeCompressionRaw
	}

	o.attributeCompressionModeSet = true
}

func (o *encodeConfig) SetTrackEncodedProperties(enabled bool) {
	o.trackEncodedProperties = enabled
	o.trackEncodedPropertiesSet = true
}

func (o *encodeConfig) SetSpeed(encodingSpeed, decodingSpeed int) {
	o.encodingSpeed = encodingSpeed
	o.encodingSpeedSet = true
	o.decodingSpeed = decodingSpeed
	o.decodingSpeedSet = true
}

func (o *encodeConfig) SetKDTreeCompressionLevel(level int) {
	o.kdTreeCompressionLevel = level
	o.kdTreeCompressionLevelSet = true
}

func (o *encodeConfig) SetSplitMeshOnSeams(enabled bool) {
	o.splitMeshOnSeams = enabled
	o.splitMeshOnSeamsSet = true
}

func (o *encodeConfig) SetEdgebreakerMethod(method EdgebreakerMethod) {
	o.edgebreakerMethod = method
	o.edgebreakerMethodSet = true
}

func (o *encodeConfig) attributeOptions(attType AttributeType) *attributeEncodeOptions {
	if o.attributeDefaults == nil {
		o.attributeDefaults = make(map[AttributeType]*attributeEncodeOptions)
	}

	options := o.attributeDefaults[attType]
	if options == nil {
		options = &attributeEncodeOptions{}
		o.attributeDefaults[attType] = options
	}

	return options
}

func (o *encodeConfig) attributeOptionsForID(attID int) *attributeEncodeOptions {
	if o.attributeOverrides == nil {
		o.attributeOverrides = make(map[int]*attributeEncodeOptions)
	}

	options := o.attributeOverrides[attID]
	if options == nil {
		options = &attributeEncodeOptions{}
		o.attributeOverrides[attID] = options
	}

	return options
}

func (o *encodeConfig) SetAttributeQuantization(attType AttributeType, quantizationBits int) {
	options := o.attributeOptions(attType)
	options.quantizationBits = configValue[int]{set: true, value: quantizationBits}
}

func (o *encodeConfig) SetAttributeQuantizationByID(attID int, quantizationBits int) {
	options := o.attributeOptionsForID(attID)
	options.quantizationBits = configValue[int]{set: true, value: quantizationBits}
}

func (o *encodeConfig) setAttributeQuantizationOrigin(attType AttributeType, origin []float32) {
	options := o.attributeOptions(attType)
	options.quantizationOrigin = configValue[[]float32]{set: true, value: cloneFloat32Slice(origin)}
}

func (o *encodeConfig) setAttributeQuantizationOriginByID(attID int, origin []float32) {
	options := o.attributeOptionsForID(attID)
	options.quantizationOrigin = configValue[[]float32]{set: true, value: cloneFloat32Slice(origin)}
}

func (o *encodeConfig) setAttributeQuantizationRange(attType AttributeType, rangeValue float32) {
	options := o.attributeOptions(attType)
	options.quantizationRange = configValue[float32]{set: true, value: rangeValue}
}

func (o *encodeConfig) setAttributeQuantizationRangeByID(attID int, rangeValue float32) {
	options := o.attributeOptionsForID(attID)
	options.quantizationRange = configValue[float32]{set: true, value: rangeValue}
}

func (o *encodeConfig) SetAttributeExplicitQuantization(attType AttributeType, quantizationBits int, origin []float32, rangeValue float32) {
	o.SetAttributeQuantization(attType, quantizationBits)
	o.setAttributeQuantizationOrigin(attType, origin)
	o.setAttributeQuantizationRange(attType, rangeValue)
}

func (o *encodeConfig) SetAttributeExplicitQuantizationByID(attID int, quantizationBits int, origin []float32, rangeValue float32) {
	o.SetAttributeQuantizationByID(attID, quantizationBits)
	o.setAttributeQuantizationOriginByID(attID, origin)
	o.setAttributeQuantizationRangeByID(attID, rangeValue)
}

func (o *encodeConfig) SetAttributeGridQuantization(attType AttributeType, spacing float32) {
	options := o.attributeOptions(attType)
	options.quantizationSpace = configValue[float32]{set: true, value: spacing}
}

func (o *encodeConfig) SetAttributeGridQuantizationByID(attID int, spacing float32) {
	options := o.attributeOptionsForID(attID)
	options.quantizationSpace = configValue[float32]{set: true, value: spacing}
}

func (o *encodeConfig) SetAttributePrediction(attType AttributeType, method PredictionMethod) {
	options := o.attributeOptions(attType)
	options.prediction = configValue[PredictionMethod]{set: true, value: method}
}

func (o *encodeConfig) SetAttributePredictionByID(attID int, method PredictionMethod) {
	options := o.attributeOptionsForID(attID)
	options.prediction = configValue[PredictionMethod]{set: true, value: method}
}

func (o encodeConfig) quantizationBits(attType AttributeType) int {
	if options, ok := o.attributeDefaults[attType]; ok && options != nil && options.quantizationBits.set {
		return options.quantizationBits.value
	}

	return 0
}

func (o encodeConfig) quantizationBitsForAttribute(attID int, attType AttributeType) int {
	if options, ok := o.attributeOverrides[attID]; ok && options != nil && options.quantizationBits.set {
		return options.quantizationBits.value
	}

	return o.quantizationBits(attType)
}

func (o encodeConfig) predictionMethod(attType AttributeType) PredictionMethod {
	if options, ok := o.attributeDefaults[attType]; ok && options != nil && options.prediction.set {
		return options.prediction.value
	}

	return PredictionMethodUndefined
}

func (o encodeConfig) predictionMethodForAttribute(attID int, attType AttributeType) PredictionMethod {
	if options, ok := o.attributeOverrides[attID]; ok && options != nil && options.prediction.set {
		return options.prediction.value
	}

	return o.predictionMethod(attType)
}

func (o encodeConfig) quantizationOriginForAttribute(attID int, attType AttributeType) ([]float32, bool) {
	if options, ok := o.attributeOverrides[attID]; ok && options != nil && options.quantizationOrigin.set {
		return cloneFloat32Slice(options.quantizationOrigin.value), true
	}

	if options, ok := o.attributeDefaults[attType]; ok && options != nil && options.quantizationOrigin.set {
		return cloneFloat32Slice(options.quantizationOrigin.value), true
	}

	return nil, false
}

func (o encodeConfig) quantizationRangeForAttribute(attID int, attType AttributeType) (float32, bool) {
	if options, ok := o.attributeOverrides[attID]; ok && options != nil && options.quantizationRange.set {
		return options.quantizationRange.value, true
	}

	if options, ok := o.attributeDefaults[attType]; ok && options != nil && options.quantizationRange.set {
		return options.quantizationRange.value, true
	}

	return 0, false
}

func (o encodeConfig) quantizationSpacingForAttribute(attID int, attType AttributeType) (float32, bool) {
	if options, ok := o.attributeOverrides[attID]; ok && options != nil && options.quantizationSpace.set {
		return options.quantizationSpace.value, true
	}

	if options, ok := o.attributeDefaults[attType]; ok && options != nil && options.quantizationSpace.set {
		return options.quantizationSpace.value, true
	}

	return 0, false
}

func (o encodeConfig) useBuiltInAttributeCompression() bool {
	if o.attributeCompressionModeSet || o.AttributeCompressionMode != 0 {
		return o.AttributeCompressionMode != AttributeCompressionRaw
	}

	return true
}

func (o encodeConfig) normalizedSymbolCompressionLevel() int {
	level := o.SymbolCompressionLevel
	if level == 0 {
		level = o.CompressionLevel
	}

	switch {
	case level <= 0:
		return 7
	case level > 10:
		return 10
	default:
		return level
	}
}

func (o encodeConfig) compressConnectivity() bool {
	if o.compressConnectivitySet {
		return o.CompressConnectivity
	}

	return o.CompressConnectivity
}

func (o encodeConfig) TrackEncodedProperties() bool {
	return o.trackEncodedPropertiesSet && o.trackEncodedProperties
}

func (o encodeConfig) normalizedKDTreeCompressionLevel(totalComponents int) int {
	level := minInt(10-o.Speed(), 6)
	if level == 6 && totalComponents > 15 {
		level = 5
	}

	if o.kdTreeCompressionLevelSet {
		level = o.kdTreeCompressionLevel
	}

	if level < 0 {
		return 0
	}

	if level > 6 {
		return 6
	}

	return level
}

func (o encodeConfig) splitMeshOnSeamsEnabled() (bool, bool) {
	if o.splitMeshOnSeamsSet {
		return o.splitMeshOnSeams, true
	}

	return false, false
}

func (o encodeConfig) edgebreakerMethodOr(defaultValue EdgebreakerMethod) EdgebreakerMethod {
	if o.edgebreakerMethodSet {
		return o.edgebreakerMethod
	}

	return defaultValue
}

func (o encodeConfig) EncodingSpeed() int {
	if o.encodingSpeedSet {
		return o.encodingSpeed
	}

	return 5
}

func (o encodeConfig) DecodingSpeed() int {
	if o.decodingSpeedSet {
		return o.decodingSpeed
	}

	return 5
}

func (o encodeConfig) Speed() int {
	encodingSpeed := -1
	if o.encodingSpeedSet {
		encodingSpeed = o.encodingSpeed
	}

	decodingSpeed := -1
	if o.decodingSpeedSet {
		decodingSpeed = o.decodingSpeed
	}

	if encodingSpeed < decodingSpeed {
		encodingSpeed = decodingSpeed
	}

	if encodingSpeed < 0 {
		return 5
	}

	return encodingSpeed
}

func (o encodeConfig) IsSpeedSet() bool {
	return o.encodingSpeedSet || o.decodingSpeedSet
}

// SpatialQuantizationOptions describes the quantization applied by WithSpatialQuantization.
type SpatialQuantizationOptions struct {
	quantizationBits int
	spacing          float32
	useGrid          bool
}

// NewSpatialQuantizationOptions configures bit-based quantization.
func NewSpatialQuantizationOptions(quantizationBits int) SpatialQuantizationOptions {
	return SpatialQuantizationOptions{quantizationBits: quantizationBits}
}

// AreQuantizationBitsDefined reports whether the option uses quantization bits instead of grid spacing.
func (o SpatialQuantizationOptions) AreQuantizationBitsDefined() bool {
	return !o.useGrid
}

// QuantizationBits returns the configured quantization bit width.
func (o SpatialQuantizationOptions) QuantizationBits() int {
	return o.quantizationBits
}

// SetQuantizationBits configures bit-based quantization.
func (o *SpatialQuantizationOptions) SetQuantizationBits(quantizationBits int) {
	o.quantizationBits = quantizationBits
	o.useGrid = false
}

// SetGrid configures grid-based quantization.
func (o *SpatialQuantizationOptions) SetGrid(spacing float32) {
	o.spacing = spacing
	o.useGrid = true
}

// Spacing returns the configured grid spacing.
func (o SpatialQuantizationOptions) Spacing() float32 {
	return o.spacing
}

func minInt(a, b int) int {
	if a < b {
		return a
	}

	return b
}

func cloneFloat32Slice(values []float32) []float32 {
	if values == nil {
		return nil
	}

	return append([]float32(nil), values...)
}

func cloneEncodeConfig(src encodeConfig) encodeConfig {
	out := src
	if src.attributeDefaults != nil {
		out.attributeDefaults = make(map[AttributeType]*attributeEncodeOptions, len(src.attributeDefaults))
		for key, value := range src.attributeDefaults {
			if value != nil {
				cloned := value.clone()
				out.attributeDefaults[key] = &cloned
			}
		}
	}

	if src.attributeOverrides != nil {
		out.attributeOverrides = make(map[int]*attributeEncodeOptions, len(src.attributeOverrides))
		for key, value := range src.attributeOverrides {
			if value != nil {
				cloned := value.clone()
				out.attributeOverrides[key] = &cloned
			}
		}
	}

	return out
}

func mergeEncodeConfig(base, override encodeConfig) encodeConfig {
	out := cloneEncodeConfig(base)
	if override.pointCloudMethodSet || override.pointCloudMethod != 0 {
		out.pointCloudMethod = override.pointCloudMethod
		out.pointCloudMethodSet = override.pointCloudMethodSet || override.pointCloudMethod != 0
	}

	if override.meshMethodSet || override.meshMethod != 0 {
		out.meshMethod = override.meshMethod
		out.meshMethodSet = override.meshMethodSet || override.meshMethod != 0
	}

	if override.compressConnectivitySet {
		out.CompressConnectivity = override.CompressConnectivity
		out.compressConnectivitySet = true
	}

	if override.attributeCompressionModeSet || override.AttributeCompressionMode != 0 {
		out.AttributeCompressionMode = override.AttributeCompressionMode
		out.attributeCompressionModeSet = override.attributeCompressionModeSet || override.AttributeCompressionMode != 0
	}

	if override.SymbolCompressionLevel != 0 {
		out.SymbolCompressionLevel = override.SymbolCompressionLevel
	}

	if override.compressionLevelSet || override.CompressionLevel != 0 {
		out.CompressionLevel = override.CompressionLevel
		out.compressionLevelSet = override.compressionLevelSet || override.CompressionLevel != 0
	}

	if override.trackEncodedPropertiesSet {
		out.trackEncodedProperties = override.trackEncodedProperties
		out.trackEncodedPropertiesSet = true
	}

	if override.encodingSpeedSet {
		out.encodingSpeed = override.encodingSpeed
		out.encodingSpeedSet = true
	}

	if override.decodingSpeedSet {
		out.decodingSpeed = override.decodingSpeed
		out.decodingSpeedSet = true
	}

	if override.kdTreeCompressionLevelSet {
		out.kdTreeCompressionLevel = override.kdTreeCompressionLevel
		out.kdTreeCompressionLevelSet = true
	}

	if override.splitMeshOnSeamsSet {
		out.splitMeshOnSeams = override.splitMeshOnSeams
		out.splitMeshOnSeamsSet = true
	}

	if override.edgebreakerMethodSet {
		out.edgebreakerMethod = override.edgebreakerMethod
		out.edgebreakerMethodSet = true
	}

	if override.attributeDefaults != nil {
		if out.attributeDefaults == nil {
			out.attributeDefaults = make(map[AttributeType]*attributeEncodeOptions, len(override.attributeDefaults))
		}

		for key, value := range override.attributeDefaults {
			if value == nil {
				out.attributeDefaults[key] = nil
				continue
			}

			cloned := value.clone()
			out.attributeDefaults[key] = &cloned
		}
	}

	if override.attributeOverrides != nil {
		if out.attributeOverrides == nil {
			out.attributeOverrides = make(map[int]*attributeEncodeOptions, len(override.attributeOverrides))
		}

		for key, value := range override.attributeOverrides {
			if value == nil {
				out.attributeOverrides[key] = nil
				continue
			}

			cloned := value.clone()
			out.attributeOverrides[key] = &cloned
		}
	}

	return out
}
