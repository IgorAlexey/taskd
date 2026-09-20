package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestNotes(t *testing.T) {
	type postRecord struct {
		path   string
		author string
		text   string
	}

	var mu sync.Mutex
	var posted []postRecord

	t1List := task{
		ID:        "task-with-notes",
		Project:   "infra",
		Status:    "pending",
		Priority:  2,
		Body:      "Investigate daemon memory leak\n\nWhy: daemon memory usage grows steadily under load.",
		CreatedAt: 1700000000,
	}

	notesList := []taskNote{
		{
			ID:        1,
			CreatedAt: 1700000100,
			Author:    "alice",
			Text:      "checked pprof heap profiles, retained blocks in sqlite rows",
		},
		{
			ID:        2,
			CreatedAt: 1700000200,
			Author:    "bob",
			Text:      "confirmed, statements were missing close on error path",
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]task{t1List})
	})
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats{Pending: 1, Total: 1})
	})
	mux.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]string{"infra"})
	})
	mux.HandleFunc("GET /workers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]string{"charlie"})
	})
	mux.HandleFunc("GET /tasks/task-with-notes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		resp := struct {
			task
			Notes []taskNote `json:"notes"`
		}{
			task:  t1List,
			Notes: notesList,
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("POST /tasks/task-with-notes/notes", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Author string `json:"author"`
			Text   string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		posted = append(posted, postRecord{
			path:   r.URL.Path,
			author: req.Author,
			text:   req.Text,
		})
		newNote := taskNote{
			ID:        int64(len(notesList) + 1),
			CreatedAt: time.Now().Unix(),
			Author:    req.Author,
			Text:      req.Text,
		}
		notesList = append(notesList, newNote)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(newNote)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Run("renders_notes_under_separator", func(t *testing.T) {
		m := newModel(config{url: srv.URL, worker: "charlie", icons: false}, newClient(srv.URL))
		m.width = 100
		m.height = 30
		m.tasks = []task{t1List}
		m.rebuildShown()
		m.cursor = 0
		m.syncDetail()

		if strings.Contains(m.detail.GetContent(), "--- notes ---") {
			t.Fatal("notes separator should not appear before notes are fetched")
		}

		up, fetchCmd := m.Update(noteFetchMsg{id: "task-with-notes", seq: m.noteSeq})
		m = up.(model)
		if fetchCmd == nil {
			t.Fatal("expected noteFetchMsg to return taskNotesCmd")
		}
		notesMsg := fetchCmd()
		up, _ = m.Update(notesMsg)
		m = up.(model)

		content := m.detail.GetContent()
		stripped := ansi.Strip(content)

		if !strings.Contains(stripped, "Investigate daemon memory leak") {
			t.Fatalf("expected task body in detail pane, got:\n%s", stripped)
		}

		sepIdx := strings.Index(stripped, "--- notes ---")
		if sepIdx < 0 {
			t.Fatalf("expected '--- notes ---' separator in detail pane, got:\n%s", stripped)
		}

		bodyIdx := strings.Index(stripped, "Investigate daemon memory leak")
		if sepIdx <= bodyIdx {
			t.Fatalf("separator at %d must appear below task description at %d", sepIdx, bodyIdx)
		}

		notesPart := stripped[sepIdx:]
		if !strings.Contains(notesPart, "alice") {
			t.Fatalf("expected author 'alice' under separator, got:\n%s", notesPart)
		}
		if !strings.Contains(notesPart, "checked pprof heap profiles") {
			t.Fatalf("expected alice note text under separator, got:\n%s", notesPart)
		}
		if !strings.Contains(notesPart, "bob") {
			t.Fatalf("expected author 'bob' under separator, got:\n%s", notesPart)
		}
		if !strings.Contains(notesPart, "confirmed, statements were missing close") {
			t.Fatalf("expected bob note text under separator, got:\n%s", notesPart)
		}
	})

	t.Run("pressing_a_prompts_and_posts_note", func(t *testing.T) {
		m := newModel(config{url: srv.URL, worker: "charlie", icons: false}, newClient(srv.URL))
		m.width = 100
		m.height = 30
		m.tasks = []task{t1List}
		m.rebuildShown()
		m.cursor = 0
		m.syncDetail()

		up, _ := m.Update(tea.KeyPressMsg{Text: "a"})
		m = up.(model)

		if m.mode != modeNote {
			t.Fatalf("expected modeNote after pressing 'a', got %v", m.mode)
		}

		viewStr := ansi.Strip(m.View().Content)
		if !strings.Contains(viewStr, "Add Note") {
			t.Fatalf("expected note prompt modal to show 'Add Note', got:\n%s", viewStr)
		}

		noteText := "applied fix in PR 42, memory remains flat"
		for _, ch := range noteText {
			up, _ = m.Update(tea.KeyPressMsg{Text: string(ch)})
			m = up.(model)
		}

		if gotVal := m.note.input.Value(); gotVal != noteText {
			t.Fatalf("expected input value %q, got %q", noteText, gotVal)
		}

		up, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = up.(model)

		if m.mode != modeNote {
			t.Fatalf("expected modeNote to remain while submit request is in flight, got %v", m.mode)
		}

		if cmd == nil {
			t.Fatal("expected non-nil cmd after submitting note")
		}

		actResult := cmd()
		formAct, ok := actResult.(formActMsg)
		if !ok {
			t.Fatalf("expected formActMsg from cmd, got %T: %+v", actResult, actResult)
		}
		if formAct.err != nil {
			t.Fatalf("unexpected error from formActCmd: %v", formAct.err)
		}

		up, _ = m.Update(formAct)
		m = up.(model)

		if m.mode != modeTable {
			t.Fatalf("expected modeTable after successful formActMsg, got %v", m.mode)
		}

		mu.Lock()
		defer mu.Unlock()
		if len(posted) == 0 {
			t.Fatal("expected POST /tasks/task-with-notes/notes request, none recorded")
		}
		last := posted[len(posted)-1]
		if last.path != "/tasks/task-with-notes/notes" {
			t.Fatalf("posted to %q, want /tasks/task-with-notes/notes", last.path)
		}
		if last.author != "charlie" {
			t.Fatalf("posted author %q, want 'charlie'", last.author)
		}
		if last.text != noteText {
			t.Fatalf("posted text %q, want %q", last.text, noteText)
		}
	})

	t.Run("submit_error_keeps_modal_open", func(t *testing.T) {
		m := newModel(config{url: srv.URL, worker: "charlie", icons: false}, newClient(srv.URL))
		m.width = 100
		m.height = 30
		m.tasks = []task{t1List}
		m.rebuildShown()
		m.cursor = 0
		m.syncDetail()

		up, _ := m.Update(tea.KeyPressMsg{Text: "a"})
		m = up.(model)
		m.note.input.SetValue("failing note")

		up, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = up.(model)
		if cmd == nil {
			t.Fatal("expected cmd from enter")
		}

		up, _ = m.Update(formActMsg{seq: m.formSeq, err: errors.New("daemon connection refused")})
		m = up.(model)

		if m.mode != modeNote {
			t.Fatalf("expected modeNote on submit error, got %v", m.mode)
		}
		if !strings.Contains(m.note.errText, "daemon connection refused") {
			t.Fatalf("expected errText set on note modal, got %q", m.note.errText)
		}
		if m.note.input.Value() != "failing note" {
			t.Fatalf("expected input value preserved, got %q", m.note.input.Value())
		}
	})

	t.Run("prompt_from_detail_and_zoom_modes", func(t *testing.T) {
		for _, initialMode := range []mode{modeDetail, modeZoom} {
			m := newModel(config{url: srv.URL, worker: "charlie", icons: false}, newClient(srv.URL))
			m.width = 100
			m.height = 30
			m.tasks = []task{t1List}
			m.rebuildShown()
			m.cursor = 0
			m.mode = initialMode
			m.syncDetail()

			up, _ := m.Update(tea.KeyPressMsg{Text: "a"})
			m = up.(model)

			if m.mode != modeNote {
				t.Fatalf("mode %v: expected modeNote after pressing 'a', got %v", initialMode, m.mode)
			}
			if m.note.prev != initialMode {
				t.Fatalf("mode %v: expected prev mode %v, got %v", initialMode, initialMode, m.note.prev)
			}

			up, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			m = up.(model)

			if m.mode != initialMode {
				t.Fatalf("mode %v: expected return to %v after escape, got %v", initialMode, initialMode, m.mode)
			}
		}
	})

	t.Run("footer_displays_note_action", func(t *testing.T) {
		for _, currentMode := range []mode{modeDetail, modeZoom} {
			m := newModel(config{url: srv.URL, worker: "charlie", icons: false}, newClient(srv.URL))
			m.width = 100
			m.height = 30
			m.tasks = []task{t1List}
			m.rebuildShown()
			m.cursor = 0
			m.mode = currentMode
			m.syncDetail()

			items := m.footerItems()
			found := false
			for _, it := range items {
				if it[0] == "a" && it[1] == "note" {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("mode %v: expected footer items to contain {'a', 'note'}, got: %+v", currentMode, items)
			}
		}
	})

	t.Run("navigation_debounces_notes_fetch", func(t *testing.T) {
		t2 := task{ID: "task-2", Status: "pending", Body: "second task"}
		m := newModel(config{url: srv.URL, worker: "charlie", icons: false}, newClient(srv.URL))
		m.width = 100
		m.height = 30
		m.tasks = []task{t1List, t2}
		m.rebuildShown()
		m.cursor = 0
		m.syncDetail()

		up, cmd := m.Update(tea.KeyPressMsg{Text: "j"})
		m = up.(model)
		if m.cursor != 1 {
			t.Fatalf("cursor = %d, want 1", m.cursor)
		}
		if cmd == nil {
			t.Fatal("expected debounced fetch cmd when selection changes")
		}
	})
}

func TestNoteFormSeqResetOnError(t *testing.T) {
	m := newModel(config{worker: "charlie", icons: false}, nil)
	m.width = 100
	m.height = 30
	m.tasks = []task{{ID: "task-1", Status: "pending", Body: "sample task"}}
	m.rebuildShown()
	m.cursor = 0
	m.syncDetail()

	up, _ := m.Update(tea.KeyPressMsg{Text: "a"})
	m = up.(model)
	if m.mode != modeNote {
		t.Fatalf("expected modeNote, got %v", m.mode)
	}
	m.note.input.SetValue("initial note")

	up, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = up.(model)
	if cmd == nil {
		t.Fatal("expected cmd from enter")
	}
	if m.formSeq == 0 {
		t.Fatal("expected non-zero formSeq while submission is in-flight")
	}

	up, _ = m.Update(formActMsg{seq: m.formSeq, err: errors.New("daemon connection refused")})
	m = up.(model)

	if m.mode != modeNote {
		t.Fatalf("expected modeNote on submit error, got %v", m.mode)
	}
	if m.formSeq != 0 {
		t.Fatalf("expected formSeq == 0 after error, got %d", m.formSeq)
	}
	if !strings.Contains(m.note.errText, "daemon connection refused") {
		t.Fatalf("expected errText set, got %q", m.note.errText)
	}

	up, _ = m.Update(tea.KeyPressMsg{Text: "!"})
	m = up.(model)
	if got := m.note.input.Value(); got != "initial note!" {
		t.Fatalf("expected input value updated to %q, got %q", "initial note!", got)
	}
}
