package main

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestTUICompleteLeasedTask(t *testing.T) {
	t.Setenv("TASKD_WORKER", "worker-complete")
	h := newTestHarness(t)

	h.selectID("aaaaaaa1")
	h.press('c')
	eventually(t, func() bool {
		var status, worker string
		h.query(func() {
			for _, tk := range h.u.all {
				if tk.ID == "aaaaaaa1" {
					status = tk.Status
					worker = tk.Worker
				}
			}
		})
		return status == "leased" && worker == "worker-complete"
	})

	h.press('x')
	var modal *tview.Modal
	eventually(t, func() bool {
		h.query(func() { modal = h.u.modal })
		return modal != nil
	})

	h.query(func() {
		modal.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), nil)
	})
	eventually(t, func() bool {
		h.query(func() { modal = h.u.modal })
		return modal == nil
	})
	if len(doneCallsSnapshot()) != 0 {
		t.Fatalf("expected no done calls after cancel, got %v", doneCallsSnapshot())
	}

	h.press('x')
	eventually(t, func() bool {
		h.query(func() { modal = h.u.modal })
		return modal != nil
	})

	h.query(func() {
		modal.InputHandler()(tcell.NewEventKey(tcell.KeyLeft, 0, 0), nil)
		modal.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	eventually(t, func() bool {
		h.query(func() { modal = h.u.modal })
		return modal == nil
	})

	eventually(t, func() bool {
		return len(doneCallsSnapshot()) == 1
	})
	calls := doneCallsSnapshot()
	if calls[0].uri != "/tasks/aaaaaaa1/done" || calls[0].worker != "worker-complete" {
		t.Fatalf("unexpected done call: %+v", calls[0])
	}

	eventually(t, func() bool {
		var status string
		h.query(func() {
			for _, tk := range h.u.all {
				if tk.ID == "aaaaaaa1" {
					status = tk.Status
				}
			}
		})
		return status == "done"
	})

	h.mu.Lock()
	var serverStatus string
	for _, tk := range *h.tasks {
		if tk.ID == "aaaaaaa1" {
			serverStatus = tk.Status
		}
	}
	h.mu.Unlock()
	if serverStatus != "done" {
		t.Fatalf("expected server task status to be done, got %q", serverStatus)
	}

	eventually(t, func() bool {
		return strings.Contains(h.message(), "completed task aaaaaaa")
	})
}

func TestTUICompleteExpiredLeasedTaskFallback(t *testing.T) {
	t.Setenv("TASKD_WORKER", "worker-expired")
	h := newTestHarness(t)

	h.mu.Lock()
	for i := range *h.tasks {
		if (*h.tasks)[i].ID == "bbbbbbb2" {
			(*h.tasks)[i].Worker = "worker-expired"
			(*h.tasks)[i].LeaseExpires = time.Now().Unix() - 10
		}
	}
	h.mu.Unlock()

	ts, err := h.u.fetch("", "")
	if err != nil {
		t.Fatal(err)
	}
	h.query(func() { h.u.render(ts) })

	h.selectID("bbbbbbb2")
	h.press('x')

	var modal *tview.Modal
	eventually(t, func() bool {
		h.query(func() { modal = h.u.modal })
		return modal != nil
	})

	h.query(func() {
		modal.InputHandler()(tcell.NewEventKey(tcell.KeyLeft, 0, 0), nil)
		modal.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	eventually(t, func() bool {
		h.query(func() { modal = h.u.modal })
		return modal == nil
	})

	eventually(t, func() bool {
		return len(closeCallsSnapshot()) == 1
	})
	if len(doneCallsSnapshot()) != 0 {
		t.Fatalf("expired lease must not call done endpoint, got: %v", doneCallsSnapshot())
	}
	closes := closeCallsSnapshot()
	if closes[0].uri != "/tasks/bbbbbbb2/close" || closes[0].body != 0 {
		t.Fatalf("unexpected close call for expired lease: %+v", closes[0])
	}

	eventually(t, func() bool {
		var status string
		h.query(func() {
			for _, tk := range h.u.all {
				if tk.ID == "bbbbbbb2" {
					status = tk.Status
				}
			}
		})
		return status == "done"
	})
}
