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
		"body": "only body",
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
		requiredKeys := []string{"id", "asset_path", "status", "worker", "lease_expires", "priority", "body", "primitives"}
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

func TestDeleteTask(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	code, body := post(t, srv.URL+"/tasks", map[string]any{
		"body": "to delete",
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
