package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func releaseCallsSnapshot() []string {
	releaseMu.Lock()
	defer releaseMu.Unlock()
	return slices.Clone(releaseCalls)
}

func pressBodyKey(h *testHarness, ev *tcell.EventKey) *tcell.EventKey {
	var ret *tcell.EventKey
	h.query(func() {
		ret = h.u.bodyKeys(ev)
	})
	return ret
}

func pressBody(h *testHarness, r rune) *tcell.EventKey {
	return pressBodyKey(h, tcell.NewEventKey(tcell.KeyRune, r, 0))
}

func TestBodyPaneActionGuards(t *testing.T) {
	h := newTestHarness(t)

	tests := []struct {
		name      string
		taskID    string
		key       rune
		wantMsg   string
		checkCall func(t *testing.T)
	}{
		{
			name:    "claim_on_leased",
			taskID:  "bbbbbbb2",
			key:     'c',
			wantMsg: "not pending",
			checkCall: func(t *testing.T) {
				if calls := claimCallsSnapshot(); len(calls) != 0 {
					t.Fatalf("expected no claim calls, got %v", calls)
				}
			},
		},
		{
			name:    "release_on_pending",
			taskID:  "aaaaaaa1",
			key:     'u',
			wantMsg: "not leased",
			checkCall: func(t *testing.T) {
				if calls := releaseCallsSnapshot(); len(calls) != 0 {
					t.Fatalf("expected no release calls, got %v", calls)
				}
			},
		},
		{
			name:    "touch_on_pending",
			taskID:  "aaaaaaa1",
			key:     't',
			wantMsg: "not leased",
			checkCall: func(t *testing.T) {
				if calls := touchCallsSnapshot(); len(calls) != 0 {
					t.Fatalf("expected no touch calls, got %v", calls)
				}
			},
		},
		{
			name:    "edit_on_done",
			taskID:  "ccccccc3",
			key:     'e',
			wantMsg: "cannot edit done task",
			checkCall: func(t *testing.T) {
				var form *tview.Form
				h.query(func() { form = h.u.form })
				if form != nil {
					t.Fatalf("expected no edit form on done task, got %v", form)
				}
			},
		},
		{
			name:    "edit_on_leased",
			taskID:  "bbbbbbb2",
			key:     'e',
			wantMsg: "cannot edit actively leased task",
			checkCall: func(t *testing.T) {
				var form *tview.Form
				h.query(func() { form = h.u.form })
				if form != nil {
					t.Fatalf("expected no edit form on leased task, got %v", form)
				}
			},
		},
		{
			name:    "complete_on_done",
			taskID:  "ccccccc3",
			key:     'x',
			wantMsg: "task is already done",
			checkCall: func(t *testing.T) {
				var modal *tview.Modal
				h.query(func() { modal = h.u.modal })
				if modal != nil {
					t.Fatalf("expected no modal on done task, got %v", modal)
				}
			},
		},
		{
			name:    "complete_on_leased",
			taskID:  "bbbbbbb2",
			key:     'x',
			wantMsg: "cannot complete actively leased task",
			checkCall: func(t *testing.T) {
				var modal *tview.Modal
				h.query(func() { modal = h.u.modal })
				if modal != nil {
					t.Fatalf("expected no modal on leased task, got %v", modal)
				}
			},
		},
		{
			name:    "delete_on_leased",
			taskID:  "bbbbbbb2",
			key:     'D',
			wantMsg: "cannot delete actively leased task",
			checkCall: func(t *testing.T) {
				if calls := deleteCallsSnapshot(); len(calls) != 0 {
					t.Fatalf("expected no delete calls, got %v", calls)
				}
				var modal *tview.Modal
				h.query(func() { modal = h.u.modal })
				if modal != nil {
					t.Fatalf("expected no modal on leased delete, got %v", modal)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h.selectID(tc.taskID)
			h.query(func() { h.u.msg = "" })
			ret := pressBody(h, tc.key)
			if ret != nil {
				t.Fatalf("expected nil return for %q, got %v", tc.key, ret)
			}
			if msg := h.message(); !strings.Contains(msg, tc.wantMsg) {
				t.Fatalf("msg = %q, want containing %q", msg, tc.wantMsg)
			}
			if tc.checkCall != nil {
				tc.checkCall(t)
			}
		})
	}
}

func TestBodyPaneActionHappyPaths(t *testing.T) {
	t.Run("claim", func(t *testing.T) {
		h := newTestHarness(t)
		h.selectID("aaaaaaa1")
		ret := pressBody(h, 'c')
		if ret != nil {
			t.Fatalf("expected nil return for 'c', got %v", ret)
		}
		eventually(t, func() bool {
			return len(claimCallsSnapshot()) == 1
		})
		if got := claimCallsSnapshot()[0]; !strings.HasPrefix(got, "aaaaaaa1 ") {
			t.Fatalf("claim call = %q, want for aaaaaaa1", got)
		}
		eventually(t, func() bool {
			var st string
			h.query(func() {
				for _, tk := range h.u.all {
					if tk.ID == "aaaaaaa1" {
						st = tk.Status
					}
				}
			})
			return st == "leased"
		})
	})

	t.Run("release", func(t *testing.T) {
		h := newTestHarness(t)
		h.selectID("bbbbbbb2")
		ret := pressBody(h, 'u')
		if ret != nil {
			t.Fatalf("expected nil return for 'u', got %v", ret)
		}
		eventually(t, func() bool {
			return len(releaseCallsSnapshot()) == 1
		})
		if got := releaseCallsSnapshot()[0]; got != "bbbbbbb2 w1" {
			t.Fatalf("release call = %q, want 'bbbbbbb2 w1'", got)
		}
	})

	t.Run("priority", func(t *testing.T) {
		h := newTestHarness(t)
		h.selectID("aaaaaaa1")
		ret := pressBody(h, '-')
		if ret != nil {
			t.Fatalf("expected nil return for '-', got %v", ret)
		}
		eventually(t, func() bool {
			var pri int
			h.query(func() {
				if sel, ok := h.u.selected(); ok && sel.ID == "aaaaaaa1" {
					pri = sel.Priority
				}
			})
			return pri == 3
		})
		eventually(t, func() bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			for _, tk := range *h.tasks {
				if tk.ID == "aaaaaaa1" {
					return tk.Priority == 3
				}
			}
			return false
		})
		ret = pressBody(h, '+')
		if ret != nil {
			t.Fatalf("expected nil return for '+', got %v", ret)
		}
		eventually(t, func() bool {
			var pri int
			h.query(func() {
				if sel, ok := h.u.selected(); ok && sel.ID == "aaaaaaa1" {
					pri = sel.Priority
				}
			})
			return pri == 2
		})
		eventually(t, func() bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			for _, tk := range *h.tasks {
				if tk.ID == "aaaaaaa1" {
					return tk.Priority == 2
				}
			}
			return false
		})
	})

	t.Run("project_cycle", func(t *testing.T) {
		h := newTestHarness(t)
		var initialProj string
		h.query(func() {
			h.u.projects = []string{"proj-a", "proj-b"}
			initialProj = h.u.project
		})
		if initialProj != "" {
			t.Fatalf("initial project = %q, want empty", initialProj)
		}
		ret := pressBody(h, 'p')
		if ret != nil {
			t.Fatalf("expected nil return for 'p', got %v", ret)
		}
		var proj1 string
		h.query(func() { proj1 = h.u.project })
		if proj1 != "proj-a" {
			t.Fatalf("project after 1st 'p' = %q, want 'proj-a'", proj1)
		}
		ret = pressBody(h, 'p')
		if ret != nil {
			t.Fatalf("expected nil return for 2nd 'p', got %v", ret)
		}
		var proj2 string
		h.query(func() { proj2 = h.u.project })
		if proj2 != "proj-b" {
			t.Fatalf("project after 2nd 'p' = %q, want 'proj-b'", proj2)
		}
		ret = pressBody(h, 'p')
		if ret != nil {
			t.Fatalf("expected nil return for 3rd 'p', got %v", ret)
		}
		var proj3 string
		h.query(func() { proj3 = h.u.project })
		if proj3 != "" {
			t.Fatalf("project after 3rd 'p' = %q, want empty", proj3)
		}
	})

	t.Run("refresh", func(t *testing.T) {
		h := newTestHarness(t)
		var countBefore int
		h.query(func() { countBefore = len(h.u.all) })
		if countBefore != 3 {
			t.Fatalf("expected 3 tasks before refresh, got %d", countBefore)
		}
		h.mu.Lock()
		*h.tasks = append(*h.tasks, task{
			ID:       "ddddddd4",
			Project:  "proj-a",
			Status:   "pending",
			Priority: 1,
			Body:     "fourth task",
		})
		h.mu.Unlock()

		ret := pressBody(h, 'r')
		if ret != nil {
			t.Fatalf("expected nil return for 'r', got %v", ret)
		}
		eventually(t, func() bool {
			var count int
			h.query(func() { count = len(h.u.all) })
			return count == 4
		})

		h.mu.Lock()
		*h.tasks = append(*h.tasks, task{
			ID:       "eeeeeee5",
			Project:  "proj-b",
			Status:   "pending",
			Priority: 2,
			Body:     "fifth task",
		})
		h.mu.Unlock()

		ret = pressBody(h, 'R')
		if ret != nil {
			t.Fatalf("expected nil return for 'R', got %v", ret)
		}
		eventually(t, func() bool {
			var count int
			h.query(func() { count = len(h.u.all) })
			return count == 5
		})
	})
}

func TestBodyPaneActionModals(t *testing.T) {
	t.Run("delete_modal", func(t *testing.T) {
		h := newTestHarness(t)
		h.selectID("aaaaaaa1")
		ret := pressBody(h, 'D')
		if ret != nil {
			t.Fatalf("expected nil return for 'D', got %v", ret)
		}
		eventually(t, func() bool {
			var modal *tview.Modal
			h.query(func() { modal = h.u.modal })
			return modal != nil
		})
		h.u.app.QueueUpdateDraw(func() {
			h.u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), nil)
		})
		eventually(t, func() bool {
			var modal *tview.Modal
			h.query(func() { modal = h.u.modal })
			return modal == nil
		})
		if calls := deleteCallsSnapshot(); len(calls) != 0 {
			t.Fatalf("expected no delete calls, got %v", calls)
		}
	})

	t.Run("complete_modal", func(t *testing.T) {
		h := newTestHarness(t)
		h.selectID("aaaaaaa1")
		ret := pressBody(h, 'x')
		if ret != nil {
			t.Fatalf("expected nil return for 'x', got %v", ret)
		}
		eventually(t, func() bool {
			var modal *tview.Modal
			h.query(func() { modal = h.u.modal })
			return modal != nil
		})
		h.u.app.QueueUpdateDraw(func() {
			h.u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), nil)
		})
		eventually(t, func() bool {
			var modal *tview.Modal
			h.query(func() { modal = h.u.modal })
			return modal == nil
		})
		if calls := closeCallsSnapshot(); len(calls) != 0 {
			t.Fatalf("expected no close calls, got %v", calls)
		}
	})

	t.Run("edit_form", func(t *testing.T) {
		h := newTestHarness(t)
		h.selectID("aaaaaaa1")
		ret := pressBody(h, 'e')
		if ret != nil {
			t.Fatalf("expected nil return for 'e', got %v", ret)
		}
		eventually(t, func() bool {
			var form *tview.Form
			h.query(func() { form = h.u.form })
			return form != nil
		})
		h.u.app.QueueUpdateDraw(func() {
			h.u.form.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), nil)
		})
		eventually(t, func() bool {
			var form *tview.Form
			h.query(func() { form = h.u.form })
			return form == nil
		})
	})

	t.Run("create_form", func(t *testing.T) {
		h := newTestHarness(t)
		ret := pressBody(h, 'n')
		if ret != nil {
			t.Fatalf("expected nil return for 'n', got %v", ret)
		}
		eventually(t, func() bool {
			var form *tview.Form
			h.query(func() { form = h.u.form })
			return form != nil
		})
		h.u.app.QueueUpdateDraw(func() {
			h.u.form.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), nil)
		})
		eventually(t, func() bool {
			var form *tview.Form
			h.query(func() { form = h.u.form })
			return form == nil
		})
	})
}

func TestBodyPaneActionNoSelection(t *testing.T) {
	h := newTestHarness(t)
	h.query(func() {
		h.u.shown = nil
		h.u.table.Clear()
	})

	taskActionRunes := []rune{'+', '=', '-', 'c', 'u', 't', 'D', 'x', 'e'}
	for _, r := range taskActionRunes {
		ret := pressBody(h, r)
		if ret != nil {
			t.Fatalf("expected bodyKeys(%q) with no selection to return nil, got %v", r, ret)
		}
	}
	h.query(func() {
		if h.u.modal != nil {
			t.Fatal("expected no modal opened when no selection")
		}
		if h.u.form != nil {
			t.Fatal("expected no form opened when no selection")
		}
		if h.u.msg != "" {
			t.Fatalf("expected empty message when no selection, got %q", h.u.msg)
		}
	})
	if calls := claimCallsSnapshot(); len(calls) != 0 {
		t.Fatalf("expected no claim calls, got %v", calls)
	}
	if calls := releaseCallsSnapshot(); len(calls) != 0 {
		t.Fatalf("expected no release calls, got %v", calls)
	}
	if calls := touchCallsSnapshot(); len(calls) != 0 {
		t.Fatalf("expected no touch calls, got %v", calls)
	}
	if calls := deleteCallsSnapshot(); len(calls) != 0 {
		t.Fatalf("expected no delete calls, got %v", calls)
	}
	if calls := closeCallsSnapshot(); len(calls) != 0 {
		t.Fatalf("expected no close calls, got %v", calls)
	}
}

func TestBodyPaneActionFallthrough(t *testing.T) {
	h := newTestHarness(t)
	ev := tcell.NewEventKey(tcell.KeyRune, 'j', 0)
	ret := pressBodyKey(h, ev)
	if ret != ev {
		t.Fatalf("expected bodyKeys to pass through 'j' event unchanged, got %v", ret)
	}
}

func TestBodyPaneActionFocusRestore(t *testing.T) {
	h := newTestHarness(t)
	h.selectID("aaaaaaa1")
	h.u.app.QueueUpdateDraw(func() {
		h.u.keys(tcell.NewEventKey(tcell.KeyTab, 0, 0))
	})
	eventually(t, func() bool {
		var focused bool
		h.query(func() { focused = h.u.body.HasFocus() })
		return focused
	})
	if ret := pressBody(h, 'e'); ret != nil {
		t.Fatalf("expected nil return for 'e' in split view, got %v", ret)
	}
	eventually(t, func() bool {
		var form *tview.Form
		h.query(func() { form = h.u.form })
		return form != nil
	})
	h.u.app.QueueUpdateDraw(func() {
		h.u.form.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), nil)
	})
	eventually(t, func() bool {
		var form *tview.Form
		var focused bool
		h.query(func() { form, focused = h.u.form, h.u.body.HasFocus() })
		return form == nil && focused
	})

	h.u.app.QueueUpdateDraw(func() {
		h.u.keys(tcell.NewEventKey(tcell.KeyRune, 'z', 0))
	})
	eventually(t, func() bool {
		var zoomed, focused bool
		h.query(func() { zoomed, focused = h.u.zoomed, h.u.body.HasFocus() })
		return zoomed && focused
	})

	if ret := pressBody(h, 'e'); ret != nil {
		t.Fatalf("expected nil return for 'e', got %v", ret)
	}
	eventually(t, func() bool {
		var form *tview.Form
		h.query(func() { form = h.u.form })
		return form != nil
	})
	h.u.app.QueueUpdateDraw(func() {
		h.u.form.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), nil)
	})
	eventually(t, func() bool {
		var form *tview.Form
		var focused bool
		h.query(func() { form, focused = h.u.form, h.u.body.HasFocus() })
		return form == nil && focused
	})

	if ret := pressBody(h, 'D'); ret != nil {
		t.Fatalf("expected nil return for 'D', got %v", ret)
	}
	eventually(t, func() bool {
		var modal *tview.Modal
		h.query(func() { modal = h.u.modal })
		return modal != nil
	})
	h.u.app.QueueUpdateDraw(func() {
		h.u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
	eventually(t, func() bool {
		var modal *tview.Modal
		var focused bool
		h.query(func() { modal, focused = h.u.modal, h.u.body.HasFocus() })
		return modal == nil && focused
	})
	if calls := deleteCallsSnapshot(); len(calls) != 0 {
		t.Fatalf("cancelled delete must not call the daemon, got %v", calls)
	}
}
