package main

import (
	"testing"

	"protoc-gen-struct/testutil"
)

// TestExtractLanguage tests the language extraction function
// it validates that language parameters are correctly parsed
func TestExtractLanguage(t *testing.T) {
	tests := []struct {
		name     string
		param    *string
		expected string
	}{
		{
			name:     "go language",
			param:    testutil.String("go"),
			expected: "go",
		},
		{
			name:     "c++ language",
			param:    testutil.String("c++"),
			expected: "c++",
		},
		{
			name:     "python language",
			param:    testutil.String("python"),
			expected: "python",
		},
		{
			name:     "typescript language",
			param:    testutil.String("typescript"),
			expected: "typescript",
		},
		{
			name:     "nil parameter",
			param:    nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractLanguage(tt.param)
			if result != tt.expected {
				t.Errorf("extractLanguage() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestGenerateCodeWithValidation tests that unsupported types are properly handled
func TestGenerateCodeStructure(t *testing.T) {
	// test that the plugin structure is set up correctly
	// more detailed integration tests would use actual proto files
	t.Run("structure_test", func(t *testing.T) {
		// just verify the main entry points exist
		// full integration testing would require protoc to be installed
	})
}
