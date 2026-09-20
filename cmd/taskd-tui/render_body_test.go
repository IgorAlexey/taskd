package main

import (
	"strings"
	"testing"
)

func TestRenderBodySingleLine(t *testing.T) {
	var m model
	body120 := "This is a 120-character single-line task body without any newlines in it at all to test wrapping in the detail pane view"
	if len(body120) != 120 {
		t.Fatalf("expected length 120, got %d", len(body120))
	}
	got := m.renderBody(task{Body: body120})
	if strings.TrimSpace(got) == "" {
		t.Fatal("renderBody returned empty string for single-line body")
	}
	tail := "detail pane view"
	if !strings.Contains(got, tail) {
		t.Fatalf("expected %q in %q", tail, got)
	}
}
