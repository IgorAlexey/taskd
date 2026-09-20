package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFooterWidth80Columns(t *testing.T) {
	m := newModel(config{icons: true, refresh: time.Hour}, nil)
	m.width = 80
	m.height = 24
	m.mode = modeTable
	m.projects = []string{"alpha", "beta"}
	m.workers = []string{"worker-1", "worker-2"}
	m.tasks = []task{
		{ID: 1, Project: "alpha", Worker: "worker-1", Status: "pending", Body: "sample"},
	}
	m.rebuildShown()
	m.cursor = 0

	view := m.View().Content
	lines := strings.Split(view, "\n")
	if len(lines) == 0 {
		t.Fatal("empty view content")
	}
	footerLine := ansi.Strip(lines[len(lines)-1])
	if ansi.StringWidth(footerLine) > 80 {
		t.Fatalf("footer line width %d exceeds 80 columns: %q", ansi.StringWidth(footerLine), footerLine)
	}

	strippedRight := ansi.Strip(m.footRight())
	footLeft := strings.TrimRight(strings.TrimSuffix(footerLine, strippedRight), " ")

	validTokens := make(map[string]bool)
	validTokens[m.glyph.ellipsis] = true
	for _, it := range append(m.footerItems(), m.footerPinned()...) {
		validTokens[it[0]] = true
		validTokens[it[1]] = true
	}
	for _, token := range strings.Fields(footLeft) {
		if !validTokens[token] {
			t.Fatalf("footLeft contains unexpected or chopped token %q in line: %q", token, footLeft)
		}
	}

	if !strings.Contains(footLeft, "? help") || !strings.Contains(footLeft, "q quit") {
		t.Errorf("footer missing essential shortcuts; got: %q", footLeft)
	}

	targets := m.footerTargets()
	if len(targets) == 0 {
		t.Fatal("expected non-empty footer targets at width 80")
	}
	runes := []rune(footerLine)
	for _, target := range targets {
		if target.end > 80 {
			t.Errorf("target %+v exceeds 80 columns", target)
		}
		if target.start < 0 || target.start >= target.end {
			t.Errorf("invalid target bounds: %+v", target)
		}
		label := string(runes[target.start:target.end])
		switch target.action {
		case "sort":
			if label != "s sort" {
				t.Errorf("sort target mismatch: %q", label)
			}
		case "create":
			if label != "n new" {
				t.Errorf("create target mismatch: %q", label)
			}
		case "edit":
			if label != "e edit" {
				t.Errorf("edit target mismatch: %q", label)
			}
		case "project":
			if label != "p project" {
				t.Errorf("project target mismatch: %q", label)
			}
		case "worker":
			if label != "w worker" {
				t.Errorf("worker target mismatch: %q", label)
			}
		case "quit":
			if label != "q quit" {
				t.Errorf("quit target mismatch: %q", label)
			}
		case "help":
			if label != "? help" {
				t.Errorf("help target mismatch: %q", label)
			}
		}
	}

	clickTests := []struct {
		token  string
		action string
		verify func(model, tea.Cmd)
	}{
		{
			token:  "s sort",
			action: "sort",
			verify: func(res model, _ tea.Cmd) {
				if res.sortCol == m.sortCol {
					t.Fatalf("clicking 's sort' did not cycle sort column")
				}
			},
		},
		{
			token:  "p project",
			action: "project",
			verify: func(res model, _ tea.Cmd) {
				if res.project == m.project {
					t.Fatalf("clicking 'p project' did not cycle project")
				}
			},
		},
		{
			token:  "w worker",
			action: "worker",
			verify: func(res model, _ tea.Cmd) {
				if res.worker == m.worker {
					t.Fatalf("clicking 'w worker' did not cycle worker")
				}
			},
		},
		{
			token:  "n new",
			action: "create",
			verify: func(res model, _ tea.Cmd) {
				if res.mode != modeForm || res.form.editing {
					t.Fatalf("clicking 'n new' did not open new task form; got mode %v", res.mode)
				}
			},
		},
		{
			token:  "? help",
			action: "help",
			verify: func(res model, _ tea.Cmd) {
				if res.mode != modeHelp {
					t.Fatalf("clicking '? help' did not open help modal; got mode %v", res.mode)
				}
			},
		},
		{
			token:  "q quit",
			action: "quit",
			verify: func(_ model, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("clicking 'q quit' did not return quit command")
				}
			},
		},
	}

	for _, tt := range clickTests {
		idx := strings.Index(footerLine, tt.token)
		if idx < 0 {
			t.Fatalf("footer missing %q at width 80 in: %q", tt.token, footerLine)
		}
		res, cmd := m.Update(tea.MouseClickMsg{
			X:      idx,
			Y:      m.height - 1,
			Button: tea.MouseLeft,
		})
		tt.verify(res.(model), cmd)
	}

	mWideRight := newModel(config{icons: true, refresh: time.Hour}, nil)
	mWideRight.width = 80
	mWideRight.height = 24
	mWideRight.mode = modeTable
	mWideRight.tasks = []task{{ID: 1, Status: "pending"}}
	mWideRight.rebuildShown()
	mWideRight.more = true
	mWideRight.total = 1000

	viewWide := mWideRight.View().Content
	linesWide := strings.Split(viewWide, "\n")
	footerLineWide := ansi.Strip(linesWide[len(linesWide)-1])
	if ansi.StringWidth(footerLineWide) > 80 {
		t.Fatalf("footer line width %d exceeds 80 columns: %q", ansi.StringWidth(footerLineWide), footerLineWide)
	}
	strippedRightWide := ansi.Strip(mWideRight.footRight())
	footLeftWide := strings.TrimRight(strings.TrimSuffix(footerLineWide, strippedRightWide), " ")
	for _, token := range strings.Fields(footLeftWide) {
		if !validTokens[token] {
			t.Fatalf("wide footLeft contains unexpected or chopped token %q in line: %q", token, footLeftWide)
		}
	}
	if !strings.Contains(footLeftWide, "? help") || !strings.Contains(footLeftWide, "q quit") {
		t.Errorf("wide footer missing essential shortcuts; got: %q", footLeftWide)
	}
	for _, target := range mWideRight.footerTargets() {
		if target.end > 80 {
			t.Errorf("wide target %+v exceeds 80 columns", target)
		}
	}
}
