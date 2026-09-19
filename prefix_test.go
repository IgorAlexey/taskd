package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrefixResolution(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "test_prefix.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	tasksToCreate := []string{"ab12cd34ef", "ab12cd99ff", "7f3a0011", "ab12"}
	for _, id := range tasksToCreate {
		code, body := post(t, srv.URL+"/tasks", map[string]string{
			"id":         id,
			"asset_path": "model.obj",
			"project":    "proj",
		})
		if code != http.StatusCreated {
			t.Fatalf("failed to create task %s: status %d, body %s", id, code, body)
		}
	}

	code, body := do(t, http.MethodGet, srv.URL+"/tasks/7f3a0011", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks/7f3a0011 expected 200, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks/7f3a", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks/7f3a expected 200, got %d: %s", code, body)
	}
	var res struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("unmarshal GET /tasks/7f3a: %v", err)
	}
	if res.ID != "7f3a0011" {
		t.Fatalf("GET /tasks/7f3a id want 7f3a0011, got %q", res.ID)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks/ab12c", nil)
	if code != http.StatusConflict {
		t.Fatalf("GET /tasks/ab12c expected 409, got %d: %s", code, body)
	}
	sbody := string(body)
	if !strings.Contains(sbody, "ab12cd34ef") || !strings.Contains(sbody, "ab12cd99ff") {
		t.Fatalf("GET /tasks/ab12c body expected to name both candidates, got: %s", sbody)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks/ab12", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks/ab12 expected 200, got %d: %s", code, body)
	}
	res.ID = ""
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("unmarshal GET /tasks/ab12: %v", err)
	}
	if res.ID != "ab12" {
		t.Fatalf("GET /tasks/ab12 id want ab12, got %q", res.ID)
	}

	code, body = post(t, srv.URL+"/tasks/7f3a/claim", map[string]string{"worker": "w1"})
	if code != http.StatusOK {
		t.Fatalf("POST /tasks/7f3a/claim expected 200, got %d: %s", code, body)
	}
	res.ID = ""
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("unmarshal claim response: %v", err)
	}
	if res.ID != "7f3a0011" {
		t.Fatalf("claimed id want 7f3a0011, got %q", res.ID)
	}

	code, body = do(t, http.MethodDelete, srv.URL+"/tasks/ab12c", nil)
	if code != http.StatusConflict {
		t.Fatalf("DELETE /tasks/ab12c expected 409, got %d: %s", code, body)
	}
	code, _ = do(t, http.MethodGet, srv.URL+"/tasks/ab12cd34ef", nil)
	if code != http.StatusOK {
		t.Fatalf("ab12cd34ef should still exist after ambiguous delete, got %d", code)
	}
	code, _ = do(t, http.MethodGet, srv.URL+"/tasks/ab12cd99ff", nil)
	if code != http.StatusOK {
		t.Fatalf("ab12cd99ff should still exist after ambiguous delete, got %d", code)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks/zz", nil)
	if code != http.StatusNotFound {
		t.Fatalf("GET /tasks/zz expected 404, got %d: %s", code, body)
	}
	if !strings.Contains(string(body), "task not found") {
		t.Fatalf("GET /tasks/zz body expected 'task not found', got %s", body)
	}
}

func TestMutatingPrefixRoutes(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "test_mutating_prefix.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	ids := []string{"prefix-single-1", "prefix-ambig-1", "prefix-ambig-2"}
	for _, id := range ids {
		code, body := post(t, srv.URL+"/tasks", map[string]string{
			"id":         id,
			"asset_path": "asset.obj",
			"project":    "proj",
		})
		if code != http.StatusCreated {
			t.Fatalf("create %s failed: %d %s", id, code, body)
		}
	}

	code, body := do(t, http.MethodPatch, srv.URL+"/tasks/prefix-ambig", map[string]string{"body": "mutated"})
	if code != http.StatusConflict {
		t.Fatalf("PATCH ambiguous prefix expected 409, got %d: %s", code, body)
	}
	if !strings.Contains(string(body), "prefix-ambig-1") || !strings.Contains(string(body), "prefix-ambig-2") {
		t.Fatalf("PATCH ambiguous prefix body expected both candidates, got: %s", body)
	}

	code, body = do(t, http.MethodPatch, srv.URL+"/tasks/prefix-single", map[string]string{"body": "updated"})
	if code != http.StatusNoContent {
		t.Fatalf("PATCH unique prefix expected 204, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/prefix-single/claim", map[string]string{"worker": "worker-a"})
	if code != http.StatusOK {
		t.Fatalf("POST claim unique prefix expected 200, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/prefix-single/touch", map[string]string{"worker": "worker-a"})
	if code != http.StatusOK {
		t.Fatalf("POST touch unique prefix expected 200, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/prefix-single/release", map[string]string{"worker": "worker-a"})
	if code != http.StatusNoContent {
		t.Fatalf("POST release unique prefix expected 204, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/prefix-single/close", nil)
	if code != http.StatusNoContent {
		t.Fatalf("POST close unique prefix expected 204, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodDelete, srv.URL+"/tasks/prefix-single?force=1", nil)
	if code != http.StatusNoContent {
		t.Fatalf("DELETE unique prefix expected 204, got %d: %s", code, body)
	}

	code, _ = do(t, http.MethodGet, srv.URL+"/tasks/prefix-single-1", nil)
	if code != http.StatusNotFound {
		t.Fatalf("expected deleted task to be 404, got %d", code)
	}
}
