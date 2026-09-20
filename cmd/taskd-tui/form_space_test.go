package main

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestFormSpaceKeySave(t *testing.T) {
	t.Run("ValidationFailureOnSpace", func(t *testing.T) {
		m := newModel(config{project: "default", refresh: time.Hour}, nil)
		up, _ := m.Update(tea.KeyPressMsg{Text: "n"})
		m = up.(model)
		m.form.setFocus(fieldSave)

		up, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
		m = up.(model)
		if cmd != nil || m.form.errText != "missing body" {
			t.Fatalf("expected validation error without cmd, err=%q cmd=%v", m.form.errText, cmd)
		}
	})

	t.Run("SubmitOnSpace", func(t *testing.T) {
		m := newModel(config{project: "default", refresh: time.Hour}, nil)
		up, _ := m.Update(tea.KeyPressMsg{Text: "n"})
		m = up.(model)
		m.form.body.SetValue("test body")
		m.form.setFocus(fieldSave)

		up, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
		m = up.(model)
		if cmd == nil || m.formSeq != 1 {
			t.Fatalf("expected form submission cmd on Space, got cmd=%v seq=%d", cmd, m.formSeq)
		}
	})

	t.Run("CancelOnSpace", func(t *testing.T) {
		m := newModel(config{project: "default", refresh: time.Hour}, nil)
		up, _ := m.Update(tea.KeyPressMsg{Text: "n"})
		m = up.(model)
		m.form.setFocus(fieldCancel)

		up, _ = m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
		m = up.(model)
		if m.mode != modeTable {
			t.Fatalf("expected modeTable after cancel on Space, got %v", m.mode)
		}
	})

	t.Run("IgnoresModifiedSpace", func(t *testing.T) {
		m := newModel(config{project: "default", refresh: time.Hour}, nil)
		up, _ := m.Update(tea.KeyPressMsg{Text: "n"})
		m = up.(model)
		m.form.setFocus(fieldSave)

		up, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
		m = up.(model)
		if cmd != nil || m.formSeq != 0 {
			t.Fatal("Ctrl+Space should not trigger save")
		}
	})
}
