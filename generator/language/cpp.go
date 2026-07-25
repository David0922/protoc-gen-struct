package language

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"strings"

	"google.golang.org/protobuf/types/descriptorpb"
)

// CppGenerator generates C++ code from proto files
// it produces struct definitions with JSON/YAML serialization helpers
type CppGenerator struct {
	registry *TypeRegistry
	buf      *bytes.Buffer

	// per-file state, reset at the start of every Generate call
	file       *descriptorpb.FileDescriptorProto
	namespaces []string // the nested namespaces this file's types live in
}

// NewCppGenerator creates a new C++ code generator. takes a TypeRegistry for message type resolution
//
// registry: TypeRegistry for type resolution
//
// configured CppGenerator ready to generate C++ code
func NewCppGenerator(registry *TypeRegistry) Generator {
	return &CppGenerator{
		registry: registry,
		buf:      &bytes.Buffer{},
	}
}

// Generate generates C++ code (header file) for a single proto file. produces struct definitions with JSON/YAML serialization functions
//
// the header is laid out in four blocks rather than one, because both serialization libraries
// resolve their converters by name lookup at the point of use. the structs' member functions
// convert their message-typed fields through nlohmann's to_json/from_json overloads and
// yaml-cpp's YAML::convert specialization, so those have to be *declared* before the first
// struct that uses one. their definitions only need the complete type, so they come afterwards
//
// file: the FileDescriptorProto to generate code for
//
// code: the generated C++ code
// err: generation errors or nil
func (g *CppGenerator) Generate(file *descriptorpb.FileDescriptorProto) (string, error) {
	g.buf.Reset()
	g.file = file
	g.namespaces = cppNamespaceSegments(file)

	messages := CollectMessages(file)

	externals, err := ExternalFiles(messages, file, g.registry)
	if err != nil {
		return "", err
	}

	// write header guards. the guard is derived from the full path, not just the base name, so
	// two protos of the same name in different directories do not share one
	guard := cppIncludeGuard(file.GetName())
	fmt.Fprintf(g.buf, "#ifndef %s\n#define %s\n\n", guard, guard)

	g.writeCppIncludes(externals)

	g.writeCppForwardDeclarations(messages)
	g.writeCppYamlConverterDeclarations(messages)

	g.openNamespaces()
	for _, msg := range messages {
		if err := g.generateCppMessage(msg); err != nil {
			return "", err
		}
	}
	g.closeNamespaces()

	g.writeCppYamlConverterDefinitions(messages)

	// close header guard
	fmt.Fprintf(g.buf, "#endif // %s\n", guard)

	return g.buf.String(), nil
}

// openNamespaces opens one nested namespace per segment of the proto package. a dotted proto
// package such as "example.v1" is not a legal C++ namespace name on its own
func (g *CppGenerator) openNamespaces() {
	for _, segment := range g.namespaces {
		fmt.Fprintf(g.buf, "namespace %s {\n", segment)
	}
	if len(g.namespaces) > 0 {
		fmt.Fprint(g.buf, "\n")
	}
}

// closeNamespaces closes the namespaces opened by openNamespaces, in reverse order
func (g *CppGenerator) closeNamespaces() {
	for i := len(g.namespaces) - 1; i >= 0; i-- {
		fmt.Fprintf(g.buf, "} // namespace %s\n", g.namespaces[i])
	}
	fmt.Fprint(g.buf, "\n")
}

// writeCppIncludes writes the include directives for C++. includes the standard library and
// serialization headers, plus the generated header of every proto file whose types are
// referenced -- those types are defined in another header, so naming one without including it
// would not compile
//
// externals: the referenced foreign files
func (g *CppGenerator) writeCppIncludes(externals []*descriptorpb.FileDescriptorProto) {
	fmt.Fprint(g.buf, "#include <string>\n")
	fmt.Fprint(g.buf, "#include <vector>\n")
	fmt.Fprint(g.buf, "#include <map>\n")
	fmt.Fprint(g.buf, "#include <optional>\n")
	fmt.Fprint(g.buf, "#include <nlohmann/json.hpp>\n")
	fmt.Fprint(g.buf, "#include <yaml-cpp/yaml.h>\n")

	if len(externals) > 0 {
		fmt.Fprint(g.buf, "\n")
		for _, ext := range externals {
			fmt.Fprintf(g.buf, "#include %q\n", ProtoFileStem(ext.GetName())+".h")
		}
	}

	fmt.Fprint(g.buf, "\n")
}

// writeCppForwardDeclarations declares every struct in the file along with its nlohmann
// converters. nlohmann finds to_json/from_json by argument-dependent lookup from inside the
// member functions that serialize a message-typed field, so an overload declared only after the
// struct that needs it would never be found
//
// messages: the messages being generated
func (g *CppGenerator) writeCppForwardDeclarations(messages []*MessageRef) {
	if len(messages) == 0 {
		return
	}

	g.openNamespaces()

	for _, msg := range messages {
		fmt.Fprintf(g.buf, "struct %s;\n", g.localName(msg))
	}
	fmt.Fprint(g.buf, "\n")

	for _, msg := range messages {
		fmt.Fprintf(g.buf, "inline void to_json(nlohmann::json& j, const %s& value);\n", g.localName(msg))
		fmt.Fprintf(g.buf, "inline void from_json(const nlohmann::json& j, %s& value);\n", g.localName(msg))
	}
	fmt.Fprint(g.buf, "\n")

	g.closeNamespaces()
}

// writeCppYamlConverterDeclarations declares a YAML::convert specialization for every struct.
// yaml-cpp reads and writes a user type through this specialization, and it has to live in
// namespace YAML rather than the file's own. only the declaration is needed here -- a
// specialization's member functions are not instantiated until they are called, so the structs
// may still be incomplete at this point
//
// messages: the messages being generated
func (g *CppGenerator) writeCppYamlConverterDeclarations(messages []*MessageRef) {
	if len(messages) == 0 {
		return
	}

	fmt.Fprint(g.buf, "namespace YAML {\n\n")
	for _, msg := range messages {
		qualified := g.qualifiedName(msg)
		fmt.Fprintf(g.buf, "template <>\n")
		fmt.Fprintf(g.buf, "struct convert<%s> {\n", qualified)
		fmt.Fprintf(g.buf, "  static Node encode(const %s& value);\n", qualified)
		fmt.Fprintf(g.buf, "  static bool decode(const Node& node, %s& value);\n", qualified)
		fmt.Fprintf(g.buf, "};\n\n")
	}
	fmt.Fprint(g.buf, "} // namespace YAML\n\n")
}

// writeCppYamlConverterDefinitions defines the YAML::convert specializations declared earlier,
// forwarding to each struct's own ToYAML/FromYAML. these come after the struct definitions
// because the bodies need the complete type
//
// messages: the messages being generated
func (g *CppGenerator) writeCppYamlConverterDefinitions(messages []*MessageRef) {
	if len(messages) == 0 {
		return
	}

	fmt.Fprint(g.buf, "namespace YAML {\n\n")
	for _, msg := range messages {
		qualified := g.qualifiedName(msg)
		fmt.Fprintf(g.buf, "inline Node convert<%s>::encode(const %s& value) {\n", qualified, qualified)
		fmt.Fprintf(g.buf, "  return value.ToYAML();\n")
		fmt.Fprintf(g.buf, "}\n\n")
		fmt.Fprintf(g.buf, "inline bool convert<%s>::decode(const Node& node, %s& value) {\n", qualified, qualified)
		fmt.Fprintf(g.buf, "  if (!node.IsMap()) {\n")
		fmt.Fprintf(g.buf, "    return false;\n")
		fmt.Fprintf(g.buf, "  }\n")
		fmt.Fprintf(g.buf, "  value.FromYAML(node);\n")
		fmt.Fprintf(g.buf, "  return true;\n")
		fmt.Fprintf(g.buf, "}\n\n")
	}
	fmt.Fprint(g.buf, "} // namespace YAML\n\n")
}

// generateCppMessage generates a C++ struct and its serialization functions for a message,
// followed by the nlohmann converters that let the struct be nested inside another one
//
// msg: the message to generate code for
//
// err: generation errors or nil
func (g *CppGenerator) generateCppMessage(msg *MessageRef) error {
	fields, err := GetMessageFields(msg.Descriptor, g.registry)
	if err != nil {
		return err
	}

	typeName := g.localName(msg)
	names := g.memberNames(fields)

	// write struct definition
	fmt.Fprintf(g.buf, "// %s is a generated struct from proto message %s\n", typeName, msg.FullName)
	fmt.Fprintf(g.buf, "struct %s {\n", typeName)

	for i, field := range fields {
		cppType, err := g.getCppType(field)
		if err != nil {
			return err
		}

		// scalars have no default constructor, so a member left without an initializer holds an
		// indeterminate value: reading one is undefined behaviour, and a field missing from the
		// input would keep whatever happened to be on the stack rather than the zero the Go,
		// Python and TypeScript readers all produce. strings, containers, optionals and nested
		// messages already default-construct to the right thing
		if initializer := g.zeroInitializer(field); initializer != "" {
			fmt.Fprintf(g.buf, "  %s %s = %s;\n", cppType, names[i], initializer)
			continue
		}
		fmt.Fprintf(g.buf, "  %s %s;\n", cppType, names[i])
	}

	// write member functions
	fmt.Fprint(g.buf, "\n")

	// ToJSON function
	if err := g.writeCppToJSON(fields, names); err != nil {
		return err
	}

	// FromJSON function
	if err := g.writeCppFromJSON(fields, names); err != nil {
		return err
	}

	// ToYAML function
	g.writeCppToYAML(fields, names)

	// FromYAML function
	if err := g.writeCppFromYAML(fields, names); err != nil {
		return err
	}

	fmt.Fprint(g.buf, "};\n\n")

	// the converters nlohmann looks up when this struct appears as a field of another one
	fmt.Fprintf(g.buf, "inline void to_json(nlohmann::json& j, const %s& value) {\n", typeName)
	fmt.Fprintf(g.buf, "  j = value.ToJSON();\n")
	fmt.Fprintf(g.buf, "}\n\n")
	fmt.Fprintf(g.buf, "inline void from_json(const nlohmann::json& j, %s& value) {\n", typeName)
	fmt.Fprintf(g.buf, "  value.FromJSON(j);\n")
	fmt.Fprintf(g.buf, "}\n\n")

	return nil
}

// localName returns the name a message is declared under in this file, escaped so it cannot be a
// C++ keyword
//
// msg: the message to name
//
// the struct name
func (g *CppGenerator) localName(msg *MessageRef) string {
	return EscapeIdentifier(msg.TypeName, "c++")
}

// memberNames returns the member name for each field, escaped so a proto field called "class" or
// "operator" does not produce a struct that fails to parse, and de-duplicated in case escaping
// made two of them equal
//
// fields: the message fields, in declaration order
//
// the member names, in the same order
func (g *CppGenerator) memberNames(fields []*FieldInfo) []string {
	names := make([]string, len(fields))
	for i, field := range fields {
		names[i] = EscapeIdentifier(field.Name, "c++")
	}
	return UniqueNames(names)
}

// zeroInitializer returns the default member initializer a field needs, or "" when the type
// already default-constructs to the right value
//
// field: the field to initialize
//
// the initializer expression, or "" when none is needed
func (g *CppGenerator) zeroInitializer(field *FieldInfo) string {
	// a container, an optional, a string and a nested struct all default-construct to "empty"
	if field.IsMap || field.IsRepeated || field.IsOptional ||
		field.Type == FieldTypeMessage || field.Type == FieldTypeString || field.Type == FieldTypeBytes {
		return ""
	}
	return GetZeroValue(field.Type, "c++")
}

// qualifiedName returns the fully qualified C++ name of a message, rooted at the global
// namespace. a YAML::convert specialization is written outside the file's own namespace, so it
// cannot name a type relative to it
//
// msg: the message to name
//
// the fully qualified C++ type name, e.g. "::example::v1::Outer_Inner"
func (g *CppGenerator) qualifiedName(msg *MessageRef) string {
	var b strings.Builder
	for _, segment := range cppNamespaceSegments(msg.File) {
		b.WriteString("::")
		b.WriteString(segment)
	}
	b.WriteString("::")
	b.WriteString(EscapeIdentifier(msg.TypeName, "c++"))
	return b.String()
}

// cppNamespaceSegments splits a file's proto package into legal C++ namespace names, one per
// dotted segment
//
// f: the proto file descriptor
//
// the namespace segments, empty when the file declares no package
func cppNamespaceSegments(f *descriptorpb.FileDescriptorProto) []string {
	var segments []string
	for _, segment := range strings.Split(GetPackageName(f), ".") {
		if sanitized := SanitizePackageName(segment); sanitized != "" {
			segments = append(segments, sanitized)
		}
	}
	return segments
}

// getCppType returns the declared C++ type for a FieldInfo. this is the base type, wrapped in
// std::optional<> for fields declared `optional` in proto3 so that an unset field is
// distinguishable from one set to its zero value
//
// field: the FieldInfo to get the C++ type for
//
// cppType: C++ type as a string
// err: unresolvable message reference
func (g *CppGenerator) getCppType(field *FieldInfo) (string, error) {
	base, err := g.getCppBaseType(field)
	if err != nil {
		return "", err
	}
	return cppDeclaredType(field, base), nil
}

// cppDeclaredType wraps a base type in std::optional when the field is declared `optional`. the
// decoders already hold the base type, so this saves them resolving the field a second time
//
// field: the field the type belongs to
// base: the type the field holds, ignoring optionality
//
// the declared member type
func cppDeclaredType(field *FieldInfo, base string) string {
	if field.IsOptional {
		return fmt.Sprintf("std::optional<%s>", base)
	}
	return base
}

// getCppBaseType returns the C++ type a FieldInfo holds, ignoring optionality. handles
// primitives, message types, vectors (lists), and maps. this is the type stored *inside* a
// std::optional, so it is what the JSON/YAML converters have to be asked for
//
// field: the FieldInfo to get the C++ type for
//
// cppType: C++ type as a string
// err: unresolvable message reference
func (g *CppGenerator) getCppBaseType(field *FieldInfo) (string, error) {
	if field.IsMap {
		valueType, err := g.getCppValueType(field.MapValueType, field.MessageType)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("std::map<%s, %s>", GetTypeName(field.MapKeyType, "c++"), valueType), nil
	}

	if field.IsRepeated {
		baseType, err := g.getCppValueType(field.Type, field.MessageType)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("std::vector<%s>", baseType), nil
	}

	return g.getCppValueType(field.Type, field.MessageType)
}

// getCppValueType returns the C++ type for a single value. a message defined in another proto
// file is named by its fully qualified type, since it lives in that file's namespace
//
// ft: the FieldType
// messageType: fully qualified message type name (empty for primitives)
//
// name: C++ type name
// err: unresolvable message reference
func (g *CppGenerator) getCppValueType(ft FieldType, messageType string) (string, error) {
	if ft != FieldTypeMessage || messageType == "" {
		return GetTypeName(ft, "c++"), nil
	}

	ref, err := g.registry.Resolve(messageType)
	if err != nil {
		return "", err
	}
	if ref.File.GetName() == g.file.GetName() {
		return g.localName(ref), nil
	}
	return g.qualifiedName(ref), nil
}

// hasStringJSONKeys reports whether a map field's key type is already a JSON object key. a JSON
// object can only be keyed by a string, so nlohmann encodes any other std::map as an array of
// pairs -- while Go and Python both write an object with the key converted to a string. those
// maps need converting by hand to stay readable across languages
//
// field: the map field to check
//
// true when the key needs no conversion
func hasStringJSONKeys(field *FieldInfo) bool {
	return field.MapKeyType == FieldTypeString
}

// cppKeyToString returns the expression turning a map key into its JSON object key
//
// field: the map field
// key: the expression holding the key
//
// the string-valued expression
func cppKeyToString(field *FieldInfo, key string) string {
	if hasStringJSONKeys(field) {
		return key
	}
	return fmt.Sprintf("std::to_string(%s)", key)
}

// cppKeyFromString returns the expression parsing a JSON object key back into the map's key type
//
// field: the map field
// key: the expression holding the string key
//
// the key-typed expression
func cppKeyFromString(field *FieldInfo, key string) string {
	keyType := GetTypeName(field.MapKeyType, "c++")
	if hasStringJSONKeys(field) {
		return key
	}
	// every remaining proto map key type is an integer; unsigned ones go through stoull so a
	// large value is not rejected as out of range for a signed long long
	switch field.MapKeyType {
	case FieldTypeUint32, FieldTypeUint64, FieldTypeFixed32, FieldTypeFixed64:
		return fmt.Sprintf("static_cast<%s>(std::stoull(%s))", keyType, key)
	default:
		return fmt.Sprintf("static_cast<%s>(std::stoll(%s))", keyType, key)
	}
}

// writeCppToJSON writes the ToJSON serialization function for a struct. converts the struct to a JSON object and returns it. function signature: nlohmann::json ToJSON() const;
//
// fields: the message fields to serialize
// names: the member name of each field
//
// err: unresolvable message reference
func (g *CppGenerator) writeCppToJSON(fields []*FieldInfo, names []string) error {
	fmt.Fprintf(g.buf, "  nlohmann::json ToJSON() const {\n")
	// as in ToYAML: a default-constructed json is null, so a message that emits no key would
	// serialize as null rather than as the empty object the other three generators write
	fmt.Fprintf(g.buf, "    nlohmann::json j = nlohmann::json::object();\n")

	for i, field := range fields {
		name := names[i]

		// a map with a non-string key is written out entry by entry, so it lands as a JSON object
		// keyed by the stringified key rather than as the array of pairs nlohmann would produce
		if field.IsMap && !hasStringJSONKeys(field) {
			fmt.Fprintf(g.buf, "    {\n")
			fmt.Fprintf(g.buf, "      nlohmann::json entries = nlohmann::json::object();\n")
			fmt.Fprintf(g.buf, "      for (const auto& entry : this->%s) {\n", name)
			fmt.Fprintf(g.buf, "        entries[%s] = entry.second;\n", cppKeyToString(field, "entry.first"))
			fmt.Fprintf(g.buf, "      }\n")
			fmt.Fprintf(g.buf, "      j[\"%s\"] = entries;\n", field.ProtoName)
			fmt.Fprintf(g.buf, "    }\n")
			continue
		}

		// an unset optional field is omitted from the object entirely rather than written as
		// null, so a round trip through ToJSON/FromJSON preserves absence
		if field.IsOptional {
			fmt.Fprintf(g.buf, "    if (this->%s.has_value()) {\n", name)
			fmt.Fprintf(g.buf, "      j[\"%s\"] = *this->%s;\n", field.ProtoName, name)
			fmt.Fprintf(g.buf, "    }\n")
			continue
		}
		fmt.Fprintf(g.buf, "    j[\"%s\"] = this->%s;\n", field.ProtoName, name)
	}

	fmt.Fprintf(g.buf, "    return j;\n")
	fmt.Fprintf(g.buf, "  }\n\n")

	return nil
}

// writeCppFromJSON writes the FromJSON deserialization function for a struct. populates the struct from a JSON object. function signature: void FromJSON(const nlohmann::json& j);
//
// fields: the message fields to deserialize
// names: the member name of each field
//
// err: unresolvable message reference
func (g *CppGenerator) writeCppFromJSON(fields []*FieldInfo, names []string) error {
	fmt.Fprintf(g.buf, "  void FromJSON(const nlohmann::json& j) {\n")

	for i, field := range fields {
		name := names[i]

		// always decode into the base type: asking for the contained type keeps this working
		// regardless of whether the nlohmann version in use knows how to convert into a
		// std::optional, and an optional member is assignable from its contained type anyway
		cppType, err := g.getCppBaseType(field)
		if err != nil {
			return err
		}

		// a repeated field or map that the writer left out, or wrote as null, means "empty" --
		// asking nlohmann to decode a null into a container throws instead
		if field.IsRepeated || field.IsMap {
			fmt.Fprintf(g.buf, "    this->%s.clear();\n", name)
			fmt.Fprintf(g.buf, "    if (j.contains(\"%s\") && !j[\"%s\"].is_null()) {\n", field.ProtoName, field.ProtoName)

			if field.IsMap && !hasStringJSONKeys(field) {
				valueType, err := g.getCppValueType(field.MapValueType, field.MessageType)
				if err != nil {
					return err
				}
				fmt.Fprintf(g.buf, "      for (auto it = j[\"%s\"].begin(); it != j[\"%s\"].end(); ++it) {\n", field.ProtoName, field.ProtoName)
				fmt.Fprintf(g.buf, "        this->%s[%s] = it.value().get<%s>();\n", name, cppKeyFromString(field, "it.key()"), valueType)
				fmt.Fprintf(g.buf, "      }\n")
			} else {
				fmt.Fprintf(g.buf, "      this->%s = j[\"%s\"].get<%s>();\n", name, field.ProtoName, cppType)
			}

			fmt.Fprintf(g.buf, "    }\n")
			continue
		}

		// a singular member is reset first, so that a key the document leaves out (or writes as
		// null) reads back as absent even when decoding into a struct that already holds a value.
		// without this, decoding twice would merge scalars while replacing containers, and the
		// other three generators all rebuild the whole message
		fmt.Fprintf(g.buf, "    this->%s = %s{};\n", name, cppDeclaredType(field, cppType))
		fmt.Fprintf(g.buf, "    if (j.contains(\"%s\") && !j[\"%s\"].is_null()) {\n", field.ProtoName, field.ProtoName)
		fmt.Fprintf(g.buf, "      this->%s = j[\"%s\"].get<%s>();\n", name, field.ProtoName, cppType)
		fmt.Fprintf(g.buf, "    }\n")
	}

	fmt.Fprintf(g.buf, "  }\n\n")

	return nil
}

// writeCppToYAML writes the ToYAML serialization function for a struct. converts the struct to a YAML node and returns it. function signature: YAML::Node ToYAML() const;
//
// YAML keys are not restricted to strings, so a map is emitted with its own key type here and
// needs none of the conversion the JSON path does
//
// fields: the message fields to serialize
// names: the member name of each field
func (g *CppGenerator) writeCppToYAML(fields []*FieldInfo, names []string) {
	fmt.Fprintf(g.buf, "  YAML::Node ToYAML() const {\n")
	// a default-constructed Node is Null, and stays Null when the message emits no key at all --
	// an empty message, or one whose every field is an unset optional. YAML::convert<T>::decode
	// rejects a non-map node, so such a node throws TypedBadConversion when it is read back as an
	// element of a repeated field or a map value. starting from an explicitly empty map keeps it
	// decodable, and matches the "{}" the other three generators write
	fmt.Fprintf(g.buf, "    YAML::Node node(YAML::NodeType::Map);\n")

	for i, field := range fields {
		name := names[i]

		// as in ToJSON, an unset optional field is left out of the emitted node
		if field.IsOptional {
			fmt.Fprintf(g.buf, "    if (this->%s.has_value()) {\n", name)
			fmt.Fprintf(g.buf, "      node[\"%s\"] = *this->%s;\n", field.ProtoName, name)
			fmt.Fprintf(g.buf, "    }\n")
			continue
		}
		fmt.Fprintf(g.buf, "    node[\"%s\"] = this->%s;\n", field.ProtoName, name)
	}

	fmt.Fprintf(g.buf, "    return node;\n")
	fmt.Fprintf(g.buf, "  }\n\n")
}

// writeCppFromYAML writes the FromYAML deserialization function for a struct. populates the struct from a YAML node. function signature: void FromYAML(const YAML::Node& node);
//
// fields: the message fields to deserialize
// names: the member name of each field
//
// err: unresolvable message reference
func (g *CppGenerator) writeCppFromYAML(fields []*FieldInfo, names []string) error {
	fmt.Fprintf(g.buf, "  void FromYAML(const YAML::Node& node) {\n")

	for i, field := range fields {
		name := names[i]

		// always decode into the base type: yaml-cpp ships no converter for std::optional, and an
		// optional member is assignable from its contained type anyway
		cppType, err := g.getCppBaseType(field)
		if err != nil {
			return err
		}

		if field.IsRepeated || field.IsMap {
			fmt.Fprintf(g.buf, "    this->%s.clear();\n", name)
			fmt.Fprintf(g.buf, "    if (node[\"%s\"] && !node[\"%s\"].IsNull()) {\n", field.ProtoName, field.ProtoName)
			fmt.Fprintf(g.buf, "      this->%s = node[\"%s\"].as<%s>();\n", name, field.ProtoName, cppType)
			fmt.Fprintf(g.buf, "    }\n")
			continue
		}

		// as in FromJSON, a singular member is reset first so that a missing or null entry reads
		// back as absent rather than leaving whatever the struct already held
		fmt.Fprintf(g.buf, "    this->%s = %s{};\n", name, cppDeclaredType(field, cppType))
		fmt.Fprintf(g.buf, "    if (node[\"%s\"] && !node[\"%s\"].IsNull()) {\n", field.ProtoName, field.ProtoName)
		fmt.Fprintf(g.buf, "      this->%s = node[\"%s\"].as<%s>();\n", name, field.ProtoName, cppType)
		fmt.Fprintf(g.buf, "    }\n")
	}

	fmt.Fprintf(g.buf, "  }\n\n")

	return nil
}

// cppIncludeGuard returns the include guard macro for a generated header
//
// the readable part of the macro is lossy -- stringToConstant maps both "/" and "_" to "_", so
// "a/b.proto" and "a_b.proto" mangle alike -- and two headers sharing a guard is silent: the
// second one a translation unit includes is skipped whole, leaving every type it defines
// undefined at the point of use. a hash of the untouched path keeps distinct protos distinct
//
// name: the proto file name
//
// the guard macro
func cppIncludeGuard(name string) string {
	sum := fnv.New32a()
	// Write on a Hash never fails, and the error it returns to satisfy io.Writer is always nil
	sum.Write([]byte(name))
	return fmt.Sprintf("PROTO_%s_%08X_H_", stringToConstant(ProtoFileStem(name)), sum.Sum32())
}

// stringToConstant converts a string to a C++ constant name. converts to uppercase and replaces non-alphanumeric characters with underscores
//
// s: the string to convert
//
// C++ constant name
func stringToConstant(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
			b.WriteRune(c - 32)
		case (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'):
			b.WriteRune(c)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
