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

func TestSearchNavigationKeys(t *testing.T) {
	m := newModel(config{refresh: time.Hour}, nil)
	m.width = 80
	m.height = 24
	m.mode = modeTable
	m.tasks = []task{
		{ID: "task-1", Body: "alpha first\nfirst detail"},
		{ID: "task-2", Body: "alpha second\nsecond detail"},
		{ID: "task-3", Body: "alpha third\nthird detail"},
	}
	m.query = "alpha"
	m.rebuildShown()
	m.cursor = 0
	m.clamp()
	m.syncDetail()

	up, _ := m.Update(tea.KeyPressMsg{Text: "/"})
	m = up.(model)
	if m.mode != modeSearch {
		t.Fatalf("expected modeSearch, got %v", m.mode)
	}

	assertState := func(step string, wantCursor int, wantID, wantDetail string) {
		t.Helper()
		if m.mode != modeSearch {
			t.Fatalf("%s: expected modeSearch, got %v", step, m.mode)
		}
		if m.cursor != wantCursor {
			t.Fatalf("%s: expected cursor %d, got %d", step, wantCursor, m.cursor)
		}
		if sel, ok := m.selected(); !ok || sel.ID != wantID {
			t.Fatalf("%s: expected %s selected, got %+v", step, wantID, sel)
		}
		if m.detailID != wantID || !strings.Contains(m.detail.GetContent(), wantDetail) {
			t.Fatalf("%s: expected detail %s (%q), got %q (%q)",
				step, wantID, wantDetail, m.detailID, m.detail.GetContent())
		}
	}

	assertState("initial", 0, "task-1", "first detail")

	press := func(step string, msg tea.Msg, wantCursor int, wantID, wantDetail string) {
		up, _ = m.Update(msg)
		m = up.(model)
		assertState(step, wantCursor, wantID, wantDetail)
	}

	press("down", tea.KeyPressMsg{Code: tea.KeyDown}, 1, "task-2", "second detail")
	press("ctrl-n", tea.KeyPressMsg{Mod: tea.ModCtrl, Code: 'n'}, 2, "task-3", "third detail")
	press("down clamp", tea.KeyPressMsg{Code: tea.KeyDown}, 2, "task-3", "third detail")
	press("up", tea.KeyPressMsg{Code: tea.KeyUp}, 1, "task-2", "second detail")
	press("ctrl-p", tea.KeyPressMsg{Mod: tea.ModCtrl, Code: 'p'}, 0, "task-1", "first detail")
	press("up clamp", tea.KeyPressMsg{Code: tea.KeyUp}, 0, "task-1", "first detail")

	m.shown = nil
	for _, msg := range []tea.Msg{
		tea.KeyPressMsg{Code: tea.KeyDown},
		tea.KeyPressMsg{Code: tea.KeyUp},
		tea.KeyPressMsg{Mod: tea.ModCtrl, Code: 'n'},
		tea.KeyPressMsg{Mod: tea.ModCtrl, Code: 'p'},
	} {
		up, _ = m.Update(msg)
		m = up.(model)
		if m.mode != modeSearch {
			t.Fatalf("expected modeSearch on empty shown for %v, got %v", msg, m.mode)
		}
	}
}

func TestClickClearSearchFooter(t *testing.T) {
	m := newModel(config{refresh: time.Hour}, nil)
	m.width = 100
	m.height = 24
	m.mode = modeTable
	m.query = "alpha"
	m.tasks = []task{
		{ID: "1", Status: "pending", Body: "alpha task"},
	}
	m.rebuildShown()

	view := ansi.Strip(m.View().Content)
	want := `filter "alpha" [Esc clear]`
	if !strings.Contains(view, want) {
		t.Fatalf("expected %q in view, got:\n%s", want, view)
	}

	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	footer := lines[len(lines)-1]
	idx := strings.Index(footer, "[Esc clear]")
	if idx == -1 {
		t.Fatalf("footer missing [Esc clear]: %q", footer)
	}

	up, _ := m.Update(tea.MouseClickMsg{
		X:      idx + 2,
		Y:      m.height - 1,
		Button: tea.MouseLeft,
	})
	m = up.(model)

	if m.query != "" {
		t.Fatalf("expected query cleared, got %q", m.query)
	}

	restored := ansi.Strip(m.View().Content)
	if strings.Contains(restored, want) {
		t.Fatalf("unexpected [Esc clear] in restored footer:\n%s", restored)
	}
}

func TestFilterTargetsDetailAndZoomPreserveShortcuts(t *testing.T) {
	m := newModel(config{refresh: time.Hour}, nil)
	m.width = 100
	m.height = 24
	m.query = "alpha"
	m.tasks = []task{
		{ID: "1", Status: "pending", Body: "alpha task"},
	}
	m.rebuildShown()

	m.mode = modeDetail
	targets := m.footerTargets()
	for _, target := range targets {
		if target.action == "clear_search" {
			t.Fatalf("expected detail mode not to expose clear_search target")
		}
	}

	m.mode = modeZoom
	targets = m.footerTargets()
	for _, target := range targets {
		if target.action == "clear_search" {
			t.Fatalf("expected zoom mode not to expose clear_search target")
		}
	}
}
