package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestFormKeepOpenOnDaemonRejection(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]task{})
	})
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats{})
	})
	mux.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]string{"proj-test"})
	})
	mux.HandleFunc("GET /workers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]string{})
	})
	mux.HandleFunc("POST /tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "project name is reserved",
		})
	})
	mux.HandleFunc("PATCH /tasks/456", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "cannot edit locked task",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Run("CreateTask", func(t *testing.T) {
		m := newModel(config{project: "proj-test"}, newClient(srv.URL))
		up, _ := m.Update(tea.KeyPressMsg{Text: "n"})
		m = up.(model)
		if m.mode != modeForm {
			t.Fatalf("mode after pressing n = %v, want modeForm", m.mode)
		}

		m.form.project.SetValue("reserved-proj")
		m.form.priority.SetValue("4")
		m.form.body.SetValue("important task body\nsecond line")

		up, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
		m = up.(model)
		if m.mode != modeForm {
			t.Fatalf("mode immediately after submit = %v, want modeForm", m.mode)
		}
		if cmd == nil {
			t.Fatal("expected formActCmd after submitting create form")
		}

		up2, cmd2 := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
		m2 := up2.(model)
		if cmd2 != nil {
			t.Fatal("expected concurrent ctrl-s to be ignored while in flight")
		}
		if m2.formSeq == 0 {
			t.Fatal("expected formSeq to remain non-zero while in flight")
		}

		upIgnore, _ := m.Update(actMsg{msg: "unrelated action finished"})
		mIgnore := upIgnore.(model)
		if mIgnore.mode != modeForm {
			t.Fatal("unrelated background actMsg must not dismiss form")
		}
		if mIgnore.form.errText != "" {
			t.Fatalf("unrelated actMsg must not set errText: %q", mIgnore.form.errText)
		}

		actResult := cmd()
		act, ok := actResult.(formActMsg)
		if !ok || act.err == nil {
			t.Fatalf("expected error formActMsg from rejection, got %+v", actResult)
		}

		up, _ = m.Update(act)
		m = up.(model)

		if m.mode != modeForm {
			t.Fatalf("mode after daemon error = %v, want modeForm", m.mode)
		}
		if m.formSeq != 0 {
			t.Fatalf("expected formSeq to be cleared after error, got %d", m.formSeq)
		}
		if got := m.form.project.Value(); got != "reserved-proj" {
			t.Fatalf("project = %q, want reserved-proj", got)
		}
		if got := m.form.priority.Value(); got != "4" {
			t.Fatalf("priority = %q, want 4", got)
		}
		if got := m.form.body.Value(); got != "important task body\nsecond line" {
			t.Fatalf("body = %q, want important task body\\nsecond line", got)
		}
		if got := m.form.errText; got != "project name is reserved" {
			t.Fatalf("form.errText = %q, want %q", got, "project name is reserved")
		}
		if !strings.Contains(m.form.View(), "project name is reserved") {
			t.Fatalf("form.View() missing error text:\n%s", m.form.View())
		}
		if !strings.Contains(m.View().Content, "project name is reserved") {
			t.Fatalf("m.View() missing error text:\n%s", m.View().Content)
		}
	})

	t.Run("EditTask", func(t *testing.T) {
		tOriginal := task{
			ID:       456,
			Project:  "proj-test",
			Priority: 2,
			Body:     "orig body",
		}
		m := newModel(config{project: "proj-test"}, newClient(srv.URL))
		m.tasks = []task{tOriginal}
		m.rebuildShown()
		m.cursor = 0

		up, _ := m.Update(tea.KeyPressMsg{Text: "e"})
		m = up.(model)
		if m.mode != modeForm {
			t.Fatalf("mode after pressing e = %v, want modeForm", m.mode)
		}

		m.form.project.SetValue("proj-changed")
		m.form.priority.SetValue("7")
		m.form.body.SetValue("changed body text")

		up, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
		m = up.(model)
		if m.mode != modeForm {
			t.Fatalf("mode immediately after submit = %v, want modeForm", m.mode)
		}
		if cmd == nil {
			t.Fatal("expected formActCmd after submitting edit form")
		}

		actResult := cmd()
		act, ok := actResult.(formActMsg)
		if !ok || act.err == nil {
			t.Fatalf("expected error formActMsg from rejection, got %+v", actResult)
		}

		up, _ = m.Update(act)
		m = up.(model)

		if m.mode != modeForm {
			t.Fatalf("mode after daemon error = %v, want modeForm", m.mode)
		}
		if got := m.form.project.Value(); got != "proj-changed" {
			t.Fatalf("project = %q, want proj-changed", got)
		}
		if got := m.form.priority.Value(); got != "7" {
			t.Fatalf("priority = %q, want 7", got)
		}
		if got := m.form.body.Value(); got != "changed body text" {
			t.Fatalf("body = %q, want changed body text", got)
		}
		if got := m.form.errText; got != "cannot edit locked task" {
			t.Fatalf("form.errText = %q, want %q", got, "cannot edit locked task")
		}
		if !strings.Contains(m.form.View(), "cannot edit locked task") {
			t.Fatalf("form.View() missing error text:\n%s", m.form.View())
		}
		if !strings.Contains(m.View().Content, "cannot edit locked task") {
			t.Fatalf("m.View() missing error text:\n%s", m.View().Content)
		}
	})

	t.Run("SuccessClosesForm", func(t *testing.T) {
		m := newModel(config{project: "proj-test"}, newClient(srv.URL))
		up, _ := m.Update(tea.KeyPressMsg{Text: "n"})
		m = up.(model)
		if m.mode != modeForm {
			t.Fatalf("mode after pressing n = %v, want modeForm", m.mode)
		}

		m.formSeq = 10
		up, _ = m.Update(formActMsg{seq: 10, msg: "created task"})
		m = up.(model)
		if m.mode != modeTable {
			t.Fatalf("mode after successful creation = %v, want modeTable", m.mode)
		}
		if m.msg != "created task" {
			t.Fatalf("msg = %q, want %q", m.msg, "created task")
		}
		if m.formSeq != 0 {
			t.Fatalf("expected formSeq reset to 0, got %d", m.formSeq)
		}
	})

	t.Run("CancelledFormDropsInFlightReply", func(t *testing.T) {
		m := newModel(config{project: "proj-test"}, newClient(srv.URL))
		up, _ := m.Update(tea.KeyPressMsg{Text: "n"})
		m = up.(model)
		m.form.body.SetValue("some body text")

		up, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
		m = up.(model)
		if cmd == nil {
			t.Fatal("expected formActCmd")
		}
		res := cmd()
		act := res.(formActMsg)

		up, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = up.(model)
		if !m.form.discarding {
			t.Fatal("expected discarding prompt on escape with dirty form")
		}
		up, _ = m.Update(tea.KeyPressMsg{Text: "y"})
		m = up.(model)
		if m.mode != modeTable {
			t.Fatalf("expected modeTable after discard, got %v", m.mode)
		}

		up, _ = m.Update(act)
		m = up.(model)
		if m.mode != modeTable {
			t.Fatalf("expected modeTable after in-flight reply dropped, got %v", m.mode)
		}
		if m.form.errText != "" {
			t.Fatalf("expected no form error applied to cancelled form, got %q", m.form.errText)
		}
	})
}
