package taskd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaimHonorsMaxClaimsAfterRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.db")

	db1, err := openDB(dbPath, 0)
	if err != nil {
		t.Fatalf("openDB 1 failed: %v", err)
	}
	srv1 := httptest.NewServer(newHandler(db1, 300))

	createBody, _ := json.Marshal(map[string]string{"body": "work", "project": "p"})
	resp, err := http.Post(srv1.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var created map[string]int64
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	taskID := created["id"]

	claimBody, _ := json.Marshal(map[string]string{"worker": "w1", "project": "p"})
	resp, err = http.Post(srv1.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("first claim failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first claim status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	resp.Body.Close()

	if _, err := db1.rw.Exec("UPDATE tasks SET lease_expires = unixepoch() - 1 WHERE id = ?", taskID); err != nil {
		t.Fatalf("expire first lease failed: %v", err)
	}
	if _, err := db1.sweep(); err != nil {
		t.Fatalf("sweep 1 failed: %v", err)
	}

	resp, err = http.Post(srv1.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("second claim failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second claim status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	resp.Body.Close()

	if _, err := db1.rw.Exec("UPDATE tasks SET lease_expires = unixepoch() - 1 WHERE id = ?", taskID); err != nil {
		t.Fatalf("expire second lease failed: %v", err)
	}
	if _, err := db1.sweep(); err != nil {
		t.Fatalf("sweep 2 failed: %v", err)
	}

	srv1.Close()
	db1.Close()

	db2, err := openDB(dbPath, 2)
	if err != nil {
		t.Fatalf("openDB 2 failed: %v", err)
	}
	defer db2.Close()
	srv2 := httptest.NewServer(newHandler(db2, 300))
	defer srv2.Close()

	resp, err = http.Post(srv2.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("third claim failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("claim at max-claims limit status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	respGet, err := http.Get(fmt.Sprintf("%s/tasks/%d", srv2.URL, taskID))
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	defer respGet.Body.Close()
	var item taskItem
	if err := json.NewDecoder(respGet.Body).Decode(&item); err != nil {
		t.Fatalf("decode task failed: %v", err)
	}
	if item.Status != "buried" {
		t.Fatalf("task status = %q, want buried", item.Status)
	}
}

func TestClaimBelowMaxClaimsSucceeds(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 2)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]string{"body": "work", "project": "p"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	resp.Body.Close()

	claimBody, _ := json.Marshal(map[string]string{"worker": "w1", "project": "p"})
	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("claim status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var claimed taskItem
	if err := json.NewDecoder(resp.Body).Decode(&claimed); err != nil {
		t.Fatalf("decode claimed failed: %v", err)
	}
	if claimed.ClaimCount != 1 {
		t.Fatalf("claim_count = %d, want 1", claimed.ClaimCount)
	}
}

func TestLapsedLeaseReportedAsPendingAcrossReadEndpoints(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 30))
	defer srv.Close()

	createReq, err := http.NewRequest(http.MethodPost, srv.URL+"/tasks", strings.NewReader(`{"body":"task1","project":"p1"}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	createReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create task status: %d", resp.StatusCode)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("decode created: %v", err)
	}
	resp.Body.Close()
	createdPath := fmt.Sprintf("/tasks/%d", created.ID)
	if want := createdPath; resp.Header.Get("Location") != want {
		t.Fatalf("Location = %q, want %q", resp.Header.Get("Location"), want)
	}

	claimReq, err := http.NewRequest(http.MethodPost, srv.URL+"/tasks/claim", strings.NewReader(`{"worker":"w1","project":"p1"}`))
	if err != nil {
		t.Fatalf("new claim request: %v", err)
	}
	claimReq.Header.Set("Content-Type", "application/json")
	claimResp, err := http.DefaultClient.Do(claimReq)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimResp.StatusCode != http.StatusOK {
		t.Fatalf("claim status: %d", claimResp.StatusCode)
	}
	claimResp.Body.Close()

	if _, err := db.rw.Exec("UPDATE tasks SET lease_expires = unixepoch() - 10 WHERE id = ?", created.ID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}

	getResp, err := http.Get(srv.URL + createdPath)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get task status: %d", getResp.StatusCode)
	}
	var single taskItem
	if err := json.NewDecoder(getResp.Body).Decode(&single); err != nil {
		getResp.Body.Close()
		t.Fatalf("decode task: %v", err)
	}
	getResp.Body.Close()

	if single.Status != "pending" {
		t.Fatalf("GET /tasks/{id} status = %q, want pending", single.Status)
	}
	if single.Worker != "" {
		t.Fatalf("GET /tasks/{id} worker = %q, want empty", single.Worker)
	}
	if single.LeaseExpires != 0 {
		t.Fatalf("GET /tasks/{id} lease_expires = %d, want 0", single.LeaseExpires)
	}

	listResp, err := http.Get(srv.URL + "/tasks?project=p1")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	var allTasks []taskItem
	if err := json.NewDecoder(listResp.Body).Decode(&allTasks); err != nil {
		listResp.Body.Close()
		t.Fatalf("decode list: %v", err)
	}
	listResp.Body.Close()
	if len(allTasks) != 1 || allTasks[0].Status != "pending" || allTasks[0].Worker != "" {
		t.Fatalf("GET /tasks = %+v, want one pending task with empty worker", allTasks)
	}

	pendingResp, err := http.Get(srv.URL + "/tasks?project=p1&status=pending")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	var pendingTasks []taskItem
	if err := json.NewDecoder(pendingResp.Body).Decode(&pendingTasks); err != nil {
		pendingResp.Body.Close()
		t.Fatalf("decode pending: %v", err)
	}
	pendingResp.Body.Close()
	if len(pendingTasks) != 1 || pendingTasks[0].ID != created.ID {
		t.Fatalf("GET /tasks?status=pending returned %+v, want [task1]", pendingTasks)
	}

	leasedResp, err := http.Get(srv.URL + "/tasks?project=p1&status=leased")
	if err != nil {
		t.Fatalf("list leased: %v", err)
	}
	var leasedTasks []taskItem
	if err := json.NewDecoder(leasedResp.Body).Decode(&leasedTasks); err != nil {
		leasedResp.Body.Close()
		t.Fatalf("decode leased: %v", err)
	}
	leasedResp.Body.Close()
	if len(leasedTasks) != 0 {
		t.Fatalf("GET /tasks?status=leased = %+v, want 0 tasks", leasedTasks)
	}

	statsResp, err := http.Get(srv.URL + "/stats?project=p1")
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	var stats statsResponse
	if err := json.NewDecoder(statsResp.Body).Decode(&stats); err != nil {
		statsResp.Body.Close()
		t.Fatalf("decode stats: %v", err)
	}
	statsResp.Body.Close()
	if stats.Pending != 1 {
		t.Fatalf("stats.Pending = %d, want 1", stats.Pending)
	}
	if stats.Leased != 0 {
		t.Fatalf("stats.Leased = %d, want 0", stats.Leased)
	}
	if len(pendingTasks) != stats.Pending {
		t.Fatalf("status=pending count (%d) != stats.Pending (%d)", len(pendingTasks), stats.Pending)
	}

	if _, err := db.sweep(); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	claim2Req, err := http.NewRequest(http.MethodPost, srv.URL+"/tasks/claim", strings.NewReader(`{"worker":"w2","project":"p1"}`))
	if err != nil {
		t.Fatalf("claim 2 req: %v", err)
	}
	claim2Req.Header.Set("Content-Type", "application/json")
	claim2Resp, err := http.DefaultClient.Do(claim2Req)
	if err != nil {
		t.Fatalf("claim 2: %v", err)
	}
	if claim2Resp.StatusCode != http.StatusOK {
		t.Fatalf("claim 2 status: %d", claim2Resp.StatusCode)
	}
	var claimed2 taskItem
	if err := json.NewDecoder(claim2Resp.Body).Decode(&claimed2); err != nil {
		claim2Resp.Body.Close()
		t.Fatalf("decode claimed 2: %v", err)
	}
	claim2Resp.Body.Close()
	if claimed2.ID != created.ID || claimed2.Worker != "w2" || claimed2.ClaimCount != 2 {
		t.Fatalf("reclaimed task: %+v, want ID=%d Worker=w2 ClaimCount=2", claimed2, created.ID)
	}
}

func TestSweepLapsedBuriesExhaustedClaims(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 1)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandlerWithCORS(db, 30, ""))
	defer srv.Close()

	createReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/tasks", strings.NewReader(`{"body":"task-max","project":"p"}`))
	createReq.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(createReq)
	var created struct{ ID int64 }
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	claimReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/tasks/claim", strings.NewReader(`{"worker":"w1","project":"p"}`))
	claimReq.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(claimReq)
	resp.Body.Close()

	if _, err := db.rw.Exec("UPDATE tasks SET lease_expires = unixepoch() - 10 WHERE id = ?", created.ID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}

	db.maxClaims = 1
	if err := db.sweepLapsed(0); err != nil {
		t.Fatalf("sweepLapsed: %v", err)
	}

	var status string
	if err := db.ro.QueryRow("SELECT status FROM tasks WHERE id = ?", created.ID).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "buried" {
		t.Fatalf("expected task to be buried after maxClaims exceeded, got status %q", status)
	}
}

type testClient struct {
	t   *testing.T
	srv *httptest.Server
}

func (c *testClient) post(path, body string) (*http.Response, taskItem) {
	c.t.Helper()
	res, err := c.srv.Client().Post(c.srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	var item taskItem
	if strings.Contains(res.Header.Get("Content-Type"), "application/json") {
		json.NewDecoder(res.Body).Decode(&item)
	}
	res.Body.Close()
	return res, item
}

func (c *testClient) get(path string) taskItem {
	c.t.Helper()
	res, err := c.srv.Client().Get(c.srv.URL + path)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	var item taskItem
	json.NewDecoder(res.Body).Decode(&item)
	return item
}

func TestVoluntaryReleaseRefundsMaxClaims(t *testing.T) {
	st, err := openDB(filepath.Join(t.TempDir(), "test.db"), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	srv := httptest.NewServer(newHandlerWithCORS(st, 300, ""))
	defer srv.Close()
	c := testClient{t: t, srv: srv}

	_, created := c.post("/tasks", `{"project":"mc","body":"healthy"}`)
	taskPath := fmt.Sprintf("/tasks/%d", created.ID)
	_, itemA := c.post("/tasks/claim", `{"worker":"A","project":"mc"}`)
	if itemA.ClaimCount != 1 {
		t.Fatalf("worker A claim_count = %d, want 1", itemA.ClaimCount)
	}
	c.post(taskPath+"/release", `{"worker":"A"}`)

	_, itemB := c.post("/tasks/claim", `{"worker":"B","project":"mc"}`)
	if itemB.ClaimCount != 1 {
		t.Fatalf("worker B claim_count = %d, want 1", itemB.ClaimCount)
	}
	c.post(taskPath+"/release", `{"worker":"B"}`)

	task := c.get(taskPath)
	if task.Status != "pending" || task.ClaimCount != 0 {
		t.Fatalf("task = %+v, want pending with claim_count 0", task)
	}

	resC, itemC := c.post("/tasks/claim", `{"worker":"C","project":"mc"}`)
	if resC.StatusCode != http.StatusOK || itemC.ID != created.ID {
		t.Fatalf("worker C claim status = %d, id = %d", resC.StatusCode, itemC.ID)
	}
}

func TestLeaseExpirationExhaustsMaxClaims(t *testing.T) {
	st, err := openDB(filepath.Join(t.TempDir(), "test.db"), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	srv := httptest.NewServer(newHandlerWithCORS(st, -1, ""))
	defer srv.Close()
	c := testClient{t: t, srv: srv}

	_, created := c.post("/tasks", `{"project":"mc","body":"failing"}`)
	taskPath := fmt.Sprintf("/tasks/%d", created.ID)
	_, itemA := c.post("/tasks/claim", `{"worker":"A","project":"mc"}`)
	if itemA.ClaimCount != 1 {
		t.Fatalf("worker A claim_count = %d, want 1", itemA.ClaimCount)
	}

	if _, err := st.sweep(); err != nil {
		t.Fatal(err)
	}

	_, itemB := c.post("/tasks/claim", `{"worker":"B","project":"mc"}`)
	if itemB.ClaimCount != 2 {
		t.Fatalf("worker B claim_count = %d, want 2", itemB.ClaimCount)
	}

	if _, err := st.sweep(); err != nil {
		t.Fatal(err)
	}

	resC, _ := c.post("/tasks/claim", `{"worker":"C","project":"mc"}`)
	if resC.StatusCode != http.StatusNoContent {
		t.Fatalf("worker C status = %d, want 204 No Content", resC.StatusCode)
	}

	task := c.get(taskPath)
	if task.Status != "buried" || task.ClaimCount != 2 {
		t.Fatalf("task = %+v, want buried with claim_count 2", task)
	}
}
