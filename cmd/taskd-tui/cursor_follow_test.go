package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestVanishedTaskSelectionDoesNotRebind(t *testing.T) {
	m := newModel(config{refresh: time.Second, worker: "testworker", icons: false}, nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(model)

	initialTasks := []task{
		{ID: "task-A", Project: "proj", Status: "pending", Body: "task A"},
		{ID: "task-B", Project: "proj", Status: "pending", Body: "task B"},
		{ID: "task-C", Project: "proj", Status: "pending", Body: "task C"},
		{ID: "task-D", Project: "proj", Status: "pending", Body: "task D"},
	}

	updated, _ = m.Update(pollMsg{
		tasks:   initialTasks,
		changed: true,
	})
	m = updated.(model)

	updated, _ = m.Update(tea.KeyPressMsg{Text: "j"})
	m = updated.(model)

	sel, ok := m.selected()
	if !ok || sel.ID != "task-B" {
		t.Fatalf("expected task-B to be selected, got %v (ok=%v)", sel.ID, ok)
	}

	_ = m.View()

	survivingTasks := []task{
		{ID: "task-A", Project: "proj", Status: "pending", Body: "task A"},
		{ID: "task-C", Project: "proj", Status: "pending", Body: "task C"},
		{ID: "task-D", Project: "proj", Status: "pending", Body: "task D"},
	}

	updated, _ = m.Update(pollMsg{
		tasks:   survivingTasks,
		changed: true,
	})
	m = updated.(model)

	_ = m.View()

	selAfter, okAfter := m.selected()
	if okAfter && selAfter.ID == "task-C" {
		t.Fatalf("expected selection not to silently rebind to task-C without explicit operator move")
	}
	if okAfter {
		t.Fatalf("expected selection to be cleared when selected task leaves view, got %s", selAfter.ID)
	}
	if !strings.Contains(m.msg, "left the view") {
		t.Fatalf("expected status line message indicating task left the view, got %q", m.msg)
	}
	if !strings.Contains(m.msg, "task-B") {
		t.Fatalf("expected status line message to name task-B, got %q", m.msg)
	}

	downModel, _ := m.Update(tea.KeyPressMsg{Text: "j"})
	mDown := downModel.(model)
	selDown, okDown := mDown.selected()
	if !okDown || selDown.ID != "task-C" {
		t.Fatalf("expected j to select task-C, got %v (ok=%v)", selDown.ID, okDown)
	}

	upModel, _ := m.Update(tea.KeyPressMsg{Text: "k"})
	mUp := upModel.(model)
	selUp, okUp := mUp.selected()
	if !okUp || selUp.ID != "task-A" {
		t.Fatalf("expected k to select task-A, got %v (ok=%v)", selUp.ID, okUp)
	}
}

func TestVanishedLastTaskSelectionMoveUp(t *testing.T) {
	m := newModel(config{refresh: time.Second, worker: "testworker", icons: false}, nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(model)

	initialTasks := []task{
		{ID: "task-A", Project: "proj", Status: "pending", Body: "task A"},
		{ID: "task-B", Project: "proj", Status: "pending", Body: "task B"},
		{ID: "task-C", Project: "proj", Status: "pending", Body: "task C"},
	}

	updated, _ = m.Update(pollMsg{
		tasks:   initialTasks,
		changed: true,
	})
	m = updated.(model)

	updated, _ = m.Update(tea.KeyPressMsg{Text: "G"})
	m = updated.(model)

	sel, ok := m.selected()
	if !ok || sel.ID != "task-C" {
		t.Fatalf("expected task-C to be selected, got %v", sel.ID)
	}

	survivingTasks := []task{
		{ID: "task-A", Project: "proj", Status: "pending", Body: "task A"},
		{ID: "task-B", Project: "proj", Status: "pending", Body: "task B"},
	}

	updated, _ = m.Update(pollMsg{
		tasks:   survivingTasks,
		changed: true,
	})
	m = updated.(model)

	if _, okAfter := m.selected(); okAfter {
		t.Fatalf("expected selection cleared")
	}

	upModel, _ := m.Update(tea.KeyPressMsg{Text: "k"})
	mUp := upModel.(model)
	selUp, okUp := mUp.selected()
	if !okUp || selUp.ID != "task-B" {
		t.Fatalf("expected k to select surviving task-B at end of list, got %v", selUp.ID)
	}

	downModel, _ := m.Update(tea.KeyPressMsg{Text: "j"})
	mDown := downModel.(model)
	selDown, okDown := mDown.selected()
	if !okDown || selDown.ID != "task-B" {
		t.Fatalf("expected j to clamp at task-B at end of list, got %v", selDown.ID)
	}
}

func TestVanishedFirstTaskSelectionMoveDown(t *testing.T) {
	m := newModel(config{refresh: time.Second, worker: "testworker", icons: false}, nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(model)

	initialTasks := []task{
		{ID: "task-A", Project: "proj", Status: "pending", Body: "task A"},
		{ID: "task-B", Project: "proj", Status: "pending", Body: "task B"},
		{ID: "task-C", Project: "proj", Status: "pending", Body: "task C"},
	}

	updated, _ = m.Update(pollMsg{
		tasks:   initialTasks,
		changed: true,
	})
	m = updated.(model)

	sel, ok := m.selected()
	if !ok || sel.ID != "task-A" {
		t.Fatalf("expected task-A to be selected, got %v", sel.ID)
	}

	survivingTasks := []task{
		{ID: "task-B", Project: "proj", Status: "pending", Body: "task B"},
		{ID: "task-C", Project: "proj", Status: "pending", Body: "task C"},
	}

	updated, _ = m.Update(pollMsg{
		tasks:   survivingTasks,
		changed: true,
	})
	m = updated.(model)

	if _, okAfter := m.selected(); okAfter {
		t.Fatalf("expected selection cleared")
	}

	downModel, _ := m.Update(tea.KeyPressMsg{Text: "j"})
	mDown := downModel.(model)
	selDown, okDown := mDown.selected()
	if !okDown || selDown.ID != "task-B" {
		t.Fatalf("expected j to select task-B, got %v", selDown.ID)
	}

	upModel, _ := m.Update(tea.KeyPressMsg{Text: "k"})
	mUp := upModel.(model)
	selUp, okUp := mUp.selected()
	if !okUp || selUp.ID != "task-B" {
		t.Fatalf("expected k to clamp at task-B at top of list, got %v", selUp.ID)
	}
}
