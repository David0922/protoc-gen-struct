package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"

	"protoc-gen-struct/generator"
	"protoc-gen-struct/validator"
)

// main entry point for the protoc plugin. reads a CodeGeneratorRequest from stdin, processes it, and writes a
// CodeGeneratorResponse to stdout
func main() {
	// read the entire stdin as the serialized CodeGeneratorRequest
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("failed to read stdin: %v", err)
	}

	// unmarshal the protobuf request
	var req pluginpb.CodeGeneratorRequest
	if err := proto.Unmarshal(input, &req); err != nil {
		log.Fatalf("failed to unmarshal request: %v\n", err)
	}

	// generate code
	resp, err := generateCode(&req)
	if err != nil {
		// return error response to protoc
		resp = &pluginpb.CodeGeneratorResponse{
			Error: proto.String(err.Error()),
		}
	}

	// marshal and write response
	output, err := proto.Marshal(resp)
	if err != nil {
		log.Fatalf("failed to marshal response: %v\n", err)
	}

	if _, err := os.Stdout.Write(output); err != nil {
		log.Fatalf("failed to write stdout: %v\n", err)
	}
}

// generateCode processes the CodeGeneratorRequest and generates code for all requested files
// validates all messages first, then generates code for each language
//
// req: the CodeGeneratorRequest from protoc
//
// resp: the generated CodeGeneratorResponse
// err: validation or generation errors (unsupported types, circular dependencies, unknown languages)
func generateCode(req *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
	// validate all input files
	if err := validator.ValidateRequest(req); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// determine target language from plugin parameter
	lang := extractLanguage(req.Parameter)
	if lang == "" {
		return nil, fmt.Errorf("language not specified in parameter")
	}

	// generate code for each requested file
	//
	// declaring FEATURE_PROTO3_OPTIONAL is what makes `optional` fields usable: protoc refuses to
	// hand a descriptor containing one to a plugin that has not advertised the feature, failing
	// with "plugin does not support the 'optional' label". it also means we accept the synthetic
	// one-field oneof protoc wraps each such field in
	resp := &pluginpb.CodeGeneratorResponse{
		SupportedFeatures: proto.Uint64(uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)),
	}

	// filters req.ProtoFile down to only the files protoc asked you to generate
	//
	// in a CodeGeneratorRequest:
	//
	// FileToGenerate: string names of .proto files the user put on the command line. generate code only for these
	// ProtoFile: full FileDescriptorProtos for those files plus every transitive import
	//
	// example:
	//
	// protoc --struct_out=lang=go:. user.proto
	//
	// if user.proto imports common.proto, which imports google/protobuf/timestamp.proto, then:
	//
	// | field          | contents                                                  |
	// | -------------- | --------------------------------------------------------- |
	// | FileToGenerate | ["user.proto"]                                            |
	// | ProtoFile      | descriptors for timestamp.proto, common.proto, user.proto |
	//
	// we need the imports in ProtoFile so we can resolve types (common.Address, etc.)
	// we should not emit generated code for them. that's another plugin's / another invocation's job
	for _, protoFile := range req.ProtoFile {
		// check if this file was requested
		if protoFile.Name == nil {
			return nil, fmt.Errorf("proto file has no name")
		}
		isRequested := false
		for _, f := range req.FileToGenerate {
			if f == *protoFile.Name {
				isRequested = true
				break
			}
		}
		if !isRequested {
			continue
		}

		// generate code based on language
		content, filename, err := generator.Generate(protoFile, req.ProtoFile, req.FileToGenerate, lang)
		if err != nil {
			return nil, fmt.Errorf("failed to generate %s for %s: %w", lang, *protoFile.Name, err)
		}

		resp.File = append(resp.File, &pluginpb.CodeGeneratorResponse_File{
			Name:    proto.String(filename),
			Content: proto.String(content),
		})
	}

	return resp, nil
}

// extractLanguage extracts the target language from the plugin parameter
// the parameter format is expected to be a language name (c++, go, python, typescript)
//
// the parameter is a comma separated option list, so both a bare language ("go") and an
// explicit key ("lang=go", possibly alongside other options) are accepted. splitting on "="
// unconditionally would turn "lang=go" into the language "lang"
//
// param: the plugin parameter string
//
// language name or empty string if not specified
func extractLanguage(param *string) string {
	if param == nil || *param == "" {
		return ""
	}

	lang := ""
	for _, opt := range strings.Split(*param, ",") {
		opt = strings.TrimSpace(opt)
		if opt == "" {
			continue
		}

		key, value, hasValue := strings.Cut(opt, "=")
		switch {
		case hasValue && (key == "lang" || key == "language"):
			// explicit key always wins
			return value
		case !hasValue && lang == "":
			// bare option: treat the first one as the language
			lang = opt
		}
	}

	return lang
}
