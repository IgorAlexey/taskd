package taskd

import (
	"bytes"
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

	postTask := func(body string) int64 {
		t.Helper()
		payload, err := json.Marshal(map[string]string{
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
		var created map[string]int64
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		return created["id"]
	}

	id1 := postTask("primary body text\nsecond line details")
	id2 := postTask("01234567890123456789012345678901234567890123456789extra characters that should be trimmed")

	resp, err := http.Get(srv.URL + "/tasks?fields=id,summary&order=asc")
	if err != nil {
		t.Fatalf("GET /tasks?fields=id,summary failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status: %d", resp.StatusCode)
	}

	var items []struct {
		ID      int64  `json:"id"`
		Summary string `json:"summary"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode summary failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}

	want := map[int64]string{
		id1: "primary body text",
		id2: "01234567890123456789012345678901234567890123456789…",
	}
	for _, item := range items {
		expected, ok := want[item.ID]
		if !ok {
			t.Errorf("unexpected task ID %d", item.ID)
			continue
		}
		if item.Summary != expected {
			t.Errorf("task %d summary = %q, want %q", item.ID, item.Summary, expected)
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
