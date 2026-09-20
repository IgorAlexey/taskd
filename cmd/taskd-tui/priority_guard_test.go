package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestTUIAdjustPriorityGuards(t *testing.T) {
	var (
		patchCalls int32
		gotPatch   map[string]any
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			atomic.AddInt32(&patchCalls, 1)
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotPatch = body
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "t-1", "priority": body["priority"]})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	t.Run("DoneTaskPlusMinus", func(t *testing.T) {
		for _, key := range []string{"+", "-"} {
			m := newModel(config{url: ts.URL, worker: "w1"}, newClient(ts.URL))
			m.tasks = []task{{
				ID:       "t-done",
				Status:   "done",
				Priority: 2,
			}}
			m.rebuildShown()
			m.cursor = 0

			before := atomic.LoadInt32(&patchCalls)
			up, _ := m.Update(tea.KeyPressMsg{Text: key})
			m = up.(model)
			if m.msg != "cannot adjust priority on done task" {
				t.Fatalf("key %q: msg = %q, want %q", key, m.msg, "cannot adjust priority on done task")
			}
			if after := atomic.LoadInt32(&patchCalls); after != before {
				t.Fatalf("key %q: expected no PATCH requests, got %d", key, after-before)
			}
		}
	})

	t.Run("ActivelyLeasedTaskPlusMinus", func(t *testing.T) {
		for _, key := range []string{"+", "-"} {
			m := newModel(config{url: ts.URL, worker: "w1"}, newClient(ts.URL))
			m.tasks = []task{{
				ID:           "t-leased",
				Status:       "leased",
				LeaseExpires: time.Now().Add(10 * time.Minute).Unix(),
				Priority:     2,
			}}
			m.rebuildShown()
			m.cursor = 0

			before := atomic.LoadInt32(&patchCalls)
			up, _ := m.Update(tea.KeyPressMsg{Text: key})
			m = up.(model)
			if m.msg != "cannot adjust priority on actively leased task" {
				t.Fatalf("key %q: msg = %q, want %q", key, m.msg, "cannot adjust priority on actively leased task")
			}
			if after := atomic.LoadInt32(&patchCalls); after != before {
				t.Fatalf("key %q: expected no PATCH requests, got %d", key, after-before)
			}
		}
	})

	t.Run("PriorityOneRaisesToZero", func(t *testing.T) {
		m := newModel(config{url: ts.URL, worker: "w1"}, newClient(ts.URL))
		m.tasks = []task{{
			ID:       "t-pending-1",
			Status:   "pending",
			Priority: 1,
		}}
		m.rebuildShown()
		m.cursor = 0

		before := atomic.LoadInt32(&patchCalls)
		up, cmd := m.Update(tea.KeyPressMsg{Text: "+"})
		m = up.(model)
		if cmd == nil {
			t.Fatal("expected actCmd when raising priority from 1 to 0")
		}
		cmd()
		if after := atomic.LoadInt32(&patchCalls); after != before+1 {
			t.Fatalf("expected 1 PATCH request, got %d", after-before)
		}
		if gotPatch == nil || gotPatch["priority"] != float64(0) {
			t.Fatalf("got PATCH payload %v, want priority 0", gotPatch)
		}
	})

	t.Run("PriorityZeroAlreadyHighest", func(t *testing.T) {
		m := newModel(config{url: ts.URL, worker: "w1"}, newClient(ts.URL))
		m.tasks = []task{{
			ID:       "t-pending-0",
			Status:   "pending",
			Priority: 0,
		}}
		m.rebuildShown()
		m.cursor = 0

		before := atomic.LoadInt32(&patchCalls)
		up, _ := m.Update(tea.KeyPressMsg{Text: "+"})
		m = up.(model)
		if m.msg != "already at highest priority" {
			t.Fatalf("msg = %q, want %q", m.msg, "already at highest priority")
		}
		if after := atomic.LoadInt32(&patchCalls); after != before {
			t.Fatalf("expected no PATCH requests, got %d", after-before)
		}
	})
}
