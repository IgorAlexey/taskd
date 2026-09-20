package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestTUIIfNoneMatchAndNotModified(t *testing.T) {
	var pollCount int32
	var lastIfNoneMatch string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tasks" {
			count := atomic.AddInt32(&pollCount, 1)
			inm := r.Header.Get("If-None-Match")
			lastIfNoneMatch = inm
			if count == 1 {
				w.Header().Set("ETag", `"etag-v1"`)
				w.Header().Set("X-Total-Count", "1")
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]task{
					{ID: "task-1", Body: "initial body", Status: "pending", Project: "p1"},
				})
				return
			}
			if inm == `"etag-v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/stats" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(stats{Total: 1, Pending: 1})
			return
		}
		if r.URL.Path == "/projects" || r.URL.Path == "/workers" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]"))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	cl := newClient(ts.URL)
	m := newModel(config{
		url:     ts.URL,
		worker:  "worker-test",
		refresh: time.Hour,
	}, cl)

	cmd1 := pollCmd(cl, m.listScope(), m.etag, m.seq)
	msg1 := cmd1().(pollMsg)
	if msg1.err != nil {
		t.Fatalf("poll 1 failed: %v", msg1.err)
	}
	up1, _ := m.Update(msg1)
	m = up1.(model)

	if len(m.tasks) != 1 || m.tasks[0].ID != "task-1" {
		t.Fatalf("expected 1 task in table after first poll, got: %+v", m.tasks)
	}
	if m.etag != `"etag-v1"` {
		t.Fatalf("expected model etag to be \"etag-v1\", got %q", m.etag)
	}

	cmd2 := pollCmd(cl, m.listScope(), m.etag, m.seq)
	msg2 := cmd2().(pollMsg)
	if msg2.err != nil {
		t.Fatalf("poll 2 failed: %v", msg2.err)
	}
	if msg2.changed {
		t.Fatalf("expected msg2.changed to be false on 304, got true")
	}

	if lastIfNoneMatch != `"etag-v1"` {
		t.Fatalf("expected daemon to receive If-None-Match \"etag-v1\", got %q", lastIfNoneMatch)
	}

	m.tasks[0].Body = "sentinel untouched marker"
	up2, _ := m.Update(msg2)
	m = up2.(model)

	if len(m.tasks) != 1 || m.tasks[0].Body != "sentinel untouched marker" {
		t.Fatalf("expected tasks table to remain untouched on 304, got %+v", m.tasks)
	}
}
