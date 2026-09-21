package taskd

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

func (s *server) getNotesHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := pathTaskID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	var dummy int
	if err := s.db.ro.QueryRow("SELECT 1 FROM tasks WHERE id = ?", id).Scan(&dummy); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	} else if err != nil {
		internalError(w, err)
		return
	}
	notes, err := fetchTaskNotes(s.db.ro, id)
	if err != nil {
		internalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(notes)
}

func (s *server) createNoteHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Author string `json:"author"`
		Text   string `json:"text"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Author = strings.TrimSpace(req.Author)
	if req.Author == "" {
		writeFieldError(w, http.StatusBadRequest, "missing author or text", "author")
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeFieldError(w, http.StatusBadRequest, "missing author or text", "text")
		return
	}
	if msg, ok := checkAuthor(req.Author); !ok {
		writeFieldError(w, http.StatusBadRequest, msg, "author")
		return
	}
	if len(req.Text) > maxNoteTextLen {
		writeFieldError(w, http.StatusBadRequest, "text too long", "text")
		return
	}
	id, ok := pathTaskID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	var dummy int
	if err := s.db.ro.QueryRow("SELECT 1 FROM tasks WHERE id = ?", id).Scan(&dummy); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	} else if err != nil {
		internalError(w, err)
		return
	}
	var note taskNote
	err := s.db.rw.QueryRow("INSERT INTO notes (task_id, created_at, author, text) VALUES (?, unixepoch(), ?, ?) RETURNING id, created_at, author, text", id, req.Author, req.Text).
		Scan(&note.ID, &note.CreatedAt, &note.Author, &note.Text)
	if err != nil {
		internalError(w, err)
		return
	}
	s.db.events.publish()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(note)
}
