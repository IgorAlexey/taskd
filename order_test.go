package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestTasksOrder(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	resp1, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(`{"body":"early","project":"sort-test"}`))
	if err != nil {
		t.Fatalf("post early: %v", err)
	}
	resp1.Body.Close()

	resp2, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(`{"body":"late","project":"sort-test"}`))
	if err != nil {
		t.Fatalf("post late: %v", err)
	}
	resp2.Body.Close()

	getDesc, err := http.Get(srv.URL + "/tasks?project=sort-test&order=desc")
	if err != nil {
		t.Fatalf("get desc: %v", err)
	}
	defer getDesc.Body.Close()
	if getDesc.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", getDesc.StatusCode)
	}
	var descTasks []taskItem
	if err := json.NewDecoder(getDesc.Body).Decode(&descTasks); err != nil {
		t.Fatalf("decode desc: %v", err)
	}
	if len(descTasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(descTasks))
	}
	if descTasks[0].Body != "late" || descTasks[1].Body != "early" {
		t.Fatalf("expected [late, early], got [%s, %s]", descTasks[0].Body, descTasks[1].Body)
	}

	getAsc, err := http.Get(srv.URL + "/tasks?project=sort-test&order=asc")
	if err != nil {
		t.Fatalf("get asc: %v", err)
	}
	defer getAsc.Body.Close()
	var ascTasks []taskItem
	if err := json.NewDecoder(getAsc.Body).Decode(&ascTasks); err != nil {
		t.Fatalf("decode asc: %v", err)
	}
	if len(ascTasks) != 2 || ascTasks[0].Body != "early" || ascTasks[1].Body != "late" {
		t.Fatalf("expected [early, late], got: %+v", ascTasks)
	}

	getDefault, err := http.Get(srv.URL + "/tasks?project=sort-test")
	if err != nil {
		t.Fatalf("get default: %v", err)
	}
	defer getDefault.Body.Close()
	var defaultTasks []taskItem
	if err := json.NewDecoder(getDefault.Body).Decode(&defaultTasks); err != nil {
		t.Fatalf("decode default: %v", err)
	}
	if len(defaultTasks) != 2 || defaultTasks[0].Body != "early" || defaultTasks[1].Body != "late" {
		t.Fatalf("expected default [early, late], got: %+v", defaultTasks)
	}

	getInvalid, err := http.Get(srv.URL + "/tasks?order=invalid")
	if err != nil {
		t.Fatalf("get invalid: %v", err)
	}
	defer getInvalid.Body.Close()
	if getInvalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid order, got %d", getInvalid.StatusCode)
	}
	var errResp apiError
	if err := json.NewDecoder(getInvalid.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode errResp: %v", err)
	}
	if errResp.Error != "invalid order" {
		t.Fatalf("expected 'invalid order', got %q", errResp.Error)
	}

	getInvalidWithAfter, err := http.Get(srv.URL + "/tasks?order=garbage&after=foo")
	if err != nil {
		t.Fatalf("get invalid with after: %v", err)
	}
	defer getInvalidWithAfter.Body.Close()
	if getInvalidWithAfter.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", getInvalidWithAfter.StatusCode)
	}
	var errAfterResp apiError
	if err := json.NewDecoder(getInvalidWithAfter.Body).Decode(&errAfterResp); err != nil {
		t.Fatalf("decode errAfterResp: %v", err)
	}
	if errAfterResp.Error != "invalid order" {
		t.Fatalf("expected 'invalid order' before 'invalid after', got %q", errAfterResp.Error)
	}
	p1Resp, err := http.Get(srv.URL + "/tasks?project=sort-test&order=desc&limit=1")
	if err != nil {
		t.Fatalf("get page 1: %v", err)
	}
	defer p1Resp.Body.Close()
	cursor := p1Resp.Header.Get("X-Next-Cursor")
	if cursor == "" {
		t.Fatalf("expected next cursor on limit=1")
	}
	var p1Tasks []taskItem
	if err := json.NewDecoder(p1Resp.Body).Decode(&p1Tasks); err != nil {
		t.Fatalf("decode page 1: %v", err)
	}
	if len(p1Tasks) != 1 || p1Tasks[0].Body != "late" {
		t.Fatalf("expected page 1 [late], got: %+v", p1Tasks)
	}

	p2Resp, err := http.Get(srv.URL + "/tasks?project=sort-test&order=desc&limit=1&after=" + cursor)
	if err != nil {
		t.Fatalf("get page 2: %v", err)
	}
	defer p2Resp.Body.Close()
	var p2Tasks []taskItem
	if err := json.NewDecoder(p2Resp.Body).Decode(&p2Tasks); err != nil {
		t.Fatalf("decode page 2: %v", err)
	}
	if len(p2Tasks) != 1 || p2Tasks[0].Body != "early" {
		t.Fatalf("expected page 2 [early], got: %+v", p2Tasks)
	}

	pDefaultResp, err := http.Get(srv.URL + "/tasks?project=sort-test&limit=1")
	if err != nil {
		t.Fatalf("get default page 1: %v", err)
	}
	defer pDefaultResp.Body.Close()
	defaultCursor := pDefaultResp.Header.Get("X-Next-Cursor")
	if defaultCursor == "" {
		t.Fatalf("expected next cursor on default limit=1")
	}
	pAscResp, err := http.Get(srv.URL + "/tasks?project=sort-test&order=asc&limit=1&after=" + defaultCursor)
	if err != nil {
		t.Fatalf("get asc page 2 with default cursor: %v", err)
	}
	defer pAscResp.Body.Close()
	if pAscResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for compatible cursor between default and order=asc, got %d", pAscResp.StatusCode)
	}
}

func TestClaimOrderFIFO(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	zBody, _ := json.Marshal(map[string]any{"id": "z-task", "body": "z", "priority": 3, "project": "testfifo"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(zBody))
	if err != nil {
		t.Fatalf("create z-task failed: %v", err)
	}
	resp.Body.Close()

	aBody, _ := json.Marshal(map[string]any{"id": "a-task", "body": "a", "priority": 3, "project": "testfifo"})
	resp, err = http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(aBody))
	if err != nil {
		t.Fatalf("create a-task failed: %v", err)
	}
	resp.Body.Close()

	claimPayload, _ := json.Marshal(map[string]string{"worker": "w1", "project": "testfifo"})

	resp1, err := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimPayload))
	if err != nil {
		t.Fatalf("first claim failed: %v", err)
	}
	defer resp1.Body.Close()
	var claim1 struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp1.Body).Decode(&claim1); err != nil {
		t.Fatalf("decode first claim: %v", err)
	}
	if claim1.ID != "z-task" {
		t.Fatalf("first claimed = %q, want z-task", claim1.ID)
	}

	resp2, err := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimPayload))
	if err != nil {
		t.Fatalf("second claim failed: %v", err)
	}
	defer resp2.Body.Close()
	var claim2 struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&claim2); err != nil {
		t.Fatalf("decode second claim: %v", err)
	}
	if claim2.ID != "a-task" {
		t.Fatalf("second claimed = %q, want a-task", claim2.ID)
	}

	var (
		selectID, ord, from int
		detail              string
	)
	err = db.ro.QueryRow(`EXPLAIN QUERY PLAN SELECT id FROM tasks WHERE status='pending' AND project='testfifo' ORDER BY priority ASC, created_at ASC LIMIT 1`).
		Scan(&selectID, &ord, &from, &detail)
	if err != nil {
		t.Fatalf("explain query plan: %v", err)
	}
	if strings.Contains(detail, "TEMP B-TREE") {
		t.Fatalf("claim query uses temp b-tree: %s", detail)
	}
	if !strings.Contains(detail, "idx_tasks_pending_project") {
		t.Fatalf("claim query does not use index: %s", detail)
	}
}

func TestSortCursor(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	for _, task := range []map[string]any{
		{"id": "task-1", "project": "demo", "priority": 1, "body": "first"},
		{"id": "task-2", "project": "demo", "priority": 2, "body": "second"},
		{"id": "task-3", "project": "demo", "priority": 3, "body": "third"},
	} {
		payload, err := json.Marshal(task)
		if err != nil {
			t.Fatalf("marshal task failed: %v", err)
		}
		resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("create task failed: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			t.Fatalf("create task status: %d", resp.StatusCode)
		}
	}

	sortResp, err := http.Get(srv.URL + "/tasks?sort=priority&limit=1")
	if err != nil {
		t.Fatalf("GET with sort failed: %v", err)
	}
	defer sortResp.Body.Close()
	if sortResp.StatusCode != http.StatusOK {
		t.Fatalf("GET with sort status: %d, want 200", sortResp.StatusCode)
	}
	if cursor := sortResp.Header.Get("X-Next-Cursor"); cursor != "" {
		t.Fatalf("expected no X-Next-Cursor on sort query, got %q", cursor)
	}

	defaultResp, err := http.Get(srv.URL + "/tasks?limit=1")
	if err != nil {
		t.Fatalf("GET default failed: %v", err)
	}
	defer defaultResp.Body.Close()
	if defaultResp.StatusCode != http.StatusOK {
		t.Fatalf("GET default status: %d, want 200", defaultResp.StatusCode)
	}
	defaultCursor := defaultResp.Header.Get("X-Next-Cursor")
	if defaultCursor == "" {
		t.Fatalf("expected X-Next-Cursor on limit=1 query without sort")
	}

	combineResp, err := http.Get(srv.URL + "/tasks?sort=priority&after=" + defaultCursor)
	if err != nil {
		t.Fatalf("GET combined sort and after failed: %v", err)
	}
	defer combineResp.Body.Close()
	if combineResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 when combining sort and after, got %d", combineResp.StatusCode)
	}
}

func TestSortEffectiveStatusAndWorker(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 2))
	defer srv.Close()

	postJSON := func(url string, payload any) *http.Response {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload failed: %v", err)
		}
		resp, err := http.Post(srv.URL+url, "application/json", bytes.NewReader(data))
		if err != nil {
			t.Fatalf("POST %s failed: %v", url, err)
		}
		return resp
	}

	r1 := postJSON("/tasks", map[string]any{"id": "t1", "body": "first", "project": "p"})
	r1.Body.Close()
	if r1.StatusCode != http.StatusCreated {
		t.Fatalf("create t1 status = %d, want 201", r1.StatusCode)
	}
	c1 := postJSON("/tasks/claim", map[string]any{"worker": "w2"})
	if c1.StatusCode != http.StatusOK {
		t.Fatalf("claim t1 status = %d, want 200", c1.StatusCode)
	}
	c1.Body.Close()

	if _, err := db.rw.Exec("UPDATE tasks SET lease_expires = unixepoch() - 1 WHERE id = 't1'"); err != nil {
		t.Fatalf("expire lease failed: %v", err)
	}
	r2 := postJSON("/tasks", map[string]any{"id": "t2", "body": "second", "project": "p"})
	r2.Body.Close()
	if r2.StatusCode != http.StatusCreated {
		t.Fatalf("create t2 status = %d, want 201", r2.StatusCode)
	}

	c2 := postJSON("/tasks/claim", map[string]any{"worker": "w1"})
	c2.Body.Close()
	if c2.StatusCode != http.StatusOK {
		t.Fatalf("claim t2 status = %d, want 200", c2.StatusCode)
	}

	type listTask struct {
		ID           string `json:"id"`
		Status       string `json:"status"`
		Worker       string `json:"worker"`
		LeaseExpires int64  `json:"lease_expires"`
	}
	getTasks := func(query string) []listTask {
		resp, err := http.Get(srv.URL + "/tasks?" + query)
		if err != nil {
			t.Fatalf("GET /tasks?%s failed: %v", query, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /tasks?%s status = %d, want 200", query, resp.StatusCode)
		}
		var tasks []listTask
		if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
			t.Fatalf("decode tasks failed: %v", err)
		}
		return tasks
	}

	statusDesc := getTasks("sort=status&order=desc")
	if len(statusDesc) < 2 {
		t.Fatalf("got %d tasks, want at least 2", len(statusDesc))
	}
	if statusDesc[0].Status != "pending" || statusDesc[1].Status != "leased" {
		t.Fatalf("sort=status&order=desc got statuses [%s, %s], want [pending, leased]", statusDesc[0].Status, statusDesc[1].Status)
	}

	workerAsc := getTasks("sort=worker&order=asc")
	if len(workerAsc) < 2 {
		t.Fatalf("got %d tasks, want at least 2", len(workerAsc))
	}
	if workerAsc[0].Worker != "" || workerAsc[1].Worker != "w1" {
		t.Fatalf("sort=worker&order=asc got workers [%q, %q], want [\"\", \"w1\"]", workerAsc[0].Worker, workerAsc[1].Worker)
	}
}
