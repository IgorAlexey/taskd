package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	handler := newHandler(db, 30)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json content type, got %q", ct)
	}

	var res map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if res["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", res["status"])
	}

	reqPost := httptest.NewRequest(http.MethodPost, "/health", nil)
	recPost := httptest.NewRecorder()
	handler.ServeHTTP(recPost, reqPost)
	if recPost.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405 for POST, got %d", recPost.Code)
	}

	db.rw.Close()
	reqClosed := httptest.NewRequest(http.MethodGet, "/health", nil)
	recClosed := httptest.NewRecorder()
	handler.ServeHTTP(recClosed, reqClosed)
	if recClosed.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500 when db is closed, got %d", recClosed.Code)
	}
}

func TestHealthAllowsQueryParams(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	handler := newHandler(db, 30)

	req := httptest.NewRequest(http.MethodGet, "/health?probe=ready&t=123", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json content type, got %q", ct)
	}

	var res map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if res["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", res["status"])
	}
}
