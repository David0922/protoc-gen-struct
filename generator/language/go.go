package language

import (
	"bytes"
	"fmt"
	"path"
	"strings"

	"google.golang.org/protobuf/types/descriptorpb"
)

// GoGenerator generates Go code from proto files
// it produces struct definitions with JSON/YAML serialization helpers
type GoGenerator struct {
	registry *TypeRegistry
	buf      *bytes.Buffer

	// per-file state, reset at the start of every Generate call
	file    *descriptorpb.FileDescriptorProto
	aliases map[string]string // proto file name -> the alias its package is imported under
	// proto file names whose generated code lands in the same Go package as this file's, so
	// their types are named unqualified and no import is emitted for them
	samePackage map[string]bool
}

// NewGoGenerator creates a new Go code generator. takes a TypeRegistry for message type resolution
//
// registry: TypeRegistry for type resolution
//
// configured GoGenerator ready to generate Go code
func NewGoGenerator(registry *TypeRegistry) Generator {
	return &GoGenerator{
		registry: registry,
		buf:      &bytes.Buffer{},
	}
}

// Generate generates Go code for a single proto file. produces struct definitions with JSON/YAML tags and serialization functions
//
// file: the FileDescriptorProto to generate code for
//
// code: the generated Go code
// err: generation errors or nil
func (g *GoGenerator) Generate(file *descriptorpb.FileDescriptorProto) (string, error) {
	g.buf.Reset()
	g.file = file

	messages := CollectMessages(file)

	externals, err := g.resolveImports(messages)
	if err != nil {
		return "", err
	}

	// write package declaration. the go_package option names the package this file's code belongs
	// to; without one, fall back to the proto package, which is dotted and so has to be sanitized
	// into a legal Go identifier
	pkg := goPackageName(file)
	if pkg == "" {
		pkg = "main"
	}
	fmt.Fprintf(g.buf, "package %s\n\n", pkg)

	g.writeGoImports(messages, externals)

	for _, msg := range messages {
		if err := g.generateGoMessage(msg); err != nil {
			return "", err
		}
	}

	return g.buf.String(), nil
}

// resolveImports finds the proto files, other than the one being generated, whose types this
// file refers to, and assigns each one the alias its package will be imported under. a Go import
// needs a module path, and go_package is the only place a descriptor carries one, so a file that
// omits the option cannot be imported and is reported rather than silently referenced
//
// several protos routinely share one go_package -- every .proto in a directory usually does --
// and their generated code is then one Go package. such a file is not imported at all: emitting
// an import for it would make the generated file import the very package it declares, which does
// not compile. its types are named unqualified instead
//
// messages: the messages being generated
//
// imports: the foreign files that need an import, sorted by proto file name
// err: an unresolvable reference, or a referenced file with no go_package option
func (g *GoGenerator) resolveImports(messages []*MessageRef) ([]*descriptorpb.FileDescriptorProto, error) {
	externals, err := ExternalFiles(messages, g.file, g.registry)
	if err != nil {
		return nil, err
	}

	currentPath, _ := goPackageOption(g.file)

	g.samePackage = make(map[string]bool, len(externals))
	imports := make([]*descriptorpb.FileDescriptorProto, 0, len(externals))
	for _, ext := range externals {
		importPath, _ := goPackageOption(ext)
		if importPath == "" {
			return nil, fmt.Errorf(
				"cannot generate Go for %s: it references types from %s, which declares no go_package option to import it by",
				g.file.GetName(), ext.GetName())
		}
		// only an explicit, matching go_package proves the two land in one package: without the
		// option the package name is guessed from the proto package, which two files in different
		// directories can share while their generated code cannot
		if currentPath != "" && importPath == currentPath {
			g.samePackage[ext.GetName()] = true
			continue
		}
		imports = append(imports, ext)
	}

	// aliases must not shadow the serialization packages, land on a keyword, or collide with a
	// type declared here
	reserved := ReservedSet("go")
	for _, msg := range messages {
		reserved[EscapeIdentifier(msg.TypeName, "go")] = true
	}
	g.aliases = AssignAliases(imports, reserved, goPackageName)

	return imports, nil
}

// writeGoImports writes the import block for Go. includes the serialization packages plus one
// import per proto file whose types this file references
//
// messages: the messages being generated
// externals: the referenced foreign files
func (g *GoGenerator) writeGoImports(messages []*MessageRef, externals []*descriptorpb.FileDescriptorProto) {
	// only emit imports when at least one struct is actually generated, otherwise the generated
	// file would not compile due to unused imports
	if len(messages) == 0 {
		return
	}

	// always include serialization imports
	// all generated messages have serialization methods
	fmt.Fprint(g.buf, "import (\n")
	fmt.Fprint(g.buf, "\t\"encoding/json\"\n")
	fmt.Fprint(g.buf, "\n")
	fmt.Fprint(g.buf, "\t\"go.yaml.in/yaml/v4\"\n")

	if len(externals) > 0 {
		fmt.Fprint(g.buf, "\n")
		for _, ext := range externals {
			importPath, _ := goPackageOption(ext)
			fmt.Fprintf(g.buf, "\t%s %q\n", g.aliases[ext.GetName()], importPath)
		}
	}

	fmt.Fprint(g.buf, ")\n\n")
}

// generateGoMessage generates a Go struct and its serialization functions for a message. handles nested messages and generates JSON/YAML serialization helpers
//
// msg: the message to generate code for
//
// err: generation errors or nil
func (g *GoGenerator) generateGoMessage(msg *MessageRef) error {
	fields, err := GetMessageFields(msg.Descriptor, g.registry)
	if err != nil {
		return err
	}

	typeName := EscapeIdentifier(msg.TypeName, "go")

	// two proto names can title-case to one Go name -- "user_id" and "userId" both give "UserId"
	// -- and emitting both would declare the field twice
	names := make([]string, len(fields))
	for i, field := range fields {
		names[i] = EscapeIdentifier(SnakeToTitleCase(field.Name), "go")
	}
	names = UniqueNames(names)

	// write struct definition
	fmt.Fprintf(g.buf, "// %s is a generated struct from proto message %s\n", typeName, msg.FullName)
	fmt.Fprintf(g.buf, "type %s struct {\n", typeName)

	for i, field := range fields {
		goType, err := g.getGoType(field)
		if err != nil {
			return err
		}
		fmt.Fprintf(g.buf, "\t%s %s `%s`\n", names[i], goType, g.getGoTag(field))
	}

	fmt.Fprint(g.buf, "}\n\n")

	// write ToJSON function
	g.writeGoToJSON(typeName)

	// write FromJSON function
	g.writeGoFromJSON(typeName)

	// write ToYAML function
	g.writeGoToYAML(typeName)

	// write FromYAML function
	g.writeGoFromYAML(typeName)

	return nil
}

// getGoTag returns the struct tag for a FieldInfo. fields declared `optional` in proto3 are
// generated as pointers and tagged omitempty, so a nil pointer is left out of the encoded
// JSON/YAML while a pointer to the zero value is still written
//
// repeated fields and maps are tagged omitempty too, for a different reason: a nil slice or map
// marshals to JSON null, which the C++ reader rejects outright when it tries to decode the value.
// proto3 gives them no presence -- empty and absent mean the same thing -- so omitting an empty
// one is both safe and what canonical protobuf JSON does
//
// field: the FieldInfo to build the tag for
//
// the struct tag body, without the surrounding backquotes
func (g *GoGenerator) getGoTag(field *FieldInfo) string {
	if field.IsOptional || field.IsRepeated || field.IsMap {
		return fmt.Sprintf(`json:"%s,omitempty" yaml:"%s,omitempty"`, field.ProtoName, field.ProtoName)
	}
	return fmt.Sprintf(`json:"%s" yaml:"%s"`, field.ProtoName, field.ProtoName)
}

// getGoType returns the Go type for a FieldInfo. a field declared `optional` in proto3 becomes a
// pointer: omitempty alone cannot express presence, since it treats a field set to its zero value
// exactly like an unset one, and for a struct field it does nothing at all
//
// field: the FieldInfo to get the Go type for
//
// goType: Go type as a string
// err: unresolvable message reference
func (g *GoGenerator) getGoType(field *FieldInfo) (string, error) {
	base, err := g.getGoBaseType(field)
	if err != nil {
		return "", err
	}
	if field.IsOptional {
		return "*" + base, nil
	}
	return base, nil
}

// getGoBaseType returns the Go type a FieldInfo holds, ignoring optionality. handles primitives,
// message types, slices, and maps
//
// field: the FieldInfo to get the Go type for
//
// goType: Go type as a string
// err: unresolvable message reference
func (g *GoGenerator) getGoBaseType(field *FieldInfo) (string, error) {
	if field.IsMap {
		valueType, err := g.getValueType(field.MapValueType, field.MessageType)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("map[%s]%s", GetTypeName(field.MapKeyType, "go"), valueType), nil
	}

	if field.IsRepeated {
		baseType, err := g.getValueType(field.Type, field.MessageType)
		if err != nil {
			return "", err
		}
		return "[]" + baseType, nil
	}

	return g.getValueType(field.Type, field.MessageType)
}

// getValueType returns the Go type for a single value. a message defined in another proto file is
// qualified with the alias its package is imported under, since the generated code for that file
// is a separate Go package
//
// ft: the FieldType
// messageType: fully qualified message type name (empty for primitives)
//
// name: Go type name
// err: unresolvable message reference
func (g *GoGenerator) getValueType(ft FieldType, messageType string) (string, error) {
	if ft != FieldTypeMessage || messageType == "" {
		return GetTypeName(ft, "go"), nil
	}

	ref, err := g.registry.Resolve(messageType)
	if err != nil {
		return "", err
	}
	name := EscapeIdentifier(ref.TypeName, "go")
	if ref.File.GetName() == g.file.GetName() || g.samePackage[ref.File.GetName()] {
		return name, nil
	}
	return g.aliases[ref.File.GetName()] + "." + name, nil
}

// goPackageOption splits a file's go_package option into the module path to import it by and the
// package name to refer to it by. the option may name the package explicitly after a semicolon
// ("example.com/gen/user;userpb"); otherwise the last path segment is the package name
//
// f: the proto file descriptor
//
// importPath: the Go import path, or "" when the option is absent
// pkgName: the Go package name, or "" when the option is absent
func goPackageOption(f *descriptorpb.FileDescriptorProto) (string, string) {
	option := f.GetOptions().GetGoPackage()
	if option == "" {
		return "", ""
	}

	if importPath, name, found := strings.Cut(option, ";"); found {
		return importPath, name
	}
	return option, path.Base(option)
}

// goPackageName returns the Go package name for a proto file. the go_package option wins when
// present; otherwise the proto package is sanitized into a legal Go identifier
//
// f: the proto file descriptor
//
// the Go package name, or "" when the file declares neither
func goPackageName(f *descriptorpb.FileDescriptorProto) string {
	if _, name := goPackageOption(f); name != "" {
		return SanitizePackageName(name)
	}
	return SanitizePackageName(GetPackageName(f))
}

// writeGoToJSON writes the ToJSON serialization function for a struct. marshals the struct to JSON and returns the result as a byte slice. function signature: func (x *TypeName) ToJSON() ([]byte, error)
//
// msgName: the generated type name
func (g *GoGenerator) writeGoToJSON(msgName string) {
	fmt.Fprintf(g.buf, "// ToJSON serializes %s to JSON.\n", msgName)
	fmt.Fprintf(g.buf, "// it converts the struct to a JSON byte slice.\n")
	fmt.Fprintf(g.buf, "// returns the JSON bytes and any marshaling error\n")
	fmt.Fprintf(g.buf, "func (x *%s) ToJSON() ([]byte, error) {\n", msgName)
	fmt.Fprintf(g.buf, "\treturn json.Marshal(x)\n")
	fmt.Fprintf(g.buf, "}\n\n")
}

// writeGoFromJSON writes the FromJSON deserialization function for a struct. unmarshals JSON data into the struct. function signature: func (x *TypeName) FromJSON(data []byte) error
//
// the receiver is zeroed first, so decoding is a full load rather than a patch. encoding/json
// leaves a field the document omits untouched and merges into an existing map, so without this a
// second decode into the same value would keep the first one's scalars and accumulate map entries
// -- while the C++, Python and TypeScript decoders all rebuild the whole message
//
// msgName: the generated type name
func (g *GoGenerator) writeGoFromJSON(msgName string) {
	fmt.Fprintf(g.buf, "// FromJSON deserializes %s from JSON.\n", msgName)
	fmt.Fprintf(g.buf, "// it unmarshals the JSON byte slice into the struct, replacing any value it already held.\n")
	fmt.Fprintf(g.buf, "// returns an error if unmarshaling fails\n")
	fmt.Fprintf(g.buf, "func (x *%s) FromJSON(data []byte) error {\n", msgName)
	fmt.Fprintf(g.buf, "\t*x = %s{}\n", msgName)
	fmt.Fprintf(g.buf, "\treturn json.Unmarshal(data, x)\n")
	fmt.Fprintf(g.buf, "}\n\n")
}

// writeGoToYAML writes the ToYAML serialization function for a struct. marshals the struct to YAML and returns the result as a byte slice. function signature: func (x *TypeName) ToYAML() ([]byte, error)
//
// msgName: the generated type name
func (g *GoGenerator) writeGoToYAML(msgName string) {
	fmt.Fprintf(g.buf, "// ToYAML serializes %s to YAML.\n", msgName)
	fmt.Fprintf(g.buf, "// it converts the struct to a YAML byte slice.\n")
	fmt.Fprintf(g.buf, "// returns the YAML bytes and any marshaling error\n")
	fmt.Fprintf(g.buf, "func (x *%s) ToYAML() ([]byte, error) {\n", msgName)
	fmt.Fprintf(g.buf, "\treturn yaml.Marshal(x)\n")
	fmt.Fprintf(g.buf, "}\n\n")
}

// writeGoFromYAML writes the FromYAML deserialization function for a struct. unmarshals YAML data into the struct. function signature: func (x *TypeName) FromYAML(data []byte) error
//
// the receiver is zeroed first, for the same reason FromJSON does it: yaml.Unmarshal also leaves
// an omitted field alone, so decoding twice into one value would otherwise merge the two documents
//
// msgName: the generated type name
func (g *GoGenerator) writeGoFromYAML(msgName string) {
	fmt.Fprintf(g.buf, "// FromYAML deserializes %s from YAML.\n", msgName)
	fmt.Fprintf(g.buf, "// it unmarshals the YAML byte slice into the struct, replacing any value it already held.\n")
	fmt.Fprintf(g.buf, "// returns an error if unmarshaling fails\n")
	fmt.Fprintf(g.buf, "func (x *%s) FromYAML(data []byte) error {\n", msgName)
	fmt.Fprintf(g.buf, "\t*x = %s{}\n", msgName)
	fmt.Fprintf(g.buf, "\treturn yaml.Unmarshal(data, x)\n")
	fmt.Fprintf(g.buf, "}\n\n")
}
