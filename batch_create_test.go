package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestBatchCreateTasks(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 300)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	t.Run("single task backward compatibility", func(t *testing.T) {
		payload := `{"project":"p-single","body":"single task"}`
		res, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(payload))
		if err != nil {
			t.Fatalf("POST /tasks failed: %v", err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", res.StatusCode)
		}
		loc := res.Header.Get("Location")
		if loc == "" {
			t.Fatal("expected Location header on single create")
		}
		var created map[string]string
		if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if created["id"] == "" {
			t.Fatal("expected task id in response")
		}
	})

	t.Run("batch create success", func(t *testing.T) {
		payload := `[{"project":"p-batch","body":"b1"},{"project":"p-batch","body":"b2","priority":5}]`
		res, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(payload))
		if err != nil {
			t.Fatalf("POST /tasks batch failed: %v", err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", res.StatusCode)
		}
		var created []map[string]string
		if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
			t.Fatalf("decode batch response failed: %v", err)
		}
		if len(created) != 2 {
			t.Fatalf("expected 2 created tasks, got %d", len(created))
		}
		if created[0]["id"] == "" || created[1]["id"] == "" {
			t.Fatal("expected non-empty IDs for created tasks")
		}
		if created[0]["id"] == created[1]["id"] {
			t.Fatalf("expected distinct IDs, got %s and %s", created[0]["id"], created[1]["id"])
		}

		for _, item := range created {
			getRes, err := http.Get(srv.URL + "/tasks/" + item["id"])
			if err != nil {
				t.Fatalf("GET /tasks/%s failed: %v", item["id"], err)
			}
			if getRes.StatusCode != http.StatusOK {
				getRes.Body.Close()
				t.Fatalf("expected 200 for task %s, got %d", item["id"], getRes.StatusCode)
			}
			getRes.Body.Close()
		}
	})

	t.Run("batch atomic rollback on conflict", func(t *testing.T) {
		first := `{"id":"existing-id","project":"p-atomic","body":"initial"}`
		resInit, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(first))
		if err != nil {
			t.Fatalf("create initial task failed: %v", err)
		}
		resInit.Body.Close()

		conflictBatch := `[
			{"id":"batch-atomic-1","project":"p-atomic","body":"first in batch"},
			{"id":"existing-id","project":"p-atomic","body":"duplicate id"},
			{"id":"batch-atomic-2","project":"p-atomic","body":"second in batch"}
		]`
		resConf, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(conflictBatch))
		if err != nil {
			t.Fatalf("POST conflict batch failed: %v", err)
		}
		defer resConf.Body.Close()
		if resConf.StatusCode != http.StatusConflict {
			t.Fatalf("expected 409 Conflict, got %d", resConf.StatusCode)
		}

		check1, err := http.Get(srv.URL + "/tasks/batch-atomic-1")
		if err != nil {
			t.Fatalf("get check1 failed: %v", err)
		}
		defer check1.Body.Close()
		if check1.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 for uncommitted task 1, got %d", check1.StatusCode)
		}

		check2, err := http.Get(srv.URL + "/tasks/batch-atomic-2")
		if err != nil {
			t.Fatalf("get check2 failed: %v", err)
		}
		defer check2.Body.Close()
		if check2.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 for uncommitted task 2, got %d", check2.StatusCode)
		}
	})

	t.Run("batch wakes waiting claimers", func(t *testing.T) {
		var wg sync.WaitGroup
		claimed := make(chan string, 2)

		for i := range 2 {
			wg.Add(1)
			go func(workerIndex int) {
				defer wg.Done()
				claimBody := map[string]any{
					"worker":  "wait-worker",
					"project": "p-wake",
					"wait":    5.0,
				}
				data, _ := json.Marshal(claimBody)
				cRes, cErr := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(data))
				if cErr != nil {
					return
				}
				defer cRes.Body.Close()
				if cRes.StatusCode == http.StatusOK {
					var item taskItem
					if err := json.NewDecoder(cRes.Body).Decode(&item); err == nil {
						claimed <- item.ID
					}
				}
			}(i)
		}

		time.Sleep(50 * time.Millisecond)

		batchPayload := `[{"project":"p-wake","body":"w1"},{"project":"p-wake","body":"w2"}]`
		bRes, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(batchPayload))
		if err != nil {
			t.Fatalf("batch post failed: %v", err)
		}
		bRes.Body.Close()
		if bRes.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", bRes.StatusCode)
		}

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for claimers to wake up")
		}

		close(claimed)
		ids := make([]string, 0, 2)
		for id := range claimed {
			ids = append(ids, id)
		}
		if len(ids) != 2 {
			t.Fatalf("expected 2 tasks claimed, got %d", len(ids))
		}
	})
}
