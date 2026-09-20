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

func TestClickFormFieldsAndSave(t *testing.T) {
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
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "new-task-1", "project": "default", "status": "pending"})
	})
	mux.HandleFunc("PATCH /tasks/task-edit", func(w http.ResponseWriter, r *http.Request) {
		lastMethod = r.Method
		lastPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&lastBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "task-edit", "project": "default", "status": "pending"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	findFormRows := func(content string) (projY, priY, assetY, idY, bodyY, saveY, saveX int) {
		lines := strings.Split(content, "\n")
		projY, priY, assetY, idY, bodyY, saveY, saveX = -1, -1, -1, -1, -1, -1, -1
		for y, raw := range lines {
			stripped := ansi.Strip(raw)
			if strings.Contains(stripped, "project:") {
				projY = y
			}
			if strings.Contains(stripped, "priority:") {
				priY = y
			}
			if strings.Contains(stripped, "asset:") {
				assetY = y
			}
			if strings.Contains(stripped, "ID:") {
				idY = y
			}
			if strings.Contains(stripped, "body:") && bodyY == -1 {
				bodyY = y + 1
			}
			if strings.Contains(stripped, "[ save ]") {
				saveY = y
				saveX = strings.Index(stripped, "[ save ]")
			}
		}
		return
	}

	t.Run("CreateFormFocusAndSubmit", func(t *testing.T) {
		m := newModel(config{project: "default", refresh: time.Hour}, newClient(srv.URL))
		m.width = 80
		m.height = 24
		up, _ := m.Update(tea.KeyPressMsg{Text: "n"})
		m = up.(model)
		if m.mode != modeForm {
			t.Fatalf("expected modeForm, got %v", m.mode)
		}
		if m.form.focus != fieldBody {
			t.Fatalf("expected initial focus %d (body), got %d", fieldBody, m.form.focus)
		}

		projY, priY, assetY, idY, bodyY, saveY, saveX := findFormRows(m.View().Content)
		if projY == -1 || priY == -1 || assetY == -1 || idY == -1 || bodyY == -1 || saveY == -1 || saveX == -1 {
			t.Fatalf("missing form lines: proj=%d pri=%d asset=%d id=%d body=%d save=%d saveX=%d\nview:\n%s",
				projY, priY, assetY, idY, bodyY, saveY, saveX, m.View().Content)
		}

		up, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 20, Y: projY})
		m = up.(model)
		if m.form.focus != fieldProject {
			t.Fatalf("expected focus %d (project) after click, got %d", fieldProject, m.form.focus)
		}

		up, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 20, Y: priY})
		m = up.(model)
		if m.form.focus != fieldPriority {
			t.Fatalf("expected focus %d (priority) after click, got %d", fieldPriority, m.form.focus)
		}

		up, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 20, Y: assetY})
		m = up.(model)
		if m.form.focus != fieldAsset {
			t.Fatalf("expected focus %d (asset) after click, got %d", fieldAsset, m.form.focus)
		}

		up, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 20, Y: idY})
		m = up.(model)
		if m.form.focus != fieldID {
			t.Fatalf("expected focus %d (id) after click, got %d", fieldID, m.form.focus)
		}

		up, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 20, Y: bodyY})
		m = up.(model)
		if m.form.focus != fieldBody {
			t.Fatalf("expected focus %d (body) after click, got %d", fieldBody, m.form.focus)
		}

		up, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: saveX + 2, Y: saveY})
		m = up.(model)
		if m.form.errText == "" {
			t.Fatal("expected validation error when saving empty form")
		}
		if m.mode != modeForm {
			t.Fatalf("expected modeForm to remain on error, got %v", m.mode)
		}

		m.form.body.SetValue("valid task body")
		up, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: saveX + 2, Y: saveY})
		m = up.(model)
		if cmd == nil {
			t.Fatal("expected form submission command after clicking save")
		}
		actMsg := cmd()
		up, _ = m.Update(actMsg)
		m = up.(model)
		if m.mode != modeTable {
			t.Fatalf("expected modeTable after successful submit, got %v", m.mode)
		}
		if lastMethod != "POST" || lastPath != "/tasks" {
			t.Fatalf("unexpected request: %s %s", lastMethod, lastPath)
		}
		if body, _ := lastBody["body"].(string); body != "valid task body" {
			t.Fatalf("unexpected body in payload: %v", lastBody)
		}
	})

	t.Run("EditFormFocusAndSubmit", func(t *testing.T) {
		m := newModel(config{project: "default", refresh: time.Hour}, newClient(srv.URL))
		m.width = 80
		m.height = 24
		m.tasks = []task{
			{ID: "task-edit", Project: "default", Priority: 2, Status: "pending", Body: "existing task body"},
		}
		m.rebuildShown()
		m.syncDetail()

		up, _ := m.Update(tea.KeyPressMsg{Text: "e"})
		m = up.(model)
		if m.mode != modeForm || !m.form.editing {
			t.Fatalf("expected editing modeForm, got mode %v editing %v", m.mode, m.form.editing)
		}

		projY, priY, _, _, _, saveY, saveX := findFormRows(m.View().Content)
		if priY == -1 || saveY == -1 {
			t.Fatalf("missing form lines in edit mode: pri=%d save=%d", priY, saveY)
		}

		up, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 20, Y: projY})
		m = up.(model)
		if m.form.focus != 0 {
			t.Fatalf("expected focus 0 (project), got %d", m.form.focus)
		}

		up, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 20, Y: priY})
		m = up.(model)
		if m.form.focus != 1 {
			t.Fatalf("expected focus 1 (priority), got %d", m.form.focus)
		}

		m.form.priority.SetValue("8")
		up, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: saveX + 2, Y: saveY})
		m = up.(model)
		if cmd == nil {
			t.Fatal("expected form submission command after clicking save in edit mode")
		}
		actMsg := cmd()
		up, _ = m.Update(actMsg)
		m = up.(model)
		if m.mode != modeTable {
			t.Fatalf("expected modeTable after edit submit, got %v", m.mode)
		}
		if lastMethod != "PATCH" || lastPath != "/tasks/task-edit" {
			t.Fatalf("unexpected patch request: %s %s", lastMethod, lastPath)
		}
		if p, _ := lastBody["priority"].(float64); int(p) != 8 {
			t.Fatalf("expected priority 8 in patch payload, got %v", lastBody["priority"])
		}
	})

	t.Run("ClicksOutsideModalIgnored", func(t *testing.T) {
		m := newModel(config{project: "default", refresh: time.Hour}, newClient(srv.URL))
		m.width = 80
		m.height = 24
		up, _ := m.Update(tea.KeyPressMsg{Text: "n"})
		m = up.(model)
		initialFocus := m.form.focus

		up, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: 0})
		m = up.(model)
		if m.mode != modeForm {
			t.Fatalf("expected modeForm after click outside, got %v", m.mode)
		}
		if m.form.focus != initialFocus {
			t.Fatalf("focus changed on outside click: got %d, want %d", m.form.focus, initialFocus)
		}

		up, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseRight, X: 20, Y: 3})
		m = up.(model)
		if m.form.focus != initialFocus {
			t.Fatalf("focus changed on right click: got %d, want %d", m.form.focus, initialFocus)
		}
	})
}
