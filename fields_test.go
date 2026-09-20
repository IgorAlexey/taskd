package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestGetTaskFields(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createPayload := `{"id":"task-f1","project":"proj1","body":"line 1 summary\nline 2 details"}`
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(createPayload))
	if err != nil {
		t.Fatalf("POST /tasks failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /tasks status %d", resp.StatusCode)
	}

	claimPayload := `{"worker":"w1","project":"proj1"}`
	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewBufferString(claimPayload))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("claim status %d", resp.StatusCode)
	}

	resp, err = http.Get(srv.URL + "/tasks/task-f1?fields=id,status")
	if err != nil {
		t.Fatalf("GET /tasks/task-f1?fields=id,status failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
	}

	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode JSON failed: %v", err)
	}

	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	expectedKeys := []string{"id", "status"}
	if !reflect.DeepEqual(keys, expectedKeys) {
		t.Fatalf("expected keys %v, got %v", expectedKeys, keys)
	}
	if m["id"] != "task-f1" {
		t.Fatalf("expected id task-f1, got %v", m["id"])
	}
	if m["status"] != "leased" {
		t.Fatalf("expected status leased, got %v", m["status"])
	}

	resp, err = http.Get(srv.URL + "/tasks/task-f1?columns=id,summary")
	if err != nil {
		t.Fatalf("GET with columns failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for columns alias, got %d", resp.StatusCode)
	}
	var mSummary map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&mSummary); err != nil {
		t.Fatalf("decode summary JSON failed: %v", err)
	}
	if mSummary["summary"] != "line 1 summary" {
		t.Fatalf("expected summary 'line 1 summary', got %v", mSummary["summary"])
	}
	if len(mSummary) != 2 || mSummary["id"] != "task-f1" {
		t.Fatalf("unexpected keys for columns alias: %v", mSummary)
	}
	badFieldURLs := []string{
		srv.URL + "/tasks/task-f1?fields=id,invalid_field",
		srv.URL + "/tasks/task-f1?fields=unknown",
		srv.URL + "/tasks/task-f1?fields=notes",
		srv.URL + "/tasks/task-f1?fields=",
		srv.URL + "/tasks/task-f1?fields=   ",
		srv.URL + "/tasks/task-f1?columns=invalid",
	}
	for _, badURL := range badFieldURLs {
		badResp, err := http.Get(badURL)
		if err != nil {
			t.Fatalf("GET %s failed: %v", badURL, err)
		}
		badResp.Body.Close()
		if badResp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s, got %d", badURL, badResp.StatusCode)
		}
	}

	respUnknown, err := http.Get(srv.URL + "/tasks/task-f1?unknown=1")
	if err != nil {
		t.Fatalf("GET unknown param failed: %v", err)
	}
	respUnknown.Body.Close()
	if respUnknown.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown param, got %d", respUnknown.StatusCode)
	}

	resp404, err := http.Get(srv.URL + "/tasks/no-such-task?fields=id,status")
	if err != nil {
		t.Fatalf("GET 404 failed: %v", err)
	}
	resp404.Body.Close()
	if resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing task, got %d", resp404.StatusCode)
	}
}
