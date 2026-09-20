package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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

func TestAssetOnlyTaskTitleAndDetailAndConfirm(t *testing.T) {
	taskItem := task{
		ID:        "abc1234567890",
		Project:   "render",
		AssetPath: "models/hero.blend",
		Body:      "",
		Status:    "pending",
		Priority:  2,
	}

	scope, title := titleOf(taskItem)
	if title != "models/hero.blend" {
		t.Fatalf("titleOf title = %q, want %q", title, "models/hero.blend")
	}
	if scope != "" {
		t.Fatalf("titleOf scope = %q, want empty", scope)
	}

	m := newModel(config{project: "render"}, nil)
	m.tasks = []task{taskItem}
	m.rebuildShown()
	m.cursor = 0
	m.width = 100
	m.height = 24

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "models/hero.blend") {
		t.Fatalf("expected view to contain asset path in title column and detail pane, got:\n%s", view)
	}

	m.confirmTask("Delete", "deleted", "DELETE", "/tasks/"+taskItem.ID, taskItem, nil)
	confirmView := ansi.Strip(m.confirm.View(m.width, m.height, m.theme))
	if !strings.Contains(confirmView, "models/hero.blend") {
		t.Fatalf("expected confirm view to contain asset path, got:\n%s", confirmView)
	}
	if !strings.Contains(m.confirm.text, "models/hero.blend") {
		t.Fatalf("expected confirm.text to contain asset path, got %q", m.confirm.text)
	}
}

func TestEditFormNoChanges(t *testing.T) {
	var requests int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	tOriginal := task{
		ID:        "task-edit-clean",
		Project:   "proj-clean",
		Priority:  1,
		Body:      "clean body",
		AssetPath: "clean/path",
	}

	t.Run("CtrlSWithoutChanges", func(t *testing.T) {
		m := newModel(config{project: "proj-clean", worker: "w1"}, newClient(ts.URL))
		m.tasks = []task{tOriginal}
		m.rebuildShown()
		m.cursor = 0

		up, _ := m.Update(tea.KeyPressMsg{Text: "e"})
		m = up.(model)
		if m.mode != modeForm {
			t.Fatalf("mode after pressing e = %v, want modeForm", m.mode)
		}

		before := atomic.LoadInt32(&requests)
		up, _ = m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
		m = up.(model)
		if m.mode != modeTable {
			t.Fatalf("mode after ctrl+s without changes = %v, want modeTable", m.mode)
		}
		if m.msg != "no changes" {
			t.Fatalf("msg = %q, want %q", m.msg, "no changes")
		}
		if m.formSeq != 0 {
			t.Fatalf("formSeq = %d, want 0", m.formSeq)
		}
		if after := atomic.LoadInt32(&requests); after != before {
			t.Fatalf("expected no HTTP requests, got %d", after-before)
		}
	})

	t.Run("SaveButtonWithoutChanges", func(t *testing.T) {
		m := newModel(config{project: "proj-clean", worker: "w1"}, newClient(ts.URL))
		m.tasks = []task{tOriginal}
		m.rebuildShown()
		m.cursor = 0

		up, _ := m.Update(tea.KeyPressMsg{Text: "e"})
		m = up.(model)
		if m.mode != modeForm {
			t.Fatalf("mode after pressing e = %v, want modeForm", m.mode)
		}

		m.form.setFocus(fieldSave)

		before := atomic.LoadInt32(&requests)
		up, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = up.(model)
		if m.mode != modeTable {
			t.Fatalf("mode after enter on save without changes = %v, want modeTable", m.mode)
		}
		if m.msg != "no changes" {
			t.Fatalf("msg = %q, want %q", m.msg, "no changes")
		}
		if m.formSeq != 0 {
			t.Fatalf("formSeq = %d, want 0", m.formSeq)
		}
		if after := atomic.LoadInt32(&requests); after != before {
			t.Fatalf("expected no HTTP requests, got %d", after-before)
		}
	})
}
func TestPasteRouting(t *testing.T) {
	t.Run("PasteInSearch", func(t *testing.T) {
		m := newModel(config{url: "http://localhost:8080", worker: "w1"}, newClient("http://localhost:8080"))
		up, _ := m.Update(tea.KeyPressMsg{Text: "/"})
		m = up.(model)
		if m.mode != modeSearch {
			t.Fatalf("expected modeSearch, got %v", m.mode)
		}

		up, _ = m.Update(tea.PasteMsg{Content: "sample-query\r\n"})
		m = up.(model)
		if m.query != "sample-query" {
			t.Fatalf("query = %q, want %q", m.query, "sample-query")
		}

		up, _ = m.Update(tea.PasteMsg{Content: " more\nterms"})
		m = up.(model)
		if m.query != "sample-query more terms" {
			t.Fatalf("query = %q, want %q", m.query, "sample-query more terms")
		}
	})

	t.Run("PasteInForm", func(t *testing.T) {
		m := newModel(config{url: "http://localhost:8080", worker: "w1"}, newClient("http://localhost:8080"))
		up, _ := m.Update(tea.KeyPressMsg{Text: "n"})
		m = up.(model)
		if m.mode != modeForm {
			t.Fatalf("expected modeForm, got %v", m.mode)
		}

		up, _ = m.Update(tea.PasteMsg{Content: "pasted-project"})
		m = up.(model)
		if m.form.project.Value() != "pasted-project" {
			t.Fatalf("form project = %q, want %q", m.form.project.Value(), "pasted-project")
		}

		m.form.setFocus(fieldBody)
		up, _ = m.Update(tea.PasteMsg{Content: "pasted task body"})
		m = up.(model)
		if !strings.Contains(m.form.body.Value(), "pasted task body") {
			t.Fatalf("form body = %q, want to contain %q", m.form.body.Value(), "pasted task body")
		}
	})

	t.Run("PasteInNote", func(t *testing.T) {
		m := newModel(config{url: "http://localhost:8080", worker: "w1"}, newClient("http://localhost:8080"))
		m.tasks = []task{{ID: "task-1", Status: "pending", Project: "p1"}}
		m.rebuildShown()
		m.cursor = 0

		up, _ := m.Update(tea.KeyPressMsg{Text: "a"})
		m = up.(model)
		if m.mode != modeNote {
			t.Fatalf("expected modeNote, got %v", m.mode)
		}

		up, _ = m.Update(tea.PasteMsg{Content: "pasted-note-content"})
		m = up.(model)
		if m.note.input.Value() != "pasted-note-content" {
			t.Fatalf("note input = %q, want %q", m.note.input.Value(), "pasted-note-content")
		}
	})
}
