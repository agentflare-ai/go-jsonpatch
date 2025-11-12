package jsonpatch_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/agentflare-ai/go-jsonpatch"
)

func TestNew_ObjectBasic(t *testing.T) {
	a := map[string]any{"a": 1.0, "b": map[string]any{"x": 10.0}}
	b := map[string]any{"a": 2.0, "b": map[string]any{"x": 10.0, "y": 20.0}}

	p, err := jsonpatch.New(a, b)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	out, err := jsonpatch.Apply(a, p)
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if !reflect.DeepEqual(out, b) {
		t.Fatalf("Apply(New(a,b)) != b\nout=%#v\nb  =%#v", out, b)
	}
}

func TestNew_ArrayInsertRemoveMove(t *testing.T) {
	type tc struct {
		name string
		a, b any
	}
	cases := []tc{
		{
			name: "insert middle",
			a:    map[string]any{"arr": []any{"bar", "baz"}},
			b:    map[string]any{"arr": []any{"bar", "qux", "baz"}},
		},
		{
			name: "remove middle",
			a:    map[string]any{"arr": []any{"bar", "qux", "baz"}},
			b:    map[string]any{"arr": []any{"bar", "baz"}},
		},
		{
			name: "simple move",
			a:    map[string]any{"arr": []any{"a", "b", "c", "d"}},
			b:    map[string]any{"arr": []any{"a", "c", "b", "d"}},
		},
		{
			name: "duplicates not guaranteed move",
			a:    map[string]any{"arr": []any{"a", "b", "a"}},
			b:    map[string]any{"arr": []any{"a", "a", "b"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := jsonpatch.New(c.a, c.b)
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}
			out, err := jsonpatch.Apply(c.a, p)
			if err != nil {
				t.Fatalf("Apply() error: %v", err)
			}
			if !reflect.DeepEqual(out, c.b) {
				ob, _ := json.Marshal(out)
				bb, _ := json.Marshal(c.b)
				t.Fatalf("Apply(New(a,b)) mismatch\nout=%s\nb  =%s", ob, bb)
			}
		})
	}
}

func TestNew_MixedInputs(t *testing.T) {
	aJSON := []byte(`{"a":1,"arr":["x","y"]}`)
	bMap := map[string]any{"a": 1.0, "arr": []any{"x", "y", "z"}}

	p, err := jsonpatch.New(aJSON, bMap)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	var a any
	if err := json.Unmarshal(aJSON, &a); err != nil {
		t.Fatalf("unmarshal a: %v", err)
	}
	out, err := jsonpatch.Apply(a, p)
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if !reflect.DeepEqual(out, bMap) {
		t.Fatalf("Apply(New(a,b)) != b")
	}
}

func TestNew_NumericNormalization(t *testing.T) {
	type S struct {
		N int `json:"n"`
	}
	a := S{N: 1}
	b := map[string]any{"n": 1.0}

	p, err := jsonpatch.New(a, b)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	// Expect no-op patch (or a patch that changes nothing when applied)
	if len(p) != 0 {
		// Still acceptable as long as apply yields b
		var av any
		_ = json.Unmarshal([]byte(`{"n":1}`), &av)
		out, err := jsonpatch.Apply(av, p)
		if err != nil {
			t.Fatalf("Apply() error: %v", err)
		}
		if !reflect.DeepEqual(out, b) {
			t.Fatalf("numeric normalization failed: %v", out)
		}
	}
}

func TestNew_RootReplace_TypeChange(t *testing.T) {
	a := map[string]any{"x": 1.0}
	b := []any{1.0, 2.0}

	p, err := jsonpatch.New(a, b)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	out, err := jsonpatch.Apply(a, p)
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if !reflect.DeepEqual(out, b) {
		t.Fatalf("Apply(New(a,b)) != b")
	}
}

func TestNew_NoOpWhenEqual(t *testing.T) {
	a := map[string]any{"a": 1.0, "b": []any{1.0, 2.0}}
	p, err := jsonpatch.New(a, a)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if len(p) != 0 {
		t.Fatalf("expected empty patch when inputs equal, got %v", p)
	}
}
