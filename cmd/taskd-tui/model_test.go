package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestEmptyWorkerClaimGuard(t *testing.T) {
	var requests int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	t.Run("ClaimWithoutWorker", func(t *testing.T) {
		m := newModel(config{url: ts.URL, worker: ""}, newClient(ts.URL))
		m.tasks = []task{{
			ID:      "task-pending",
			Status:  "pending",
			Project: "taskd",
		}}
		m.rebuildShown()
		m.cursor = 0

		before := atomic.LoadInt32(&requests)
		up, _ := m.Update(tea.KeyPressMsg{Text: "c"})
		m = up.(model)
		wantMsg := "worker required; set via -worker flag or TASKD_WORKER"
		if m.msg != wantMsg {
			t.Fatalf("msg = %q, want %q", m.msg, wantMsg)
		}
		if after := atomic.LoadInt32(&requests); after != before {
			t.Fatalf("expected no HTTP requests, got %d", after-before)
		}
	})

	t.Run("TouchWithoutWorker", func(t *testing.T) {
		m := newModel(config{url: ts.URL, worker: ""}, newClient(ts.URL))
		m.tasks = []task{{
			ID:           "task-leased",
			Status:       "leased",
			Worker:       "worker-1",
			LeaseExpires: time.Now().Add(10 * time.Minute).Unix(),
			Project:      "taskd",
		}}
		m.rebuildShown()
		m.cursor = 0

		before := atomic.LoadInt32(&requests)
		up, _ := m.Update(tea.KeyPressMsg{Text: "t"})
		m = up.(model)
		wantMsg := "worker required; set via -worker flag or TASKD_WORKER"
		if m.msg != wantMsg {
			t.Fatalf("msg = %q, want %q", m.msg, wantMsg)
		}
		if after := atomic.LoadInt32(&requests); after != before {
			t.Fatalf("expected no HTTP requests, got %d", after-before)
		}
	})
}
