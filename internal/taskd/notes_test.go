package taskd

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
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

	createRes, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(`{"body":"spec","project":"p1"}`))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var created1 map[string]int64
	json.NewDecoder(createRes.Body).Decode(&created1)
	createRes.Body.Close()
	taskPath1 := fmt.Sprintf("/tasks/%d", created1["id"])
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", createRes.StatusCode)
	}

	notePayload1 := `{"author":"worker-1","text":"first note"}`
	res1, err := http.Post(srv.URL+taskPath1+"/notes", "application/json", bytes.NewBufferString(notePayload1))
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
	res2, err := http.Post(srv.URL+taskPath1+"/notes", "application/json", bytes.NewBufferString(notePayload2))
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

	getRes, err := http.Get(srv.URL + taskPath1)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	defer getRes.Body.Close()
	if getRes.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", getRes.StatusCode)
	}
	var detail struct {
		ID    int64      `json:"id"`
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

func TestNotes_CascadeOnTaskDelete(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 30))
	defer srv.Close()

	createRes, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(`{"body":"spec","project":"p1"}`))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var createdCascade map[string]int64
	json.NewDecoder(createRes.Body).Decode(&createdCascade)
	createRes.Body.Close()
	taskCascadeID := createdCascade["id"]
	taskCascadePath := fmt.Sprintf("/tasks/%d", taskCascadeID)

	noteRes, err := http.Post(srv.URL+taskCascadePath+"/notes", "application/json", bytes.NewBufferString(`{"author":"alice","text":"note to delete"}`))
	if err != nil {
		t.Fatalf("post note failed: %v", err)
	}
	noteRes.Body.Close()
	if noteRes.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", noteRes.StatusCode)
	}

	var countBefore int
	if err := db.ro.QueryRow("SELECT count(*) FROM notes WHERE task_id = ?", taskCascadeID).Scan(&countBefore); err != nil {
		t.Fatalf("count notes failed: %v", err)
	}
	if countBefore != 1 {
		t.Fatalf("expected 1 note before delete, got %d", countBefore)
	}

	req, err := http.NewRequest(http.MethodDelete, srv.URL+taskCascadePath, nil)
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
	if err := db.ro.QueryRow("SELECT count(*) FROM notes WHERE task_id = ?", taskCascadeID).Scan(&countAfter); err != nil {
		t.Fatalf("count notes after delete failed: %v", err)
	}
	if countAfter != 0 {
		t.Fatalf("expected 0 notes after cascade delete, got %d", countAfter)
	}
}

func TestNotes_ListCarriesLastNote(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 30))
	defer srv.Close()

	createRes, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(`{"body":"spec body","project":"p1"}`))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var created map[string]int64
	json.NewDecoder(createRes.Body).Decode(&created)
	createRes.Body.Close()
	notesPath := fmt.Sprintf("/tasks/%d/notes", created["id"])

	list := func() []map[string]any {
		res, err := http.Get(srv.URL + "/tasks?project=p1")
		if err != nil {
			t.Fatalf("get tasks failed: %v", err)
		}
		defer res.Body.Close()
		var items []map[string]any
		json.NewDecoder(res.Body).Decode(&items)
		return items
	}

	if _, has := list()[0]["last_note"]; has {
		t.Fatalf("expected no last_note before any note")
	}
	for _, text := range []string{"first", "second"} {
		res, err := http.Post(srv.URL+notesPath, "application/json", bytes.NewBufferString(`{"author":"alice","text":"`+text+`"}`))
		if err != nil {
			t.Fatalf("post note failed: %v", err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", res.StatusCode)
		}
	}
	last, _ := list()[0]["last_note"].(map[string]any)
	if last["author"] != "alice" || last["text"] != "second" {
		t.Fatalf("expected last_note alice/second, got %v", last)
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
		createRes, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(`{"body":"test","project":"p1"}`))
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}
		var createdStatus map[string]int64
		json.NewDecoder(createRes.Body).Decode(&createdStatus)
		createRes.Body.Close()
		taskStatusPath := fmt.Sprintf("/tasks/%d", createdStatus["id"])

		claimRes, err := http.Post(srv.URL+taskStatusPath+"/claim", "application/json", bytes.NewBufferString(`{"worker":"w1"}`))
		if err != nil {
			t.Fatalf("claim failed: %v", err)
		}
		claimRes.Body.Close()

		actionRes, err := http.Post(srv.URL+taskStatusPath+"/"+status, "application/json", bytes.NewBufferString(`{"worker":"w1"}`))
		if err != nil {
			t.Fatalf("action failed: %v", err)
		}
		actionRes.Body.Close()

		noteRes, err := http.Post(srv.URL+taskStatusPath+"/notes", "application/json", bytes.NewBufferString(`{"author":"auditor","text":"status note"}`))
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

	db, err := openDB(dbPath, 0)
	if err != nil {
		t.Fatalf("openDB v9 database failed: %v", err)
	}
	defer db.Close()

	var version int
	if err := db.ro.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("query user_version failed: %v", err)
	}
	if version < 11 {
		t.Fatalf("expected schema version >= 11, got %d", version)
	}

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	res, err := http.Post(srv.URL+"/tasks/1/notes", "application/json", bytes.NewBufferString(`{"author":"migrator","text":"migrated note"}`))
	if err != nil {
		t.Fatalf("post note failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created on migrated db, got %d", res.StatusCode)
	}
}

func TestGetTaskNotesEndpoint(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 30))
	defer srv.Close()

	createRes, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(`{"body":"spec","project":"p1"}`))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var createdGetNotes map[string]int64
	json.NewDecoder(createRes.Body).Decode(&createdGetNotes)
	createRes.Body.Close()
	taskGetNotesPath := fmt.Sprintf("/tasks/%d", createdGetNotes["id"])

	emptyRes, err := http.Get(srv.URL + taskGetNotesPath + "/notes")
	if err != nil {
		t.Fatalf("get empty notes failed: %v", err)
	}
	defer emptyRes.Body.Close()
	if emptyRes.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for empty notes, got %d", emptyRes.StatusCode)
	}
	if ct := emptyRes.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected application/json Content-Type, got %q", ct)
	}
	var emptyNotes []taskNote
	if err := json.NewDecoder(emptyRes.Body).Decode(&emptyNotes); err != nil {
		t.Fatalf("decode empty notes failed: %v", err)
	}
	if len(emptyNotes) != 0 {
		t.Fatalf("expected 0 notes, got %d", len(emptyNotes))
	}

	postRes1, err := http.Post(srv.URL+taskGetNotesPath+"/notes", "application/json", bytes.NewBufferString(`{"author":"alice","text":"note one"}`))
	if err != nil {
		t.Fatalf("post note 1 failed: %v", err)
	}
	postRes1.Body.Close()

	postRes2, err := http.Post(srv.URL+taskGetNotesPath+"/notes", "application/json", bytes.NewBufferString(`{"author":"bob","text":"note two"}`))
	if err != nil {
		t.Fatalf("post note 2 failed: %v", err)
	}
	postRes2.Body.Close()

	getRes, err := http.Get(srv.URL + taskGetNotesPath + "/notes")
	if err != nil {
		t.Fatalf("get notes failed: %v", err)
	}
	defer getRes.Body.Close()
	if getRes.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", getRes.StatusCode)
	}
	var notes []taskNote
	if err := json.NewDecoder(getRes.Body).Decode(&notes); err != nil {
		t.Fatalf("decode notes failed: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(notes))
	}
	if notes[0].Author != "alice" || notes[0].Text != "note one" {
		t.Fatalf("unexpected note 0: %+v", notes[0])
	}
	if notes[1].Author != "bob" || notes[1].Text != "note two" {
		t.Fatalf("unexpected note 1: %+v", notes[1])
	}
	if notes[0].ID >= notes[1].ID {
		t.Fatalf("expected notes ordered by id ASC, got %d >= %d", notes[0].ID, notes[1].ID)
	}

	notFoundRes, err := http.Get(srv.URL + "/tasks/99999/notes")
	if err != nil {
		t.Fatalf("get nonexistent notes failed: %v", err)
	}
	notFoundRes.Body.Close()
	if notFoundRes.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d", notFoundRes.StatusCode)
	}

	putReq, err := http.NewRequest(http.MethodPut, srv.URL+taskGetNotesPath+"/notes", nil)
	if err != nil {
		t.Fatalf("new put request failed: %v", err)
	}
	putRes, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatalf("put request failed: %v", err)
	}
	defer putRes.Body.Close()
	if putRes.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed, got %d", putRes.StatusCode)
	}
	allow := putRes.Header.Get("Allow")
	if !strings.Contains(allow, "GET") || !strings.Contains(allow, "POST") {
		t.Fatalf("expected Allow header to contain GET and POST, got %q", allow)
	}
}
