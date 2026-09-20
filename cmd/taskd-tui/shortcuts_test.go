package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var shortcutDocs = []struct {
	modal string
	usage string
}{
	{"[j/k] move", "j, Down"},
	{"[g/G] top/bottom", "g "},
	{"[Ctrl+D/U] half page", "Ctrl+D, Ctrl+U"},
	{"[0-5] filter status", "0 "},
	{"[/] keyword filter", "/ "},
	{"[p] cycle project", "p "},
	{"[n] new task", "n "},
	{"[e] edit task", "e "},
	{"[c] claim task", "c "},
	{"[u] release task", "u "},
	{"[t] touch lease", "t "},
	{"[x] complete task", "x "},
	{"[b] bury own task", "b "},
	{"[K] kick task", "K "},
	{"[D] delete task", "D "},
	{"[+/-] priority", "+ / ="},
	{"[z] zoom task body", "z "},
	{"[y] copy ID", "y "},
	{"[Y] copy body", "Y "},
	{"[r] refresh", "r, R"},
	{"[Tab] toggle pane focus", "Tab, Backtab"},
	{"[?] help", "? "},
	{"[q] quit", "q "},
}

func TestHelpModalRendersEveryShortcut(t *testing.T) {
	u, query, screenText, cleanup := setupTestApp(t)
	defer cleanup()

	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, '?', 0))
	})

	var modal *tview.Modal
	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal != nil
	})

	eventually(t, func() bool {
		return strings.Contains(screenText(), "Keyboard Shortcuts")
	})

	st := screenText()
	for _, d := range shortcutDocs {
		if !strings.Contains(st, d.modal) {
			t.Errorf("help modal screen missing %q in:\n%s", d.modal, st)
		}
	}
}

func TestUsageDocumentsEveryShortcut(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	out := buf.String()
	for _, d := range shortcutDocs {
		if !strings.Contains(out, "\n  "+d.usage) {
			t.Errorf("usage output missing shortcut %q (modal entry %q):\n%s", d.usage, d.modal, out)
		}
	}
	for _, want := range []string{"Show this keyboard shortcut help", "half a page"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage output missing %q:\n%s", want, out)
		}
	}
}

func TestFooterAdvertisesHelp(t *testing.T) {
	_, _, screenText, cleanup := setupTestApp(t)
	defer cleanup()

	eventually(t, func() bool {
		return strings.Contains(screenText(), "[?] help")
	})

	st := screenText()
	rows := strings.Split(st, "\n")
	if len(rows) < 25 {
		t.Fatalf("expected at least 25 screen rows, got %d", len(rows))
	}
	footer := rows[24]
	for _, want := range []string{"[j/k]", "[e] edit", "[+/-] pri", "[D] del", "[z] zoom", "[?] help", "[q] quit"} {
		if !strings.Contains(footer, want) {
			t.Errorf("footer missing %q: %q", want, footer)
		}
	}
	if len(footer) > 80 {
		t.Errorf("footer wider than 80 columns: %d", len(footer))
	}
}
