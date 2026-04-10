// Package metadata defines the metadata model used by Draco geometry.
package metadata

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unsafe"

	"github.com/gophics/go-draco/internal/core"
)

var (
	ErrInvalidArgument        = errors.New("metadata: invalid argument")
	ErrInvalidMetadata        = errors.New("metadata: invalid metadata")
	ErrNilGeometryMetadata    = errors.New("metadata: geometry metadata is nil")
	ErrNilAttributeMetadata   = errors.New("metadata: attribute metadata is nil")
	ErrDuplicateAttributeMeta = errors.New("metadata: duplicate attribute metadata")
	ErrNilElement             = errors.New("metadata: element is nil")
	ErrDuplicateChild         = errors.New("metadata: duplicate child metadata")
	ErrEntryNotFound          = errors.New("metadata: entry not found")
	ErrAttributeMetaNotFound  = errors.New("metadata: attribute metadata not found")
	ErrNilStructuralMetadata  = errors.New("metadata: structural metadata is nil")
	ErrNilPropertyTable       = errors.New("metadata: property table is nil")
	ErrNilPropertyAttribute   = errors.New("metadata: property attribute is nil")
	ErrNilProperty            = errors.New("metadata: property is nil")
	ErrIndexOutOfRange        = errors.New("metadata: index out of range")
)

type Entry struct {
	Key   string
	Value []byte
}

func (e Entry) Clone() Entry {
	return Entry{
		Key:   e.Key,
		Value: append([]byte(nil), e.Value...),
	}
}

func (e Entry) String() string {
	return string(e.Value)
}

type NamedElement struct {
	Key     string
	Element Element
}

func (n NamedElement) Clone() NamedElement {
	return NamedElement{
		Key:     n.Key,
		Element: n.Element.Clone(),
	}
}

type Element struct {
	Entries     []Entry
	SubMetadata []NamedElement
}

func (e *Element) Set(key, value string) error {
	return e.SetString(key, value)
}

func (e *Element) SetInt(key string, value int32) error {
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], uint32(value))
	return e.setEntryValue(key, raw[:])
}

func (e *Element) Int(key string) (int32, bool) {
	entry := e.entry(key)
	if entry == nil || len(entry.Value) != 4 {
		return 0, false
	}

	return int32(binary.LittleEndian.Uint32(entry.Value)), true
}

func (e *Element) SetIntArray(key string, value []int32) error {
	raw := make([]byte, len(value)*4)
	for i, v := range value {
		binary.LittleEndian.PutUint32(raw[i*4:], uint32(v))
	}

	return e.setEntryValue(key, raw)
}

func (e *Element) IntArray(key string) ([]int32, bool) {
	entry := e.entry(key)
	if entry == nil || len(entry.Value) == 0 || len(entry.Value)%4 != 0 {
		return nil, false
	}

	out := make([]int32, len(entry.Value)/4)
	for i := range out {
		out[i] = int32(binary.LittleEndian.Uint32(entry.Value[i*4:]))
	}

	return out, true
}

func (e *Element) SetFloat64(key string, value float64) error {
	var raw [8]byte
	binary.LittleEndian.PutUint64(raw[:], math.Float64bits(value))
	return e.setEntryValue(key, raw[:])
}

func (e *Element) Float64(key string) (float64, bool) {
	entry := e.entry(key)
	if entry == nil || len(entry.Value) != 8 {
		return 0, false
	}

	return math.Float64frombits(binary.LittleEndian.Uint64(entry.Value)), true
}

func (e *Element) SetFloat64Array(key string, value []float64) error {
	raw := make([]byte, len(value)*8)
	for i, v := range value {
		binary.LittleEndian.PutUint64(raw[i*8:], math.Float64bits(v))
	}

	return e.setEntryValue(key, raw)
}

func (e *Element) Float64Array(key string) ([]float64, bool) {
	entry := e.entry(key)
	if entry == nil || len(entry.Value) == 0 || len(entry.Value)%8 != 0 {
		return nil, false
	}

	out := make([]float64, len(entry.Value)/8)
	for i := range out {
		out[i] = math.Float64frombits(binary.LittleEndian.Uint64(entry.Value[i*8:]))
	}

	return out, true
}

func (e *Element) SetString(key, value string) error {
	return e.setEntryValue(key, []byte(value))
}

func (e *Element) String(key string) (string, bool) {
	entry := e.entry(key)
	if entry == nil || len(entry.Value) == 0 {
		return "", false
	}

	return string(entry.Value), true
}

func (e *Element) SetBinary(key string, value []byte) error {
	return e.setEntryValue(key, value)
}

func (e *Element) Binary(key string) ([]byte, bool) {
	entry := e.entry(key)
	if entry == nil || len(entry.Value) == 0 {
		return nil, false
	}

	return append([]byte(nil), entry.Value...), true
}

func (e *Element) RemoveEntry(key string) error {
	if e == nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilElement)
	}

	index := e.entryIndex(key)
	if index < 0 {
		return fmt.Errorf("%w: %w: entry %q", ErrInvalidArgument, ErrEntryNotFound, key)
	}

	e.Entries = append(e.Entries[:index], e.Entries[index+1:]...)
	return nil
}

func (e *Element) SetChild(key string, child *Element) error {
	if e == nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilElement)
	}

	if child == nil {
		return fmt.Errorf("%w: %w: child for key %q", ErrInvalidArgument, ErrNilElement, key)
	}

	if len(key) > math.MaxUint8 {
		return fmt.Errorf("%w: metadata child key %q exceeds uint8 length", ErrInvalidMetadata, key)
	}

	if err := child.Validate(); err != nil {
		return err
	}

	if e.subMetadataIndex(key) >= 0 {
		return fmt.Errorf("%w: %w: child key %q", ErrInvalidArgument, ErrDuplicateChild, key)
	}

	e.SubMetadata = append(e.SubMetadata, NamedElement{
		Key:     key,
		Element: child.Clone(),
	})
	return nil
}

func (e *Element) Child(key string) *Element {
	if e == nil {
		return nil
	}

	index := e.subMetadataIndex(key)
	if index < 0 {
		return nil
	}

	child := e.SubMetadata[index].Element.Clone()
	return &child
}

func (e *Element) Validate() error {
	if e == nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilElement)
	}

	seenEntries := make(map[string]struct{}, len(e.Entries))
	for _, entry := range e.Entries {
		if len(entry.Key) > math.MaxUint8 {
			return fmt.Errorf("%w: metadata entry key %q exceeds uint8 length", ErrInvalidMetadata, entry.Key)
		}

		if len(entry.Value) == 0 {
			return fmt.Errorf("%w: metadata entry %q has empty value", ErrInvalidMetadata, entry.Key)
		}

		if _, exists := seenEntries[entry.Key]; exists {
			return fmt.Errorf("%w: duplicate metadata entry key %q", ErrInvalidMetadata, entry.Key)
		}

		seenEntries[entry.Key] = struct{}{}
	}

	seenChildren := make(map[string]struct{}, len(e.SubMetadata))
	for i := range e.SubMetadata {
		child := &e.SubMetadata[i]
		if len(child.Key) > math.MaxUint8 {
			return fmt.Errorf("%w: metadata child key %q exceeds uint8 length", ErrInvalidMetadata, child.Key)
		}

		if _, exists := seenChildren[child.Key]; exists {
			return fmt.Errorf("%w: duplicate metadata child key %q", ErrInvalidMetadata, child.Key)
		}

		if err := child.Element.Validate(); err != nil {
			return err
		}

		seenChildren[child.Key] = struct{}{}
	}

	return nil
}

func (e *Element) Clone() Element {
	if e == nil {
		return Element{}
	}

	out := Element{
		Entries:     make([]Entry, len(e.Entries)),
		SubMetadata: make([]NamedElement, len(e.SubMetadata)),
	}
	for i, entry := range e.Entries {
		out.Entries[i] = entry.Clone()
	}

	for i, sub := range e.SubMetadata {
		out.SubMetadata[i] = sub.Clone()
	}

	return out
}

func (e Element) Equal(other Element) bool {
	if len(e.Entries) != len(other.Entries) || len(e.SubMetadata) != len(other.SubMetadata) {
		return false
	}

	for i := range e.Entries {
		if e.Entries[i].Key != other.Entries[i].Key || !bytes.Equal(e.Entries[i].Value, other.Entries[i].Value) {
			return false
		}
	}

	for i := range e.SubMetadata {
		if e.SubMetadata[i].Key != other.SubMetadata[i].Key || !e.SubMetadata[i].Element.Equal(other.SubMetadata[i].Element) {
			return false
		}
	}

	return true
}

type AttributeMetadata struct {
	AttributeUniqueID uint32
	Element           Element
}

func (a *AttributeMetadata) Clone() *AttributeMetadata {
	if a == nil {
		return nil
	}

	return &AttributeMetadata{
		AttributeUniqueID: a.AttributeUniqueID,
		Element:           a.Element.Clone(),
	}
}

func (a *AttributeMetadata) Validate() error {
	if a == nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilAttributeMetadata)
	}

	return a.Element.Validate()
}

type GeometryMetadata struct {
	Attributes []AttributeMetadata
	Root       Element
}

func (g *GeometryMetadata) Clone() *GeometryMetadata {
	if g == nil {
		return nil
	}

	out := &GeometryMetadata{
		Attributes: make([]AttributeMetadata, len(g.Attributes)),
		Root:       g.Root.Clone(),
	}
	for i := range g.Attributes {
		out.Attributes[i] = AttributeMetadata{
			AttributeUniqueID: g.Attributes[i].AttributeUniqueID,
			Element:           g.Attributes[i].Element.Clone(),
		}
	}

	return out
}

func (g *GeometryMetadata) Equal(other *GeometryMetadata) bool {
	if g == nil || other == nil {
		return g == other
	}

	if len(g.Attributes) != len(other.Attributes) || !g.Root.Equal(other.Root) {
		return false
	}

	for i := range g.Attributes {
		if g.Attributes[i].AttributeUniqueID != other.Attributes[i].AttributeUniqueID ||
			!g.Attributes[i].Element.Equal(other.Attributes[i].Element) {
			return false
		}
	}

	return true
}

func (g *GeometryMetadata) AddAttributeMetadata(attribute *AttributeMetadata) error {
	if g == nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilGeometryMetadata)
	}

	if attribute == nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilAttributeMetadata)
	}

	clone := attribute.Clone()
	if err := clone.Validate(); err != nil {
		return err
	}

	if g.attributeMetadataByUniqueIDRef(clone.AttributeUniqueID) != nil {
		return fmt.Errorf("%w: %w: unique id %d", ErrInvalidArgument, ErrDuplicateAttributeMeta, attribute.AttributeUniqueID)
	}

	g.Attributes = append(g.Attributes, *clone)
	return nil
}

func (g *GeometryMetadata) AttributeMetadataByUniqueID(uniqueID uint32) *AttributeMetadata {
	attribute := g.attributeMetadataByUniqueIDRef(uniqueID)
	if attribute == nil {
		return nil
	}

	return attribute.Clone()
}

func (g *GeometryMetadata) attributeMetadataByUniqueIDRef(uniqueID uint32) *AttributeMetadata {
	if g == nil {
		return nil
	}

	for i := range g.Attributes {
		if g.Attributes[i].AttributeUniqueID == uniqueID {
			return &g.Attributes[i]
		}
	}

	return nil
}

func (g *GeometryMetadata) DeleteAttributeMetadataByUniqueID(uniqueID uint32) error {
	if g == nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilGeometryMetadata)
	}

	for i := range g.Attributes {
		if g.Attributes[i].AttributeUniqueID == uniqueID {
			g.Attributes = append(g.Attributes[:i], g.Attributes[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("%w: %w: unique id %d", ErrInvalidArgument, ErrAttributeMetaNotFound, uniqueID)
}

func (g *GeometryMetadata) AttributeMetadataByStringEntry(entryName, entryValue string) *AttributeMetadata {
	if g == nil {
		return nil
	}

	for i := range g.Attributes {
		value, ok := g.Attributes[i].Element.String(entryName)
		if ok && value == entryValue {
			return g.Attributes[i].Clone()
		}
	}

	return nil
}

func (g *GeometryMetadata) Validate() error {
	if g == nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilGeometryMetadata)
	}

	if err := g.Root.Validate(); err != nil {
		return err
	}

	seenUniqueIDs := make(map[uint32]struct{}, len(g.Attributes))
	for i := range g.Attributes {
		attribute := &g.Attributes[i]
		if err := attribute.Validate(); err != nil {
			return err
		}

		if _, exists := seenUniqueIDs[attribute.AttributeUniqueID]; exists {
			return fmt.Errorf("%w: duplicate attribute unique id %d", ErrInvalidMetadata, attribute.AttributeUniqueID)
		}

		seenUniqueIDs[attribute.AttributeUniqueID] = struct{}{}
	}

	return nil
}

func EncodeGeometryMetadata(w *core.Writer, gm *GeometryMetadata) error {
	if gm == nil {
		return nil
	}

	if err := gm.Validate(); err != nil {
		return err
	}

	if err := guardMetadataUint32Len(len(gm.Attributes), "geometry metadata attribute count"); err != nil {
		return err
	}

	if err := core.EncodeVarUint32(w, uint32(len(gm.Attributes))); err != nil {
		return err
	}

	for _, attr := range gm.Attributes {
		if err := core.EncodeVarUint32(w, attr.AttributeUniqueID); err != nil {
			return err
		}

		if err := encodeElement(w, attr.Element); err != nil {
			return err
		}
	}

	return encodeElement(w, gm.Root)
}

func DecodeGeometryMetadata(ctx context.Context, r *core.Reader) (*GeometryMetadata, error) {
	if err := checkDecodeContext(ctx); err != nil {
		return nil, err
	}

	numAttributes, err := core.DecodeVarUint32(r)
	if err != nil {
		return nil, err
	}

	attrCount, err := guardedMetadataSliceLen(numAttributes, unsafe.Sizeof(AttributeMetadata{}))
	if err != nil {
		return nil, err
	}

	gm := &GeometryMetadata{
		Attributes: make([]AttributeMetadata, attrCount),
	}
	seenIDs := make(map[uint32]struct{}, attrCount)
	for i := 0; i < int(numAttributes); i++ {
		if err := checkDecodeContextEvery(ctx, i); err != nil {
			return nil, err
		}

		uniqueID, err := core.DecodeVarUint32(r)
		if err != nil {
			return nil, err
		}

		if _, exists := seenIDs[uniqueID]; exists {
			return nil, fmt.Errorf("%w: duplicate attribute unique id %d", ErrInvalidMetadata, uniqueID)
		}

		element, err := decodeElement(ctx, r, 0)
		if err != nil {
			return nil, err
		}

		gm.Attributes[i] = AttributeMetadata{
			AttributeUniqueID: uniqueID,
			Element:           element,
		}
		seenIDs[uniqueID] = struct{}{}
	}

	root, err := decodeElement(ctx, r, 0)
	if err != nil {
		return nil, err
	}

	gm.Root = root
	if err := gm.Validate(); err != nil {
		return nil, err
	}

	return gm, nil
}

func encodeElement(w *core.Writer, element Element) error {
	if err := guardMetadataUint32Len(len(element.Entries), "metadata entry count"); err != nil {
		return err
	}

	if err := core.EncodeVarUint32(w, uint32(len(element.Entries))); err != nil {
		return err
	}

	for _, entry := range element.Entries {
		if len(entry.Value) == 0 {
			return fmt.Errorf("%w: metadata entry %q has empty value", ErrInvalidMetadata, entry.Key)
		}

		if err := guardMetadataUint8Len(len(entry.Key), fmt.Sprintf("metadata entry key %q length", entry.Key)); err != nil {
			return err
		}

		if err := guardMetadataUint32Len(len(entry.Value), fmt.Sprintf("metadata entry %q value length", entry.Key)); err != nil {
			return err
		}

		if err := w.WriteUint8(uint8(len(entry.Key))); err != nil {
			return err
		}

		if err := w.WriteBytes([]byte(entry.Key)); err != nil {
			return err
		}

		if err := core.EncodeVarUint32(w, uint32(len(entry.Value))); err != nil {
			return err
		}

		if err := w.WriteBytes(entry.Value); err != nil {
			return err
		}
	}

	if err := guardMetadataUint32Len(len(element.SubMetadata), "metadata child count"); err != nil {
		return err
	}

	if err := core.EncodeVarUint32(w, uint32(len(element.SubMetadata))); err != nil {
		return err
	}

	for _, sub := range element.SubMetadata {
		if err := guardMetadataUint8Len(len(sub.Key), fmt.Sprintf("metadata child key %q length", sub.Key)); err != nil {
			return err
		}

		if err := w.WriteUint8(uint8(len(sub.Key))); err != nil {
			return err
		}

		if err := w.WriteBytes([]byte(sub.Key)); err != nil {
			return err
		}

		if err := encodeElement(w, sub.Element); err != nil {
			return err
		}
	}

	return nil
}

func decodeElement(ctx context.Context, r *core.Reader, depth int) (Element, error) {
	if err := checkDecodeContext(ctx); err != nil {
		return Element{}, err
	}

	if depth > maxDecodedMetadataDepth {
		return Element{}, fmt.Errorf("%w: metadata nesting exceeds max depth %d", ErrInvalidMetadata, maxDecodedMetadataDepth)
	}

	numEntries, err := core.DecodeVarUint32(r)
	if err != nil {
		return Element{}, err
	}

	entryCount, err := guardedMetadataSliceLen(numEntries, unsafe.Sizeof(Entry{}))
	if err != nil {
		return Element{}, err
	}

	element := Element{
		Entries: make([]Entry, entryCount),
	}
	seenEntries := make(map[string]struct{}, entryCount)
	for i := 0; i < int(numEntries); i++ {
		if err := checkDecodeContextEvery(ctx, i); err != nil {
			return Element{}, err
		}

		keyLen, err := r.ReadUint8()
		if err != nil {
			return Element{}, err
		}

		key, err := r.ReadBytes(int(keyLen))
		if err != nil {
			return Element{}, err
		}

		valueLen, err := core.DecodeVarUint32(r)
		if err != nil {
			return Element{}, err
		}

		if valueLen == 0 {
			return Element{}, fmt.Errorf("%w: metadata entry %q has empty value", ErrInvalidMetadata, string(key))
		}

		if uint64(valueLen) > uint64(r.Remaining()) {
			return Element{}, fmt.Errorf("%w: metadata entry %q exceeds remaining buffer", ErrInvalidMetadata, string(key))
		}

		keyString := string(key)
		if _, exists := seenEntries[keyString]; exists {
			return Element{}, fmt.Errorf("%w: duplicate metadata entry key %q", ErrInvalidMetadata, keyString)
		}

		value, err := r.ReadBytes(int(valueLen))
		if err != nil {
			return Element{}, err
		}

		element.Entries[i] = Entry{Key: keyString, Value: value}
		seenEntries[keyString] = struct{}{}
	}

	numSub, err := core.DecodeVarUint32(r)
	if err != nil {
		return Element{}, err
	}

	subCount, err := guardedMetadataSliceLen(numSub, unsafe.Sizeof(NamedElement{}))
	if err != nil {
		return Element{}, err
	}

	if uint64(numSub) > uint64(r.Remaining()) {
		return Element{}, fmt.Errorf("%w: metadata child count exceeds remaining buffer", ErrInvalidMetadata)
	}

	element.SubMetadata = make([]NamedElement, subCount)
	seenChildren := make(map[string]struct{}, subCount)
	for i := 0; i < int(numSub); i++ {
		if err := checkDecodeContextEvery(ctx, i); err != nil {
			return Element{}, err
		}

		keyLen, err := r.ReadUint8()
		if err != nil {
			return Element{}, err
		}

		key, err := r.ReadBytes(int(keyLen))
		if err != nil {
			return Element{}, err
		}

		keyString := string(key)
		if _, exists := seenChildren[keyString]; exists {
			return Element{}, fmt.Errorf("%w: duplicate metadata child key %q", ErrInvalidMetadata, keyString)
		}

		child, err := decodeElement(ctx, r, depth+1)
		if err != nil {
			return Element{}, err
		}

		element.SubMetadata[i] = NamedElement{Key: keyString, Element: child}
		seenChildren[keyString] = struct{}{}
	}

	return element, nil
}

func (e *Element) setEntryValue(key string, value []byte) error {
	if e == nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilElement)
	}

	if len(key) > math.MaxUint8 {
		return fmt.Errorf("%w: metadata entry key %q exceeds uint8 length", ErrInvalidMetadata, key)
	}

	if len(value) == 0 {
		return fmt.Errorf("%w: metadata entry %q has empty value", ErrInvalidMetadata, key)
	}

	entry := Entry{
		Key:   key,
		Value: append([]byte(nil), value...),
	}
	index := e.entryIndex(key)
	if index >= 0 {
		e.Entries[index] = entry
		return nil
	}

	e.Entries = append(e.Entries, entry)
	return nil
}

func (e *Element) entry(key string) *Entry {
	index := e.entryIndex(key)
	if index < 0 {
		return nil
	}

	return &e.Entries[index]
}

func (e *Element) entryIndex(key string) int {
	if e == nil {
		return -1
	}

	for i := range e.Entries {
		if e.Entries[i].Key == key {
			return i
		}
	}

	return -1
}

func (e *Element) subMetadataIndex(key string) int {
	if e == nil {
		return -1
	}

	for i := range e.SubMetadata {
		if e.SubMetadata[i].Key == key {
			return i
		}
	}

	return -1
}

const maxDecodedMetadataAllocBytes = 512 << 20
const maxDecodedMetadataDepth = 256
const metadataContextCheckInterval = 128

func checkDecodeContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: context is nil", ErrInvalidArgument)
	}

	return ctx.Err()
}

func checkDecodeContextEvery(ctx context.Context, index int) error {
	if index%metadataContextCheckInterval != 0 {
		return nil
	}

	return checkDecodeContext(ctx)
}

func guardedMetadataSliceLen(count uint32, elemSize uintptr) (int, error) {
	if count == 0 {
		return 0, nil
	}

	if elemSize == 0 {
		return 0, fmt.Errorf("%w: invalid metadata allocation element size", ErrInvalidMetadata)
	}

	maxInt := int(^uint(0) >> 1)
	if uint64(count) > uint64(maxInt)/uint64(elemSize) {
		return 0, fmt.Errorf("%w: decoded metadata allocation exceeds max int", ErrInvalidMetadata)
	}

	total := uint64(count) * uint64(elemSize)
	if total > maxDecodedMetadataAllocBytes {
		return 0, fmt.Errorf("%w: decoded metadata allocation of %d bytes exceeds %d-byte limit", ErrInvalidMetadata, total, maxDecodedMetadataAllocBytes)
	}

	return int(count), nil
}

func guardMetadataUint32Len(length int, label string) error {
	if length < 0 {
		return fmt.Errorf("%w: %s is negative", ErrInvalidMetadata, label)
	}

	if uint64(length) > math.MaxUint32 {
		return fmt.Errorf("%w: %s %d exceeds uint32 range", ErrInvalidMetadata, label, length)
	}

	return nil
}

func guardMetadataUint8Len(length int, label string) error {
	if length < 0 {
		return fmt.Errorf("%w: %s is negative", ErrInvalidMetadata, label)
	}

	if length > math.MaxUint8 {
		return fmt.Errorf("%w: %s %d exceeds uint8 range", ErrInvalidMetadata, label, length)
	}

	return nil
}
