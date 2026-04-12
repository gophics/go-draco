package draco

import (
	"errors"
	"fmt"

	md "github.com/gophics/go-draco/metadata"
)

type PointCloud struct {
	numPoints  int
	attributes []*Attribute
	nextUnique uint32
	metadata   *md.GeometryMetadata
}

// NewPointCloud constructs a validating point cloud from a point count and attributes.
func NewPointCloud(numPoints int, attrs ...*Attribute) (*PointCloud, error) {
	pc := newPointCloud(numPoints)
	for _, attr := range attrs {
		if _, err := pc.AddAttribute(attr); err != nil {
			return nil, err
		}
	}

	if err := pc.Validate(); err != nil {
		return nil, err
	}

	return pc, nil
}

func newPointCloud(numPoints int) *PointCloud {
	return &PointCloud{numPoints: numPoints}
}

func (pc *PointCloud) GeometryType() EncodedGeometryType { return PointCloudGeometry }

func (pc *PointCloud) PointCount() int {
	if pc == nil {
		return 0
	}

	return pc.numPoints
}

func (pc *PointCloud) setPointCount(numPoints int) { pc.numPoints = numPoints }

func (pc *PointCloud) AttributeCount() int {
	if pc == nil {
		return 0
	}

	return len(pc.attributes)
}

func (pc *PointCloud) MetadataClone() *md.GeometryMetadata {
	if pc == nil {
		return nil
	}

	return pc.metadata.Clone()
}

func (pc *PointCloud) SetMetadata(metadata *md.GeometryMetadata) error {
	if pc == nil {
		return fmt.Errorf("%w: point cloud is nil", ErrInvalidGeometry)
	}

	clone := metadata.Clone()
	if clone != nil {
		if err := clone.Validate(); err != nil {
			return err
		}

		if err := pc.validateMetadataReferences(clone); err != nil {
			return err
		}
	}

	pc.metadata = clone
	return nil
}

func (pc *PointCloud) Attributes() []*Attribute {
	if pc == nil {
		return nil
	}

	out := make([]*Attribute, len(pc.attributes))
	for i, attr := range pc.attributes {
		out[i] = attr.Clone()
	}

	return out
}

func (pc *PointCloud) Attribute(i int) (*Attribute, error) {
	if pc == nil {
		return nil, fmt.Errorf("%w: point cloud is nil", ErrInvalidGeometry)
	}

	if i < 0 || i >= len(pc.attributes) {
		return nil, fmt.Errorf("%w: attribute %d out of range", ErrInvalidGeometry, i)
	}

	return pc.attributes[i].Clone(), nil
}

func (pc *PointCloud) attribute(i int) *Attribute {
	return pc.attributes[i]
}

func (pc *PointCloud) AttributeByUniqueID(id uint32) *Attribute {
	attr := pc.attributeByUniqueID(id)
	if attr == nil {
		return nil
	}

	return attr.Clone()
}

func (pc *PointCloud) attributeByUniqueID(id uint32) *Attribute {
	if pc == nil {
		return nil
	}

	for _, attr := range pc.attributes {
		if attr.UniqueID == id {
			return attr
		}
	}

	return nil
}

func (pc *PointCloud) AddAttribute(attr *Attribute) (int, error) {
	return pc.addAttribute(attr, true)
}

func (pc *PointCloud) addAttributeOwned(attr *Attribute) (int, error) {
	return pc.addAttribute(attr, false)
}

func (pc *PointCloud) addAttribute(attr *Attribute, clone bool) (int, error) {
	if pc == nil {
		return -1, fmt.Errorf("%w: point cloud is nil", ErrInvalidGeometry)
	}

	if attr == nil {
		return -1, fmt.Errorf("%w: attribute is nil", ErrInvalidGeometry)
	}

	if attr.IsIdentityMapping() {
		if attr.EntryCount() < pc.numPoints {
			return -1, fmt.Errorf("%w: attribute has %d entries for %d points", ErrInvalidGeometry, attr.EntryCount(), pc.numPoints)
		}
	} else {
		if attr.MappingSize() < pc.numPoints {
			return -1, fmt.Errorf("%w: attribute mapping has %d entries for %d points", ErrInvalidGeometry, attr.MappingSize(), pc.numPoints)
		}

		for pointID := 0; pointID < pc.numPoints; pointID++ {
			if int(attr.mapping[pointID]) >= attr.EntryCount() {
				return -1, fmt.Errorf("%w: attribute mapping for point %d references entry %d out of %d", ErrInvalidGeometry, pointID, attr.mapping[pointID], attr.EntryCount())
			}
		}
	}

	if clone {
		attr = attr.Clone()
	}

	if pc.uniqueIDInUse(attr.UniqueID) {
		attr.UniqueID = pc.allocateUniqueID()
	} else if attr.UniqueID == 0 && pc.nextUnique == 0 {
		pc.nextUnique = 1
	} else if attr.UniqueID >= pc.nextUnique {
		pc.nextUnique = attr.UniqueID + 1
	}

	pc.attributes = append(pc.attributes, attr)
	return len(pc.attributes) - 1, nil
}

func (pc *PointCloud) NamedAttribute(attType AttributeType) *Attribute {
	attr := pc.namedAttribute(attType)
	if attr == nil {
		return nil
	}

	return attr.Clone()
}

func (pc *PointCloud) namedAttribute(attType AttributeType) *Attribute {
	if pc == nil {
		return nil
	}

	for _, attr := range pc.attributes {
		if attr.Type == attType {
			return attr
		}
	}

	return nil
}

func (pc *PointCloud) NamedAttributeByName(attType AttributeType, name string) *Attribute {
	attr := pc.namedAttributeByName(attType, name)
	if attr == nil {
		return nil
	}

	return attr.Clone()
}

func (pc *PointCloud) namedAttributeByName(attType AttributeType, name string) *Attribute {
	if pc == nil {
		return nil
	}

	for _, attr := range pc.attributes {
		if attr.Type == attType && attr.Name == name {
			return attr
		}
	}

	return nil
}

func (pc *PointCloud) NamedAttributeID(attType AttributeType, occurrence int) int {
	if pc == nil {
		return -1
	}

	if occurrence < 0 {
		return -1
	}

	count := 0
	for attID, attr := range pc.attributes {
		if attr.Type != attType {
			continue
		}

		if count == occurrence {
			return attID
		}

		count++
	}

	return -1
}

func (pc *PointCloud) NamedAttributeCount(attType AttributeType) int {
	if pc == nil {
		return 0
	}

	count := 0
	for _, attr := range pc.attributes {
		if attr.Type == attType {
			count++
		}
	}

	return count
}

func (pc *PointCloud) AttributeIDByUniqueID(uniqueID uint32) int {
	if pc == nil {
		return -1
	}

	for attID, attr := range pc.attributes {
		if attr.UniqueID == uniqueID {
			return attID
		}
	}

	return -1
}

func (pc *PointCloud) DeleteAttribute(attID int) error {
	if pc == nil {
		return fmt.Errorf("%w: point cloud is nil", ErrInvalidGeometry)
	}

	if attID < 0 || attID >= len(pc.attributes) {
		return fmt.Errorf("%w: attribute %d out of range", ErrInvalidGeometry, attID)
	}

	uniqueID := pc.attributes[attID].UniqueID
	pc.attributes = append(pc.attributes[:attID], pc.attributes[attID+1:]...)
	if pc.metadata != nil {
		if err := pc.metadata.DeleteAttributeMetadataByUniqueID(uniqueID); err != nil && !errors.Is(err, md.ErrAttributeMetaNotFound) {
			return err
		}
	}

	return nil
}

func (pc *PointCloud) AddAttributeMetadata(attID int, metadata *md.AttributeMetadata) error {
	if pc == nil {
		return fmt.Errorf("%w: point cloud is nil", ErrInvalidGeometry)
	}

	if attID < 0 || attID >= len(pc.attributes) {
		return fmt.Errorf("%w: attribute %d out of range", ErrInvalidGeometry, attID)
	}

	if metadata == nil {
		return fmt.Errorf("%w: %w", ErrInvalidGeometry, md.ErrNilAttributeMetadata)
	}

	target := pc.metadata.Clone()
	if target == nil {
		target = &md.GeometryMetadata{}
	}

	copyMetadata := metadata.Clone()
	copyMetadata.AttributeUniqueID = pc.attributes[attID].UniqueID
	if err := target.AddAttributeMetadata(copyMetadata); err != nil {
		return err
	}

	pc.metadata = target
	return nil
}

func (pc *PointCloud) AttributeMetadata(attID int) *md.AttributeMetadata {
	if pc == nil || pc.metadata == nil || attID < 0 || attID >= len(pc.attributes) {
		return nil
	}

	metadata := pc.metadata.AttributeMetadataByUniqueID(pc.attributes[attID].UniqueID)
	if metadata == nil {
		return nil
	}

	return metadata.Clone()
}

func (pc *PointCloud) AttributeMetadataByStringEntry(entryName, entryValue string) *md.AttributeMetadata {
	if pc == nil || pc.metadata == nil {
		return nil
	}

	metadata := pc.metadata.AttributeMetadataByStringEntry(entryName, entryValue)
	if metadata == nil {
		return nil
	}

	return metadata.Clone()
}

func (pc *PointCloud) Validate() error {
	if pc == nil {
		return fmt.Errorf("%w: point cloud is nil", ErrInvalidGeometry)
	}

	if pc.numPoints < 0 {
		return fmt.Errorf("%w: invalid point count %d", ErrInvalidGeometry, pc.numPoints)
	}

	if pc.metadata != nil {
		if err := pc.metadata.Validate(); err != nil {
			return err
		}

		if err := pc.validateMetadataReferences(pc.metadata); err != nil {
			return err
		}
	}

	var seenUniqueIDs map[uint32]int
	var smallUniqueIDs [16]uint32
	useSmallUniqueIDs := len(pc.attributes) <= len(smallUniqueIDs)
	if !useSmallUniqueIDs {
		seenUniqueIDs = make(map[uint32]int, len(pc.attributes))
	}

	for attrID, attr := range pc.attributes {
		if attr == nil {
			return fmt.Errorf("%w: attribute %d is nil", ErrInvalidGeometry, attrID)
		}

		if attr.ByteStride() == 0 {
			return fmt.Errorf("%w: attribute %d has invalid data type %s", ErrInvalidGeometry, attrID, attr.DataType)
		}

		if attr.NumComponents <= 0 {
			return fmt.Errorf("%w: attribute %d has invalid component count %d", ErrInvalidGeometry, attrID, attr.NumComponents)
		}

		if attr.IsIdentityMapping() {
			if attr.EntryCount() < pc.numPoints {
				return fmt.Errorf("%w: attribute %d has %d entries for %d points", ErrInvalidGeometry, attrID, attr.EntryCount(), pc.numPoints)
			}
		} else {
			if attr.MappingSize() < pc.numPoints {
				return fmt.Errorf("%w: attribute %d mapping has %d entries for %d points", ErrInvalidGeometry, attrID, attr.MappingSize(), pc.numPoints)
			}

			for pointID := 0; pointID < pc.numPoints; pointID++ {
				entryID := attr.mapping[pointID]
				if int(entryID) >= attr.EntryCount() {
					return fmt.Errorf("%w: attribute %d mapping for point %d references entry %d out of %d", ErrInvalidGeometry, attrID, pointID, entryID, attr.EntryCount())
				}
			}
		}

		if useSmallUniqueIDs {
			for previous := range attrID {
				if smallUniqueIDs[previous] == attr.UniqueID {
					return fmt.Errorf("%w: duplicate attribute unique id %d for attributes %d and %d", ErrInvalidGeometry, attr.UniqueID, previous, attrID)
				}
			}

			smallUniqueIDs[attrID] = attr.UniqueID
		} else {
			if previous, ok := seenUniqueIDs[attr.UniqueID]; ok {
				return fmt.Errorf("%w: duplicate attribute unique id %d for attributes %d and %d", ErrInvalidGeometry, attr.UniqueID, previous, attrID)
			}

			seenUniqueIDs[attr.UniqueID] = attrID
		}
	}

	return nil
}

func (pc *PointCloud) validateMetadataReferences(metadata *md.GeometryMetadata) error {
	if pc == nil || metadata == nil {
		return nil
	}

	seenUniqueIDs := make(map[uint32]struct{}, len(pc.attributes))
	for _, attr := range pc.attributes {
		if attr == nil {
			continue
		}

		seenUniqueIDs[attr.UniqueID] = struct{}{}
	}

	for _, attributeMetadata := range metadata.Attributes {
		if _, ok := seenUniqueIDs[attributeMetadata.AttributeUniqueID]; !ok {
			return fmt.Errorf("%w: metadata references missing attribute unique id %d", ErrInvalidGeometry, attributeMetadata.AttributeUniqueID)
		}
	}

	return nil
}

func (pc *PointCloud) Equivalent(other *PointCloud) bool {
	if pc == nil || other == nil {
		return pc == other
	}

	return pointCloudEquivalent(pc, other)
}

func (pc *PointCloud) Clone() *PointCloud {
	if pc == nil {
		return nil
	}

	out := &PointCloud{
		numPoints:  pc.numPoints,
		attributes: make([]*Attribute, len(pc.attributes)),
		nextUnique: pc.nextUnique,
		metadata:   pc.metadata.Clone(),
	}
	for i := range pc.attributes {
		out.attributes[i] = pc.attributes[i].Clone()
	}

	return out
}

func (pc *PointCloud) uniqueIDInUse(id uint32) bool {
	if pc == nil {
		return false
	}

	if id == 0 && len(pc.attributes) == 0 {
		return false
	}

	for _, attr := range pc.attributes {
		if attr.UniqueID == id {
			return true
		}
	}

	return false
}

func (pc *PointCloud) allocateUniqueID() uint32 {
	if pc == nil {
		return 0
	}

	id := pc.nextUnique
	for pc.uniqueIDInUse(id) {
		id++
	}

	pc.nextUnique = id + 1
	return id
}

func (pc *PointCloud) metadataRef() *md.GeometryMetadata {
	if pc == nil {
		return nil
	}

	return pc.metadata
}

func (pc *PointCloud) setMetadata(metadata *md.GeometryMetadata) {
	if pc == nil {
		return
	}

	pc.metadata = metadata
}
