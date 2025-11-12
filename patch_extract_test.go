package jsonpatch

import (
	"encoding/json"
	"testing"
)

func mustUnmarshal(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return v
}

func TestExtractAdded_ArrayAppendDash(t *testing.T) {
	after := mustUnmarshal(t, `["a","b","c"]`)
	patch := Patch{
		{Op: Add, Path: "/-", Value: "c"},
	}
	rem, add, err := ExtractAdded(after, patch)
	if err != nil {
		t.Fatalf("ExtractAdded error: %v", err)
	}
	remJSON, _ := json.Marshal(rem)
	addJSON, _ := json.Marshal(add)
	if string(remJSON) != `["a","b"]` {
		t.Fatalf("remaining mismatch: %s", remJSON)
	}
	if string(addJSON) != `["c"]` {
		t.Fatalf("addedOnly mismatch: %s", addJSON)
	}
	// Ensure after unchanged
	afterJSON, _ := json.Marshal(after)
	if string(afterJSON) != `["a","b","c"]` {
		t.Fatalf("after mutated: %s", afterJSON)
	}
}

func TestExtractAdded_ArrayNumericInsideBase(t *testing.T) {
	after := mustUnmarshal(t, `["a","x","b"]`)
	patch := Patch{
		{Op: Add, Path: "/1", Value: "x"},
	}
	rem, add, err := ExtractAdded(after, patch)
	if err != nil {
		t.Fatalf("ExtractAdded error: %v", err)
	}
	remJSON, _ := json.Marshal(rem)
	addJSON, _ := json.Marshal(add)
	if string(remJSON) != `["a","b"]` {
		t.Fatalf("remaining mismatch: %s", remJSON)
	}
	if string(addJSON) != `["x"]` {
		t.Fatalf("addedOnly mismatch: %s", addJSON)
	}
}

func TestExtractAdded_ObjectNested(t *testing.T) {
	after := mustUnmarshal(t, `{"a":{"b":{"c":1}}}`)
	patch := Patch{
		{Op: Add, Path: "/a/b/c", Value: 1},
	}
	rem, add, err := ExtractAdded(after, patch)
	if err != nil {
		t.Fatalf("ExtractAdded error: %v", err)
	}
	remJSON, _ := json.Marshal(rem)
	addJSON, _ := json.Marshal(add)
	if string(remJSON) != `{"a":{"b":{}}}` {
		t.Fatalf("remaining mismatch: %s", remJSON)
	}
	if string(addJSON) != `{"a":{"b":{"c":1}}}` {
		t.Fatalf("addedOnly mismatch: %s", addJSON)
	}
}

func TestExtractAdded_ObjectRepeatedKey_LastWins(t *testing.T) {
	after := mustUnmarshal(t, `{"x":2}`)
	patch := Patch{
		{Op: Add, Path: "/x", Value: 1},
		{Op: Add, Path: "/x", Value: 2},
	}
	rem, add, err := ExtractAdded(after, patch)
	if err != nil {
		t.Fatalf("ExtractAdded error: %v", err)
	}
	remJSON, _ := json.Marshal(rem)
	addJSON, _ := json.Marshal(add)
	if string(remJSON) != `{}` {
		t.Fatalf("remaining mismatch: %s", remJSON)
	}
	if string(addJSON) != `{"x":2}` {
		t.Fatalf("addedOnly mismatch: %s", addJSON)
	}
}

func TestExtractAdded_ErrRootAdd(t *testing.T) {
	after := mustUnmarshal(t, `{"a":1}`)
	patch := Patch{
		{Op: Add, Path: "", Value: map[string]any{"b": 2}},
	}
	_, _, err := ExtractAdded(after, patch)
	if err == nil {
		t.Fatalf("expected error for root-level add")
	}
}

func TestExtractAdded_ErrMissingParent(t *testing.T) {
	after := mustUnmarshal(t, `{"z":1}`)
	patch := Patch{
		{Op: Add, Path: "/a/b", Value: 1},
	}
	_, _, err := ExtractAdded(after, patch)
	if err == nil {
		t.Fatalf("expected error for missing parent")
	}
}


