package metadata

import "fmt"

type StructuralMetadata struct {
	Schema             StructuralMetadataSchema
	PropertyTables     []*PropertyTable
	PropertyAttributes []*PropertyAttribute
}

func (s *StructuralMetadata) AddPropertyTable(table *PropertyTable) (int, error) {
	if s == nil {
		return -1, fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilStructuralMetadata)
	}

	if table == nil {
		return -1, fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilPropertyTable)
	}

	clone := table.Clone()
	if err := clone.Validate(); err != nil {
		return -1, err
	}

	s.PropertyTables = append(s.PropertyTables, clone)
	return len(s.PropertyTables) - 1, nil
}

func (s *StructuralMetadata) PropertyTableCount() int {
	if s == nil {
		return 0
	}

	return len(s.PropertyTables)
}

func (s *StructuralMetadata) PropertyTable(index int) (*PropertyTable, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilStructuralMetadata)
	}

	if index < 0 || index >= len(s.PropertyTables) {
		return nil, fmt.Errorf("%w: %w: property table %d", ErrInvalidArgument, ErrIndexOutOfRange, index)
	}

	return s.PropertyTables[index].Clone(), nil
}

func (s *StructuralMetadata) RemovePropertyTable(index int) error {
	if s == nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilStructuralMetadata)
	}

	if index < 0 || index >= len(s.PropertyTables) {
		return fmt.Errorf("%w: %w: property table %d", ErrInvalidArgument, ErrIndexOutOfRange, index)
	}

	s.PropertyTables = append(s.PropertyTables[:index], s.PropertyTables[index+1:]...)
	return nil
}

func (s *StructuralMetadata) AddPropertyAttribute(attribute *PropertyAttribute) (int, error) {
	if s == nil {
		return -1, fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilStructuralMetadata)
	}

	if attribute == nil {
		return -1, fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilPropertyAttribute)
	}

	clone := attribute.Clone()
	if err := clone.Validate(); err != nil {
		return -1, err
	}

	s.PropertyAttributes = append(s.PropertyAttributes, clone)
	return len(s.PropertyAttributes) - 1, nil
}

func (s *StructuralMetadata) PropertyAttributeCount() int {
	if s == nil {
		return 0
	}

	return len(s.PropertyAttributes)
}

func (s *StructuralMetadata) PropertyAttribute(index int) (*PropertyAttribute, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilStructuralMetadata)
	}

	if index < 0 || index >= len(s.PropertyAttributes) {
		return nil, fmt.Errorf("%w: %w: property attribute %d", ErrInvalidArgument, ErrIndexOutOfRange, index)
	}

	return s.PropertyAttributes[index].Clone(), nil
}

func (s *StructuralMetadata) RemovePropertyAttribute(index int) error {
	if s == nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilStructuralMetadata)
	}

	if index < 0 || index >= len(s.PropertyAttributes) {
		return fmt.Errorf("%w: %w: property attribute %d", ErrInvalidArgument, ErrIndexOutOfRange, index)
	}

	s.PropertyAttributes = append(s.PropertyAttributes[:index], s.PropertyAttributes[index+1:]...)
	return nil
}

func (s *StructuralMetadata) Clone() *StructuralMetadata {
	if s == nil {
		return nil
	}

	out := &StructuralMetadata{
		Schema:             s.Schema.Clone(),
		PropertyTables:     make([]*PropertyTable, len(s.PropertyTables)),
		PropertyAttributes: make([]*PropertyAttribute, len(s.PropertyAttributes)),
	}
	for i, table := range s.PropertyTables {
		if table != nil {
			out.PropertyTables[i] = table.Clone()
		}
	}

	for i, attribute := range s.PropertyAttributes {
		if attribute != nil {
			out.PropertyAttributes[i] = attribute.Clone()
		}
	}

	return out
}

func (s *StructuralMetadata) Equal(other *StructuralMetadata) bool {
	if s == nil || other == nil {
		return s == other
	}

	if !s.Schema.Equal(other.Schema) ||
		len(s.PropertyTables) != len(other.PropertyTables) ||
		len(s.PropertyAttributes) != len(other.PropertyAttributes) {
		return false
	}

	for i := range s.PropertyTables {
		if s.PropertyTables[i] == nil || other.PropertyTables[i] == nil {
			if s.PropertyTables[i] != other.PropertyTables[i] {
				return false
			}

			continue
		}

		if !s.PropertyTables[i].Equal(other.PropertyTables[i]) {
			return false
		}
	}

	for i := range s.PropertyAttributes {
		if s.PropertyAttributes[i] == nil || other.PropertyAttributes[i] == nil {
			if s.PropertyAttributes[i] != other.PropertyAttributes[i] {
				return false
			}

			continue
		}

		if !s.PropertyAttributes[i].Equal(other.PropertyAttributes[i]) {
			return false
		}
	}

	return true
}

func (s *StructuralMetadata) Validate() error {
	if s == nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, ErrNilStructuralMetadata)
	}

	for i, table := range s.PropertyTables {
		if table == nil {
			return fmt.Errorf("%w: property table %d is nil", ErrInvalidMetadata, i)
		}

		if err := table.Validate(); err != nil {
			return err
		}
	}

	for i, attribute := range s.PropertyAttributes {
		if attribute == nil {
			return fmt.Errorf("%w: property attribute %d is nil", ErrInvalidMetadata, i)
		}

		if err := attribute.Validate(); err != nil {
			return err
		}
	}

	return nil
}
