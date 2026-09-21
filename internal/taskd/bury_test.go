package taskd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestBuryAcceptsLapsedLeaseWhileUnclaimed(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]string{"body": "bury-lapse", "project": "p1"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var created struct {
		ID int64 `json:"id"`
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
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("claim status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	if _, err := db.rw.Exec("UPDATE tasks SET lease_expires = 1 WHERE id = ?", created.ID); err != nil {
		t.Fatalf("expire lease failed: %v", err)
	}

	buryBody, _ := json.Marshal(map[string]string{"worker": "w1"})
	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/bury", srv.URL, created.ID), "application/json", bytes.NewReader(buryBody))
	if err != nil {
		t.Fatalf("bury task failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("bury status = %d, want 204", resp.StatusCode)
	}

	respGet, err := http.Get(fmt.Sprintf("%s/tasks/%d", srv.URL, created.ID))
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	defer respGet.Body.Close()

	var item struct {
		ID           int64  `json:"id"`
		Status       string `json:"status"`
		Worker       string `json:"worker"`
		LeaseExpires int64  `json:"lease_expires"`
	}
	if err := json.NewDecoder(respGet.Body).Decode(&item); err != nil {
		t.Fatalf("decode task failed: %v", err)
	}
	if item.Status != "buried" {
		t.Fatalf("status = %q, want buried", item.Status)
	}
	if item.Worker != "" {
		t.Fatalf("worker = %q, want empty", item.Worker)
	}
	if item.LeaseExpires != 0 {
		t.Fatalf("lease_expires = %d, want 0", item.LeaseExpires)
	}
}

func TestBuryAfterTakeoverNamesTheHolder(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]string{"body": "bury-takeover", "project": "p1"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("decode created failed: %v", err)
	}
	resp.Body.Close()

	claimBody, _ := json.Marshal(map[string]string{"worker": "w1", "project": "p1"})
	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("claim status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	if _, err := db.rw.Exec("UPDATE tasks SET lease_expires = 1 WHERE id = ?", created.ID); err != nil {
		t.Fatalf("expire lease failed: %v", err)
	}

	claim2Body, _ := json.Marshal(map[string]string{"worker": "w2"})
	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/claim", srv.URL, created.ID), "application/json", bytes.NewReader(claim2Body))
	if err != nil {
		t.Fatalf("claim 2 failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("claim 2 status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	buryBody, _ := json.Marshal(map[string]string{"worker": "w1"})
	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/bury", srv.URL, created.ID), "application/json", bytes.NewReader(buryBody))
	if err != nil {
		t.Fatalf("bury failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	var conflict struct {
		Error      string `json:"error"`
		Worker     string `json:"worker"`
		ClaimCount int    `json:"claim_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&conflict); err != nil {
		t.Fatalf("decode conflict failed: %v", err)
	}
	if conflict.Error != "task leased by another worker" || conflict.Worker != "w2" || conflict.ClaimCount != 2 {
		t.Fatalf("conflict = %+v, want task leased by another worker w2 claim 2", conflict)
	}
}

func TestBuryWithMatchingClaimCountOnLapsedLease(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]string{"body": "bury-count", "project": "p1"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("decode created failed: %v", err)
	}
	resp.Body.Close()

	claimBody, _ := json.Marshal(map[string]string{"worker": "w1", "project": "p1"})
	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("claim status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	if _, err := db.rw.Exec("UPDATE tasks SET lease_expires = 1 WHERE id = ?", created.ID); err != nil {
		t.Fatalf("expire lease failed: %v", err)
	}

	claimCount := 1
	buryReq := struct {
		Worker     string `json:"worker"`
		ClaimCount *int   `json:"claim_count"`
	}{
		Worker:     "w1",
		ClaimCount: &claimCount,
	}
	buryBody, _ := json.Marshal(buryReq)
	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/bury", srv.URL, created.ID), "application/json", bytes.NewReader(buryBody))
	if err != nil {
		t.Fatalf("bury task failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("bury status = %d, want 204", resp.StatusCode)
	}
}

func TestBuryFromSupersededGenerationIsRefused(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]string{"body": "bury-superseded", "project": "p1"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("decode created failed: %v", err)
	}
	resp.Body.Close()

	claimBody, _ := json.Marshal(map[string]string{"worker": "w1", "project": "p1"})
	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("claim status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	if _, err := db.rw.Exec("UPDATE tasks SET lease_expires = 1 WHERE id = ?", created.ID); err != nil {
		t.Fatalf("expire lease failed: %v", err)
	}

	claim2Body, _ := json.Marshal(map[string]string{"worker": "w1"})
	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/claim", srv.URL, created.ID), "application/json", bytes.NewReader(claim2Body))
	if err != nil {
		t.Fatalf("claim 2 failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("claim 2 status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	claimCount := 1
	buryReq := struct {
		Worker     string `json:"worker"`
		ClaimCount *int   `json:"claim_count"`
	}{
		Worker:     "w1",
		ClaimCount: &claimCount,
	}
	buryBody, _ := json.Marshal(buryReq)
	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/bury", srv.URL, created.ID), "application/json", bytes.NewReader(buryBody))
	if err != nil {
		t.Fatalf("bury failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	var conflict struct {
		Error      string `json:"error"`
		Worker     string `json:"worker"`
		ClaimCount int    `json:"claim_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&conflict); err != nil {
		t.Fatalf("decode conflict failed: %v", err)
	}
	if conflict.Error != "task claimed again" || conflict.Worker != "w1" || conflict.ClaimCount != 2 {
		t.Fatalf("conflict = %+v, want task claimed again w1 claim 2", conflict)
	}
}

func TestBuryPrimitives(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]string{"body": "t", "project": "p"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("decode created task failed: %v", err)
	}
	resp.Body.Close()

	claimBody, _ := json.Marshal(map[string]string{"worker": "w1", "project": "p"})
	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim task failed: %v", err)
	}
	resp.Body.Close()

	buryBody, _ := json.Marshal(map[string]any{
		"worker":     "w1",
		"primitives": map[string]string{"error": "compiler crash"},
	})
	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/bury", srv.URL, created.ID), "application/json", bytes.NewReader(buryBody))
	if err != nil {
		t.Fatalf("bury task failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("bury status = %d, want 204", resp.StatusCode)
	}

	resp, err = http.Get(fmt.Sprintf("%s/tasks/%d", srv.URL, created.ID))
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	var item struct {
		Status     string          `json:"status"`
		Primitives json.RawMessage `json:"primitives"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		resp.Body.Close()
		t.Fatalf("decode task failed: %v", err)
	}
	resp.Body.Close()
	if item.Status != "buried" {
		t.Fatalf("status = %q, want buried", item.Status)
	}
	var errPrim struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(item.Primitives, &errPrim); err != nil || errPrim.Error != "compiler crash" {
		t.Fatalf("primitives = %s, want compiler crash", string(item.Primitives))
	}

	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/kick", srv.URL, created.ID), "application/json", nil)
	if err != nil {
		t.Fatalf("kick task failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("kick status = %d, want 204", resp.StatusCode)
	}

	resp, err = http.Get(fmt.Sprintf("%s/tasks/%d", srv.URL, created.ID))
	if err != nil {
		t.Fatalf("get kicked task failed: %v", err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		resp.Body.Close()
		t.Fatalf("decode kicked task failed: %v", err)
	}
	resp.Body.Close()
	if item.Status != "pending" {
		t.Fatalf("status = %q, want pending", item.Status)
	}
	if len(item.Primitives) > 0 && string(item.Primitives) != "null" {
		t.Fatalf("kicked primitives = %s, want null", string(item.Primitives))
	}

	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("reclaim task failed: %v", err)
	}
	resp.Body.Close()

	buryNoPrim, _ := json.Marshal(map[string]string{"worker": "w1"})
	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/bury", srv.URL, created.ID), "application/json", bytes.NewReader(buryNoPrim))
	if err != nil {
		t.Fatalf("bury without primitives failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("bury status = %d, want 204", resp.StatusCode)
	}

	resp, err = http.Get(fmt.Sprintf("%s/tasks/%d", srv.URL, created.ID))
	if err != nil {
		t.Fatalf("get reburied task failed: %v", err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		resp.Body.Close()
		t.Fatalf("decode reburied task failed: %v", err)
	}
	resp.Body.Close()
	if item.Status != "buried" {
		t.Fatalf("status = %q, want buried", item.Status)
	}
	if len(item.Primitives) > 0 && string(item.Primitives) != "null" {
		t.Fatalf("reburied primitives = %s, want null", string(item.Primitives))
	}
}
