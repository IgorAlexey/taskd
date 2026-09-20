package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestWorkerMaxLength(t *testing.T) {
	_, srv := conflictServer(t)
	id := createTask(t, srv.URL, "p")
	long := strings.Repeat("a", maxWorkerLen+1)

	paths := []string{
		"/tasks/claim",
		"/tasks/" + id + "/claim",
		"/tasks/" + id + "/done",
		"/tasks/" + id + "/touch",
		"/tasks/" + id + "/release",
		"/tasks/" + id + "/bury",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			code, body := post(t, srv.URL+p, map[string]any{"worker": long})
			if code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", code, body)
			}
			if !strings.Contains(string(body), "worker too long") {
				t.Fatalf("expected worker too long, got %s", body)
			}
			code, body = post(t, srv.URL+p, map[string]any{"worker": "  "})
			if code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", code, body)
			}
			if !strings.Contains(string(body), "missing worker") {
				t.Fatalf("expected missing worker, got %s", body)
			}
		})
	}
}

func TestWorkerAtMaxLengthAccepted(t *testing.T) {
	db, srv := conflictServer(t)
	id := createTask(t, srv.URL, "p")
	worker := " " + strings.Repeat("a", maxWorkerLen) + " "

	code, body := post(t, srv.URL+"/tasks/"+id+"/claim", map[string]any{"worker": worker})
	if code != http.StatusOK {
		t.Fatalf("claim expected 200, got %d: %s", code, body)
	}

	var stored string
	if err := db.QueryRow("SELECT worker FROM tasks WHERE id=?", id).Scan(&stored); err != nil {
		t.Fatalf("read worker: %v", err)
	}
	if stored != strings.TrimSpace(worker) {
		t.Fatalf("stored worker %q, want the trimmed %d byte value", stored, maxWorkerLen)
	}

	code, body = post(t, srv.URL+"/tasks/"+id+"/done", map[string]any{"worker": stored})
	if code != http.StatusNoContent {
		t.Fatalf("done expected 204, got %d: %s", code, body)
	}
}

func TestWorkerColumnBoundInSchema(t *testing.T) {
	db, srv := conflictServer(t)
	id := createTask(t, srv.URL, "p")

	_, err := db.Exec("UPDATE tasks SET worker=? WHERE id=?", strings.Repeat("a", maxWorkerLen+1), id)
	if err == nil {
		t.Fatal("expected the worker CHECK constraint to reject an oversized value")
	}
	if !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Fatalf("expected a CHECK constraint failure, got %v", err)
	}
}

func TestMigrationTruncatesOversizedWorker(t *testing.T) {
	dbPath := t.TempDir() + "/old.db"
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	long := strings.Repeat("a", 4096)
	setup := `CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  asset_path TEXT NOT NULL DEFAULT '',
  status TEXT DEFAULT 'pending',
  worker TEXT,
  lease_expires INTEGER,
  primitives JSON,
  body TEXT NOT NULL DEFAULT '',
  priority INTEGER NOT NULL DEFAULT 3,
  project TEXT NOT NULL DEFAULT '',
  claim_count INTEGER NOT NULL DEFAULT 0);
PRAGMA user_version = 4;`
	if _, err := raw.Exec(setup); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := raw.Exec("INSERT INTO tasks (id, status, worker, body) VALUES ('t1', 'leased', ?, 'b')", long); err != nil {
		t.Fatalf("insert: %v", err)
	}
	raw.Close()

	db, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer db.Close()

	var stored string
	if err := db.QueryRow("SELECT worker FROM tasks WHERE id='t1'").Scan(&stored); err != nil {
		t.Fatalf("read worker: %v", err)
	}
	if len(stored) != maxWorkerLen {
		t.Fatalf("worker kept %d bytes, want %d", len(stored), maxWorkerLen)
	}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("expected user_version %d, got %d", schemaVersion, version)
	}
}

func TestExpiredWorkerExcludedFromWorkersAndSearch(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	t1 := createTask(t, srv.URL, "p1")
	claimTask(t, srv.URL, "p1", "w1")
	expireLease(t, db, t1)

	code, body := do(t, http.MethodGet, srv.URL+"/tasks?q=w1", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks?q=w1 expected 200, got %d: %s", code, body)
	}
	var qTasks []taskItem
	if err := json.Unmarshal(body, &qTasks); err != nil {
		t.Fatalf("unmarshal tasks: %v", err)
	}
	if len(qTasks) != 0 {
		t.Fatalf("expected 0 tasks for q=w1 after expiry, got %+v", qTasks)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/workers", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /workers expected 200, got %d: %s", code, body)
	}
	var workers []string
	if err := json.Unmarshal(body, &workers); err != nil {
		t.Fatalf("unmarshal workers: %v", err)
	}
	if len(workers) != 0 {
		t.Fatalf("expected 0 workers after expiry, got %v", workers)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks?worker=w1", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks?worker=w1 expected 200, got %d: %s", code, body)
	}
	var wTasks []taskItem
	if err := json.Unmarshal(body, &wTasks); err != nil {
		t.Fatalf("unmarshal tasks: %v", err)
	}
	if len(wTasks) != 0 {
		t.Fatalf("expected 0 tasks for worker=w1 after expiry, got %+v", wTasks)
	}

	t2 := createTask(t, srv.URL, "p1")
	if code, body := post(t, srv.URL+"/tasks/"+t2+"/claim", map[string]any{"worker": "w2"}); code != http.StatusOK {
		t.Fatalf("claim t2 expected 200, got %d: %s", code, body)
	}

	t3 := createTask(t, srv.URL, "p1")
	if code, body := post(t, srv.URL+"/tasks/"+t3+"/claim", map[string]any{"worker": "w3"}); code != http.StatusOK {
		t.Fatalf("claim t3 expected 200, got %d: %s", code, body)
	}
	if code, body := post(t, srv.URL+"/tasks/"+t3+"/done", map[string]any{"worker": "w3"}); code != http.StatusNoContent {
		t.Fatalf("done expected 204, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/workers", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /workers expected 200, got %d: %s", code, body)
	}
	workers = nil
	if err := json.Unmarshal(body, &workers); err != nil {
		t.Fatalf("unmarshal workers: %v", err)
	}
	if !slices.Equal(workers, []string{"w2", "w3"}) {
		t.Fatalf("expected [w2, w3], got %v", workers)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks?q=w2", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks?q=w2 expected 200, got %d: %s", code, body)
	}
	qTasks = nil
	if err := json.Unmarshal(body, &qTasks); err != nil {
		t.Fatalf("unmarshal tasks: %v", err)
	}
	if len(qTasks) != 1 || qTasks[0].ID != t2 {
		t.Fatalf("expected [t2] for q=w2, got %+v", qTasks)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks?q=w3", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks?q=w3 expected 200, got %d: %s", code, body)
	}
	qTasks = nil
	if err := json.Unmarshal(body, &qTasks); err != nil {
		t.Fatalf("unmarshal tasks: %v", err)
	}
	if len(qTasks) != 1 || qTasks[0].ID != t3 {
		t.Fatalf("expected [t3] for q=w3, got %+v", qTasks)
	}
}
