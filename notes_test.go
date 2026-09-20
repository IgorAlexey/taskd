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

func TestNotes_ValidationAndNotFound(t *testing.T) {
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
	var createdVal map[string]int64
	json.NewDecoder(createRes.Body).Decode(&createdVal)
	createRes.Body.Close()
	taskValURL := fmt.Sprintf("%s/tasks/%d/notes", srv.URL, createdVal["id"])

	cases := []struct {
		url       string
		body      string
		status    int
		wantField string
	}{
		{taskValURL, `{"author":"","text":"hello"}`, http.StatusBadRequest, "author"},
		{taskValURL, `{"author":"   ","text":"hello"}`, http.StatusBadRequest, "author"},
		{taskValURL, `{"author":"alice","text":""}`, http.StatusBadRequest, "text"},
		{taskValURL, `{"author":"alice","text":"   "}`, http.StatusBadRequest, "text"},
		{taskValURL, fmt.Sprintf(`{"author":"%s","text":"hello"}`, strings.Repeat("a", 129)), http.StatusBadRequest, "author"},
		{taskValURL, `{}`, http.StatusBadRequest, "author"},
		{srv.URL + "/tasks/99999/notes", `{"author":"alice","text":"note"}`, http.StatusNotFound, ""},
	}

	for _, tc := range cases {
		res, err := http.Post(tc.url, "application/json", bytes.NewBufferString(tc.body))
		if err != nil {
			t.Fatalf("post failed: %v", err)
		}
		if res.StatusCode != tc.status {
			t.Fatalf("POST %s with %s: expected status %d, got %d", tc.url, tc.body, tc.status, res.StatusCode)
		}
		if tc.wantField != "" {
			var errResp struct {
				Error string `json:"error"`
				Field string `json:"field"`
			}
			if err := json.NewDecoder(res.Body).Decode(&errResp); err != nil {
				t.Fatalf("decode error response failed: %v", err)
			}
			if errResp.Field != tc.wantField {
				t.Fatalf("POST %s with %s: expected field %q, got %q", tc.url, tc.body, tc.wantField, errResp.Field)
			}
		}
		res.Body.Close()
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

func TestNotes_ListByteIdentical(t *testing.T) {
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
	var createdListCheck map[string]int64
	json.NewDecoder(createRes.Body).Decode(&createdListCheck)
	createRes.Body.Close()
	taskListCheckPath := fmt.Sprintf("/tasks/%d", createdListCheck["id"])

	listResBefore, err := http.Get(srv.URL + "/tasks?project=p1")
	if err != nil {
		t.Fatalf("get tasks before failed: %v", err)
	}
	beforeBytes, err := io.ReadAll(listResBefore.Body)
	listResBefore.Body.Close()
	if err != nil {
		t.Fatalf("read before failed: %v", err)
	}

	noteRes, err := http.Post(srv.URL+taskListCheckPath+"/notes", "application/json", bytes.NewBufferString(`{"author":"alice","text":"new note"}`))
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

func TestNoteTextLengthLimit(t *testing.T) {
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
	var createdLimit map[string]int64
	json.NewDecoder(createRes.Body).Decode(&createdLimit)
	createRes.Body.Close()
	taskLimitPath := fmt.Sprintf("/tasks/%d", createdLimit["id"])

	longText := strings.Repeat("a", 65537)
	body, _ := json.Marshal(map[string]string{
		"author": "worker-1",
		"text":   longText,
	})

	res, err := http.Post(srv.URL+taskLimitPath+"/notes", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post note failed: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request, got %d", res.StatusCode)
	}

	var errResp map[string]string
	if err := json.NewDecoder(res.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response failed: %v", err)
	}
	if errResp["error"] != "text too long" {
		t.Fatalf("expected error %q, got %q", "text too long", errResp["error"])
	}

	maxValidText := strings.Repeat("b", 65536)
	validBody, _ := json.Marshal(map[string]string{
		"author": "worker-1",
		"text":   maxValidText,
	})
	resValid, err := http.Post(srv.URL+taskLimitPath+"/notes", "application/json", bytes.NewReader(validBody))
	if err != nil {
		t.Fatalf("post valid note failed: %v", err)
	}
	defer resValid.Body.Close()

	if resValid.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201 Created for 65536 byte note, got %d", resValid.StatusCode)
	}
}
func TestCreateNoteAuthorValidation(t *testing.T) {
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
	var createdAuthor map[string]int64
	json.NewDecoder(createRes.Body).Decode(&createdAuthor)
	createRes.Body.Close()
	taskAuthorPath := fmt.Sprintf("/tasks/%d", createdAuthor["id"])

	validTrimBody := `{"author":"  alice:worker  ","text":"test trimmed author"}`
	res, err := http.Post(srv.URL+taskAuthorPath+"/notes", "application/json", bytes.NewBufferString(validTrimBody))
	if err != nil {
		t.Fatalf("post note failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created for trimmed author, got %d", res.StatusCode)
	}
	var createdNote taskNote
	if err := json.NewDecoder(res.Body).Decode(&createdNote); err != nil {
		t.Fatalf("decode note response failed: %v", err)
	}
	if createdNote.Author != "alice:worker" {
		t.Fatalf("expected author %q, got %q", "alice:worker", createdNote.Author)
	}

	getRes, err := http.Get(srv.URL + taskAuthorPath + "/notes")
	if err != nil {
		t.Fatalf("get notes failed: %v", err)
	}
	defer getRes.Body.Close()
	var notes []taskNote
	if err := json.NewDecoder(getRes.Body).Decode(&notes); err != nil {
		t.Fatalf("decode notes failed: %v", err)
	}
	if len(notes) != 1 || notes[0].Author != "alice:worker" {
		t.Fatalf("expected 1 note with author 'alice:worker', got %+v", notes)
	}

	validCharsBody := `{"author":"worker.1_sub:dir/node-a","text":"test valid chars"}`
	resChars, err := http.Post(srv.URL+taskAuthorPath+"/notes", "application/json", bytes.NewBufferString(validCharsBody))
	if err != nil {
		t.Fatalf("post note failed: %v", err)
	}
	defer resChars.Body.Close()
	if resChars.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created for valid characters, got %d", resChars.StatusCode)
	}
	validWhitespaceBody := `{"author":"  alice\n\t  ","text":"test whitespace author"}`
	resWs, err := http.Post(srv.URL+taskAuthorPath+"/notes", "application/json", bytes.NewBufferString(validWhitespaceBody))
	if err != nil {
		t.Fatalf("post note failed: %v", err)
	}
	defer resWs.Body.Close()
	if resWs.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created for whitespace trimmed author, got %d", resWs.StatusCode)
	}
	var wsNote taskNote
	if err := json.NewDecoder(resWs.Body).Decode(&wsNote); err != nil {
		t.Fatalf("decode note failed: %v", err)
	}
	if wsNote.Author != "alice" {
		t.Fatalf("expected author 'alice', got %q", wsNote.Author)
	}

	invalidAuthors := []string{
		"  alice\nworker  ",
		"alice\tbob",
		"bad author",
		"author@domain",
		"author!",
		"author#1",
		"author$foo",
	}
	for _, badAuthor := range invalidAuthors {
		reqBody, _ := json.Marshal(map[string]string{
			"author": badAuthor,
			"text":   "some note",
		})
		badRes, err := http.Post(srv.URL+taskAuthorPath+"/notes", "application/json", bytes.NewReader(reqBody))
		if err != nil {
			t.Fatalf("post note with bad author %q failed: %v", badAuthor, err)
		}
		if badRes.StatusCode != http.StatusBadRequest {
			badRes.Body.Close()
			t.Fatalf("expected 400 Bad Request for author %q, got %d", badAuthor, badRes.StatusCode)
		}
		var errResp map[string]string
		if err := json.NewDecoder(badRes.Body).Decode(&errResp); err != nil {
			badRes.Body.Close()
			t.Fatalf("decode error response for author %q failed: %v", badAuthor, err)
		}
		badRes.Body.Close()
		if errResp["error"] != "invalid author" {
			t.Fatalf("expected error 'invalid author' for author %q, got %q", badAuthor, errResp["error"])
		}
	}
}
