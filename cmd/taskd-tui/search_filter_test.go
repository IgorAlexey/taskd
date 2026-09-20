package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSearchFilterIndicatorInTable(t *testing.T) {
	m := newModel(config{refresh: time.Hour}, nil)
	m.width = 80
	m.height = 24
	m.mode = modeTable
	m.query = "alpha"
	m.tasks = []task{{ID: "1", Status: "pending", Body: "hello"}}
	m.rebuildShown()

	filtered := ansi.Strip(m.View().Content)
	want := `filter "alpha" [Esc clear]`
	if !strings.Contains(filtered, want) {
		t.Fatalf("expected %q in view, got:\n%s", want, filtered)
	}

	up, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = up.(model)
	if m.query != "" {
		t.Fatalf("expected query cleared, got %q", m.query)
	}

	restored := ansi.Strip(m.View().Content)
	if strings.Contains(restored, want) {
		t.Fatalf("unexpected filter indicator in restored view:\n%s", restored)
	}
	if !strings.Contains(restored, "0-4") || !strings.Contains(restored, "j/k") {
		t.Fatalf("expected default table legend restored, got:\n%s", restored)
	}
}

func TestSearchFilterIndicatorTruncation(t *testing.T) {
	m := newModel(config{refresh: time.Hour}, nil)
	m.width = 40
	m.height = 10
	m.mode = modeTable
	m.query = strings.Repeat("x", 80)
	m.tasks = []task{{ID: "1", Status: "pending", Body: "hello"}}
	m.rebuildShown()

	view := ansi.Strip(m.View().Content)
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	if len(lines) != 10 {
		t.Fatalf("expected view height 10, got %d lines", len(lines))
	}
	footer := lines[len(lines)-1]
	if len(footer) > 40 {
		t.Fatalf("footer exceeded width 40: %d chars", len(footer))
	}
	if !strings.Contains(footer, "filter") || !strings.Contains(footer, "[Esc clear]") {
		t.Fatalf("footer missing filter or esc hint: %q", footer)
	}
}
