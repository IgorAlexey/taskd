package taskd

import (
	"bytes"
	"encoding/json"
	"fmt"
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
		var created map[string]int64
		if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if created["id"] <= 0 {
			t.Fatal("expected positive task id in response")
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
		var created []map[string]int64
		if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
			t.Fatalf("decode batch response failed: %v", err)
		}
		if len(created) != 2 {
			t.Fatalf("expected 2 created tasks, got %d", len(created))
		}
		if created[0]["id"] <= 0 || created[1]["id"] <= 0 {
			t.Fatal("expected positive IDs for created tasks")
		}
		if created[0]["id"] == created[1]["id"] {
			t.Fatalf("expected distinct IDs, got %d and %d", created[0]["id"], created[1]["id"])
		}

		for _, item := range created {
			path := fmt.Sprintf("/tasks/%d", item["id"])
			getRes, err := http.Get(srv.URL + path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", path, err)
			}
			if getRes.StatusCode != http.StatusOK {
				getRes.Body.Close()
				t.Fatalf("expected 200 for task %s, got %d", path, getRes.StatusCode)
			}
			getRes.Body.Close()
		}
	})

	t.Run("batch wakes waiting claimers", func(t *testing.T) {
		var wg sync.WaitGroup
		claimed := make(chan int64, 2)

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
		ids := make([]int64, 0, 2)
		for id := range claimed {
			ids = append(ids, id)
		}
		if len(ids) != 2 {
			t.Fatalf("expected 2 tasks claimed, got %d", len(ids))
		}
	})
}

func TestAutoincrementNoReuse(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	create := func(body string) int64 {
		payload, _ := json.Marshal(map[string]string{"body": body, "project": "p"})
		resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("POST /tasks: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d, want 201", resp.StatusCode)
		}
		var res struct {
			ID int64 `json:"id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return res.ID
	}

	id1 := create("first")
	if id1 != 1 {
		t.Fatalf("id1 = %d, want 1", id1)
	}
	id2 := create("second")
	if id2 != 2 {
		t.Fatalf("id2 = %d, want 2", id2)
	}

	delReq, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/tasks/%d", srv.URL, id2), nil)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("DELETE /tasks/2 failed: %v", err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", delResp.StatusCode)
	}

	id3 := create("third")
	if id3 != 3 {
		t.Fatalf("id3 = %d, want 3 (no reuse of autoincrement ID)", id3)
	}
}
