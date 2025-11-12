# JSON Patch for Go

`jsonpatch` is a Go package that provides a complete and compliant implementation of JSON Patch (RFC 6902). It allows you to apply a sequence of operations to a JSON document.

This package works seamlessly with standard Go types for JSON, such as `map[string]any` and `[]any`, as produced by `encoding/json`. It relies on the `github.com/gogo-agent/jsonpointer` package for path resolution, ensuring correct and efficient navigation within the JSON document.

## Features

* **Full RFC 6902 Compliance**: Implements all standard operations: `add`, `remove`, `replace`, `move`, `copy`, and `test`.
* **Type-Safe**: Works with `any` type for documents, making it compatible with `encoding/json`.
* **Immutable by Default**: The default `Apply` function operates on a deep copy of the input document, ensuring the original document remains unchanged.
* **High-Performance In-Place Operations**: Provides an `ApplyInPlace` function for performance-critical scenarios that modifies the document directly.
* **Stream-Based Processing**: Includes an `ApplyStream` function for memory-efficient patching of large JSON documents.
* **Comprehensive Tests**: Includes the full test suite from RFC 6902 Appendix A.

## Installation

```bash
go get github.com/agentflare-ai/go-jsonpatch
```

Then import it in your code:

```go
import "github.com/agentflare-ai/go-jsonpatch"
```

## Usage

Here is a complete example of applying a patch to a JSON document:

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/agentflare-ai/go-jsonpatch"
)

func main() {
	// The original JSON document
	docJSON := `{"a":"b","c":"d"}`
	var document any
	if err := json.Unmarshal([]byte(docJSON), &document); err != nil {
		log.Fatal(err)
	}

	// The JSON Patch
	patchJSON := `[{"op":"replace","path":"/a","value":"e"}]`
	var patch jsonpatch.Patch
	if err := json.Unmarshal([]byte(patchJSON), &patch); err != nil {
		log.Fatal(err)
	}

	// Apply the patch, returning a new document
	modifiedDoc, err := jsonpatch.Apply(document, patch)
	if err != nil {
		log.Fatal(err)
	}

	// The original document remains unchanged

	// Print the result
	resultJSON, _ := json.MarshalIndent(modifiedDoc, "", "  ")
	fmt.Println(string(resultJSON))
	// Output:
	// {
	//   "a": "e",
	//   "c": "d"
	// }
}
```

### Generate a patch from two JSON values

You can compute a JSON Patch that transforms one JSON value into another:

```go
var a, b any
_ = json.Unmarshal([]byte(`{"a":1,"arr":["x","y"]}`), &a)
_ = json.Unmarshal([]byte(`{"a":1,"arr":["y","x","z"]}`), &b)

patch, err := jsonpatch.New(a, b)
if err != nil {
    log.Fatal(err)
}

out, err := jsonpatch.Apply(a, patch)
if err != nil {
    log.Fatal(err)
}
// out equals b
```

Notes:

* Arrays are diffed element-wise, with opportunistic move detection for unique elements. When duplicates are present, move detection is not guaranteed.
* Inputs can be `[]byte`, `json.RawMessage`, or Go values. All numbers are normalized to `float64` (encoding/json semantics).

## API Overview

* `type Op string`: Represents the patch operation type (e.g., `jsonpatch.Add`).
* `type Operation struct`: Represents a single operation with `Op`, `Path`, `From`, and `Value` fields.
* `type Patch []Operation`: A slice of operations that represents a full JSON Patch.
* `func Apply(document any, patch Patch) (any, error)`: Applies a patch to a document and returns a **new** modified document. The original document is not changed.
* `func ApplyInPlace(document any, patch Patch) (any, error)`: Applies a patch to a document **in-place**. This is faster but modifies the original document.
* `func ApplyStream(reader io.Reader, writer io.Writer, patch Patch) error`: Reads a JSON document from a stream, applies the patch, and writes the result to a stream.

## Extract additions (utility)

Extract values introduced by Add operations while also producing the remaining document without those additions.

```go
// After document (result of applying a patch)
after := []any{"a", "b", "c"}

// The patch that introduced the addition
patch := jsonpatch.Patch{
    {Op: jsonpatch.Add, Path: "/-", Value: "c"},
}

remaining, addedOnly, err := jsonpatch.ExtractAdded(after, patch)
if err != nil {
    log.Fatal(err)
}
// remaining == []any{"a","b"}
// addedOnly == []any{"c"}
```

Notes:

* Only Add operations are considered; other ops are ignored.
* Arrays: append via "-" is supported. Numeric indices refer to original positions only.
* Copy-on-write cloning is used for containers; leaf values are reused by reference.

## Supported Operations

This package supports all operations defined in RFC 6902:

* `add`: Adds a value to an object or inserts it into an array.
* `remove`: Removes a value from an object or array.
* `replace`: Replaces a value.
* `move`: Moves a value from one location to another.
* `copy`: Copies a value from one location to another.
* `test`: Tests that a value at a specified location is equal to a given value.

## Benchmarks

Benchmarks were run on an Apple M3 Max. The results show the performance of individual patch operations and a comparison between the default copying `Apply` and the mutating `ApplyInPlace`.

As expected, `ApplyInPlace` is significantly faster and performs fewer allocations as it does not perform a deep copy of the document before applying the patch.

```
goos: darwin
goarch: arm64
pkg: github.com/agentflare-ai/go-jsonpatch
cpu: Apple M3 Max
BenchmarkAdd_Object-16                    	  731515	      1606 ns/op	    1945 B/op	      42 allocs/op
BenchmarkAdd_Array-16                     	  686097	      1698 ns/op	    2041 B/op	      45 allocs/op
BenchmarkRemove_Object-16                 	  713197	      1615 ns/op	    1929 B/op	      41 allocs/op
BenchmarkRemove_Array-16                  	  674248	      1664 ns/op	    1969 B/op	      42 allocs/op
BenchmarkReplace_Simple-16                	  701826	      1640 ns/op	    1929 B/op	      41 allocs/op
BenchmarkReplace_Nested-16                	  672884	      1683 ns/op	    1961 B/op	      41 allocs/op
BenchmarkMove-16                          	  697226	      1670 ns/op	    1945 B/op	      42 allocs/op
BenchmarkCopy-16                          	  702669	      1667 ns/op	    1945 B/op	      41 allocs/op
BenchmarkTest_Success-16                  	  658363	      1686 ns/op	    1929 B/op	      42 allocs/op
BenchmarkTest_Failure-16                  	  617853	      1913 ns/op	    2123 B/op	      47 allocs/op
BenchmarkCombinedOperations_Copy-16       	  307822	      3820 ns/op	    3618 B/op	      87 allocs/op
BenchmarkCombinedOperations_InPlace-16    	  411802	      2875 ns/op	    2946 B/op	      63 allocs/op
PASS
ok  	github.com/agentflare-ai/go-jsonpatch	15.087s
```

## License

This project is licensed under the MIT License.
