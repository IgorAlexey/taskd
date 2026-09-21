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

func TestClaimWaitReceivesReleasedTask(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	createBody, _ := json.Marshal(map[string]string{"body": "releaseme", "project": "p1"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	claimBody, _ := json.Marshal(map[string]any{"worker": "holder", "project": "p1"})
	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	resp.Body.Close()

	type result struct {
		status int
		body   string
		err    error
	}
	otherCh := make(chan result, 1)
	p1Ch := make(chan result, 1)

	go func() {
		waitBody, _ := json.Marshal(map[string]any{"worker": "other-waiter", "project": "other", "wait": 5})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/tasks/claim", bytes.NewReader(waitBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			otherCh <- result{err: err}
			return
		}
		defer resp.Body.Close()
		otherCh <- result{status: resp.StatusCode}
	}()

	go func() {
		waitBody, _ := json.Marshal(map[string]any{"worker": "p1-waiter", "project": "p1", "wait": 5})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/tasks/claim", bytes.NewReader(waitBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			p1Ch <- result{err: err}
			return
		}
		defer resp.Body.Close()
		var item struct {
			Body string `json:"body"`
		}
		if resp.StatusCode == http.StatusOK {
			_ = json.NewDecoder(resp.Body).Decode(&item)
		}
		p1Ch <- result{status: resp.StatusCode, body: item.Body}
	}()

	for db.waiterCount() < 2 {
		time.Sleep(time.Millisecond)
	}

	relBody, _ := json.Marshal(map[string]string{"worker": "holder"})
	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/release", srv.URL, created.ID), "application/json", bytes.NewReader(relBody))
	if err != nil {
		t.Fatalf("release failed: %v", err)
	}
	resp.Body.Close()

	select {
	case res := <-p1Ch:
		if res.err != nil {
			t.Fatalf("claim error: %v", res.err)
		}
		if res.status != http.StatusOK {
			t.Fatalf("claim status = %d, want 200", res.status)
		}
		if res.body != "releaseme" {
			t.Fatalf("body = %q, want releaseme", res.body)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("p1 waiter did not unblock in time after release")
	}

	select {
	case other := <-otherCh:
		t.Fatalf("other waiter unexpectedly unblocked with status %d", other.status)
	case <-time.After(50 * time.Millisecond):
	}

	if count := db.waiterCount(); count != 1 {
		t.Fatalf("expected other waiter still waiting, got %d", count)
	}
}

func TestClaimWaitReceivesSweptTask(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	createBody, _ := json.Marshal(map[string]string{"body": "swept-task", "project": "sweep-proj"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	claimBody, _ := json.Marshal(map[string]any{"worker": "old-worker", "project": "sweep-proj"})
	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	resp.Body.Close()

	if _, err := db.rw.Exec("UPDATE tasks SET lease_expires = 1 WHERE id = ?", created.ID); err != nil {
		t.Fatalf("expire lease failed: %v", err)
	}

	type result struct {
		status int
		body   string
		err    error
	}
	ch := make(chan result, 1)

	go func() {
		waitBody, _ := json.Marshal(map[string]any{"worker": "new-worker", "project": "sweep-proj", "wait": 5})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/tasks/claim", bytes.NewReader(waitBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer resp.Body.Close()
		var item struct {
			Body string `json:"body"`
		}
		if resp.StatusCode == http.StatusOK {
			_ = json.NewDecoder(resp.Body).Decode(&item)
		}
		ch <- result{status: resp.StatusCode, body: item.Body}
	}()

	for db.waiterCount() == 0 {
		time.Sleep(time.Millisecond)
	}

	projects, err := db.sweep()
	if err != nil {
		t.Fatalf("sweep failed: %v", err)
	}
	for _, p := range projects {
		db.notifyPending(p)
	}

	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("claim error: %v", res.err)
		}
		if res.status != http.StatusOK {
			t.Fatalf("claim status = %d, want 200", res.status)
		}
		if res.body != "swept-task" {
			t.Fatalf("body = %q, want swept-task", res.body)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("new worker did not unblock in time after lease sweep")
	}
}

func TestClaimWaitReceivesKickedTask(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	createBody, _ := json.Marshal(map[string]string{"body": "kickme", "project": "p1"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	claimBody, _ := json.Marshal(map[string]any{"worker": "w1", "project": "p1"})
	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	resp.Body.Close()

	buryBody, _ := json.Marshal(map[string]string{"worker": "w1"})
	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/bury", srv.URL, created.ID), "application/json", bytes.NewReader(buryBody))
	if err != nil {
		t.Fatalf("bury failed: %v", err)
	}
	resp.Body.Close()

	type result struct {
		status int
		body   string
		err    error
	}
	otherCh := make(chan result, 1)
	p1Ch := make(chan result, 1)

	go func() {
		waitBody, _ := json.Marshal(map[string]any{"worker": "other-waiter", "project": "other", "wait": 5})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/tasks/claim", bytes.NewReader(waitBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			otherCh <- result{err: err}
			return
		}
		defer resp.Body.Close()
		otherCh <- result{status: resp.StatusCode}
	}()

	go func() {
		waitBody, _ := json.Marshal(map[string]any{"worker": "p1-waiter", "project": "p1", "wait": 5})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/tasks/claim", bytes.NewReader(waitBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			p1Ch <- result{err: err}
			return
		}
		defer resp.Body.Close()
		var item struct {
			Body string `json:"body"`
		}
		if resp.StatusCode == http.StatusOK {
			_ = json.NewDecoder(resp.Body).Decode(&item)
		}
		p1Ch <- result{status: resp.StatusCode, body: item.Body}
	}()

	for db.waiterCount() < 2 {
		time.Sleep(time.Millisecond)
	}

	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/kick", srv.URL, created.ID), "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatalf("kick failed: %v", err)
	}
	resp.Body.Close()

	select {
	case res := <-p1Ch:
		if res.err != nil {
			t.Fatalf("claim error: %v", res.err)
		}
		if res.status != http.StatusOK {
			t.Fatalf("claim status = %d, want 200", res.status)
		}
		if res.body != "kickme" {
			t.Fatalf("body = %q, want kickme", res.body)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("p1 waiter did not unblock in time after kick")
	}

	select {
	case other := <-otherCh:
		t.Fatalf("other waiter unexpectedly unblocked with status %d", other.status)
	case <-time.After(50 * time.Millisecond):
	}

	if count := db.waiterCount(); count != 1 {
		t.Fatalf("expected other waiter still waiting, got %d", count)
	}
}

func TestClaimWaitReceivesPatchedProjectTask(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	createBody, _ := json.Marshal(map[string]string{"body": "task-patch", "project": "projA"})
	resp, err := srv.Client().Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created task failed: %v", err)
	}

	type result struct {
		status  int
		body    string
		elapsed time.Duration
		err     error
	}
	ch := make(chan result, 1)

	go func() {
		claimBody, _ := json.Marshal(map[string]any{"worker": "w-projb", "project": "projB", "wait": 5})
		claimReq, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/tasks/claim", bytes.NewReader(claimBody))
		if err != nil {
			ch <- result{err: err}
			return
		}
		claimReq.Header.Set("Content-Type", "application/json")
		start := time.Now()
		resp, err := srv.Client().Do(claimReq)
		elapsed := time.Since(start)
		if err != nil {
			ch <- result{err: err, elapsed: elapsed}
			return
		}
		defer resp.Body.Close()
		var item struct {
			Body string `json:"body"`
		}
		if resp.StatusCode == http.StatusOK {
			_ = json.NewDecoder(resp.Body).Decode(&item)
		}
		ch <- result{status: resp.StatusCode, body: item.Body, elapsed: elapsed}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for db.waiterCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("waiter did not register in time")
		}
		time.Sleep(time.Millisecond)
	}

	patchBody, _ := json.Marshal(map[string]string{"project": "projB"})
	patchReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, fmt.Sprintf("%s/tasks/%d", srv.URL, created.ID), bytes.NewReader(patchBody))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	patchReq.Header.Set("Content-Type", "application/json")
	patchResp, err := srv.Client().Do(patchReq)
	if err != nil {
		t.Fatalf("patch request failed: %v", err)
	}
	patchResp.Body.Close()
	if patchResp.StatusCode != http.StatusNoContent {
		t.Fatalf("patch status = %d, want 204", patchResp.StatusCode)
	}

	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("claim failed: %v", res.err)
		}
		if res.status != http.StatusOK {
			t.Fatalf("claim status = %d, want 200", res.status)
		}
		if res.body != "task-patch" {
			t.Fatalf("claim body = %q, want %q", res.body, "task-patch")
		}
		if res.elapsed > 2*time.Second {
			t.Fatalf("claim took %v, want immediate wakeup without waiting for timeout", res.elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker was not woken up after task reassigned to project B")
	}
}
