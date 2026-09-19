package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func touchFixture(t *testing.T, lease int) (*sql.DB, *httptest.Server, string) {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	srv := httptest.NewServer(newHandler(db, lease))
	t.Cleanup(srv.Close)

	code, body := post(t, srv.URL+"/tasks", map[string]any{
		"body":    "touch test",
		"project": "p1",
	})
	if code != http.StatusCreated {
		t.Fatalf("create task expected 201, got %d: %s", code, body)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal created failed: %v", err)
	}
	return db, srv, created.ID
}

func TestTouchTask(t *testing.T) {
	db, srv, taskID := touchFixture(t, 300)

	code, body := post(t, srv.URL+"/tasks/"+taskID+"/touch", map[string]any{
		"worker": "w1",
	})
	if code != http.StatusConflict {
		t.Fatalf("touch unleased task expected 409, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/claim", map[string]any{
		"worker":  "w1",
		"project": "p1",
	})
	if code != http.StatusOK {
		t.Fatalf("claim expected 200, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/"+taskID+"/touch", map[string]any{
		"worker": "w2",
	})
	if code != http.StatusConflict {
		t.Fatalf("touch with wrong worker expected 409, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/"+taskID+"/touch", map[string]any{
		"worker": "w1",
	})
	if code != http.StatusOK {
		t.Fatalf("touch with correct worker expected 200, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/"+taskID+"/touch", map[string]any{})
	if code != http.StatusBadRequest {
		t.Fatalf("touch with missing worker expected 400, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/"+taskID+"/touch", map[string]any{
		"worker":  "w1",
		"unknown": "value",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("touch with unknown field expected 400, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/nonexistent/touch", map[string]any{
		"worker": "w1",
	})
	if code != http.StatusNotFound {
		t.Fatalf("touch nonexistent expected 404, got %d: %s", code, body)
	}

	if _, err := db.Exec("UPDATE tasks SET lease_expires = unixepoch() - 10 WHERE id = ?", taskID); err != nil {
		t.Fatalf("update expired failed: %v", err)
	}
	code, body = post(t, srv.URL+"/tasks/"+taskID+"/touch", map[string]any{
		"worker": "w1",
	})
	if code != http.StatusConflict {
		t.Fatalf("touch expired task expected 409, got %d: %s", code, body)
	}
}

func TestTouchReturnsLeaseEnvelope(t *testing.T) {
	db, srv, taskID := touchFixture(t, 300)

	code, body := post(t, srv.URL+"/tasks/"+taskID+"/claim", map[string]any{
		"worker": "w1",
	})
	if code != http.StatusOK {
		t.Fatalf("claim expected 200, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/"+taskID+"/touch", map[string]any{
		"worker": "w1",
	})
	if code != http.StatusOK {
		t.Fatalf("touch expected 200, got %d: %s", code, body)
	}
	var env struct {
		ID           string `json:"id"`
		LeaseExpires int64  `json:"lease_expires"`
		Status       string `json:"status"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal touch envelope failed: %v (%s)", err, body)
	}
	if env.ID != taskID {
		t.Fatalf("touch id = %q, want %q", env.ID, taskID)
	}
	if env.Status != "leased" {
		t.Fatalf("touch status = %q, want leased", env.Status)
	}
	now := time.Now().Unix()
	if env.LeaseExpires < now+295 || env.LeaseExpires > now+305 {
		t.Fatalf("touch lease_expires = %d, want near %d", env.LeaseExpires, now+300)
	}

	var stored int64
	if err := db.QueryRow("SELECT lease_expires FROM tasks WHERE id = ?", taskID).Scan(&stored); err != nil {
		t.Fatalf("query lease_expires failed: %v", err)
	}
	if stored != env.LeaseExpires {
		t.Fatalf("stored lease_expires = %d, response = %d", stored, env.LeaseExpires)
	}
}

func TestTouchPrefixIDReturnsFullID(t *testing.T) {
	_, srv, taskID := touchFixture(t, 300)

	code, body := post(t, srv.URL+"/tasks/"+taskID+"/claim", map[string]any{
		"worker": "w1",
	})
	if code != http.StatusOK {
		t.Fatalf("claim expected 200, got %d: %s", code, body)
	}
	if len(taskID) < 8 {
		t.Fatalf("task id %q too short to take a prefix of", taskID)
	}

	code, body = post(t, srv.URL+"/tasks/"+taskID[:8]+"/touch", map[string]any{
		"worker": "w1",
	})
	if code != http.StatusOK {
		t.Fatalf("touch by prefix expected 200, got %d: %s", code, body)
	}
	var env struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal touch envelope failed: %v (%s)", err, body)
	}
	if env.ID != taskID {
		t.Fatalf("touch id = %q, want full id %q", env.ID, taskID)
	}
}
