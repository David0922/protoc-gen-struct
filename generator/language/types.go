package language

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"google.golang.org/protobuf/types/descriptorpb"
)

// Generator is the interface that all language generators must implement
// each generator is responsible for generating code for a specific target language
type Generator interface {
	// Generate generates code for a single proto file
	// it takes the file descriptor and returns the generated code as a string
	// returns an error if code generation fails
	Generate(file *descriptorpb.FileDescriptorProto) (string, error)
}

// MessageRef describes a single message type: the descriptor itself, where it is defined, and the
// name the generated code gives it. nested messages are flattened into file scope, so the
// generated name joins the path from the top-level parent with underscores ("Outer_Inner") --
// using only the unqualified name would make two nested messages with the same name collide
type MessageRef struct {
	Descriptor *descriptorpb.DescriptorProto
	FullName   string                            // fully qualified proto name, e.g. "example.Outer.Inner"
	TypeName   string                            // generated type name, e.g. "Outer_Inner"
	File       *descriptorpb.FileDescriptorProto // the proto file that defines the message
}

// TypeRegistry maintains a map of all message types for reference resolution
// it is used to look up message types by their fully qualified name
type TypeRegistry struct {
	messages map[string]*MessageRef
	files    map[string]*descriptorpb.FileDescriptorProto

	// the proto files protoc was asked to generate. a type defined outside this set has no
	// generated output to import, so referring to it cannot be resolved
	generated map[string]bool
}

// NewTypeRegistry creates a new TypeRegistry from a list of proto files. indexes all messages in all files for quick lookup
//
// files: the proto files to index, which includes every transitive import
// filesToGenerate: the names of the files protoc was asked to generate code for
//
// populated TypeRegistry for message type resolution
func NewTypeRegistry(files []*descriptorpb.FileDescriptorProto, filesToGenerate []string) *TypeRegistry {
	registry := &TypeRegistry{
		messages:  make(map[string]*MessageRef),
		files:     make(map[string]*descriptorpb.FileDescriptorProto),
		generated: make(map[string]bool, len(filesToGenerate)),
	}

	for _, name := range filesToGenerate {
		registry.generated[name] = true
	}

	for _, f := range files {
		registry.indexFile(f)
	}

	return registry
}

// IsGenerated reports whether a proto file is one protoc asked to generate code for, and so
// whether an output file exists that its types can be imported from
//
// name: the proto file name
//
// true if the file has generated output
func (tr *TypeRegistry) IsGenerated(name string) bool {
	return tr.generated[name]
}

// indexFile indexes all messages in a proto file. populates the messages map with fully qualified type names
//
// f: the proto file to index
func (tr *TypeRegistry) indexFile(f *descriptorpb.FileDescriptorProto) {
	if f.Name != nil {
		tr.files[*f.Name] = f
	}

	// share the naming pass with CollectMessages so that a type is named identically whether it is
	// reached as a local declaration or as a reference from another file
	for _, ref := range assignTypeNames(f) {
		tr.messages[ref.FullName] = ref
	}
}

// assignTypeNames computes the generated type name of every message in a file, map entries
// included. nested messages are flattened into file scope by joining the path from the top-level
// parent with underscores, which can land on a name a message already holds -- a nested
// "User.Profile" and a top-level "User_Profile" both want "User_Profile" -- so names are handed
// out shallowest-first and a taken one gets a numeric suffix. a top-level message therefore keeps
// its own name unless escaping would collapse it onto another one, and the assignment is a pure
// function of the file, so the defining and the referring generator always agree
//
// f: the proto file to name the messages of
//
// every message in the file, in declaration order
func assignTypeNames(f *descriptorpb.FileDescriptorProto) []*MessageRef {
	prefix := ""
	if f.GetPackage() != "" {
		prefix = f.GetPackage() + "."
	}

	type candidate struct {
		ref       *MessageRef
		preferred string
		depth     int
	}

	var candidates []*candidate
	var walk func(msg *descriptorpb.DescriptorProto, fullName, preferred string, depth int)
	walk = func(msg *descriptorpb.DescriptorProto, fullName, preferred string, depth int) {
		candidates = append(candidates, &candidate{
			ref:       &MessageRef{Descriptor: msg, FullName: fullName, File: f},
			preferred: preferred,
			depth:     depth,
		})
		for _, nested := range msg.NestedType {
			walk(nested, fullName+"."+nested.GetName(), preferred+"_"+nested.GetName(), depth+1)
		}
	}

	for _, msg := range f.MessageType {
		walk(msg, prefix+msg.GetName(), msg.GetName(), 0)
	}

	// hand out names shallowest-first, keeping declaration order within a depth so the result is
	// stable. proto already forbids two messages at the same level sharing a name, so only a
	// flattened nested name can ever find its preferred name taken
	order := make([]*candidate, len(candidates))
	copy(order, candidates)
	slices.SortStableFunc(order, func(a, b *candidate) int { return a.depth - b.depth })

	// a generator escapes a type name after this pass and never de-duplicates it again, and
	// escaping can map two distinct names together -- a file declaring both "class" and "class_"
	// would emit "struct class_" twice in C++. so a name is only free if it stays distinct in
	// every target language once escaped, which is checked here rather than per generator: the
	// name has to be a pure function of the file for the defining and the referring generator to
	// agree on it
	taken := make(map[string]bool, len(order))
	escaped := make(map[string]map[string]bool, len(reservedWords))
	for language := range reservedWords {
		escaped[language] = make(map[string]bool, len(order))
	}

	free := func(name string) bool {
		if taken[name] {
			return false
		}
		for language, seen := range escaped {
			if seen[EscapeIdentifier(name, language)] {
				return false
			}
		}
		return true
	}

	for _, c := range order {
		name := c.preferred
		for i := 2; !free(name); i++ {
			name = fmt.Sprintf("%s%d", c.preferred, i)
		}
		taken[name] = true
		for language, seen := range escaped {
			seen[EscapeIdentifier(name, language)] = true
		}
		c.ref.TypeName = name
	}

	refs := make([]*MessageRef, len(candidates))
	for i, c := range candidates {
		refs[i] = c.ref
	}
	return refs
}

// Resolve looks up a message reference by its fully qualified name
//
// typeName: fully qualified type name to look up, with or without protoc's leading dot
//
// ref: the message reference
// err: not found error or nil
func (tr *TypeRegistry) Resolve(typeName string) (*MessageRef, error) {
	// protoc emits fully qualified references with a leading dot (".package.Message") while the
	// index is keyed without one, so normalize before looking up
	ref, exists := tr.messages[strings.TrimPrefix(typeName, ".")]
	if !exists {
		return nil, fmt.Errorf("message type not found: %s", typeName)
	}
	return ref, nil
}

// LookupMessage looks up a message type by its fully qualified name
//
// typeName: fully qualified type name to look up
//
// msg: the message descriptor
// err: not found error or nil
func (tr *TypeRegistry) LookupMessage(typeName string) (*descriptorpb.DescriptorProto, error) {
	ref, err := tr.Resolve(typeName)
	if err != nil {
		return nil, err
	}
	return ref.Descriptor, nil
}

// FieldInfo contains information about a proto field for code generation
// it includes the field name, type, and metadata needed for serialization
type FieldInfo struct {
	Name         string // original proto field name (snake_case)
	ProtoName    string // proto field name for serialization
	Type         FieldType
	IsRepeated   bool
	IsOptional   bool // declared `optional` in proto3, so the field tracks presence
	IsMap        bool
	MapKeyType   FieldType
	MapValueType FieldType
	MessageType  string // for nested messages, the fully qualified type name
	DefaultValue string // default value for the type
}

// FieldType represents a simple field type
type FieldType int

const (
	FieldTypeBool FieldType = iota
	FieldTypeInt32
	FieldTypeInt64
	FieldTypeUint32
	FieldTypeUint64
	FieldTypeSint32
	FieldTypeSint64
	FieldTypeFixed32
	FieldTypeFixed64
	FieldTypeSfixed32
	FieldTypeSfixed64
	FieldTypeFloat
	FieldTypeDouble
	FieldTypeString
	FieldTypeMessage
	FieldTypeBytes
)

// ExtractFieldInfo extracts information about a proto field for code generation. converts the proto field descriptor into a FieldInfo structure. for map types, MapKeyType and MapValueType are populated instead of Type
//
// field: the proto field descriptor to extract from
// registry: TypeRegistry for type resolution
//
// info: FieldInfo for code generation
// err: invalid field type or nil
func ExtractFieldInfo(field *descriptorpb.FieldDescriptorProto, registry *TypeRegistry) (*FieldInfo, error) {
	// the name is what every generator spells the member with, so a descriptor missing one is
	// reported rather than dereferenced
	if field.Name == nil {
		return nil, fmt.Errorf("field has no name")
	}

	info := &FieldInfo{
		Name:      *field.Name,
		ProtoName: *field.Name,
	}

	// check if it's a map (repeated message with specific internal structure)
	isMap := false
	if field.Label != nil && *field.Label == descriptorpb.FieldDescriptorProto_LABEL_REPEATED {
		if field.Type != nil && *field.Type == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
			// check if this is a map entry
			if field.TypeName != nil {
				msg, err := registry.LookupMessage(*field.TypeName)
				if err == nil && IsMapEntry(msg) {
					isMap = true
					info.IsMap = true

					// get key and value types. a map entry always carries exactly the key and
					// value fields; anything else means a malformed descriptor, and falling
					// through would leave MapKeyType/MapValueType at their zero value
					// (FieldTypeBool) and silently emit a map[bool]bool
					if len(msg.Field) < 2 {
						return nil, fmt.Errorf("malformed map entry %s: expected 2 fields, got %d", *field.TypeName, len(msg.Field))
					}

					keyField := msg.Field[0]
					valueField := msg.Field[1]

					keyType, err := protoTypeToFieldType(keyField)
					if err != nil {
						return nil, fmt.Errorf("invalid map key type: %w", err)
					}
					info.MapKeyType = keyType

					valueType, err := fieldToFieldType(valueField)
					if err != nil {
						return nil, fmt.Errorf("invalid map value type: %w", err)
					}
					info.MapValueType = valueType

					if valueField.Type != nil && *valueField.Type == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
						if valueField.TypeName == nil {
							return nil, fmt.Errorf("map value of message type has no type name in field %s", *field.Name)
						}
						info.MessageType = *valueField.TypeName
					}
				}
			}
		}
	}

	if !isMap {
		// regular field (possibly repeated)
		if field.Label != nil && *field.Label == descriptorpb.FieldDescriptorProto_LABEL_REPEATED {
			info.IsRepeated = true
		}

		// an explicit `optional` in proto3 is reported through proto3_optional (protoc also wraps
		// the field in a synthetic one-field oneof). the LABEL_OPTIONAL label cannot be used for
		// this: every singular proto3 field carries it, optional keyword or not. repeated fields
		// and maps already model absence as "empty", and proto3 forbids `optional` on them
		info.IsOptional = field.GetProto3Optional()

		fieldType, err := fieldToFieldType(field)
		if err != nil {
			return nil, err
		}
		info.Type = fieldType

		if field.Type != nil && *field.Type == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
			if field.TypeName == nil {
				return nil, fmt.Errorf("message field %s has no type name", *field.Name)
			}
			info.MessageType = *field.TypeName
		}
	}

	return info, nil
}

// fieldToFieldType converts a proto field to a FieldType. extracts the base type from the field descriptor
//
// field: the proto field descriptor
//
// ft: the FieldType
// err: unsupported type or nil
func fieldToFieldType(field *descriptorpb.FieldDescriptorProto) (FieldType, error) {
	if field.Type == nil {
		return 0, fmt.Errorf("field has no type")
	}
	return protoTypeToFieldType(field)
}

// protoTypeToFieldType converts a protobuf type enum to a FieldType
//
// field: the proto field descriptor
//
// ft: the FieldType
// err: unsupported type or nil
func protoTypeToFieldType(field *descriptorpb.FieldDescriptorProto) (FieldType, error) {
	switch *field.Type {
	case descriptorpb.FieldDescriptorProto_TYPE_BOOL:
		return FieldTypeBool, nil
	case descriptorpb.FieldDescriptorProto_TYPE_INT32:
		return FieldTypeInt32, nil
	case descriptorpb.FieldDescriptorProto_TYPE_INT64:
		return FieldTypeInt64, nil
	case descriptorpb.FieldDescriptorProto_TYPE_UINT32:
		return FieldTypeUint32, nil
	case descriptorpb.FieldDescriptorProto_TYPE_UINT64:
		return FieldTypeUint64, nil
	case descriptorpb.FieldDescriptorProto_TYPE_SINT32:
		return FieldTypeSint32, nil
	case descriptorpb.FieldDescriptorProto_TYPE_SINT64:
		return FieldTypeSint64, nil
	case descriptorpb.FieldDescriptorProto_TYPE_FIXED32:
		return FieldTypeFixed32, nil
	case descriptorpb.FieldDescriptorProto_TYPE_FIXED64:
		return FieldTypeFixed64, nil
	case descriptorpb.FieldDescriptorProto_TYPE_SFIXED32:
		return FieldTypeSfixed32, nil
	case descriptorpb.FieldDescriptorProto_TYPE_SFIXED64:
		return FieldTypeSfixed64, nil
	case descriptorpb.FieldDescriptorProto_TYPE_FLOAT:
		return FieldTypeFloat, nil
	case descriptorpb.FieldDescriptorProto_TYPE_DOUBLE:
		return FieldTypeDouble, nil
	case descriptorpb.FieldDescriptorProto_TYPE_STRING:
		return FieldTypeString, nil
	case descriptorpb.FieldDescriptorProto_TYPE_MESSAGE:
		return FieldTypeMessage, nil
	case descriptorpb.FieldDescriptorProto_TYPE_BYTES:
		return FieldTypeBytes, nil
	default:
		return 0, fmt.Errorf("unsupported field type: %v", field.Type)
	}
}

// SnakeToCamelCase converts snake_case to camelCase. capitalizes the first letter of each word separated by underscores, then removes underscores. the first word is lowercase. example: "first_name" -> "firstName"
//
// s: the snake_case string to convert
//
// camelCase string
func SnakeToCamelCase(s string) string {
	parts := strings.Split(s, "_")
	if len(parts) == 0 {
		return s
	}

	// first part stays lowercase
	result := parts[0]

	// capitalize subsequent parts
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		result += strings.ToUpper(string(parts[i][0])) + parts[i][1:]
	}

	return result
}

// SnakeToTitleCase converts snake_case to TitleCase. each word (separated by underscores) is capitalized and underscores are removed. example: "first_name" -> "FirstName"
//
// the rest of each word is left as-is: proto allows field names that are not purely lowercase
// (e.g. "userID"), and lowercasing the remainder would mangle them into "Userid"
//
// s: the snake_case string to convert
//
// TitleCase string
func SnakeToTitleCase(s string) string {
	parts := strings.Split(s, "_")
	result := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		result += strings.ToUpper(string(part[0])) + part[1:]
	}
	return result
}

// GetMessageFields extracts field information from a message descriptor. returns a slice of FieldInfo for all fields in the message
//
// msg: the message descriptor to extract fields from
// registry: TypeRegistry for type resolution
//
// fields: slice of FieldInfo
// err: invalid field or nil
func GetMessageFields(msg *descriptorpb.DescriptorProto, registry *TypeRegistry) ([]*FieldInfo, error) {
	// skip map entry messages entirely (internal representation, never emitted as a struct).
	// this is a property of the message, not of any individual field
	if IsMapEntry(msg) {
		return nil, nil
	}

	var fields []*FieldInfo
	seen := make(map[string]bool)

	for _, protoField := range msg.Field {
		fieldInfo, err := ExtractFieldInfo(protoField, registry)
		if err != nil {
			return nil, err
		}

		// avoid duplicate fields (shouldn't happen in valid protos)
		if seen[fieldInfo.Name] {
			continue
		}
		seen[fieldInfo.Name] = true

		fields = append(fields, fieldInfo)
	}

	return fields, nil
}

// IsMapEntry reports whether a message is a synthetic map entry generated by protoc for a
// map<k, v> field. these messages back the map representation and must never be emitted as
// standalone structs
//
// msg: the message descriptor to check
//
// true if the message is a synthetic map entry
func IsMapEntry(msg *descriptorpb.DescriptorProto) bool {
	return msg.Options != nil && msg.Options.MapEntry != nil && *msg.Options.MapEntry
}

// CollectMessages returns every message in a file that should have code generated for it,
// flattening nested messages into file scope. protoc only lists top-level messages in
// FileDescriptorProto.MessageType, so without this walk a field referring to a nested type would
// name a type that is never defined. synthetic map entries are skipped
//
// the result is topologically sorted: a message is emitted only after every message it holds by
// value. C++ needs this, since a struct member of an incomplete type does not compile. messages
// that do not depend on one another keep their declaration order
//
// file: the proto file to collect messages from
//
// flattened, dependency-ordered slice of message references to generate
func CollectMessages(file *descriptorpb.FileDescriptorProto) []*MessageRef {
	// index every message in the file, map entries included: a map field points at its synthetic
	// entry message, and resolving the entry is the only way to reach the map's value type
	byName := make(map[string]*MessageRef)
	var declared []*MessageRef

	for _, ref := range assignTypeNames(file) {
		byName[ref.FullName] = ref
		if !IsMapEntry(ref.Descriptor) {
			declared = append(declared, ref)
		}
	}

	return sortMessagesByDependency(declared, byName)
}

// sortMessagesByDependency orders messages so that each one follows every message it holds by
// value. a proto containing a reference cycle cannot be ordered at all, but the validator
// rejects those before code generation ever runs, so the cycle guard here only keeps the walk
// from recursing forever on a descriptor that slipped through
//
// declared: the messages to emit, in declaration order
// byName: every message in the file keyed by fully qualified name, map entries included
//
// the messages in dependency order
func sortMessagesByDependency(declared []*MessageRef, byName map[string]*MessageRef) []*MessageRef {
	const (
		unvisited = iota
		inProgress
		done
	)

	state := make(map[string]int, len(byName))
	result := make([]*MessageRef, 0, len(declared))

	var visit func(ref *MessageRef)
	visit = func(ref *MessageRef) {
		if state[ref.FullName] != unvisited {
			return
		}
		state[ref.FullName] = inProgress

		for _, dep := range localDependencies(ref.Descriptor, byName) {
			visit(dep)
		}

		state[ref.FullName] = done
		result = append(result, ref)
	}

	for _, ref := range declared {
		visit(ref)
	}

	return result
}

// localDependencies returns the messages, defined in the same file, that a message holds by
// value. a message reached through a map field is reported as the map's value type rather than
// the synthetic entry message, which is never emitted as a struct. references to other files are
// left out: they are satisfied by an import, not by ordering within this file
//
// msg: the message whose fields to inspect
// byName: every message in the file keyed by fully qualified name, map entries included
//
// the referenced messages that are defined in this file
func localDependencies(msg *descriptorpb.DescriptorProto, byName map[string]*MessageRef) []*MessageRef {
	var deps []*MessageRef

	for _, field := range msg.Field {
		if field.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE || field.TypeName == nil {
			continue
		}

		ref, ok := byName[strings.TrimPrefix(field.GetTypeName(), ".")]
		if !ok {
			continue
		}

		if !IsMapEntry(ref.Descriptor) {
			deps = append(deps, ref)
			continue
		}

		// a map entry always carries exactly the key and value fields; a malformed one is
		// reported by ExtractFieldInfo, so here it simply contributes no ordering constraint
		if len(ref.Descriptor.Field) < 2 {
			continue
		}
		value := ref.Descriptor.Field[1]
		if value.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE || value.TypeName == nil {
			continue
		}
		if valueRef, ok := byName[strings.TrimPrefix(value.GetTypeName(), ".")]; ok && !IsMapEntry(valueRef.Descriptor) {
			deps = append(deps, valueRef)
		}
	}

	return deps
}

// SanitizePackageName converts a proto package into a single identifier usable as a package or
// namespace name. proto packages are dotted ("example.v1"), which is not a legal Go package
// name or C++ namespace name, so the last segment is used and any remaining non-identifier
// characters are replaced with underscores
//
// pkg: the proto package name
//
// an identifier-safe package name, or "" if pkg is empty
func SanitizePackageName(pkg string) string {
	if pkg == "" {
		return ""
	}

	parts := strings.Split(pkg, ".")
	last := parts[len(parts)-1]

	var b strings.Builder
	for i, c := range last {
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isDigit := c >= '0' && c <= '9'
		switch {
		case isLetter || c == '_':
			b.WriteRune(c)
		case isDigit && i > 0:
			// an identifier may not start with a digit
			b.WriteRune(c)
		default:
			b.WriteRune('_')
		}
	}

	return b.String()
}

// GetPackageName extracts the package name from a proto file. returns the package name or an empty string if not specified
//
// file: the proto file descriptor
//
// package name or empty string
func GetPackageName(file *descriptorpb.FileDescriptorProto) string {
	if file.Package != nil && *file.Package != "" {
		return *file.Package
	}
	return ""
}

// GetUnqualifiedTypeName extracts the last component of a fully qualified type name. for "package.Outer.Inner", returns "Inner". for "Simple", returns "Simple"
//
// fullName: the fully qualified type name
//
// the unqualified type name
func GetUnqualifiedTypeName(fullName string) string {
	parts := strings.Split(fullName, ".")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return fullName
}

// reservedWords lists, per target language, the identifiers a generated name must not collide
// with: the language's own keywords, plus the names the generated code itself binds. a proto
// field called "from" or "class" is perfectly legal, so without escaping the output is not even
// syntactically valid
var reservedWords = map[string]map[string]bool{
	// Go keywords, plus the methods every generated struct carries. field names are title-cased
	// before they get here, so only an already-capitalized proto name can reach these.
	// "json" and "yaml" are the package names the generated file imports: a type declared under
	// either name would be redeclared in the same block as the import
	"go": setOf(
		"break", "case", "chan", "const", "continue", "default", "defer", "else", "fallthrough",
		"for", "func", "go", "goto", "if", "import", "interface", "map", "package", "range",
		"return", "select", "struct", "switch", "type", "var",
		"json", "yaml",
		"ToJSON", "FromJSON", "ToYAML", "FromYAML",
	),
	// C++ keywords (through C++20), plus the generated member functions
	"c++": setOf(
		"alignas", "alignof", "and", "and_eq", "asm", "auto", "bitand", "bitor", "bool", "break",
		"case", "catch", "char", "char8_t", "char16_t", "char32_t", "class", "compl", "concept",
		"const", "consteval", "constexpr", "constinit", "const_cast", "continue", "co_await",
		"co_return", "co_yield", "decltype", "default", "delete", "do", "double", "dynamic_cast",
		"else", "enum", "explicit", "export", "extern", "false", "float", "for", "friend", "goto",
		"if", "inline", "int", "long", "mutable", "namespace", "new", "noexcept", "not", "not_eq",
		"nullptr", "operator", "or", "or_eq", "private", "protected", "public", "register",
		"reinterpret_cast", "requires", "return", "short", "signed", "sizeof", "static",
		"static_assert", "static_cast", "struct", "switch", "template", "this", "thread_local",
		"throw", "true", "try", "typedef", "typeid", "typename", "union", "unsigned", "using",
		"virtual", "void", "volatile", "wchar_t", "while", "xor", "xor_eq",
		"ToJSON", "FromJSON", "ToYAML", "FromYAML",
	),
	// Python keywords, the generated methods, and the names the class body itself evaluates --
	// a field called "field" would shadow dataclasses.field in a later default_factory expression
	"python": setOf(
		"False", "None", "True", "and", "as", "assert", "async", "await", "break", "class",
		"continue", "def", "del", "elif", "else", "except", "finally", "for", "from", "global",
		"if", "import", "in", "is", "lambda", "nonlocal", "not", "or", "pass", "raise", "return",
		"try", "while", "with", "yield",
		"field", "json", "yaml", "dataclass", "annotations",
		"to_dict", "from_dict", "to_json", "from_json", "to_yaml", "from_yaml",
	),
	// TypeScript reserved words. object properties may be keywords, but an interface or namespace
	// name may not, and neither may shadow a built-in type name or "yaml", the module every
	// generated file imports its serializer from
	"typescript": setOf(
		"yaml",
		"break", "case", "catch", "class", "const", "continue", "debugger", "default", "delete",
		"do", "else", "enum", "export", "extends", "false", "finally", "for", "function", "if",
		"import", "in", "instanceof", "new", "null", "return", "super", "switch", "this", "throw",
		"true", "try", "typeof", "var", "void", "while", "with",
		"implements", "interface", "let", "package", "private", "protected", "public", "static",
		"yield", "any", "boolean", "never", "number", "object", "string", "symbol", "undefined",
		"unknown",
	),
}

// setOf builds a lookup set from a list of words
//
// words: the words to index
//
// a set containing every word
func setOf(words ...string) map[string]bool {
	set := make(map[string]bool, len(words))
	for _, word := range words {
		set[word] = true
	}
	return set
}

// EscapeIdentifier makes a name safe to emit as an identifier in the target language by
// suffixing an underscore until it no longer collides with a reserved word. the proto field name
// is untouched, so escaping changes only how the generated code spells the member -- never the
// key it serializes under
//
// name: the identifier to escape
// language: target language (go, c++, python, typescript)
//
// an identifier that is safe to emit
func EscapeIdentifier(name, language string) string {
	reserved := reservedWords[language]
	for reserved[name] {
		name += "_"
	}
	return name
}

// ReservedSet returns a fresh, mutable set of the identifiers reserved in a target language.
// generators seed their import-alias bookkeeping with it, so an alias never lands on a keyword
//
// language: target language (go, c++, python, typescript)
//
// a copy of the language's reserved words
func ReservedSet(language string) map[string]bool {
	reserved := make(map[string]bool, len(reservedWords[language]))
	for word := range reservedWords[language] {
		reserved[word] = true
	}
	return reserved
}

// UniqueNames rewrites a list of identifiers so that no two are equal, appending a numeric suffix
// to later duplicates. two distinct proto names can converge on one generated name -- Go
// title-cases "user_id" and "userId" to the same thing, and escaping can map "class" and "class_"
// together -- and emitting the duplicate declares the same member twice
//
// names: the identifiers, in declaration order
//
// the identifiers, made unique, in the same order
func UniqueNames(names []string) []string {
	taken := make(map[string]bool, len(names))
	result := make([]string, len(names))

	for i, name := range names {
		unique := name
		for n := 2; taken[unique]; n++ {
			unique = fmt.Sprintf("%s%d", name, n)
		}
		taken[unique] = true
		result[i] = unique
	}

	return result
}

// ExternalFiles returns every proto file, other than the one being generated, that defines a
// message type referenced by the given messages. one output file is produced per proto file, so
// each of these has to be turned into an import in the target language -- naming the type alone
// would leave the generated code referring to something it never defines
//
// a file protoc was not asked to generate has no output to import at all, so referring into one
// is reported rather than turned into an import of a module that does not exist. this is what
// catches a reference to a well-known type such as google.protobuf.Timestamp
//
// messages: the messages being generated
// current: the file being generated
// registry: TypeRegistry for type resolution
//
// files: the referenced foreign files, sorted by proto file name
// err: unresolvable reference, or a reference into a file that is not being generated
func ExternalFiles(messages []*MessageRef, current *descriptorpb.FileDescriptorProto, registry *TypeRegistry) ([]*descriptorpb.FileDescriptorProto, error) {
	seen := make(map[string]*descriptorpb.FileDescriptorProto)

	for _, msg := range messages {
		fields, err := GetMessageFields(msg.Descriptor, registry)
		if err != nil {
			return nil, err
		}

		for _, field := range fields {
			// MessageType is set both for a plain message field and for a map whose value is a
			// message, so this covers every way a foreign type can be named
			if field.MessageType == "" {
				continue
			}

			ref, err := registry.Resolve(field.MessageType)
			if err != nil {
				return nil, fmt.Errorf("field %s: %w", field.Name, err)
			}
			if ref.File.GetName() == current.GetName() {
				continue
			}
			if !registry.IsGenerated(ref.File.GetName()) {
				return nil, fmt.Errorf(
					"field %s refers to %s, which is defined in %s -- that file is not being generated, so there is no output to import it from. pass it to protoc alongside %s, or replace the field with a type this plugin generates",
					field.Name, strings.TrimPrefix(field.MessageType, "."), ref.File.GetName(), current.GetName())
			}
			seen[ref.File.GetName()] = ref.File
		}
	}

	files := make([]*descriptorpb.FileDescriptorProto, 0, len(seen))
	for _, f := range seen {
		files = append(files, f)
	}
	slices.SortFunc(files, func(a, b *descriptorpb.FileDescriptorProto) int {
		return strings.Compare(a.GetName(), b.GetName())
	})
	return files, nil
}

// AssignAliases gives each file a unique identifier to import it under, so that two files
// defining a type of the same name stay distinguishable in the generated code. an alias that
// collides with a reserved name or with an already assigned one gets a numeric suffix
//
// files: the files to name, in a stable order
// reserved: identifiers the aliases must avoid (locally generated type names, language keywords)
// base: produces the preferred alias for a file
//
// map from proto file name to the alias to import it under
func AssignAliases(files []*descriptorpb.FileDescriptorProto, reserved map[string]bool, base func(*descriptorpb.FileDescriptorProto) string) map[string]string {
	aliases := make(map[string]string, len(files))
	taken := make(map[string]bool, len(reserved)+len(files))
	for name := range reserved {
		taken[name] = true
	}

	for _, f := range files {
		preferred := SanitizePackageName(base(f))
		if preferred == "" {
			preferred = "pb"
		}

		alias := preferred
		for i := 1; taken[alias]; i++ {
			alias = fmt.Sprintf("%s%d", preferred, i)
		}

		taken[alias] = true
		aliases[f.GetName()] = alias
	}

	return aliases
}

// GetFileBaseName extracts the base name from a file path. for "path/to/file.proto", returns "file". returns the base name without directory path or extension
//
// filename: the file path
//
// base name without directory path or extension
func GetFileBaseName(filename string) string {
	return ProtoFileStem(path.Base(filename))
}

// ProtoFileStem strips the extension from a proto file name, keeping any directory part.
// generated files sit alongside their proto in the output tree, so "a/b/msg.proto" becomes
// "a/b/msg" -- the stem every language builds its import or include path from
//
// name: the proto file name
//
// the file name without its extension
func ProtoFileStem(name string) string {
	return strings.TrimSuffix(name, path.Ext(name))
}

// RelativeImportPath expresses a generated file's path relative to the directory of the file
// importing it, for languages whose imports are resolved relative to the importing module. proto
// file names always use forward slashes, so the result does too, and it is always prefixed with
// "./" or "../" so it reads as a relative path rather than a package name
//
// fromFile: the proto file name of the importing file
// toStem: the extensionless path of the file being imported
//
// the relative path to toStem
func RelativeImportPath(fromFile, toStem string) string {
	fromParts := strings.Split(path.Dir(fromFile), "/")
	if path.Dir(fromFile) == "." {
		fromParts = nil
	}
	toParts := strings.Split(toStem, "/")

	// drop the directories the two paths share, keeping at least the target's file name
	common := 0
	for common < len(fromParts) && common < len(toParts)-1 && fromParts[common] == toParts[common] {
		common++
	}

	var rel []string
	for range fromParts[common:] {
		rel = append(rel, "..")
	}
	rel = append(rel, toParts[common:]...)

	result := strings.Join(rel, "/")
	if !strings.HasPrefix(result, ".") {
		result = "./" + result
	}
	return result
}

// IsBuiltinType checks if a FieldType is a built-in type (not a message). returns true for primitive types, false for message types
//
// ft: the FieldType to check
//
// true if builtin type, false if message type
func IsBuiltinType(ft FieldType) bool {
	return ft != FieldTypeMessage && ft != FieldTypeBytes
}

// GetZeroValue returns the zero/default value for a FieldType in a given language. for example, bool defaults to false, numbers to 0, strings to empty string, etc
//
// ft: the FieldType
// language: target language (go, c++, python, typescript)
//
// zero value as a string representation suitable for code generation
func GetZeroValue(ft FieldType, language string) string {
	switch language {
	case "go":
		// Go: use zero values or constructors
		switch ft {
		case FieldTypeBool:
			return "false"
		case FieldTypeString:
			return `""`
		case FieldTypeFloat, FieldTypeDouble:
			return "0.0"
		default:
			return "0"
		}
	case "python":
		switch ft {
		case FieldTypeBool:
			return "False"
		case FieldTypeString:
			return `""`
		case FieldTypeFloat, FieldTypeDouble:
			return "0.0"
		case FieldTypeBytes:
			return "b''"
		default:
			return "0"
		}
	case "typescript":
		switch ft {
		case FieldTypeBool:
			return "false"
		case FieldTypeString:
			return `""`
		case FieldTypeFloat, FieldTypeDouble:
			return "0.0"
		case FieldTypeBytes:
			return "new Uint8Array()"
		default:
			return "0"
		}
	case "c++":
		switch ft {
		case FieldTypeBool:
			return "false"
		case FieldTypeString:
			return `""`
		case FieldTypeFloat:
			return "0.0f"
		case FieldTypeDouble:
			return "0.0"
		default:
			return "0"
		}
	default:
		return "null"
	}
}

// GetTypeName converts a FieldType to a language-specific type name. handles built-in types and returns the appropriate type for the target language. for message types, use GetUnqualifiedTypeName or full message name directly
//
// ft: the FieldType to convert
// language: target language (go, c++, python, typescript)
//
// language-specific type name
func GetTypeName(ft FieldType, language string) string {
	switch language {
	case "go":
		switch ft {
		case FieldTypeBool:
			return "bool"
		case FieldTypeInt32, FieldTypeSint32, FieldTypeSfixed32:
			return "int32"
		case FieldTypeInt64, FieldTypeSint64, FieldTypeSfixed64:
			return "int64"
		// fixed32/fixed64 are the *unsigned* fixed-width types in proto; only sfixed32/sfixed64
		// are signed. mapping them to a signed type would reject the top half of their range
		case FieldTypeUint32, FieldTypeFixed32:
			return "uint32"
		case FieldTypeUint64, FieldTypeFixed64:
			return "uint64"
		case FieldTypeFloat:
			return "float32"
		case FieldTypeDouble:
			return "float64"
		case FieldTypeString:
			return "string"
		case FieldTypeBytes:
			return "[]byte"
		default:
			return "interface{}"
		}
	case "python":
		switch ft {
		case FieldTypeBool:
			return "bool"
		case FieldTypeInt32, FieldTypeSint32, FieldTypeFixed32, FieldTypeSfixed32:
			return "int"
		case FieldTypeInt64, FieldTypeSint64, FieldTypeFixed64, FieldTypeSfixed64:
			return "int"
		case FieldTypeUint32, FieldTypeUint64:
			return "int"
		case FieldTypeFloat:
			return "float"
		case FieldTypeDouble:
			return "float"
		case FieldTypeString:
			return "str"
		case FieldTypeBytes:
			return "bytes"
		default:
			return "Any"
		}
	case "typescript":
		switch ft {
		case FieldTypeBool:
			return "boolean"
		case FieldTypeInt32, FieldTypeSint32, FieldTypeFixed32, FieldTypeSfixed32,
			FieldTypeInt64, FieldTypeSint64, FieldTypeFixed64, FieldTypeSfixed64,
			FieldTypeUint32, FieldTypeUint64:
			return "number"
		case FieldTypeFloat, FieldTypeDouble:
			return "number"
		case FieldTypeString:
			return "string"
		case FieldTypeBytes:
			return "Uint8Array"
		default:
			return "any"
		}
	case "c++":
		switch ft {
		case FieldTypeBool:
			return "bool"
		case FieldTypeInt32, FieldTypeSint32, FieldTypeSfixed32:
			return "int32_t"
		case FieldTypeInt64, FieldTypeSint64, FieldTypeSfixed64:
			return "int64_t"
		// as in Go: fixed32/fixed64 are unsigned, sfixed32/sfixed64 are the signed ones
		case FieldTypeUint32, FieldTypeFixed32:
			return "uint32_t"
		case FieldTypeUint64, FieldTypeFixed64:
			return "uint64_t"
		case FieldTypeFloat:
			return "float"
		case FieldTypeDouble:
			return "double"
		case FieldTypeString:
			return "std::string"
		case FieldTypeBytes:
			return "std::string"
		default:
			return "void*"
		}
	default:
		return "unknown"
	}
}
