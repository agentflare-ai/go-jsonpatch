package jsonpatch_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/agentflare-ai/go-jsonpatch"
)

func TestApply(t *testing.T) {
	testCases := []struct {
		name        string
		doc         string
		patch       string
		expected    string
		expectedErr string
	}{
		// RFC 6902, Appendix A.1. Add an Object Member
		{
			name:     "add an object member",
			doc:      `{"a":"b","c":"d"}`,
			patch:    `[{"op":"add","path":"/b","value":"e"}]`,
			expected: `{"a":"b","b":"e","c":"d"}`,
		},
		// RFC 6902, Appendix A.2. Add an Array Element
		{
			name:     "add an array element",
			doc:      `{"foo":["bar","baz"]}`,
			patch:    `[{"op":"add","path":"/foo/1","value":"qux"}]`,
			expected: `{"foo":["bar","qux","baz"]}`,
		},
		// RFC 6902, Appendix A.3. Remove an Object Member
		{
			name:     "remove an object member",
			doc:      `{"a":"b","c":"d"}`,
			patch:    `[{"op":"remove","path":"/a"}]`,
			expected: `{"c":"d"}`,
		},
		// RFC 6902, Appendix A.4. Remove an Array Element
		{
			name:     "remove an array element",
			doc:      `{"foo":["bar","qux","baz"]}`,
			patch:    `[{"op":"remove","path":"/foo/1"}]`,
			expected: `{"foo":["bar","baz"]}`,
		},
		// RFC 6902, Appendix A.5. Replace a Value
		{
			name:     "replace a value",
			doc:      `{"a":"b","c":"d"}`,
			patch:    `[{"op":"replace","path":"/a","value":"e"}]`,
			expected: `{"a":"e","c":"d"}`,
		},
		// RFC 6902, Appendix A.6. Move a Value
		{
			name:     "move a value",
			doc:      `{"foo":{"bar":"baz","waldo":"fred"},"qux":{"corge":"grault"}}`,
			patch:    `[{"op":"move","from":"/foo/waldo","path":"/qux/thud"}]`,
			expected: `{"foo":{"bar":"baz"},"qux":{"corge":"grault","thud":"fred"}}`,
		},
		// RFC 6902, Appendix A.7. Move an Array Element
		{
			name:     "move an array element",
			doc:      `{"foo":["all","grass","cows","eat"]}`,
			patch:    `[{"op":"move","from":"/foo/1","path":"/foo/3"}]`,
			expected: `{"foo":["all","cows","eat","grass"]}`,
		},
		// RFC 6902, Appendix A.8. Test a Value
		{
			name:     "test a value (success)",
			doc:      `{"baz":"qux","foo":["a",2,"c"]}`,
			patch:    `[{"op":"test","path":"/baz","value":"qux"}]`,
			expected: `{"baz":"qux","foo":["a",2,"c"]}`,
		},
		// RFC 6902, Appendix A.9. Test a Value (error)
		{
			name:        "test a value (error)",
			doc:         `{"baz":"qux"}`,
			patch:       `[{"op":"test","path":"/baz","value":"bar"}]`,
			expectedErr: "test failed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var doc any
			json.Unmarshal([]byte(tc.doc), &doc)

			var patch jsonpatch.Patch
			json.Unmarshal([]byte(tc.patch), &patch)

			result, err := jsonpatch.Apply(doc, patch)

			if tc.expectedErr != "" {
				if err == nil {
					t.Errorf("expected error containing %q, but got none", tc.expectedErr)
				} else if !strings.Contains(err.Error(), tc.expectedErr) {
					t.Errorf("expected error containing %q, but got %q", tc.expectedErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			var expected any
			json.Unmarshal([]byte(tc.expected), &expected)

			if !reflect.DeepEqual(result, expected) {
				resBytes, _ := json.Marshal(result)
				expBytes, _ := json.Marshal(expected)
				t.Errorf("unexpected result\n\tgot: %s\n\twant: %s", resBytes, expBytes)
			}
		})
	}
}

func TestApplyStream(t *testing.T) {
	doc := `{"a":"b","c":"d"}`
	patch := `[{"op":"add","path":"/b","value":"e"}]`
	expected := `{"a":"b","b":"e","c":"d"}`

	reader := strings.NewReader(doc)
	var writer bytes.Buffer

	var patchOps jsonpatch.Patch
	json.Unmarshal([]byte(patch), &patchOps)

	err := jsonpatch.ApplyStream(reader, &writer, patchOps)
	if err != nil {
		t.Fatalf("ApplyStream() unexpected error: %v", err)
	}

	// The JSON encoder adds a newline, so we trim it for comparison
	result := strings.TrimSpace(writer.String())

	var resultJSON, expectedJSON any
	json.Unmarshal([]byte(result), &resultJSON)
	json.Unmarshal([]byte(expected), &expectedJSON)

	if !reflect.DeepEqual(resultJSON, expectedJSON) {
		t.Errorf("ApplyStream() result mismatch:\ngot:  %s\nwant: %s", result, expected)
	}
}
