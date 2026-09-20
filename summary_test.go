package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSummaryAssetPathFallback(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	postTask := func(id, body, assetPath string) {
		t.Helper()
		payload, err := json.Marshal(map[string]string{
			"id":         id,
			"project":    "render",
			"asset_path": assetPath,
			"body":       body,
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

	postTask("t-empty", "", "models/hero.blend")
	postTask("t-body", "primary body text", "models/unused.blend")

	const insertLegacy = `INSERT INTO tasks (id, project, asset_path, body, status, priority, claim_count, created_at, version)
VALUES (?, 'render', 'models/whitespace.blend', ?, 'pending', 3, 0, unixepoch(), 1)`
	if _, err := db.rw.Exec(insertLegacy, "t-whitespace", "   \n\t"); err != nil {
		t.Fatalf("insert legacy whitespace task failed: %v", err)
	}

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
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}

	want := map[string]string{
		"t-empty":      "models/hero.blend",
		"t-whitespace": "models/whitespace.blend",
		"t-body":       "primary body text",
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
