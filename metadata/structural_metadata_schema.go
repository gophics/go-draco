package metadata

type StructuralMetadataSchemaObjectType int

const (
	StructuralMetadataSchemaObjectTypeObject StructuralMetadataSchemaObjectType = iota
	StructuralMetadataSchemaObjectTypeArray
	StructuralMetadataSchemaObjectTypeString
	StructuralMetadataSchemaObjectTypeInteger
	StructuralMetadataSchemaObjectTypeBoolean
)

type StructuralMetadataSchemaObject struct {
	Name    string
	Type    StructuralMetadataSchemaObjectType
	Objects []StructuralMetadataSchemaObject
	Array   []StructuralMetadataSchemaObject
	String  string
	Integer int
	Boolean bool
}

func NewStructuralMetadataSchemaObject(name string) StructuralMetadataSchemaObject {
	return StructuralMetadataSchemaObject{Name: name, Type: StructuralMetadataSchemaObjectTypeObject}
}

func NewStructuralMetadataSchemaString(name, value string) StructuralMetadataSchemaObject {
	obj := NewStructuralMetadataSchemaObject(name)
	obj.setString(value)
	return obj
}

func NewStructuralMetadataSchemaInteger(name string, value int) StructuralMetadataSchemaObject {
	obj := NewStructuralMetadataSchemaObject(name)
	obj.setInteger(value)
	return obj
}

func NewStructuralMetadataSchemaBoolean(name string, value bool) StructuralMetadataSchemaObject {
	obj := NewStructuralMetadataSchemaObject(name)
	obj.setBoolean(value)
	return obj
}

func NewStructuralMetadataSchemaObjects(name string, objects []StructuralMetadataSchemaObject) StructuralMetadataSchemaObject {
	obj := NewStructuralMetadataSchemaObject(name)
	obj.setObjects(objects)
	return obj
}

func NewStructuralMetadataSchemaArray(name string, values []StructuralMetadataSchemaObject) StructuralMetadataSchemaObject {
	obj := NewStructuralMetadataSchemaObject(name)
	obj.setArray(values)
	return obj
}

func (o *StructuralMetadataSchemaObject) setObjects(objects []StructuralMetadataSchemaObject) {
	o.Type = StructuralMetadataSchemaObjectTypeObject
	o.Objects = cloneStructuralMetadataSchemaObjects(objects)
}

func (o *StructuralMetadataSchemaObject) setArray(array []StructuralMetadataSchemaObject) {
	o.Type = StructuralMetadataSchemaObjectTypeArray
	o.Array = cloneStructuralMetadataSchemaObjects(array)
}

func (o *StructuralMetadataSchemaObject) setString(value string) {
	o.Type = StructuralMetadataSchemaObjectTypeString
	o.String = value
}

func (o *StructuralMetadataSchemaObject) setInteger(value int) {
	o.Type = StructuralMetadataSchemaObjectTypeInteger
	o.Integer = value
}

func (o *StructuralMetadataSchemaObject) setBoolean(value bool) {
	o.Type = StructuralMetadataSchemaObjectTypeBoolean
	o.Boolean = value
}

func (o StructuralMetadataSchemaObject) ObjectByName(name string) *StructuralMetadataSchemaObject {
	for i := range o.Objects {
		if o.Objects[i].Name == name {
			return &o.Objects[i]
		}
	}

	return nil
}

func (o StructuralMetadataSchemaObject) Clone() StructuralMetadataSchemaObject {
	return StructuralMetadataSchemaObject{
		Name:    o.Name,
		Type:    o.Type,
		Objects: cloneStructuralMetadataSchemaObjects(o.Objects),
		Array:   cloneStructuralMetadataSchemaObjects(o.Array),
		String:  o.String,
		Integer: o.Integer,
		Boolean: o.Boolean,
	}
}

func (o StructuralMetadataSchemaObject) Equal(other StructuralMetadataSchemaObject) bool {
	if o.Name != other.Name || o.Type != other.Type {
		return false
	}

	switch o.Type {
	case StructuralMetadataSchemaObjectTypeObject:
		return equalStructuralMetadataSchemaObjects(o.Objects, other.Objects)
	case StructuralMetadataSchemaObjectTypeArray:
		return equalStructuralMetadataSchemaObjects(o.Array, other.Array)
	case StructuralMetadataSchemaObjectTypeString:
		return o.String == other.String
	case StructuralMetadataSchemaObjectTypeInteger:
		return o.Integer == other.Integer
	case StructuralMetadataSchemaObjectTypeBoolean:
		return o.Boolean == other.Boolean
	default:
		return false
	}
}

type StructuralMetadataSchema struct {
	JSON StructuralMetadataSchemaObject
}

func NewStructuralMetadataSchema() StructuralMetadataSchema {
	return StructuralMetadataSchema{JSON: NewStructuralMetadataSchemaObject("schema")}
}

func (s StructuralMetadataSchema) Empty() bool {
	return len(s.JSON.Objects) == 0
}

func (s StructuralMetadataSchema) Clone() StructuralMetadataSchema {
	return StructuralMetadataSchema{JSON: s.JSON.Clone()}
}

func (s StructuralMetadataSchema) Equal(other StructuralMetadataSchema) bool {
	return s.JSON.Equal(other.JSON)
}

func cloneStructuralMetadataSchemaObjects(in []StructuralMetadataSchemaObject) []StructuralMetadataSchemaObject {
	if in == nil {
		return nil
	}

	out := make([]StructuralMetadataSchemaObject, len(in))
	for i := range in {
		out[i] = in[i].Clone()
	}

	return out
}

func equalStructuralMetadataSchemaObjects(a, b []StructuralMetadataSchemaObject) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}

	return true
}
