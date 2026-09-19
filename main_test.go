package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

	code, body := post(t, srv.URL+"/tasks", map[string]string{"asset_path": "a.glb", "project": "p"})
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

	code, body = post(t, srv.URL+"/tasks", map[string]string{"body": "x"})
	if code != http.StatusBadRequest {
		t.Fatalf("POST /tasks without project expected 400, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks", map[string]string{"body": "x", "project": "*"})
	if code != http.StatusBadRequest {
		t.Fatalf("POST /tasks with project * expected 400, got %d: %s", code, body)
	}

	explicit := map[string]string{
		"id":         "explicit-1",
		"asset_path": "model.glb",
		"project":    "p",
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

	code, body := post(t, srv.URL+"/tasks", map[string]string{"asset_path": "scene.gltf", "project": "p"})
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
			"project":    "p",
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
				"worker":  worker,
				"project": "*",
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

func do(t *testing.T, method, url string, body any) (int, []byte) {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		switch v := body.(type) {
		case []byte:
			reqBody = bytes.NewReader(v)
		case string:
			reqBody = strings.NewReader(v)
		default:
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}
			reqBody = bytes.NewReader(data)
		}
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("http.NewRequest failed: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.DefaultClient.Do failed: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	return resp.StatusCode, data
}

func TestEnqueueBodyPriority(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	code, body := post(t, srv.URL+"/tasks", map[string]any{
		"body":     "# fix X\ndetails",
		"priority": 5,
		"project":  "p",
	})
	if code != http.StatusCreated {
		t.Fatalf("POST /tasks with body and priority expected 201, got %d: %s", code, body)
	}
	var res1 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &res1); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if res1.ID == "" {
		t.Fatalf("expected non-empty id, got empty string")
	}

	var bodyVal string
	var priorityVal int
	err = db.QueryRow("SELECT body, priority FROM tasks WHERE id = ?", res1.ID).Scan(&bodyVal, &priorityVal)
	if err != nil {
		t.Fatalf("query db failed: %v", err)
	}
	if bodyVal != "# fix X\ndetails" {
		t.Fatalf("expected body %q, got %q", "# fix X\ndetails", bodyVal)
	}
	if priorityVal != 5 {
		t.Fatalf("expected priority 5, got %d", priorityVal)
	}

	code, body = post(t, srv.URL+"/tasks", map[string]any{})
	if code != http.StatusBadRequest {
		t.Fatalf("POST /tasks {} expected 400, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks", map[string]any{
		"body":    "only body",
		"project": "p",
	})
	if code != http.StatusCreated {
		t.Fatalf("POST /tasks with only body expected 201, got %d: %s", code, body)
	}
	var res2 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &res2); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}

	var bodyVal2, assetPathVal2 string
	var priorityVal2 int
	err = db.QueryRow("SELECT asset_path, body, priority FROM tasks WHERE id = ?", res2.ID).Scan(&assetPathVal2, &bodyVal2, &priorityVal2)
	if err != nil {
		t.Fatalf("query db failed: %v", err)
	}
	if assetPathVal2 != "" {
		t.Fatalf("expected empty asset_path, got %q", assetPathVal2)
	}
	if bodyVal2 != "only body" {
		t.Fatalf("expected body %q, got %q", "only body", bodyVal2)
	}
	if priorityVal2 != 0 {
		t.Fatalf("expected default priority 0, got %d", priorityVal2)
	}
}

func TestClaimPriorityOrder(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	codeA, bodyA := post(t, srv.URL+"/tasks", map[string]any{
		"body":     "task A",
		"priority": 0,
		"project":  "p",
	})
	if codeA != http.StatusCreated {
		t.Fatalf("create A failed: %d: %s", codeA, bodyA)
	}
	var resA struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(bodyA, &resA); err != nil {
		t.Fatalf("unmarshal A failed: %v", err)
	}

	codeB, bodyB := post(t, srv.URL+"/tasks", map[string]any{
		"body":     "task B",
		"priority": 9,
		"project":  "p",
	})
	if codeB != http.StatusCreated {
		t.Fatalf("create B failed: %d: %s", codeB, bodyB)
	}
	var resB struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(bodyB, &resB); err != nil {
		t.Fatalf("unmarshal B failed: %v", err)
	}

	codeC, bodyC := post(t, srv.URL+"/tasks", map[string]any{
		"body":     "task C",
		"priority": 0,
		"project":  "p",
	})
	if codeC != http.StatusCreated {
		t.Fatalf("create C failed: %d: %s", codeC, bodyC)
	}
	var resC struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(bodyC, &resC); err != nil {
		t.Fatalf("unmarshal C failed: %v", err)
	}

	type claimResp struct {
		ID        string `json:"id"`
		AssetPath string `json:"asset_path"`
		Body      string `json:"body"`
		Priority  int    `json:"priority"`
	}

	code1, data1 := post(t, srv.URL+"/tasks/claim", map[string]string{"worker": "w1"})
	if code1 != http.StatusOK {
		t.Fatalf("claim 1 expected 200, got %d: %s", code1, data1)
	}
	var claim1 claimResp
	if err := json.Unmarshal(data1, &claim1); err != nil {
		t.Fatalf("unmarshal claim 1 failed: %v", err)
	}
	if claim1.ID != resB.ID {
		t.Fatalf("expected first claim to be B (%q), got %q", resB.ID, claim1.ID)
	}
	if claim1.Body != "task B" {
		t.Fatalf("expected body %q, got %q", "task B", claim1.Body)
	}
	if claim1.Priority != 9 {
		t.Fatalf("expected priority 9, got %d", claim1.Priority)
	}

	code2, data2 := post(t, srv.URL+"/tasks/claim", map[string]string{"worker": "w1"})
	if code2 != http.StatusOK {
		t.Fatalf("claim 2 expected 200, got %d: %s", code2, data2)
	}
	var claim2 claimResp
	if err := json.Unmarshal(data2, &claim2); err != nil {
		t.Fatalf("unmarshal claim 2 failed: %v", err)
	}
	if claim2.ID != resA.ID {
		t.Fatalf("expected second claim to be A (%q), got %q", resA.ID, claim2.ID)
	}
	if claim2.Body != "task A" {
		t.Fatalf("expected body %q, got %q", "task A", claim2.Body)
	}
	if claim2.Priority != 0 {
		t.Fatalf("expected priority 0, got %d", claim2.Priority)
	}

	code3, data3 := post(t, srv.URL+"/tasks/claim", map[string]string{"worker": "w1"})
	if code3 != http.StatusOK {
		t.Fatalf("claim 3 expected 200, got %d: %s", code3, data3)
	}
	var claim3 claimResp
	if err := json.Unmarshal(data3, &claim3); err != nil {
		t.Fatalf("unmarshal claim 3 failed: %v", err)
	}
	if claim3.ID != resC.ID {
		t.Fatalf("expected third claim to be C (%q), got %q", resC.ID, claim3.ID)
	}
	if claim3.Body != "task C" {
		t.Fatalf("expected body %q, got %q", "task C", claim3.Body)
	}
	if claim3.Priority != 0 {
		t.Fatalf("expected priority 0, got %d", claim3.Priority)
	}
}

func TestListTasks(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	code, body := do(t, http.MethodGet, srv.URL+"/tasks", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks expected 200, got %d: %s", code, body)
	}
	if strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("GET /tasks on empty db expected [], got %q", string(body))
	}

	code1, body1 := post(t, srv.URL+"/tasks", map[string]any{
		"asset_path": "model1.glb",
		"body":       "first task",
		"priority":   2,
		"project":    "p",
	})
	if code1 != http.StatusCreated {
		t.Fatalf("create task 1 failed: %d: %s", code1, body1)
	}
	var res1 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body1, &res1); err != nil {
		t.Fatalf("unmarshal task 1 failed: %v", err)
	}

	code2, body2 := post(t, srv.URL+"/tasks", map[string]any{
		"body":     "second task",
		"priority": 1,
		"project":  "p",
	})
	if code2 != http.StatusCreated {
		t.Fatalf("create task 2 failed: %d: %s", code2, body2)
	}
	var res2 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body2, &res2); err != nil {
		t.Fatalf("unmarshal task 2 failed: %v", err)
	}

	claimCode, claimBody := post(t, srv.URL+"/tasks/claim", map[string]string{"worker": "worker-list"})
	if claimCode != http.StatusOK {
		t.Fatalf("claim failed: %d: %s", claimCode, claimBody)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks expected 200, got %d: %s", code, body)
	}
	var allTasks []map[string]any
	if err := json.Unmarshal(body, &allTasks); err != nil {
		t.Fatalf("unmarshal all tasks failed: %v: %s", err, body)
	}
	if len(allTasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(allTasks))
	}

	for _, taskMap := range allTasks {
		requiredKeys := []string{"id", "asset_path", "status", "worker", "lease_expires", "priority", "body", "primitives", "project"}
		for _, key := range requiredKeys {
			if _, ok := taskMap[key]; !ok {
				t.Fatalf("missing required key %q in task JSON: %v", key, taskMap)
			}
		}
		if taskMap["id"] == res2.ID {
			if taskMap["primitives"] != nil {
				t.Fatalf("expected pending task primitives to be null, got %v", taskMap["primitives"])
			}
		}
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks?status=leased", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks?status=leased expected 200, got %d: %s", code, body)
	}
	var leasedTasks []struct {
		ID           string `json:"id"`
		Status       string `json:"status"`
		Worker       string `json:"worker"`
		LeaseExpires int64  `json:"lease_expires"`
	}
	if err := json.Unmarshal(body, &leasedTasks); err != nil {
		t.Fatalf("unmarshal leased tasks failed: %v: %s", err, body)
	}
	if len(leasedTasks) != 1 {
		t.Fatalf("expected 1 leased task, got %d", len(leasedTasks))
	}
	if leasedTasks[0].Worker != "worker-list" {
		t.Fatalf("expected worker %q, got %q", "worker-list", leasedTasks[0].Worker)
	}
	if leasedTasks[0].LeaseExpires <= 0 {
		t.Fatalf("expected lease_expires > 0, got %d", leasedTasks[0].LeaseExpires)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks?status=done", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks?status=done expected 200, got %d: %s", code, body)
	}
	if strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("GET /tasks?status=done expected [], got %q", string(body))
	}
}

func TestPatchTask(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	code, body := post(t, srv.URL+"/tasks", map[string]any{
		"body":     "initial body",
		"priority": 1,
		"project":  "p",
	})
	if code != http.StatusCreated {
		t.Fatalf("create task failed: %d: %s", code, body)
	}
	var res struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("unmarshal create response failed: %v", err)
	}

	code, body = do(t, http.MethodPatch, srv.URL+"/tasks/"+res.ID, map[string]any{
		"priority": 3,
	})
	if code != http.StatusNoContent {
		t.Fatalf("PATCH priority expected 204, got %d: %s", code, body)
	}

	var bodyVal string
	var priorityVal int
	err = db.QueryRow("SELECT body, priority FROM tasks WHERE id = ?", res.ID).Scan(&bodyVal, &priorityVal)
	if err != nil {
		t.Fatalf("query db failed: %v", err)
	}
	if priorityVal != 3 {
		t.Fatalf("expected priority 3, got %d", priorityVal)
	}
	if bodyVal != "initial body" {
		t.Fatalf("expected body unchanged %q, got %q", "initial body", bodyVal)
	}

	code, body = do(t, http.MethodPatch, srv.URL+"/tasks/"+res.ID, map[string]any{
		"body": "new",
	})
	if code != http.StatusNoContent {
		t.Fatalf("PATCH body expected 204, got %d: %s", code, body)
	}

	err = db.QueryRow("SELECT body, priority FROM tasks WHERE id = ?", res.ID).Scan(&bodyVal, &priorityVal)
	if err != nil {
		t.Fatalf("query db failed: %v", err)
	}
	if bodyVal != "new" {
		t.Fatalf("expected body %q, got %q", "new", bodyVal)
	}
	if priorityVal != 3 {
		t.Fatalf("expected priority unchanged 3, got %d", priorityVal)
	}

	code, body = do(t, http.MethodPatch, srv.URL+"/tasks/"+res.ID, map[string]any{})
	if code != http.StatusBadRequest {
		t.Fatalf("PATCH {} expected 400, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodPatch, srv.URL+"/tasks/nope", map[string]any{
		"priority": 1,
	})
	if code != http.StatusNotFound {
		t.Fatalf("PATCH /tasks/nope expected 404, got %d: %s", code, body)
	}
}

func TestPatchLeasedTask(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	code, body := post(t, srv.URL+"/tasks", map[string]any{
		"body":     "orig",
		"project":  "p1",
		"priority": 1,
	})
	if code != http.StatusCreated {
		t.Fatalf("create task failed: %d: %s", code, body)
	}
	var res struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("unmarshal create response failed: %v", err)
	}

	code, body = post(t, srv.URL+"/tasks/claim", map[string]string{
		"worker":  "w1",
		"project": "p1",
	})
	if code != http.StatusOK {
		t.Fatalf("claim failed: %d: %s", code, body)
	}

	code, body = do(t, http.MethodPatch, srv.URL+"/tasks/"+res.ID, map[string]any{
		"body": "mutated",
	})
	if code != http.StatusConflict {
		t.Fatalf("PATCH on leased task expected 409, got %d: %s", code, body)
	}

	var bodyVal string
	var priorityVal int
	err = db.QueryRow("SELECT body, priority FROM tasks WHERE id = ?", res.ID).Scan(&bodyVal, &priorityVal)
	if err != nil {
		t.Fatalf("query db failed: %v", err)
	}
	if bodyVal != "orig" || priorityVal != 1 {
		t.Fatalf("task was mutated while leased: body=%q, priority=%d", bodyVal, priorityVal)
	}

	code, body = post(t, srv.URL+"/tasks/"+res.ID+"/done", map[string]any{
		"worker": "w1",
	})
	if code != http.StatusNoContent {
		t.Fatalf("done failed: %d: %s", code, body)
	}

	code, body = do(t, http.MethodPatch, srv.URL+"/tasks/"+res.ID, map[string]any{
		"body": "mutated after done",
	})
	if code != http.StatusNoContent {
		t.Fatalf("PATCH on done task expected 204, got %d: %s", code, body)
	}

	err = db.QueryRow("SELECT body FROM tasks WHERE id = ?", res.ID).Scan(&bodyVal)
	if err != nil {
		t.Fatalf("query db failed: %v", err)
	}
	if bodyVal != "mutated after done" {
		t.Fatalf("expected body %q, got %q", "mutated after done", bodyVal)
	}

	code, body = post(t, srv.URL+"/tasks", map[string]any{
		"body":    "expired task",
		"project": "p1",
	})
	if code != http.StatusCreated {
		t.Fatalf("create task failed: %d: %s", code, body)
	}
	var resExpired struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &resExpired); err != nil {
		t.Fatalf("unmarshal create response failed: %v", err)
	}

	code, body = post(t, srv.URL+"/tasks/claim", map[string]string{
		"worker":  "w2",
		"project": "p1",
	})
	if code != http.StatusOK {
		t.Fatalf("claim failed: %d: %s", code, body)
	}

	if _, err := db.Exec("UPDATE tasks SET lease_expires = unixepoch() - 10 WHERE id = ?", resExpired.ID); err != nil {
		t.Fatalf("set expired lease failed: %v", err)
	}

	code, body = do(t, http.MethodPatch, srv.URL+"/tasks/"+resExpired.ID, map[string]any{
		"body": "updated after expired lease",
	})
	if code != http.StatusNoContent {
		t.Fatalf("PATCH on expired lease task expected 204, got %d: %s", code, body)
	}
}

func TestDeleteTask(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	code, body := post(t, srv.URL+"/tasks", map[string]any{
		"body":    "to delete",
		"project": "p",
	})
	if code != http.StatusCreated {
		t.Fatalf("create task failed: %d: %s", code, body)
	}
	var res struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("unmarshal create response failed: %v", err)
	}

	code, body = do(t, http.MethodDelete, srv.URL+"/tasks/"+res.ID, nil)
	if code != http.StatusNoContent {
		t.Fatalf("DELETE expected 204, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks expected 200, got %d: %s", code, body)
	}
	if strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("GET /tasks expected [], got %q", string(body))
	}

	code, body = do(t, http.MethodDelete, srv.URL+"/tasks/"+res.ID, nil)
	if code != http.StatusNotFound {
		t.Fatalf("second DELETE expected 404, got %d: %s", code, body)
	}
}

func TestDeleteLeasedTask(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	code, body := post(t, srv.URL+"/tasks", map[string]any{
		"body":    "cannot delete leased",
		"project": "p",
	})
	if code != http.StatusCreated {
		t.Fatalf("create task failed: %d: %s", code, body)
	}
	var res struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("unmarshal create response failed: %v", err)
	}

	code, body = post(t, srv.URL+"/tasks/claim", map[string]string{
		"worker": "w1",
	})
	if code != http.StatusOK {
		t.Fatalf("claim failed: %d: %s", code, body)
	}

	code, body = do(t, http.MethodDelete, srv.URL+"/tasks/"+res.ID, nil)
	if code != http.StatusConflict {
		t.Fatalf("DELETE on leased task expected 409, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodDelete, srv.URL+"/tasks/"+res.ID+"?force=1", nil)
	if code != http.StatusConflict {
		t.Fatalf("DELETE on leased task with force expected 409, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/"+res.ID+"/done", map[string]any{
		"worker": "w1",
	})
	if code != http.StatusNoContent {
		t.Fatalf("done failed: %d: %s", code, body)
	}

	code, body = do(t, http.MethodDelete, srv.URL+"/tasks/"+res.ID, nil)
	if code != http.StatusConflict {
		t.Fatalf("DELETE on done task expected 409, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodDelete, srv.URL+"/tasks/"+res.ID+"?force=1", nil)
	if code != http.StatusNoContent {
		t.Fatalf("DELETE on done task with force expected 204, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodDelete, srv.URL+"/tasks/"+res.ID, nil)
	if code != http.StatusNotFound {
		t.Fatalf("second DELETE expected 404, got %d: %s", code, body)
	}
}

func TestClaimProject(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	codeA, bodyA := post(t, srv.URL+"/tasks", map[string]any{
		"body":    "task A",
		"project": "x",
	})
	if codeA != http.StatusCreated {
		t.Fatalf("enqueue A failed: %d: %s", codeA, bodyA)
	}
	var resA struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(bodyA, &resA); err != nil {
		t.Fatalf("unmarshal A failed: %v", err)
	}

	codeB, bodyB := post(t, srv.URL+"/tasks", map[string]any{
		"body":    "task B",
		"project": "y",
	})
	if codeB != http.StatusCreated {
		t.Fatalf("enqueue B failed: %d: %s", codeB, bodyB)
	}
	var resB struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(bodyB, &resB); err != nil {
		t.Fatalf("unmarshal B failed: %v", err)
	}

	type claimResp struct {
		ID      string `json:"id"`
		Body    string `json:"body"`
		Project string `json:"project"`
	}

	codeY, dataY := post(t, srv.URL+"/tasks/claim", map[string]string{
		"worker":  "w1",
		"project": "y",
	})
	if codeY != http.StatusOK {
		t.Fatalf("claim project y expected 200, got %d: %s", codeY, dataY)
	}
	var claimY claimResp
	if err := json.Unmarshal(dataY, &claimY); err != nil {
		t.Fatalf("unmarshal claim y failed: %v", err)
	}
	if claimY.ID != resB.ID {
		t.Fatalf("expected claim y to return task B (%q), got %q", resB.ID, claimY.ID)
	}
	if claimY.Project != "y" {
		t.Fatalf("expected claim y project 'y', got %q", claimY.Project)
	}

	codeX, dataX := post(t, srv.URL+"/tasks/claim", map[string]string{
		"worker":  "w1",
		"project": "x",
	})
	if codeX != http.StatusOK {
		t.Fatalf("claim project x expected 200, got %d: %s", codeX, dataX)
	}
	var claimX claimResp
	if err := json.Unmarshal(dataX, &claimX); err != nil {
		t.Fatalf("unmarshal claim x failed: %v", err)
	}
	if claimX.ID != resA.ID {
		t.Fatalf("expected claim x to return task A (%q), got %q", resA.ID, claimX.ID)
	}
	if claimX.Project != "x" {
		t.Fatalf("expected claim x project 'x', got %q", claimX.Project)
	}

	codeZ, dataZ := post(t, srv.URL+"/tasks/claim", map[string]string{
		"worker":  "w1",
		"project": "z",
	})
	if codeZ != http.StatusNoContent {
		t.Fatalf("claim project z expected 204, got %d: %s", codeZ, dataZ)
	}

	codeEmpty, dataEmpty := post(t, srv.URL+"/tasks/claim", map[string]string{
		"worker": "w1",
	})
	if codeEmpty != http.StatusNoContent {
		t.Fatalf("claim without project after both leased expected 204, got %d: %s", codeEmpty, dataEmpty)
	}

	codeAll, dataAll := post(t, srv.URL+"/tasks/claim", map[string]string{
		"worker": "w1",
	})
	if codeAll != http.StatusNoContent {
		t.Fatalf("claim '*' after both leased expected 204, got %d: %s", codeAll, dataAll)
	}
}

func TestListProjectFilter(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	code1, body1 := post(t, srv.URL+"/tasks", map[string]any{
		"body":    "task 1",
		"project": "x",
	})
	if code1 != http.StatusCreated {
		t.Fatalf("create task 1 failed: %d: %s", code1, body1)
	}
	var res1 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body1, &res1); err != nil {
		t.Fatalf("unmarshal task 1 failed: %v", err)
	}

	code2, body2 := post(t, srv.URL+"/tasks", map[string]any{
		"body":    "task 2",
		"project": "x",
	})
	if code2 != http.StatusCreated {
		t.Fatalf("create task 2 failed: %d: %s", code2, body2)
	}
	var res2 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body2, &res2); err != nil {
		t.Fatalf("unmarshal task 2 failed: %v", err)
	}

	code3, body3 := post(t, srv.URL+"/tasks", map[string]any{
		"body":    "task 3",
		"project": "y",
	})
	if code3 != http.StatusCreated {
		t.Fatalf("create task 3 failed: %d: %s", code3, body3)
	}

	claimCode, claimBody := post(t, srv.URL+"/tasks/claim", map[string]string{
		"worker":  "w1",
		"project": "x",
	})
	if claimCode != http.StatusOK {
		t.Fatalf("claim failed: %d: %s", claimCode, claimBody)
	}
	var claimResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(claimBody, &claimResp); err != nil {
		t.Fatalf("unmarshal claim failed: %v", err)
	}
	if claimResp.ID != res1.ID {
		t.Fatalf("expected claimed task to be task 1 (%q), got %q", res1.ID, claimResp.ID)
	}

	type taskItem struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Project string `json:"project"`
		Body    string `json:"body"`
	}

	code, body := do(t, http.MethodGet, srv.URL+"/tasks?project=x", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks?project=x expected 200, got %d: %s", code, body)
	}
	var tasksX []taskItem
	if err := json.Unmarshal(body, &tasksX); err != nil {
		t.Fatalf("unmarshal tasks failed: %v: %s", err, body)
	}
	if len(tasksX) != 2 {
		t.Fatalf("expected 2 tasks for project x, got %d", len(tasksX))
	}
	for _, item := range tasksX {
		if item.Project != "x" {
			t.Fatalf("expected project 'x', got %q", item.Project)
		}
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks?project=y", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks?project=y expected 200, got %d: %s", code, body)
	}
	var tasksY []taskItem
	if err := json.Unmarshal(body, &tasksY); err != nil {
		t.Fatalf("unmarshal tasks failed: %v: %s", err, body)
	}
	if len(tasksY) != 1 {
		t.Fatalf("expected 1 task for project y, got %d", len(tasksY))
	}
	if tasksY[0].Project != "y" {
		t.Fatalf("expected project 'y', got %q", tasksY[0].Project)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks?project=x&status=leased", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks?project=x&status=leased expected 200, got %d: %s", code, body)
	}
	var leasedX []taskItem
	if err := json.Unmarshal(body, &leasedX); err != nil {
		t.Fatalf("unmarshal leased tasks failed: %v: %s", err, body)
	}
	if len(leasedX) != 1 {
		t.Fatalf("expected 1 leased task for project x, got %d", len(leasedX))
	}
	if leasedX[0].ID != res1.ID {
		t.Fatalf("expected leased task id %q, got %q", res1.ID, leasedX[0].ID)
	}
	if leasedX[0].Project != "x" || leasedX[0].Status != "leased" {
		t.Fatalf("unexpected task fields: %+v", leasedX[0])
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks?project=x&status=pending", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks?project=x&status=pending expected 200, got %d: %s", code, body)
	}
	var pendingX []taskItem
	if err := json.Unmarshal(body, &pendingX); err != nil {
		t.Fatalf("unmarshal pending tasks failed: %v: %s", err, body)
	}
	if len(pendingX) != 1 {
		t.Fatalf("expected 1 pending task for project x, got %d", len(pendingX))
	}
	if pendingX[0].ID != res2.ID {
		t.Fatalf("expected pending task id %q, got %q", res2.ID, pendingX[0].ID)
	}
	if pendingX[0].Project != "x" || pendingX[0].Status != "pending" {
		t.Fatalf("unexpected task fields: %+v", pendingX[0])
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks?project=x&status=done", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks?project=x&status=done expected 200, got %d: %s", code, body)
	}
	if strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("expected [], got %q", string(body))
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks?project=nonexistent", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks?project=nonexistent expected 200, got %d: %s", code, body)
	}
	if strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("expected [], got %q", string(body))
	}
	code, body = do(t, http.MethodGet, srv.URL+"/tasks?project=*", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks?project=* expected 200, got %d: %s", code, body)
	}
	var tasksWildcard []taskItem
	if err := json.Unmarshal(body, &tasksWildcard); err != nil {
		t.Fatalf("unmarshal wildcard tasks failed: %v: %s", err, body)
	}
	if len(tasksWildcard) != 3 {
		t.Fatalf("expected 3 tasks for project *, got %d", len(tasksWildcard))
	}
}

func TestDeleteExpiredLeasedTask(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	code, body := post(t, srv.URL+"/tasks", map[string]any{
		"body":    "exp-del",
		"project": "p1",
	})
	if code != http.StatusCreated {
		t.Fatalf("create task failed: %d: %s", code, body)
	}
	var res struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("unmarshal create response failed: %v", err)
	}

	code, body = post(t, srv.URL+"/tasks/claim", map[string]string{
		"worker":  "w1",
		"project": "p1",
	})
	if code != http.StatusOK {
		t.Fatalf("claim failed: %d: %s", code, body)
	}

	code, body = do(t, http.MethodDelete, srv.URL+"/tasks/"+res.ID, nil)
	if code != http.StatusConflict {
		t.Fatalf("DELETE on active leased task expected 409, got %d: %s", code, body)
	}

	if _, err := db.Exec("UPDATE tasks SET lease_expires=0 WHERE id = ?", res.ID); err != nil {
		t.Fatalf("expire lease failed: %v", err)
	}

	code, body = do(t, http.MethodDelete, srv.URL+"/tasks/"+res.ID, nil)
	if code != http.StatusNoContent {
		t.Fatalf("DELETE on expired leased task expected 204, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodDelete, srv.URL+"/tasks/"+res.ID, nil)
	if code != http.StatusNotFound {
		t.Fatalf("second DELETE expected 404, got %d: %s", code, body)
	}
}

func TestGetTask(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	code, body := do(t, http.MethodGet, srv.URL+"/tasks/missing-id", nil)
	if code != http.StatusNotFound {
		t.Fatalf("GET /tasks/missing-id expected 404, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks", map[string]any{
		"body":    "inspect me",
		"project": "p1",
	})
	if code != http.StatusCreated {
		t.Fatalf("POST /tasks expected 201, got %d: %s", code, body)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal created failed: %v", err)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks/"+created.ID, nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks/%s expected 200, got %d: %s", created.ID, code, body)
	}

	var task struct {
		ID           string          `json:"id"`
		AssetPath    string          `json:"asset_path"`
		Status       string          `json:"status"`
		Worker       string          `json:"worker"`
		LeaseExpires int64           `json:"lease_expires"`
		Priority     int             `json:"priority"`
		Body         string          `json:"body"`
		Primitives   json.RawMessage `json:"primitives"`
		Project      string          `json:"project"`
	}
	if err := json.Unmarshal(body, &task); err != nil {
		t.Fatalf("unmarshal task failed: %v", err)
	}
	if task.ID != created.ID {
		t.Fatalf("expected id %q, got %q", created.ID, task.ID)
	}
	if task.Body != "inspect me" {
		t.Fatalf("expected body 'inspect me', got %q", task.Body)
	}
	if task.Status != "pending" {
		t.Fatalf("expected status 'pending', got %q", task.Status)
	}
	if task.Project != "p1" {
		t.Fatalf("expected project 'p1', got %q", task.Project)
	}
	if task.Priority != 0 {
		t.Fatalf("expected priority 0, got %d", task.Priority)
	}
	if task.AssetPath != "" {
		t.Fatalf("expected empty asset_path, got %q", task.AssetPath)
	}
	if task.Worker != "" {
		t.Fatalf("expected empty worker, got %q", task.Worker)
	}
	if task.Primitives != nil && string(task.Primitives) != "null" {
		t.Fatalf("expected null primitives, got %s", string(task.Primitives))
	}

	code, _ = post(t, srv.URL+"/tasks/claim", map[string]string{
		"worker":  "w1",
		"project": "p1",
	})
	if code != http.StatusOK {
		t.Fatalf("claim failed: %d", code)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks/"+created.ID, nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks/%s expected 200, got %d", created.ID, code)
	}
	if err := json.Unmarshal(body, &task); err != nil {
		t.Fatalf("unmarshal task failed: %v", err)
	}
	if task.Status != "leased" {
		t.Fatalf("expected status 'leased', got %q", task.Status)
	}
	if task.Worker != "w1" {
		t.Fatalf("expected worker 'w1', got %q", task.Worker)
	}
	if task.LeaseExpires <= 0 {
		t.Fatalf("expected lease_expires > 0, got %d", task.LeaseExpires)
	}

	code, _ = post(t, srv.URL+"/tasks/"+created.ID+"/done", map[string]any{
		"worker":     "w1",
		"primitives": map[string]string{"result": "ok"},
	})
	if code != http.StatusNoContent {
		t.Fatalf("done failed: %d", code)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks/"+created.ID, nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks/%s expected 200, got %d", created.ID, code)
	}
	if err := json.Unmarshal(body, &task); err != nil {
		t.Fatalf("unmarshal task failed: %v", err)
	}
	if task.Status != "done" {
		t.Fatalf("expected status 'done', got %q", task.Status)
	}
	var primMap map[string]any
	if err := json.Unmarshal(task.Primitives, &primMap); err != nil {
		t.Fatalf("unmarshal primitives failed: %v", err)
	}
	if primMap["result"] != "ok" {
		t.Fatalf("expected result 'ok', got %v", primMap["result"])
	}
}

func TestValidatePositiveLease(t *testing.T) {
	for _, val := range []string{"0", "-5"} {
		_, err := parseFlags([]string{"-lease", val})
		if err == nil {
			t.Fatalf("expected -lease %s to fail, but got nil error", val)
		}
		if !strings.Contains(strings.ToLower(err.Error()), "lease") {
			t.Fatalf("expected error for -lease %s to mention 'lease', got: %v", val, err)
		}
	}

	cfg, err := parseFlags([]string{"-lease", "60"})
	if err != nil {
		t.Fatalf("unexpected error for valid lease: %v", err)
	}
	if cfg.lease != 60 {
		t.Fatalf("expected lease 60, got %d", cfg.lease)
	}
}

func TestOpenDBCreateParentDir(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sub", "nested", "taskd.db")
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("openDB failed on nested path: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(filepath.Dir(dbPath)); err != nil {
		t.Fatalf("expected parent dir to exist: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT count(*) FROM tasks").Scan(&count); err != nil {
		t.Fatalf("failed to query tasks table: %v", err)
	}
}

func TestOpenDBInMemoryURI(t *testing.T) {
	tempDir := t.TempDir()
	nestedMem := filepath.Join(tempDir, "nested_mem", "db1")
	memURI := "file:" + nestedMem + "?mode=memory&cache=shared"
	db, err := openDB(memURI)
	if err != nil {
		t.Fatalf("openDB failed on memory URI: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(filepath.Dir(nestedMem)); !os.IsNotExist(err) {
		t.Fatalf("in-memory database should not create parent directory")
	}
}

func TestGetTasksLimitValidation(t *testing.T) {
	db, err := openDB(":memory:")
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	for _, invalid := range []string{"0", "-1", "-10", "invalid", "abc", "1001", "1000000", ""} {
		code, _ := do(t, http.MethodGet, srv.URL+"/tasks?limit="+invalid, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("GET /tasks?limit=%s expected 400, got %d", invalid, code)
		}
	}

	for _, valid := range []string{"1", "500", "1000"} {
		code, _ := do(t, http.MethodGet, srv.URL+"/tasks?limit="+valid, nil)
		if code != http.StatusOK {
			t.Fatalf("GET /tasks?limit=%s expected 200, got %d", valid, code)
		}
	}

	code, _ := do(t, http.MethodGet, srv.URL+"/tasks", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks without limit expected 200, got %d", code)
	}

	for i := 0; i < 5; i++ {
		post(t, srv.URL+"/tasks", map[string]any{
			"asset_path": fmt.Sprintf("task-%d.gltf", i),
			"project":    "test",
		})
	}

	code, body := do(t, http.MethodGet, srv.URL+"/tasks?limit=2", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks?limit=2 expected 200, got %d", code)
	}
	var tasks []taskItem
	if err := json.Unmarshal(body, &tasks); err != nil {
		t.Fatalf("unmarshal tasks failed: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestMigrationV2LegacyV1(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v1.db")
	db0, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("setup open: %v", err)
	}
	setup := `CREATE TABLE tasks (id TEXT PRIMARY KEY, asset_path TEXT NOT NULL, status TEXT DEFAULT 'pending', worker TEXT, lease_expires INTEGER, primitives JSON);
CREATE INDEX idx_tasks_claim ON tasks (status, lease_expires);
INSERT INTO tasks (id, asset_path) VALUES ('legacy-1','models/car.glb');
PRAGMA user_version = 1;`
	if _, err := db0.Exec(setup); err != nil {
		db0.Close()
		t.Fatalf("setup exec: %v", err)
	}
	db0.Close()

	db, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("openDB legacy v1 failed: %v", err)
	}

	srv := httptest.NewServer(newHandler(db, 30))
	code, body := do(t, http.MethodGet, srv.URL+"/tasks", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks expected 200, got %d", code)
	}
	var tasks []taskItem
	if err := json.Unmarshal(body, &tasks); err != nil {
		t.Fatalf("unmarshal tasks failed: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "legacy-1" || tasks[0].Priority != 0 || tasks[0].Body != "" {
		t.Fatalf("unexpected tasks: %+v", tasks)
	}

	var userVersion int
	if err := db.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
		t.Fatalf("query user_version: %v", err)
	}
	if userVersion != 2 {
		t.Fatalf("expected user_version 2, got %d", userVersion)
	}

	var queueIdxCount int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name='idx_tasks_queue'").Scan(&queueIdxCount); err != nil {
		t.Fatalf("query idx_tasks_queue: %v", err)
	}
	if queueIdxCount != 1 {
		t.Fatalf("expected idx_tasks_queue count 1, got %d", queueIdxCount)
	}

	var claimIdxCount int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name='idx_tasks_claim'").Scan(&claimIdxCount); err != nil {
		t.Fatalf("query idx_tasks_claim: %v", err)
	}
	if claimIdxCount != 0 {
		t.Fatalf("expected idx_tasks_claim count 0, got %d", claimIdxCount)
	}

	srv.Close()
	db.Close()

	db2, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("reopen legacy v1 after migration failed: %v", err)
	}
	defer db2.Close()

	srv2 := httptest.NewServer(newHandler(db2, 30))
	defer srv2.Close()

	code, body = do(t, http.MethodGet, srv2.URL+"/tasks", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks on restarted db expected 200, got %d", code)
	}
	tasks = nil
	if err := json.Unmarshal(body, &tasks); err != nil {
		t.Fatalf("unmarshal tasks on restart failed: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "legacy-1" {
		t.Fatalf("unexpected tasks on restart: %+v", tasks)
	}
}

func TestMigrationV2RollbackOnFailure(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v1-fail.db")
	db0, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("setup open: %v", err)
	}
	setup := `CREATE TABLE tasks (id TEXT PRIMARY KEY, asset_path TEXT NOT NULL, status TEXT DEFAULT 'pending', worker TEXT, lease_expires INTEGER, primitives JSON);
CREATE INDEX idx_tasks_claim ON tasks (status, lease_expires);
CREATE TABLE idx_tasks_queue (x INT);
INSERT INTO tasks (id, asset_path) VALUES ('legacy-1','models/car.glb');
PRAGMA user_version = 1;`
	if _, err := db0.Exec(setup); err != nil {
		db0.Close()
		t.Fatalf("setup exec: %v", err)
	}
	db0.Close()

	db, err := openDB(dbPath)
	if err == nil {
		db.Close()
		t.Fatalf("expected openDB to fail due to conflicting idx_tasks_queue table")
	}

	dbCheck, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen check: %v", err)
	}
	defer dbCheck.Close()

	var userVersion int
	if err := dbCheck.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
		t.Fatalf("query user_version: %v", err)
	}
	if userVersion != 1 {
		t.Fatalf("expected user_version 1 after rollback, got %d", userVersion)
	}

	var hasProject int
	if err := dbCheck.QueryRow("SELECT count(*) FROM pragma_table_info('tasks') WHERE name='project'").Scan(&hasProject); err != nil {
		t.Fatalf("query project col: %v", err)
	}
	if hasProject != 0 {
		t.Fatalf("expected no project column after rollback, got %d", hasProject)
	}

	var hasBody int
	if err := dbCheck.QueryRow("SELECT count(*) FROM pragma_table_info('tasks') WHERE name='body'").Scan(&hasBody); err != nil {
		t.Fatalf("query body col: %v", err)
	}
	if hasBody != 0 {
		t.Fatalf("expected no body column after rollback, got %d", hasBody)
	}

	var hasPriority int
	if err := dbCheck.QueryRow("SELECT count(*) FROM pragma_table_info('tasks') WHERE name='priority'").Scan(&hasPriority); err != nil {
		t.Fatalf("query priority col: %v", err)
	}
	if hasPriority != 0 {
		t.Fatalf("expected no priority column after rollback, got %d", hasPriority)
	}

	var claimIdxCount int
	if err := dbCheck.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name='idx_tasks_claim'").Scan(&claimIdxCount); err != nil {
		t.Fatalf("query idx_tasks_claim: %v", err)
	}
	if claimIdxCount != 1 {
		t.Fatalf("expected idx_tasks_claim count 1 after rollback, got %d", claimIdxCount)
	}
}

func TestDeleteDoneTaskRequiresForce(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	code, body := post(t, srv.URL+"/tasks", map[string]string{
		"body":    "finished",
		"project": "del",
	})
	if code != http.StatusCreated {
		t.Fatalf("create failed: %d: %s", code, body)
	}
	var res struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("unmarshal create response failed: %v", err)
	}

	code, body = post(t, srv.URL+"/tasks/claim", map[string]string{
		"worker":  "w1",
		"project": "del",
	})
	if code != http.StatusOK {
		t.Fatalf("claim failed: %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/"+res.ID+"/done", map[string]any{
		"worker": "w1",
		"primitives": map[string]string{
			"result": "expensive output",
		},
	})
	if code != http.StatusNoContent {
		t.Fatalf("done failed: %d: %s", code, body)
	}

	code, body = do(t, http.MethodDelete, srv.URL+"/tasks/"+res.ID, nil)
	if code != http.StatusConflict {
		t.Fatalf("plain DELETE on done task expected 409, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks/"+res.ID, nil)
	if code != http.StatusOK {
		t.Fatalf("GET on done task expected 200, got %d: %s", code, body)
	}
	var check struct {
		Primitives struct {
			Result string `json:"result"`
		} `json:"primitives"`
	}
	if err := json.Unmarshal(body, &check); err != nil {
		t.Fatalf("unmarshal GET task failed: %v", err)
	}
	if check.Primitives.Result != "expensive output" {
		t.Fatalf("primitives result mismatch, got: %s", check.Primitives.Result)
	}

	code, body = do(t, http.MethodDelete, srv.URL+"/tasks/"+res.ID+"?force=1", nil)
	if code != http.StatusNoContent {
		t.Fatalf("DELETE with force=1 expected 204, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks/"+res.ID, nil)
	if code != http.StatusNotFound {
		t.Fatalf("GET after forced delete expected 404, got %d: %s", code, body)
	}
}

func TestProjects(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	code, body := do(t, http.MethodGet, srv.URL+"/projects", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /projects on empty db expected 200, got %d: %s", code, body)
	}
	if strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("GET /projects on empty db expected [], got %q", string(body))
	}

	code, body = post(t, srv.URL+"/tasks", map[string]string{"body": "t1", "project": "beta"})
	if code != http.StatusCreated {
		t.Fatalf("create t1 failed: %d: %s", code, body)
	}
	code, body = post(t, srv.URL+"/tasks", map[string]string{"body": "t2", "project": "alpha"})
	if code != http.StatusCreated {
		t.Fatalf("create t2 failed: %d: %s", code, body)
	}
	code, body = post(t, srv.URL+"/tasks", map[string]string{"body": "t3", "project": "alpha"})
	if code != http.StatusCreated {
		t.Fatalf("create t3 failed: %d: %s", code, body)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/projects", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /projects expected 200, got %d: %s", code, body)
	}
	var projects []string
	if err := json.Unmarshal(body, &projects); err != nil {
		t.Fatalf("unmarshal projects failed: %v", err)
	}
	if len(projects) != 2 || projects[0] != "alpha" || projects[1] != "beta" {
		t.Fatalf("expected [alpha beta], got %v", projects)
	}

	if _, err := db.Exec("INSERT INTO tasks (id, body, project) VALUES ('raw1', 'empty proj', '')"); err != nil {
		t.Fatalf("insert empty project task failed: %v", err)
	}
	code, body = do(t, http.MethodGet, srv.URL+"/projects", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /projects expected 200, got %d: %s", code, body)
	}
	if err := json.Unmarshal(body, &projects); err != nil {
		t.Fatalf("unmarshal projects failed: %v", err)
	}
	if len(projects) != 2 || projects[0] != "alpha" || projects[1] != "beta" {
		t.Fatalf("expected [alpha beta], got %v", projects)
	}
}

func TestListTasksOffset(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	code, body := post(t, srv.URL+"/tasks", map[string]any{
		"body":     "first",
		"priority": 2,
		"project":  "page-test",
	})
	if code != http.StatusCreated {
		t.Fatalf("create first failed: %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks", map[string]any{
		"body":     "second",
		"priority": 1,
		"project":  "page-test",
	})
	if code != http.StatusCreated {
		t.Fatalf("create second failed: %d: %s", code, body)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks?project=page-test&limit=1&offset=1", nil)
	if code != http.StatusOK {
		t.Fatalf("GET with offset failed: %d: %s", code, body)
	}
	var items []taskItem
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("unmarshal offset items failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Body != "second" {
		t.Fatalf("expected second, got %s", items[0].Body)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks?project=page-test&limit=1&offset=0", nil)
	if code != http.StatusOK {
		t.Fatalf("GET offset=0 failed: %d: %s", code, body)
	}
	var page0 []taskItem
	if err := json.Unmarshal(body, &page0); err != nil {
		t.Fatalf("unmarshal page0 items failed: %v", err)
	}
	if len(page0) != 1 || page0[0].Body != "first" {
		t.Fatalf("expected page0 to return first, got %v", page0)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks?project=page-test&limit=1&offset=2", nil)
	if code != http.StatusOK {
		t.Fatalf("GET offset=2 failed: %d: %s", code, body)
	}
	var page2 []taskItem
	if err := json.Unmarshal(body, &page2); err != nil {
		t.Fatalf("unmarshal page2 items failed: %v", err)
	}
	if len(page2) != 0 {
		t.Fatalf("expected page2 to be empty, got %v", page2)
	}

	for _, invalid := range []string{"-1", "-10", "invalid", "abc"} {
		code, _ = do(t, http.MethodGet, srv.URL+"/tasks?offset="+invalid, nil)
		if code != http.StatusBadRequest {
			t.Fatalf("GET /tasks?offset=%s expected 400, got %d", invalid, code)
		}
	}
}

func TestPatchProjectAndAssetPath(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	code, body := post(t, srv.URL+"/tasks", map[string]any{
		"body":    "move test",
		"project": "old-p",
	})
	if code != http.StatusCreated {
		t.Fatalf("create task failed: %d: %s", code, body)
	}
	var res struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("unmarshal create response failed: %v", err)
	}

	code, body = do(t, http.MethodPatch, srv.URL+"/tasks/"+res.ID, map[string]any{
		"project": "",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("PATCH empty project expected 400, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodPatch, srv.URL+"/tasks/"+res.ID, map[string]any{
		"body":    "updated",
		"project": "",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("PATCH empty project with body expected 400, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodPatch, srv.URL+"/tasks/"+res.ID, map[string]any{
		"project": "*",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("PATCH wildcard project expected 400, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodPatch, srv.URL+"/tasks/"+res.ID, map[string]any{
		"body":    "updated",
		"project": "*",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("PATCH wildcard project with body expected 400, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodPatch, srv.URL+"/tasks/"+res.ID, map[string]any{
		"project":    "new-p",
		"asset_path": "docs/spec.md",
	})
	if code != http.StatusNoContent {
		t.Fatalf("PATCH project and asset_path expected 204, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks?project=new-p", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks?project=new-p expected 200, got %d: %s", code, body)
	}

	var items []struct {
		ID        string `json:"id"`
		Project   string `json:"project"`
		AssetPath string `json:"asset_path"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("unmarshal list failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 task in new-p, got %d", len(items))
	}
	if items[0].ID != res.ID || items[0].Project != "new-p" || items[0].AssetPath != "docs/spec.md" {
		t.Fatalf("unexpected task data: %+v", items[0])
	}

	code, body = do(t, http.MethodPatch, srv.URL+"/tasks/"+res.ID, map[string]any{
		"asset_path": "docs/updated.md",
	})
	if code != http.StatusNoContent {
		t.Fatalf("PATCH asset_path only expected 204, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodPatch, srv.URL+"/tasks/"+res.ID, map[string]any{
		"project": "final-p",
	})
	if code != http.StatusNoContent {
		t.Fatalf("PATCH project only expected 204, got %d: %s", code, body)
	}

	var pVal, aVal string
	err = db.QueryRow("SELECT project, asset_path FROM tasks WHERE id = ?", res.ID).Scan(&pVal, &aVal)
	if err != nil {
		t.Fatalf("query db failed: %v", err)
	}
	if pVal != "final-p" {
		t.Fatalf("expected project %q, got %q", "final-p", pVal)
	}
	if aVal != "docs/updated.md" {
		t.Fatalf("expected asset_path %q, got %q", "docs/updated.md", aVal)
	}

	code, body = do(t, http.MethodPatch, srv.URL+"/tasks/"+res.ID, map[string]any{
		"asset_path": "",
	})
	if code != http.StatusNoContent {
		t.Fatalf("PATCH clear asset_path expected 204, got %d: %s", code, body)
	}
	err = db.QueryRow("SELECT asset_path FROM tasks WHERE id = ?", res.ID).Scan(&aVal)
	if err != nil {
		t.Fatalf("query db failed: %v", err)
	}
	if aVal != "" {
		t.Fatalf("expected asset_path cleared, got %q", aVal)
	}
}

func TestOpenDBHashPath(t *testing.T) {
	tempDir := t.TempDir()
	dbDir := filepath.Join(tempDir, "test#dir")
	dbPath := filepath.Join(dbDir, "taskd.db")
	truncatedPath := filepath.Join(tempDir, "test")

	db, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected database at %s, got error: %v", dbPath, err)
	}
	if _, err := os.Stat(truncatedPath); !os.IsNotExist(err) {
		t.Fatalf("expected %s to not exist, but it exists", truncatedPath)
	}

	var count int
	if err := db.QueryRow("SELECT count(*) FROM tasks").Scan(&count); err != nil {
		t.Fatalf("query tasks failed: %v", err)
	}

	qDir := filepath.Join(tempDir, "test?dir")
	qPath := filepath.Join(qDir, "taskd.db")
	qDB, err := openDB(qPath)
	if err != nil {
		t.Fatalf("openDB with question mark failed: %v", err)
	}
	defer qDB.Close()

	if _, err := os.Stat(qPath); err != nil {
		t.Fatalf("expected database at %s, got error: %v", qPath, err)
	}
	if err := qDB.QueryRow("SELECT count(*) FROM tasks").Scan(&count); err != nil {
		t.Fatalf("query tasks with question mark failed: %v", err)
	}
}
