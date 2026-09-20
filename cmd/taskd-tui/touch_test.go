package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestTouchLease(t *testing.T) {
	type touchReq struct {
		method string
		path   string
		worker string
	}
	var (
		mu       sync.Mutex
		requests []touchReq
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Worker string `json:"worker"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		requests = append(requests, touchReq{
			method: r.Method,
			path:   r.URL.Path,
			worker: body.Worker,
		})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	getRequests := func() []touchReq {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]touchReq, len(requests))
		copy(cp, requests)
		return cp
	}

	t.Run("UnleasedTask", func(t *testing.T) {
		m := newModel(config{url: ts.URL, worker: "worker-1"}, newClient(ts.URL))
		m.tasks = []task{{
			ID:      1,
			Status:  "pending",
			Project: "taskd",
		}}
		m.rebuildShown()
		m.cursor = 0

		before := len(getRequests())
		up, _ := m.Update(tea.KeyPressMsg{Text: "t"})
		m = up.(model)
		if m.msg != "task is not leased" {
			t.Fatalf("msg = %q, want %q", m.msg, "task is not leased")
		}
		if after := len(getRequests()); after != before {
			t.Fatalf("expected no HTTP requests, got %d", after-before)
		}
	})

	t.Run("ExpiredLease", func(t *testing.T) {
		m := newModel(config{url: ts.URL, worker: "worker-1"}, newClient(ts.URL))
		m.tasks = []task{{
			ID:           2,
			Status:       "leased",
			Worker:       "worker-1",
			LeaseExpires: time.Now().Add(-10 * time.Second).Unix(),
			Project:      "taskd",
		}}
		m.rebuildShown()
		m.cursor = 0

		before := len(getRequests())
		up, _ := m.Update(tea.KeyPressMsg{Text: "t"})
		m = up.(model)
		if m.msg != "lease has expired" {
			t.Fatalf("msg = %q, want %q", m.msg, "lease has expired")
		}
		if after := len(getRequests()); after != before {
			t.Fatalf("expected no HTTP requests, got %d", after-before)
		}
	})

	t.Run("AnotherWorker", func(t *testing.T) {
		m := newModel(config{url: ts.URL, worker: "worker-1"}, newClient(ts.URL))
		m.tasks = []task{{
			ID:           3,
			Status:       "leased",
			Worker:       "worker-2",
			LeaseExpires: time.Now().Add(10 * time.Minute).Unix(),
			Project:      "taskd",
		}}
		m.rebuildShown()
		m.cursor = 0

		before := len(getRequests())
		up, _ := m.Update(tea.KeyPressMsg{Text: "t"})
		m = up.(model)
		if m.msg != "cannot touch lease held by another worker" {
			t.Fatalf("msg = %q, want %q", m.msg, "cannot touch lease held by another worker")
		}
		if after := len(getRequests()); after != before {
			t.Fatalf("expected no HTTP requests, got %d", after-before)
		}
	})

	t.Run("HeldLease", func(t *testing.T) {
		taskID := int64(1)
		m := newModel(config{url: ts.URL, worker: "worker-1"}, newClient(ts.URL))
		m.tasks = []task{{
			ID:           taskID,
			Status:       "leased",
			Worker:       "worker-1",
			LeaseExpires: time.Now().Add(10 * time.Minute).Unix(),
			Project:      "taskd",
		}}
		m.rebuildShown()
		m.cursor = 0

		before := len(getRequests())
		up, cmd := m.Update(tea.KeyPressMsg{Text: "t"})
		m = up.(model)
		if cmd == nil {
			t.Fatal("expected command when touching held lease")
		}

		res := cmd()
		act, ok := res.(actMsg)
		if !ok || act.err != nil {
			t.Fatalf("touch action failed: %+v", res)
		}

		reqs := getRequests()
		if len(reqs) != before+1 {
			t.Fatalf("expected 1 HTTP request, got %d", len(reqs)-before)
		}
		req := reqs[len(reqs)-1]
		wantPath := "/tasks/" + strconv.FormatInt(taskID, 10) + "/touch"
		if req.method != http.MethodPost || req.path != wantPath {
			t.Fatalf("request = %s %s, want POST %s", req.method, req.path, wantPath)
		}
		if req.worker != "worker-1" {
			t.Fatalf("worker = %q, want %q", req.worker, "worker-1")
		}

		up, _ = m.Update(act)
		m = up.(model)
		if !strings.Contains(m.msg, "touched task "+strconv.FormatInt(taskID, 10)) {
			t.Fatalf("msg = %q, want containing %q", m.msg, "touched task "+strconv.FormatInt(taskID, 10))
		}
	})
}
