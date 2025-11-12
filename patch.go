package jsonpatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"

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

	// Use add semantics for destination to ensure array insert behavior per RFC6902.
	return applyAdd(doc, to, val)
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

// New computes an RFC 6902 JSON Patch that transforms a into b.
// It accepts []byte, json.RawMessage, or Go values (maps, slices, primitives).
func New(a, b any) (Patch, error) {
	na, err := normalizeJSONInput(a)
	if err != nil {
		return nil, err
	}
	nb, err := normalizeJSONInput(b)
	if err != nil {
		return nil, err
	}
	return diffValue("", na, nb)
}

// normalizeJSONInput canonicalizes arbitrary input into encoding/json's standard
// Go representation: map[string]any, []any, float64, string, bool, nil.
func normalizeJSONInput(v any) (any, error) {
	switch tv := v.(type) {
	case []byte:
		var out any
		if err := json.Unmarshal(tv, &out); err != nil {
			return nil, fmt.Errorf("invalid JSON bytes: %w", err)
		}
		return out, nil
	case json.RawMessage:
		var out any
		if err := json.Unmarshal(tv, &out); err != nil {
			return nil, fmt.Errorf("invalid json.RawMessage: %w", err)
		}
		return out, nil
	default:
		// Round-trip through JSON to normalize numeric types to float64, etc.
		return deepCopyAny(tv)
	}
}

// escapeToken applies RFC 6901 escaping for '~' and '/' characters.
func escapeToken(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
}

// joinPath concatenates RFC 6901 tokens onto a JSON Pointer path.
func joinPath(base, token string) string {
	if base == "" {
		return "/" + escapeToken(token)
	}
	return base + "/" + escapeToken(token)
}

func diffValue(path string, a, b any) (Patch, error) {
	// If fully equal, no ops.
	if reflect.DeepEqual(a, b) {
		return nil, nil
	}

	// Object vs Object
	if ma, ok := a.(map[string]any); ok {
		if mb, ok := b.(map[string]any); ok {
			return diffObject(path, ma, mb)
		}
	}

	// Array vs Array
	if sa, ok := a.([]any); ok {
		if sb, ok := b.([]any); ok {
			return diffArray(path, sa, sb)
		}
	}

	// Fallback to replace when types differ or primitive mismatch
	return Patch{
		{Op: Replace, Path: path, Value: b},
	}, nil
}

func diffObject(path string, a, b map[string]any) (Patch, error) {
	var out Patch

	// Track keys in a
	for ka := range a {
		if _, exists := b[ka]; !exists {
			// Key removed
			out = append(out, Operation{
				Op:   Remove,
				Path: joinPath(path, ka),
			})
		}
	}

	// Keys present in b
	for kb, vb := range b {
		if va, exists := a[kb]; exists {
			// Recurse
			child, err := diffValue(joinPath(path, kb), va, vb)
			if err != nil {
				return nil, err
			}
			out = append(out, child...)
			continue
		}
		// Key added
		cpv, err := deepCopyAny(vb)
		if err != nil {
			return nil, err
		}
		out = append(out, Operation{
			Op:    Add,
			Path:  joinPath(path, kb),
			Value: cpv,
		})
	}

	return out, nil
}

// diffArray produces a patch transforming a -> b using an LCS-based edit script.
// It uses tokenized equality (cached JSON marshal of elements) and emits removes
// in descending index order followed by adds in ascending index order.
func diffArray(path string, a, b []any) (Patch, error) {
	// Precompute tokens
	atoks, err := tokenizeArray(a)
	if err != nil {
		return nil, err
	}
	btoks, err := tokenizeArray(b)
	if err != nil {
		return nil, err
	}
	n := len(atoks)
	m := len(btoks)

	// Build token -> positions queue for 'a'
	posMap := make(map[string][]int, n)
	for i, t := range atoks {
		posMap[t] = append(posMap[t], i)
	}
	type pair struct{ ai, bj int }
	pairs := make([]pair, 0, min(n, m))
	seq := make([]int, 0, min(n, m))
	for j, t := range btoks {
		q := posMap[t]
		if len(q) == 0 {
			continue
		}
		ai := q[0]
		posMap[t] = q[1:]
		pairs = append(pairs, pair{ai: ai, bj: j})
		seq = append(seq, ai)
	}

	// Compute LIS over seq, keeping predecessors to reconstruct indices
	k := len(seq)
	tails := make([]int, 0, k) // indices into seq
	prev := make([]int, k)
	for i := range prev {
		prev[i] = -1
	}
	for i, v := range seq {
		lo, hi := 0, len(tails)
		for lo < hi {
			mid := (lo + hi) / 2
			if seq[tails[mid]] < v {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		pos := lo
		if pos > 0 {
			prev[i] = tails[pos-1]
		}
		if pos == len(tails) {
			tails = append(tails, i)
		} else {
			tails[pos] = i
		}
	}
	lisLen := len(tails)
	lisIdx := make([]int, lisLen)
	if lisLen > 0 {
		p := tails[lisLen-1]
		for x := lisLen - 1; x >= 0; x-- {
			lisIdx[x] = p
			p = prev[p]
			if p < 0 && x > 0 {
				break
			}
		}
	}

	keepA := make([]bool, n)
	keepB := make([]bool, m)
	for _, idxPair := range lisIdx {
		ai := pairs[idxPair].ai
		bj := pairs[idxPair].bj
		keepA[ai] = true
		keepB[bj] = true
	}

	var patch Patch
	// Removes: descending indices
	for i := n - 1; i >= 0; i-- {
		if !keepA[i] {
			patch = append(patch, Operation{
				Op:   Remove,
				Path: joinPath(path, strconv.Itoa(i)),
			})
		}
	}
	// Adds: ascending indices
	for j := 0; j < m; j++ {
		if !keepB[j] {
			patch = append(patch, Operation{
				Op:    Add,
				Path:  joinPath(path, strconv.Itoa(j)),
				Value: b[j],
			})
		}
	}
	return patch, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func tokenizeArray(arr []any) ([]string, error) {
	out := make([]string, len(arr))
	for i, v := range arr {
		switch tv := v.(type) {
		case nil:
			out[i] = "0"
		case bool:
			if tv {
				out[i] = "b:1"
			} else {
				out[i] = "b:0"
			}
		case float64:
			// Normalize -0 to +0 for stable equality
			if tv == 0 {
				out[i] = "n:0"
				continue
			}
			out[i] = "n:" + strconv.FormatUint(math.Float64bits(tv), 16)
		case string:
			out[i] = "s:" + tv
		default:
			// Fallback to canonical JSON for arrays/objects
			bs, err := json.Marshal(tv)
			if err != nil {
				return nil, err
			}
			out[i] = "j:" + string(bs)
		}
	}
	return out, nil
}

// ExtractAdded splits `after` using only Add ops in `patch`.
// - remaining: `after` with added elements/keys removed (copy-on-write)
// - addedOnly: partial structure with only the added content
// Hot path: no JSON serialization; no deep copies of values; only container COW.
func ExtractAdded(after any, patch Patch) (remaining any, addedOnly any, err error) {
	// Shallow clone the root so the caller's value is never mutated.
	switch root := after.(type) {
	case map[string]any:
		remaining = shallowCloneMap(root)
	case []any:
		remaining = shallowCloneSlice(root)
	default:
		// Non-container roots cannot have child adds; return as-is.
		remaining = after
	}

	// Group add ops by tokenized parent pointer; preserve op order.
	type addOp struct {
		parent jsonpointer.Pointer
		child  string
		value  any
		order  int
		raw    Operation
	}
	groups := make(map[string][]addOp)
	parentByKey := make(map[string]jsonpointer.Pointer)
	for i, op := range patch {
		if op.Op != Add {
			continue
		}
		// Root replacement is not considered an addition for extraction.
		if op.Path == "" {
			return nil, nil, fmt.Errorf("jsonpatch: root-level add is not supported by ExtractAdded")
		}
		tokens, perr := jsonpointer.New(op.Path)
		if perr != nil {
			return nil, nil, perr
		}
		if len(tokens) == 0 {
			return nil, nil, fmt.Errorf("jsonpatch: invalid empty path in add")
		}
		parent := jsonpointer.Pointer(tokens[:len(tokens)-1])
		child := tokens[len(tokens)-1]
		key := parent.String()
		groups[key] = append(groups[key], addOp{
			parent: parent,
			child:  child,
			value:  op.Value,
			order:  i,
			raw:    op,
		})
		parentByKey[key] = parent
	}

	// Nothing to extract.
	if len(groups) == 0 {
		return remaining, nil, nil
	}

	// Sort parent keys by depth (token length) to have deterministic order.
	type parentEntry struct {
		key    string
		tokens jsonpointer.Pointer
	}
	orderParents := make([]parentEntry, 0, len(groups))
	for k, p := range parentByKey {
		orderParents = append(orderParents, parentEntry{key: k, tokens: p})
	}
	// Shallow-first or deep-first both work when we COW from current remaining.
	// Use shallow-first (shorter paths first) for readability.
	for i := 0; i < len(orderParents)-1; i++ {
		for j := i + 1; j < len(orderParents); j++ {
			if len(orderParents[i].tokens) > len(orderParents[j].tokens) {
				orderParents[i], orderParents[j] = orderParents[j], orderParents[i]
			}
		}
	}

	// Process each parent group.
	for _, pe := range orderParents {
		parentTokens := pe.tokens
		ops := groups[pe.key]

		// Resolve parent from 'after' to confirm existence and type, also needed for addedOnly leaf values.
		parentAfter, gerr := parentTokens.Get(after)
		if gerr != nil {
			return nil, nil, fmt.Errorf("jsonpatch: parent '%s' not found in after: %w", parentTokens.String(), gerr)
		}

		switch pa := parentAfter.(type) {
		case map[string]any:
			// Object semantics with last-wins per key.
			final := make(map[string]any, len(ops))
			seen := make(map[string]struct{}, len(ops))
			for _, op := range ops {
				// Only string child tokens valid for object parents.
				if _, numErr := jsonpointer.ParseArrayIndex(op.child); numErr == nil || op.child == "-" {
					return nil, nil, fmt.Errorf("jsonpatch: object parent '%s' received array-style add at child '%s'", parentTokens.String(), op.child)
				}
				final[op.child] = op.value
				seen[op.child] = struct{}{}
			}

			// Build new parent map for remaining by removing the keys (COW).
			parentRem, gerr := parentTokens.Get(remaining)
			if gerr != nil {
				return nil, nil, fmt.Errorf("jsonpatch: parent '%s' not found in remaining: %w", parentTokens.String(), gerr)
			}
			pm, ok := parentRem.(map[string]any)
			if !ok {
				return nil, nil, fmt.Errorf("jsonpatch: parent '%s' expected object in remaining", parentTokens.String())
			}
			newMap := shallowCloneMap(pm)
			for k := range final {
				delete(newMap, k)
			}
			remaining, err = cowSetAtPath(remaining, parentTokens, newMap)
			if err != nil {
				return nil, nil, err
			}

			// Populate addedOnly branch under same parent path with only the added keys.
			addedOnly, err = ensureAddedOnlyParent(addedOnly, parentTokens, false)
			if err != nil {
				return nil, nil, err
			}
			// Fetch the addedOnly parent we just ensured (it must exist and be a map).
			aoPar, gerr := parentTokens.Get(addedOnly)
			if gerr != nil {
				return nil, nil, fmt.Errorf("jsonpatch: failed to get addedOnly parent '%s': %w", parentTokens.String(), gerr)
			}
			aoMap, ok := aoPar.(map[string]any)
			if !ok {
				return nil, nil, fmt.Errorf("jsonpatch: addedOnly parent '%s' is not object", parentTokens.String())
			}
			// Use values from 'after' to ensure leaf references match final document.
			for k := range final {
				v, ok := pa[k]
				if !ok {
					// Should not happen if patch/after are consistent.
					aoMap[k] = nil
					continue
				}
				aoMap[k] = v
			}

		case []any:
			// Array semantics:
			// Infer baseLen = len(afterArray) - numberOfAddsToThisParent
			lAfter := len(pa)
			numAdds := len(ops)
			baseLen := lAfter - numAdds
			if baseLen < 0 {
				return nil, nil, fmt.Errorf("jsonpatch: invalid baseLen for parent '%s'", parentTokens.String())
			}

			// Resolve '-' appends and validate numeric indices against baseLen.
			type idxVal struct {
				idx   int
				value any
				order int
			}
			tmp := make([]idxVal, 0, len(ops))
			appendCount := 0
			for _, op := range ops {
				if op.child == "-" {
					idx := baseLen + appendCount
					appendCount++
					tmp = append(tmp, idxVal{idx: idx, value: op.value, order: op.order})
					continue
				}
				// Numeric index must be < baseLen (concrete indices refer to original positions).
				uidx, ierr := jsonpointer.ParseArrayIndex(op.child)
				if ierr != nil {
					return nil, nil, fmt.Errorf("jsonpatch: array parent '%s' child '%s' is not numeric nor '-': %v", parentTokens.String(), op.child, ierr)
				}
				if int(uidx) >= baseLen {
					return nil, nil, fmt.Errorf("jsonpatch: array parent '%s' child index %d >= baseLen %d", parentTokens.String(), uidx, baseLen)
				}
				tmp = append(tmp, idxVal{idx: int(uidx), value: op.value, order: op.order})
			}
			// Last-wins per final index.
			final := make(map[int]idxVal, len(tmp))
			for _, it := range tmp {
				final[it.idx] = it
			}
			// Validate reconstructed range bounds
			if len(final) > 0 {
				maxIdx := -1
				for idx := range final {
					if idx > maxIdx {
						maxIdx = idx
					}
				}
				if maxIdx >= baseLen+appendCount {
					return nil, nil, fmt.Errorf("jsonpatch: resolved index %d outside reconstructed range (0..%d) for parent '%s'", maxIdx, baseLen+appendCount-1, parentTokens.String())
				}
			}

			// Build new parent slice for remaining by filtering out indices.
			parentRem, gerr := parentTokens.Get(remaining)
			if gerr != nil {
				return nil, nil, fmt.Errorf("jsonpatch: parent '%s' not found in remaining: %w", parentTokens.String(), gerr)
			}
			ps, ok := parentRem.([]any)
			if !ok {
				return nil, nil, fmt.Errorf("jsonpatch: parent '%s' expected array in remaining", parentTokens.String())
			}
			// Collect indices to remove in a set.
			removeSet := make(map[int]struct{}, len(final))
			for idx := range final {
				removeSet[idx] = struct{}{}
			}
			filtered := make([]any, 0, len(ps)-len(removeSet))
			for i := 0; i < len(ps); i++ {
				if _, drop := removeSet[i]; drop {
					continue
				}
				filtered = append(filtered, ps[i])
			}
			remaining, err = cowSetAtPath(remaining, parentTokens, filtered)
			if err != nil {
				return nil, nil, err
			}

			// Populate addedOnly branch under same parent path as compact slice in ascending index order.
			addedOnly, err = ensureAddedOnlyParent(addedOnly, parentTokens, true)
			if err != nil {
				return nil, nil, err
			}
			// Build ascending list of indices
			idxs := make([]int, 0, len(final))
			for idx := range final {
				idxs = append(idxs, idx)
			}
			for i := 0; i < len(idxs)-1; i++ {
				for j := i + 1; j < len(idxs); j++ {
					if idxs[i] > idxs[j] {
						idxs[i], idxs[j] = idxs[j], idxs[i]
					}
				}
			}
			// Use values from 'after' at those indices to preserve leaf references.
			values := make([]any, 0, len(idxs))
			for _, idx := range idxs {
				if idx < 0 || idx >= len(pa) {
					return nil, nil, fmt.Errorf("jsonpatch: after array index %d out of bounds for parent '%s'", idx, parentTokens.String())
				}
				values = append(values, pa[idx])
			}
			// Set compact slice at parent path in addedOnly
			addedOnly, err = cowSetAtPath(addedOnly, parentTokens, values)
			if err != nil {
				return nil, nil, err
			}

		default:
			return nil, nil, fmt.Errorf("jsonpatch: parent '%s' must be object or array", parentTokens.String())
		}
	}

	return remaining, addedOnly, nil
}

// cowSetAtPath performs copy-on-write assignment of a value at the given tokenized path.
// It shallow-clones containers along the path to avoid mutating shared structures.
func cowSetAtPath(root any, tokens jsonpointer.Pointer, newVal any) (any, error) {
	// Empty path replaces the root
	if len(tokens) == 0 {
		// root replacement is allowed for internal construction
		return newVal, nil
	}

	// Build a stack of frames along the path
	type frame struct {
		container any
		isMap     bool
		key       string
		isSlice   bool
		index     int
	}
	var stack []frame
	current := root
	for i, tok := range tokens {
		switch c := current.(type) {
		case map[string]any:
			child, ok := c[tok]
			if !ok {
				return nil, fmt.Errorf("jsonpatch: cowSetAtPath missing key '%s' at segment %d", tok, i)
			}
			stack = append(stack, frame{container: c, isMap: true, key: tok})
			current = child
		case []any:
			if tok == "-" {
				return nil, fmt.Errorf("jsonpatch: cowSetAtPath does not accept '-' in path")
			}
			uidx, err := jsonpointer.ParseArrayIndex(tok)
			if err != nil {
				return nil, fmt.Errorf("jsonpatch: cowSetAtPath invalid array index '%s' at segment %d: %v", tok, i, err)
			}
			if int(uidx) >= len(c) {
				return nil, fmt.Errorf("jsonpatch: cowSetAtPath index %d out of bounds at segment %d", uidx, i)
			}
			stack = append(stack, frame{container: c, isSlice: true, index: int(uidx)})
			current = c[uidx]
		default:
			return nil, fmt.Errorf("jsonpatch: cowSetAtPath encountered non-container at segment %d", i)
		}
	}

	updated := newVal
	for i := len(stack) - 1; i >= 0; i-- {
		fr := stack[i]
		if fr.isMap {
			orig := fr.container.(map[string]any)
			cp := shallowCloneMap(orig)
			cp[fr.key] = updated
			updated = cp
			continue
		}
		if fr.isSlice {
			orig := fr.container.([]any)
			cp := shallowCloneSlice(orig)
			cp[fr.index] = updated
			updated = cp
			continue
		}
		return nil, errors.New("jsonpatch: cowSetAtPath invalid frame")
	}
	return updated, nil
}

// ensureAddedOnlyParent creates missing intermediate containers along tokens in the addedOnly tree.
// It only supports object (map) intermediates. The final container is created as a map or slice depending on wantArray.
func ensureAddedOnlyParent(root any, tokens jsonpointer.Pointer, wantArray bool) (any, error) {
	// If root is nil, initialize as appropriate container for first token chain.
	if len(tokens) == 0 {
		if wantArray {
			return []any{}, nil
		}
		return map[string]any{}, nil
	}
	var out any = root
	if out == nil {
		out = map[string]any{}
	}
	current := out
	for i, tok := range tokens {
		last := i == len(tokens)-1
		switch c := current.(type) {
		case map[string]any:
			child, ok := c[tok]
			if !ok {
				// Create container
				var created any
				if last {
					if wantArray {
						created = []any{}
					} else {
						created = map[string]any{}
					}
				} else {
					created = map[string]any{}
				}
				// COW assign into the map
				cp := shallowCloneMap(c)
				cp[tok] = created
				current = created
				// Rebuild out root up to here
				head := jsonpointer.Pointer(tokens[:i])
				var err error
				out, err = cowSetAtPath(out, head, cp)
				if err != nil {
					return nil, err
				}
				continue
			}
			// If existing child type mismatches requested final, replace only at last step
			if last {
				switch child.(type) {
				case []any:
					if wantArray {
						current = child
						continue
					}
				case map[string]any:
					if !wantArray {
						current = child
						continue
					}
				}
				// Replace with desired type
				var desired any
				if wantArray {
					desired = []any{}
				} else {
					desired = map[string]any{}
				}
				cp := shallowCloneMap(c)
				cp[tok] = desired
				head := jsonpointer.Pointer(tokens[:i])
				var err error
				out, err = cowSetAtPath(out, head, cp)
				if err != nil {
					return nil, err
				}
				current = desired
				continue
			}
			// Continue walking
			current = child
		case []any:
			// Not supported to auto-create through array indices in addedOnly path.
			return nil, fmt.Errorf("jsonpatch: ensureAddedOnlyParent does not support array indices in intermediate path at segment %d", i)
		default:
			return nil, fmt.Errorf("jsonpatch: ensureAddedOnlyParent encountered non-container at segment %d", i)
		}
	}
	return out, nil
}

func shallowCloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

func shallowCloneSlice(s []any) []any {
	if s == nil {
		return nil
	}
	cp := make([]any, len(s))
	copy(cp, s)
	return cp
}
