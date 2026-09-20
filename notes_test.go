package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotes_CreateAndOrder(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 30))
	defer srv.Close()

	createRes, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(`{"id":"task-notes-1","body":"spec","project":"p1"}`))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	createRes.Body.Close()
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", createRes.StatusCode)
	}

	notePayload1 := `{"author":"worker-1","text":"first note"}`
	res1, err := http.Post(srv.URL+"/tasks/task-notes-1/notes", "application/json", bytes.NewBufferString(notePayload1))
	if err != nil {
		t.Fatalf("post note 1 failed: %v", err)
	}
	defer res1.Body.Close()
	if res1.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", res1.StatusCode)
	}
	var note1 taskNote
	if err := json.NewDecoder(res1.Body).Decode(&note1); err != nil {
		t.Fatalf("decode note 1 failed: %v", err)
	}
	if note1.ID <= 0 || note1.CreatedAt <= 0 || note1.Author != "worker-1" || note1.Text != "first note" {
		t.Fatalf("unexpected note 1: %+v", note1)
	}

	notePayload2 := `{"author":"worker-2","text":"second note"}`
	res2, err := http.Post(srv.URL+"/tasks/task-notes-1/notes", "application/json", bytes.NewBufferString(notePayload2))
	if err != nil {
		t.Fatalf("post note 2 failed: %v", err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", res2.StatusCode)
	}
	var note2 taskNote
	if err := json.NewDecoder(res2.Body).Decode(&note2); err != nil {
		t.Fatalf("decode note 2 failed: %v", err)
	}
	if note2.ID <= note1.ID {
		t.Fatalf("expected note2.ID > note1.ID, got %d <= %d", note2.ID, note1.ID)
	}

	getRes, err := http.Get(srv.URL + "/tasks/task-notes-1")
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	defer getRes.Body.Close()
	if getRes.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", getRes.StatusCode)
	}
	var detail struct {
		ID    string     `json:"id"`
		Notes []taskNote `json:"notes"`
	}
	if err := json.NewDecoder(getRes.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail failed: %v", err)
	}
	if len(detail.Notes) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(detail.Notes))
	}
	if detail.Notes[0].ID != note1.ID || detail.Notes[0].Text != "first note" {
		t.Fatalf("expected note1 first, got %+v", detail.Notes[0])
	}
	if detail.Notes[1].ID != note2.ID || detail.Notes[1].Text != "second note" {
		t.Fatalf("expected note2 second, got %+v", detail.Notes[1])
	}
}

func TestNotes_ValidationAndNotFound(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 30))
	defer srv.Close()

	createRes, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(`{"id":"task-val-1","body":"spec","project":"p1"}`))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	createRes.Body.Close()

	cases := []struct {
		url    string
		body   string
		status int
	}{
		{srv.URL + "/tasks/task-val-1/notes", `{"author":"","text":"hello"}`, http.StatusBadRequest},
		{srv.URL + "/tasks/task-val-1/notes", `{"author":"   ","text":"hello"}`, http.StatusBadRequest},
		{srv.URL + "/tasks/task-val-1/notes", `{"author":"alice","text":""}`, http.StatusBadRequest},
		{srv.URL + "/tasks/task-val-1/notes", `{"author":"alice","text":"   "}`, http.StatusBadRequest},
		{srv.URL + "/tasks/task-val-1/notes", fmt.Sprintf(`{"author":"%s","text":"hello"}`, strings.Repeat("a", 129)), http.StatusBadRequest},
		{srv.URL + "/tasks/task-val-1/notes", `{}`, http.StatusBadRequest},
		{srv.URL + "/tasks/nonexistent-task-id/notes", `{"author":"alice","text":"note"}`, http.StatusNotFound},
	}

	for _, tc := range cases {
		res, err := http.Post(tc.url, "application/json", bytes.NewBufferString(tc.body))
		if err != nil {
			t.Fatalf("post failed: %v", err)
		}
		res.Body.Close()
		if res.StatusCode != tc.status {
			t.Fatalf("POST %s with %s: expected status %d, got %d", tc.url, tc.body, tc.status, res.StatusCode)
		}
	}
}

func TestNotes_CascadeOnTaskDelete(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 30))
	defer srv.Close()

	createRes, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(`{"id":"task-cascade","body":"spec","project":"p1"}`))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	createRes.Body.Close()

	noteRes, err := http.Post(srv.URL+"/tasks/task-cascade/notes", "application/json", bytes.NewBufferString(`{"author":"alice","text":"note to delete"}`))
	if err != nil {
		t.Fatalf("post note failed: %v", err)
	}
	noteRes.Body.Close()
	if noteRes.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", noteRes.StatusCode)
	}

	var countBefore int
	if err := db.ro.QueryRow("SELECT count(*) FROM notes WHERE task_id = 'task-cascade'").Scan(&countBefore); err != nil {
		t.Fatalf("count notes failed: %v", err)
	}
	if countBefore != 1 {
		t.Fatalf("expected 1 note before delete, got %d", countBefore)
	}

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/tasks/task-cascade", nil)
	if err != nil {
		t.Fatalf("new delete req failed: %v", err)
	}
	delRes, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	delRes.Body.Close()
	if delRes.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", delRes.StatusCode)
	}

	var countAfter int
	if err := db.ro.QueryRow("SELECT count(*) FROM notes WHERE task_id = 'task-cascade'").Scan(&countAfter); err != nil {
		t.Fatalf("count notes after delete failed: %v", err)
	}
	if countAfter != 0 {
		t.Fatalf("expected 0 notes after cascade delete, got %d", countAfter)
	}
}

func TestNotes_ListByteIdentical(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 30))
	defer srv.Close()

	createRes, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(`{"id":"task-list-check","body":"spec body","project":"p1"}`))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	createRes.Body.Close()

	listResBefore, err := http.Get(srv.URL + "/tasks?project=p1")
	if err != nil {
		t.Fatalf("get tasks before failed: %v", err)
	}
	beforeBytes, err := io.ReadAll(listResBefore.Body)
	listResBefore.Body.Close()
	if err != nil {
		t.Fatalf("read before failed: %v", err)
	}

	noteRes, err := http.Post(srv.URL+"/tasks/task-list-check/notes", "application/json", bytes.NewBufferString(`{"author":"alice","text":"new note"}`))
	if err != nil {
		t.Fatalf("post note failed: %v", err)
	}
	noteRes.Body.Close()
	if noteRes.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", noteRes.StatusCode)
	}

	listResAfter, err := http.Get(srv.URL + "/tasks?project=p1")
	if err != nil {
		t.Fatalf("get tasks after failed: %v", err)
	}
	afterBytes, err := io.ReadAll(listResAfter.Body)
	listResAfter.Body.Close()
	if err != nil {
		t.Fatalf("read after failed: %v", err)
	}

	if !bytes.Equal(beforeBytes, afterBytes) {
		t.Fatalf("expected GET /tasks output to be byte-identical:\nbefore: %s\nafter:  %s", string(beforeBytes), string(afterBytes))
	}
}

func TestNotes_AcceptsAnyStatus(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 30))
	defer srv.Close()

	statuses := []string{"done", "buried"}
	for _, status := range statuses {
		id := fmt.Sprintf("task-%s", status)
		createRes, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(fmt.Sprintf(`{"id":"%s","body":"test","project":"p1"}`, id)))
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}
		createRes.Body.Close()

		claimRes, err := http.Post(srv.URL+"/tasks/"+id+"/claim", "application/json", bytes.NewBufferString(`{"worker":"w1"}`))
		if err != nil {
			t.Fatalf("claim failed: %v", err)
		}
		claimRes.Body.Close()

		actionRes, err := http.Post(srv.URL+"/tasks/"+id+"/"+status, "application/json", bytes.NewBufferString(`{"worker":"w1"}`))
		if err != nil {
			t.Fatalf("action failed: %v", err)
		}
		actionRes.Body.Close()

		noteRes, err := http.Post(srv.URL+"/tasks/"+id+"/notes", "application/json", bytes.NewBufferString(`{"author":"auditor","text":"status note"}`))
		if err != nil {
			t.Fatalf("post note on %s task failed: %v", status, err)
		}
		noteRes.Body.Close()
		if noteRes.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201 on %s task, got %d", status, noteRes.StatusCode)
		}
	}
}

func TestNotes_MigrationV11(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v9.db")
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	v9Schema := `
CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  asset_path TEXT NOT NULL DEFAULT '',
  status TEXT DEFAULT 'pending',
  worker TEXT CHECK (octet_length(worker) <= 128),
  lease_expires INTEGER,
  primitives JSON,
  body TEXT NOT NULL DEFAULT '',
  priority INTEGER NOT NULL DEFAULT 3 CHECK (priority >= 0),
  project TEXT NOT NULL DEFAULT '',
  claim_count INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE INDEX idx_tasks_done ON tasks (project) WHERE status = 'done';
PRAGMA user_version = 9;`
	if _, err := rawDB.Exec(v9Schema); err != nil {
		t.Fatalf("exec v9 schema failed: %v", err)
	}
	if _, err := rawDB.Exec("INSERT INTO tasks (id, body, project) VALUES ('v9-task', 'v9 body', 'p1')"); err != nil {
		t.Fatalf("insert v9 task failed: %v", err)
	}
	rawDB.Close()

	store, err := openDB(dbPath, 0)
	if err != nil {
		t.Fatalf("openDB v9 database failed: %v", err)
	}
	defer store.Close()

	var version int
	if err := store.ro.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("query user_version failed: %v", err)
	}
	if version < 11 {
		t.Fatalf("expected schema version >= 11, got %d", version)
	}

	srv := httptest.NewServer(newHandler(store, 300))
	defer srv.Close()

	res, err := http.Post(srv.URL+"/tasks/v9-task/notes", "application/json", bytes.NewBufferString(`{"author":"migrator","text":"migrated note"}`))
	if err != nil {
		t.Fatalf("post note failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created on migrated db, got %d", res.StatusCode)
	}
}
