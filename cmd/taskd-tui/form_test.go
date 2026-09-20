package main

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestFormCtrlEnterSubmits(t *testing.T) {
	fields := []formField{
		fieldProject,
		fieldPriority,
		fieldID,
		fieldBody,
	}

	for _, fld := range fields {
		f, _ := newCreateForm("test-project")
		f.body.SetValue("some body text")
		f.setFocus(fld)

		ctrlEnter := tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}
		up, _ := f.Update(ctrlEnter)
		if !up.done {
			t.Fatalf("expected f.done == true after Ctrl+Enter on field %v, got false", fld)
		}
	}

	t.Run("validation error on Ctrl+Enter", func(t *testing.T) {
		f, _ := newCreateForm("")
		f.setFocus(fieldBody)
		f.body.SetValue("some body")

		ctrlEnter := tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}
		up, _ := f.Update(ctrlEnter)
		if up.done {
			t.Fatal("expected f.done == false when project is empty on Ctrl+Enter")
		}
		if up.errText == "" {
			t.Fatal("expected error text when project is empty on Ctrl+Enter")
		}
	})
}

func TestEditFormCtrlEnterSubmits(t *testing.T) {
	tsk := task{ID: "task-edit-1", Project: "test", Priority: 2, Body: "initial body"}
	f, _ := newEditForm(tsk)
	f.body.SetValue("updated body")

	ctrlEnter := tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}
	up, _ := f.Update(ctrlEnter)
	if !up.done {
		t.Fatal("expected f.done == true after Ctrl+Enter on edit form, got false")
	}
}
