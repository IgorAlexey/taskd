package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func poolDo(t *testing.T, client *http.Client, method, url string, body any) (int, []byte, http.Header) {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body failed: %v", err)
		}
		reqBody = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("new request %s %s failed: %v", method, url, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response of %s %s failed: %v", method, url, err)
	}
	return resp.StatusCode, data, resp.Header
}

func TestReadsDoNotBlockOnWriter(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	id := createTask(t, srv.URL, "p-pool")

	tx, err := db.rw.Begin()
	if err != nil {
		t.Fatalf("db.Begin failed: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("INSERT INTO tasks (id, asset_path, body, priority, project) VALUES (?, ?, ?, ?, ?)",
		"held-by-open-transaction", "held.blend", "", defaultPriority, "p-pool"); err != nil {
		t.Fatalf("write inside open transaction failed: %v", err)
	}

	client := &http.Client{Timeout: time.Second}

	code, body, _ := poolDo(t, client, http.MethodGet, srv.URL+"/tasks/"+id, nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks/{id} while a write transaction is open expected 200, got %d: %s", code, body)
	}
	var item taskItem
	if err := json.Unmarshal(body, &item); err != nil {
		t.Fatalf("unmarshal task failed: %v", err)
	}
	if item.ID != id {
		t.Fatalf("GET /tasks/{id} returned id %q, want %q", item.ID, id)
	}
	if item.Status != "pending" {
		t.Fatalf("GET /tasks/{id} returned status %q, want pending", item.Status)
	}

	code, body, header := poolDo(t, client, http.MethodGet, srv.URL+"/tasks?limit=10", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks while a write transaction is open expected 200, got %d: %s", code, body)
	}
	var tasks []taskItem
	if err := json.Unmarshal(body, &tasks); err != nil {
		t.Fatalf("unmarshal list failed: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != id {
		t.Fatalf("list expected the single committed task %q, got %s", id, body)
	}
	if total := header.Get("X-Total-Count"); total != "1" {
		t.Fatalf("list expected X-Total-Count: 1, got %q", total)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("tx.Rollback failed: %v", err)
	}

	code, body, _ = poolDo(t, client, http.MethodPost, srv.URL+"/tasks/"+id+"/claim", map[string]any{"worker": "w1"})
	if code != http.StatusOK {
		t.Fatalf("claim after the write transaction ended expected 200, got %d: %s", code, body)
	}
}

func TestMemoryDatabaseServesReads(t *testing.T) {
	mem, err := openDB(":memory:")
	if err != nil {
		t.Fatalf("openDB :memory: failed: %v", err)
	}
	defer mem.Close()

	srv := httptest.NewServer(newHandler(mem, 300))
	defer srv.Close()

	id := createTask(t, srv.URL, "p-pool")

	client := &http.Client{Timeout: time.Second}
	code, body, _ := poolDo(t, client, http.MethodGet, srv.URL+"/tasks/"+id, nil)
	if code != http.StatusOK {
		t.Fatalf("in-memory GET /tasks/{id} expected 200, got %d: %s", code, body)
	}

	code, tasks, total := listTotalCount(t, srv.URL+"/tasks")
	if code != http.StatusOK {
		t.Fatalf("in-memory list expected 200, got %d", code)
	}
	if len(tasks) != 1 || total != "1" {
		t.Fatalf("in-memory list expected 1 row and X-Total-Count 1, got %d rows and %q", len(tasks), total)
	}
}
