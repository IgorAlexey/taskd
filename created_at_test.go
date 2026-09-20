package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestCreatedAt_NewTaskAndQuery(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	now := time.Now().Unix()

	createResp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(`{"body":"ts-test","project":"p1"}`))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", createResp.StatusCode)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	listResp, err := http.Get(srv.URL + "/tasks?project=p1")
	if err != nil {
		t.Fatalf("list tasks failed: %v", err)
	}
	defer listResp.Body.Close()
	var tasks []struct {
		ID        string `json:"id"`
		CreatedAt int64  `json:"created_at"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode list tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].ID != created.ID {
		t.Fatalf("expected task id %s, got %s", created.ID, tasks[0].ID)
	}
	if tasks[0].CreatedAt < now-5 || tasks[0].CreatedAt > now+5 {
		t.Fatalf("expected created_at near %d, got %d", now, tasks[0].CreatedAt)
	}

	getResp, err := http.Get(srv.URL + "/tasks/" + created.ID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	defer getResp.Body.Close()
	var single struct {
		ID        string `json:"id"`
		CreatedAt int64  `json:"created_at"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&single); err != nil {
		t.Fatalf("decode single task: %v", err)
	}
	if single.CreatedAt != tasks[0].CreatedAt {
		t.Fatalf("expected single task created_at %d, got %d", tasks[0].CreatedAt, single.CreatedAt)
	}

	fieldsResp, err := http.Get(srv.URL + "/tasks?project=p1&fields=id,created_at")
	if err != nil {
		t.Fatalf("list fields failed: %v", err)
	}
	defer fieldsResp.Body.Close()
	var fieldTasks []struct {
		ID        string `json:"id"`
		CreatedAt int64  `json:"created_at"`
	}
	if err := json.NewDecoder(fieldsResp.Body).Decode(&fieldTasks); err != nil {
		t.Fatalf("decode field tasks: %v", err)
	}
	if len(fieldTasks) != 1 || fieldTasks[0].CreatedAt != tasks[0].CreatedAt {
		t.Fatalf("fields created_at mismatch: %+v", fieldTasks)
	}

	claimResp, err := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewBufferString(`{"worker":"w1","project":"p1"}`))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	defer claimResp.Body.Close()
	var claimed struct {
		ID        string `json:"id"`
		CreatedAt int64  `json:"created_at"`
	}
	if err := json.NewDecoder(claimResp.Body).Decode(&claimed); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if claimed.CreatedAt != tasks[0].CreatedAt {
		t.Fatalf("expected claimed created_at %d, got %d", tasks[0].CreatedAt, claimed.CreatedAt)
	}
}

func TestCreatedAt_MigrationV8(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v7.db")
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	v7Schema := `
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
  claim_count INTEGER NOT NULL DEFAULT 0
);
PRAGMA user_version = 7;
`
	if _, err := rawDB.Exec(v7Schema); err != nil {
		rawDB.Close()
		t.Fatalf("init v7 schema failed: %v", err)
	}
	if _, err := rawDB.Exec("INSERT INTO tasks (id, body, project) VALUES ('v7-task', 'old task', 'p1')"); err != nil {
		rawDB.Close()
		t.Fatalf("insert old task failed: %v", err)
	}
	rawDB.Close()

	store, err := openDB(dbPath, 0)
	if err != nil {
		t.Fatalf("openDB v7 database failed: %v", err)
	}
	defer store.Close()

	var version int
	if err := store.ro.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("query user_version failed: %v", err)
	}
	if version < 8 {
		t.Fatalf("expected schema version >= 8, got %d", version)
	}

	var createdAt int64
	if err := store.ro.QueryRow("SELECT created_at FROM tasks WHERE id = 'v7-task'").Scan(&createdAt); err != nil {
		t.Fatalf("query created_at from migrated task failed: %v", err)
	}
	if createdAt <= 0 {
		t.Fatalf("expected positive created_at, got %d", createdAt)
	}

	srv := httptest.NewServer(newHandler(store, 300))
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(`{"body":"migrated-post","project":"p1"}`))
	if err != nil {
		t.Fatalf("create on migrated db failed: %v", err)
	}
	defer resp.Body.Close()
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	var postCreatedAt int64
	if err := store.ro.QueryRow("SELECT created_at FROM tasks WHERE id = ?", created.ID).Scan(&postCreatedAt); err != nil {
		t.Fatalf("query postCreatedAt failed: %v", err)
	}
	if postCreatedAt <= 0 {
		t.Fatalf("expected positive created_at on migrated db, got %d", postCreatedAt)
	}
}
