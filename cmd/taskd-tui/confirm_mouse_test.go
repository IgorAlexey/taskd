package main

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestClickConfirmModalButtons(t *testing.T) {
	m := newModel(config{icons: false, refresh: time.Hour}, newClient("http://localhost:8080"))
	m.width = 80
	m.height = 24
	m.tasks = []task{
		{ID: "task-0", Project: "test", Status: "pending", Body: "zero [y] task with [n] cancel token"},
	}
	m.rebuildShown()
	m.syncDetail()

	m, _ = m.actionDelete()
	if m.mode != modeConfirm {
		t.Fatalf("expected modeConfirm, got %v", m.mode)
	}

	mShort := m
	mShort.height = 2
	if len(mShort.confirmTargets()) != 0 {
		t.Fatalf("expected no confirm targets on terminal too short to render modal")
	}
	mNarrow := m
	mNarrow.width = 4
	if len(mNarrow.confirmTargets()) != 0 {
		t.Fatalf("expected no confirm targets on terminal too narrow to render modal")
	}

	targets := m.confirmTargets()
	var yTarget, nTarget *confirmTarget
	for i := range targets {
		switch targets[i].action {
		case confirmActionYes:
			yTarget = &targets[i]
		case confirmActionNo:
			nTarget = &targets[i]
		}
	}
	if yTarget == nil || nTarget == nil {
		t.Fatalf("expected both confirm and cancel targets, got %+v", targets)
	}

	up, cmd := m.Update(tea.MouseClickMsg{
		X:      10,
		Y:      4,
		Button: tea.MouseLeft,
	})
	if up.(model).mode != modeConfirm || cmd != nil {
		t.Fatalf("click outside changed modal state")
	}

	up, cmd = m.Update(tea.MouseClickMsg{
		X:      (yTarget.end + nTarget.start) / 2,
		Y:      yTarget.y,
		Button: tea.MouseLeft,
	})
	if up.(model).mode != modeConfirm || cmd != nil {
		t.Fatalf("click between buttons changed modal state")
	}

	up, cmd = m.Update(tea.MouseClickMsg{
		X:      nTarget.start,
		Y:      nTarget.y,
		Button: tea.MouseLeft,
	})
	mCancel := up.(model)
	if mCancel.mode != modeTable {
		t.Fatalf("click [n] mode = %v, want modeTable", mCancel.mode)
	}
	if cmd != nil {
		t.Fatal("expected nil cmd when cancelling modal with [n]")
	}

	m, _ = m.actionDelete()
	if m.mode != modeConfirm {
		t.Fatalf("expected modeConfirm on reopen, got %v", m.mode)
	}

	up, cmd = m.Update(tea.MouseClickMsg{
		X:      yTarget.start,
		Y:      yTarget.y,
		Button: tea.MouseLeft,
	})
	mConfirm := up.(model)
	if mConfirm.mode != modeTable {
		t.Fatalf("click [y] mode = %v, want modeTable", mConfirm.mode)
	}
	if cmd == nil {
		t.Fatal("expected action cmd when confirming modal with [y]")
	}
}
