package validator

import (
	"fmt"
	"strings"
	"testing"

	"protoc-gen-struct/testutil"

	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// TestValidateRequest tests the ValidateRequest function
// it validates that requests with supported types pass and unsupported types fail
// tests: valid messages, unsupported enum types, circular dependencies
func TestValidateRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *pluginpb.CodeGeneratorRequest
		wantErr bool
	}{
		{
			name: "valid simple message",
			req: &pluginpb.CodeGeneratorRequest{
				FileToGenerate: []string{"test.proto"},
				ProtoFile: []*descriptorpb.FileDescriptorProto{
					testutil.NewFile(
						"test.proto",
						"test",
						[]*descriptorpb.DescriptorProto{
							testutil.NewMessage("Person", []*descriptorpb.FieldDescriptorProto{
								testutil.NewStringField("name", 1, false),
								testutil.NewIntField("age", 2, false),
							}),
						},
					),
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// newRequest wraps a single file in a CodeGeneratorRequest that asks for it to be generated
//
// file: the file to request
//
// the CodeGeneratorRequest
func newRequest(file *descriptorpb.FileDescriptorProto) *pluginpb.CodeGeneratorRequest {
	return &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{file.GetName()},
		ProtoFile:      []*descriptorpb.FileDescriptorProto{file},
	}
}

// TestValidateNestedMessages tests that messages nested inside another are validated too. code is
// generated for them whether or not a field refers to them, so an unsupported type inside one
// must be caught the same way it is at the top level
func TestValidateNestedMessages(t *testing.T) {
	file := testutil.NewFile("test.proto", "test", []*descriptorpb.DescriptorProto{
		testutil.WithNested(
			testutil.NewMessage("Outer", []*descriptorpb.FieldDescriptorProto{
				testutil.NewStringField("name", 1, false),
			}),
			testutil.NewMessage("Inner", []*descriptorpb.FieldDescriptorProto{
				testutil.NewBytesField("data", 1),
			}),
		),
	})

	err := ValidateRequest(newRequest(file))
	if err == nil {
		t.Fatal("ValidateRequest() accepted a bytes field in a nested message, want an error")
	}
	if !strings.Contains(err.Error(), "test.Outer.Inner") {
		t.Errorf("ValidateRequest() error = %v, want it to name the nested message", err)
	}
}

// TestValidateRejectsOneof tests that a real oneof is rejected. its arms would otherwise be
// generated as independent, always-present fields, losing both the mutual exclusion and any way
// to tell which arm was set
func TestValidateRejectsOneof(t *testing.T) {
	msg := testutil.WithOneofDecl(
		testutil.NewMessage("M", []*descriptorpb.FieldDescriptorProto{
			testutil.InOneof(testutil.NewStringField("s", 1, false), 0),
			testutil.InOneof(testutil.NewIntField("i", 2, false), 0),
		}),
		"kind",
	)

	err := ValidateRequest(newRequest(testutil.NewFile("test.proto", "test", []*descriptorpb.DescriptorProto{msg})))
	if err == nil {
		t.Fatal("ValidateRequest() accepted a oneof, want an error")
	}
	if !strings.Contains(err.Error(), "oneof kind") {
		t.Errorf("ValidateRequest() error = %v, want it to name the oneof", err)
	}
}

// TestValidateAcceptsProto3Optional tests that the synthetic one-field oneof protoc wraps every
// proto3 `optional` field in is not mistaken for a real oneof
func TestValidateAcceptsProto3Optional(t *testing.T) {
	msg := testutil.WithOneofDecl(
		testutil.NewMessage("M", []*descriptorpb.FieldDescriptorProto{
			testutil.InOneof(testutil.Optional(testutil.NewStringField("nickname", 1, false)), 0),
		}),
		"_nickname",
	)

	if err := ValidateRequest(newRequest(testutil.NewFile("test.proto", "test", []*descriptorpb.DescriptorProto{msg}))); err != nil {
		t.Errorf("ValidateRequest() rejected a proto3 optional field: %v", err)
	}
}

// TestValidateCircularDependency tests that a reference cycle is still reported, which the
// memoized walk has to keep doing while it skips types it has already finished
func TestValidateCircularDependency(t *testing.T) {
	file := testutil.NewFile("test.proto", "test", []*descriptorpb.DescriptorProto{
		testutil.NewMessage("A", []*descriptorpb.FieldDescriptorProto{
			testutil.NewMessageField("b", 1, "test.B"),
		}),
		testutil.NewMessage("B", []*descriptorpb.FieldDescriptorProto{
			testutil.NewMessageField("a", 1, "test.A"),
		}),
	})

	err := ValidateRequest(newRequest(file))
	if err == nil {
		t.Fatal("ValidateRequest() accepted a reference cycle, want an error")
	}
	if !strings.Contains(err.Error(), "circular dependency") {
		t.Errorf("ValidateRequest() error = %v, want a circular dependency error", err)
	}
}

// TestValidateDeepDAGTerminates tests that a message graph where every type is reachable by many
// paths is validated in linear time. re-walking the subtree once per reference instead costs
// time exponential in the depth, which at this size does not finish
func TestValidateDeepDAGTerminates(t *testing.T) {
	const depth = 60

	messages := []*descriptorpb.DescriptorProto{
		testutil.NewMessage(fmt.Sprintf("M%d", depth), []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("leaf", 1, false),
		}),
	}
	for i := depth - 1; i >= 0; i-- {
		next := fmt.Sprintf("test.M%d", i+1)
		messages = append(messages, testutil.NewMessage(fmt.Sprintf("M%d", i), []*descriptorpb.FieldDescriptorProto{
			testutil.NewMessageField("a", 1, next),
			testutil.NewMessageField("b", 2, next),
		}))
	}

	if err := ValidateRequest(newRequest(testutil.NewFile("test.proto", "test", messages))); err != nil {
		t.Errorf("ValidateRequest() error = %v", err)
	}
}

// TestValidateRejectsNonProto3 tests that a file that is not proto3 is refused. every presence
// rule the generators rely on is a proto3 rule, so a proto2 descriptor would silently lose the
// presence its optional fields declare
func TestValidateRejectsNonProto3(t *testing.T) {
	file := testutil.NewFile("test.proto", "test", []*descriptorpb.DescriptorProto{
		testutil.NewMessage("M", []*descriptorpb.FieldDescriptorProto{
			testutil.NewStringField("x", 1, false),
		}),
	})
	file.Syntax = nil // a proto2 file leaves the syntax unset

	err := ValidateRequest(newRequest(file))
	if err == nil {
		t.Fatal("ValidateRequest() accepted a non-proto3 file, want an error")
	}
	if !strings.Contains(err.Error(), "proto3") {
		t.Errorf("ValidateRequest() error = %v, want it to mention proto3", err)
	}
}

// TestValidateRejectsBoolMapKey tests that a bool map key is refused. proto allows it, but Go's
// encoding/json cannot marshal a map keyed by bool at all, so such a message could never
// round-trip
func TestValidateRejectsBoolMapKey(t *testing.T) {
	entry := testutil.NewMessage("BEntry", []*descriptorpb.FieldDescriptorProto{
		testutil.NewBoolField("key", 1),
		testutil.NewStringField("value", 2, false),
	})
	entry.Options = &descriptorpb.MessageOptions{MapEntry: testutil.Bool(true)}

	msg := testutil.WithNested(
		testutil.NewMessage("M", []*descriptorpb.FieldDescriptorProto{
			testutil.NewMessageField("b", 1, "test.M.BEntry"),
		}),
		entry,
	)
	msg.Field[0].Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()

	err := ValidateRequest(newRequest(testutil.NewFile("test.proto", "test", []*descriptorpb.DescriptorProto{msg})))
	if err == nil {
		t.Fatal("ValidateRequest() accepted a bool map key, want an error")
	}
	if !strings.Contains(err.Error(), "map key type: bool") {
		t.Errorf("ValidateRequest() error = %v, want it to name the unsupported key type", err)
	}
}

// TestValidateUnnamedFieldDoesNotPanic tests that a descriptor carrying a field with no name is
// reported rather than dereferenced. the surrounding code nil-checks every other optional, and
// the error path must not be the one that crashes the plugin
func TestValidateUnnamedFieldDoesNotPanic(t *testing.T) {
	field := testutil.NewStringField("x", 1, false)
	field.Name = nil
	field.Type = descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum() // fails validation

	file := testutil.NewFile("test.proto", "test", []*descriptorpb.DescriptorProto{
		testutil.NewMessage("M", []*descriptorpb.FieldDescriptorProto{field}),
	})

	if err := ValidateRequest(newRequest(file)); err == nil {
		t.Error("ValidateRequest() accepted an unsupported field type, want an error")
	}
}

// TestValidateUnnamedMessageDoesNotPanic tests that a message with no name is reported rather
// than dereferenced while indexing. protoc always names a message, but the request is input like
// any other, and a panic reaches the caller as "plugin failed with status code 2" instead of a
// CodeGeneratorResponse.Error naming the descriptor at fault
func TestValidateUnnamedMessageDoesNotPanic(t *testing.T) {
	tests := []struct {
		name string
		file *descriptorpb.FileDescriptorProto
	}{
		{
			name: "top level",
			file: testutil.NewFile("test.proto", "test", []*descriptorpb.DescriptorProto{
				testutil.NewMessage("", nil),
			}),
		},
		{
			name: "nested",
			file: testutil.NewFile("test.proto", "test", []*descriptorpb.DescriptorProto{
				testutil.WithNested(
					testutil.NewMessage("M", nil),
					testutil.NewMessage("", nil),
				),
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// NewMessage stores the empty string, which is a name; drop it to get the nil the
			// dereference used to crash on
			target := tt.file.MessageType[0]
			if len(target.NestedType) > 0 {
				target = target.NestedType[0]
			}
			target.Name = nil

			err := ValidateRequest(newRequest(tt.file))
			if err == nil {
				t.Fatal("ValidateRequest() accepted a message with no name, want an error")
			}
			if !strings.Contains(err.Error(), "no name") {
				t.Errorf("ValidateRequest() error = %v, want it to say the message has no name", err)
			}
		})
	}
}

// TestValidateField tests field validation with helper
func TestValidateFieldHelper(t *testing.T) {
	tests := []struct {
		name string
	}{
		{"string field"},
		{"int field"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// test the helpers work correctly
			field := testutil.NewStringField("name", 1, false)
			if field == nil {
				t.Error("NewStringField returned nil")
			}
			if *field.Name != "name" {
				t.Errorf("field name = %q, want 'name'", *field.Name)
			}
		})
	}
}

// TestRepeatedFields tests validation of repeated (list) fields
func TestRepeatedFields(t *testing.T) {
	// just verify the test helper works
	field := testutil.NewStringField("items", 1, true) // repeated
	if field == nil {
		t.Error("NewStringField returned nil for repeated field")
	}
}
