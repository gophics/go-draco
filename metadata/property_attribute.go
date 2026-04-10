package metadata

import "fmt"

type PropertyAttributeProperty struct {
	Name          string
	AttributeName string
}

func (p PropertyAttributeProperty) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("%w: property attribute property name is empty", ErrInvalidMetadata)
	}

	if p.AttributeName == "" {
		return fmt.Errorf("%w: property attribute property %q attribute name is empty", ErrInvalidMetadata, p.Name)
	}

	return nil
}

func (p PropertyAttributeProperty) Equal(other PropertyAttributeProperty) bool {
	return p.Name == other.Name && p.AttributeName == other.AttributeName
}

func (p PropertyAttributeProperty) Clone() *PropertyAttributeProperty {
	out := p
	return &out
}

type PropertyAttribute struct {
	Name       string
	Class      string
	Properties []*PropertyAttributeProperty
}

func (p *PropertyAttribute) AddProperty(property *PropertyAttributeProperty) (int, error) {
	if p == nil {
		return -1, fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilPropertyAttribute)
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

func (p *PropertyAttribute) PropertyCount() int {
	if p == nil {
		return 0
	}

	return len(p.Properties)
}

func (p *PropertyAttribute) Property(index int) (*PropertyAttributeProperty, error) {
	if p == nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilPropertyAttribute)
	}

	if index < 0 || index >= len(p.Properties) {
		return nil, fmt.Errorf("%w: %w: property attribute property %d", ErrInvalidArgument, ErrIndexOutOfRange, index)
	}

	return p.Properties[index].Clone(), nil
}

func (p *PropertyAttribute) RemoveProperty(index int) error {
	if p == nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilPropertyAttribute)
	}

	if index < 0 || index >= len(p.Properties) {
		return fmt.Errorf("%w: %w: property attribute property %d", ErrInvalidArgument, ErrIndexOutOfRange, index)
	}

	p.Properties = append(p.Properties[:index], p.Properties[index+1:]...)
	return nil
}

func (p *PropertyAttribute) Clone() *PropertyAttribute {
	if p == nil {
		return nil
	}

	out := &PropertyAttribute{
		Name:       p.Name,
		Class:      p.Class,
		Properties: make([]*PropertyAttributeProperty, len(p.Properties)),
	}
	for i, property := range p.Properties {
		if property != nil {
			out.Properties[i] = property.Clone()
		}
	}

	return out
}

func (p *PropertyAttribute) Equal(other *PropertyAttribute) bool {
	if p == nil || other == nil {
		return p == other
	}

	if p.Name != other.Name || p.Class != other.Class || len(p.Properties) != len(other.Properties) {
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

func (p *PropertyAttribute) Validate() error {
	if p == nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilPropertyAttribute)
	}

	for i, property := range p.Properties {
		if property == nil {
			return fmt.Errorf("%w: property attribute property %d is nil", ErrInvalidMetadata, i)
		}

		if err := property.Validate(); err != nil {
			return err
		}
	}

	return nil
}
