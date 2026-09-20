package main

import (
	"strings"
	"testing"
)

func TestDeleteConfirm(t *testing.T) {
	th := newTheme(true)
	f, _ := newCreateForm("test")
	f.width, f.height, f.th, f.discarding = 80, 24, th, true

	tests := []struct {
		name      string
		view      string
		dimToken  string
		wantDim   string
		wantFocus string
	}{
		{"delete single", confirmModel{text: "Delete?", button: "delete"}.View(80, 24, th), "[y] delete", th.dim.Render("[y] delete"), th.accent.Render("[n] cancel")},
		{"delete stacked", confirmModel{text: "Delete?", button: "delete"}.View(20, 24, th), "[y] delete", th.dim.Render("[y] delete"), th.accent.Render("[n] cancel")},
		{"purge modal", confirmModel{text: "Purge?", button: "purge"}.View(80, 24, th), "[y] purge", th.dim.Render("[y] purge"), th.accent.Render("[n] cancel")},
		{"form discard", f.View(), "[y] discard", th.dim.Render("[y] discard"), th.accent.Render("[n] cancel")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.view, tt.wantFocus) {
				t.Errorf("%s: view missing accented cancel button", tt.name)
			}
			if !strings.Contains(tt.view, tt.wantDim) {
				t.Errorf("%s: view missing dim action token %q", tt.name, tt.dimToken)
			}
		})
	}
}
