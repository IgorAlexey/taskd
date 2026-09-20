package main

import (
	"database/sql"
	"net/http"
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
