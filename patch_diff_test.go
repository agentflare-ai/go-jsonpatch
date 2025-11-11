package jsonpatch

import (
	"reflect"
	"testing"
)

func TestDiffApplyRevert_ObjectOps(t *testing.T) {
	original := map[string]any{
		"a": 1.0,
		"b": map[string]any{"x": 10.0},
	}
	patch := Patch{
		{Op: Add, Path: "/b/y", Value: 20.0},     // new property
		{Op: Add, Path: "/a", Value: 2.0},        // overwrite existing (add on object acts as set)
		{Op: Replace, Path: "/b/x", Value: 11.0}, // replace existing
	}

	want, err := Apply(original, patch)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	diff, err := Prepare(original, patch)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	got, err := diff.Apply(original)
	if err != nil {
		t.Fatalf("Diff.Apply failed: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("Apply vs Diff.Apply mismatch:\nwant=%#v\ngot =%#v", want, got)
	}

	restored, err := diff.Revert(got)
	if err != nil {
		t.Fatalf("Diff.Revert failed: %v", err)
	}
	if !reflect.DeepEqual(original, restored) {
		t.Fatalf("Revert did not restore original:\nwant=%#v\ngot =%#v", original, restored)
	}
}

func TestDiffApplyRevert_ArrayOps(t *testing.T) {
	original := map[string]any{
		"arr": []any{"A", "B"},
	}
	patch := Patch{
		{Op: Add, Path: "/arr/-", Value: "C"}, // append -> [A,B,C]
		{Op: Add, Path: "/arr/1", Value: "X"}, // insert at 1 -> [A,X,B,C]
		{Op: Remove, Path: "/arr/0"},          // remove "A" -> [X,B,C]
	}

	want, err := Apply(original, patch)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	diff, err := Prepare(original, patch)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	got, err := diff.Apply(original)
	if err != nil {
		t.Fatalf("Diff.Apply failed: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("Apply vs Diff.Apply mismatch:\nwant=%#v\ngot =%#v", want, got)
	}

	restored, err := diff.Revert(got)
	if err != nil {
		t.Fatalf("Diff.Revert failed: %v", err)
	}
	if !reflect.DeepEqual(original, restored) {
		t.Fatalf("Revert did not restore original:\nwant=%#v\ngot =%#v", original, restored)
	}
}

func TestDiffApplyRevert_Move(t *testing.T) {
	original := map[string]any{
		"a": map[string]any{"x": 1.0, "z": 3.0},
		"b": map[string]any{},
	}
	patch := Patch{
		{Op: Move, From: "/a/x", Path: "/b/y"},
	}

	want, err := Apply(original, patch)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	diff, err := Prepare(original, patch)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	got, err := diff.Apply(original)
	if err != nil {
		t.Fatalf("Diff.Apply failed: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("Apply vs Diff.Apply mismatch:\nwant=%#v\ngot =%#v", want, got)
	}

	restored, err := diff.Revert(got)
	if err != nil {
		t.Fatalf("Diff.Revert failed: %v", err)
	}
	if !reflect.DeepEqual(original, restored) {
		t.Fatalf("Revert did not restore original:\nwant=%#v\ngot =%#v", original, restored)
	}
}

func TestDiffApplyRevert_CopyAndArrayAppend(t *testing.T) {
	original := map[string]any{
		"src": map[string]any{"v": 5.0},
		"arr": []any{1.0, 2.0},
	}
	patch := Patch{
		{Op: Copy, From: "/src/v", Path: "/arr/-"}, // arr -> [1,2,5]
	}

	want, err := Apply(original, patch)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	diff, err := Prepare(original, patch)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	got, err := diff.Apply(original)
	if err != nil {
		t.Fatalf("Diff.Apply failed: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("Apply vs Diff.Apply mismatch:\nwant=%#v\ngot =%#v", want, got)
	}

	restored, err := diff.Revert(got)
	if err != nil {
		t.Fatalf("Diff.Revert failed: %v", err)
	}
	if !reflect.DeepEqual(original, restored) {
		t.Fatalf("Revert did not restore original:\nwant=%#v\ngot =%#v", original, restored)
	}
}
