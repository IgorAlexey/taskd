package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONErrors(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	client := srv.Client()

	assertJSONError := func(resp *http.Response, expectedStatus int, expectedMsg string) {
		t.Helper()
		if resp.StatusCode != expectedStatus {
			t.Fatalf("expected status %d, got %d", expectedStatus, resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Fatalf("expected Content-Type application/json, got %q", ct)
		}
		if nosniff := resp.Header.Get("X-Content-Type-Options"); nosniff != "nosniff" {
			t.Fatalf("expected X-Content-Type-Options nosniff, got %q", nosniff)
		}
		var body struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode JSON error body: %v", err)
		}
		if body.Error != expectedMsg {
			t.Fatalf("expected error %q, got %q", expectedMsg, body.Error)
		}
	}

	t.Run("not found", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/nope")
		if err != nil {
			t.Fatalf("GET /nope failed: %v", err)
		}
		defer resp.Body.Close()
		assertJSONError(resp, http.StatusNotFound, "not found")
	})

	t.Run("invalid limit", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/tasks?limit=5000")
		if err != nil {
			t.Fatalf("GET /tasks?limit=5000 failed: %v", err)
		}
		defer resp.Body.Close()
		assertJSONError(resp, http.StatusBadRequest, "invalid limit")
	})

	t.Run("method not allowed", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPut, srv.URL+"/tasks", nil)
		if err != nil {
			t.Fatalf("new request failed: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("PUT /tasks failed: %v", err)
		}
		defer resp.Body.Close()
		if allow := resp.Header.Get("Allow"); allow != "GET, HEAD, POST" {
			t.Fatalf("expected Allow: GET, HEAD, POST, got %q", allow)
		}
		assertJSONError(resp, http.StatusMethodNotAllowed, "method not allowed")
	})

	t.Run("missing asset_path or body", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/tasks", strings.NewReader("{}"))
		if err != nil {
			t.Fatalf("new request failed: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("POST /tasks failed: %v", err)
		}
		defer resp.Body.Close()
		assertJSONError(resp, http.StatusBadRequest, "missing asset_path or body")
	})

	t.Run("task not found", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/tasks/missing-task-id")
		if err != nil {
			t.Fatalf("GET /tasks/missing-task-id failed: %v", err)
		}
		defer resp.Body.Close()
		assertJSONError(resp, http.StatusNotFound, "task not found")
	})

	t.Run("method not allowed on get only route", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/projects", nil)
		if err != nil {
			t.Fatalf("new request failed: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("POST /projects failed: %v", err)
		}
		defer resp.Body.Close()
		if allow := resp.Header.Get("Allow"); allow != "GET, HEAD" {
			t.Fatalf("expected Allow: GET, HEAD, got %q", allow)
		}
		assertJSONError(resp, http.StatusMethodNotAllowed, "method not allowed")
	})

	t.Run("method not allowed precedes query validation", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPut, srv.URL+"/tasks?limit=5000", nil)
		if err != nil {
			t.Fatalf("new request failed: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("PUT /tasks?limit=5000 failed: %v", err)
		}
		defer resp.Body.Close()
		if allow := resp.Header.Get("Allow"); allow != "GET, HEAD, POST" {
			t.Fatalf("expected Allow: GET, HEAD, POST, got %q", allow)
		}
		assertJSONError(resp, http.StatusMethodNotAllowed, "method not allowed")
	})

	t.Run("request body too large", func(t *testing.T) {
		huge := []byte(`{"body":"` + strings.Repeat("a", maxBodyBytes) + `"}`)
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/tasks", bytes.NewReader(huge))
		if err != nil {
			t.Fatalf("new request failed: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("POST /tasks huge body failed: %v", err)
		}
		defer resp.Body.Close()
		assertJSONError(resp, http.StatusRequestEntityTooLarge, "request body too large")
	})
}
