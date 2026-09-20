package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestClickFormCancel(t *testing.T) {
	t.Run("CreateFormCleanCancelClick", func(t *testing.T) {
		m := newModel(config{project: "default", refresh: time.Hour}, nil)
		m.width = 80
		m.height = 24
		up, _ := m.Update(tea.KeyPressMsg{Text: "n"})
		m = up.(model)
		if m.mode != modeForm {
			t.Fatalf("expected modeForm, got %v", m.mode)
		}

		view := ansi.Strip(m.View().Content)
		if !strings.Contains(view, "[ save ]") || !strings.Contains(view, "[ cancel ]") {
			t.Fatalf("expected both [ save ] and [ cancel ] in form view, got:\n%s", view)
		}

		lines := strings.Split(view, "\n")
		cancelY, cancelX := -1, -1
		for y, raw := range lines {
			if strings.Contains(raw, "[ cancel ]") {
				cancelY = y
				cancelX = strings.Index(raw, "[ cancel ]")
				break
			}
		}
		if cancelY == -1 || cancelX == -1 {
			t.Fatalf("could not find [ cancel ] coordinates in view:\n%s", view)
		}

		up, _ = m.Update(tea.MouseClickMsg{
			Button: tea.MouseLeft,
			X:      cancelX + 2,
			Y:      cancelY,
		})
		m = up.(model)
		if m.mode != modeTable {
			t.Fatalf("expected modeTable after clicking clean [ cancel ], got %v", m.mode)
		}
	})

	t.Run("CreateFormDirtyCancelTriggersDiscardPrompt", func(t *testing.T) {
		m := newModel(config{project: "default", refresh: time.Hour}, nil)
		m.width = 80
		m.height = 24
		up, _ := m.Update(tea.KeyPressMsg{Text: "n"})
		m = up.(model)

		view := ansi.Strip(m.View().Content)
		lines := strings.Split(view, "\n")
		cancelY, cancelX := -1, -1
		for y, raw := range lines {
			if strings.Contains(raw, "[ cancel ]") {
				cancelY = y
				cancelX = strings.Index(raw, "[ cancel ]")
				break
			}
		}

		m.form.body.SetValue("draft text that must not be discarded silently")
		up, _ = m.Update(tea.MouseClickMsg{
			Button: tea.MouseLeft,
			X:      cancelX + 2,
			Y:      cancelY,
		})
		m = up.(model)
		if m.mode != modeForm {
			t.Fatalf("expected modeForm to stay open on discard prompt, got %v", m.mode)
		}
		if !m.form.discarding {
			t.Fatal("expected form to enter discarding state on dirty cancel click")
		}

		up, _ = m.Update(tea.KeyPressMsg{Text: "y"})
		m = up.(model)
		if m.mode != modeTable {
			t.Fatalf("expected modeTable after confirming discard, got %v", m.mode)
		}
	})

	t.Run("EditFormCleanCancelClick", func(t *testing.T) {
		m := newModel(config{project: "default", refresh: time.Hour}, nil)
		m.width = 80
		m.height = 24
		m.tasks = []task{
			{ID: 100, Project: "default", Priority: 2, Status: "pending", Body: "existing task body"},
		}
		m.rebuildShown()
		m.syncDetail()

		up, _ := m.Update(tea.KeyPressMsg{Text: "e"})
		m = up.(model)
		if m.mode != modeForm || !m.form.editing {
			t.Fatalf("expected editing modeForm, got mode %v editing %v", m.mode, m.form.editing)
		}

		view := ansi.Strip(m.View().Content)
		if !strings.Contains(view, "[ cancel ]") {
			t.Fatalf("expected [ cancel ] in edit form view, got:\n%s", view)
		}

		lines := strings.Split(view, "\n")
		cancelY, cancelX := -1, -1
		for y, raw := range lines {
			if strings.Contains(raw, "[ cancel ]") {
				cancelY = y
				cancelX = strings.Index(raw, "[ cancel ]")
				break
			}
		}

		up, _ = m.Update(tea.MouseClickMsg{
			Button: tea.MouseLeft,
			X:      cancelX + 2,
			Y:      cancelY,
		})
		m = up.(model)
		if m.mode != modeTable {
			t.Fatalf("expected modeTable after clicking clean [ cancel ] in edit mode, got %v", m.mode)
		}
	})

	t.Run("EditFormDirtyCancelTriggersDiscardPrompt", func(t *testing.T) {
		m := newModel(config{project: "default", refresh: time.Hour}, nil)
		m.width = 80
		m.height = 24
		m.tasks = []task{
			{ID: 100, Project: "default", Priority: 2, Status: "pending", Body: "existing task body"},
		}
		m.rebuildShown()
		m.syncDetail()

		up, _ := m.Update(tea.KeyPressMsg{Text: "e"})
		m = up.(model)

		view := ansi.Strip(m.View().Content)
		lines := strings.Split(view, "\n")
		cancelY, cancelX := -1, -1
		for y, raw := range lines {
			if strings.Contains(raw, "[ cancel ]") {
				cancelY = y
				cancelX = strings.Index(raw, "[ cancel ]")
				break
			}
		}

		m.form.priority.SetValue("9")
		up, _ = m.Update(tea.MouseClickMsg{
			Button: tea.MouseLeft,
			X:      cancelX + 2,
			Y:      cancelY,
		})
		m = up.(model)
		if !m.form.discarding {
			t.Fatal("expected edit form to enter discarding state on dirty cancel click")
		}

		up, _ = m.Update(tea.KeyPressMsg{Text: "y"})
		m = up.(model)
		if m.mode != modeTable {
			t.Fatalf("expected modeTable after confirming discard in edit mode, got %v", m.mode)
		}
	})
}
