package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestPurgeCompletedTasks(t *testing.T) {
	for _, initialMode := range []mode{modeTable, modeDetail} {
		t.Run(fmt.Sprintf("mode_%d", initialMode), func(t *testing.T) {
			var (
				receivedMethod string
				receivedURL    string
			)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedMethod = r.Method
				receivedURL = r.URL.RequestURI()
				if r.URL.Path == "/tasks/purge" && r.Method == http.MethodPost {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"deleted":3}`))
					return
				}
				if r.URL.Path == "/tasks" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`[]`))
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer ts.Close()

			m := newModel(config{
				url:    ts.URL,
				worker: "worker-1",
			}, newClient(ts.URL, ""))
			m.tasks = []task{
				{ID: 1, Project: "proj-a", Status: "done", Body: "done 1"},
				{ID: 2, Project: "proj-b", Status: "done", Body: "done 2"},
			}
			m.rebuildShown()
			m.mode = initialMode
			m.cursor = 0
			m.syncDetail()

			up, _ := m.Update(tea.KeyPressMsg{Text: "X"})
			m = up.(model)
			if m.mode != modeConfirm {
				t.Fatalf("mode after pressing X = %v, want modeConfirm", m.mode)
			}
			if m.confirm.method != http.MethodPost || m.confirm.path != "/tasks/purge" || m.confirm.button != "purge" {
				t.Fatalf("unexpected confirm modal for all projects: %+v", m.confirm)
			}

			up, cmd := m.Update(tea.KeyPressMsg{Text: "y"})
			m = up.(model)
			if m.mode != modeTable {
				t.Fatalf("mode after confirming = %v, want modeTable", m.mode)
			}
			if cmd == nil {
				t.Fatal("expected actCmd after confirming purge")
			}
			actResult := cmd()
			act, ok := actResult.(actMsg)
			if !ok || act.err != nil {
				t.Fatalf("purge action failed: %+v", actResult)
			}

			if receivedMethod != http.MethodPost {
				t.Fatalf("received method = %q, want %q", receivedMethod, http.MethodPost)
			}
			if receivedURL != "/tasks/purge" {
				t.Fatalf("received URL = %q, want %q", receivedURL, "/tasks/purge")
			}

			mNext, _ := m.Update(act)
			m = mNext.(model)
			if m.msg != act.msg {
				t.Fatalf("footer message = %q, want %q", m.msg, act.msg)
			}
		})
	}
}

func TestPurgeScopedToProjectFilter(t *testing.T) {
	var (
		receivedMethod string
		receivedURL    string
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedURL = r.URL.RequestURI()
		if r.URL.Path == "/tasks/purge" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"deleted":1}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	m := newModel(config{
		url:    ts.URL,
		worker: "worker-1",
	}, newClient(ts.URL, ""))
	m.project = "alpha"
	m.tasks = []task{
		{ID: 1, Project: "alpha", Status: "done", Body: "done in alpha"},
	}
	m.rebuildShown()
	m.cursor = 0

	up, _ := m.Update(tea.KeyPressMsg{Text: "X"})
	m = up.(model)
	if m.mode != modeConfirm {
		t.Fatalf("mode after pressing X with project filter = %v, want modeConfirm", m.mode)
	}
	wantPath := "/tasks/purge?project=alpha"
	if m.confirm.method != http.MethodPost || m.confirm.path != wantPath || m.confirm.button != "purge" {
		t.Fatalf("unexpected confirm modal with project filter: %+v", m.confirm)
	}

	up, cmd := m.Update(tea.KeyPressMsg{Text: "y"})
	m = up.(model)
	if m.mode != modeTable {
		t.Fatalf("mode after confirming = %v, want modeTable", m.mode)
	}
	if cmd == nil {
		t.Fatal("expected actCmd after confirming purge")
	}
	actResult := cmd()
	act, ok := actResult.(actMsg)
	if !ok || act.err != nil {
		t.Fatalf("purge action failed: %+v", actResult)
	}

	if receivedMethod != http.MethodPost {
		t.Fatalf("received method = %q, want %q", receivedMethod, http.MethodPost)
	}
	if receivedURL != wantPath {
		t.Fatalf("received URL = %q, want %q", receivedURL, wantPath)
	}
}

func TestPurgeCancel(t *testing.T) {
	m := newModel(config{
		worker: "worker-1",
	}, newClient("http://localhost:8080", ""))
	m.tasks = []task{
		{ID: 1, Project: "alpha", Status: "done", Body: "done in alpha"},
	}
	m.rebuildShown()

	up, _ := m.Update(tea.KeyPressMsg{Text: "X"})
	m = up.(model)
	if m.mode != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm", m.mode)
	}

	up, cmd := m.Update(tea.KeyPressMsg{Text: "n"})
	m = up.(model)
	if m.mode != modeTable {
		t.Fatalf("mode after cancel = %v, want modeTable", m.mode)
	}
	if cmd != nil {
		t.Fatal("expected nil cmd after cancelling purge")
	}
}
