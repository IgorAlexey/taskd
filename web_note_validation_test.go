package main

import (
	"strings"
	"testing"
)

func TestWebUINoteValidation(t *testing.T) {
	ui := string(uiHTML)

	if strings.Contains(ui, "!author || !text") {
		t.Error("expected submitNote not to silently drop blank notes")
	}
	for _, errStr := range []string{"Author is required", "Note text is required"} {
		if !strings.Contains(ui, errStr) {
			t.Errorf("missing validation error message %q", errStr)
		}
	}
	for _, id := range []string{"note-author", "note-text", "error-banner"} {
		if !strings.Contains(ui, `id="`+id+`"`) {
			t.Errorf("missing DOM element id=%q", id)
		}
	}
}
