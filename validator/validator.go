package validator

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// messageState tracks how far validation has got with a message type. a message reachable by
// more than one path is only walked once, and one that is currently being walked identifies a
// reference cycle
type messageState int

const (
	messageUnvisited messageState = iota
	messageInProgress
	messageValidated
)

// validation carries the state shared by every message checked for one request: the type index
// used to resolve references, and how far each type has been validated
type validation struct {
	typeToFile map[string]*descriptorpb.FileDescriptorProto
	state      map[string]messageState
}

// normalizeTypeName strips the leading dot that protoc puts on fully qualified type references.
// descriptor.proto documents FieldDescriptorProto.type_name as ".package.Message" when fully
// qualified, while the type index is keyed as "package.Message". normalizing on lookup keeps
// the two representations in sync
//
// typeName: the type name to normalize
//
// the type name without a leading dot
func normalizeTypeName(typeName string) string {
	return strings.TrimPrefix(typeName, ".")
}

// ValidateRequest validates the entire CodeGeneratorRequest. checks all messages in all files for unsupported types and circular dependencies
//
// req: the CodeGeneratorRequest from protoc
//
// err: unsupported types, circular dependencies, or nil if validation passes
func ValidateRequest(req *pluginpb.CodeGeneratorRequest) error {
	// build a map of all message types and their files for circular dependency detection
	typeToFile := make(map[string]*descriptorpb.FileDescriptorProto)
	for _, f := range req.ProtoFile {
		if err := indexMessages(f, typeToFile); err != nil {
			return err
		}
	}

	// one validation state for the whole request, so a type shared by several files is walked once
	v := &validation{
		typeToFile: typeToFile,
		state:      make(map[string]messageState, len(typeToFile)),
	}

	// validate all requested files
	for _, f := range req.ProtoFile {
		if f.Name == nil {
			return fmt.Errorf("proto file has no name")
		}
		isRequested := false
		for _, requested := range req.FileToGenerate {
			if requested == *f.Name {
				isRequested = true
				break
			}
		}
		if !isRequested {
			continue
		}

		// everything the generators and this validator assume -- that a singular field has no
		// presence unless proto3_optional says so, that there are no required fields, and that no
		// field carries a user-supplied default -- holds for proto3 only. a proto2 file leaves
		// Syntax unset, so anything that is not exactly "proto3" is refused rather than silently
		// mishandled
		if syntax := f.GetSyntax(); syntax != "proto3" {
			if syntax == "" {
				syntax = "proto2"
			}
			return fmt.Errorf("%s declares syntax %q: only proto3 is supported", *f.Name, syntax)
		}

		if err := v.validateFile(f); err != nil {
			return err
		}
	}

	return nil
}

// indexMessages recursively indexes all message types in a file. populates the typeToFile map with fully qualified message names (e.g., "package.MessageName")
//
// f: the FileDescriptorProto to index
// typeToFile: map to populate with message names
//
// err: invalid message names, or nil if indexing succeeds
func indexMessages(f *descriptorpb.FileDescriptorProto, typeToFile map[string]*descriptorpb.FileDescriptorProto) error {
	prefix := ""
	if f.Package != nil && *f.Package != "" {
		prefix = *f.Package + "."
	}

	for _, msg := range f.MessageType {
		// a descriptor protoc produced always carries a name, but the request is input like any
		// other: dereferencing a missing one would panic the plugin, and protoc would report only
		// "plugin failed with status code 2" rather than what was wrong with the descriptor
		if msg.Name == nil {
			return fmt.Errorf("%s: top-level message has no name", f.GetName())
		}
		fullName := prefix + *msg.Name
		typeToFile[fullName] = f
		if err := indexNestedMessages(msg, fullName, typeToFile); err != nil {
			return err
		}
	}

	return nil
}

// indexNestedMessages recursively indexes nested message types within a parent message. updates typeToFile with fully qualified names of nested messages
//
// msg: the message containing nested types
// prefix: the fully qualified name of the parent message
// typeToFile: map to populate with message names
//
// err: indexing errors, or nil on success
func indexNestedMessages(msg *descriptorpb.DescriptorProto, prefix string, typeToFile map[string]*descriptorpb.FileDescriptorProto) error {
	for _, nested := range msg.NestedType {
		if nested.Name == nil {
			return fmt.Errorf("message %s: nested message has no name", prefix)
		}
		fullName := prefix + "." + *nested.Name
		typeToFile[fullName] = typeToFile[prefix] // use parent's file
		if err := indexNestedMessages(nested, fullName, typeToFile); err != nil {
			return err
		}
	}
	return nil
}

// validateFile validates a single file for unsupported types and circular dependencies. every
// message the file defines is checked, nested ones included: a nested message gets code
// generated for it whether or not anything refers to it, so validating only the ones reached
// through a field would let an unsupported type through
//
// f: the FileDescriptorProto to validate
//
// err: validation errors, or nil if all messages are valid
func (v *validation) validateFile(f *descriptorpb.FileDescriptorProto) error {
	prefix := ""
	if f.Package != nil && *f.Package != "" {
		prefix = *f.Package + "."
	}

	var walk func(msg *descriptorpb.DescriptorProto, fullName string) error
	walk = func(msg *descriptorpb.DescriptorProto, fullName string) error {
		if err := v.validateMessage(msg, fullName); err != nil {
			return fmt.Errorf("in message %s: %w", fullName, err)
		}
		for _, nested := range msg.NestedType {
			if err := walk(nested, fullName+"."+nested.GetName()); err != nil {
				return err
			}
		}
		return nil
	}

	for _, msg := range f.MessageType {
		if err := walk(msg, prefix+msg.GetName()); err != nil {
			return err
		}
	}

	return nil
}

// validateMessage validates a single message for unsupported types and circular dependencies.
// the walk is a three-colour depth-first search: a type still being walked is part of a cycle,
// and a type already finished is skipped. without that second half, a message referenced from
// several places is re-walked once per path, which costs time exponential in the depth of the
// reference graph
//
// msg: the message to validate
// fullName: the fully qualified name of the message
//
// err: unsupported types, circular dependencies, or nil if valid
func (v *validation) validateMessage(msg *descriptorpb.DescriptorProto, fullName string) error {
	switch v.state[fullName] {
	case messageInProgress:
		return fmt.Errorf("circular dependency detected: %s", fullName)
	case messageValidated:
		return nil
	}

	v.state[fullName] = messageInProgress

	// validate all fields. the name is only used to describe where an error came from, so read it
	// through the accessor -- an unnamed field is a descriptor problem to report, not to panic on
	for _, field := range msg.Field {
		if err := v.validateField(field, msg); err != nil {
			return fmt.Errorf("in field %s: %w", field.GetName(), err)
		}
	}

	v.state[fullName] = messageValidated

	return nil
}

// validateField validates a single field for supported types. checks the field's type and, for message types, recursively validates the referenced message
//
// field: the field to validate
// msg: the message the field belongs to, for resolving its oneof
//
// err: unsupported types (enum, service, oneof, etc.) or nil if valid
func (v *validation) validateField(field *descriptorpb.FieldDescriptorProto, msg *descriptorpb.DescriptorProto) error {
	// a field in a real oneof would need a tagged union to be generated faithfully; emitting the
	// arms as independent, always-present fields instead loses both the mutual exclusion and any
	// way to tell which one was set, so reject it rather than generate something wrong. the
	// synthetic one-field oneof protoc wraps every proto3 `optional` field in is not one of these,
	// and is identified by proto3_optional
	if field.OneofIndex != nil && !field.GetProto3Optional() {
		name := "<unknown>"
		if index := int(field.GetOneofIndex()); index >= 0 && index < len(msg.OneofDecl) {
			name = msg.OneofDecl[index].GetName()
		}
		return fmt.Errorf("unsupported oneof: field belongs to oneof %s", name)
	}

	// check field type
	if field.Type == nil {
		return fmt.Errorf("field has no type")
	}

	switch *field.Type {
	case descriptorpb.FieldDescriptorProto_TYPE_BOOL,
		descriptorpb.FieldDescriptorProto_TYPE_INT32,
		descriptorpb.FieldDescriptorProto_TYPE_INT64,
		descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		descriptorpb.FieldDescriptorProto_TYPE_UINT64,
		descriptorpb.FieldDescriptorProto_TYPE_SINT32,
		descriptorpb.FieldDescriptorProto_TYPE_SINT64,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED32,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED64,
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED32,
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED64,
		descriptorpb.FieldDescriptorProto_TYPE_FLOAT,
		descriptorpb.FieldDescriptorProto_TYPE_DOUBLE,
		descriptorpb.FieldDescriptorProto_TYPE_STRING:
		return nil

	case descriptorpb.FieldDescriptorProto_TYPE_MESSAGE:
		// validate nested message recursively
		if field.TypeName == nil {
			return fmt.Errorf("message field has no type name")
		}
		typeName := normalizeTypeName(*field.TypeName)
		if err := v.validateMapKey(typeName); err != nil {
			return err
		}
		return v.validateMessageReference(typeName)

	case descriptorpb.FieldDescriptorProto_TYPE_ENUM,
		descriptorpb.FieldDescriptorProto_TYPE_GROUP,
		descriptorpb.FieldDescriptorProto_TYPE_BYTES:
		return fmt.Errorf("unsupported field type: %v", *field.Type)

	default:
		return fmt.Errorf("unknown field type: %v", *field.Type)
	}
}

// validateMapKey rejects map key types the generated code cannot carry across languages. proto
// allows a bool key, but Go's encoding/json refuses to marshal a map keyed by one at all, so a
// message using it could never round-trip. every other legal key type -- the integers and string
// -- is handled
//
// typeName: the fully qualified type name a field refers to, which for a map is its entry message
//
// err: an unsupported map key type, or nil
func (v *validation) validateMapKey(typeName string) error {
	file, exists := v.typeToFile[typeName]
	if !exists {
		// a missing type is reported by validateMessageReference, with a better message
		return nil
	}

	msg := findMessageInFile(file, typeName)
	if msg == nil || msg.Options == nil || !msg.Options.GetMapEntry() || len(msg.Field) == 0 {
		return nil
	}

	if msg.Field[0].GetType() == descriptorpb.FieldDescriptorProto_TYPE_BOOL {
		return fmt.Errorf("unsupported map key type: bool (Go cannot encode a map keyed by bool, so it would not round-trip)")
	}

	return nil
}

// validateMessageReference validates that a referenced message exists and is valid. recursively validates the referenced message for circular dependencies
//
// typeName: the fully qualified type name (e.g., "package.MessageName")
//
// err: message not found, circular dependencies, or nil if valid
func (v *validation) validateMessageReference(typeName string) error {
	file, exists := v.typeToFile[typeName]
	if !exists {
		return fmt.Errorf("referenced message type not found: %s", typeName)
	}

	// find the message definition in the descriptor
	msg := findMessageInFile(file, typeName)
	if msg == nil {
		return fmt.Errorf("could not locate message definition: %s", typeName)
	}

	return v.validateMessage(msg, typeName)
}

// findMessageInFile finds a message definition in a file by its fully qualified name. recursively searches nested messages if necessary
//
// f: the FileDescriptorProto to search
// fullName: fully qualified name (e.g., "package.MessageName" or "package.Parent.Nested")
//
// message descriptor or nil if not found
func findMessageInFile(f *descriptorpb.FileDescriptorProto, fullName string) *descriptorpb.DescriptorProto {
	prefix := ""
	if f.Package != nil && *f.Package != "" {
		prefix = *f.Package + "."
	}

	// extract the message name without package. the name must actually live in this file's
	// package, otherwise slicing by prefix length would silently mis-split (or panic when the
	// prefix is longer than the name)
	fullName = normalizeTypeName(fullName)
	if prefix != "" && !strings.HasPrefix(fullName, prefix) {
		return nil
	}
	messagePart := strings.TrimPrefix(fullName, prefix)

	// split by "." for nested messages
	parts := strings.Split(messagePart, ".")
	if len(parts) == 0 || parts[0] == "" {
		return nil
	}

	// find top-level message
	var current *descriptorpb.DescriptorProto
	for _, msg := range f.MessageType {
		// GetName rather than a dereference: this is a lookup with no error to report, and a
		// nameless descriptor should simply not match
		if msg.GetName() == parts[0] {
			current = msg
			break
		}
	}

	if current == nil {
		return nil
	}

	// find nested messages
	for i := 1; i < len(parts); i++ {
		found := false
		for _, nested := range current.NestedType {
			if nested.GetName() == parts[i] {
				current = nested
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}

	return current
}
