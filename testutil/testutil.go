package testutil

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// String returns a pointer to the given string. used for convenience in tests when creating protobuf descriptors
//
// s: the string value
//
// pointer to the string
func String(s string) *string {
	return proto.String(s)
}

// Int32 returns a pointer to the given int32. used for convenience in tests when creating protobuf descriptors
//
// i: the int32 value
//
// pointer to the int32
func Int32(i int32) *int32 {
	return proto.Int32(i)
}

// Bool returns a pointer to the given bool. used for convenience in tests when creating protobuf descriptors
//
// b: the bool value
//
// pointer to the bool
func Bool(b bool) *bool {
	return proto.Bool(b)
}

// NewStringField creates a new string field descriptor. returns a FieldDescriptorProto for a string field with the given name and number
//
// name: the field name
// number: the field number (1-indexed)
// repeated: whether the field is repeated (a singular optional field when false)
//
// FieldDescriptorProto for the string field
func NewStringField(name string, number int32, repeated bool) *descriptorpb.FieldDescriptorProto {
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	if repeated {
		label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	}

	return &descriptorpb.FieldDescriptorProto{
		Name:   proto.String(name),
		Number: proto.Int32(number),
		Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		Label:  label.Enum(),
	}
}

// NewIntField creates a new int32 field descriptor. returns a FieldDescriptorProto for an int32 field with the given name and number
//
// name: the field name
// number: the field number (1-indexed)
// repeated: whether the field is repeated (a singular optional field when false)
//
// FieldDescriptorProto for the int32 field
func NewIntField(name string, number int32, repeated bool) *descriptorpb.FieldDescriptorProto {
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	if repeated {
		label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	}

	return &descriptorpb.FieldDescriptorProto{
		Name:   proto.String(name),
		Number: proto.Int32(number),
		Type:   descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
		Label:  label.Enum(),
	}
}

// NewMessageField creates a new message field descriptor. returns a FieldDescriptorProto for a message field with the given name and type
//
// name: the field name
// number: the field number (1-indexed)
// typeName: fully qualified message type name (e.g., "package.Message")
//
// FieldDescriptorProto for the message field
func NewMessageField(name string, number int32, typeName string) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:     proto.String(name),
		Number:   proto.Int32(number),
		Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
		TypeName: proto.String(typeName),
		Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
	}
}

// NewBoolField creates a new bool field descriptor
//
// name: the field name
// number: the field number (1-indexed)
//
// FieldDescriptorProto for the bool field
func NewBoolField(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:   proto.String(name),
		Number: proto.Int32(number),
		Type:   descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
		Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
	}
}

// NewFloatField creates a new float field descriptor
//
// name: the field name
// number: the field number (1-indexed)
//
// FieldDescriptorProto for the float field
func NewFloatField(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:   proto.String(name),
		Number: proto.Int32(number),
		Type:   descriptorpb.FieldDescriptorProto_TYPE_FLOAT.Enum(),
		Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
	}
}

// Optional marks a field descriptor as a proto3 `optional` field and returns it. protoc reports
// an explicit `optional` through proto3_optional (and by putting the field in a synthetic
// one-field oneof), which is the signal the generators key off — the LABEL_OPTIONAL label alone
// says nothing, since every singular proto3 field carries it
//
// field: the field descriptor to mark
//
// the same field descriptor, marked optional
func Optional(field *descriptorpb.FieldDescriptorProto) *descriptorpb.FieldDescriptorProto {
	field.Proto3Optional = proto.Bool(true)
	field.Label = descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	return field
}

// NewBytesField creates a new bytes field descriptor. bytes is an unsupported type, so this is
// mainly useful for testing that the validator rejects it wherever it appears
//
// name: the field name
// number: the field number (1-indexed)
//
// FieldDescriptorProto for the bytes field
func NewBytesField(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:   proto.String(name),
		Number: proto.Int32(number),
		Type:   descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
		Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
	}
}

// InOneof puts a field in a real (non-synthetic) oneof and returns it. protoc reports oneof
// membership through oneof_index; a field carrying one without proto3_optional is an arm of a
// oneof the user actually declared, rather than the synthetic wrapper around an `optional` field
//
// field: the field descriptor to mark
// index: the index of the oneof in the message's oneof_decl
//
// the same field descriptor, marked as a oneof member
func InOneof(field *descriptorpb.FieldDescriptorProto, index int32) *descriptorpb.FieldDescriptorProto {
	field.OneofIndex = proto.Int32(index)
	return field
}

// NewMessage creates a new message descriptor. returns a DescriptorProto with the given name and fields
//
// name: the message name
// fields: the message fields
//
// DescriptorProto for the message
func NewMessage(name string, fields []*descriptorpb.FieldDescriptorProto) *descriptorpb.DescriptorProto {
	return &descriptorpb.DescriptorProto{
		Name:  proto.String(name),
		Field: fields,
	}
}

// WithNested attaches nested message types to a message and returns it
//
// msg: the parent message
// nested: the messages to nest inside it
//
// the same message descriptor, carrying the nested types
func WithNested(msg *descriptorpb.DescriptorProto, nested ...*descriptorpb.DescriptorProto) *descriptorpb.DescriptorProto {
	msg.NestedType = append(msg.NestedType, nested...)
	return msg
}

// WithOneofDecl declares oneofs on a message and returns it. a field placed in a oneof by
// InOneof refers to one of these by index
//
// msg: the message to declare the oneofs on
// names: the oneof names, in index order
//
// the same message descriptor, carrying the oneof declarations
func WithOneofDecl(msg *descriptorpb.DescriptorProto, names ...string) *descriptorpb.DescriptorProto {
	for _, name := range names {
		msg.OneofDecl = append(msg.OneofDecl, &descriptorpb.OneofDescriptorProto{Name: proto.String(name)})
	}
	return msg
}

// NewFile creates a new file descriptor. returns a FileDescriptorProto with the given name, package, and messages
//
// name: the file name
// pkg: the package name
// messages: the messages in the file
//
// FileDescriptorProto for the file
func NewFile(name string, pkg string, messages []*descriptorpb.DescriptorProto) *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:        proto.String(name),
		Package:     proto.String(pkg),
		MessageType: messages,
		// the validator only accepts proto3, which is what protoc reports for a proto3 source file
		Syntax: proto.String("proto3"),
	}
}
