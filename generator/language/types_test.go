package language

import (
	"testing"

	"google.golang.org/protobuf/types/descriptorpb"
	"protoc-gen-struct/testutil"
)

// TestSnakeToCamelCase tests conversion of snake_case to camelCase
func TestSnakeToCamelCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"first_name", "firstName"},
		{"first", "first"},
		{"first_second_third", "firstSecondThird"},
		{"a_b_c", "aBC"},
		{"name", "name"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := SnakeToCamelCase(tt.input)
			if result != tt.expected {
				t.Errorf("SnakeToCamelCase(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestSnakeToTitleCase tests conversion of snake_case to TitleCase
func TestSnakeToTitleCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"first_name", "FirstName"},
		{"first", "First"},
		{"first_second_third", "FirstSecondThird"},
		{"a_b_c", "ABC"},
		{"name", "Name"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := SnakeToTitleCase(tt.input)
			if result != tt.expected {
				t.Errorf("SnakeToTitleCase(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestGetTypeName tests type name conversion for different languages
func TestGetTypeName(t *testing.T) {
	tests := []struct {
		fieldType FieldType
		language  string
		expected  string
	}{
		// Go types
		{FieldTypeBool, "go", "bool"},
		{FieldTypeInt32, "go", "int32"},
		{FieldTypeString, "go", "string"},

		// Python types
		{FieldTypeBool, "python", "bool"},
		{FieldTypeInt32, "python", "int"},
		{FieldTypeString, "python", "str"},

		// TypeScript types
		{FieldTypeBool, "typescript", "boolean"},
		{FieldTypeInt32, "typescript", "number"},
		{FieldTypeString, "typescript", "string"},

		// C++ types
		{FieldTypeBool, "c++", "bool"},
		{FieldTypeInt32, "c++", "int32_t"},
		{FieldTypeString, "c++", "std::string"},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			result := GetTypeName(tt.fieldType, tt.language)
			if result != tt.expected {
				t.Errorf("GetTypeName(%v, %q) = %q, want %q", tt.fieldType, tt.language, result, tt.expected)
			}
		})
	}
}

// TestGetUnqualifiedTypeName tests extraction of unqualified type names
func TestGetUnqualifiedTypeName(t *testing.T) {
	tests := []struct {
		fullName string
		expected string
	}{
		{"Simple", "Simple"},
		{"package.Message", "Message"},
		{"package.Outer.Inner", "Inner"},
	}

	for _, tt := range tests {
		t.Run(tt.fullName, func(t *testing.T) {
			result := GetUnqualifiedTypeName(tt.fullName)
			if result != tt.expected {
				t.Errorf("GetUnqualifiedTypeName(%q) = %q, want %q", tt.fullName, result, tt.expected)
			}
		})
	}
}

// TestIsBuiltinType tests checking if a type is built-in
func TestIsBuiltinType(t *testing.T) {
	tests := []struct {
		fieldType FieldType
		expected  bool
	}{
		{FieldTypeBool, true},
		{FieldTypeInt32, true},
		{FieldTypeString, true},
		{FieldTypeMessage, false},
	}

	for _, tt := range tests {
		result := IsBuiltinType(tt.fieldType)
		if result != tt.expected {
			t.Errorf("IsBuiltinType(%v) = %v, want %v", tt.fieldType, result, tt.expected)
		}
	}
}

// TestGetMessageFields tests extraction of all fields from a message
func TestGetMessageFields(t *testing.T) {
	registry := NewTypeRegistry(nil, nil)

	msg := testutil.NewMessage("Person", []*descriptorpb.FieldDescriptorProto{
		testutil.NewStringField("name", 1, false),
		testutil.NewIntField("age", 2, false),
	})

	fields, err := GetMessageFields(msg, registry)
	if err != nil {
		t.Fatalf("GetMessageFields() error = %v", err)
	}

	if len(fields) != 2 {
		t.Errorf("GetMessageFields() returned %d fields, want 2", len(fields))
	}

	if fields[0].Name != "name" {
		t.Errorf("First field name = %q, want 'name'", fields[0].Name)
	}

	if fields[1].Name != "age" {
		t.Errorf("Second field name = %q, want 'age'", fields[1].Name)
	}
}

// TestExtractFieldInfoOptional tests that the proto3 optional keyword is picked up from
// proto3_optional, and that the LABEL_OPTIONAL label alone does not mark a field optional
func TestExtractFieldInfoOptional(t *testing.T) {
	registry := NewTypeRegistry(nil, nil)

	tests := []struct {
		name     string
		field    *descriptorpb.FieldDescriptorProto
		expected bool
	}{
		// every singular proto3 field carries LABEL_OPTIONAL, so this one is not optional
		{"singular field", testutil.NewStringField("name", 1, false), false},
		{"optional field", testutil.Optional(testutil.NewStringField("nickname", 2, false)), true},
		{"repeated field", testutil.NewStringField("tags", 3, true), false},
		{"message field", testutil.NewMessageField("address", 4, "testpkg.Address"), false},
		{"optional message field", testutil.Optional(testutil.NewMessageField("home", 5, "testpkg.Address")), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := ExtractFieldInfo(tt.field, registry)
			if err != nil {
				t.Fatalf("ExtractFieldInfo() error = %v", err)
			}
			if info.IsOptional != tt.expected {
				t.Errorf("ExtractFieldInfo(%q).IsOptional = %v, want %v", tt.name, info.IsOptional, tt.expected)
			}
		})
	}
}

// BenchmarkSnakeToCamelCase benchmarks the snake to camel case conversion
func BenchmarkSnakeToCamelCase(b *testing.B) {
	for i := 0; i < b.N; i++ {
		SnakeToCamelCase("first_name_string_value")
	}
}

// BenchmarkGetTypeName benchmarks type name resolution
func BenchmarkGetTypeName(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GetTypeName(FieldTypeInt32, "go")
		GetTypeName(FieldTypeString, "python")
		GetTypeName(FieldTypeFloat, "typescript")
	}
}
