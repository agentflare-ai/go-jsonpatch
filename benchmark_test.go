package jsonpatch_test

import (
	"encoding/json"
	"testing"

	"github.com/agentflare-ai/go-jsonpatch"
	wi2ljsondiff "github.com/wI2L/jsondiff"
)

func BenchmarkNew_ObjectSmall(b *testing.B) {
	a := map[string]any{
		"a": 1.0,
		"b": map[string]any{"x": 10.0, "y": 20.0},
	}
	c := map[string]any{
		"a": 2.0,
		"b": map[string]any{"x": 10.0, "y": 21.0, "z": 30.0},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := jsonpatch.New(a, c); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNew_ArrayMedium(b *testing.B) {
	var arrA, arrB []any
	for i := 0; i < 200; i++ {
		arrA = append(arrA, i)
	}
	for i := 0; i < 200; i++ {
		arrB = append(arrB, (i+3)%200) // small rotation
	}
	a := map[string]any{"arr": arrA}
	c := map[string]any{"arr": arrB}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := jsonpatch.New(a, c); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRoundTrip_ApplyAfterNew(b *testing.B) {
	a := map[string]any{"a": 1.0, "arr": []any{1.0, 2.0, 3.0}}
	c := map[string]any{"a": 1.0, "arr": []any{3.0, 2.0, 1.0, 4.0}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, err := jsonpatch.New(a, c)
		if err != nil {
			b.Fatal(err)
		}
		// Apply on a fresh copy of a each iteration
		var av any
		jb, _ := json.Marshal(a)
		_ = json.Unmarshal(jb, &av)
		if _, err := jsonpatch.Apply(av, p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJSONDiff_ObjectSmall(b *testing.B) {
	a := map[string]any{
		"a": 1.0,
		"b": map[string]any{"x": 10.0, "y": 20.0},
	}
	c := map[string]any{
		"a": 2.0,
		"b": map[string]any{"x": 10.0, "y": 21.0, "z": 30.0},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := wi2ljsondiff.Compare(a, c); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJSONDiff_ArrayMedium(b *testing.B) {
	var arrA, arrB []any
	for i := 0; i < 200; i++ {
		arrA = append(arrA, i)
	}
	for i := 0; i < 200; i++ {
		arrB = append(arrB, (i+3)%200) // small rotation
	}
	a := map[string]any{"arr": arrA}
	c := map[string]any{"arr": arrB}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := wi2ljsondiff.Compare(a, c); err != nil {
			b.Fatal(err)
		}
	}
}
