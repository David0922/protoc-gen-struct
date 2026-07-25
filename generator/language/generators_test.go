package language

import (
	"slices"
	"strings"
	"testing"

	"protoc-gen-struct/testutil"

	"google.golang.org/protobuf/types/descriptorpb"
)

// TestGoGenerator tests Go code generation
// it validates that Go structs and serialization functions are correctly generated
func TestGoGenerator(t *testing.T) {
	file := testutil.NewFile(
		"test.proto",
		"testpkg",
		[]*descriptorpb.DescriptorProto{
			testutil.NewMessage(
				"Person",
				[]*descriptorpb.FieldDescriptorProto{
					testutil.NewStringField("first_name", 1, false),
					testutil.NewIntField("age", 2, false),
				},
			),
		},
	)

	registry := NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})
	gen := NewGoGenerator(registry)

	code, err := gen.Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// check that generated code contains expected elements
	if !strings.Contains(code, "package testpkg") {
		t.Error("Generated code missing 'package testpkg'")
	}

	if !strings.Contains(code, "type Person struct") {
		t.Error("Generated code missing 'type Person struct'")
	}

	if !strings.Contains(code, "FirstName string") {
		t.Error("Generated code missing 'FirstName string'")
	}

	if !strings.Contains(code, "Age int32") {
		t.Error("Generated code missing 'Age int32'")
	}

	if !strings.Contains(code, "func (x *Person) ToJSON()") {
		t.Error("Generated code missing ToJSON function")
	}

	if !strings.Contains(code, `json:"first_name"`) {
		t.Error("Generated code missing JSON tag")
	}
}

// TestCppGenerator tests C++ code generation
func TestCppGenerator(t *testing.T) {
	file := testutil.NewFile(
		"test.proto",
		"testpkg",
		[]*descriptorpb.DescriptorProto{
			testutil.NewMessage(
				"Person",
				[]*descriptorpb.FieldDescriptorProto{
					testutil.NewStringField("first_name", 1, false),
					testutil.NewIntField("age", 2, false),
				},
			),
		},
	)

	registry := NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})
	gen := NewCppGenerator(registry)

	code, err := gen.Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// check that generated code contains expected elements. the guard carries a hash of the proto
	// path between the mangled name and the suffix, so only the two ends are asserted here
	if !strings.Contains(code, "#ifndef PROTO_TEST_") || !strings.Contains(code, "_H_") {
		t.Error("Generated code missing header guard")
	}

	if !strings.Contains(code, "namespace testpkg") {
		t.Error("Generated code missing 'namespace testpkg'")
	}

	if !strings.Contains(code, "struct Person") {
		t.Error("Generated code missing 'struct Person'")
	}

	if !strings.Contains(code, "std::string first_name;") {
		t.Error("Generated code missing std::string field")
	}

	if !strings.Contains(code, "nlohmann::json ToJSON()") {
		t.Error("Generated code missing ToJSON function")
	}
}

// TestPythonGenerator tests Python code generation
func TestPythonGenerator(t *testing.T) {
	file := testutil.NewFile(
		"test.proto",
		"testpkg",
		[]*descriptorpb.DescriptorProto{
			testutil.NewMessage(
				"Person",
				[]*descriptorpb.FieldDescriptorProto{
					testutil.NewStringField("first_name", 1, false),
					testutil.NewIntField("age", 2, false),
				},
			),
		},
	)

	registry := NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})
	gen := NewPythonGenerator(registry)

	code, err := gen.Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// check that generated code contains expected elements
	if !strings.Contains(code, "from dataclasses import dataclass") {
		t.Error("Generated code missing 'from dataclasses import dataclass'")
	}

	if !strings.Contains(code, "@dataclass") {
		t.Error("Generated code missing '@dataclass' decorator")
	}

	if !strings.Contains(code, "class Person:") {
		t.Error("Generated code missing 'class Person:'")
	}

	if !strings.Contains(code, "first_name: str") {
		t.Error("Generated code missing 'first_name: str' annotation")
	}
}

// TestTypescriptGenerator tests TypeScript code generation
func TestTypescriptGenerator(t *testing.T) {
	file := testutil.NewFile(
		"test.proto",
		"testpkg",
		[]*descriptorpb.DescriptorProto{
			testutil.NewMessage(
				"Person",
				[]*descriptorpb.FieldDescriptorProto{
					testutil.NewStringField("first_name", 1, false),
					testutil.NewIntField("age", 2, false),
				},
			),
		},
	)

	registry := NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})
	gen := NewTypescriptGenerator(registry)

	code, err := gen.Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// check that generated code contains expected elements
	if !strings.Contains(code, "interface Person") {
		t.Error("Generated code missing 'interface Person'")
	}

	// the property is named after the proto field, matching the key the other three generators
	// serialize under
	if !strings.Contains(code, "first_name: string;") {
		t.Error("Generated code missing 'first_name: string;' property")
	}

	if strings.Contains(code, "firstName") {
		t.Error("Generated code should use proto field names, found 'firstName'")
	}

	if !strings.Contains(code, "age: number;") {
		t.Error("Generated code missing 'age: number;' property")
	}

	if !strings.Contains(code, "namespace Person") {
		t.Error("Generated code missing 'namespace Person'")
	}

	if !strings.Contains(code, "export function toJSON(") {
		t.Error("Generated code missing toJSON function")
	}
}

// TestGeneratorWithNestedMessage tests that nested message types are handled correctly
func TestGeneratorWithNestedMessage(t *testing.T) {
	file := testutil.NewFile(
		"test.proto",
		"testpkg",
		[]*descriptorpb.DescriptorProto{
			testutil.NewMessage("Person", []*descriptorpb.FieldDescriptorProto{
				testutil.NewStringField("name", 1, false),
			}),
			testutil.NewMessage("Book", []*descriptorpb.FieldDescriptorProto{
				testutil.NewMessageField("author", 1, "testpkg.Person"),
			}),
		},
	)

	registry := NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})
	gen := NewGoGenerator(registry)

	code, err := gen.Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if !strings.Contains(code, "Author Person") {
		t.Error("Generated code missing nested message type 'Author Person'")
	}
}

// newOptionalFieldFile builds a file with one message holding both a plain and an optional field
// of the same type, so a test can tell the two code paths apart
//
// FileDescriptorProto with a Person message
func newOptionalFieldFile() *descriptorpb.FileDescriptorProto {
	return testutil.NewFile(
		"test.proto",
		"testpkg",
		[]*descriptorpb.DescriptorProto{
			testutil.NewMessage(
				"Person",
				[]*descriptorpb.FieldDescriptorProto{
					testutil.NewStringField("first_name", 1, false),
					testutil.Optional(testutil.NewStringField("nickname", 2, false)),
					testutil.Optional(testutil.NewIntField("age", 3, false)),
				},
			),
		},
	)
}

// TestGoGeneratorOptionalFields tests that proto3 optional fields get omitempty in Go, and that
// fields without the optional keyword do not
func TestGoGeneratorOptionalFields(t *testing.T) {
	file := newOptionalFieldFile()
	code, err := NewGoGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// a pointer is what actually carries presence: omitempty on a value field cannot tell a field
	// explicitly set to its zero value apart from an unset one
	if !strings.Contains(code, "Nickname *string `json:\"nickname,omitempty\" yaml:\"nickname,omitempty\"`") {
		t.Errorf("Generated code missing pointer and omitempty tags for optional string field:\n%s", code)
	}

	if !strings.Contains(code, "Age *int32 `json:\"age,omitempty\" yaml:\"age,omitempty\"`") {
		t.Errorf("Generated code missing pointer and omitempty tags for optional int field:\n%s", code)
	}

	if !strings.Contains(code, "FirstName string `json:\"first_name\" yaml:\"first_name\"`") {
		t.Errorf("Non-optional field should not get omitempty:\n%s", code)
	}
}

// TestCppGeneratorOptionalFields tests that proto3 optional fields become std::optional in C++
func TestCppGeneratorOptionalFields(t *testing.T) {
	file := newOptionalFieldFile()
	code, err := NewCppGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if !strings.Contains(code, "#include <optional>") {
		t.Error("Generated code missing <optional> include")
	}

	if !strings.Contains(code, "std::optional<std::string> nickname;") {
		t.Errorf("Generated code missing std::optional string member:\n%s", code)
	}

	if !strings.Contains(code, "std::optional<int32_t> age;") {
		t.Errorf("Generated code missing std::optional int member:\n%s", code)
	}

	if !strings.Contains(code, "std::string first_name;") {
		t.Errorf("Non-optional field should not be wrapped in std::optional:\n%s", code)
	}

	// an unset field must be skipped rather than emitted as null
	if !strings.Contains(code, "if (this->nickname.has_value()) {") {
		t.Errorf("Generated serializers missing has_value guard:\n%s", code)
	}

	// the yaml decoder has no std::optional converter, so it has to ask for the base type
	if !strings.Contains(code, "node[\"nickname\"].as<std::string>();") {
		t.Errorf("Generated FromYAML should decode the contained type:\n%s", code)
	}

	if strings.Contains(code, "as<std::optional<") {
		t.Errorf("Generated FromYAML should not decode into std::optional directly:\n%s", code)
	}
}

// TestPythonGeneratorOptionalFields tests that proto3 optional fields are annotated `X | None`
func TestPythonGeneratorOptionalFields(t *testing.T) {
	file := newOptionalFieldFile()
	code, err := NewPythonGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// an optional field defaults to None, so JSON written by another generator -- which leaves an
	// unset optional out entirely -- still constructs
	if !strings.Contains(code, "nickname: str | None = None") {
		t.Errorf("Generated code missing 'nickname: str | None = None' annotation:\n%s", code)
	}

	if !strings.Contains(code, "age: int | None = None") {
		t.Errorf("Generated code missing 'age: int | None = None' annotation:\n%s", code)
	}

	if !strings.Contains(code, "first_name: str = \"\"\n") {
		t.Errorf("Non-optional field should take its zero value, not None:\n%s", code)
	}
}

// TestPythonGeneratorNestedSerialization tests that a message-typed field is converted to and
// from a plain dict, rather than handed to json.dumps as a dataclass it cannot encode
func TestPythonGeneratorNestedSerialization(t *testing.T) {
	file := testutil.NewFile(
		"test.proto",
		"testpkg",
		[]*descriptorpb.DescriptorProto{
			testutil.NewMessage("Address", []*descriptorpb.FieldDescriptorProto{
				testutil.NewStringField("city", 1, false),
			}),
			testutil.NewMessage("Person", []*descriptorpb.FieldDescriptorProto{
				testutil.NewMessageField("address", 1, "testpkg.Address"),
				testutil.NewMessageField("tags", 2, "testpkg.Address"),
			}),
		},
	)
	file.MessageType[1].Field[1].Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()

	code, err := NewPythonGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if !strings.Contains(code, `data["address"] = self.address.to_dict()`) {
		t.Errorf("Generated to_dict should convert a nested message:\n%s", code)
	}

	if !strings.Contains(code, `data["tags"] = [item.to_dict() for item in self.tags]`) {
		t.Errorf("Generated to_dict should convert a repeated nested message:\n%s", code)
	}

	if !strings.Contains(code, "return json.dumps(self.to_dict())") {
		t.Errorf("Generated to_json should serialize the dict form:\n%s", code)
	}

	if strings.Contains(code, "self.__dict__") {
		t.Errorf("Generated code should not serialize __dict__, which cannot encode a nested message:\n%s", code)
	}

	// yaml.dump would write a !!python/object tag that the generated from_yaml refuses to read
	if !strings.Contains(code, "result: str = yaml.safe_dump(self.to_dict())") {
		t.Errorf("Generated to_yaml should use safe_dump:\n%s", code)
	}

	// PyYAML is unstubbed, so both yaml calls bind through an annotated local to keep the Unknown
	// they return from reaching a return statement or a from_dict argument undeclared
	if !strings.Contains(code, "parsed: Dict[str, Any] | None = yaml.safe_load(data)") {
		t.Errorf("Generated from_yaml should declare the type of the parsed document:\n%s", code)
	}

	if !strings.Contains(code, "Address.from_dict(data.get(\"address\"))") {
		t.Errorf("Generated from_dict should rebuild a nested message:\n%s", code)
	}
}

// TestPythonGeneratorEmptyMessage tests that a message with no fields still gets the full set of
// serialization helpers
func TestPythonGeneratorEmptyMessage(t *testing.T) {
	file := testutil.NewFile(
		"test.proto",
		"testpkg",
		[]*descriptorpb.DescriptorProto{
			testutil.NewMessage("Empty", nil),
		},
	)

	code, err := NewPythonGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	for _, method := range []string{"to_dict", "from_dict", "to_json", "from_json", "to_yaml", "from_yaml"} {
		if !strings.Contains(code, "def "+method+"(") {
			t.Errorf("Generated empty message missing %s:\n%s", method, code)
		}
	}
}

// TestTypescriptGeneratorOptionalFields tests that proto3 optional fields use `?` in the interface
func TestTypescriptGeneratorOptionalFields(t *testing.T) {
	file := newOptionalFieldFile()
	code, err := NewTypescriptGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if !strings.Contains(code, "nickname?: string;") {
		t.Errorf("Generated code missing 'nickname?: string;' property:\n%s", code)
	}

	if !strings.Contains(code, "age?: number;") {
		t.Errorf("Generated code missing 'age?: number;' property:\n%s", code)
	}

	if !strings.Contains(code, "first_name: string;") {
		t.Errorf("Non-optional field should not be marked optional:\n%s", code)
	}
}

// TestOptionalRepeatedAndMapUnaffected tests that repeated fields and maps are never treated as
// optional. proto3 forbids the optional keyword on them, and they already model absence as empty
func TestOptionalRepeatedAndMapUnaffected(t *testing.T) {
	file := testutil.NewFile(
		"test.proto",
		"testpkg",
		[]*descriptorpb.DescriptorProto{
			testutil.NewMessage("Person", []*descriptorpb.FieldDescriptorProto{
				testutil.NewStringField("tags", 1, true),
			}),
		},
	)

	registry := NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})
	fields, err := GetMessageFields(file.MessageType[0], registry)
	if err != nil {
		t.Fatalf("GetMessageFields() error = %v", err)
	}

	if fields[0].IsOptional {
		t.Error("Repeated field should not be marked optional")
	}

	code, err := NewTypescriptGenerator(registry).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if !strings.Contains(code, "tags: string[];") {
		t.Errorf("Repeated field should not be emitted as optional:\n%s", code)
	}
}

// newCollidingNestedFile builds a file with two messages that each nest a type of the same name,
// which is the case flattening by unqualified name cannot represent
//
// FileDescriptorProto with User.Inner and Other.Inner
func newCollidingNestedFile() *descriptorpb.FileDescriptorProto {
	return testutil.NewFile("test.proto", "testpkg", []*descriptorpb.DescriptorProto{
		testutil.WithNested(
			testutil.NewMessage("User", []*descriptorpb.FieldDescriptorProto{
				testutil.NewMessageField("inner", 1, "testpkg.User.Inner"),
			}),
			testutil.NewMessage("Inner", []*descriptorpb.FieldDescriptorProto{
				testutil.NewStringField("x", 1, false),
			}),
		),
		testutil.WithNested(
			testutil.NewMessage("Other", []*descriptorpb.FieldDescriptorProto{
				testutil.NewMessageField("inner", 1, "testpkg.Other.Inner"),
			}),
			testutil.NewMessage("Inner", []*descriptorpb.FieldDescriptorProto{
				testutil.NewIntField("y", 1, false),
			}),
		),
	})
}

// TestNestedMessageNamesAreQualified tests that two nested messages sharing a name get distinct
// generated types. flattening them into file scope by their unqualified name declares the same
// type twice, which does not compile in Go, C++ or TypeScript and silently redefines the class
// in Python
func TestNestedMessageNamesAreQualified(t *testing.T) {
	file := newCollidingNestedFile()
	registry := NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})

	tests := []struct {
		language  string
		generator Generator
		declares  []string
	}{
		{"go", NewGoGenerator(registry), []string{"type User_Inner struct", "type Other_Inner struct"}},
		{"c++", NewCppGenerator(registry), []string{"struct User_Inner {", "struct Other_Inner {"}},
		{"python", NewPythonGenerator(registry), []string{"class User_Inner:", "class Other_Inner:"}},
		{"typescript", NewTypescriptGenerator(registry), []string{"interface User_Inner {", "interface Other_Inner {"}},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			code, err := tt.generator.Generate(file)
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			for _, declaration := range tt.declares {
				if strings.Count(code, declaration) != 1 {
					t.Errorf("want exactly one %q, got %d:\n%s", declaration, strings.Count(code, declaration), code)
				}
			}

			// the parents must refer to their own nested type, not to a shared bare "Inner"
			if !strings.Contains(code, "User_Inner") || !strings.Contains(code, "Other_Inner") {
				t.Errorf("fields should reference the qualified nested types:\n%s", code)
			}
		})
	}
}

// TestCppTopologicalOrdering tests that a struct is emitted after every struct it holds by
// value. a C++ member of a type declared later in the file is an incomplete type, which does not
// compile
func TestCppTopologicalOrdering(t *testing.T) {
	// Person is declared first but holds an Address, so Address has to be emitted first
	file := testutil.NewFile("test.proto", "testpkg", []*descriptorpb.DescriptorProto{
		testutil.NewMessage("Person", []*descriptorpb.FieldDescriptorProto{
			testutil.NewMessageField("address", 1, "testpkg.Address"),
		}),
		testutil.NewMessage("Address", []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("city", 1, false),
		}),
	})

	code, err := NewCppGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	address := strings.Index(code, "struct Address {")
	person := strings.Index(code, "struct Person {")
	if address < 0 || person < 0 {
		t.Fatalf("Generated code missing struct definitions:\n%s", code)
	}
	if address > person {
		t.Errorf("struct Address must be defined before struct Person, which holds one:\n%s", code)
	}
}

// TestCppEmitsConverters tests that the nlohmann and yaml-cpp converters a nested message field
// is serialized through are actually generated. the member ToJSON/ToYAML functions alone do not
// satisfy either library, which looks up a free to_json overload and a YAML::convert
// specialization respectively
func TestCppEmitsConverters(t *testing.T) {
	file := testutil.NewFile("test.proto", "testpkg", []*descriptorpb.DescriptorProto{
		testutil.NewMessage("Address", []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("city", 1, false),
		}),
		testutil.NewMessage("Person", []*descriptorpb.FieldDescriptorProto{
			testutil.NewMessageField("address", 1, "testpkg.Address"),
		}),
	})

	code, err := NewCppGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	for _, want := range []string{
		"inline void to_json(nlohmann::json& j, const Address& value) {",
		"inline void from_json(const nlohmann::json& j, Address& value) {",
		"struct convert<::testpkg::Address> {",
		"inline Node convert<::testpkg::Address>::encode(",
		"inline bool convert<::testpkg::Address>::decode(",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("Generated code missing %q:\n%s", want, code)
		}
	}

	// the converters have to be declared before the struct whose member functions use them
	declaration := strings.Index(code, "inline void to_json(nlohmann::json& j, const Address& value);")
	person := strings.Index(code, "struct Person {")
	if declaration < 0 || declaration > person {
		t.Errorf("nlohmann converters must be declared before the struct that uses them:\n%s", code)
	}
}

// newCrossFileFiles builds an importing file and the file it imports, so a test can check that a
// reference across proto files turns into an import rather than a name that is never defined
//
// dependency: the imported file, defining common.Address
// importer: the importing file, defining app.Msg
func newCrossFileFiles() (*descriptorpb.FileDescriptorProto, *descriptorpb.FileDescriptorProto) {
	dependency := testutil.NewFile("common/types.proto", "common", []*descriptorpb.DescriptorProto{
		testutil.NewMessage("Address", []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("city", 1, false),
		}),
	})
	dependency.Options = &descriptorpb.FileOptions{GoPackage: testutil.String("example.com/gen/common;commonpb")}

	importer := testutil.NewFile("a/msg.proto", "app", []*descriptorpb.DescriptorProto{
		testutil.NewMessage("Msg", []*descriptorpb.FieldDescriptorProto{
			testutil.NewMessageField("addr", 1, "common.Address"),
		}),
	})
	importer.Options = &descriptorpb.FileOptions{GoPackage: testutil.String("example.com/gen/a")}

	return dependency, importer
}

// TestCrossFileReferences tests that a message defined in another proto file is imported and
// qualified. one output file is produced per proto file, so naming the type alone would leave
// the generated code referring to something it never defines
func TestCrossFileReferences(t *testing.T) {
	dependency, importer := newCrossFileFiles()
	registry := NewTypeRegistry([]*descriptorpb.FileDescriptorProto{dependency, importer}, []string{dependency.GetName(), importer.GetName()})

	tests := []struct {
		language  string
		generator Generator
		wants     []string
	}{
		{"go", NewGoGenerator(registry), []string{`commonpb "example.com/gen/common"`, "Addr commonpb.Address"}},
		{"c++", NewCppGenerator(registry), []string{`#include "common/types.h"`, "::common::Address addr;"}},
		{"python", NewPythonGenerator(registry), []string{"import common.types as types", "addr: types.Address"}},
		{"typescript", NewTypescriptGenerator(registry), []string{"import * as types from '../common/types';", "addr: types.Address;"}},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			code, err := tt.generator.Generate(importer)
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			for _, want := range tt.wants {
				if !strings.Contains(code, want) {
					t.Errorf("Generated code missing %q:\n%s", want, code)
				}
			}
		})
	}
}

// TestGoCrossFileNeedsGoPackage tests that a Go reference into a file with no go_package option
// is reported. nothing else in a descriptor carries the module path such an import needs, so
// generating a bare type name there would produce a file that does not compile
func TestGoCrossFileNeedsGoPackage(t *testing.T) {
	dependency, importer := newCrossFileFiles()
	dependency.Options = nil

	registry := NewTypeRegistry([]*descriptorpb.FileDescriptorProto{dependency, importer}, []string{dependency.GetName(), importer.GetName()})
	_, err := NewGoGenerator(registry).Generate(importer)
	if err == nil {
		t.Fatal("Generate() accepted a reference into a file with no go_package, want an error")
	}
	if !strings.Contains(err.Error(), "go_package") {
		t.Errorf("Generate() error = %v, want it to mention go_package", err)
	}
}

// TestCollectMessagesOrdersDependenciesFirst tests the shared dependency ordering, including the
// map case: a map field points at a synthetic entry message, and the type that has to be ordered
// first is the map's value, not the entry
func TestCollectMessagesOrdersDependenciesFirst(t *testing.T) {
	entry := testutil.NewMessage("ValuesEntry", []*descriptorpb.FieldDescriptorProto{
		testutil.NewStringField("key", 1, false),
		testutil.NewMessageField("value", 2, "testpkg.Value"),
	})
	entry.Options = &descriptorpb.MessageOptions{MapEntry: testutil.Bool(true)}

	holder := testutil.WithNested(
		testutil.NewMessage("Holder", []*descriptorpb.FieldDescriptorProto{
			testutil.NewMessageField("values", 1, "testpkg.Holder.ValuesEntry"),
		}),
		entry,
	)
	holder.Field[0].Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()

	file := testutil.NewFile("test.proto", "testpkg", []*descriptorpb.DescriptorProto{
		holder,
		testutil.NewMessage("Value", []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("v", 1, false),
		}),
	})

	var names []string
	for _, msg := range CollectMessages(file) {
		names = append(names, msg.TypeName)
	}

	// the synthetic entry is never emitted as a struct, and Value comes before the map that holds it
	want := []string{"Value", "Holder"}
	if !slices.Equal(names, want) {
		t.Errorf("CollectMessages() = %v, want %v", names, want)
	}
}

// TestPythonDefaultFactoryResistsShadowing tests that a field name cannot capture the name a
// later default_factory calls. a class body is looked up before the module, so a field called
// "dict" binds that name to a dataclasses.Field object, and a plain `default_factory=dict` on a
// later field would then call it -- "'Field' object is not callable" at construction time
func TestPythonDefaultFactoryResistsShadowing(t *testing.T) {
	entry := testutil.NewMessage("DictEntry", []*descriptorpb.FieldDescriptorProto{
		testutil.NewStringField("key", 1, false),
		testutil.NewStringField("value", 2, false),
	})
	entry.Options = &descriptorpb.MessageOptions{MapEntry: testutil.Bool(true)}

	// each field is named after what a *later* field's factory has to reach: the dict and list
	// builtins, and the Inner class
	msg := testutil.WithNested(
		testutil.NewMessage("Msg", []*descriptorpb.FieldDescriptorProto{
			testutil.NewMessageField("dict", 1, "testpkg.Msg.DictEntry"),
			testutil.NewMessageField("other", 2, "testpkg.Msg.DictEntry"),
			testutil.NewStringField("list", 3, true),
			testutil.NewStringField("more", 4, true),
			testutil.NewMessageField("Inner", 5, "testpkg.Inner"),
			testutil.NewMessageField("second", 6, "testpkg.Inner"),
		}),
		entry,
	)
	msg.Field[0].Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	msg.Field[1].Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()

	file := testutil.NewFile("test.proto", "testpkg", []*descriptorpb.DescriptorProto{
		testutil.NewMessage("Inner", []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("v", 1, false),
		}),
		msg,
	})
	code, err := NewPythonGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// a lambda body is a function scope, which skips the class namespace the field names live in
	for _, want := range []string{
		"dict: Dict[str, str] = field(default_factory=lambda: {})",
		"list: List[str] = field(default_factory=lambda: [])",
		"Inner: Inner = field(default_factory=lambda: Inner())",
		"second: Inner = field(default_factory=lambda: Inner())",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("Generated code missing %q:\n%s", want, code)
		}
	}

	// the bare callable is what the class body would resolve against its own attributes
	for _, unwanted := range []string{"default_factory=dict", "default_factory=list", "default_factory=Inner"} {
		if strings.Contains(code, unwanted) {
			t.Errorf("Generated code should not name %q directly in a class body:\n%s", unwanted, code)
		}
	}

	// the field names themselves are the user's, and are not renamed to dodge the collision. the
	// method bodies may go on naming the builtins, since a function scope skips the class namespace
	if !strings.Contains(code, `data["dict"] = dict(self.dict)`) || !strings.Contains(code, `data["list"] = list(self.list)`) {
		t.Errorf("Shadowing should be avoided without renaming the attributes:\n%s", code)
	}
}

// TestPythonScalarDecodeTreatsNullAsZero tests that a key present but explicitly null decodes to
// the field's zero value. dict.get only substitutes its default when the key is absent, so an
// empty YAML value ("age:") used to land None on a field annotated int
func TestPythonScalarDecodeTreatsNullAsZero(t *testing.T) {
	file := testutil.NewFile("test.proto", "testpkg", []*descriptorpb.DescriptorProto{
		testutil.NewMessage("Person", []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("name", 1, false),
			testutil.NewIntField("age", 2, false),
			testutil.Optional(testutil.NewIntField("rank", 3, false)),
		}),
	})
	code, err := NewPythonGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	for _, want := range []string{
		`name=data.get("name") if data.get("name") is not None else "",`,
		`age=data.get("age") if data.get("age") is not None else 0,`,
	} {
		if !strings.Contains(code, want) {
			t.Errorf("Generated from_dict missing %q:\n%s", want, code)
		}
	}

	// an explicitly optional field is the one place None is the answer: it is how the other
	// generators spell "unset", and collapsing it to the zero value would lose that distinction
	if !strings.Contains(code, `rank=data.get("rank"),`) {
		t.Errorf("Generated from_dict should keep None for an optional field:\n%s", code)
	}
}

// TestCppIncludeGuardsDoNotCollide tests that two protos whose paths mangle to the same
// identifier still get different guards. "a/b.proto" and "a_b.proto" both uppercase to A_B, and a
// translation unit including both headers would silently skip the second one whole
func TestCppIncludeGuardsDoNotCollide(t *testing.T) {
	guards := make(map[string]string)
	for _, name := range []string{"a/b.proto", "a_b.proto"} {
		file := testutil.NewFile(name, "testpkg", []*descriptorpb.DescriptorProto{
			testutil.NewMessage("Msg", []*descriptorpb.FieldDescriptorProto{
				testutil.NewStringField("v", 1, false),
			}),
		})
		code, err := NewCppGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
		if err != nil {
			t.Fatalf("Generate(%s) error = %v", name, err)
		}

		guard, _, found := strings.Cut(strings.TrimPrefix(code[strings.Index(code, "#ifndef "):], "#ifndef "), "\n")
		if !found {
			t.Fatalf("Generate(%s) produced no include guard:\n%s", name, code)
		}
		if other, taken := guards[guard]; taken {
			t.Errorf("%s and %s share the include guard %s", other, name, guard)
		}
		guards[guard] = name
	}

	// the readable part of the name survives the disambiguation
	for guard := range guards {
		if !strings.HasPrefix(guard, "PROTO_A_B_") || !strings.HasSuffix(guard, "_H_") {
			t.Errorf("Include guard %q should still name the proto it came from", guard)
		}
	}
}

// TestKeywordFieldNamesAreEscaped tests that a proto field whose name is a reserved word in the
// target language is renamed in the generated code. "from" and "class" are legal proto field
// names, and emitting one verbatim produces a file that does not even parse
func TestKeywordFieldNamesAreEscaped(t *testing.T) {
	file := testutil.NewFile("test.proto", "testpkg", []*descriptorpb.DescriptorProto{
		testutil.NewMessage("Msg", []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("from", 1, false),
			testutil.NewStringField("class", 2, false),
			testutil.NewIntField("operator", 3, false),
		}),
	})
	registry := NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})

	tests := []struct {
		language  string
		generator Generator
		declares  []string
	}{
		{"c++", NewCppGenerator(registry), []string{"std::string class_;", "int32_t operator_ = 0;"}},
		{"python", NewPythonGenerator(registry), []string{"from_: str = \"\"", "class_: str = \"\""}},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			code, err := tt.generator.Generate(file)
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			for _, want := range tt.declares {
				if !strings.Contains(code, want) {
					t.Errorf("Generated code missing %q:\n%s", want, code)
				}
			}

			// escaping renames the member, never the key it serializes under
			for _, key := range []string{`"from"`, `"class"`, `"operator"`} {
				if !strings.Contains(code, key) {
					t.Errorf("Generated code should still serialize under %s:\n%s", key, code)
				}
			}
		})
	}
}

// TestCppZeroInitializesScalars tests that scalar members get a default initializer. a bare
// `int32_t age;` holds an indeterminate value, so reading one is undefined behaviour and a field
// missing from the input keeps whatever was on the stack instead of the zero every other
// generator produces
func TestCppZeroInitializesScalars(t *testing.T) {
	file := testutil.NewFile("test.proto", "testpkg", []*descriptorpb.DescriptorProto{
		testutil.NewMessage("Msg", []*descriptorpb.FieldDescriptorProto{
			testutil.NewIntField("age", 1, false),
			testutil.NewBoolField("active", 2),
			testutil.NewFloatField("score", 3),
			testutil.NewStringField("name", 4, false),
			testutil.NewStringField("tags", 5, true),
		}),
	})

	code, err := NewCppGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	for _, want := range []string{"int32_t age = 0;", "bool active = false;", "float score = 0.0f;"} {
		if !strings.Contains(code, want) {
			t.Errorf("Generated code missing zero initializer %q:\n%s", want, code)
		}
	}

	// a string and a vector already default-construct to empty, so an initializer would be noise
	for _, want := range []string{"std::string name;", "std::vector<std::string> tags;"} {
		if !strings.Contains(code, want) {
			t.Errorf("Generated code should leave %q without an initializer:\n%s", want, code)
		}
	}
}

// TestCppResetsSingularMembersBeforeDecode tests that decoding clears a singular member first. a
// repeated field or map has always been cleared, so without this decoding twice into the same
// struct replaced the containers while merging the scalars -- and the other three generators all
// rebuild the whole message
func TestCppResetsSingularMembersBeforeDecode(t *testing.T) {
	file := testutil.NewFile("test.proto", "testpkg", []*descriptorpb.DescriptorProto{
		testutil.NewMessage("Address", []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("city", 1, false),
		}),
		testutil.NewMessage("Person", []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("name", 1, false),
			testutil.NewIntField("age", 2, false),
			testutil.NewMessageField("address", 3, "testpkg.Address"),
			testutil.Optional(testutil.NewStringField("nickname", 4, false)),
			testutil.NewStringField("tags", 5, true),
		}),
	})

	code, err := NewCppGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	for _, want := range []string{
		"this->name = std::string{};",
		"this->age = int32_t{};",
		"this->address = Address{};",
		"this->nickname = std::optional<std::string>{};",
		// a container keeps the clear() it always had
		"this->tags.clear();",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("Generated code missing %q:\n%s", want, code)
		}
	}

	// both decoders take the same "absent or null means reset" path
	for _, want := range []string{
		`if (j.contains("name") && !j["name"].is_null()) {`,
		`if (node["name"] && !node["name"].IsNull()) {`,
	} {
		if !strings.Contains(code, want) {
			t.Errorf("Generated code missing %q:\n%s", want, code)
		}
	}
}

// TestGoResetsReceiverBeforeDecode tests that the Go decoders zero the receiver first. both
// encoding/json and yaml leave a field the document omits untouched and merge into an existing
// map, so without the reset a second decode into the same value would keep the first document's
// scalars and accumulate its map entries -- while the other three generators rebuild the whole
// message
func TestGoResetsReceiverBeforeDecode(t *testing.T) {
	file := testutil.NewFile("test.proto", "testpkg", []*descriptorpb.DescriptorProto{
		testutil.NewMessage("Person", []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("name", 1, false),
		}),
	})

	code, err := NewGoGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	for _, want := range []string{
		"func (x *Person) FromJSON(data []byte) error {\n\t*x = Person{}\n\treturn json.Unmarshal(data, x)\n}",
		"func (x *Person) FromYAML(data []byte) error {\n\t*x = Person{}\n\treturn yaml.Unmarshal(data, x)\n}",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("Generated code missing %q:\n%s", want, code)
		}
	}
}

// TestGoRepeatedAndMapOmitEmpty tests that repeated fields and maps are tagged omitempty. a nil
// slice or map otherwise marshals to JSON null, which the C++ reader throws on
func TestGoRepeatedAndMapOmitEmpty(t *testing.T) {
	entry := testutil.NewMessage("MetaEntry", []*descriptorpb.FieldDescriptorProto{
		testutil.NewStringField("key", 1, false),
		testutil.NewStringField("value", 2, false),
	})
	entry.Options = &descriptorpb.MessageOptions{MapEntry: testutil.Bool(true)}

	msg := testutil.WithNested(
		testutil.NewMessage("Msg", []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("tags", 1, true),
			testutil.NewMessageField("meta", 2, "testpkg.Msg.MetaEntry"),
			testutil.NewStringField("name", 3, false),
		}),
		entry,
	)
	msg.Field[1].Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()

	file := testutil.NewFile("test.proto", "testpkg", []*descriptorpb.DescriptorProto{msg})
	code, err := NewGoGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	for _, want := range []string{
		"Tags []string `json:\"tags,omitempty\" yaml:\"tags,omitempty\"`",
		"Meta map[string]string `json:\"meta,omitempty\" yaml:\"meta,omitempty\"`",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("Generated code missing %q:\n%s", want, code)
		}
	}

	// a singular field has no such problem and keeps its plain tag
	if !strings.Contains(code, "Name string `json:\"name\" yaml:\"name\"`") {
		t.Errorf("Singular field should not get omitempty:\n%s", code)
	}
}

// TestTypescriptFromObjectDefaults tests that decoding fills in every non-optional property.
// casting a parsed document to the interface asserts the properties are there without making it
// so, which left a missing array undefined and threw on first use
func TestTypescriptFromObjectDefaults(t *testing.T) {
	file := testutil.NewFile("test.proto", "testpkg", []*descriptorpb.DescriptorProto{
		testutil.NewMessage("Address", []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("city", 1, false),
		}),
		testutil.NewMessage("Person", []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("name", 1, false),
			testutil.NewIntField("age", 2, false),
			testutil.NewStringField("tags", 3, true),
			testutil.NewMessageField("address", 4, "testpkg.Address"),
			testutil.Optional(testutil.NewStringField("nickname", 5, false)),
		}),
	})

	code, err := NewTypescriptGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	for _, want := range []string{
		"export function fromObject(data: unknown): Person {",
		`name: ((source["name"] ?? "") as string),`,
		`age: ((source["age"] ?? 0) as number),`,
		`tags: ((source["tags"] ?? []) as string[]),`,
		`address: Address.fromObject(source["address"]),`,
		"return fromObject(JSON.parse(data));",
		"return fromObject(yaml.parse(data));",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("Generated code missing %q:\n%s", want, code)
		}
	}

	// an unset optional stays absent rather than becoming an explicit undefined
	if !strings.Contains(code, `if (source["nickname"] !== undefined && source["nickname"] !== null) {`) {
		t.Errorf("Optional property should be assigned only when present:\n%s", code)
	}

	// the old unchecked cast must be gone
	if strings.Contains(code, "return JSON.parse(data) as Person;") {
		t.Errorf("fromJSON should not be an unchecked cast:\n%s", code)
	}
}

// TestTypescriptMapKeysAreParsed tests that an integer-keyed map is rebuilt through its declared
// key type rather than copied wholesale. a JavaScript object key is a string however it is
// assigned, so this cannot make the key a number at runtime -- what it buys is that a document
// written with a non-canonical key ("007") still lands where indexing by 7 finds it, the way the
// other three generators parse the key
func TestTypescriptMapKeysAreParsed(t *testing.T) {
	labels := testutil.NewMessage("LabelsEntry", []*descriptorpb.FieldDescriptorProto{
		testutil.NewIntField("key", 1, false),
		testutil.NewStringField("value", 2, false),
	})
	labels.Options = &descriptorpb.MessageOptions{MapEntry: testutil.Bool(true)}

	names := testutil.NewMessage("NamesEntry", []*descriptorpb.FieldDescriptorProto{
		testutil.NewStringField("key", 1, false),
		testutil.NewStringField("value", 2, false),
	})
	names.Options = &descriptorpb.MessageOptions{MapEntry: testutil.Bool(true)}

	msg := testutil.WithNested(
		testutil.WithNested(
			testutil.NewMessage("Msg", []*descriptorpb.FieldDescriptorProto{
				testutil.NewMessageField("labels", 1, "testpkg.Msg.LabelsEntry"),
				testutil.NewMessageField("names", 2, "testpkg.Msg.NamesEntry"),
			}),
			labels,
		),
		names,
	)
	msg.Field[0].Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	msg.Field[1].Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()

	file := testutil.NewFile("test.proto", "testpkg", []*descriptorpb.DescriptorProto{msg})
	code, err := NewTypescriptGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if !strings.Contains(code, "[Number(key), value as string] as [number, string]") {
		t.Errorf("Generated fromObject should parse integer map keys:\n%s", code)
	}

	// a string-keyed map of primitives is already in its final shape, so it stays a plain copy
	if !strings.Contains(code, `names: { ...((source["names"] ?? {}) as Record<string, string>) },`) {
		t.Errorf("A string-keyed map should be copied wholesale:\n%s", code)
	}
}

// TestPythonMapKeysAreCoerced tests that an integer-keyed map comes back keyed by int. JSON
// object keys are always strings, so without coercion the round trip is not identity and a lookup
// by the original key misses
func TestPythonMapKeysAreCoerced(t *testing.T) {
	entry := testutil.NewMessage("LabelsEntry", []*descriptorpb.FieldDescriptorProto{
		testutil.NewIntField("key", 1, false),
		testutil.NewStringField("value", 2, false),
	})
	entry.Options = &descriptorpb.MessageOptions{MapEntry: testutil.Bool(true)}

	msg := testutil.WithNested(
		testutil.NewMessage("Msg", []*descriptorpb.FieldDescriptorProto{
			testutil.NewMessageField("labels", 1, "testpkg.Msg.LabelsEntry"),
		}),
		entry,
	)
	msg.Field[0].Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()

	file := testutil.NewFile("test.proto", "testpkg", []*descriptorpb.DescriptorProto{msg})
	code, err := NewPythonGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if !strings.Contains(code, "{int(key): value for key, value in") {
		t.Errorf("Generated from_dict should coerce integer map keys:\n%s", code)
	}

	// an empty YAML document loads as None, which from_dict has to survive
	if !strings.Contains(code, "data = data or {}") {
		t.Errorf("Generated from_dict should tolerate a None document:\n%s", code)
	}
}

// TestCppNonStringMapKeys tests that a map with a non-string key is written as a JSON object
// keyed by the stringified key. nlohmann otherwise falls back to an array of pairs, while Go and
// Python both write an object
func TestCppNonStringMapKeys(t *testing.T) {
	entry := testutil.NewMessage("LabelsEntry", []*descriptorpb.FieldDescriptorProto{
		testutil.NewIntField("key", 1, false),
		testutil.NewStringField("value", 2, false),
	})
	entry.Options = &descriptorpb.MessageOptions{MapEntry: testutil.Bool(true)}

	msg := testutil.WithNested(
		testutil.NewMessage("Msg", []*descriptorpb.FieldDescriptorProto{
			testutil.NewMessageField("labels", 1, "testpkg.Msg.LabelsEntry"),
		}),
		entry,
	)
	msg.Field[0].Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()

	file := testutil.NewFile("test.proto", "testpkg", []*descriptorpb.DescriptorProto{msg})
	code, err := NewCppGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	for _, want := range []string{
		"entries[std::to_string(entry.first)] = entry.second;",
		"static_cast<int32_t>(std::stoll(it.key()))",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("Generated code missing %q:\n%s", want, code)
		}
	}
}

// TestReferenceIntoNonGeneratedFile tests that a type from a proto protoc was not asked to
// generate is reported. no output file exists for it, so importing it names a module that is not
// there -- which is what a reference to a well-known type such as google.protobuf.Timestamp does
func TestReferenceIntoNonGeneratedFile(t *testing.T) {
	dependency, importer := newCrossFileFiles()

	// only the importer is being generated
	registry := NewTypeRegistry(
		[]*descriptorpb.FileDescriptorProto{dependency, importer},
		[]string{importer.GetName()},
	)

	for _, generator := range []struct {
		language string
		gen      Generator
	}{
		{"go", NewGoGenerator(registry)},
		{"c++", NewCppGenerator(registry)},
		{"python", NewPythonGenerator(registry)},
		{"typescript", NewTypescriptGenerator(registry)},
	} {
		t.Run(generator.language, func(t *testing.T) {
			_, err := generator.gen.Generate(importer)
			if err == nil {
				t.Fatal("Generate() accepted a reference into a file that is not being generated, want an error")
			}
			if !strings.Contains(err.Error(), "not being generated") {
				t.Errorf("Generate() error = %v, want it to explain the file is not being generated", err)
			}
		})
	}
}

// TestFlattenedNameCollidesWithTopLevel tests that a flattened nested name giving way to a
// top-level message of the same name still leaves both declared, distinctly
func TestFlattenedNameCollidesWithTopLevel(t *testing.T) {
	file := testutil.NewFile("test.proto", "testpkg", []*descriptorpb.DescriptorProto{
		testutil.WithNested(
			testutil.NewMessage("User", []*descriptorpb.FieldDescriptorProto{
				testutil.NewMessageField("profile", 1, "testpkg.User.Profile"),
			}),
			testutil.NewMessage("Profile", []*descriptorpb.FieldDescriptorProto{
				testutil.NewStringField("bio", 1, false),
			}),
		),
		testutil.NewMessage("User_Profile", []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("other", 1, false),
		}),
	})

	code, err := NewGoGenerator(NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})).Generate(file)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// the top-level message keeps its own name, so the nested one is the one that moves
	if strings.Count(code, "type User_Profile struct") != 1 {
		t.Errorf("want exactly one 'type User_Profile struct':\n%s", code)
	}
	if strings.Count(code, "type User_Profile2 struct") != 1 {
		t.Errorf("want exactly one 'type User_Profile2 struct':\n%s", code)
	}
	if !strings.Contains(code, "Profile User_Profile2") {
		t.Errorf("the nested field should reference the renamed nested type:\n%s", code)
	}
}

// TestEscapedTypeNamesStayDistinct tests that two message names escaping to the same identifier
// still declare two types. a generator escapes a type name and never de-duplicates it again, so a
// file declaring both "class" and "class_" would otherwise emit "struct class_" twice
func TestEscapedTypeNamesStayDistinct(t *testing.T) {
	file := testutil.NewFile("test.proto", "testpkg", []*descriptorpb.DescriptorProto{
		testutil.NewMessage("class", []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("a", 1, false),
		}),
		testutil.NewMessage("class_", []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("b", 1, false),
		}),
	})

	registry := NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})

	for _, target := range []struct {
		language     string
		gen          Generator
		declarations []string
	}{
		{"go", NewGoGenerator(registry), []string{"type class struct", "type class_2 struct"}},
		{"c++", NewCppGenerator(registry), []string{"struct class_ {", "struct class_2 {"}},
		{"python", NewPythonGenerator(registry), []string{"class class_:", "class class_2:"}},
		{"typescript", NewTypescriptGenerator(registry), []string{"export interface class_ {", "export interface class_2 {"}},
	} {
		t.Run(target.language, func(t *testing.T) {
			code, err := target.gen.Generate(file)
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}
			for _, want := range target.declarations {
				if count := strings.Count(code, want); count != 1 {
					t.Errorf("want exactly one %q, got %d:\n%s", want, count, code)
				}
			}
		})
	}
}

// TestUniqueNames tests the de-duplication two proto names converging on one generated name rely
// on. Go title-cases "user_id" and "userId" to the same identifier, and escaping can map "class"
// and "class_" together
func TestUniqueNames(t *testing.T) {
	got := UniqueNames([]string{"UserId", "UserId", "Name", "UserId"})
	want := []string{"UserId", "UserId2", "Name", "UserId3"}
	if !slices.Equal(got, want) {
		t.Errorf("UniqueNames() = %v, want %v", got, want)
	}
}

// BenchmarkGoGenerator benchmarks Go code generation
func BenchmarkGoGenerator(b *testing.B) {
	file := testutil.NewFile(
		"test.proto",
		"testpkg",
		[]*descriptorpb.DescriptorProto{
			testutil.NewMessage("Person", []*descriptorpb.FieldDescriptorProto{
				testutil.NewStringField("name", 1, false),
			}),
		},
	)

	registry := NewTypeRegistry([]*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()})
	gen := NewGoGenerator(registry)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gen.Generate(file)
	}
}
