package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestFormValidateProjectLength(t *testing.T) {
	wantMsg := fmt.Sprintf("project must not exceed %d characters", maxProjectLen)
	tests := []struct {
		name    string
		editing bool
		proj    string
		wantErr string
	}{
		{"create exceeds max", false, strings.Repeat("a", maxProjectLen+1), wantMsg},
		{"create exact max", false, strings.Repeat("a", maxProjectLen), ""},
		{"edit exceeds max", true, strings.Repeat("b", maxProjectLen+1), wantMsg},
		{"edit exact max", true, strings.Repeat("b", maxProjectLen), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var f formModel
			if tc.editing {
				f, _ = newEditForm(task{ID: "task-1", Body: "body", Priority: 1})
			} else {
				f, _ = newCreateForm("")
				f.body.SetValue("body")
			}
			f.project.SetValue(tc.proj)
			if got := f.validate(); got != tc.wantErr {
				t.Fatalf("validate() = %q, want %q", got, tc.wantErr)
			}
		})
	}
}
