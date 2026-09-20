package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestClaimHonorsMaxClaimsAfterRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")

	db1, err := openDB(dbPath, 0)
	if err != nil {
		t.Fatalf("openDB 1 failed: %v", err)
	}
	srv1 := httptest.NewServer(newHandler(db1, 300))

	createBody, _ := json.Marshal(map[string]string{"id": "t1", "body": "work", "project": "p"})
	resp, err := http.Post(srv1.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	resp.Body.Close()

	claimBody, _ := json.Marshal(map[string]string{"worker": "w1", "project": "p"})
	resp, err = http.Post(srv1.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("first claim failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first claim status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	resp.Body.Close()

	if _, err := db1.rw.Exec("UPDATE tasks SET lease_expires = unixepoch() - 1 WHERE id = 't1'"); err != nil {
		t.Fatalf("expire first lease failed: %v", err)
	}
	if _, err := db1.sweep(); err != nil {
		t.Fatalf("sweep 1 failed: %v", err)
	}

	resp, err = http.Post(srv1.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("second claim failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second claim status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	resp.Body.Close()

	if _, err := db1.rw.Exec("UPDATE tasks SET lease_expires = unixepoch() - 1 WHERE id = 't1'"); err != nil {
		t.Fatalf("expire second lease failed: %v", err)
	}
	if _, err := db1.sweep(); err != nil {
		t.Fatalf("sweep 2 failed: %v", err)
	}

	srv1.Close()
	db1.Close()

	db2, err := openDB(dbPath, 2)
	if err != nil {
		t.Fatalf("openDB 2 failed: %v", err)
	}
	defer db2.Close()
	srv2 := httptest.NewServer(newHandler(db2, 300))
	defer srv2.Close()

	resp, err = http.Post(srv2.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("third claim failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("claim at max-claims limit status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	respGet, err := http.Get(srv2.URL + "/tasks/t1")
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	defer respGet.Body.Close()
	var item taskItem
	if err := json.NewDecoder(respGet.Body).Decode(&item); err != nil {
		t.Fatalf("decode task failed: %v", err)
	}
	if item.Status != "buried" {
		t.Fatalf("task status = %q, want buried", item.Status)
	}
}

func TestClaimBelowMaxClaimsSucceeds(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 2)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]string{"id": "t1", "body": "work", "project": "p"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	resp.Body.Close()

	claimBody, _ := json.Marshal(map[string]string{"worker": "w1", "project": "p"})
	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("claim status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var claimed taskItem
	if err := json.NewDecoder(resp.Body).Decode(&claimed); err != nil {
		t.Fatalf("decode claimed failed: %v", err)
	}
	if claimed.ClaimCount != 1 {
		t.Fatalf("claim_count = %d, want 1", claimed.ClaimCount)
	}
}
