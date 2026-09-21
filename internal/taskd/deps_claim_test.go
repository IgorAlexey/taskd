package taskd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestDepsClaimGate(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	// A pending, priority 3
	postA := `{"project":"p","body":"task A","priority":3}`
	respA, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postA))
	var resA struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respA.Body).Decode(&resA)
	respA.Body.Close()

	// B after A, priority 1
	postB := fmt.Sprintf(`{"project":"p","body":"task B","priority":1,"after":[%d]}`, resA.ID)
	respB, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postB))
	var resB struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respB.Body).Decode(&resB)
	respB.Body.Close()

	// POST /tasks/claim returns A not B
	claimBody := `{"worker":"w1","project":"p"}`
	respClaim, err := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewBufferString(claimBody))
	if err != nil {
		t.Fatalf("POST claim failed: %v", err)
	}
	defer respClaim.Body.Close()
	if respClaim.StatusCode != http.StatusOK {
		t.Fatalf("POST claim expected 200, got %d", respClaim.StatusCode)
	}
	var claimedA taskItem
	json.NewDecoder(respClaim.Body).Decode(&claimedA)
	if claimedA.ID != resA.ID {
		t.Fatalf("claim expected task A (%d), got task %d", resA.ID, claimedA.ID)
	}

	// per-id claim of B -> 409
	claimBBody := `{"worker":"w1"}`
	respClaimB, err := http.Post(fmt.Sprintf("%s/tasks/%d/claim", srv.URL, resB.ID), "application/json", bytes.NewBufferString(claimBBody))
	if err != nil {
		t.Fatalf("POST claim B failed: %v", err)
	}
	defer respClaimB.Body.Close()
	if respClaimB.StatusCode != http.StatusConflict {
		t.Fatalf("per-id claim of blocked task B expected 409, got %d", respClaimB.StatusCode)
	}
	var errClaimB map[string]string
	json.NewDecoder(respClaimB.Body).Decode(&errClaimB)
	if errClaimB["error"] != "task is waiting on other tasks" {
		t.Fatalf("expected 'task is waiting on other tasks', got %q", errClaimB["error"])
	}

	// done A
	doneBody := `{"worker":"w1"}`
	respDone, err := http.Post(fmt.Sprintf("%s/tasks/%d/done", srv.URL, resA.ID), "application/json", bytes.NewBufferString(doneBody))
	if err != nil {
		t.Fatalf("POST done A failed: %v", err)
	}
	defer respDone.Body.Close()
	if respDone.StatusCode != http.StatusNoContent {
		t.Fatalf("POST done A expected 204, got %d", respDone.StatusCode)
	}

	// claim now returns B
	respClaim2, err := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewBufferString(claimBody))
	if err != nil {
		t.Fatalf("POST claim 2 failed: %v", err)
	}
	defer respClaim2.Body.Close()
	if respClaim2.StatusCode != http.StatusOK {
		t.Fatalf("POST claim 2 expected 200, got %d", respClaim2.StatusCode)
	}
	var claimedB taskItem
	json.NewDecoder(respClaim2.Body).Decode(&claimedB)
	if claimedB.ID != resB.ID {
		t.Fatalf("claim 2 expected task B (%d), got task %d", resB.ID, claimedB.ID)
	}
}

func TestDepsWakeWaiter(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	// Task A on project pA
	postA := `{"project":"pA","body":"task A"}`
	respA, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postA))
	var resA struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respA.Body).Decode(&resA)
	respA.Body.Close()

	// Task B on project pB, waiting on task A
	postB := fmt.Sprintf(`{"project":"pB","body":"task B","after":[%d]}`, resA.ID)
	respB, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postB))
	var resB struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respB.Body).Decode(&resB)
	respB.Body.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		status int
		taskID int64
		err    error
	}
	ch := make(chan result, 1)

	go func() {
		claimBody, _ := json.Marshal(map[string]any{"worker": "w-pB", "project": "pB", "wait": 5})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/tasks/claim", bytes.NewReader(claimBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer resp.Body.Close()
		var item taskItem
		if resp.StatusCode == http.StatusOK {
			_ = json.NewDecoder(resp.Body).Decode(&item)
		}
		ch <- result{status: resp.StatusCode, taskID: item.ID}
	}()

	for db.waiterCount() == 0 {
		time.Sleep(time.Millisecond)
	}

	// Claim A on project pA
	claimABody := `{"worker":"w-pA","project":"pA"}`
	respClaimA, err := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewBufferString(claimABody))
	if err != nil || respClaimA.StatusCode != http.StatusOK {
		t.Fatalf("claim A failed: %v, status %d", err, respClaimA.StatusCode)
	}
	respClaimA.Body.Close()

	// Done A
	doneABody := `{"worker":"w-pA"}`
	respDoneA, err := http.Post(fmt.Sprintf("%s/tasks/%d/done", srv.URL, resA.ID), "application/json", bytes.NewBufferString(doneABody))
	if err != nil || respDoneA.StatusCode != http.StatusNoContent {
		t.Fatalf("done A failed: %v, status %d", err, respDoneA.StatusCode)
	}
	respDoneA.Body.Close()

	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("waiting claim error: %v", res.err)
		}
		if res.status != http.StatusOK {
			t.Fatalf("waiting claim status = %d, want 200", res.status)
		}
		if res.taskID != resB.ID {
			t.Fatalf("claimed task = %d, want %d", res.taskID, resB.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiting claim on project pB did not unblock in time after dependency completed")
	}
}

func TestDepsDeleteWakesWaiter(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	// Task A on project pA
	postA := `{"project":"pA","body":"task A"}`
	respA, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postA))
	var resA struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respA.Body).Decode(&resA)
	respA.Body.Close()

	// Task B on project pB, waiting on task A
	postB := fmt.Sprintf(`{"project":"pB","body":"task B","after":[%d]}`, resA.ID)
	respB, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postB))
	var resB struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respB.Body).Decode(&resB)
	respB.Body.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		status int
		taskID int64
		err    error
	}
	ch := make(chan result, 1)

	go func() {
		claimBody, _ := json.Marshal(map[string]any{"worker": "w-pB", "project": "pB", "wait": 5})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/tasks/claim", bytes.NewReader(claimBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer resp.Body.Close()
		var item taskItem
		if resp.StatusCode == http.StatusOK {
			_ = json.NewDecoder(resp.Body).Decode(&item)
		}
		ch <- result{status: resp.StatusCode, taskID: item.ID}
	}()

	for db.waiterCount() == 0 {
		time.Sleep(time.Millisecond)
	}

	// DELETE A
	delReq, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/tasks/%d", srv.URL, resA.ID), nil)
	respDel, err := http.DefaultClient.Do(delReq)
	if err != nil || respDel.StatusCode != http.StatusNoContent {
		t.Fatalf("delete A failed: %v, status %d", err, respDel.StatusCode)
	}
	respDel.Body.Close()

	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("waiting claim error: %v", res.err)
		}
		if res.status != http.StatusOK {
			t.Fatalf("waiting claim status = %d, want 200", res.status)
		}
		if res.taskID != resB.ID {
			t.Fatalf("claimed task = %d, want %d", res.taskID, resB.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiting claim on project pB did not unblock in time after dependency deleted")
	}
}
