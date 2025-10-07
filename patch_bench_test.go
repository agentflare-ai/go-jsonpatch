package jsonpatch_test

import (
	"encoding/json"
	"testing"

	"github.com/agentflare-ai/go-jsonpatch"
)

var baseDoc = `{
	"foo": "bar",
	"baz": ["qux", "quux"],
	"a": {
		"b": {
			"c": "hello"
		}
	},
	"d": null
}`

func runBenchmark(b *testing.B, docStr string, patchStr string) {
	var doc any
	if err := json.Unmarshal([]byte(docStr), &doc); err != nil {
		b.Fatalf("Failed to unmarshal document: %v", err)
	}

	var patch jsonpatch.Patch
	if err := json.Unmarshal([]byte(patchStr), &patch); err != nil {
		b.Fatalf("Failed to unmarshal patch: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := jsonpatch.Apply(doc, patch)
		if err != nil {
			b.Fatalf("Apply failed: %v", err)
		}
	}
}

func BenchmarkAdd_Object(b *testing.B) {
	runBenchmark(b, baseDoc, `[{"op": "add", "path": "/foo2", "value": "bar2"}]`)
}

func BenchmarkAdd_Array(b *testing.B) {
	runBenchmark(b, baseDoc, `[{"op": "add", "path": "/baz/1", "value": "new"}]`)
}

func BenchmarkRemove_Object(b *testing.B) {
	runBenchmark(b, baseDoc, `[{"op": "remove", "path": "/foo"}]`)
}

func BenchmarkRemove_Array(b *testing.B) {
	runBenchmark(b, baseDoc, `[{"op": "remove", "path": "/baz/0"}]`)
}

func BenchmarkReplace_Simple(b *testing.B) {
	runBenchmark(b, baseDoc, `[{"op": "replace", "path": "/foo", "value": "baz"}]`)
}

func BenchmarkReplace_Nested(b *testing.B) {
	runBenchmark(b, baseDoc, `[{"op": "replace", "path": "/a/b/c", "value": "world"}]`)
}

func BenchmarkMove(b *testing.B) {
	runBenchmark(b, baseDoc, `[{"op": "move", "from": "/foo", "path": "/foo2"}]`)
}

func BenchmarkCopy(b *testing.B) {
	runBenchmark(b, baseDoc, `[{"op": "copy", "from": "/a/b", "path": "/a/d"}]`)
}

func BenchmarkTest_Success(b *testing.B) {
	runBenchmark(b, baseDoc, `[{"op": "test", "path": "/foo", "value": "bar"}]`)
}

func BenchmarkTest_Failure(b *testing.B) {
	// Note: This benchmarks a failing test operation.
	// The error path might have different performance characteristics.
	var doc any
	if err := json.Unmarshal([]byte(baseDoc), &doc); err != nil {
		b.Fatalf("Failed to unmarshal document: %v", err)
	}

	var patch jsonpatch.Patch
	patchStr := `[{"op": "test", "path": "/foo", "value": "wrong"}]`
	if err := json.Unmarshal([]byte(patchStr), &patch); err != nil {
		b.Fatalf("Failed to unmarshal patch: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := jsonpatch.Apply(doc, patch)
		if err == nil {
			b.Fatalf("Expected an error but got none")
		}
	}
}

func BenchmarkCombinedOperations_Copy(b *testing.B) {
	doc := `{
		"metadata": {
			"id": "12345",
			"version": 1.0,
			"tags": ["alpha", "beta"]
		},
		"data": {
			"items": [
				{"name": "item1", "value": 100},
				{"name": "item2", "value": 200}
			]
		}
	}`
	patch := `[
		{"op": "replace", "path": "/metadata/version", "value": 1.1},
		{"op": "add", "path": "/data/items/1", "value": {"name": "item1.5", "value": 150}},
		{"op": "remove", "path": "/metadata/tags"},
		{"op": "test", "path": "/data/items/0/name", "value": "item1"},
		{"op": "copy", "from": "/data/items/2", "path": "/data/items/0/copy"},
		{"op": "move", "from": "/data/items/0", "path": "/data/items/1"}
	]`
	runBenchmark(b, doc, patch)
}

func BenchmarkCombinedOperations_InPlace(b *testing.B) {
	docStr := `{
		"metadata": {
			"id": "12345",
			"version": 1.0,
			"tags": ["alpha", "beta"]
		},
		"data": {
			"items": [
				{"name": "item1", "value": 100},
				{"name": "item2", "value": 200}
			]
		}
	}`
	patchStr := `[
		{"op": "replace", "path": "/metadata/version", "value": 1.1},
		{"op": "add", "path": "/data/items/1", "value": {"name": "item1.5", "value": 150}},
		{"op": "remove", "path": "/metadata/tags"},
		{"op": "test", "path": "/data/items/0/name", "value": "item1"},
		{"op": "copy", "from": "/data/items/2", "path": "/data/items/0/copy"},
		{"op": "move", "from": "/data/items/0", "path": "/data/items/1"}
	]`

	var patch jsonpatch.Patch
	if err := json.Unmarshal([]byte(patchStr), &patch); err != nil {
		b.Fatalf("Failed to unmarshal patch: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// We need to unmarshal the doc inside the loop to have a fresh copy
		// for the in-place modification, otherwise we'd be patching a patched doc.
		var doc any
		if err := json.Unmarshal([]byte(docStr), &doc); err != nil {
			b.Fatalf("Failed to unmarshal document: %v", err)
		}
		_, err := jsonpatch.ApplyInPlace(doc, patch)
		if err != nil {
			b.Fatalf("ApplyInPlace failed: %v", err)
		}
	}
}
