package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestDetailTaskNavigation(t *testing.T) {
	tasks := []task{
		{ID: 1, Body: "body 1", Status: "pending"},
		{ID: 2, Body: "body 2", Status: "pending"},
		{ID: 3, Body: "body 3", Status: "pending"},
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

		if m.detailID != 1 {
			t.Fatalf("expected initial detailID 1, got %d", m.detailID)
		}

		up, _ := m.Update(tea.KeyPressMsg{Text: "]"})
		m = up.(model)
		if m.mode != modeDetail {
			t.Fatalf("expected modeDetail after ], got %v", m.mode)
		}
		if m.cursor != 1 || m.detailID != 2 {
			t.Fatalf("expected cursor 1 and 2, got cursor %d and %d", m.cursor, m.detailID)
		}
		if !strings.Contains(m.detail.GetContent(), "body 2") {
			t.Fatalf("detail pane missing body 2; got:\n%s", m.detail.GetContent())
		}

		up, _ = m.Update(tea.KeyPressMsg{Text: "]"})
		m = up.(model)
		if m.cursor != 2 || m.detailID != 3 {
			t.Fatalf("expected cursor 2 and 3, got cursor %d and %d", m.cursor, m.detailID)
		}

		up, _ = m.Update(tea.KeyPressMsg{Text: "]"})
		m = up.(model)
		if m.cursor != 2 || m.detailID != 3 {
			t.Fatalf("expected clamp at cursor 2 and 3, got cursor %d and %d", m.cursor, m.detailID)
		}

		up, _ = m.Update(tea.KeyPressMsg{Text: "["})
		m = up.(model)
		if m.cursor != 1 || m.detailID != 2 {
			t.Fatalf("expected cursor 1 and 2 after [, got cursor %d and %d", m.cursor, m.detailID)
		}

		up, _ = m.Update(tea.KeyPressMsg{Text: "["})
		m = up.(model)
		if m.cursor != 0 || m.detailID != 1 {
			t.Fatalf("expected cursor 0 and 1 after [, got cursor %d and %d", m.cursor, m.detailID)
		}

		up, _ = m.Update(tea.KeyPressMsg{Text: "["})
		m = up.(model)
		if m.cursor != 0 || m.detailID != 1 {
			t.Fatalf("expected clamp at cursor 0 and 1, got cursor %d and %d", m.cursor, m.detailID)
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
		if m.cursor != 1 || m.detailID != 2 {
			t.Fatalf("expected cursor 1 and 2, got cursor %d and %d", m.cursor, m.detailID)
		}

		up, _ = m.Update(tea.KeyPressMsg{Text: "["})
		m = up.(model)
		if m.mode != modeZoom {
			t.Fatalf("expected modeZoom after [, got %v", m.mode)
		}
		if m.cursor != 0 || m.detailID != 1 {
			t.Fatalf("expected cursor 0 and 1, got cursor %d and %d", m.cursor, m.detailID)
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
		if m.cursor != 1 || m.detailID != 2 {
			t.Fatalf("expected cursor 1 on KeyCode ], got cursor %d and %d", m.cursor, m.detailID)
		}

		up, _ = m.Update(tea.KeyPressMsg{Code: '['})
		m = up.(model)
		if m.cursor != 0 || m.detailID != 1 {
			t.Fatalf("expected cursor 0 on KeyCode [, got cursor %d and %d", m.cursor, m.detailID)
		}
	})
}
