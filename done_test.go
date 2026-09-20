package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestDoneClearsLeaseExpires(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]string{"body": "clear-lease", "project": "p1"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("decode created task failed: %v", err)
	}
	resp.Body.Close()

	claimBody, _ := json.Marshal(map[string]string{"worker": "w1", "project": "p1"})
	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim task failed: %v", err)
	}
	resp.Body.Close()

	doneBody, _ := json.Marshal(map[string]string{"worker": "w1"})
	resp, err = http.Post(srv.URL+"/tasks/"+created.ID+"/done", "application/json", bytes.NewReader(doneBody))
	if err != nil {
		t.Fatalf("done task failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("done status = %d, want 204", resp.StatusCode)
	}

	resp, err = http.Get(srv.URL + "/tasks?status=done&project=p1")
	if err != nil {
		t.Fatalf("get tasks failed: %v", err)
	}
	defer resp.Body.Close()

	var tasks []struct {
		ID           string `json:"id"`
		LeaseExpires int64  `json:"lease_expires"`
		Status       string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode tasks failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(tasks))
	}
	if tasks[0].ID != created.ID {
		t.Fatalf("task id = %q, want %q", tasks[0].ID, created.ID)
	}
	if tasks[0].LeaseExpires != 0 {
		t.Fatalf("lease_expires = %d, want 0", tasks[0].LeaseExpires)
	}
}

func TestDoneNullPrimitives(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]string{"body": "test-prims", "project": "p1"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("decode created task failed: %v", err)
	}
	resp.Body.Close()

	claimBody, _ := json.Marshal(map[string]string{"worker": "w1", "project": "p1"})
	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim task failed: %v", err)
	}
	resp.Body.Close()

	doneBody := []byte(`{"worker":"w1", "primitives": null}`)
	resp, err = http.Post(srv.URL+"/tasks/"+created.ID+"/done", "application/json", bytes.NewReader(doneBody))
	if err != nil {
		t.Fatalf("done task failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("done status = %d, want 204", resp.StatusCode)
	}

	var isNull bool
	var primValue any
	if err := db.ro.QueryRow("SELECT primitives IS NULL, primitives FROM tasks WHERE id = ?", created.ID).Scan(&isNull, &primValue); err != nil {
		t.Fatalf("query primitives failed: %v", err)
	}
	if !isNull || primValue != nil {
		t.Errorf("primitives column in SQLite = %v (isNull=%v), want NULL", primValue, isNull)
	}

	resp, err = http.Get(srv.URL + "/tasks/" + created.ID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	defer resp.Body.Close()

	var res map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("unmarshal task detail failed: %v", err)
	}
	val, ok := res["primitives"]
	if !ok {
		t.Fatal("expected 'primitives' key in task response")
	}
	if val != nil {
		t.Errorf("expected primitives = nil, got %v (%T)", val, val)
	}
}
