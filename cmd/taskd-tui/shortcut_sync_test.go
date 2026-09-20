package main

import (
	"bytes"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestShortcutDocsSync(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	usage := buf.String()

	const wantFilter = "0-5                 filter status (0: all, 1: pending, 2: leased, 3: done, 4: buried, 5: live)"
	if !strings.Contains(usage, wantFilter) {
		t.Fatalf("printUsage missing complete status filter enumeration; want substring %q", wantFilter)
	}

	const wantCopy = "y/Y                 copy task ID / body to clipboard"
	if !strings.Contains(usage, wantCopy) {
		t.Fatalf("printUsage missing copy shortcut; want substring %q", wantCopy)
	}

	for _, want := range []string{
		"a                   add note",
		"s, S                cycle sort column forward / backward",
		"p, P                cycle project filter forward / backward",
		"Esc                 reset filters / exit panes",
		"Enter               activate detail pane",
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("printUsage missing %q", want)
		}
	}

	const wantSort = "s, S                cycle sort column forward / backward"
	if !strings.Contains(usage, wantSort) {
		t.Fatalf("printUsage missing sort shortcut; want substring %q", wantSort)
	}

	m := newModel(config{icons: true}, nil)
	m.width = 100
	m.height = 24

	m.mode = modeTable
	tableView := ansi.Strip(m.View().Content)
	if !strings.Contains(tableView, "y/Y copy") {
		t.Errorf("rendered table footer missing 'y/Y copy'; got:\n%s", tableView)
	}

	m.mode = modeDetail
	detailView := ansi.Strip(m.View().Content)
	if !strings.Contains(detailView, "y/Y copy") {
		t.Errorf("rendered detail footer missing 'y/Y copy'; got:\n%s", detailView)
	}

	m.help = newHelpModel(m.width, m.height, modeTable, m.theme)
	helpContent := ansi.Strip(m.help.View(m.height, m.theme))
	if !strings.Contains(helpContent, "[y/Y]") || !strings.Contains(helpContent, "copy id/body") {
		t.Errorf("help modal missing [y/Y] copy id/body; got:\n%s", helpContent)
	}
	if !strings.Contains(helpContent, "[0-5]") || !strings.Contains(helpContent, "filter (status)") {
		t.Errorf("help modal missing [0-5] filter (status); got:\n%s", helpContent)
	}
	for _, key := range []string{"[s/S]", "[Esc]", "[Enter]"} {
		if !strings.Contains(helpContent, key) {
			t.Errorf("help modal missing %s; got:\n%s", key, helpContent)
		}
	}
	if !strings.Contains(helpContent, "[a]") || !strings.Contains(helpContent, "note") {
		t.Errorf("help modal missing [a] note; got:\n%s", helpContent)
	}
	if !strings.Contains(helpContent, "[p/P]") || !strings.Contains(helpContent, "project") {
		t.Errorf("help modal missing [p/P] project; got:\n%s", helpContent)
	}

	m.mode = modeTable
	up, _ := m.Update(tea.KeyPressMsg{Text: "4"})
	m = up.(model)
	if m.filter != "buried" {
		t.Errorf("key '4' did not set buried filter; got %q", m.filter)
	}

	m.filter = ""
	m.tasks = []task{{ID: "task-12345", Body: "sample body", Status: "pending"}}
	m.rebuildShown()
	m.cursor = 0

	_, cmdY := m.Update(tea.KeyPressMsg{Text: "y"})
	if cmdY == nil {
		t.Fatal("key 'y' did not return command")
	}

	_, cmdUpperY := m.Update(tea.KeyPressMsg{Text: "Y"})
	if cmdUpperY == nil {
		t.Fatal("key 'Y' did not return command")
	}
}

func TestHelpSync(t *testing.T) {
	TestShortcutDocsSync(t)
}
