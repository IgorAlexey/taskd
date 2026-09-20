package main

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestEscapeClearsStatusFilterOnEmptyMatch(t *testing.T) {
	m := newModel(config{icons: false, refresh: time.Hour}, nil)
	m.width = 120
	m.height = 24
	m.mode = modeTable
	m.project = "alpha"
	m.projects = []string{"alpha"}
	m.tasks = []task{{ID: "t1", Project: "alpha", Status: "pending", Body: "task"}}
	m.filter = "done"
	m.msg = "notice"
	m.rebuild()
	if len(m.shown) != 0 {
		t.Fatalf("expected empty shown initially, got %d", len(m.shown))
	}

	res, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = res.(model)
	if m.msg != "" || m.filter != "done" {
		t.Fatalf("expected escape to dismiss msg first; msg=%q, filter=%q", m.msg, m.filter)
	}

	res, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = res.(model)
	if m.filter != "" {
		t.Fatalf("expected filter cleared to empty, got %q", m.filter)
	}
	if len(m.shown) != 1 || m.tasks[m.shown[0]].ID != "t1" {
		t.Fatalf("expected queue restored, got shown: %v", m.shown)
	}
}
