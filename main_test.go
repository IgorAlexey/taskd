package main

import (
	"bytes"
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

	"github.com/IgorAlexey/taskd/internal/version"
)

func TestCORSHeaders(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandlerWithCORS(db, 300, "http://example.com"))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/tasks", nil)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Origin", "http://example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://example.com" {
		t.Fatalf("expected Access-Control-Allow-Origin: http://example.com, got %q", got)
	}
	expose := resp.Header.Get("Access-Control-Expose-Headers")
	for _, want := range []string{"X-Total-Count", "X-Next-Cursor", "ETag", "Location"} {
		if !strings.Contains(expose, want) {
			t.Fatalf("Access-Control-Expose-Headers %q missing %q", expose, want)
		}
	}

	optReq, err := http.NewRequest(http.MethodOptions, srv.URL+"/tasks", nil)
	if err != nil {
		t.Fatalf("new options request failed: %v", err)
	}
	optReq.Header.Set("Origin", "http://example.com")
	optReq.Header.Set("Access-Control-Request-Method", "GET")
	optReq.Header.Set("Access-Control-Request-Headers", "if-none-match")
	optResp, err := http.DefaultClient.Do(optReq)
	if err != nil {
		t.Fatalf("options request failed: %v", err)
	}
	optResp.Body.Close()

	if optResp.StatusCode != http.StatusNoContent {
		t.Fatalf("options expected 204, got %d", optResp.StatusCode)
	}
	if got := optResp.Header.Get("Access-Control-Allow-Origin"); got != "http://example.com" {
		t.Fatalf("options expected Access-Control-Allow-Origin: http://example.com, got %q", got)
	}
	allowHeaders := optResp.Header.Get("Access-Control-Allow-Headers")
	if !strings.Contains(strings.ToLower(allowHeaders), "if-none-match") {
		t.Fatalf("Access-Control-Allow-Headers %q does not include If-None-Match", allowHeaders)
	}
	if got := optResp.Header.Get("Access-Control-Expose-Headers"); got != "" {
		t.Fatalf("expected empty Access-Control-Expose-Headers on preflight, got %q", got)
	}
}

func TestBackupDestinationDirectory(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := openDB(dbPath, 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	db.Close()

	destDir := filepath.Join(dir, "backup-dir")
	if err := os.Mkdir(destDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	err = run(io.Discard, []string{"-db", dbPath, "-backup", destDir})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	wantErr := "backup destination is a directory: " + destDir
	if err.Error() != wantErr {
		t.Fatalf("expected error %q, got %q", wantErr, err.Error())
	}

	roDB, err := openReadOnlyDB(dbPath)
	if err != nil {
		t.Fatalf("openReadOnlyDB failed: %v", err)
	}
	defer roDB.Close()

	if err := backupDB(roDB, destDir, io.Discard); err == nil || err.Error() != wantErr {
		t.Fatalf("backupDB expected %q, got %v", wantErr, err)
	}
}

func TestStatsWorkerFilter(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 5)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	postJSON := func(endpoint string, body any) {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}
		resp, err := http.Post(srv.URL+endpoint, "application/json", bytes.NewReader(data))
		if err != nil {
			t.Fatalf("POST %s failed: %v", endpoint, err)
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			t.Fatalf("POST %s status: %d", endpoint, resp.StatusCode)
		}
	}

	postJSON("/tasks", map[string]string{"id": "t1", "body": "task 1", "project": "p1"})
	postJSON("/tasks/t1/claim", map[string]string{"worker": "w1"})
	postJSON("/tasks/t1/done", map[string]string{"worker": "w1"})

	postJSON("/tasks", map[string]string{"id": "t2", "body": "task 2", "project": "p1"})
	postJSON("/tasks/t2/claim", map[string]string{"worker": "w1"})

	postJSON("/tasks", map[string]string{"id": "t3", "body": "task 3", "project": "p2"})
	postJSON("/tasks/t3/claim", map[string]string{"worker": "w1"})

	postJSON("/tasks", map[string]string{"id": "t4", "body": "task 4", "project": "p1"})
	postJSON("/tasks/t4/claim", map[string]string{"worker": "w2"})

	postJSON("/tasks", map[string]string{"id": "t5", "body": "task 5", "project": "p1"})

	postJSON("/tasks", map[string]string{"id": "t6", "body": "task 6", "project": "p1"})
	postJSON("/tasks/t6/claim", map[string]string{"worker": "w1"})
	postJSON("/tasks/t6/bury", map[string]string{"worker": "w1"})

	getStats := func(query string) statsResponse {
		t.Helper()
		resp, err := http.Get(srv.URL + "/stats" + query)
		if err != nil {
			t.Fatalf("GET /stats%s failed: %v", query, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /stats%s status: %d", query, resp.StatusCode)
		}
		var s statsResponse
		if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
			t.Fatalf("decode /stats%s failed: %v", query, err)
		}
		return s
	}

	sW1 := getStats("?worker=w1")
	if sW1.Pending != 0 || sW1.Leased != 2 || sW1.Done != 1 || sW1.Buried != 0 || sW1.Total != 3 {
		t.Fatalf("stats ?worker=w1: got %+v, want pending:0 leased:2 done:1 buried:0 total:3", sW1)
	}
	if sW1.Version != version.Version {
		t.Fatalf("version: got %q, want %q", sW1.Version, version.Version)
	}
	if sW1.MaxClaims != 5 {
		t.Fatalf("max_claims: got %d, want 5", sW1.MaxClaims)
	}

	sP1W1 := getStats("?project=p1&worker=w1")
	if sP1W1.Pending != 0 || sP1W1.Leased != 1 || sP1W1.Done != 1 || sP1W1.Buried != 0 || sP1W1.Total != 2 {
		t.Fatalf("stats ?project=p1&worker=w1: got %+v, want pending:0 leased:1 done:1 buried:0 total:2", sP1W1)
	}

	sP2W1 := getStats("?project=p2&worker=w1")
	if sP2W1.Pending != 0 || sP2W1.Leased != 1 || sP2W1.Done != 0 || sP2W1.Buried != 0 || sP2W1.Total != 1 {
		t.Fatalf("stats ?project=p2&worker=w1: got %+v, want pending:0 leased:1 done:0 buried:0 total:1", sP2W1)
	}

	sW2 := getStats("?worker=w2")
	if sW2.Pending != 0 || sW2.Leased != 1 || sW2.Done != 0 || sW2.Buried != 0 || sW2.Total != 1 {
		t.Fatalf("stats ?worker=w2: got %+v, want pending:0 leased:1 done:0 buried:0 total:1", sW2)
	}

	sUnassigned := getStats("?worker=")
	if sUnassigned.Pending != 1 || sUnassigned.Leased != 0 || sUnassigned.Done != 0 || sUnassigned.Buried != 1 || sUnassigned.Total != 2 {
		t.Fatalf("stats ?worker=: got %+v, want pending:1 leased:0 done:0 buried:1 total:2", sUnassigned)
	}

	var usage bytes.Buffer
	printUsage(&usage)
	if !strings.Contains(usage.String(), "?worker=") {
		t.Fatalf("printUsage does not document ?worker=")
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
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("decode created: %v", err)
	}
	resp.Body.Close()
	if want := "/tasks/" + created.ID; resp.Header.Get("Location") != want {
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

	getResp, err := http.Get(srv.URL + "/tasks/" + created.ID)
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
		t.Fatalf("reclaimed task: %+v, want ID=%s Worker=w2 ClaimCount=2", claimed2, created.ID)
	}
}

func TestStatsPendingAgreesWithStatusPendingQuery(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 30))
	defer srv.Close()

	postTask := func(body string) string {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/tasks", strings.NewReader(`{"body":"`+body+`","project":"demo"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post task: %v", err)
		}
		defer resp.Body.Close()
		var res struct{ ID string }
		json.NewDecoder(resp.Body).Decode(&res)
		return res.ID
	}

	idPending1 := postTask("pending 1")
	_ = idPending1
	idPending2 := postTask("pending 2")
	_ = idPending2

	idActive := postTask("active leased")
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/tasks/"+idActive+"/claim", strings.NewReader(`{"worker":"w-act"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	idLapsed := postTask("lapsed leased")
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/tasks/"+idLapsed+"/claim", strings.NewReader(`{"worker":"w-lapse"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if _, err := db.rw.Exec("UPDATE tasks SET lease_expires = unixepoch() - 50 WHERE id = ?", idLapsed); err != nil {
		t.Fatalf("expire lease: %v", err)
	}

	idDone := postTask("completed task")
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/tasks/"+idDone+"/claim", strings.NewReader(`{"worker":"w-done"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/tasks/"+idDone+"/done", strings.NewReader(`{"worker":"w-done"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()

	idBuried := postTask("buried task")
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/tasks/"+idBuried+"/claim", strings.NewReader(`{"worker":"w-bury"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/tasks/"+idBuried+"/bury", strings.NewReader(`{"worker":"w-bury"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()

	statsResp, err := http.Get(srv.URL + "/stats")
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	defer statsResp.Body.Close()
	var st statsResponse
	json.NewDecoder(statsResp.Body).Decode(&st)

	if st.Pending != 3 {
		t.Errorf("stats.Pending = %d, want 3 (2 pending + 1 lapsed)", st.Pending)
	}
	if st.Leased != 1 {
		t.Errorf("stats.Leased = %d, want 1 active lease", st.Leased)
	}
	if st.Done != 1 {
		t.Errorf("stats.Done = %d, want 1", st.Done)
	}
	if st.Buried != 1 {
		t.Errorf("stats.Buried = %d, want 1", st.Buried)
	}
	if st.Total != 6 {
		t.Errorf("stats.Total = %d, want 6", st.Total)
	}

	pResp, err := http.Get(srv.URL + "/tasks?status=pending")
	if err != nil {
		t.Fatalf("get pending: %v", err)
	}
	defer pResp.Body.Close()
	var pending []taskItem
	json.NewDecoder(pResp.Body).Decode(&pending)

	if len(pending) != st.Pending {
		t.Fatalf("len(tasks?status=pending) = %d != stats.Pending = %d", len(pending), st.Pending)
	}

	lResp, err := http.Get(srv.URL + "/tasks?status=leased")
	if err != nil {
		t.Fatalf("get leased: %v", err)
	}
	defer lResp.Body.Close()
	var leased []taskItem
	json.NewDecoder(lResp.Body).Decode(&leased)

	if len(leased) != st.Leased {
		t.Fatalf("len(tasks?status=leased) = %d != stats.Leased = %d", len(leased), st.Leased)
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
	var created struct{ ID string }
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	claimReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/tasks/claim", strings.NewReader(`{"worker":"w1","project":"p"}`))
	claimReq.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(claimReq)
	resp.Body.Close()

	if _, err := db.rw.Exec("UPDATE tasks SET lease_expires = unixepoch() - 10 WHERE id = ?", created.ID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}

	if err := sweepLapsed(db.rw, 1, ""); err != nil {
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

func TestClaimProjectValidation(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	postTask := func(id, project string) {
		body := `{"id":"` + id + `","body":"test","project":"` + project + `"}`
		resp, err := http.Post(srv.URL+"/tasks", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("post task failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", resp.StatusCode)
		}
	}

	postTask("t1", "valid-proj")

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

	postTask("t2", "valid-proj")
	validPayload := `{"worker":"w1","project":"valid-proj"}`
	respValid, err := http.Post(srv.URL+"/tasks/claim", "application/json", strings.NewReader(validPayload))
	if err != nil {
		t.Fatalf("claim valid failed: %v", err)
	}
	defer respValid.Body.Close()
	if respValid.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for valid project, got %d", respValid.StatusCode)
	}
	postTask("t3", "p1")
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

	c.post("/tasks", `{"id":"healthy01","project":"mc","body":"healthy"}`)
	_, itemA := c.post("/tasks/claim", `{"worker":"A","project":"mc"}`)
	if itemA.ClaimCount != 1 {
		t.Fatalf("worker A claim_count = %d, want 1", itemA.ClaimCount)
	}
	c.post("/tasks/healthy01/release", `{"worker":"A"}`)

	_, itemB := c.post("/tasks/claim", `{"worker":"B","project":"mc"}`)
	if itemB.ClaimCount != 1 {
		t.Fatalf("worker B claim_count = %d, want 1", itemB.ClaimCount)
	}
	c.post("/tasks/healthy01/release", `{"worker":"B"}`)

	task := c.get("/tasks/healthy01")
	if task.Status != "pending" || task.ClaimCount != 0 {
		t.Fatalf("task = %+v, want pending with claim_count 0", task)
	}

	resC, itemC := c.post("/tasks/claim", `{"worker":"C","project":"mc"}`)
	if resC.StatusCode != http.StatusOK || itemC.ID != "healthy01" {
		t.Fatalf("worker C claim status = %d, id = %q", resC.StatusCode, itemC.ID)
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

	c.post("/tasks", `{"id":"expired01","project":"mc","body":"failing"}`)
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

	task := c.get("/tasks/expired01")
	if task.Status != "buried" || task.ClaimCount != 2 {
		t.Fatalf("task = %+v, want buried with claim_count 2", task)
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
		body := fmt.Sprintf(`{"id":%q,"project":"bench","body":"work"}`, id)
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
	claimed := make(map[string]int)
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
			t.Fatalf("task %s was claimed %d times, want 1", id, count)
		}
	}
}

func TestStatsWorkerExpiredLeases(t *testing.T) {
	st, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer st.Close()

	srv := httptest.NewServer(newHandler(st, 300))
	defer srv.Close()

	postTask := `{"id":"task-exp","body":"expiring work","project":"proj"}`
	res, err := srv.Client().Post(srv.URL+"/tasks", "application/json", strings.NewReader(postTask))
	if err != nil {
		t.Fatalf("POST /tasks: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST /tasks status = %d", res.StatusCode)
	}

	claimPayload := `{"worker":"w1"}`
	res, err = srv.Client().Post(srv.URL+"/tasks/task-exp/claim", "application/json", strings.NewReader(claimPayload))
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("claim status = %d", res.StatusCode)
	}

	getStats := func(query string) statsResponse {
		t.Helper()
		res, err := srv.Client().Get(srv.URL + "/stats" + query)
		if err != nil {
			t.Fatalf("GET /stats%s: %v", query, err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET /stats%s status = %d", query, res.StatusCode)
		}
		var s statsResponse
		if err := json.NewDecoder(res.Body).Decode(&s); err != nil {
			t.Fatalf("decode stats: %v", err)
		}
		return s
	}

	sBefore := getStats("?worker=w1")
	if sBefore.Pending != 0 || sBefore.Leased != 1 || sBefore.Total != 1 {
		t.Fatalf("stats before expiry ?worker=w1: got %+v, want pending:0 leased:1 total:1", sBefore)
	}

	sBeforeUnassigned := getStats("?worker=")
	if sBeforeUnassigned.Pending != 0 || sBeforeUnassigned.Leased != 0 || sBeforeUnassigned.Total != 0 {
		t.Fatalf("stats before expiry ?worker=: got %+v, want pending:0 leased:0 total:0", sBeforeUnassigned)
	}

	if _, err := st.rw.Exec("UPDATE tasks SET lease_expires = unixepoch() - 10 WHERE id = 'task-exp'"); err != nil {
		t.Fatalf("expire lease failed: %v", err)
	}

	sW1 := getStats("?worker=w1")
	if sW1.Pending != 0 || sW1.Leased != 0 || sW1.Total != 0 {
		t.Fatalf("stats ?worker=w1: got %+v, want pending:0 leased:0 total:0", sW1)
	}

	sUnassigned := getStats("?worker=")
	if sUnassigned.Pending != 1 || sUnassigned.Leased != 0 || sUnassigned.Total != 1 {
		t.Fatalf("stats ?worker=: got %+v, want pending:1 leased:0 total:1", sUnassigned)
	}
}

func TestTasksAndStatsRejectInvalidProject(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	cases := []struct {
		param   string
		wantErr string
	}{
		{"?project=", "project cannot be empty"},
		{"?project=bad*name", "invalid project \"bad*name\": must contain only [a-zA-Z0-9._-]"},
	}

	for _, endpoint := range []string{"/tasks", "/stats"} {
		for _, tc := range cases {
			resp, err := http.Get(srv.URL + endpoint + tc.param)
			if err != nil {
				t.Fatalf("GET %s%s failed: %v", endpoint, tc.param, err)
			}
			var errResp map[string]string
			decErr := json.NewDecoder(resp.Body).Decode(&errResp)
			resp.Body.Close()
			if decErr != nil {
				t.Fatalf("decode %s%s error failed: %v", endpoint, tc.param, decErr)
			}
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("GET %s%s status = %d, want 400", endpoint, tc.param, resp.StatusCode)
			}
			if got := errResp["error"]; got != tc.wantErr {
				t.Fatalf("GET %s%s error = %q, want %q", endpoint, tc.param, got, tc.wantErr)
			}
		}
	}
}

func TestGetTasksEmptyStatus(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	handler := newHandler(db, 30)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tasks?status=", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	recInvalid := httptest.NewRecorder()
	handler.ServeHTTP(recInvalid, httptest.NewRequest(http.MethodGet, "/tasks?status=invalid", nil))
	if recInvalid.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recInvalid.Code)
	}
}

func TestGetTasksInvalidSortOrderFields(t *testing.T) {
	db, err := openDB(":memory:", 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	handler := newHandler(db, 30)

	cases := []struct {
		param   string
		wantErr string
	}{
		{
			param:   "?sort=bad",
			wantErr: `invalid sort "bad", must be one of [id, project, status, priority, claim_count, worker, created_at]`,
		},
		{
			param:   "?order=bad",
			wantErr: `invalid order "bad", must be one of [asc, desc]`,
		},
		{
			param:   "?fields=bad",
			wantErr: `invalid field "bad", must be one of [id, status, worker, lease_expires, priority, body, primitives, project, claim_count, summary, created_at, version]`,
		},
	}

	for _, tc := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tasks"+tc.param, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("GET /tasks%s status = %d, want 400", tc.param, rec.Code)
		}
		var errResp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("decode /tasks%s error failed: %v", tc.param, err)
		}
		if got := errResp["error"]; got != tc.wantErr {
			t.Fatalf("GET /tasks%s error = %q, want %q", tc.param, got, tc.wantErr)
		}
	}
}
