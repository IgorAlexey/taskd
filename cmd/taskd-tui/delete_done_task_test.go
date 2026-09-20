package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestDeleteDoneTaskWithForce(t *testing.T) {
	for _, initialMode := range []mode{modeTable, modeDetail} {
		t.Run(fmt.Sprintf("mode_%d", initialMode), func(t *testing.T) {
			var (
				receivedMethod string
				receivedURL    string
			)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedMethod = r.Method
				receivedURL = r.URL.RequestURI()
				if r.Method == http.MethodDelete {
					if r.URL.Query().Get("force") != "1" {
						w.WriteHeader(http.StatusConflict)
						_, _ = w.Write([]byte("task is done"))
						return
					}
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer ts.Close()

			doneTask := task{
				ID:      123,
				Project: "taskd",
				Status:  "done",
				Body:    "finished task body",
			}

			m := newModel(config{
				url:    ts.URL,
				worker: "worker-1",
			}, newClient(ts.URL))
			m.tasks = []task{doneTask}
			m.rebuildShown()
			m.mode = initialMode
			m.cursor = 0
			m.syncDetail()

			up, _ := m.Update(tea.KeyPressMsg{Text: "D"})
			m = up.(model)
			if m.mode != modeConfirm {
				t.Fatalf("mode after pressing D = %v, want modeConfirm", m.mode)
			}
			wantPath := fmt.Sprintf("/tasks/%d?force=1", doneTask.ID)
			if m.confirm.method != http.MethodDelete || m.confirm.path != wantPath {
				t.Fatalf("unexpected confirm modal for D on done task: %+v, want method %s and path %s", m.confirm, http.MethodDelete, wantPath)
			}

			up, cmd := m.Update(tea.KeyPressMsg{Text: "y"})
			m = up.(model)
			if m.mode != modeTable {
				t.Fatalf("mode after confirming = %v, want modeTable", m.mode)
			}
			if cmd == nil {
				t.Fatal("expected actCmd after confirming delete")
			}
			actResult := cmd()
			act, ok := actResult.(actMsg)
			if !ok || act.err != nil {
				t.Fatalf("delete action failed: %+v", actResult)
			}

			if receivedMethod != http.MethodDelete {
				t.Fatalf("received method = %q, want %q", receivedMethod, http.MethodDelete)
			}
			if receivedURL != wantPath {
				t.Fatalf("received URL = %q, want %q", receivedURL, wantPath)
			}
		})
	}
}
