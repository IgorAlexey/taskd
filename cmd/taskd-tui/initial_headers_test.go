package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestInitialHeaders(t *testing.T) {
	m := newModel(config{icons: false}, nil)

	view := ansi.Strip(m.View().Content)
	lines := strings.Split(view, "\n")

	var colHeadLine string
	for _, l := range lines {
		if strings.Contains(l, "title") {
			colHeadLine = l
			break
		}
	}

	if colHeadLine == "" {
		t.Fatalf("could not find column header line in view:\n%s", view)
	}

	for _, expected := range []string{"title", "scope", "lease", "id"} {
		if !strings.Contains(colHeadLine, expected) {
			t.Errorf("expected column header %q in header line: %q", expected, colHeadLine)
		}
	}

	resized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	mResized := resized.(model)
	viewResized := ansi.Strip(mResized.View().Content)
	if !strings.Contains(viewResized, "title") || !strings.Contains(viewResized, "scope") {
		t.Fatalf("expected headers preserved after resize: %s", viewResized)
	}
}
