package metadata

import (
	"encoding/binary"
	"fmt"
	"math"
)

type PropertyTablePropertyData struct {
	Data   []byte
	Target int
}

func (d PropertyTablePropertyData) Validate() error {
	if d.Target < 0 {
		return fmt.Errorf("%w: property table data target %d is negative", ErrInvalidMetadata, d.Target)
	}

	return nil
}

func (d PropertyTablePropertyData) Clone() PropertyTablePropertyData {
	return PropertyTablePropertyData{
		Data:   append([]byte(nil), d.Data...),
		Target: d.Target,
	}
}

func (d PropertyTablePropertyData) Equal(other PropertyTablePropertyData) bool {
	if d.Target != other.Target || len(d.Data) != len(other.Data) {
		return false
	}

	for i := range d.Data {
		if d.Data[i] != other.Data[i] {
			return false
		}
	}

	return true
}

type PropertyTablePropertyOffsets struct {
	Data PropertyTablePropertyData
	Type string
}

func (o PropertyTablePropertyOffsets) Validate() error {
	if err := o.Data.Validate(); err != nil {
		return err
	}

	if len(o.Data.Data) == 0 {
		if o.Type != "" {
			return fmt.Errorf("%w: offsets type %q requires offset data", ErrInvalidMetadata, o.Type)
		}

		return nil
	}

	_, err := o.ParseToInts()
	return err
}

func MakePropertyTablePropertyOffsetsFromInts(ints []uint64) PropertyTablePropertyOffsets {
	var maxValue uint64
	for _, value := range ints {
		if value > maxValue {
			maxValue = value
		}
	}

	var (
		typeName    string
		bytesPerInt int
	)
	switch {
	case maxValue <= math.MaxUint8:
		typeName = "UINT8"
		bytesPerInt = 1
	case maxValue <= math.MaxUint16:
		typeName = "UINT16"
		bytesPerInt = 2
	case maxValue <= math.MaxUint32:
		typeName = "UINT32"
		bytesPerInt = 4
	default:
		typeName = "UINT64"
		bytesPerInt = 8
	}

	data := make([]byte, len(ints)*bytesPerInt)
	for i, value := range ints {
		offset := i * bytesPerInt
		switch bytesPerInt {
		case 1:
			data[offset] = byte(value)
		case 2:
			binary.LittleEndian.PutUint16(data[offset:], uint16(value))
		case 4:
			binary.LittleEndian.PutUint32(data[offset:], uint32(value))
		case 8:
			binary.LittleEndian.PutUint64(data[offset:], value)
		}
	}

	return PropertyTablePropertyOffsets{
		Data: PropertyTablePropertyData{Data: data},
		Type: typeName,
	}
}

func (o PropertyTablePropertyOffsets) Clone() PropertyTablePropertyOffsets {
	return PropertyTablePropertyOffsets{
		Data: o.Data.Clone(),
		Type: o.Type,
	}
}

func (o PropertyTablePropertyOffsets) Equal(other PropertyTablePropertyOffsets) bool {
	return o.Type == other.Type && o.Data.Equal(other.Data)
}

func (o PropertyTablePropertyOffsets) ParseToInts() ([]uint64, error) {
	if len(o.Data.Data) == 0 {
		return nil, nil
	}

	var bytesPerInt int
	switch o.Type {
	case "UINT8":
		bytesPerInt = 1
	case "UINT16":
		bytesPerInt = 2
	case "UINT32":
		bytesPerInt = 4
	case "UINT64":
		bytesPerInt = 8
	default:
		return nil, fmt.Errorf("%w: offsets data type invalid", ErrInvalidMetadata)
	}

	if len(o.Data.Data)%bytesPerInt != 0 {
		return nil, fmt.Errorf("%w: offsets byte length %d is not divisible by %d", ErrInvalidMetadata, len(o.Data.Data), bytesPerInt)
	}

	count := len(o.Data.Data) / bytesPerInt
	out := make([]uint64, count)
	for i := 0; i < count; i++ {
		offset := i * bytesPerInt
		switch bytesPerInt {
		case 1:
			out[i] = uint64(o.Data.Data[offset])
		case 2:
			out[i] = uint64(binary.LittleEndian.Uint16(o.Data.Data[offset:]))
		case 4:
			out[i] = uint64(binary.LittleEndian.Uint32(o.Data.Data[offset:]))
		case 8:
			out[i] = binary.LittleEndian.Uint64(o.Data.Data[offset:])
		}
	}

	return out, nil
}

type PropertyTableProperty struct {
	Name          string
	Data          PropertyTablePropertyData
	ArrayOffsets  PropertyTablePropertyOffsets
	StringOffsets PropertyTablePropertyOffsets
}

func (p PropertyTableProperty) Validate() error {
	if err := p.Data.Validate(); err != nil {
		return err
	}

	if err := p.ArrayOffsets.Validate(); err != nil {
		return err
	}

	if err := p.StringOffsets.Validate(); err != nil {
		return err
	}

	return nil
}

func (p PropertyTableProperty) Clone() *PropertyTableProperty {
	return &PropertyTableProperty{
		Name:          p.Name,
		Data:          p.Data.Clone(),
		ArrayOffsets:  p.ArrayOffsets.Clone(),
		StringOffsets: p.StringOffsets.Clone(),
	}
}

func (p PropertyTableProperty) Equal(other PropertyTableProperty) bool {
	return p.Name == other.Name &&
		p.Data.Equal(other.Data) &&
		p.ArrayOffsets.Equal(other.ArrayOffsets) &&
		p.StringOffsets.Equal(other.StringOffsets)
}

type PropertyTable struct {
	Name       string
	Class      string
	Count      int
	Properties []*PropertyTableProperty
}

func (p *PropertyTable) AddProperty(property *PropertyTableProperty) (int, error) {
	if p == nil {
		return -1, fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilPropertyTable)
	}

	if property == nil {
		return -1, fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilProperty)
	}

	if err := property.Validate(); err != nil {
		return -1, err
	}

	p.Properties = append(p.Properties, property.Clone())
	return len(p.Properties) - 1, nil
}

func (p *PropertyTable) PropertyCount() int {
	if p == nil {
		return 0
	}

	return len(p.Properties)
}

func (p *PropertyTable) Property(index int) (*PropertyTableProperty, error) {
	if p == nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilPropertyTable)
	}

	if index < 0 || index >= len(p.Properties) {
		return nil, fmt.Errorf("%w: %w: property table property %d", ErrInvalidArgument, ErrIndexOutOfRange, index)
	}

	return p.Properties[index].Clone(), nil
}

func (p *PropertyTable) RemoveProperty(index int) error {
	if p == nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilPropertyTable)
	}

	if index < 0 || index >= len(p.Properties) {
		return fmt.Errorf("%w: %w: property table property %d", ErrInvalidArgument, ErrIndexOutOfRange, index)
	}

	p.Properties = append(p.Properties[:index], p.Properties[index+1:]...)
	return nil
}

func (p *PropertyTable) Clone() *PropertyTable {
	if p == nil {
		return nil
	}

	out := &PropertyTable{
		Name:       p.Name,
		Class:      p.Class,
		Count:      p.Count,
		Properties: make([]*PropertyTableProperty, len(p.Properties)),
	}
	for i, property := range p.Properties {
		if property != nil {
			out.Properties[i] = property.Clone()
		}
	}

	return out
}

func (p *PropertyTable) Equal(other *PropertyTable) bool {
	if p == nil || other == nil {
		return p == other
	}

	if p.Name != other.Name || p.Class != other.Class || p.Count != other.Count || len(p.Properties) != len(other.Properties) {
		return false
	}

	for i := range p.Properties {
		if p.Properties[i] == nil || other.Properties[i] == nil {
			if p.Properties[i] != other.Properties[i] {
				return false
			}

			continue
		}

		if !p.Properties[i].Equal(*other.Properties[i]) {
			return false
		}
	}

	return true
}

func (p *PropertyTable) Validate() error {
	if p == nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilPropertyTable)
	}

	for i, property := range p.Properties {
		if property == nil {
			return fmt.Errorf("%w: property table property %d is nil", ErrInvalidMetadata, i)
		}

		if err := property.Validate(); err != nil {
			return err
		}
	}

	return nil
}
