package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestCreateFormCustomID(t *testing.T) {
	var lastMethod, lastPath string
	var lastBody map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats{})
	})
	mux.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]string{"default"})
	})
	mux.HandleFunc("GET /workers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]string{})
	})
	mux.HandleFunc("POST /tasks", func(w http.ResponseWriter, r *http.Request) {
		lastMethod = r.Method
		lastPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&lastBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "custom-id-123",
			"project": "default",
			"status":  "pending",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Run("RendersIDField", func(t *testing.T) {
		m := newModel(config{project: "default", refresh: time.Hour}, newClient(srv.URL))
		m.width = 80
		m.height = 24
		up, _ := m.Update(tea.KeyPressMsg{Text: "n"})
		m = up.(model)
		if m.mode != modeForm {
			t.Fatalf("expected modeForm, got %v", m.mode)
		}

		view := ansi.Strip(m.View().Content)
		if !strings.Contains(view, "ID:") {
			t.Fatalf("expected create form to render ID input field, got view:\n%s", view)
		}
	})

	t.Run("SubmitWithCustomID", func(t *testing.T) {
		lastMethod, lastPath, lastBody = "", "", nil

		m := newModel(config{project: "default", refresh: time.Hour}, newClient(srv.URL))
		m.width = 80
		m.height = 24
		up, _ := m.Update(tea.KeyPressMsg{Text: "n"})
		m = up.(model)

		m.form.body.SetValue("task body description")
		m.form.customID.SetValue("custom-id-123")

		up, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
		m = up.(model)
		if cmd == nil {
			t.Fatal("expected submission command on ctrl-s")
		}

		actMsg := cmd()
		up, _ = m.Update(actMsg)
		m = up.(model)
		if m.mode != modeTable {
			t.Fatalf("expected modeTable after submit, got %v", m.mode)
		}

		if lastMethod != "POST" || lastPath != "/tasks" {
			t.Fatalf("expected POST /tasks, got %s %s", lastMethod, lastPath)
		}
		if gotID, _ := lastBody["id"].(string); gotID != "custom-id-123" {
			t.Fatalf("expected payload id = %q, got %q", "custom-id-123", gotID)
		}
		if gotBody, _ := lastBody["body"].(string); gotBody != "task body description" {
			t.Fatalf("expected payload body = %q, got %q", "task body description", gotBody)
		}
	})

	t.Run("SubmitWithoutCustomIDOmmitsField", func(t *testing.T) {
		lastMethod, lastPath, lastBody = "", "", nil

		m := newModel(config{project: "default", refresh: time.Hour}, newClient(srv.URL))
		m.width = 80
		m.height = 24
		up, _ := m.Update(tea.KeyPressMsg{Text: "n"})
		m = up.(model)

		m.form.body.SetValue("auto id task body")
		m.form.customID.SetValue("")

		up, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
		m = up.(model)
		if cmd == nil {
			t.Fatal("expected submission command on ctrl-s")
		}

		actMsg := cmd()
		m.Update(actMsg)

		if _, exists := lastBody["id"]; exists {
			t.Fatalf("expected id to be omitted from payload, got %v", lastBody["id"])
		}
	})

	t.Run("ValidateCustomIDCases", func(t *testing.T) {
		f, _ := newCreateForm("default")
		f.body.SetValue("test body")

		validCases := []string{
			"",
			"custom-task-01",
			"task.1_2-3",
			"ABCdef123",
			strings.Repeat("a", maxTaskIDLen),
		}
		for _, id := range validCases {
			f.customID.SetValue(id)
			if err := f.validate(); err != "" {
				t.Fatalf("validate() for %q = %q, want empty", id, err)
			}
		}

		invalidCases := []struct {
			id      string
			wantErr string
		}{
			{"bad id with spaces", "id may only contain [A-Za-z0-9._-]"},
			{"task@#$", "id may only contain [A-Za-z0-9._-]"},
			{".", "id \".\" is reserved"},
			{"..", "id \"..\" is reserved"},
			{"claim", "id \"claim\" is reserved"},
			{"CLAIM", "id \"CLAIM\" is reserved"},
			{"purge", "id \"purge\" is reserved"},
			{"PURGE", "id \"PURGE\" is reserved"},
			{"kick", "id \"kick\" is reserved"},
			{"KICK", "id \"KICK\" is reserved"},
			{strings.Repeat("x", maxTaskIDLen+1), "id must not exceed 128 characters"},
		}
		for _, tc := range invalidCases {
			f.customID.SetValue(tc.id)
			if got := f.validate(); got != tc.wantErr {
				t.Fatalf("validate() for %q = %q, want %q", tc.id, got, tc.wantErr)
			}
		}
	})

	t.Run("EnterFlowAdvancesThroughHeadersToBody", func(t *testing.T) {
		m := newModel(config{project: "default", refresh: time.Hour}, newClient(srv.URL))
		m.width = 80
		m.height = 24
		up, _ := m.Update(tea.KeyPressMsg{Text: "n"})
		m = up.(model)

		m.form.setFocus(fieldProject)
		if m.form.focus != fieldProject {
			t.Fatalf("expected focus %d, got %d", fieldProject, m.form.focus)
		}

		up, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = up.(model)
		if m.form.focus != fieldPriority {
			t.Fatalf("expected focus %d (priority), got %d", fieldPriority, m.form.focus)
		}

		up, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = up.(model)
		if m.form.focus != fieldAsset {
			t.Fatalf("expected focus %d (asset), got %d", fieldAsset, m.form.focus)
		}

		up, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = up.(model)
		if m.form.focus != fieldID {
			t.Fatalf("expected focus %d (id), got %d", fieldID, m.form.focus)
		}

		up, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = up.(model)
		if m.form.focus != fieldBody {
			t.Fatalf("expected focus %d (body), got %d", fieldBody, m.form.focus)
		}
	})
}

func TestValidateCustomIDKick(t *testing.T) {
	f, _ := newCreateForm("default")
	f.body.SetValue("test body")
	f.customID.SetValue("kick")

	method, path, body, success, errText := f.submit()
	if errText != `id "kick" is reserved` {
		t.Fatalf("submit() errText = %q, want %q", errText, `id "kick" is reserved`)
	}
	if method != "" || path != "" || body != nil || success != "" {
		t.Fatalf("submit() should not prepare request when id is reserved: got %s %s", method, path)
	}
}
