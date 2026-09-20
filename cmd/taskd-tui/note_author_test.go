package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestNoteModalShowsAuthor(t *testing.T) {
	th := newTheme(true)

	t.Run("explicit author", func(t *testing.T) {
		author := "agent-smith-42"
		nm, _ := newNoteModel("task-abc", author, modeTable, 80, 24, th)
		rendered := nm.View(80, 24, th)
		want := "author: " + author
		if !strings.Contains(ansi.Strip(rendered), want) {
			t.Fatalf("expected note modal view to include %q, got:\n%s", want, rendered)
		}
	})

	t.Run("resolved user from environment", func(t *testing.T) {
		t.Setenv("USER", "test-operator-99")
		nm, _ := newNoteModel("task-abc", "", modeTable, 80, 24, th)
		rendered := nm.View(80, 24, th)
		want := "author: test-operator-99"
		if !strings.Contains(ansi.Strip(rendered), want) {
			t.Fatalf("expected note modal view to include %q, got:\n%s", want, rendered)
		}
	})

	t.Run("fallback author when USER unset", func(t *testing.T) {
		t.Setenv("USER", "")
		nm, _ := newNoteModel("task-abc", "", modeTable, 80, 24, th)
		rendered := nm.View(80, 24, th)
		want := "author: operator"
		if !strings.Contains(ansi.Strip(rendered), want) {
			t.Fatalf("expected note modal view to include %q, got:\n%s", want, rendered)
		}
	})

	t.Run("short terminal preserves input and drops metadata", func(t *testing.T) {
		nm, _ := newNoteModel("task-abc", "agent-smith-42", modeTable, 80, 6, th)
		rendered6 := nm.View(80, 6, th)
		if !strings.Contains(rendered6, nm.input.View()) {
			t.Fatalf("expected 6-row terminal to preserve input line, got:\n%s", rendered6)
		}

		rendered5 := nm.View(80, 5, th)
		if !strings.Contains(rendered5, nm.input.View()) {
			t.Fatalf("expected 5-row terminal to preserve input line, got:\n%s", rendered5)
		}
		if strings.Contains(rendered5, "author:") {
			t.Fatalf("expected 5-row terminal to drop author metadata before input, got:\n%s", rendered5)
		}
	})

	t.Run("short terminal with error preserves input and title", func(t *testing.T) {
		nm, _ := newNoteModel("task-abc", "agent-smith-42", modeTable, 80, 5, th)
		nm.errText = "note text cannot be empty"
		rendered5 := nm.View(80, 5, th)
		if !strings.Contains(rendered5, nm.input.View()) {
			t.Fatalf("expected 5-row terminal with error to preserve input line, got:\n%s", rendered5)
		}
		if !strings.Contains(ansi.Strip(rendered5), "Add Note") {
			t.Fatalf("expected 5-row terminal with error to preserve title, got:\n%s", rendered5)
		}
	})
}
