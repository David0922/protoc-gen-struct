# protoc-gen-struct

A minimal protoc plugin that generates struct definitions and serialization helpers for multiple languages (C++, Go, Python, TypeScript).

## Features

- **Minimal code generation**: Generates only essential struct/class/interface definitions
- **Multi-language support**: C++, Go, Python, TypeScript
- **Serialization helpers**: JSON and YAML serialization/deserialization functions
- **Interoperable output**: all four languages serialize under the proto field name, so output
  written by one is readable by the others
- **Optional fields**: proto3 `optional` maps to each language's idiomatic presence type
- **Cross-file imports**: a type from an imported proto is imported and qualified, not renamed
- **Type validation**: Rejects unsupported types and circular dependencies

## Supported Types

- Primitives: `bool`, all integer types (`int32`, `int64`, `uint32`, `uint64`, `sint32`, `sint64`, `fixed32`, `fixed64`, `sfixed32`, `sfixed64`)
- Floating point: `float`, `double`
- String: `string`
- Collections: `repeated` (lists), `map`
- Messages: nested message types, and messages from imported proto files
- Presence: proto3 `optional` on singular fields

## Output Layout

One output file is produced per input proto, keeping the source directory: `a/b/msg.proto`
becomes `a/b/msg.go`.

Messages are emitted in dependency order rather than declaration order, so a message always
follows every message it holds by value. Nested messages are flattened into file scope under a
qualified name — `User.Inner` becomes `User_Inner` — so two messages that each nest an `Inner`
stay distinct.

## Optional Fields

A field declared `optional` in proto3 is generated so that "unset" is distinguishable from "set to
the zero value":

| Language   | Declaration                                  | Behaviour                                 |
| ---------- | -------------------------------------------- | ----------------------------------------- |
| C++        | `std::optional<T> x;`                        | unset fields are skipped when serializing |
| Go         | `X *T` with `omitempty` in the JSON/YAML tag | a nil pointer is omitted from the output  |
| Python     | `x: T \| None = None`                        | `None` represents an unset field          |
| TypeScript | `x?: T`                                      | the property may be left off entirely     |

```proto
message Person {
  string first_name = 1;
  optional string middle_name = 2;
}
```

```go
type Person struct {
	FirstName string `json:"first_name" yaml:"first_name"`
	MiddleName *string `json:"middle_name,omitempty" yaml:"middle_name,omitempty"`
}
```

```cpp
struct Person {
  std::string first_name;
  std::optional<std::string> middle_name;
};
```

```python
@dataclass
class Person:
    first_name: str = ""
    middle_name: str | None = None
```

```typescript
export interface Person {
  first_name: string;
  middle_name?: string;
}
```

Go uses a pointer rather than a bare `omitempty` because `omitempty` alone cannot express
presence: it treats a field explicitly set to its zero value exactly like an unset one, and on a
struct field it has no effect at all.

`optional` applies to singular fields only — proto3 does not allow it on `repeated` fields or
maps, which already represent absence as "empty".

## Serialized Field Names

All four generators serialize under the proto field name (`first_name`), so JSON or YAML written
by one can be read by the others. The Go struct tags, the C++ and Python member names, and the
TypeScript interface properties all use it directly.

A proto field whose name is a reserved word in the target language — `from` and `class` are both
legal proto field names — is renamed in the generated code by appending an underscore
(`class_`). Only the member is renamed; the serialized key stays the proto field name, so this
never affects the wire format.

Reading is lenient in every language: a field the writer left out takes its zero value, an
explicit `null` is treated as absent, and an empty list or map is interchangeable with a missing
one. That is what makes output from any one generator loadable by the other three, since they do
not all make the same choice about emitting empty values.

Integer map keys are written as strings in JSON (as JSON requires) and converted back to integers
when read, so `map<int64, T>` round-trips as the declared type rather than degrading to strings.
TypeScript is the exception the language forces: a JavaScript object key is always a string, so
the generated type is `Record<number, T>` and indexing by a number works, but `Object.keys` still
hands back strings.

## Unsupported Types

Each of these is refused with an error rather than generated incorrectly:

- `enum` — use integers instead
- `bytes` — not supported
- `oneof` — would need a tagged union to be represented faithfully
- `map` with a `bool` key — Go's `encoding/json` cannot encode one, so it could never round-trip
- circular message references
- any syntax other than `proto3`

RPC service definitions are ignored; only messages produce output.

Well-known types (`google.protobuf.Timestamp`, `Duration`, `Struct`, and the rest) are not
special-cased. They live in protos this plugin is not generating, so a field using one is refused
by the rule below — use an `int64` unix timestamp or a string instead.

## Referring To Other Proto Files

A type from an imported proto is imported and qualified rather than renamed, but that only works
when the imported file is being generated too — the import points at _this plugin's_ output for
it. Pass every proto in the dependency closure to the same `protoc` invocation:

```bash
protoc --struct_out=. --struct_opt=go a/msg.proto common/types.proto
```

Referring to a type from a file that is not being generated is an error naming the file, rather
than an import of a module that does not exist.

## Installation

```bash
go install ./...
```

Or build manually:

```bash
go build -o protoc-gen-struct
```

Then make the binary available to protoc (e.g., add to `$PATH`).

## Usage

```bash
protoc --go_out=. --go_opt=paths=source_relative \
       --struct_out=. --struct_opt=go \
       example.proto
```

For other languages, replace `--struct_opt=go` with:

- `--struct_opt=c++` for C++
- `--struct_opt=python` for Python
- `--struct_opt=typescript` for TypeScript

## Project Structure

```
.
├── main.go                          # plugin entry point
├── generator/
│   ├── generator.go                 # high-level code generation
│   └── language/
│       ├── types.go                 # type definitions and helpers
│       ├── go.go                    # Go code generator
│       ├── cpp.go                   # C++ code generator
│       ├── python.go                # Python code generator
│       └── typescript.go            # TypeScript code generator
├── validator/
│   └── validator.go                 # type validation and circular dependency detection
├── testutil/
│   └── testutil.go                  # test helpers
└── README.md
```

## Generated Code Examples

### Go

```go
package testpkg

import (
	"encoding/json"

	"go.yaml.in/yaml/v4"
)

type Person struct {
	FirstName string `json:"first_name" yaml:"first_name"`
	Age int32 `json:"age" yaml:"age"`
}

func (x *Person) ToJSON() ([]byte, error) {
	return json.Marshal(x)
}

func (x *Person) FromJSON(data []byte) error {
	*x = Person{}
	return json.Unmarshal(data, x)
}
```

### C++

```cpp
#ifndef PROTO_TEST_C1C986BD_H_
#define PROTO_TEST_C1C986BD_H_

#include <string>
#include <vector>
#include <map>
#include <optional>
#include <nlohmann/json.hpp>
#include <yaml-cpp/yaml.h>

namespace testpkg {

struct Person;

inline void to_json(nlohmann::json& j, const Person& value);
inline void from_json(const nlohmann::json& j, Person& value);

} // namespace testpkg

namespace YAML {

template <>
struct convert<::testpkg::Person> {
  static Node encode(const ::testpkg::Person& value);
  static bool decode(const Node& node, ::testpkg::Person& value);
};

} // namespace YAML

namespace testpkg {

struct Person {
  std::string first_name;
  int32_t age;

  nlohmann::json ToJSON() const {
    // ...
  }

  void FromJSON(const nlohmann::json& j) {
    // ...
  }
};

inline void to_json(nlohmann::json& j, const Person& value) {
  j = value.ToJSON();
}

// ... (from_json, and the YAML::convert definitions, follow)

} // namespace testpkg
#endif // PROTO_TEST_C1C986BD_H_
```

The `to_json`/`from_json` overloads and the `YAML::convert` specialization are what let a struct
be nested inside another: both libraries look those up by name when serializing a member, and
neither can see the member functions on its own.

### Python

```python
from __future__ import annotations

from dataclasses import dataclass, field
import json
import yaml
from typing import Any, Dict, List

@dataclass
class Person:
    first_name: str = ""
    age: int = 0

    def to_dict(self) -> Dict[str, Any]:
        data: Dict[str, Any] = {}
        data["first_name"] = self.first_name
        data["age"] = self.age
        return data

    @classmethod
    def from_dict(cls, data: Dict[str, Any] | None) -> 'Person':
        return cls(
            first_name=data.get("first_name") if data.get("first_name") is not None else "",
            age=data.get("age") if data.get("age") is not None else 0,
        )

    def to_json(self) -> str:
        return json.dumps(self.to_dict())

    @classmethod
    def from_json(cls, data: str) -> 'Person':
        parsed: Dict[str, Any] | None = json.loads(data)
        return cls.from_dict(parsed)
```

Serialization goes through `to_dict`/`from_dict` rather than `__dict__`: `json.dumps` cannot
encode a nested dataclass, and `yaml.dump` would write a `!!python/object:` tag that the
generated `from_yaml` refuses to read back.

### TypeScript

```typescript
import * as yaml from "yaml";

export interface Person {
  first_name: string;
  age: number;
}

export namespace Person {
  export function toJSON(obj: Person): string {
    return JSON.stringify(obj);
  }

  export function fromJSON(data: string): Person {
    return fromObject(JSON.parse(data));
  }

  // ... (toYAML and fromYAML follow)

  export function fromObject(data: unknown): Person {
    const source = (data ?? {}) as Record<string, unknown>;
    const result: Person = {
      first_name: ((source["first_name"] ?? "") as string),
      age: ((source["age"] ?? 0) as number),
    };
    return result;
  }
}
```

Both decoders go through `fromObject` rather than casting the parsed document: a cast asserts
every property is present without making it so, which left `fromJSON("{}")` returning an object
whose fields were all `undefined`. Rebuilding property by property gives the same zero values the
other three generators produce.

## Types From Another Proto File

A message defined in an imported proto is generated into that file's own output, so referring to
it emits an import:

| Language   | Import                                     | Reference           |
| ---------- | ------------------------------------------ | ------------------- |
| Go         | `commonpb "example.com/gen/common"`        | `commonpb.Address`  |
| C++        | `#include "common/types.h"`                | `::common::Address` |
| Python     | `import common.types as types`             | `types.Address`     |
| TypeScript | `import * as types from '../common/types'` | `types.Address`     |

Go is the one language whose import needs a module path, and `go_package` is the only place a
descriptor carries one. Generating Go for a file that references a proto without a `go_package`
option is an error rather than a silently undefined type.

Python imports the generated module by its path, so a proto whose path contains a segment that is
not a Python identifier (`my-pkg/types.proto`) cannot be imported by another generated module.
That is reported when it would matter; the other three languages are unaffected.

## Limitations

- Generated Go is not gofmt-formatted; run `gofmt -w` over the output if that matters
- Python module imports assume the output root is on `sys.path`
- C++ writes an empty `repeated`/`map` field as `[]`/`{}` while Go omits it; every generator
  reads both, so this only shows up when comparing encoded bytes

## todo

- switch to a faster json/yaml library for C++
- support `oneof` as a tagged union rather than rejecting it
