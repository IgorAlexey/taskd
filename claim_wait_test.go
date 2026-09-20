package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestClaimWaitImmediateWhenTaskAvailable(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]string{"body": "now", "project": "w"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	resp.Body.Close()

	start := time.Now()
	claimBody, _ := json.Marshal(map[string]any{"worker": "w1", "project": "w", "wait": 5})
	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("claim took %v, want immediate", elapsed)
	}

	var item struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if item.Body != "now" {
		t.Fatalf("body = %q, want now", item.Body)
	}
}

func TestClaimWaitTimeoutReturns204(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	start := time.Now()
	claimBody, _ := json.Marshal(map[string]any{"worker": "w1", "project": "w", "wait": 0.02})
	resp, err := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Fatalf("claim finished in %v, want >= 15ms", elapsed)
	}
}

func TestClaimWaitInvalidWaitReturns400(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	claimBody, _ := json.Marshal(map[string]any{"worker": "w1", "project": "w", "wait": -1})
	resp, err := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestClaimWaitZeroReturnsImmediately(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	start := time.Now()
	claimBody, _ := json.Marshal(map[string]any{"worker": "w1", "project": "w", "wait": 0})
	resp, err := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("claim took %v, want immediate", elapsed)
	}
}

func TestClaimWaitOmittedReturnsImmediately(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	start := time.Now()
	claimBody, _ := json.Marshal(map[string]any{"worker": "w1", "project": "w"})
	resp, err := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("claim took %v, want immediate", elapsed)
	}
}

func TestClaimWaitReceivesEnqueuedTask(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		status int
		body   string
		err    error
	}
	ch := make(chan result, 1)

	go func() {
		claimBody, _ := json.Marshal(map[string]any{"worker": "w1", "project": "w", "wait": 5})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/tasks/claim", bytes.NewReader(claimBody))
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

	createBody, _ := json.Marshal(map[string]string{"body": "enqueued-later", "project": "w"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	resp.Body.Close()

	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("claim error: %v", res.err)
		}
		if res.status != http.StatusOK {
			t.Fatalf("claim status = %d, want 200", res.status)
		}
		if res.body != "enqueued-later" {
			t.Fatalf("body = %q, want enqueued-later", res.body)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("waiting claim did not unblock in time")
	}
}

func TestClaimWaitProjectFilter(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		status int
		body   string
		err    error
	}
	ch := make(chan result, 1)

	go func() {
		claimBody, _ := json.Marshal(map[string]any{"worker": "w1", "project": "p1", "wait": 5})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/tasks/claim", bytes.NewReader(claimBody))
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

	createP2, _ := json.Marshal(map[string]string{"body": "for-p2", "project": "p2"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createP2))
	if err != nil {
		t.Fatalf("create p2 task failed: %v", err)
	}
	resp.Body.Close()

	select {
	case <-ch:
		t.Fatal("waiter on p1 should not have claimed p2 task")
	case <-time.After(50 * time.Millisecond):
	}

	createP1, _ := json.Marshal(map[string]string{"body": "for-p1", "project": "p1"})
	resp, err = http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createP1))
	if err != nil {
		t.Fatalf("create p1 task failed: %v", err)
	}
	resp.Body.Close()

	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("claim error: %v", res.err)
		}
		if res.status != http.StatusOK {
			t.Fatalf("claim status = %d, want 200", res.status)
		}
		if res.body != "for-p1" {
			t.Fatalf("body = %q, want for-p1", res.body)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("waiting claim did not unblock in time for p1 task")
	}
}

func TestClaimWaitTargetedWakeupNoThunderingHerd(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	doneCh := make(chan string, 2)

	startWaiter := func(name string) {
		go func() {
			claimBody, _ := json.Marshal(map[string]any{"worker": name, "project": "herd", "wait": 5})
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/tasks/claim", bytes.NewReader(claimBody))
			req.Header.Set("Content-Type", "application/json")
			resp, err := srv.Client().Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var item struct {
					Body string `json:"body"`
				}
				_ = json.NewDecoder(resp.Body).Decode(&item)
				doneCh <- item.Body
			}
		}()
	}

	startWaiter("w1")
	startWaiter("w2")

	for db.waiterCount() < 2 {
		time.Sleep(time.Millisecond)
	}

	createBody, _ := json.Marshal(map[string]string{"body": "single-job", "project": "herd"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	resp.Body.Close()

	select {
	case body := <-doneCh:
		if body != "single-job" {
			t.Fatalf("got body %q, want single-job", body)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("no waiter was woken")
	}

	select {
	case second := <-doneCh:
		t.Fatalf("second waiter was spuriously woken with %q", second)
	case <-time.After(50 * time.Millisecond):
	}

	if count := db.waiterCount(); count != 1 {
		t.Fatalf("expected 1 remaining waiter, got %d", count)
	}
}

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
		ID string `json:"id"`
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
	resp, err = http.Post(srv.URL+"/tasks/"+created.ID+"/release", "application/json", bytes.NewReader(relBody))
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
		ID string `json:"id"`
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
		ID string `json:"id"`
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
	resp, err = http.Post(srv.URL+"/tasks/"+created.ID+"/bury", "application/json", bytes.NewReader(buryBody))
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

	resp, err = http.Post(srv.URL+"/tasks/"+created.ID+"/kick", "application/json", bytes.NewReader([]byte("{}")))
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

func TestClaimWaitCloseStoreUnblocksCleanly(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	statusCh := make(chan int, 1)
	go func() {
		claimBody, _ := json.Marshal(map[string]any{"worker": "w1", "project": "w", "wait": 5})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/tasks/claim", bytes.NewReader(claimBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			statusCh <- -1
			return
		}
		defer resp.Body.Close()
		statusCh <- resp.StatusCode
	}()

	for db.waiterCount() == 0 {
		time.Sleep(time.Millisecond)
	}

	db.Close()

	select {
	case status := <-statusCh:
		if status != http.StatusServiceUnavailable && status != -1 {
			t.Fatalf("expected 503 on closed store, got %d", status)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("waiter did not unblock on store close")
	}
}
