package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestReleaseLeaseWorkerGuard(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotWorker string
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		var body struct {
			Worker string `json:"worker"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotWorker = body.Worker
	}))
	defer ts.Close()

	t.Run("AnotherWorker", func(t *testing.T) {
		m := newModel(config{url: ts.URL, worker: "worker-1"}, newClient(ts.URL))
		m.tasks = []task{{ID: "t-1", Status: "leased", Worker: "worker-2"}}
		m.rebuildShown()

		up, _ := m.Update(tea.KeyPressMsg{Text: "u"})
		m = up.(model)
		if m.msg != "cannot release lease held by another worker" {
			t.Fatalf("got msg %q, want %q", m.msg, "cannot release lease held by another worker")
		}
		if gotMethod != "" {
			t.Fatal("expected no request dispatched for another worker")
		}
	})

	t.Run("HeldLease", func(t *testing.T) {
		m := newModel(config{url: ts.URL, worker: "worker-1"}, newClient(ts.URL))
		m.tasks = []task{{ID: "t-1", Status: "leased", Worker: "worker-1"}}
		m.rebuildShown()

		_, cmd := m.Update(tea.KeyPressMsg{Text: "u"})
		if cmd == nil {
			t.Fatal("expected cmd for held lease")
		}
		cmd()

		if gotMethod != http.MethodPost || gotPath != "/tasks/t-1/release" {
			t.Fatalf("got %s %s, want POST /tasks/t-1/release", gotMethod, gotPath)
		}
		if gotWorker != "worker-1" {
			t.Fatalf("worker = %q, want worker-1", gotWorker)
		}
	})
}
