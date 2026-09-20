package main

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestClickNoteModalButtons(t *testing.T) {
	m := newModel(config{icons: false, refresh: time.Hour}, newClient("http://localhost:8080"))
	m.width = 80
	m.height = 24
	m.tasks = []task{
		{ID: "task-note-1", Project: "test", Status: "pending", Body: "task body"},
	}
	m.rebuildShown()
	m.syncDetail()

	up, _ := m.Update(tea.KeyPressMsg{Text: "a"})
	m = up.(model)
	if m.mode != modeNote {
		t.Fatalf("expected modeNote, got %v", m.mode)
	}

	if len(m.note.layout(m.width, 2, m.theme).targets) != 0 {
		t.Fatalf("expected no note targets on terminal too short to render modal")
	}
	if len(m.note.layout(4, m.height, m.theme).targets) != 0 {
		t.Fatalf("expected no note targets on terminal too narrow to render modal")
	}

	narrowLayout := m.note.layout(22, 3, m.theme)
	if len(narrowLayout.targets) != 1 || narrowLayout.targets[0].action != noteActionSave {
		t.Fatalf("expected only save target on narrow terminal with truncated cancel button, got %+v", narrowLayout.targets)
	}

	targets := m.note.layout(m.width, m.height, m.theme).targets
	var saveTarget, cancelTarget *noteTarget
	for i := range targets {
		switch targets[i].action {
		case noteActionSave:
			saveTarget = &targets[i]
		case noteActionCancel:
			cancelTarget = &targets[i]
		}
	}
	if saveTarget == nil || cancelTarget == nil {
		t.Fatalf("expected both save and cancel targets, got %+v", targets)
	}

	up, cmd := m.Update(tea.MouseClickMsg{
		X:      10,
		Y:      4,
		Button: tea.MouseLeft,
	})
	if up.(model).mode != modeNote || cmd != nil {
		t.Fatalf("click outside changed note modal state")
	}

	up, cmd = m.Update(tea.MouseClickMsg{
		X:      saveTarget.start,
		Y:      saveTarget.y,
		Button: tea.MouseLeft,
	})
	mEmpty := up.(model)
	if mEmpty.mode != modeNote || mEmpty.note.errText != "note text cannot be empty" {
		t.Fatalf("expected note modal open with validation error on empty save click, got errText=%q mode=%v", mEmpty.note.errText, mEmpty.mode)
	}

	m.note.input.SetValue("my test note")

	up, cmd = m.Update(tea.MouseClickMsg{
		X:      cancelTarget.start,
		Y:      cancelTarget.y,
		Button: tea.MouseLeft,
	})
	mCancel := up.(model)
	if mCancel.mode != modeTable {
		t.Fatalf("click cancel mode = %v, want modeTable", mCancel.mode)
	}
	if cmd != nil {
		t.Fatal("expected nil cmd when cancelling note modal with Cancel button")
	}

	up, _ = m.Update(tea.KeyPressMsg{Text: "a"})
	m = up.(model)
	if m.mode != modeNote {
		t.Fatalf("expected modeNote on reopen, got %v", m.mode)
	}
	m.note.input.SetValue("my test note")

	up, cmd = m.Update(tea.MouseClickMsg{
		X:      saveTarget.start,
		Y:      saveTarget.y,
		Button: tea.MouseLeft,
	})
	mSave := up.(model)
	if mSave.mode != modeNote {
		t.Fatalf("expected modeNote while submission in flight, got %v", mSave.mode)
	}
	if cmd == nil {
		t.Fatal("expected action cmd when clicking Save button")
	}
	if mSave.formSeq != 1 {
		t.Fatalf("expected formSeq == 1, got %d", mSave.formSeq)
	}
}
