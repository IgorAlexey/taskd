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
	m.tasks = []task{{ID: 1, Project: "alpha", Status: "pending", Body: "task"}}
	m.filter = "done"
	m.setError("notice")
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
	if len(m.shown) != 1 || m.tasks[m.shown[0]].ID != 1 {
		t.Fatalf("expected queue restored, got shown: %v", m.shown)
	}
}

func TestCycleProjectBackward(t *testing.T) {
	m := newModel(config{icons: false, refresh: time.Hour}, nil)
	m.width = 120
	m.height = 24
	m.mode = modeTable
	m.projects = []string{"alpha", "beta", "gamma"}
	m.project = ""

	up, _ := m.Update(tea.KeyPressMsg{Text: "P"})
	m = up.(model)
	if m.project != "gamma" {
		t.Fatalf("expected 'P' from empty to wrap to last project 'gamma', got %q", m.project)
	}

	up, _ = m.Update(tea.KeyPressMsg{Text: "P"})
	m = up.(model)
	if m.project != "beta" {
		t.Fatalf("expected 'P' to cycle to 'beta', got %q", m.project)
	}

	up, _ = m.Update(tea.KeyPressMsg{Text: "P"})
	m = up.(model)
	if m.project != "alpha" {
		t.Fatalf("expected 'P' to cycle to 'alpha', got %q", m.project)
	}

	up, _ = m.Update(tea.KeyPressMsg{Text: "P"})
	m = up.(model)
	if m.project != "" {
		t.Fatalf("expected 'P' from 'alpha' to cycle to all projects '', got %q", m.project)
	}

	up, _ = m.Update(tea.KeyPressMsg{Text: "p"})
	m = up.(model)
	if m.project != "alpha" {
		t.Fatalf("expected 'p' from '' to cycle forward to 'alpha', got %q", m.project)
	}

	up, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = up.(model)
	if m.project != "" {
		t.Fatalf("expected Escape to clear project filter when query is empty, got %q", m.project)
	}

	m.project = "alpha"
	m.query = "needle"
	up, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = up.(model)
	if m.query != "" {
		t.Fatalf("expected Escape to clear query first, got %q", m.query)
	}
	if m.project != "alpha" {
		t.Fatalf("expected project to remain unchanged when query was cleared, got %q", m.project)
	}

	up, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = up.(model)
	if m.project != "" {
		t.Fatalf("expected second Escape to clear project filter, got %q", m.project)
	}

	m.worker = "w1"
	m.workers = []string{"w1"}
	up, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = up.(model)
	if m.worker != "" {
		t.Fatalf("expected Escape to clear worker filter, got %q", m.worker)
	}
}
