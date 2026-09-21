package taskd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/IgorAlexey/taskd/internal/version"
)

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
	postTask := func(body any) int64 {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}
		resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(data))
		if err != nil {
			t.Fatalf("POST /tasks failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("POST /tasks status: %d", resp.StatusCode)
		}
		var created map[string]int64
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		return created["id"]
	}

	t1 := postTask(map[string]string{"body": "task 1", "project": "p1"})
	postJSON(fmt.Sprintf("/tasks/%d/claim", t1), map[string]string{"worker": "w1"})
	postJSON(fmt.Sprintf("/tasks/%d/done", t1), map[string]string{"worker": "w1"})

	t2 := postTask(map[string]string{"body": "task 2", "project": "p1"})
	postJSON(fmt.Sprintf("/tasks/%d/claim", t2), map[string]string{"worker": "w1"})

	t3 := postTask(map[string]string{"body": "task 3", "project": "p2"})
	postJSON(fmt.Sprintf("/tasks/%d/claim", t3), map[string]string{"worker": "w1"})

	t4 := postTask(map[string]string{"body": "task 4", "project": "p1"})
	postJSON(fmt.Sprintf("/tasks/%d/claim", t4), map[string]string{"worker": "w2"})

	t5 := postTask(map[string]string{"body": "task 5", "project": "p1"})
	_ = t5

	t6 := postTask(map[string]string{"body": "task 6", "project": "p1"})
	postJSON(fmt.Sprintf("/tasks/%d/claim", t6), map[string]string{"worker": "w1"})
	postJSON(fmt.Sprintf("/tasks/%d/bury", t6), map[string]string{"worker": "w1"})
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
		var res struct{ ID int64 }
		json.NewDecoder(resp.Body).Decode(&res)
		return strconv.FormatInt(res.ID, 10)
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

func TestStatsWorkerExpiredLeases(t *testing.T) {
	st, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer st.Close()

	srv := httptest.NewServer(newHandler(st, 300))
	defer srv.Close()

	postTask := `{"body":"expiring work","project":"proj"}`
	res, err := srv.Client().Post(srv.URL+"/tasks", "application/json", strings.NewReader(postTask))
	if err != nil {
		t.Fatalf("POST /tasks: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST /tasks status = %d", res.StatusCode)
	}
	var created map[string]int64
	json.NewDecoder(res.Body).Decode(&created)
	taskPath := fmt.Sprintf("/tasks/%d", created["id"])

	claimPayload := `{"worker":"w1"}`
	res, err = srv.Client().Post(srv.URL+taskPath+"/claim", "application/json", strings.NewReader(claimPayload))
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

	if _, err := st.rw.Exec("UPDATE tasks SET lease_expires = unixepoch() - 10 WHERE id = ?", created["id"]); err != nil {
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
