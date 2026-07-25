# example makefile showing how to use the protoc-gen-struct plugin
# build the plugin first (go build -o protoc-gen-struct ./ from the repo root)

PLUGIN ?= ../protoc-gen-struct
PROTO  := example.proto
PROTOC := protoc --plugin=protoc-gen-struct=$(PLUGIN) -I.

.PHONY: all go cpp py ts clean

all: go cpp py ts

go:
	@mkdir -p output/go
	$(PROTOC) --struct_out=output/go --struct_opt=go $(PROTO)

# requires nlohmann/json and yaml-cpp headers to compile
cpp:
	@mkdir -p output/cpp
	$(PROTOC) --struct_out=output/cpp --struct_opt=c++ $(PROTO)

py:
	@mkdir -p output/py
	$(PROTOC) --struct_out=output/py --struct_opt=python $(PROTO)

ts:
	@mkdir -p output/ts
	$(PROTOC) --struct_out=output/ts --struct_opt=typescript $(PROTO)

clean:
	rm -rf output
