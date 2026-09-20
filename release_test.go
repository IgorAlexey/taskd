package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestReleaseLapsedLease(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]string{"body": "release-lapse", "project": "p1"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var created1 map[string]int64
	json.NewDecoder(resp.Body).Decode(&created1)
	resp.Body.Close()
	taskID1 := created1["id"]
	taskPath1 := fmt.Sprintf("/tasks/%d", taskID1)
	claimBody, _ := json.Marshal(map[string]string{"worker": "w1", "project": "p1"})
	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("claim status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	if _, err := db.rw.Exec("UPDATE tasks SET lease_expires = unixepoch() - 10 WHERE id = ?", taskID1); err != nil {
		t.Fatalf("expire lease failed: %v", err)
	}

	releaseBody, _ := json.Marshal(map[string]string{"worker": "w1"})
	resp, err = http.Post(srv.URL+taskPath1+"/release", "application/json", bytes.NewReader(releaseBody))
	if err != nil {
		t.Fatalf("release failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("release status = %d, want 204", resp.StatusCode)
	}

	respGet, err := http.Get(srv.URL + taskPath1)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	defer respGet.Body.Close()
	var item taskItem
	if err := json.NewDecoder(respGet.Body).Decode(&item); err != nil {
		t.Fatalf("decode task failed: %v", err)
	}
	if item.Status != "pending" {
		t.Fatalf("status = %q, want pending", item.Status)
	}
	if item.Worker != "" {
		t.Fatalf("worker = %q, want empty", item.Worker)
	}
	if item.LeaseExpires != 0 {
		t.Fatalf("lease_expires = %d, want 0", item.LeaseExpires)
	}
}

func TestReleaseLapsedLeaseWrongWorkerFails(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]string{"body": "release-wrong-worker", "project": "p1"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var created2 map[string]int64
	json.NewDecoder(resp.Body).Decode(&created2)
	resp.Body.Close()
	taskID2 := created2["id"]
	taskPath2 := fmt.Sprintf("/tasks/%d", taskID2)
	claimBody, _ := json.Marshal(map[string]string{"worker": "w1", "project": "p1"})
	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	resp.Body.Close()

	if _, err := db.rw.Exec("UPDATE tasks SET lease_expires = unixepoch() - 10 WHERE id = ?", taskID2); err != nil {
		t.Fatalf("expire lease failed: %v", err)
	}

	releaseBody, _ := json.Marshal(map[string]string{"worker": "w2"})
	resp, err = http.Post(srv.URL+taskPath2+"/release", "application/json", bytes.NewReader(releaseBody))
	if err != nil {
		t.Fatalf("release failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("release status = %d, want 409", resp.StatusCode)
	}
}
