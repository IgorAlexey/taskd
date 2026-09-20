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

func TestRenderBodySingleLineWithTrailingNewline(t *testing.T) {
	var m model
	got := m.renderBody(task{Body: "single line task description\n"})
	if strings.TrimSpace(got) == "" {
		t.Fatal("renderBody returned empty string for single-line body with trailing newline")
	}
	if !strings.Contains(got, "single line task description") {
		t.Fatalf("expected single line task description in %q", got)
	}
}

func TestRenderBodyFullContent(t *testing.T) {
	var m model
	got := m.renderBody(task{Body: "Long task title that exceeds terminal column width\n\nWhy: description here"})
	if !strings.Contains(got, "Long task title that exceeds terminal column width") {
		t.Fatalf("expected title preserved in detail viewport, got %q", got)
	}
	if !strings.Contains(got, "Why: description here") {
		t.Fatalf("expected description preserved in detail viewport, got %q", got)
	}
}
