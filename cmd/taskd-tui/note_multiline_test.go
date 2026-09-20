package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestNoteMultilineEditing(t *testing.T) {
	var submittedText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/tasks/1/notes") {
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			submittedText = body["text"]
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": 1, "text": submittedText})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	m := newModel(config{url: srv.URL, worker: "worker-1", icons: false, refresh: time.Hour}, newClient(srv.URL))
	m.width = 100
	m.height = 30
	m.tasks = []task{
		{ID: 1, Project: "test", Status: "pending", Body: "sample task"},
	}
	m.rebuildShown()
	m.syncDetail()

	up, _ := m.Update(tea.KeyPressMsg{Text: "a"})
	m = up.(model)
	if m.mode != modeNote {
		t.Fatalf("expected modeNote, got %v", m.mode)
	}

	for _, ch := range "first line" {
		up, _ = m.Update(tea.KeyPressMsg{Text: string(ch)})
		m = up.(model)
	}

	up, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = up.(model)
	if m.mode != modeNote {
		t.Fatalf("Enter must not submit; mode = %v, want modeNote", m.mode)
	}
	if !strings.Contains(m.note.input.Value(), "first line\n") {
		t.Fatalf("Enter must insert newline; got %q", m.note.input.Value())
	}

	for _, ch := range "second line" {
		up, _ = m.Update(tea.KeyPressMsg{Text: string(ch)})
		m = up.(model)
	}
	if m.note.input.Value() != "first line\nsecond line" {
		t.Fatalf("expected two lines; got %q", m.note.input.Value())
	}

	up, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m = up.(model)
	if cmd == nil {
		t.Fatal("Ctrl+S must produce submission command")
	}

	res := cmd()
	formAct, ok := res.(formActMsg)
	if !ok {
		t.Fatalf("expected formActMsg, got %T", res)
	}
	if formAct.err != nil {
		t.Fatalf("unexpected submission error: %v", formAct.err)
	}

	up, _ = m.Update(formAct)
	m = up.(model)
	if m.mode != modeTable {
		t.Fatalf("expected modeTable after submit; got %v", m.mode)
	}

	if submittedText != "first line\nsecond line" {
		t.Fatalf("submitted text = %q, want %q", submittedText, "first line\nsecond line")
	}
}

func TestNoteEmptyCtrlSError(t *testing.T) {
	m := newModel(config{icons: false, refresh: time.Hour}, nil)
	m.width = 80
	m.height = 24
	m.tasks = []task{
		{ID: 1, Project: "test", Status: "pending", Body: "sample task"},
	}
	m.rebuildShown()

	up, _ := m.Update(tea.KeyPressMsg{Text: "a"})
	m = up.(model)

	up, cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m = up.(model)
	if cmd != nil {
		t.Fatal("empty note submission must not return command")
	}
	if m.mode != modeNote {
		t.Fatalf("mode = %v, want modeNote", m.mode)
	}
	if m.note.errText != "note text cannot be empty" {
		t.Fatalf("errText = %q, want 'note text cannot be empty'", m.note.errText)
	}
}
