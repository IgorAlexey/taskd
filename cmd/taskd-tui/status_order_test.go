package main

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestShownTaskOrderLeasedPendingDone(t *testing.T) {
	m := newModel(config{refresh: time.Second, worker: "testworker", icons: false}, nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(model)

	now := time.Now()
	initialTasks := []task{
		{ID: "task-done-1", Project: "p", Status: "done", Body: "d1"},
		{ID: "task-done-2", Project: "p", Status: "done", Body: "d2"},
		{ID: "task-pending-1", Project: "p", Status: "pending", Body: "p1"},
		{ID: "task-leased-1", Project: "p", Status: "leased", LeaseExpires: now.Unix() + 3600, Body: "l1"},
		{ID: "task-buried-1", Project: "p", Status: "buried", Body: "b1"},
		{ID: "task-done-3", Project: "p", Status: "done", Body: "d3"},
		{ID: "task-leased-2", Project: "p", Status: "leased", LeaseExpires: now.Unix() + 3600, Body: "l2"},
		{ID: "task-pending-2", Project: "p", Status: "pending", Body: "p2"},
	}

	updated, _ = m.Update(pollMsg{
		tasks:   initialTasks,
		changed: true,
	})
	m = updated.(model)

	expectedIDs := []string{
		"task-leased-1",
		"task-leased-2",
		"task-pending-1",
		"task-pending-2",
		"task-buried-1",
		"task-done-1",
		"task-done-2",
		"task-done-3",
	}

	if len(m.shown) != len(expectedIDs) {
		t.Fatalf("expected %d shown tasks, got %d", len(expectedIDs), len(m.shown))
	}

	for i, expID := range expectedIDs {
		actualID := m.tasks[m.shown[i]].ID
		if actualID != expID {
			t.Fatalf("at index %d: expected %s, got %s", i, expID, actualID)
		}
	}
}

func TestStatusFilterOrderingAndRebuild(t *testing.T) {
	m := newModel(config{refresh: time.Second, worker: "testworker", icons: false}, nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(model)

	now := time.Now()
	tasks := []task{
		{ID: "done-1", Project: "p", Status: "done", Body: "d1"},
		{ID: "pending-1", Project: "p", Status: "pending", Body: "p1"},
		{ID: "leased-1", Project: "p", Status: "leased", LeaseExpires: now.Unix() + 3600, Body: "l1"},
	}

	updated, _ = m.Update(pollMsg{
		tasks:   tasks,
		changed: true,
	})
	m = updated.(model)

	updated, _ = m.Update(tea.KeyPressMsg{Text: "1"})
	mPending := updated.(model)
	if len(mPending.shown) != 1 || mPending.tasks[mPending.shown[0]].ID != "pending-1" {
		t.Fatalf("expected 1 pending task, got %v", mPending.shown)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Text: "2"})
	mLeased := updated.(model)
	if len(mLeased.shown) != 1 || mLeased.tasks[mLeased.shown[0]].ID != "leased-1" {
		t.Fatalf("expected 1 leased task, got %v", mLeased.shown)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Text: "3"})
	mDone := updated.(model)
	if len(mDone.shown) != 1 || mDone.tasks[mDone.shown[0]].ID != "done-1" {
		t.Fatalf("expected 1 done task, got %v", mDone.shown)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Text: "0"})
	mAll := updated.(model)
	if len(mAll.shown) != 3 {
		t.Fatalf("expected 3 tasks shown, got %d", len(mAll.shown))
	}
	if mAll.tasks[mAll.shown[0]].ID != "leased-1" ||
		mAll.tasks[mAll.shown[1]].ID != "pending-1" ||
		mAll.tasks[mAll.shown[2]].ID != "done-1" {
		t.Fatalf("unexpected order under all filter: %v", mAll.shown)
	}
}

func TestExpiredLeaseNormalizedInShownOrder(t *testing.T) {
	m := newModel(config{refresh: time.Second, worker: "testworker", icons: false}, nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(model)

	now := time.Now()
	m.now = now

	tasks := []task{
		{ID: "active-leased", Project: "p", Status: "leased", LeaseExpires: now.Unix() + 3600, Body: "active"},
		{ID: "expired-leased", Project: "p", Status: "leased", LeaseExpires: now.Unix() - 100, Body: "expired"},
		{ID: "normal-pending", Project: "p", Status: "pending", Body: "pending"},
		{ID: "normal-done", Project: "p", Status: "done", Body: "done"},
	}

	updated, _ = m.Update(pollMsg{
		tasks:   tasks,
		changed: true,
	})
	m = updated.(model)

	expectedIDs := []string{
		"active-leased",
		"expired-leased",
		"normal-pending",
		"normal-done",
	}

	if len(m.shown) != len(expectedIDs) {
		t.Fatalf("expected %d tasks, got %d", len(expectedIDs), len(m.shown))
	}

	for i, expID := range expectedIDs {
		actualID := m.tasks[m.shown[i]].ID
		if actualID != expID {
			t.Fatalf("at index %d: expected %s, got %s", i, expID, actualID)
		}
	}

	updated, _ = m.Update(tea.KeyPressMsg{Text: "2"})
	mLeased := updated.(model)
	if len(mLeased.shown) != 1 || mLeased.tasks[mLeased.shown[0]].ID != "active-leased" {
		t.Fatalf("expected only active-leased task in leased view, got %d tasks", len(mLeased.shown))
	}

	updated, _ = m.Update(tea.KeyPressMsg{Text: "1"})
	mPending := updated.(model)
	if len(mPending.shown) != 2 {
		t.Fatalf("expected 2 tasks in pending view (expired-leased + normal-pending), got %d", len(mPending.shown))
	}
}

func TestFilterChangeVanishedTaskSelection(t *testing.T) {
	m := newModel(config{refresh: time.Second, worker: "testworker", icons: false}, nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(model)

	now := time.Now()
	tasks := []task{
		{ID: "leased-1", Project: "p", Status: "leased", LeaseExpires: now.Unix() + 3600, Body: "l1"},
		{ID: "pending-1", Project: "p", Status: "pending", Body: "p1"},
		{ID: "done-1", Project: "p", Status: "done", Body: "d1"},
	}

	updated, _ = m.Update(pollMsg{
		tasks:   tasks,
		changed: true,
	})
	m = updated.(model)

	updated, _ = m.Update(tea.KeyPressMsg{Text: "G"})
	m = updated.(model)

	sel, ok := m.selected()
	if !ok || sel.ID != "done-1" {
		t.Fatalf("expected done-1 selected, got %v (ok=%v)", sel.ID, ok)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Text: "1"})
	mPending := updated.(model)

	if selAfter, okAfter := mPending.selected(); okAfter {
		t.Fatalf("expected selection to clear when selected task leaves view, got %s", selAfter.ID)
	}
	if mPending.cursor != -1 {
		t.Fatalf("expected cursor to be -1, got %d", mPending.cursor)
	}
}
