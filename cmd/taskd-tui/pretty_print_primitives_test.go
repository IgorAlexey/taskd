package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrettyPrintPrimitives(t *testing.T) {
	cases := []struct {
		name       string
		primitives string
		want       []string
		wantAbsent []string
	}{
		{
			name:       "valid_object",
			primitives: `{"key":"val","count":1}`,
			want: []string{
				"result: {",
				`  "key": "val",`,
				`  "count": 1`,
				"}",
			},
			wantAbsent: []string{`{"key":"val","count":1}`},
		},
		{
			name:       "valid_array",
			primitives: `["first","second"]`,
			want: []string{
				"result: [",
				`  "first",`,
				`  "second"`,
				"]",
			},
		},
		{
			name:       "invalid_json",
			primitives: "invalid-json",
			want:       []string{"result: invalid-json"},
		},
		{
			name:       "null_primitives",
			primitives: "null",
			wantAbsent: []string{"result:"},
		},
	}

	var m model
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := m.renderBody(task{Primitives: json.RawMessage(tc.primitives)})
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Fatalf("expected %q in %q", w, got)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(got, absent) {
					t.Fatalf("expected %q to be absent in %q", absent, got)
				}
			}
		})
	}
}
