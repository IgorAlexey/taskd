package taskd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestClaimProjectValidation(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	postTask := func(project string) {
		body := `{"body":"test","project":"` + project + `"}`
		resp, err := http.Post(srv.URL+"/tasks", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("post task failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", resp.StatusCode)
		}
	}

	postTask("valid-proj")

	badPayload := `{"worker":"w1","project":"bad project name with spaces"}`
	resp, err := http.Post(srv.URL+"/tasks/claim", "application/json", strings.NewReader(badPayload))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	var errResp map[string]string
	if err := json.Unmarshal(raw, &errResp); err != nil {
		t.Fatalf("unmarshal error failed: %v", err)
	}
	want := `invalid project "bad project name with spaces": must contain only [a-zA-Z0-9._-]`
	if got := errResp["error"]; got != want {
		t.Fatalf("expected error %q, got %q", want, got)
	}

	emptyPayload := `{"worker":"w1","project":""}`
	respEmpty, err := http.Post(srv.URL+"/tasks/claim", "application/json", strings.NewReader(emptyPayload))
	if err != nil {
		t.Fatalf("claim empty failed: %v", err)
	}
	defer respEmpty.Body.Close()
	if respEmpty.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for empty project, got %d", respEmpty.StatusCode)
	}

	postTask("valid-proj")
	validPayload := `{"worker":"w1","project":"valid-proj"}`
	respValid, err := http.Post(srv.URL+"/tasks/claim", "application/json", strings.NewReader(validPayload))
	if err != nil {
		t.Fatalf("claim valid failed: %v", err)
	}
	defer respValid.Body.Close()
	if respValid.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for valid project, got %d", respValid.StatusCode)
	}
	postTask("p1")
	wildcardPayload := `{"worker":"w1","project":"*"}`
	respWildcard, err := http.Post(srv.URL+"/tasks/claim", "application/json", strings.NewReader(wildcardPayload))
	if err != nil {
		t.Fatalf("claim wildcard failed: %v", err)
	}
	defer respWildcard.Body.Close()
	if respWildcard.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for wildcard project, got %d", respWildcard.StatusCode)
	}
	var claimed taskItem
	if err := json.NewDecoder(respWildcard.Body).Decode(&claimed); err != nil {
		t.Fatalf("decode wildcard claim failed: %v", err)
	}
	if claimed.Project != "p1" {
		t.Fatalf("expected claimed task project %q, got %q", "p1", claimed.Project)
	}
}

func TestConcurrentClaimersExactlyOnce(t *testing.T) {
	st, err := openDB(filepath.Join(t.TempDir(), "test.db"), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	srv := httptest.NewServer(newHandler(st, 3600))
	defer srv.Close()

	const numTasks = 100
	const numWorkers = 32

	for i := range numTasks {
		id := fmt.Sprintf("task-%04d", i)
		body := fmt.Sprintf(`{"project":"bench","body":"work %d"}`, i)
		res, err := srv.Client().Post(srv.URL+"/tasks", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("failed to insert task %s: %v", id, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("unexpected status inserting task %s: %d", id, res.StatusCode)
		}
	}

	var mu sync.Mutex
	claimed := make(map[int64]int)
	var totalClaims int

	var wg sync.WaitGroup
	for w := range numWorkers {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			workerName := fmt.Sprintf("worker-%d", workerID)
			payload := fmt.Sprintf(`{"worker":%q,"project":"bench"}`, workerName)
			for {
				res, err := srv.Client().Post(srv.URL+"/tasks/claim", "application/json", strings.NewReader(payload))
				if err != nil {
					t.Errorf("claim error for %s: %v", workerName, err)
					return
				}
				if res.StatusCode == http.StatusNoContent {
					res.Body.Close()
					return
				}
				if res.StatusCode != http.StatusOK {
					t.Errorf("unexpected status %d for %s", res.StatusCode, workerName)
					res.Body.Close()
					return
				}
				var item taskItem
				if err := json.NewDecoder(res.Body).Decode(&item); err != nil {
					t.Errorf("decode error for %s: %v", workerName, err)
					res.Body.Close()
					return
				}
				res.Body.Close()

				mu.Lock()
				totalClaims++
				claimed[item.ID]++
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()

	if totalClaims != numTasks {
		t.Fatalf("total claims = %d, want %d", totalClaims, numTasks)
	}
	if len(claimed) != numTasks {
		t.Fatalf("distinct claimed tasks = %d, want %d", len(claimed), numTasks)
	}
	for id, count := range claimed {
		if count != 1 {
			t.Fatalf("task %d was claimed %d times, want 1", id, count)
		}
	}
}
