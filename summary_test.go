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

func TestSummaryFromBody(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	postTask := func(id, body string) {
		t.Helper()
		payload, err := json.Marshal(map[string]string{
			"id":      id,
			"project": "render",
			"body":    body,
		})
		if err != nil {
			t.Fatalf("marshal payload failed: %v", err)
		}
		resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("POST /tasks failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			t.Fatalf("POST /tasks status: %d", resp.StatusCode)
		}
	}

	postTask("t-body", "primary body text\nsecond line details")
	postTask("t-long", "01234567890123456789012345678901234567890123456789extra characters that should be trimmed")

	resp, err := http.Get(srv.URL + "/tasks?fields=id,summary&order=asc")
	if err != nil {
		t.Fatalf("GET /tasks?fields=id,summary failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status: %d", resp.StatusCode)
	}

	var items []struct {
		ID      string `json:"id"`
		Summary string `json:"summary"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode summary failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}

	want := map[string]string{
		"t-body": "primary body text",
		"t-long": "01234567890123456789012345678901234567890123456789…",
	}
	for _, item := range items {
		expected, ok := want[item.ID]
		if !ok {
			t.Errorf("unexpected task ID %q", item.ID)
			continue
		}
		if item.Summary != expected {
			t.Errorf("task %q summary = %q, want %q", item.ID, item.Summary, expected)
		}
	}
}

func TestCreateTaskMissingBody(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/tasks", "application/json", strings.NewReader(`{"project":"proj-1"}`))
	if err != nil {
		t.Fatalf("POST /tasks failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var apiErr apiError
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if apiErr.Error != "missing body" || apiErr.Field != "body" {
		t.Fatalf("got error=%q field=%q, want error=%q field=%q", apiErr.Error, apiErr.Field, "missing body", "body")
	}
}

func TestMigrationV13(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v12.db")
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}

	v12Schema := `
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
  created_at INTEGER NOT NULL DEFAULT (unixepoch()),
  version INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_tasks_pending ON tasks (priority ASC, created_at ASC) WHERE status = 'pending';
CREATE INDEX idx_tasks_pending_project ON tasks (project, priority ASC, created_at ASC) WHERE status = 'pending';
CREATE INDEX idx_tasks_lease_timeout ON tasks (lease_expires ASC) WHERE status = 'leased';
CREATE INDEX idx_tasks_project ON tasks (project, status, priority ASC);
CREATE INDEX idx_tasks_claim_count ON tasks (status, claim_count) WHERE claim_count > 0;
CREATE INDEX idx_tasks_done ON tasks (project) WHERE status = 'done';
CREATE TABLE notes (
  id INTEGER PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  author TEXT NOT NULL,
  text TEXT NOT NULL
);
CREATE INDEX idx_notes_task_id ON notes (task_id);
PRAGMA user_version = 12;
`

	if _, err := rawDB.Exec(v12Schema); err != nil {
		rawDB.Close()
		t.Fatalf("init v12 schema failed: %v", err)
	}

	insertSQL := "INSERT INTO tasks (id, asset_path, body, project) VALUES ('v12-1', 'val', 'task body', 'p1'), ('v12-2', 'models/box.glb', '', 'p1')"
	if _, err := rawDB.Exec(insertSQL); err != nil {
		rawDB.Close()
		t.Fatalf("insert v12 task failed: %v", err)
	}
	rawDB.Close()

	store, err := openDB(dbPath, 0)
	if err != nil {
		t.Fatalf("openDB v12 database failed: %v", err)
	}
	defer store.Close()

	var version int
	if err := store.ro.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("query user_version failed: %v", err)
	}
	if version != 13 {
		t.Fatalf("expected schema version 13, got %d", version)
	}

	rows, err := store.ro.Query("PRAGMA table_info('tasks')")
	if err != nil {
		t.Fatalf("PRAGMA table_info failed: %v", err)
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var (
			cid       int
			name      string
			colType   string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan pragma_table_info failed: %v", err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info iteration failed: %v", err)
	}

	if cols["asset_path"] {
		t.Fatal("expected asset_path to be dropped, but it is still present")
	}

	var body, project string
	if err := store.ro.QueryRow("SELECT body, project FROM tasks WHERE id = 'v12-1'").Scan(&body, &project); err != nil {
		t.Fatalf("query task data failed: %v", err)
	}
	if body != "task body" || project != "p1" {
		t.Fatalf("expected task body='task body' project='p1', got body=%q project=%q", body, project)
	}
	if err := store.ro.QueryRow("SELECT body FROM tasks WHERE id = 'v12-2'").Scan(&body); err != nil {
		t.Fatalf("query asset-only task failed: %v", err)
	}
	if body != "models/box.glb" {
		t.Fatalf("expected asset-only body to fall back to asset_path, got %q", body)
	}
}
