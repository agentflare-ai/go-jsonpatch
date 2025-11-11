package jsonpatch

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/agentflare-ai/go-jsonpointer"
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

// Delta represents a single path change captured during Prepare.
// Op is one of RFC6902 ops we materialize into deltas: add, remove, replace.
// Move/copy expand into one or more add/remove deltas during preparation.
type Delta struct {
	Path          string `json:"path"`
	Op            Op     `json:"op"`
	Before        any    `json:"before,omitempty"`
	After         any    `json:"after,omitempty"`
	ExistedBefore bool   `json:"existed_before"`
	ExistedAfter  bool   `json:"existed_after"`
}

// Diff encapsulates ordered deltas and precompiled forward/reverse patches.
type Diff struct {
	Deltas  []Delta `json:"deltas"`
	forward Patch   `json:"-"`
	reverse Patch   `json:"-"`
}

// Apply reproduces the patch effect on document using captured deltas.
func (d Diff) Apply(document any) (any, error) {
	return ApplyInPlace(document, d.forward)
}

// Revert undoes the effect on document using captured deltas (reverse order).
func (d Diff) Revert(document any) (any, error) {
	return ApplyInPlace(document, d.reverse)
}

func isRootPath(path string) bool {
	p, err := jsonpointer.New(path)
	if err != nil {
		return false
	}
	return len(p) == 0
}

// isParentArray is no longer needed since reverse is precompiled in Prepare.

// Prepare builds a Diff by simulating applying patch to original without mutating original.
// The returned Diff captures concrete, reproducible deltas (including resolving "-" array paths)
// that can be applied to reproduce the patch effect or reverted to undo it.
func Prepare(original any, patch Patch) (Diff, error) {
	// Work on a deep copy so the caller's document is not modified
	docCopy, err := deepCopyAny(original)
	if err != nil {
		return Diff{}, fmt.Errorf("failed to deepcopy original: %w", err)
	}

	var deltas []Delta

	for _, op := range patch {
		switch op.Op {
		case Add:
			// Resolve concrete path (handle "-" for arrays)
			resolvedPath, err := resolveConcreteAddPath(docCopy, op.Path)
			if err != nil {
				return Diff{}, fmt.Errorf("add resolve path failed: %w", err)
			}
			existedBefore, beforeVal, err := tryGetDeep(docCopy, resolvedPath)
			if err != nil {
				return Diff{}, fmt.Errorf("add read before failed: %w", err)
			}
			afterVal, err := deepCopyAny(op.Value)
			if err != nil {
				return Diff{}, fmt.Errorf("add deepcopy value failed: %w", err)
			}
			deltas = append(deltas, Delta{
				Path:          resolvedPath,
				Op:            Add,
				Before:        beforeVal,
				After:         afterVal,
				ExistedBefore: existedBefore,
				ExistedAfter:  true,
			})

			// Apply to working document using the original (possibly "-"-containing) path
			docCopy, err = applyAdd(docCopy, op.Path, op.Value)
			if err != nil {
				return Diff{}, fmt.Errorf("apply add failed: %w", err)
			}

		case Remove:
			// Capture existing value
			beforeValRaw, err := jsonpointer.Get(docCopy, op.Path)
			if err != nil {
				return Diff{}, fmt.Errorf("remove get before failed: %w", err)
			}
			beforeVal, err := deepCopyAny(beforeValRaw)
			if err != nil {
				return Diff{}, fmt.Errorf("remove deepcopy failed: %w", err)
			}
			deltas = append(deltas, Delta{
				Path:          op.Path,
				Op:            Remove,
				Before:        beforeVal,
				ExistedBefore: true,
				ExistedAfter:  false,
			})

			docCopy, err = applyRemove(docCopy, op.Path)
			if err != nil {
				return Diff{}, fmt.Errorf("apply remove failed: %w", err)
			}

		case Replace:
			// Replace must exist; capture before and after
			beforeValRaw, err := jsonpointer.Get(docCopy, op.Path)
			if err != nil {
				return Diff{}, fmt.Errorf("replace get before failed: %w", err)
			}
			beforeVal, err := deepCopyAny(beforeValRaw)
			if err != nil {
				return Diff{}, fmt.Errorf("replace deepcopy before failed: %w", err)
			}
			afterVal, err := deepCopyAny(op.Value)
			if err != nil {
				return Diff{}, fmt.Errorf("replace deepcopy after failed: %w", err)
			}
			deltas = append(deltas, Delta{
				Path:          op.Path,
				Op:            Replace,
				Before:        beforeVal,
				After:         afterVal,
				ExistedBefore: true,
				ExistedAfter:  true,
			})

			docCopy, err = applyReplace(docCopy, op.Path, op.Value)
			if err != nil {
				return Diff{}, fmt.Errorf("apply replace failed: %w", err)
			}

		case Move:
			// Move is copy then remove with respect to deltas (capture using pre-state)
			valRaw, err := jsonpointer.Get(docCopy, op.From)
			if err != nil {
				return Diff{}, fmt.Errorf("move get source failed: %w", err)
			}
			valCopy, err := deepCopyAny(valRaw)
			if err != nil {
				return Diff{}, fmt.Errorf("move deepcopy source failed: %w", err)
			}
			resolvedDest, err := resolveConcreteAddPath(docCopy, op.Path)
			if err != nil {
				return Diff{}, fmt.Errorf("move resolve dest failed: %w", err)
			}
			destExisted, destBefore, err := tryGetDeep(docCopy, resolvedDest)
			if err != nil {
				return Diff{}, fmt.Errorf("move get dest before failed: %w", err)
			}

			// Add at destination first
			deltas = append(deltas, Delta{
				Path:          resolvedDest,
				Op:            Add,
				Before:        destBefore,
				After:         valCopy,
				ExistedBefore: destExisted,
				ExistedAfter:  true,
			})
			// Then remove from source
			deltas = append(deltas, Delta{
				Path:          op.From,
				Op:            Remove,
				Before:        valCopy,
				ExistedBefore: true,
				ExistedAfter:  false,
			})

			docCopy, err = applyMove(docCopy, op.From, op.Path)
			if err != nil {
				return Diff{}, fmt.Errorf("apply move failed: %w", err)
			}

		case Copy:
			valRaw, err := jsonpointer.Get(docCopy, op.From)
			if err != nil {
				return Diff{}, fmt.Errorf("copy get source failed: %w", err)
			}
			valCopy, err := deepCopyAny(valRaw)
			if err != nil {
				return Diff{}, fmt.Errorf("copy deepcopy source failed: %w", err)
			}
			resolvedDest, err := resolveConcreteAddPath(docCopy, op.Path)
			if err != nil {
				return Diff{}, fmt.Errorf("copy resolve dest failed: %w", err)
			}
			destExisted, destBefore, err := tryGetDeep(docCopy, resolvedDest)
			if err != nil {
				return Diff{}, fmt.Errorf("copy get dest before failed: %w", err)
			}

			deltas = append(deltas, Delta{
				Path:          resolvedDest,
				Op:            Add,
				Before:        destBefore,
				After:         valCopy,
				ExistedBefore: destExisted,
				ExistedAfter:  true,
			})

			docCopy, err = applyCopy(docCopy, op.From, op.Path)
			if err != nil {
				return Diff{}, fmt.Errorf("apply copy failed: %w", err)
			}

		case Test:
			if err := applyTest(docCopy, op.Path, op.Value); err != nil {
				return Diff{}, fmt.Errorf("test failed: %w", err)
			}
			// No delta recorded
		default:
			return Diff{}, fmt.Errorf("unsupported patch operation in prepare: %s", op.Op)
		}
	}

	// Precompile forward and reverse patches from the collected deltas
	var forward Patch
	for _, delta := range deltas {
		switch delta.Op {
		case Add:
			forward = append(forward, Operation{Op: Add, Path: delta.Path, Value: delta.After})
		case Remove:
			forward = append(forward, Operation{Op: Remove, Path: delta.Path})
		case Replace:
			forward = append(forward, Operation{Op: Replace, Path: delta.Path, Value: delta.After})
		default:
			return Diff{}, fmt.Errorf("unsupported delta op in forward compile: %s", delta.Op)
		}
	}
	var reverse Patch
	for i := len(deltas) - 1; i >= 0; i-- {
		delta := deltas[i]
		if isRootPath(delta.Path) {
			// Root always restored via replace with Before
			reverse = append(reverse, Operation{Op: Replace, Path: "", Value: delta.Before})
			continue
		}
		switch delta.Op {
		case Add:
			if delta.ExistedBefore {
				reverse = append(reverse, Operation{Op: Replace, Path: delta.Path, Value: delta.Before})
			} else {
				reverse = append(reverse, Operation{Op: Remove, Path: delta.Path})
			}
		case Remove:
			reverse = append(reverse, Operation{Op: Add, Path: delta.Path, Value: delta.Before})
		case Replace:
			reverse = append(reverse, Operation{Op: Replace, Path: delta.Path, Value: delta.Before})
		default:
			return Diff{}, fmt.Errorf("unsupported delta op in reverse compile: %s", delta.Op)
		}
	}

	return Diff{Deltas: deltas, forward: forward, reverse: reverse}, nil
}

// deepCopyAny performs a JSON round-trip to safely copy arbitrary JSON-like values.
func deepCopyAny(value any) (any, error) {
	bytes, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(bytes, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// tryGetDeep attempts to get a value at path and returns whether it existed and a deep copy if so.
func tryGetDeep(document any, path string) (bool, any, error) {
	val, err := jsonpointer.Get(document, path)
	if err != nil {
		return false, nil, nil
	}
	cp, err := deepCopyAny(val)
	if err != nil {
		return false, nil, err
	}
	return true, cp, nil
}

// resolveConcreteAddPath converts an add path with "-" (array append) into a concrete index path
// based on the current state of the parent array. If the path does not end with "-", it is returned unchanged.
func resolveConcreteAddPath(document any, path string) (string, error) {
	p, err := jsonpointer.New(path)
	if err != nil {
		return "", err
	}
	if len(p) == 0 {
		// Root path
		return path, nil
	}

	parentPath := jsonpointer.Pointer(p[0 : len(p)-1]).String()
	token := p[len(p)-1]
	if token != "-" {
		return path, nil
	}

	parent, err := jsonpointer.Get(document, parentPath)
	if err != nil {
		return "", fmt.Errorf("parent path '%s' not found for '-': %w", parentPath, err)
	}
	arr, ok := parent.([]any)
	if !ok {
		return "", fmt.Errorf("path '%s' with '-' is not an array parent", parentPath)
	}
	idxStr := strconv.Itoa(len(arr))
	if parentPath == "" {
		return "/" + idxStr, nil
	}
	return parentPath + "/" + idxStr, nil
}

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
