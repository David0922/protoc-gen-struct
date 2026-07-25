package language

import (
	"bytes"
	"fmt"
	"strings"

	"google.golang.org/protobuf/types/descriptorpb"
)

// PythonGenerator generates Python code from proto files
// it produces dataclass definitions with JSON/YAML serialization helpers
type PythonGenerator struct {
	registry *TypeRegistry
	buf      *bytes.Buffer

	// per-file state, reset at the start of every Generate call
	file    *descriptorpb.FileDescriptorProto
	aliases map[string]string // proto file name -> the alias its module is imported under
}

// NewPythonGenerator creates a new Python code generator. takes a TypeRegistry for message type resolution
//
// registry: TypeRegistry for type resolution
//
// configured PythonGenerator ready to generate Python code
func NewPythonGenerator(registry *TypeRegistry) Generator {
	return &PythonGenerator{
		registry: registry,
		buf:      &bytes.Buffer{},
	}
}

// Generate generates Python code for a single proto file. produces dataclass definitions with JSON/YAML serialization functions
//
// file: the FileDescriptorProto to generate code for
//
// code: the generated Python code
// err: generation errors or nil
func (g *PythonGenerator) Generate(file *descriptorpb.FileDescriptorProto) (string, error) {
	g.buf.Reset()
	g.file = file

	messages := CollectMessages(file)

	externals, err := ExternalFiles(messages, file, g.registry)
	if err != nil {
		return "", err
	}

	// an alias must not shadow a keyword, one of the names the module imports for itself, or a
	// class declared here
	reserved := ReservedSet("python")
	for _, name := range []string{"Any", "Dict", "List"} {
		reserved[name] = true
	}
	for _, msg := range messages {
		reserved[g.localName(msg)] = true
	}
	g.aliases = AssignAliases(externals, reserved, func(f *descriptorpb.FileDescriptorProto) string {
		return GetFileBaseName(f.GetName())
	})

	if err := g.writePythonImports(externals); err != nil {
		return "", err
	}

	for _, msg := range messages {
		if err := g.generatePythonMessage(msg); err != nil {
			return "", err
		}
	}

	return g.buf.String(), nil
}

// writePythonImports writes the import block for Python. includes the dataclass, serialization
// and typing imports, plus one module import per proto file whose types are referenced
//
// externals: the referenced foreign files
//
// err: a referenced file whose path cannot be spelled as a Python module
func (g *PythonGenerator) writePythonImports(externals []*descriptorpb.FileDescriptorProto) error {
	// postpone annotation evaluation so a dataclass may reference a class declared later in the
	// file. protoc emits messages in declaration order, so forward references are common
	fmt.Fprint(g.buf, "from __future__ import annotations\n\n")
	fmt.Fprint(g.buf, "from dataclasses import dataclass, field\n")
	fmt.Fprint(g.buf, "import json\n")
	fmt.Fprint(g.buf, "import yaml\n")
	fmt.Fprint(g.buf, "from typing import Any, Dict, List\n")

	if len(externals) > 0 {
		fmt.Fprint(g.buf, "\n")
		for _, ext := range externals {
			module, err := pythonModulePath(ext.GetName())
			if err != nil {
				return fmt.Errorf("cannot import %s from %s: %w", ext.GetName(), g.file.GetName(), err)
			}
			fmt.Fprintf(g.buf, "import %s as %s\n", module, g.aliases[ext.GetName()])
		}
	}

	fmt.Fprint(g.buf, "\n")

	return nil
}

// pythonModulePath converts a proto file name into the dotted module path its generated module
// is importable by, relative to the output root. "a/b/common.proto" becomes "a.b.common"
//
// a path segment that is not a valid Python identifier -- "my-pkg" is the usual case -- produces
// a module no import statement can name, so it is reported rather than emitted as a syntax error
//
// name: the proto file name
//
// module: the dotted module path
// err: a path segment that cannot appear in an import statement
func pythonModulePath(name string) (string, error) {
	segments := strings.Split(ProtoFileStem(name), "/")
	for _, segment := range segments {
		if !isPythonIdentifier(segment) {
			return "", fmt.Errorf("path segment %q is not a valid Python module name; rename the directory or file so every segment is a Python identifier", segment)
		}
	}
	return strings.Join(segments, "."), nil
}

// isPythonIdentifier reports whether a string can be used as a Python identifier. only ASCII is
// accepted, which is what proto paths use in practice
//
// s: the string to check
//
// true when s is a usable identifier
func isPythonIdentifier(s string) bool {
	if s == "" || reservedWords["python"][s] {
		return false
	}
	for i, c := range s {
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isDigit := c >= '0' && c <= '9'
		if isLetter || c == '_' || (isDigit && i > 0) {
			continue
		}
		return false
	}
	return true
}

// generatePythonMessage generates a Python dataclass and its serialization functions for a
// message. handles nested messages and generates JSON/YAML serialization helpers
//
// msg: the message to generate code for
//
// err: generation errors or nil
func (g *PythonGenerator) generatePythonMessage(msg *MessageRef) error {
	fields, err := GetMessageFields(msg.Descriptor, g.registry)
	if err != nil {
		return err
	}

	typeName := g.localName(msg)
	names := g.attributeNames(fields)

	// write dataclass definition
	fmt.Fprintf(g.buf, "@dataclass\n")
	fmt.Fprintf(g.buf, "class %s:\n", typeName)
	fmt.Fprintf(g.buf, "    \"\"\"generated from proto message %s.\"\"\"\n\n", msg.FullName)

	// every field gets a default. proto3 has no required fields, and the other generators leave an
	// unset optional out of the encoded output entirely, so a dataclass whose fields were
	// positional could not be constructed from their JSON at all. defaulting all of them also
	// keeps dataclass field ordering legal, which forbids a defaulted field before a bare one
	for i, field := range fields {
		pyType, err := g.getPythonType(field)
		if err != nil {
			return err
		}
		defaultValue, err := g.getPythonDefault(field)
		if err != nil {
			return err
		}
		fmt.Fprintf(g.buf, "    %s: %s = %s\n", names[i], pyType, defaultValue)
	}

	fmt.Fprint(g.buf, "\n")

	// a message with no fields still gets the full set of helpers, so that every generated class
	// answers to the same interface
	if err := g.writePythonToDict(typeName, fields, names); err != nil {
		return err
	}
	if err := g.writePythonFromDict(typeName, fields, names); err != nil {
		return err
	}

	g.writePythonToJSON(typeName)
	g.writePythonFromJSON(typeName)
	g.writePythonToYAML(typeName)
	g.writePythonFromYAML(typeName)

	return nil
}

// localName returns the name a message is declared under in this module, escaped so it cannot be
// a Python keyword
//
// msg: the message to name
//
// the class name
func (g *PythonGenerator) localName(msg *MessageRef) string {
	return EscapeIdentifier(msg.TypeName, "python")
}

// attributeNames returns the attribute name for each field. a proto field called "from" or
// "class" would otherwise produce a class body that does not parse, and one called "field" would
// shadow dataclasses.field in a later default_factory expression
//
// fields: the message fields, in declaration order
//
// the attribute names, in the same order
func (g *PythonGenerator) attributeNames(fields []*FieldInfo) []string {
	names := make([]string, len(fields))
	for i, field := range fields {
		names[i] = EscapeIdentifier(field.Name, "python")
	}
	return UniqueNames(names)
}

// getPythonType returns the Python type annotation for a FieldInfo. a field declared `optional`
// in proto3 is annotated `X | None`, so an unset field is distinguishable from one set to its
// zero value. the `from __future__ import annotations` header keeps the union syntax usable on
// interpreters older than 3.10, where it is not valid at runtime
//
// field: the FieldInfo to get the Python type for
//
// annotation: Python type annotation as a string
// err: unresolvable message reference
func (g *PythonGenerator) getPythonType(field *FieldInfo) (string, error) {
	base, err := g.getPythonBaseType(field)
	if err != nil {
		return "", err
	}
	if field.IsOptional {
		return base + " | None", nil
	}
	return base, nil
}

// getPythonBaseType returns the Python type annotation a FieldInfo holds, ignoring optionality.
// handles primitives, message types, lists, and maps
//
// field: the FieldInfo to get the Python type for
//
// annotation: Python type annotation as a string
// err: unresolvable message reference
func (g *PythonGenerator) getPythonBaseType(field *FieldInfo) (string, error) {
	if field.IsMap {
		valueType, err := g.getPythonValueType(field.MapValueType, field.MessageType)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Dict[%s, %s]", GetTypeName(field.MapKeyType, "python"), valueType), nil
	}

	if field.IsRepeated {
		baseType, err := g.getPythonValueType(field.Type, field.MessageType)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("List[%s]", baseType), nil
	}

	return g.getPythonValueType(field.Type, field.MessageType)
}

// getPythonValueType returns the Python type for a single value. a message defined in another
// proto file is qualified with the alias its module is imported under
//
// ft: the FieldType
// messageType: fully qualified message type name (empty for primitives)
//
// name: Python type name
// err: unresolvable message reference
func (g *PythonGenerator) getPythonValueType(ft FieldType, messageType string) (string, error) {
	if ft != FieldTypeMessage || messageType == "" {
		return GetTypeName(ft, "python"), nil
	}
	return g.messageName(messageType)
}

// messageName returns the expression the generated module uses to name a message class, which is
// also the expression its from_dict classmethod is called on
//
// messageType: fully qualified message type name
//
// name: the class expression, e.g. "Outer_Inner" or "common.Address"
// err: unresolvable message reference
func (g *PythonGenerator) messageName(messageType string) (string, error) {
	ref, err := g.registry.Resolve(messageType)
	if err != nil {
		return "", err
	}
	if ref.File.GetName() == g.file.GetName() {
		return g.localName(ref), nil
	}
	return g.aliases[ref.File.GetName()] + "." + g.localName(ref), nil
}

// getPythonDefault returns the default value expression for a field. mutable containers and
// message instances have to go through default_factory, since a dataclass rejects a mutable
// default shared between instances
//
// the factory is always a lambda rather than the bare callable, because a class body is looked
// up before the enclosing module: a field named "dict" would bind that name to a dataclasses.Field
// object, and a later `default_factory=dict` would then resolve to it rather than the builtin. a
// lambda body is a function scope, which skips the class namespace entirely, so no field name can
// shadow what the factory names -- builtin, sibling class, or imported module alias alike
//
// field: the FieldInfo to get the default for
//
// expr: the default value expression
// err: unresolvable message reference
func (g *PythonGenerator) getPythonDefault(field *FieldInfo) (string, error) {
	switch {
	case field.IsOptional:
		return "None", nil
	case field.IsMap:
		return "field(default_factory=lambda: {})", nil
	case field.IsRepeated:
		return "field(default_factory=lambda: [])", nil
	case field.Type == FieldTypeMessage:
		// the lambda also defers the class lookup to instantiation time, so a class named here need
		// only exist by the time the dataclass is constructed
		name, err := g.messageName(field.MessageType)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("field(default_factory=lambda: %s())", name), nil
	default:
		return GetZeroValue(field.Type, "python"), nil
	}
}

// writePythonToDict writes the to_dict function for a dataclass. this is what makes nesting work:
// json.dumps and yaml.dump cannot encode a dataclass instance, so every message-typed field is
// converted to a plain dict on the way out
//
// msgName: the generated class name
// fields: the message fields to serialize
// names: the attribute name of each field
//
// err: unresolvable message reference
func (g *PythonGenerator) writePythonToDict(msgName string, fields []*FieldInfo, names []string) error {
	fmt.Fprintf(g.buf, "    def to_dict(self) -> Dict[str, Any]:\n")
	fmt.Fprintf(g.buf, "        \"\"\"convert to a plain dict.\n")
	fmt.Fprintf(g.buf, "        nested messages are converted recursively, and an unset optional field is left out.\n")
	fmt.Fprintf(g.buf, "        returns the dict representation of this %s\n", msgName)
	fmt.Fprintf(g.buf, "        \"\"\"\n")
	fmt.Fprintf(g.buf, "        data: Dict[str, Any] = {}\n")

	for i, field := range fields {
		indent := "        "
		if field.IsOptional {
			// an unset optional field is skipped rather than written as null, matching what the
			// other generators emit
			fmt.Fprintf(g.buf, "        if self.%s is not None:\n", names[i])
			indent = "            "
		}

		value, err := g.pythonEncodeExpr(field, names[i])
		if err != nil {
			return err
		}
		fmt.Fprintf(g.buf, "%sdata[\"%s\"] = %s\n", indent, field.ProtoName, value)
	}

	fmt.Fprintf(g.buf, "        return data\n\n")

	return nil
}

// pythonEncodeExpr returns the expression converting one field of self into its dict form
//
// field: the field to encode
// name: the attribute the field is stored under
//
// expr: the encoding expression
// err: unresolvable message reference
func (g *PythonGenerator) pythonEncodeExpr(field *FieldInfo, name string) (string, error) {
	self := "self." + name

	if field.IsMap {
		if field.MapValueType != FieldTypeMessage {
			return fmt.Sprintf("dict(%s)", self), nil
		}
		return fmt.Sprintf("{key: value.to_dict() for key, value in %s.items()}", self), nil
	}

	if field.IsRepeated {
		if field.Type != FieldTypeMessage {
			return fmt.Sprintf("list(%s)", self), nil
		}
		return fmt.Sprintf("[item.to_dict() for item in %s]", self), nil
	}

	if field.Type == FieldTypeMessage {
		return self + ".to_dict()", nil
	}
	return self, nil
}

// writePythonFromDict writes the from_dict classmethod for a dataclass. it rebuilds nested
// messages as their own class rather than leaving them as the plain dicts json.loads produced
//
// msgName: the generated class name
// fields: the message fields to deserialize
// names: the attribute name of each field
//
// err: unresolvable message reference
func (g *PythonGenerator) writePythonFromDict(msgName string, fields []*FieldInfo, names []string) error {
	fmt.Fprintf(g.buf, "    @classmethod\n")
	// the parameter admits None because the body does: an empty document decodes to one, and the
	// nested calls pass a dict lookup straight through
	fmt.Fprintf(g.buf, "    def from_dict(cls, data: Dict[str, Any] | None) -> '%s':\n", msgName)
	fmt.Fprintf(g.buf, "        \"\"\"build a %s from a plain dict.\n", msgName)
	fmt.Fprintf(g.buf, "        nested messages are rebuilt recursively, and a missing field takes its default.\n")
	fmt.Fprintf(g.buf, "        a data of None builds a %s with every field defaulted.\n", msgName)
	fmt.Fprintf(g.buf, "        returns a new %s instance\n", msgName)
	fmt.Fprintf(g.buf, "        \"\"\"\n")
	// an empty YAML document loads as None, and a JSON "null" likewise, both of which are ordinary
	// inputs for a config-shaped message. treat them as "everything defaulted"
	fmt.Fprintf(g.buf, "        data = data or {}\n")

	if len(fields) == 0 {
		fmt.Fprintf(g.buf, "        return cls()\n\n")
		return nil
	}

	fmt.Fprintf(g.buf, "        return cls(\n")
	for i, field := range fields {
		value, err := g.pythonDecodeExpr(field)
		if err != nil {
			return err
		}
		fmt.Fprintf(g.buf, "            %s=%s,\n", names[i], value)
	}
	fmt.Fprintf(g.buf, "        )\n\n")

	return nil
}

// pythonMapKeyExpr returns the expression normalizing a decoded map key back to the map's key
// type. JSON object keys are always strings, so an integer-keyed map read back from JSON would
// otherwise come out keyed by str -- not the declared type, and not equal to the key it went in
// under. YAML keeps the integer, so the conversion has to cope with either
//
// field: the map field
// key: the expression holding the decoded key
//
// the key expression, coerced when the declared key type is not a string
func pythonMapKeyExpr(field *FieldInfo, key string) string {
	if field.MapKeyType == FieldTypeString {
		return key
	}
	return fmt.Sprintf("int(%s)", key)
}

// pythonDecodeExpr returns the expression rebuilding one field from the dict passed to from_dict
//
// field: the field to decode
//
// expr: the decoding expression
// err: unresolvable message reference
func (g *PythonGenerator) pythonDecodeExpr(field *FieldInfo) (string, error) {
	key := fmt.Sprintf("data.get(%q)", field.ProtoName)

	if field.IsMap {
		mapKey := pythonMapKeyExpr(field, "key")
		if field.MapValueType != FieldTypeMessage {
			return fmt.Sprintf("{%s: value for key, value in (%s or {}).items()}", mapKey, key), nil
		}
		name, err := g.messageName(field.MessageType)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("{%s: %s.from_dict(value) for key, value in (%s or {}).items()}", mapKey, name, key), nil
	}

	if field.IsRepeated {
		if field.Type != FieldTypeMessage {
			return fmt.Sprintf("list(%s or [])", key), nil
		}
		name, err := g.messageName(field.MessageType)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("[%s.from_dict(item) for item in (%s or [])]", name, key), nil
	}

	if field.Type == FieldTypeMessage {
		name, err := g.messageName(field.MessageType)
		if err != nil {
			return "", err
		}
		// an absent optional message stays None; an absent required one takes its zero value, the
		// same way the Go and C++ structs start out
		if field.IsOptional {
			return fmt.Sprintf("%s.from_dict(%s) if %s is not None else None", name, key, key), nil
		}
		return fmt.Sprintf("%s.from_dict(%s) if %s is not None else %s()", name, key, key, name), nil
	}

	if field.IsOptional {
		return key, nil
	}
	// a key that is present but explicitly null is not the same as an absent one: data.get returns
	// the null, which would land on a field annotated int or str. an empty YAML value ("age:") is
	// exactly this, so both cases take the zero value, the way every other generator does
	return fmt.Sprintf("%s if %s is not None else %s", key, key, GetZeroValue(field.Type, "python")), nil
}

// writePythonToJSON writes the to_json function for a dataclass. converts the dataclass to JSON and returns it as a string. function signature: def to_json(self) -> str:
//
// msgName: the generated class name
func (g *PythonGenerator) writePythonToJSON(msgName string) {
	fmt.Fprintf(g.buf, "    def to_json(self) -> str:\n")
	fmt.Fprintf(g.buf, "        \"\"\"serialize to JSON string.\n")
	fmt.Fprintf(g.buf, "        returns the JSON string representation of this %s.\n", msgName)
	fmt.Fprintf(g.buf, "        raises TypeError if serialization fails\n")
	fmt.Fprintf(g.buf, "        \"\"\"\n")
	fmt.Fprintf(g.buf, "        return json.dumps(self.to_dict())\n\n")
}

// writePythonFromJSON writes the from_json classmethod for a dataclass. creates a new instance from JSON data. function signature: @classmethod def from_json(cls, data: str) -> 'ClassName':
//
// msgName: the generated class name
func (g *PythonGenerator) writePythonFromJSON(msgName string) {
	fmt.Fprintf(g.buf, "    @classmethod\n")
	fmt.Fprintf(g.buf, "    def from_json(cls, data: str) -> '%s':\n", msgName)
	fmt.Fprintf(g.buf, "        \"\"\"deserialize from JSON string.\n")
	fmt.Fprintf(g.buf, "        takes a JSON string and returns a new %s instance.\n", msgName)
	fmt.Fprintf(g.buf, "        raises json.JSONDecodeError if the JSON is invalid\n")
	fmt.Fprintf(g.buf, "        \"\"\"\n")
	// json.loads is typed Any, which would satisfy from_dict without any check at all. binding
	// through the same annotated local from_yaml uses declares what a document may actually be
	fmt.Fprintf(g.buf, "        parsed: Dict[str, Any] | None = json.loads(data)\n")
	fmt.Fprintf(g.buf, "        return cls.from_dict(parsed)\n\n")
}

// writePythonToYAML writes the to_yaml function for a dataclass. converts the dataclass to YAML and returns it as a string. function signature: def to_yaml(self) -> str:
//
// msgName: the generated class name
func (g *PythonGenerator) writePythonToYAML(msgName string) {
	fmt.Fprintf(g.buf, "    def to_yaml(self) -> str:\n")
	fmt.Fprintf(g.buf, "        \"\"\"serialize to YAML string.\n")
	fmt.Fprintf(g.buf, "        returns the YAML string representation of this %s.\n", msgName)
	fmt.Fprintf(g.buf, "        raises yaml.YAMLError if serialization fails\n")
	fmt.Fprintf(g.buf, "        \"\"\"\n")
	// safe_dump emits plain YAML rather than the !!python/object tags yaml.dump would write for a
	// dataclass, which safe_load in from_yaml refuses to read back.
	//
	// PyYAML ships no py.typed marker, so a type checker resolves safe_dump to Unknown and reports
	// returning it directly as unsound. binding through an annotated local declares the str the
	// stream=None form always returns
	fmt.Fprintf(g.buf, "        result: str = yaml.safe_dump(self.to_dict())\n")
	fmt.Fprintf(g.buf, "        return result\n\n")
}

// writePythonFromYAML writes the from_yaml classmethod for a dataclass. creates a new instance from YAML data. function signature: @classmethod def from_yaml(cls, data: str) -> 'ClassName':
//
// msgName: the generated class name
func (g *PythonGenerator) writePythonFromYAML(msgName string) {
	fmt.Fprintf(g.buf, "    @classmethod\n")
	fmt.Fprintf(g.buf, "    def from_yaml(cls, data: str) -> '%s':\n", msgName)
	fmt.Fprintf(g.buf, "        \"\"\"deserialize from YAML string.\n")
	fmt.Fprintf(g.buf, "        takes a YAML string and returns a new %s instance.\n", msgName)
	fmt.Fprintf(g.buf, "        raises yaml.YAMLError if the YAML is invalid\n")
	fmt.Fprintf(g.buf, "        \"\"\"\n")
	// safe_load is Unknown for the same reason safe_dump is, and passing it straight to from_dict
	// would satisfy the parameter without any check at all. the None is part of the declared type
	// rather than a lie a checker would go on to trust: an empty document loads as one
	fmt.Fprintf(g.buf, "        parsed: Dict[str, Any] | None = yaml.safe_load(data)\n")
	fmt.Fprintf(g.buf, "        return cls.from_dict(parsed)\n\n")
}
