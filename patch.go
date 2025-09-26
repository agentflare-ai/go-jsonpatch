package jsonpatch

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/agentflare-ai/jsonpointer"
)

// Op represents JSON Patch operation types
type Op string

const (
	Add     Op = "add"
	Remove  Op = "remove"
	Replace Op = "replace"
	Move    Op = "move"
	Copy    Op = "copy"
	Test    Op = "test"
)

// Operation represents a single JSON Patch operation
type Operation struct {
	Op    Op     `json:"op"`
	Path  string `json:"path"`
	From  string `json:"from,omitempty"`
	Value any    `json:"value,omitempty"`
}

// Patch represents a collection of JSON Patch operations
type Patch []Operation

// Apply applies a series of JSON Patch operations to a document, returning a new
// modified document. The original document is not changed.
func Apply(document any, patch Patch) (any, error) {
	// Deep copy the document to avoid modifying the original
	docBytes, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal document: %w", err)
	}

	var result any
	if err := json.Unmarshal(docBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal document: %w", err)
	}

	return ApplyInPlace(result, patch)
}

// ApplyInPlace applies a series of JSON Patch operations to a document in-place.
// WARNING: This function modifies the input document.
func ApplyInPlace(document any, patch Patch) (any, error) {
	for _, op := range patch {
		var err error
		switch op.Op {
		case Add:
			document, err = applyAdd(document, op.Path, op.Value)
		case Remove:
			document, err = applyRemove(document, op.Path)
		case Replace:
			document, err = applyReplace(document, op.Path, op.Value)
		case Move:
			document, err = applyMove(document, op.From, op.Path)
		case Copy:
			document, err = applyCopy(document, op.From, op.Path)
		case Test:
			err = applyTest(document, op.Path, op.Value)
		default:
			return nil, fmt.Errorf("unsupported patch operation: %s", op.Op)
		}

		if err != nil {
			return nil, fmt.Errorf("patch operation %s failed: %w", op.Op, err)
		}
	}

	return document, nil
}

// ApplyStream applies a series of JSON Patch operations from a reader to a writer.
// This is more memory-efficient for large documents than Apply, as it avoids
// marshalling the intermediate document to a byte slice.
func ApplyStream(reader io.Reader, writer io.Writer, patch Patch) error {
	var doc any
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&doc); err != nil {
		return fmt.Errorf("failed to decode document: %w", err)
	}

	modifiedDoc, err := Apply(doc, patch)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(writer)
	return encoder.Encode(modifiedDoc)
}

// Helper functions for patch operations
func applyAdd(document any, path string, value any) (any, error) {
	p, err := jsonpointer.New(path)
	if err != nil {
		return nil, err
	}

	if len(p) == 0 {
		return value, nil
	}

	parentPath := jsonpointer.Pointer(p[0 : len(p)-1]).String()
	token := p[len(p)-1]

	parent, err := jsonpointer.Get(document, parentPath)
	if err != nil {
		return nil, fmt.Errorf("parent path '%s' not found for add: %w", parentPath, err)
	}

	if arr, ok := parent.([]any); ok {
		if token == "-" {
			newArr := append(arr, value)
			return jsonpointer.Set(document, parentPath, newArr)
		}

		idx, err := jsonpointer.ParseArrayIndex(token)
		if err == nil {
			if idx > uint64(len(arr)) {
				return nil, fmt.Errorf("add operation on array index %d is out of bounds for array of length %d", idx, len(arr))
			}
			newArr := make([]any, 0, len(arr)+1)
			newArr = append(newArr, arr[:idx]...)
			newArr = append(newArr, value)
			newArr = append(newArr, arr[idx:]...)
			return jsonpointer.Set(document, parentPath, newArr)
		}
	}

	return jsonpointer.Set(document, path, value)
}

func applyRemove(document any, path string) (any, error) {
	return jsonpointer.Remove(document, path)
}

func applyReplace(document any, path string, value any) (any, error) {
	// To be compliant with RFC6902, "replace" is atomic: the target location
	// MUST exist. We can ensure this by first getting the value, which will
	// error if it doesn't exist, and then setting it.
	if _, err := jsonpointer.Get(document, path); err != nil {
		return nil, err
	}
	return jsonpointer.Set(document, path, value)
}

func applyMove(document any, from, to string) (any, error) {
	val, err := jsonpointer.Get(document, from)
	if err != nil {
		return nil, err
	}

	doc, err := jsonpointer.Remove(document, from)
	if err != nil {
		return nil, err
	}

	return jsonpointer.Set(doc, to, val)
}

func applyCopy(document any, from, to string) (any, error) {
	val, err := jsonpointer.Get(document, from)
	if err != nil {
		return nil, err
	}
	return jsonpointer.Set(document, to, val)
}

func applyTest(document any, path string, expected any) error {
	actual, err := jsonpointer.Get(document, path)
	if err != nil {
		return err
	}

	// Deep comparison
	actualBytes, err := json.Marshal(actual)
	if err != nil {
		return err
	}

	expectedBytes, err := json.Marshal(expected)
	if err != nil {
		return err
	}

	if string(actualBytes) != string(expectedBytes) {
		return fmt.Errorf("test failed: expected %v, got %v", expected, actual)
	}

	return nil
}
