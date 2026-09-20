package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"
)

func deleteCallsSnapshot() []string {
	deleteMu.Lock()
	defer deleteMu.Unlock()
	return slices.Clone(deleteCalls)
}

func closeCallsSnapshot() []closeCall {
	closeMu.Lock()
	defer closeMu.Unlock()
	return slices.Clone(closeCalls)
}

func TestTUIDeleteLeasedTaskRejection(t *testing.T) {
	h := newTestHarness(t)

	h.selectID("bbbbbbb2")
	h.press('D')

	eventually(t, func() bool {
		return strings.Contains(h.message(), "cannot delete actively leased task")
	})

	var modal *tview.Modal
	h.query(func() {
		modal = h.u.modal
	})
	if modal != nil {
		t.Fatal("actively leased task must not open delete confirmation modal")
	}

	if calls := deleteCallsSnapshot(); len(calls) != 0 {
		t.Fatalf("expected no delete calls, got %v", calls)
	}

	h.mu.Lock()
	count := len(*h.tasks)
	h.mu.Unlock()
	if count != 3 {
		t.Fatalf("expected 3 tasks to remain, got %d", count)
	}

	h.mu.Lock()
	for i := range *h.tasks {
		if (*h.tasks)[i].ID == "bbbbbbb2" {
			(*h.tasks)[i].LeaseExpires = time.Now().Unix() - 10
		}
	}
	h.mu.Unlock()

	ts, _, err := h.u.fetch("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	h.query(func() { h.u.render(ts) })
	h.selectID("bbbbbbb2")
	h.press('D')

	eventually(t, func() bool {
		h.query(func() { modal = h.u.modal })
		return modal != nil
	})
}

func TestTUICompleteLeasedTaskRejection(t *testing.T) {
	h := newTestHarness(t)

	h.selectID("bbbbbbb2")
	h.press('x')

	eventually(t, func() bool {
		return strings.Contains(h.message(), "cannot complete actively leased task")
	})

	var modal *tview.Modal
	h.query(func() {
		modal = h.u.modal
	})
	if modal != nil {
		t.Fatal("actively leased task must not open complete confirmation modal")
	}

	if calls := closeCallsSnapshot(); len(calls) != 0 {
		t.Fatalf("expected no close calls, got %v", calls)
	}

	h.mu.Lock()
	var status string
	for _, tk := range *h.tasks {
		if tk.ID == "bbbbbbb2" {
			status = tk.Status
		}
	}
	h.mu.Unlock()
	if status != "leased" {
		t.Fatalf("expected task status to remain leased, got %q", status)
	}

	h.mu.Lock()
	for i := range *h.tasks {
		if (*h.tasks)[i].ID == "bbbbbbb2" {
			(*h.tasks)[i].LeaseExpires = time.Now().Unix() - 10
		}
	}
	h.mu.Unlock()

	ts, _, err := h.u.fetch("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	h.query(func() { h.u.render(ts) })
	h.selectID("bbbbbbb2")
	h.press('x')

	eventually(t, func() bool {
		h.query(func() { modal = h.u.modal })
		return modal != nil
	})
}
func TestEditRefusedOnLeased(t *testing.T) {
	h := newTestHarness(t)

	h.selectID("bbbbbbb2")
	h.press('e')

	eventually(t, func() bool {
		return strings.Contains(h.message(), "cannot edit actively leased task")
	})

	var (
		form    *tview.Form
		hasPage bool
	)
	h.query(func() {
		form = h.u.form
		hasPage = h.u.pages.HasPage("edit")
	})
	if form != nil || hasPage {
		t.Fatal("actively leased task must not open edit form or page")
	}

	h.selectID("ccccccc3")
	h.press('e')

	eventually(t, func() bool {
		return strings.Contains(h.message(), "cannot edit done task")
	})

	h.query(func() {
		form = h.u.form
		hasPage = h.u.pages.HasPage("edit")
	})
	if form != nil || hasPage {
		t.Fatal("done task must not open edit form or page")
	}

	h.mu.Lock()
	for i := range *h.tasks {
		if (*h.tasks)[i].ID == "bbbbbbb2" {
			(*h.tasks)[i].LeaseExpires = time.Now().Unix() - 10
		}
	}
	h.mu.Unlock()

	ts, _, err := h.u.fetch("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	h.query(func() { h.u.render(ts) })
	h.selectID("bbbbbbb2")
	h.press('e')

	eventually(t, func() bool {
		h.query(func() {
			form = h.u.form
			hasPage = h.u.pages.HasPage("edit")
		})
		return form != nil && hasPage
	})
}

func TestTUIAdjustPriorityGuards(t *testing.T) {
	h := newTestHarness(t)

	cases := []struct {
		taskID  string
		wantMsg string
	}{
		{"ccccccc3", "cannot adjust priority on done task"},
		{"bbbbbbb2", "cannot adjust priority on actively leased task"},
	}

	keys := []rune{'+', '=', '-'}
	actions := []struct {
		name string
		act  func(rune)
	}{
		{"table", h.press},
		{"body", func(k rune) { pressBody(h, k) }},
	}

	for _, c := range cases {
		for _, a := range actions {
			for _, k := range keys {
				h.selectID(c.taskID)
				h.query(func() { h.u.msg = "" })
				a.act(k)

				eventually(t, func() bool {
					return strings.Contains(h.message(), c.wantMsg)
				})
				if calls := patchCallsSnapshot(); len(calls) != 0 {
					t.Fatalf("expected no patch calls for %s on %s with key %q, got %v", a.name, c.taskID, k, calls)
				}
			}
		}
	}

	h.mu.Lock()
	for i := range *h.tasks {
		if (*h.tasks)[i].ID == "bbbbbbb2" {
			(*h.tasks)[i].LeaseExpires = time.Now().Unix() - 10
		}
	}
	h.mu.Unlock()

	ts, _, err := h.u.fetch("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	h.query(func() { h.u.render(ts) })
	h.selectID("bbbbbbb2")
	h.query(func() { h.u.msg = "" })
	h.press('-')

	eventually(t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		for _, tk := range *h.tasks {
			if tk.ID == "bbbbbbb2" {
				return tk.Priority == 2
			}
		}
		return false
	})
	if calls := patchCallsSnapshot(); len(calls) != 1 {
		t.Fatalf("expected one patch call on expired leased task, got %v", calls)
	}
	if msg := h.message(); strings.Contains(msg, "cannot adjust priority") {
		t.Fatalf("unexpected priority refusal on expired leased task: %q", msg)
	}

	h.mu.Lock()
	for i := range *h.tasks {
		if (*h.tasks)[i].ID == "aaaaaaa1" {
			(*h.tasks)[i].Priority = 0
		}
	}
	h.mu.Unlock()

	ts, _, err = h.u.fetch("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	h.query(func() { h.u.render(ts) })
	h.selectID("aaaaaaa1")
	h.query(func() { h.u.msg = "" })
	patchMu.Lock()
	patchCalls = nil
	patchMu.Unlock()
	h.press('+')

	eventually(t, func() bool {
		return strings.Contains(h.message(), "already at highest priority")
	})
	if calls := patchCallsSnapshot(); len(calls) != 0 {
		t.Fatalf("expected no patch call at highest priority, got %v", calls)
	}
}

func TestFrozenLeaseBoundary(t *testing.T) {
	now := time.Now().Unix()
	cases := []struct {
		expires int64
		want    string
	}{
		{now - 1, ""},
		{now, "actively leased task"},
		{now + 1, "actively leased task"},
	}
	for _, c := range cases {
		tk := task{Status: "leased", LeaseExpires: c.expires}
		if got := tk.frozenReason(now); got != c.want {
			t.Fatalf("frozenReason(lease_expires=now%+d) = %q, want %q", c.expires-now, got, c.want)
		}
	}
	if got := (task{Status: "done"}).frozenReason(now); got != "done task" {
		t.Fatalf("frozenReason(done) = %q, want %q", got, "done task")
	}
}
