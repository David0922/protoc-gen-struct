package language

import (
	"bytes"
	"fmt"

	"google.golang.org/protobuf/types/descriptorpb"
)

// TypescriptGenerator generates TypeScript code from proto files
// it produces interface definitions with JSON/YAML serialization helpers
type TypescriptGenerator struct {
	registry *TypeRegistry
	buf      *bytes.Buffer

	// per-file state, reset at the start of every Generate call
	file    *descriptorpb.FileDescriptorProto
	aliases map[string]string // proto file name -> the alias its module is imported under
}

// NewTypescriptGenerator creates a new TypeScript code generator. takes a TypeRegistry for message type resolution
//
// registry: TypeRegistry for type resolution
//
// configured TypescriptGenerator ready to generate TypeScript code
func NewTypescriptGenerator(registry *TypeRegistry) Generator {
	return &TypescriptGenerator{
		registry: registry,
		buf:      &bytes.Buffer{},
	}
}

// Generate generates TypeScript code for a single proto file. produces interface definitions with JSON/YAML serialization functions
//
// file: the FileDescriptorProto to generate code for
//
// code: the generated TypeScript code
// err: generation errors or nil
func (g *TypescriptGenerator) Generate(file *descriptorpb.FileDescriptorProto) (string, error) {
	g.buf.Reset()
	g.file = file

	messages := CollectMessages(file)

	externals, err := ExternalFiles(messages, file, g.registry)
	if err != nil {
		return "", err
	}

	reserved := ReservedSet("typescript")
	for _, msg := range messages {
		reserved[g.localName(msg)] = true
	}
	g.aliases = AssignAliases(externals, reserved, func(f *descriptorpb.FileDescriptorProto) string {
		return GetFileBaseName(f.GetName())
	})

	g.writeTypescriptImports(externals)

	for _, msg := range messages {
		if err := g.generateTypescriptMessage(msg); err != nil {
			return "", err
		}
	}

	return g.buf.String(), nil
}

// writeTypescriptImports writes the import block for TypeScript. includes the yaml module plus
// one module import per proto file whose types are referenced. a generated module resolves its
// imports relative to its own directory, so the path is expressed relative to this file
//
// externals: the referenced foreign files
func (g *TypescriptGenerator) writeTypescriptImports(externals []*descriptorpb.FileDescriptorProto) {
	fmt.Fprint(g.buf, "import * as yaml from 'yaml';\n")

	if len(externals) > 0 {
		fmt.Fprint(g.buf, "\n")
		for _, ext := range externals {
			relative := RelativeImportPath(g.file.GetName(), ProtoFileStem(ext.GetName()))
			fmt.Fprintf(g.buf, "import * as %s from '%s';\n", g.aliases[ext.GetName()], relative)
		}
	}

	fmt.Fprint(g.buf, "\n")
}

// generateTypescriptMessage generates a TypeScript interface and its serialization functions for a message. handles nested messages and generates JSON/YAML serialization helpers
//
// msg: the message to generate code for
//
// err: generation errors or nil
func (g *TypescriptGenerator) generateTypescriptMessage(msg *MessageRef) error {
	fields, err := GetMessageFields(msg.Descriptor, g.registry)
	if err != nil {
		return err
	}

	typeName := g.localName(msg)

	// write interface definition. the generated file is a module (it imports yaml), so the
	// interface and its helpers must be exported to be usable by consumers
	fmt.Fprintf(g.buf, "// %s is a generated interface from proto message %s\n", typeName, msg.FullName)
	fmt.Fprintf(g.buf, "export interface %s {\n", typeName)

	for _, field := range fields {
		tsType, err := g.getTypescriptType(field)
		if err != nil {
			return err
		}

		// a field declared `optional` in proto3 becomes an optional property, so it may be left
		// off an object literal and is absent (rather than zero-valued) after a round trip
		optional := ""
		if field.IsOptional {
			optional = "?"
		}

		// properties are named after the proto field, which is also the key the C++, Go and Python
		// generators serialize under. camelCasing them here would leave every field undefined when
		// reading JSON or YAML that any of the other three wrote, since neither toJSON nor
		// fromJSON translates keys. a property may be a reserved word, so no escaping is needed
		fmt.Fprintf(g.buf, "  %s%s: %s;\n", field.ProtoName, optional, tsType)
	}

	fmt.Fprint(g.buf, "}\n\n")

	// write helper namespace with serialization functions
	fmt.Fprintf(g.buf, "export namespace %s {\n", typeName)

	// ToJSON function
	g.writeTypescriptToJSON(typeName)

	// FromJSON function
	g.writeTypescriptFromJSON(typeName)

	// ToYAML function
	g.writeTypescriptToYAML(typeName)

	// FromYAML function
	g.writeTypescriptFromYAML(typeName)

	// fromObject, which the two decoders and any nested message go through
	if err := g.writeTypescriptFromObject(typeName, fields); err != nil {
		return err
	}

	fmt.Fprint(g.buf, "}\n\n")

	return nil
}

// localName returns the name a message is declared under in this module, escaped so it cannot be
// a TypeScript reserved word
//
// msg: the message to name
//
// the interface name
func (g *TypescriptGenerator) localName(msg *MessageRef) string {
	return EscapeIdentifier(msg.TypeName, "typescript")
}

// writeTypescriptFromObject writes the fromObject function for an interface. parsing JSON or YAML
// yields whatever the document happened to contain, so casting it to the interface asserts every
// property is present without making it so -- reading `{}` as a Library left `books` undefined and
// threw on first use. this rebuilds the object property by property, defaulting anything missing
// to the same zero value the other three generators produce, and rebuilding nested messages
// through their own fromObject
//
// msgName: the generated type name
// fields: the message fields
//
// err: unresolvable message reference
func (g *TypescriptGenerator) writeTypescriptFromObject(msgName string, fields []*FieldInfo) error {
	fmt.Fprintf(g.buf, "  export function fromObject(data: unknown): %s {\n", msgName)
	fmt.Fprintf(g.buf, "    const source = (data ?? {}) as Record<string, unknown>;\n")
	fmt.Fprintf(g.buf, "    const result: %s = {\n", msgName)

	for _, field := range fields {
		if field.IsOptional {
			continue
		}
		value, err := g.typescriptDecodeExpr(field)
		if err != nil {
			return err
		}
		fmt.Fprintf(g.buf, "      %s: %s,\n", field.ProtoName, value)
	}

	fmt.Fprintf(g.buf, "    };\n")

	// an optional property is assigned only when it is actually present, so that "absent" survives
	// the round trip rather than becoming an explicit undefined
	for _, field := range fields {
		if !field.IsOptional {
			continue
		}
		tsType, err := g.getTypescriptType(field)
		if err != nil {
			return err
		}
		fmt.Fprintf(g.buf, "    if (source[%q] !== undefined && source[%q] !== null) {\n", field.ProtoName, field.ProtoName)
		if field.Type == FieldTypeMessage {
			name, err := g.messageName(field.MessageType)
			if err != nil {
				return err
			}
			fmt.Fprintf(g.buf, "      result.%s = %s.fromObject(source[%q]);\n", field.ProtoName, name, field.ProtoName)
		} else {
			fmt.Fprintf(g.buf, "      result.%s = source[%q] as %s;\n", field.ProtoName, field.ProtoName, tsType)
		}
		fmt.Fprintf(g.buf, "    }\n")
	}

	fmt.Fprintf(g.buf, "    return result;\n")
	fmt.Fprintf(g.buf, "  }\n\n")

	return nil
}

// typescriptDecodeExpr returns the expression rebuilding one non-optional field from the parsed
// document, falling back to the field's zero value when it is absent
//
// field: the field to decode
//
// expr: the decoding expression
// err: unresolvable message reference
func (g *TypescriptGenerator) typescriptDecodeExpr(field *FieldInfo) (string, error) {
	key := fmt.Sprintf("source[%q]", field.ProtoName)

	if field.IsMap {
		valueType, err := g.getTypescriptValueType(field.MapValueType, field.MessageType)
		if err != nil {
			return "", err
		}
		mapType, err := g.getTypescriptType(field)
		if err != nil {
			return "", err
		}

		// a string-keyed map of primitives is already in its final shape, so it is copied wholesale
		if field.MapKeyType == FieldTypeString && field.MapValueType != FieldTypeMessage {
			return fmt.Sprintf("{ ...((%s ?? {}) as %s) }", key, mapType), nil
		}

		// JSON and YAML both hand back object keys as strings, so an integer-keyed map is parsed
		// back to the declared key type, the way the other three generators do. a JavaScript
		// object key is a string whatever it is assigned as, so this normalizes the key rather
		// than making it a number at runtime -- what it buys is that the value written under "007"
		// or "1e3" lands where indexing by 7 or 1000 finds it
		keyType := GetTypeName(field.MapKeyType, "typescript")
		keyExpr := "key"
		if field.MapKeyType != FieldTypeString {
			keyExpr = "Number(key)"
		}

		valueExpr := fmt.Sprintf("value as %s", valueType)
		if field.MapValueType == FieldTypeMessage {
			name, err := g.messageName(field.MessageType)
			if err != nil {
				return "", err
			}
			valueExpr = fmt.Sprintf("%s.fromObject(value)", name)
		}

		return fmt.Sprintf(
			"Object.fromEntries(Object.entries((%s ?? {}) as Record<string, unknown>).map(([key, value]) => [%s, %s] as [%s, %s])) as %s",
			key, keyExpr, valueExpr, keyType, valueType, mapType), nil
	}

	if field.IsRepeated {
		elementType, err := g.getTypescriptValueType(field.Type, field.MessageType)
		if err != nil {
			return "", err
		}
		if field.Type != FieldTypeMessage {
			return fmt.Sprintf("((%s ?? []) as %s[])", key, elementType), nil
		}
		name, err := g.messageName(field.MessageType)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("((%s ?? []) as unknown[]).map((item) => %s.fromObject(item))", key, name), nil
	}

	if field.Type == FieldTypeMessage {
		name, err := g.messageName(field.MessageType)
		if err != nil {
			return "", err
		}
		// fromObject defaults a missing object to a fully zero-valued instance
		return fmt.Sprintf("%s.fromObject(%s)", name, key), nil
	}

	tsType, err := g.getTypescriptType(field)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("((%s ?? %s) as %s)", key, GetZeroValue(field.Type, "typescript"), tsType), nil
}

// messageName returns the expression naming a message type, which is also the namespace its
// fromObject is called on
//
// messageType: fully qualified message type name
//
// name: the type expression, e.g. "Outer_Inner" or "types.Address"
// err: unresolvable message reference
func (g *TypescriptGenerator) messageName(messageType string) (string, error) {
	ref, err := g.registry.Resolve(messageType)
	if err != nil {
		return "", err
	}
	if ref.File.GetName() == g.file.GetName() {
		return g.localName(ref), nil
	}
	return g.aliases[ref.File.GetName()] + "." + g.localName(ref), nil
}

// getTypescriptType returns the TypeScript type for a FieldInfo. handles primitives, message types, arrays, and objects
//
// field: the FieldInfo to get the TypeScript type for
//
// tsType: TypeScript type as a string
// err: unresolvable message reference
func (g *TypescriptGenerator) getTypescriptType(field *FieldInfo) (string, error) {
	if field.IsMap {
		valueType, err := g.getTypescriptValueType(field.MapValueType, field.MessageType)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Record<%s, %s>", GetTypeName(field.MapKeyType, "typescript"), valueType), nil
	}

	if field.IsRepeated {
		baseType, err := g.getTypescriptValueType(field.Type, field.MessageType)
		if err != nil {
			return "", err
		}
		return baseType + "[]", nil
	}

	return g.getTypescriptValueType(field.Type, field.MessageType)
}

// getTypescriptValueType returns the TypeScript type for a single value. a message defined in
// another proto file is qualified with the alias its module is imported under
//
// ft: the FieldType
// messageType: fully qualified message type name (empty for primitives)
//
// name: TypeScript type name
// err: unresolvable message reference
func (g *TypescriptGenerator) getTypescriptValueType(ft FieldType, messageType string) (string, error) {
	if ft != FieldTypeMessage || messageType == "" {
		return GetTypeName(ft, "typescript"), nil
	}

	return g.messageName(messageType)
}

// writeTypescriptToJSON writes the toJSON serialization function for an interface. converts the object to JSON and returns it as a string. function signature: export function toJSON(obj: ClassName): string
//
// msgName: the generated type name
func (g *TypescriptGenerator) writeTypescriptToJSON(msgName string) {
	fmt.Fprintf(g.buf, "  export function toJSON(obj: %s): string {\n", msgName)
	fmt.Fprintf(g.buf, "    return JSON.stringify(obj);\n")
	fmt.Fprintf(g.buf, "  }\n\n")
}

// writeTypescriptFromJSON writes the fromJSON deserialization function for an interface. creates a new object from JSON data. function signature: export function fromJSON(data: string): ClassName
//
// msgName: the generated type name
func (g *TypescriptGenerator) writeTypescriptFromJSON(msgName string) {
	fmt.Fprintf(g.buf, "  export function fromJSON(data: string): %s {\n", msgName)
	fmt.Fprintf(g.buf, "    return fromObject(JSON.parse(data));\n")
	fmt.Fprintf(g.buf, "  }\n\n")
}

// writeTypescriptToYAML writes the toYAML serialization function for an interface. converts the object to YAML and returns it as a string. function signature: export function toYAML(obj: ClassName): string
//
// msgName: the generated type name
func (g *TypescriptGenerator) writeTypescriptToYAML(msgName string) {
	fmt.Fprintf(g.buf, "  export function toYAML(obj: %s): string {\n", msgName)
	fmt.Fprintf(g.buf, "    return yaml.stringify(obj);\n")
	fmt.Fprintf(g.buf, "  }\n\n")
}

// writeTypescriptFromYAML writes the fromYAML deserialization function for an interface. creates a new object from YAML data. function signature: export function fromYAML(data: string): ClassName
//
// msgName: the generated type name
func (g *TypescriptGenerator) writeTypescriptFromYAML(msgName string) {
	fmt.Fprintf(g.buf, "  export function fromYAML(data: string): %s {\n", msgName)
	fmt.Fprintf(g.buf, "    return fromObject(yaml.parse(data));\n")
	fmt.Fprintf(g.buf, "  }\n\n")
}
