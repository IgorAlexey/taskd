package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestPurgeDoneTasks(t *testing.T) {
	db, err := openDB(":memory:", 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	srv := httptest.NewServer(newHandler(db, 300))
	t.Cleanup(srv.Close)

	create := func(id, project, status string) {
		t.Helper()
		_, err := db.rw.Exec(
			"INSERT INTO tasks (id, project, status, body, priority, created_at) VALUES (?, ?, ?, ?, 3, unixepoch())",
			id, project, status, "task "+id,
		)
		if err != nil {
			t.Fatalf("insert task %s failed: %v", id, err)
		}
	}

	create("d1", "p1", "done")
	create("d2", "p1", "done")
	create("d3", "p2", "done")
	create("p1", "p1", "pending")
	create("l1", "p1", "leased")
	create("b1", "p1", "buried")

	resp, err := http.Post(srv.URL+"/tasks/purge?project=p1", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("POST /tasks/purge failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /tasks/purge status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result struct {
		Deleted int64 `json:"deleted"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if result.Deleted != 2 {
		t.Fatalf("deleted = %d, want 2", result.Deleted)
	}

	var count int
	if err := db.ro.QueryRow("SELECT count(*) FROM tasks WHERE id IN ('d1', 'd2')").Scan(&count); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected d1, d2 deleted, found %d", count)
	}

	respAll, err := http.Post(srv.URL+"/tasks/purge", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("POST /tasks/purge without project failed: %v", err)
	}
	defer respAll.Body.Close()

	if respAll.StatusCode != http.StatusOK {
		t.Fatalf("POST /tasks/purge without project status = %d, want %d", respAll.StatusCode, http.StatusOK)
	}

	var resultAll struct {
		Deleted int64 `json:"deleted"`
	}
	if err := json.NewDecoder(respAll.Body).Decode(&resultAll); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if resultAll.Deleted != 1 {
		t.Fatalf("deleted = %d, want 1", resultAll.Deleted)
	}

	if err := db.ro.QueryRow("SELECT count(*) FROM tasks WHERE status = 'done'").Scan(&count); err != nil {
		t.Fatalf("count done failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 done tasks remaining, found %d", count)
	}

	if err := db.ro.QueryRow("SELECT count(*) FROM tasks").Scan(&count); err != nil {
		t.Fatalf("count total failed: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 tasks remaining (pending, leased, buried), found %d", count)
	}

	respEmpty, err := http.Post(srv.URL+"/tasks/purge", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("POST /tasks/purge empty failed: %v", err)
	}
	defer respEmpty.Body.Close()

	var resultEmpty struct {
		Deleted int64 `json:"deleted"`
	}
	if err := json.NewDecoder(respEmpty.Body).Decode(&resultEmpty); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if resultEmpty.Deleted != 0 {
		t.Fatalf("deleted = %d, want 0", resultEmpty.Deleted)
	}
}

func TestPurgeValidationAndMethods(t *testing.T) {
	db, err := openDB(":memory:", 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	srv := httptest.NewServer(newHandler(db, 300))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/tasks/purge", nil)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /tasks/purge failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /tasks/purge status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}

	respInvalidParam, err := http.Post(srv.URL+"/tasks/purge?invalid=param", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /tasks/purge invalid param failed: %v", err)
	}
	defer respInvalidParam.Body.Close()
	if respInvalidParam.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /tasks/purge invalid param status = %d, want %d", respInvalidParam.StatusCode, http.StatusBadRequest)
	}

	respEmptyProject, err := http.Post(srv.URL+"/tasks/purge?project=", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /tasks/purge empty project failed: %v", err)
	}
	defer respEmptyProject.Body.Close()
	if respEmptyProject.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /tasks/purge empty project status = %d, want %d", respEmptyProject.StatusCode, http.StatusBadRequest)
	}

	respBadProject, err := http.Post(srv.URL+"/tasks/purge?project=invalid%20name", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /tasks/purge bad project failed: %v", err)
	}
	defer respBadProject.Body.Close()
	if respBadProject.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /tasks/purge bad project status = %d, want %d", respBadProject.StatusCode, http.StatusBadRequest)
	}

	var usage bytes.Buffer
	printUsage(&usage)
	if !strings.Contains(usage.String(), "POST   /tasks/purge") {
		t.Fatalf("printUsage missing POST /tasks/purge: %s", usage.String())
	}
}

func TestPurgeMigrationV9(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v8.db")
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	schema := `
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
PRAGMA user_version = 8;`
	if _, err := rawDB.Exec(schema); err != nil {
		t.Fatalf("exec schema failed: %v", err)
	}
	rawDB.Close()

	store, err := openDB(dbPath, 0)
	if err != nil {
		t.Fatalf("openDB v8 database failed: %v", err)
	}
	defer store.Close()

	var version int
	if err := store.ro.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("query user_version failed: %v", err)
	}
	if version != 9 {
		t.Fatalf("expected schema version 9, got %d", version)
	}

	var indexExists int
	if err := store.ro.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name='idx_tasks_done'").Scan(&indexExists); err != nil {
		t.Fatalf("query idx_tasks_done index failed: %v", err)
	}
	if indexExists != 1 {
		t.Fatalf("expected idx_tasks_done index to exist, got count %d", indexExists)
	}
}
