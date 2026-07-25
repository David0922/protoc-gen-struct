package generator

import (
	"strings"
	"testing"

	"protoc-gen-struct/testutil"

	"google.golang.org/protobuf/types/descriptorpb"
)

// TestGenerateForAllLanguages tests the Generate function for all supported languages
// it validates that code is correctly generated for each language
func TestGenerateForAllLanguages(t *testing.T) {
	file := testutil.NewFile(
		"test.proto",
		"testpkg",
		[]*descriptorpb.DescriptorProto{
			testutil.NewMessage(
				"Person",
				[]*descriptorpb.FieldDescriptorProto{
					testutil.NewStringField("name", 1, false),
					testutil.NewIntField("age", 2, false),
				},
			),
		},
	)

	tests := []struct {
		lang            string
		expectedExt     string
		expectedKeyword string
	}{
		{"go", ".go", "package testpkg"},
		{"c++", ".h", "namespace testpkg"},
		{"cpp", ".h", "namespace testpkg"},
		{"python", ".py", "@dataclass"},
		{"typescript", ".ts", "interface Person"},
		{"ts", ".ts", "interface Person"},
	}

	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			content, filename, err := Generate(file, []*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()}, tt.lang)
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			if !strings.HasSuffix(filename, tt.expectedExt) {
				t.Errorf("Generate() returned filename %q, want to end with %q", filename, tt.expectedExt)
			}

			if !strings.Contains(content, tt.expectedKeyword) {
				t.Errorf("Generate() content missing %q", tt.expectedKeyword)
			}

			if len(content) == 0 {
				t.Error("Generate() returned empty content")
			}
		})
	}
}

// TestGenerateHeader tests that every generated file opens with the do-not-edit notice, written
// in the target language's own comment syntax
func TestGenerateHeader(t *testing.T) {
	file := testutil.NewFile(
		"test.proto",
		"testpkg",
		[]*descriptorpb.DescriptorProto{
			testutil.NewMessage("Person", []*descriptorpb.FieldDescriptorProto{
				testutil.NewStringField("name", 1, false),
			}),
		},
	)

	tests := []struct {
		lang     string
		expected string
	}{
		{"go", "// " + generatedHeader},
		{"c++", "// " + generatedHeader},
		{"python", "# " + generatedHeader},
		{"typescript", "// " + generatedHeader},
	}

	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			content, _, err := Generate(file, []*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()}, tt.lang)
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			// the notice has to be the first line, so it is visible without scrolling and, for Go,
			// is separated from the package clause by the blank line that follows
			if !strings.HasPrefix(content, tt.expected+"\n\n") {
				t.Errorf("Generate() content does not start with %q, got %q", tt.expected, content[:min(len(content), 120)])
			}
		})
	}
}

// TestGenerateUnsupportedLanguage tests that unsupported languages are rejected
func TestGenerateUnsupportedLanguage(t *testing.T) {
	file := testutil.NewFile(
		"test.proto",
		"testpkg",
		[]*descriptorpb.DescriptorProto{
			testutil.NewMessage("Person", []*descriptorpb.FieldDescriptorProto{
				testutil.NewStringField("name", 1, false),
			}),
		},
	)

	_, _, err := Generate(file, []*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()}, "rust")
	if err == nil {
		t.Error("Generate() expected error for unsupported language, got nil")
	}

	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("Generate() error = %v, want to contain 'unsupported'", err)
	}
}

// TestGenerateFilename tests that output filenames are correctly derived
func TestGenerateFilename(t *testing.T) {
	tests := []struct {
		protoFile string
		lang      string
		expected  string
	}{
		{"test.proto", "go", "test.go"},
		{"test.proto", "c++", "test.h"},
		{"test.proto", "python", "test.py"},
		{"test.proto", "typescript", "test.ts"},
		// the source directory is preserved, so two protos of the same name in different
		// directories do not both claim the same output path
		{"path/to/test.proto", "go", "path/to/test.go"},
		{"path/to/test.proto", "c++", "path/to/test.h"},
	}

	for _, tt := range tests {
		t.Run(tt.protoFile+"_"+tt.lang, func(t *testing.T) {
			file := testutil.NewFile(
				tt.protoFile,
				"pkg",
				[]*descriptorpb.DescriptorProto{
					testutil.NewMessage("Message", []*descriptorpb.FieldDescriptorProto{
						testutil.NewStringField("value", 1, false),
					}),
				},
			)

			_, filename, err := Generate(file, []*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()}, tt.lang)
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			if filename != tt.expected {
				t.Errorf("Generate() filename = %q, want %q", filename, tt.expected)
			}
		})
	}
}

// BenchmarkGenerate benchmarks the code generation process
func BenchmarkGenerate(b *testing.B) {
	file := testutil.NewFile(
		"test.proto",
		"testpkg",
		[]*descriptorpb.DescriptorProto{
			testutil.NewMessage("Person", []*descriptorpb.FieldDescriptorProto{
				testutil.NewStringField("name", 1, false),
				testutil.NewIntField("age", 2, false),
			}),
		},
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Generate(file, []*descriptorpb.FileDescriptorProto{file}, []string{file.GetName()}, "go")
	}
}
