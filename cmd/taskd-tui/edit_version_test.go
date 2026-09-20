package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestTUIEditPassesIfVersion(t *testing.T) {
	t.Run("FormSubmitEditIncludesIfVersion", func(t *testing.T) {
		taskItem := task{
			ID:       "task-test-1",
			Project:  "project-a",
			Body:     "initial body",
			Priority: 2,
			Version:  5,
		}
		f, _ := newEditForm(taskItem)
		f.body.SetValue("updated body")
		method, path, body, _, errText := f.submit()
		if errText != "" {
			t.Fatalf("unexpected errText: %s", errText)
		}
		if method != "PATCH" {
			t.Fatalf("expected PATCH, got %s", method)
		}
		if path != "/tasks/task-test-1" {
			t.Fatalf("expected path /tasks/task-test-1, got %s", path)
		}
		if body["if_version"] != 5 {
			t.Fatalf("expected if_version to be 5, got %v", body["if_version"])
		}
	})

	t.Run("FormSubmitEditUnchangedEmpty", func(t *testing.T) {
		taskItem := task{
			ID:       "task-test-2",
			Project:  "project-a",
			Body:     "initial body",
			Priority: 2,
			Version:  5,
		}
		f, _ := newEditForm(taskItem)
		_, _, body, _, errText := f.submit()
		if errText != "" {
			t.Fatalf("unexpected errText: %s", errText)
		}
		if len(body) != 0 {
			t.Fatalf("expected empty body for unchanged edit, got %v", body)
		}
	})

	t.Run("ActionPriAdjustIncludesIfVersion", func(t *testing.T) {
		var gotPatch map[string]any
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPatch {
				_ = json.NewDecoder(r.Body).Decode(&gotPatch)
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "t-1"})
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		m := newModel(config{url: ts.URL, worker: "w1"}, newClient(ts.URL))
		m.tasks = []task{{
			ID:       "t-1",
			Status:   "pending",
			Priority: 3,
			Version:  7,
		}}
		m.rebuildShown()
		m.cursor = 0

		up, cmd := m.Update(tea.KeyPressMsg{Text: "+"})
		m = up.(model)
		if cmd == nil {
			t.Fatal("expected actCmd for +")
		}
		cmd()
		if gotPatch == nil {
			t.Fatal("expected PATCH request payload")
		}
		if gotPatch["if_version"] != float64(7) {
			t.Fatalf("expected if_version 7 in PATCH, got %v", gotPatch["if_version"])
		}
		if gotPatch["priority"] != float64(2) {
			t.Fatalf("expected priority 2, got %v", gotPatch["priority"])
		}

		gotPatch = nil
		up, cmd = m.Update(tea.KeyPressMsg{Text: "-"})
		m = up.(model)
		if cmd == nil {
			t.Fatal("expected actCmd for -")
		}
		cmd()
		if gotPatch == nil {
			t.Fatal("expected PATCH request payload")
		}
		if gotPatch["if_version"] != float64(7) {
			t.Fatalf("expected if_version 7 in PATCH, got %v", gotPatch["if_version"])
		}
		if gotPatch["priority"] != float64(4) {
			t.Fatalf("expected priority 4, got %v", gotPatch["priority"])
		}
	})
}
