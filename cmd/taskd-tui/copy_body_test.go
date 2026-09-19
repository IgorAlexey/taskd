package main

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestCopyTaskBody(t *testing.T) {
	u, _, _ := stub(t)
	ts, err := u.fetch()
	if err != nil || len(ts) != 3 {
		t.Fatalf("fetch: %v %d", err, len(ts))
	}
	u.render(ts)

	var copied string
	origCopy := copyToClipboard
	copyToClipboard = func(text string) {
		copied = text
	}
	t.Cleanup(func() { copyToClipboard = origCopy })

	u.keys(tcell.NewEventKey(tcell.KeyRune, 'Y', 0))
	if copied != "first task\n\nWhy: a" {
		t.Fatalf("copied Body = %q, want %q", copied, "first task\n\nWhy: a")
	}
	if !strings.Contains(u.status.GetText(true), "copied body to clipboard") {
		t.Fatalf("status line missing copied body confirmation: %q", u.status.GetText(true))
	}

	u.table.Select(2, 0)
	u.keys(tcell.NewEventKey(tcell.KeyRune, 'Y', 0))
	if copied != "second" {
		t.Fatalf("copied Body = %q, want %q", copied, "second")
	}
	if !strings.Contains(u.status.GetText(true), "copied body to clipboard") {
		t.Fatalf("status line missing copied body confirmation: %q", u.status.GetText(true))
	}

	u.table.Select(3, 0)
	u.bodyKeys(tcell.NewEventKey(tcell.KeyRune, 'Y', 0))
	if copied != "third" {
		t.Fatalf("copied Body = %q, want %q", copied, "third")
	}
	if !strings.Contains(u.status.GetText(true), "copied body to clipboard") {
		t.Fatalf("status line missing copied body confirmation: %q", u.status.GetText(true))
	}

	u.toggleZoom()
	if !u.zoomed {
		t.Fatal("expected zoomed")
	}
	copied = ""
	u.bodyKeys(tcell.NewEventKey(tcell.KeyRune, 'Y', 0))
	if copied != "third" {
		t.Fatalf("copied Body in zoom = %q, want %q", copied, "third")
	}
	if !strings.Contains(u.status.GetText(true), "copied body to clipboard") {
		t.Fatalf("status line missing copied body confirmation in zoom: %q", u.status.GetText(true))
	}
	u.toggleZoom()

	u.shown = nil
	copied = ""
	u.keys(tcell.NewEventKey(tcell.KeyRune, 'Y', 0))
	if copied != "" {
		t.Fatalf("copied on empty list = %q, want empty", copied)
	}

	copied = ""
	u.bodyKeys(tcell.NewEventKey(tcell.KeyRune, 'Y', 0))
	if copied != "" {
		t.Fatalf("copied on empty list in bodyKeys = %q, want empty", copied)
	}
}
