package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestDetailTaskNavigation(t *testing.T) {
	tasks := []task{
		{ID: "task-1", Body: "body 1", Status: "pending"},
		{ID: "task-2", Body: "body 2", Status: "pending"},
		{ID: "task-3", Body: "body 3", Status: "pending"},
	}

	t.Run("modeDetail navigation", func(t *testing.T) {
		m := newModel(config{refresh: time.Hour}, nil)
		m.width = 100
		m.height = 24
		m.tasks = tasks
		m.rebuildShown()
		m.cursor = 0
		m.mode = modeDetail
		m.syncDetail()

		if m.detailID != "task-1" {
			t.Fatalf("expected initial detailID task-1, got %q", m.detailID)
		}

		up, _ := m.Update(tea.KeyPressMsg{Text: "]"})
		m = up.(model)
		if m.mode != modeDetail {
			t.Fatalf("expected modeDetail after ], got %v", m.mode)
		}
		if m.cursor != 1 || m.detailID != "task-2" {
			t.Fatalf("expected cursor 1 and task-2, got cursor %d and %q", m.cursor, m.detailID)
		}
		if !strings.Contains(m.detail.GetContent(), "body 2") {
			t.Fatalf("detail pane missing body 2; got:\n%s", m.detail.GetContent())
		}

		up, _ = m.Update(tea.KeyPressMsg{Text: "]"})
		m = up.(model)
		if m.cursor != 2 || m.detailID != "task-3" {
			t.Fatalf("expected cursor 2 and task-3, got cursor %d and %q", m.cursor, m.detailID)
		}

		up, _ = m.Update(tea.KeyPressMsg{Text: "]"})
		m = up.(model)
		if m.cursor != 2 || m.detailID != "task-3" {
			t.Fatalf("expected clamp at cursor 2 and task-3, got cursor %d and %q", m.cursor, m.detailID)
		}

		up, _ = m.Update(tea.KeyPressMsg{Text: "["})
		m = up.(model)
		if m.cursor != 1 || m.detailID != "task-2" {
			t.Fatalf("expected cursor 1 and task-2 after [, got cursor %d and %q", m.cursor, m.detailID)
		}

		up, _ = m.Update(tea.KeyPressMsg{Text: "["})
		m = up.(model)
		if m.cursor != 0 || m.detailID != "task-1" {
			t.Fatalf("expected cursor 0 and task-1 after [, got cursor %d and %q", m.cursor, m.detailID)
		}

		up, _ = m.Update(tea.KeyPressMsg{Text: "["})
		m = up.(model)
		if m.cursor != 0 || m.detailID != "task-1" {
			t.Fatalf("expected clamp at cursor 0 and task-1, got cursor %d and %q", m.cursor, m.detailID)
		}
	})

	t.Run("modeZoom navigation", func(t *testing.T) {
		m := newModel(config{refresh: time.Hour}, nil)
		m.width = 100
		m.height = 24
		m.tasks = tasks
		m.rebuildShown()
		m.cursor = 0
		m.mode = modeZoom
		m.syncDetail()

		up, _ := m.Update(tea.KeyPressMsg{Text: "]"})
		m = up.(model)
		if m.mode != modeZoom {
			t.Fatalf("expected modeZoom after ], got %v", m.mode)
		}
		if m.cursor != 1 || m.detailID != "task-2" {
			t.Fatalf("expected cursor 1 and task-2, got cursor %d and %q", m.cursor, m.detailID)
		}

		up, _ = m.Update(tea.KeyPressMsg{Text: "["})
		m = up.(model)
		if m.mode != modeZoom {
			t.Fatalf("expected modeZoom after [, got %v", m.mode)
		}
		if m.cursor != 0 || m.detailID != "task-1" {
			t.Fatalf("expected cursor 0 and task-1, got cursor %d and %q", m.cursor, m.detailID)
		}
	})

	t.Run("KeyCode navigation", func(t *testing.T) {
		m := newModel(config{refresh: time.Hour}, nil)
		m.width = 100
		m.height = 24
		m.tasks = tasks
		m.rebuildShown()
		m.cursor = 0
		m.mode = modeDetail
		m.syncDetail()

		up, _ := m.Update(tea.KeyPressMsg{Code: ']'})
		m = up.(model)
		if m.cursor != 1 || m.detailID != "task-2" {
			t.Fatalf("expected cursor 1 on KeyCode ], got cursor %d and %q", m.cursor, m.detailID)
		}

		up, _ = m.Update(tea.KeyPressMsg{Code: '['})
		m = up.(model)
		if m.cursor != 0 || m.detailID != "task-1" {
			t.Fatalf("expected cursor 0 on KeyCode [, got cursor %d and %q", m.cursor, m.detailID)
		}
	})
}
