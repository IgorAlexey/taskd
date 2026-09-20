package main

import (
	"regexp"
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
	matches := regexp.MustCompile(`<input\b[^>]*>`).FindAllString(ui, -1)
	found := false
	for _, m := range matches {
		if strings.Contains(m, `id="note-author"`) && strings.Contains(m, `maxlength="64"`) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected note-author input element to define maxlength=64")
	}
}
