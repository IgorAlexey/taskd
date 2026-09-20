package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestHelpViewAt80x24(t *testing.T) {
	m := newModel(config{icons: true, refresh: time.Hour}, nil)
	m.width = 80
	m.height = 24
	m.mode = modeTable

	updated, _ := m.Update(tea.KeyPressMsg{Text: "?"})
	m = updated.(model)

	if m.mode != modeHelp {
		t.Fatalf("expected modeHelp, got %v", m.mode)
	}

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "[j/k]") {
		t.Fatalf("expected view to contain [j/k], got:\n%s", view)
	}
	if !strings.Contains(view, "[q]") || !strings.Contains(view, "quit") {
		t.Fatalf("expected view to contain [q] and quit, got:\n%s", view)
	}
	if !strings.Contains(view, "Close") {
		t.Fatalf("expected view to contain Close, got:\n%s", view)
	}
}

func TestHelpScrollAt80x12(t *testing.T) {
	m := newModel(config{icons: true, refresh: time.Hour}, nil)
	m.width = 80
	m.height = 12
	m.mode = modeTable

	updated, _ := m.Update(tea.KeyPressMsg{Text: "?"})
	m = updated.(model)

	if m.mode != modeHelp {
		t.Fatalf("expected modeHelp, got %v", m.mode)
	}

	a := ansi.Strip(m.View().Content)
	if !strings.Contains(a, "[j/k]") {
		t.Fatalf("expected initial view at 80x12 to contain [j/k], got:\n%s", a)
	}
	if strings.Contains(a, "quit") {
		t.Fatalf("expected initial view at 80x12 not to contain quit before scroll, got:\n%s", a)
	}
	if !strings.Contains(a, "Close") {
		t.Fatalf("expected pinned footer to contain Close at 80x12, got:\n%s", a)
	}

	for i := 0; i < 5; i++ {
		updated, _ = m.Update(tea.KeyPressMsg{Text: "j"})
		m = updated.(model)
	}

	b := ansi.Strip(m.View().Content)
	if a == b {
		t.Fatalf("expected view to change after scrolling, but a == b:\n%s", b)
	}
	if !strings.Contains(b, "[q]") || !strings.Contains(b, "quit") {
		t.Fatalf("expected scrolled view at 80x12 to contain [q] and quit, got:\n%s", b)
	}
	if !strings.Contains(b, "Close") {
		t.Fatalf("expected scrolled view at 80x12 to contain Close, got:\n%s", b)
	}

	for i := 0; i < 5; i++ {
		updated, _ = m.Update(tea.KeyPressMsg{Text: "k"})
		m = updated.(model)
	}
	c := ansi.Strip(m.View().Content)
	if c != a {
		t.Fatalf("expected view after scrolling up to match initial view, got:\n%s\nwant:\n%s", c, a)
	}
}

func TestHelpNarrowTerminal40x20(t *testing.T) {
	m := newModel(config{icons: true, refresh: time.Hour}, nil)
	m.width = 40
	m.height = 20
	m.mode = modeTable

	updated, _ := m.Update(tea.KeyPressMsg{Text: "?"})
	m = updated.(model)

	if m.mode != modeHelp {
		t.Fatalf("expected modeHelp, got %v", m.mode)
	}

	a := ansi.Strip(m.View().Content)
	if !strings.Contains(a, "[j/k]") {
		t.Fatalf("expected initial view at 40x20 to contain [j/k], got:\n%s", a)
	}
	if strings.Contains(a, "quit") {
		t.Fatalf("expected initial view at 40x20 not to contain quit before scroll, got:\n%s", a)
	}
	if !strings.Contains(a, "Close") {
		t.Fatalf("expected pinned footer to contain Close at 40x20, got:\n%s", a)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Text: "G"})
	m = updated.(model)

	b := ansi.Strip(m.View().Content)
	if !strings.Contains(b, "quit") {
		t.Fatalf("expected scrolled view at 40x20 to contain quit, got:\n%s", b)
	}
	if !strings.Contains(b, "Close") {
		t.Fatalf("expected pinned footer to contain Close at 40x20 after scroll, got:\n%s", b)
	}
}

func TestHelpDismiss(t *testing.T) {
	m := newModel(config{icons: true, refresh: time.Hour}, nil)
	m.width = 80
	m.height = 24
	m.mode = modeTable

	updated, _ := m.Update(tea.KeyPressMsg{Text: "?"})
	m = updated.(model)
	if m.mode != modeHelp {
		t.Fatalf("expected modeHelp, got %v", m.mode)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(model)
	if m.mode != modeTable {
		t.Fatalf("expected modeTable after Escape, got %v", m.mode)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Text: "?"})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyPressMsg{Text: "q"})
	m = updated.(model)
	if m.mode != modeTable {
		t.Fatalf("expected modeTable after q, got %v", m.mode)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Text: "?"})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyPressMsg{Text: "?"})
	m = updated.(model)
	if m.mode != modeTable {
		t.Fatalf("expected modeTable after ?, got %v", m.mode)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Text: "?"})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if m.mode != modeTable {
		t.Fatalf("expected modeTable after Enter, got %v", m.mode)
	}
}

func TestHelpFromDetailRestoresDetail(t *testing.T) {
	m := newModel(config{icons: true, refresh: time.Hour}, nil)
	m.width = 80
	m.height = 24
	m.mode = modeDetail

	updated, _ := m.Update(tea.KeyPressMsg{Text: "?"})
	m = updated.(model)
	if m.mode != modeHelp {
		t.Fatalf("expected modeHelp, got %v", m.mode)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(model)
	if m.mode != modeDetail {
		t.Fatalf("expected modeDetail after Escape from help, got %v", m.mode)
	}
}

func TestHelpNavigationAndBounds(t *testing.T) {
	m := newModel(config{icons: true, refresh: time.Hour}, nil)
	m.width = 80
	m.height = 12
	m.mode = modeTable

	updated, _ := m.Update(tea.KeyPressMsg{Text: "?"})
	m = updated.(model)

	updated, _ = m.Update(tea.KeyPressMsg{Text: "G"})
	m = updated.(model)
	if !m.help.vp.AtBottom() {
		t.Fatalf("expected help viewport to be at bottom after G")
	}
	botOffset := m.help.vp.YOffset()

	updated, _ = m.Update(tea.KeyPressMsg{Text: "j"})
	m = updated.(model)
	if m.help.vp.YOffset() != botOffset {
		t.Fatalf("help offset exceeded max: got %d, want %d", m.help.vp.YOffset(), botOffset)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Text: "g"})
	m = updated.(model)
	if m.help.vp.YOffset() != 0 {
		t.Fatalf("expected help offset 0 after g, got %d", m.help.vp.YOffset())
	}

	updated, _ = m.Update(tea.KeyPressMsg{Text: "k"})
	m = updated.(model)
	if m.help.vp.YOffset() != 0 {
		t.Fatalf("help offset below 0: got %d", m.help.vp.YOffset())
	}

	updated, _ = m.Update(tea.KeyPressMsg{Mod: tea.ModCtrl, Code: 'd'})
	m = updated.(model)
	if m.help.vp.YOffset() == 0 {
		t.Fatalf("expected help offset > 0 after ctrl-d")
	}

	updated, _ = m.Update(tea.KeyPressMsg{Mod: tea.ModCtrl, Code: 'u'})
	m = updated.(model)
	if m.help.vp.YOffset() != 0 {
		t.Fatalf("expected help offset 0 after ctrl-u, got %d", m.help.vp.YOffset())
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	m = updated.(model)
	if m.help.vp.YOffset() == 0 {
		t.Fatalf("expected help offset > 0 after PgDown")
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	m = updated.(model)
	if m.help.vp.YOffset() != 0 {
		t.Fatalf("expected help offset 0 after PgUp, got %d", m.help.vp.YOffset())
	}
}

func TestHelpOverlayWorkerShortcuts(t *testing.T) {
	th := newTheme(true)
	h := newHelpModel(100, 30, modeTable, th)
	content := ansi.Strip(h.View(30, th))
	if !strings.Contains(content, "[w/W]") || !strings.Contains(content, "worker") {
		t.Fatalf("help overlay missing [w/W] worker; got:\n%s", content)
	}
}
func TestHelpDismissOnMouseClick(t *testing.T) {
	for _, prev := range []mode{modeTable, modeDetail, modeZoom} {
		m := newModel(config{icons: true, refresh: time.Hour}, nil)
		m.width = 80
		m.height = 24
		m.mode = prev

		updated, _ := m.Update(tea.KeyPressMsg{Text: "?"})
		m = updated.(model)
		if m.mode != modeHelp {
			t.Fatalf("expected modeHelp, got %v", m.mode)
		}

		updated, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseRight, X: 10, Y: 10})
		m = updated.(model)
		if m.mode != modeHelp {
			t.Fatalf("expected right click to be ignored in modeHelp, got %v", m.mode)
		}

		updated, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 10, Y: 10})
		m = updated.(model)
		if m.mode != prev {
			t.Fatalf("expected mode %v after left click, got %v", prev, m.mode)
		}
	}
}
