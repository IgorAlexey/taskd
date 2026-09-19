package main

import (
	"bytes"
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

func post(t *testing.T, url string, body any) (int, []byte) {
	t.Helper()
	var reqBody []byte
	if body != nil {
		switch v := body.(type) {
		case []byte:
			reqBody = v
		case string:
			reqBody = []byte(v)
		default:
			var err error
			reqBody, err = json.Marshal(body)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}
		}
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("http.Post %s failed: %v", url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	return resp.StatusCode, data
}

func TestFlow(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	code, body := post(t, srv.URL+"/tasks", map[string]string{"asset_path": "a.glb"})
	if code != http.StatusCreated {
		t.Fatalf("POST /tasks expected 201, got %d: %s", code, body)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal created task failed: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected non-empty id from POST /tasks, got empty string")
	}

	code, body = post(t, srv.URL+"/tasks/claim", map[string]string{"worker": "w1"})
	if code != http.StatusOK {
		t.Fatalf("POST /tasks/claim expected 200, got %d: %s", code, body)
	}
	var claimed struct {
		ID        string `json:"id"`
		AssetPath string `json:"asset_path"`
	}
	if err := json.Unmarshal(body, &claimed); err != nil {
		t.Fatalf("unmarshal claim failed: %v", err)
	}
	if claimed.ID != created.ID {
		t.Fatalf("claimed id %q != created id %q", claimed.ID, created.ID)
	}
	if claimed.AssetPath != "a.glb" {
		t.Fatalf("claimed asset_path %q != %q", claimed.AssetPath, "a.glb")
	}

	code, body = post(t, srv.URL+"/tasks/claim", map[string]string{"worker": "w1"})
	if code != http.StatusNoContent {
		t.Fatalf("second claim expected 204, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/"+created.ID+"/done", map[string]any{
		"worker":     "w2",
		"primitives": []int{1},
	})
	if code != http.StatusConflict {
		t.Fatalf("done with worker w2 expected 409, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/"+created.ID+"/done", map[string]any{
		"worker": "w1",
		"primitives": map[string]any{
			"meshes": []string{"m"},
		},
	})
	if code != http.StatusNoContent {
		t.Fatalf("done with worker w1 expected 204, got %d: %s", code, body)
	}

	var status, worker, primitivesRaw string
	err = db.QueryRow("SELECT status, worker, primitives FROM tasks WHERE id = ?", created.ID).Scan(&status, &worker, &primitivesRaw)
	if err != nil {
		t.Fatalf("query db failed: %v", err)
	}
	if status != "done" {
		t.Fatalf("expected status 'done', got %q", status)
	}
	if worker != "w1" {
		t.Fatalf("expected worker 'w1', got %q", worker)
	}

	var parsedPrimitives struct {
		Meshes []string `json:"meshes"`
	}
	if err := json.Unmarshal([]byte(primitivesRaw), &parsedPrimitives); err != nil {
		t.Fatalf("unmarshal primitives JSON from db failed: %v (raw: %s)", err, primitivesRaw)
	}
	if len(parsedPrimitives.Meshes) != 1 || parsedPrimitives.Meshes[0] != "m" {
		t.Fatalf("primitives JSON round-trip mismatch: got %v (raw: %s)", parsedPrimitives.Meshes, primitivesRaw)
	}

	code, body = post(t, srv.URL+"/tasks/"+created.ID+"/done", map[string]any{
		"worker": "w1",
		"primitives": map[string]any{
			"meshes": []string{"m"},
		},
	})
	if code != http.StatusConflict {
		t.Fatalf("done again expected 409, got %d: %s", code, body)
	}
}

func TestValidation(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	code, body := post(t, srv.URL+"/tasks", map[string]string{})
	if code != http.StatusBadRequest {
		t.Fatalf("POST /tasks {} expected 400, got %d: %s", code, body)
	}

	explicit := map[string]string{
		"id":         "explicit-1",
		"asset_path": "model.glb",
	}
	code, body = post(t, srv.URL+"/tasks", explicit)
	if code != http.StatusCreated {
		t.Fatalf("POST /tasks explicit id first expected 201, got %d: %s", code, body)
	}
	var res struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if res.ID != "explicit-1" {
		t.Fatalf("expected id %q, got %q", "explicit-1", res.ID)
	}

	code, body = post(t, srv.URL+"/tasks", explicit)
	if code != http.StatusConflict {
		t.Fatalf("POST /tasks explicit id duplicate expected 409, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/claim", map[string]string{"worker": ""})
	if code != http.StatusBadRequest {
		t.Fatalf("claim empty worker expected 400, got %d: %s", code, body)
	}

	db2, err := openDB(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("openDB fresh failed: %v", err)
	}
	defer db2.Close()

	srv2 := httptest.NewServer(newHandler(db2, 300))
	defer srv2.Close()

	code, body = post(t, srv2.URL+"/tasks/claim", map[string]string{"worker": "w1"})
	if code != http.StatusNoContent {
		t.Fatalf("claim on empty table expected 204, got %d: %s", code, body)
	}
}

func TestExpiredLeaseReclaim(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	code, body := post(t, srv.URL+"/tasks", map[string]string{"asset_path": "scene.gltf"})
	if code != http.StatusCreated {
		t.Fatalf("enqueue expected 201, got %d: %s", code, body)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal created failed: %v", err)
	}

	code, body = post(t, srv.URL+"/tasks/claim", map[string]string{"worker": "w1"})
	if code != http.StatusOK {
		t.Fatalf("claim by w1 expected 200, got %d: %s", code, body)
	}
	var claimed1 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &claimed1); err != nil {
		t.Fatalf("unmarshal claim1 failed: %v", err)
	}
	if claimed1.ID != created.ID {
		t.Fatalf("claim1 id %q != created id %q", claimed1.ID, created.ID)
	}

	if _, err := db.Exec("UPDATE tasks SET lease_expires=0 WHERE id = ?", created.ID); err != nil {
		t.Fatalf("update lease_expires failed: %v", err)
	}

	code, body = post(t, srv.URL+"/tasks/claim", map[string]string{"worker": "w2"})
	if code != http.StatusOK {
		t.Fatalf("claim by w2 expected 200, got %d: %s", code, body)
	}
	var claimed2 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &claimed2); err != nil {
		t.Fatalf("unmarshal claim2 failed: %v", err)
	}
	if claimed2.ID != created.ID {
		t.Fatalf("claim2 id %q != created id %q", claimed2.ID, created.ID)
	}

	var worker, status string
	err = db.QueryRow("SELECT worker, status FROM tasks WHERE id = ?", created.ID).Scan(&worker, &status)
	if err != nil {
		t.Fatalf("query db failed: %v", err)
	}
	if worker != "w2" {
		t.Fatalf("expected worker 'w2', got %q", worker)
	}
	if status != "leased" {
		t.Fatalf("expected status 'leased', got %q", status)
	}

	code, body = post(t, srv.URL+"/tasks/"+created.ID+"/done", map[string]any{
		"worker":     "w1",
		"primitives": []any{},
	})
	if code != http.StatusConflict {
		t.Fatalf("done by w1 expected 409, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/"+created.ID+"/done", map[string]any{
		"worker":     "w2",
		"primitives": []any{},
	})
	if code != http.StatusNoContent {
		t.Fatalf("done by w2 expected 204, got %d: %s", code, body)
	}
}

func TestConcurrentClaims(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	const numTasks = 20
	const numWorkers = 50

	for i := 0; i < numTasks; i++ {
		code, body := post(t, srv.URL+"/tasks", map[string]string{
			"asset_path": fmt.Sprintf("asset-%d.glb", i),
		})
		if code != http.StatusCreated {
			t.Fatalf("enqueue task %d expected 201, got %d: %s", i, code, body)
		}
	}

	type claimResult struct {
		code int
		id   string
	}
	results := make(chan claimResult, numWorkers)

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	start := make(chan struct{})

	for i := 0; i < numWorkers; i++ {
		workerName := fmt.Sprintf("worker-%d", i)
		go func(worker string) {
			defer wg.Done()
			<-start
			code, body := post(t, srv.URL+"/tasks/claim", map[string]string{
				"worker": worker,
			})
			var id string
			if code == http.StatusOK {
				var res struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(body, &res); err != nil {
					t.Errorf("unmarshal claim response failed: %v", err)
				}
				id = res.ID
			}
			results <- claimResult{code: code, id: id}
		}(workerName)
	}

	close(start)
	wg.Wait()
	close(results)

	var count200, count204 int
	claimedIDs := make(map[string]bool)

	for res := range results {
		switch res.code {
		case http.StatusOK:
			count200++
			if res.id == "" {
				t.Fatalf("got status 200 but task id was empty")
			}
			if claimedIDs[res.id] {
				t.Fatalf("task id %q claimed more than once", res.id)
			}
			claimedIDs[res.id] = true
		case http.StatusNoContent:
			count204++
		default:
			t.Fatalf("unexpected claim response code: %d", res.code)
		}
	}

	if count200 != numTasks {
		t.Fatalf("expected %d status 200 responses, got %d", numTasks, count200)
	}
	if count204 != (numWorkers - numTasks) {
		t.Fatalf("expected %d status 204 responses, got %d", numWorkers-numTasks, count204)
	}
	if len(claimedIDs) != numTasks {
		t.Fatalf("expected %d distinct claimed task IDs, got %d", numTasks, len(claimedIDs))
	}
}

func TestWAL(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode failed: %v", err)
	}
	if strings.ToLower(strings.TrimSpace(mode)) != "wal" {
		t.Fatalf("expected journal_mode 'wal', got %q", mode)
	}
}
func TestMaxBytesLimit(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	big := fmt.Sprintf(`{"asset_path":"%s"}`, strings.Repeat("x", 2<<20))
	code, _ := post(t, srv.URL+"/tasks", big)
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for 2 MiB body, got %d", code)
	}

	bigClaim := fmt.Sprintf(`{"worker":"%s"}`, strings.Repeat("w", 2<<20))
	code, _ = post(t, srv.URL+"/tasks/claim", bigClaim)
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("claim expected 413 for 2 MiB body, got %d", code)
	}

	bigDone := fmt.Sprintf(`{"worker":"w1","primitives":{"data":"%s"}}`, strings.Repeat("d", 2<<20))
	code, _ = post(t, srv.URL+"/tasks/task-1/done", bigDone)
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("done expected 413 for 2 MiB body, got %d", code)
	}
}
