package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestDepsCreateAndList(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	// POST A
	postA := `{"project":"p","body":"task A"}`
	respA, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postA))
	if err != nil {
		t.Fatalf("POST A failed: %v", err)
	}
	defer respA.Body.Close()
	if respA.StatusCode != http.StatusCreated {
		t.Fatalf("POST A expected 201, got %d", respA.StatusCode)
	}
	var resA struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(respA.Body).Decode(&resA); err != nil {
		t.Fatalf("decode A failed: %v", err)
	}

	// POST B with after [A]
	postB := fmt.Sprintf(`{"project":"p","body":"task B","after":[%d]}`, resA.ID)
	respB, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postB))
	if err != nil {
		t.Fatalf("POST B failed: %v", err)
	}
	defer respB.Body.Close()
	if respB.StatusCode != http.StatusCreated {
		t.Fatalf("POST B expected 201, got %d", respB.StatusCode)
	}
	var resB struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(respB.Body).Decode(&resB); err != nil {
		t.Fatalf("decode B failed: %v", err)
	}

	// GET /tasks shows B.after == [A] and A.after == []
	respList, err := http.Get(srv.URL + "/tasks")
	if err != nil {
		t.Fatalf("GET /tasks failed: %v", err)
	}
	defer respList.Body.Close()
	if respList.StatusCode != http.StatusOK {
		t.Fatalf("GET /tasks expected 200, got %d", respList.StatusCode)
	}
	var list []taskItem
	if err := json.NewDecoder(respList.Body).Decode(&list); err != nil {
		t.Fatalf("decode /tasks failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(list))
	}
	for _, it := range list {
		if it.ID == resA.ID {
			if len(it.After) != 0 || it.After == nil {
				t.Fatalf("task A expected after == [], got %v", it.After)
			}
		} else if it.ID == resB.ID {
			if !reflect.DeepEqual(it.After, []int64{resA.ID}) {
				t.Fatalf("task B expected after == [%d], got %v", resA.ID, it.After)
			}
		}
	}

	// GET /tasks/B likewise
	respGetB, err := http.Get(fmt.Sprintf("%s/tasks/%d", srv.URL, resB.ID))
	if err != nil {
		t.Fatalf("GET /tasks/B failed: %v", err)
	}
	defer respGetB.Body.Close()
	if respGetB.StatusCode != http.StatusOK {
		t.Fatalf("GET /tasks/B expected 200, got %d", respGetB.StatusCode)
	}
	var detailB taskDetail
	if err := json.NewDecoder(respGetB.Body).Decode(&detailB); err != nil {
		t.Fatalf("decode detail B failed: %v", err)
	}
	if !reflect.DeepEqual(detailB.After, []int64{resA.ID}) {
		t.Fatalf("detail B expected after == [%d], got %v", resA.ID, detailB.After)
	}

	// GET /tasks/A likewise
	respGetA, err := http.Get(fmt.Sprintf("%s/tasks/%d", srv.URL, resA.ID))
	if err != nil {
		t.Fatalf("GET /tasks/A failed: %v", err)
	}
	defer respGetA.Body.Close()
	if respGetA.StatusCode != http.StatusOK {
		t.Fatalf("GET /tasks/A expected 200, got %d", respGetA.StatusCode)
	}
	var detailA taskDetail
	if err := json.NewDecoder(respGetA.Body).Decode(&detailA); err != nil {
		t.Fatalf("decode detail A failed: %v", err)
	}
	if len(detailA.After) != 0 || detailA.After == nil {
		t.Fatalf("detail A expected after == [], got %v", detailA.After)
	}

	// GET /tasks?fields=id,after
	respFields, err := http.Get(srv.URL + "/tasks?fields=id,after")
	if err != nil || respFields.StatusCode != http.StatusOK {
		t.Fatalf("GET fields failed: %v, status %d", err, respFields.StatusCode)
	}
	defer respFields.Body.Close()
	var projList []struct {
		ID    int64   `json:"id"`
		After []int64 `json:"after"`
	}
	if err := json.NewDecoder(respFields.Body).Decode(&projList); err != nil {
		t.Fatalf("decode projList failed: %v", err)
	}
	if len(projList) != 2 {
		t.Fatalf("expected 2 tasks in projList, got %d", len(projList))
	}
	for _, it := range projList {
		if it.ID == resB.ID && !reflect.DeepEqual(it.After, []int64{resA.ID}) {
			t.Fatalf("projected task B expected after [%d], got %v", resA.ID, it.After)
		}
		if it.ID == resA.ID && (len(it.After) != 0 || it.After == nil) {
			t.Fatalf("projected task A expected after [], got %v", it.After)
		}
	}
}

func TestDepsValidation(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	// after [999] -> 400 field=after
	badPost := `{"project":"p","body":"task","after":[999]}`
	respBad, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(badPost))
	if err != nil {
		t.Fatalf("POST bad failed: %v", err)
	}
	defer respBad.Body.Close()
	if respBad.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown task 999, got %d", respBad.StatusCode)
	}
	var errResp map[string]string
	json.NewDecoder(respBad.Body).Decode(&errResp)
	if errResp["field"] != "after" {
		t.Fatalf("expected field=after, got %q", errResp["field"])
	}
	if errResp["error"] != "unknown task 999" {
		t.Fatalf("expected 'unknown task 999', got %q", errResp["error"])
	}

	// Create task A
	postA := `{"project":"p","body":"task A"}`
	respA, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postA))
	if err != nil {
		t.Fatalf("POST A failed: %v", err)
	}
	defer respA.Body.Close()
	var resA struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respA.Body).Decode(&resA)

	// after [self] on PATCH -> 400
	patchSelf := fmt.Sprintf(`{"after":[%d]}`, resA.ID)
	reqSelf, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/tasks/%d", srv.URL, resA.ID), bytes.NewBufferString(patchSelf))
	reqSelf.Header.Set("Content-Type", "application/json")
	respSelf, err := http.DefaultClient.Do(reqSelf)
	if err != nil {
		t.Fatalf("PATCH self failed: %v", err)
	}
	defer respSelf.Body.Close()
	if respSelf.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for self reference, got %d", respSelf.StatusCode)
	}
	var errSelf map[string]string
	json.NewDecoder(respSelf.Body).Decode(&errSelf)
	if errSelf["field"] != "after" {
		t.Fatalf("expected field=after for self reference, got %q", errSelf["field"])
	}

	// Create task B with after [A]
	postB := fmt.Sprintf(`{"project":"p","body":"task B","after":[%d]}`, resA.ID)
	respB, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postB))
	if err != nil {
		t.Fatalf("POST B failed: %v", err)
	}
	defer respB.Body.Close()
	var resB struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respB.Body).Decode(&resB)

	// A after B then B after A -> 400 dependency cycle
	patchCycle := fmt.Sprintf(`{"after":[%d]}`, resB.ID)
	reqCycle, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/tasks/%d", srv.URL, resA.ID), bytes.NewBufferString(patchCycle))
	reqCycle.Header.Set("Content-Type", "application/json")
	respCycle, err := http.DefaultClient.Do(reqCycle)
	if err != nil {
		t.Fatalf("PATCH cycle failed: %v", err)
	}
	defer respCycle.Body.Close()
	if respCycle.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for cycle, got %d", respCycle.StatusCode)
	}
	var errCycle map[string]string
	json.NewDecoder(respCycle.Body).Decode(&errCycle)
	if errCycle["field"] != "after" {
		t.Fatalf("expected field=after for cycle, got %q", errCycle["field"])
	}
	if errCycle["error"] != "dependency cycle" {
		t.Fatalf("expected 'dependency cycle', got %q", errCycle["error"])
	}

	// Longer chain: X -> Y -> Z, then X after Z -> 400
	postX := `{"project":"p","body":"task X"}`
	respX, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postX))
	var resX struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respX.Body).Decode(&resX)
	respX.Body.Close()

	postY := fmt.Sprintf(`{"project":"p","body":"task Y","after":[%d]}`, resX.ID)
	respY, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postY))
	var resY struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respY.Body).Decode(&resY)
	respY.Body.Close()

	postZ := fmt.Sprintf(`{"project":"p","body":"task Z","after":[%d]}`, resY.ID)
	respZ, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postZ))
	var resZ struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respZ.Body).Decode(&resZ)
	respZ.Body.Close()

	patchLongCycle := fmt.Sprintf(`{"after":[%d]}`, resZ.ID)
	reqLongCycle, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/tasks/%d", srv.URL, resX.ID), bytes.NewBufferString(patchLongCycle))
	reqLongCycle.Header.Set("Content-Type", "application/json")
	respLongCycle, err := http.DefaultClient.Do(reqLongCycle)
	if err != nil {
		t.Fatalf("PATCH long cycle failed: %v", err)
	}
	defer respLongCycle.Body.Close()
	if respLongCycle.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for long cycle, got %d", respLongCycle.StatusCode)
	}
	var errLongCycle map[string]string
	json.NewDecoder(respLongCycle.Body).Decode(&errLongCycle)
	if errLongCycle["field"] != "after" || errLongCycle["error"] != "dependency cycle" {
		t.Fatalf("expected field=after and 'dependency cycle', got %+v", errLongCycle)
	}

	// Batch create: referencing existing task succeeds
	batchPayload := fmt.Sprintf(`[{"project":"p","body":"b1","after":[%d]},{"project":"p","body":"b2"}]`, resA.ID)
	respBatch, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(batchPayload))
	if err != nil || respBatch.StatusCode != http.StatusCreated {
		t.Fatalf("batch create with existing dep failed: %v, status: %d", err, respBatch.StatusCode)
	}
	respBatch.Body.Close()

	// Batch create: referencing non-existent task fails
	batchBad := `[{"project":"p","body":"b1"},{"project":"p","body":"b2","after":[99999]}]`
	respBatchBad, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(batchBad))
	if err != nil {
		t.Fatalf("batch bad post failed: %v", err)
	}
	defer respBatchBad.Body.Close()
	if respBatchBad.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for batch bad dep, got %d", respBatchBad.StatusCode)
	}
	var errBatchBad map[string]string
	json.NewDecoder(respBatchBad.Body).Decode(&errBatchBad)
	if errBatchBad["field"] != "after" {
		t.Fatalf("expected field=after, got %q", errBatchBad["field"])
	}
}

func TestDepsClaimGate(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	// A pending, priority 3
	postA := `{"project":"p","body":"task A","priority":3}`
	respA, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postA))
	var resA struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respA.Body).Decode(&resA)
	respA.Body.Close()

	// B after A, priority 1
	postB := fmt.Sprintf(`{"project":"p","body":"task B","priority":1,"after":[%d]}`, resA.ID)
	respB, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postB))
	var resB struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respB.Body).Decode(&resB)
	respB.Body.Close()

	// POST /tasks/claim returns A not B
	claimBody := `{"worker":"w1","project":"p"}`
	respClaim, err := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewBufferString(claimBody))
	if err != nil {
		t.Fatalf("POST claim failed: %v", err)
	}
	defer respClaim.Body.Close()
	if respClaim.StatusCode != http.StatusOK {
		t.Fatalf("POST claim expected 200, got %d", respClaim.StatusCode)
	}
	var claimedA taskItem
	json.NewDecoder(respClaim.Body).Decode(&claimedA)
	if claimedA.ID != resA.ID {
		t.Fatalf("claim expected task A (%d), got task %d", resA.ID, claimedA.ID)
	}

	// per-id claim of B -> 409
	claimBBody := `{"worker":"w1"}`
	respClaimB, err := http.Post(fmt.Sprintf("%s/tasks/%d/claim", srv.URL, resB.ID), "application/json", bytes.NewBufferString(claimBBody))
	if err != nil {
		t.Fatalf("POST claim B failed: %v", err)
	}
	defer respClaimB.Body.Close()
	if respClaimB.StatusCode != http.StatusConflict {
		t.Fatalf("per-id claim of blocked task B expected 409, got %d", respClaimB.StatusCode)
	}
	var errClaimB map[string]string
	json.NewDecoder(respClaimB.Body).Decode(&errClaimB)
	if errClaimB["error"] != "task is waiting on other tasks" {
		t.Fatalf("expected 'task is waiting on other tasks', got %q", errClaimB["error"])
	}

	// done A
	doneBody := `{"worker":"w1"}`
	respDone, err := http.Post(fmt.Sprintf("%s/tasks/%d/done", srv.URL, resA.ID), "application/json", bytes.NewBufferString(doneBody))
	if err != nil {
		t.Fatalf("POST done A failed: %v", err)
	}
	defer respDone.Body.Close()
	if respDone.StatusCode != http.StatusNoContent {
		t.Fatalf("POST done A expected 204, got %d", respDone.StatusCode)
	}

	// claim now returns B
	respClaim2, err := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewBufferString(claimBody))
	if err != nil {
		t.Fatalf("POST claim 2 failed: %v", err)
	}
	defer respClaim2.Body.Close()
	if respClaim2.StatusCode != http.StatusOK {
		t.Fatalf("POST claim 2 expected 200, got %d", respClaim2.StatusCode)
	}
	var claimedB taskItem
	json.NewDecoder(respClaim2.Body).Decode(&claimedB)
	if claimedB.ID != resB.ID {
		t.Fatalf("claim 2 expected task B (%d), got task %d", resB.ID, claimedB.ID)
	}
}

func TestDepsDeleteCascade(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	// B after A
	postA := `{"project":"p","body":"task A"}`
	respA, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postA))
	var resA struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respA.Body).Decode(&resA)
	respA.Body.Close()

	postB := fmt.Sprintf(`{"project":"p","body":"task B","after":[%d]}`, resA.ID)
	respB, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postB))
	var resB struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respB.Body).Decode(&resB)
	respB.Body.Close()

	// DELETE A
	reqDel, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/tasks/%d", srv.URL, resA.ID), nil)
	respDel, err := http.DefaultClient.Do(reqDel)
	if err != nil {
		t.Fatalf("DELETE A failed: %v", err)
	}
	defer respDel.Body.Close()
	if respDel.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE A expected 204, got %d", respDel.StatusCode)
	}

	// claim returns B
	claimBody := `{"worker":"w1","project":"p"}`
	respClaim, err := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewBufferString(claimBody))
	if err != nil {
		t.Fatalf("POST claim failed: %v", err)
	}
	defer respClaim.Body.Close()
	if respClaim.StatusCode != http.StatusOK {
		t.Fatalf("POST claim expected 200, got %d", respClaim.StatusCode)
	}
	var claimedB taskItem
	json.NewDecoder(respClaim.Body).Decode(&claimedB)
	if claimedB.ID != resB.ID {
		t.Fatalf("expected claimed task B (%d), got %d", resB.ID, claimedB.ID)
	}

	// task_deps has no rows
	var count int
	if err := db.ro.QueryRow("SELECT count(*) FROM task_deps").Scan(&count); err != nil {
		t.Fatalf("query task_deps count failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected task_deps to have 0 rows, got %d", count)
	}
}

func TestDepsPatchReplaces(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	// Create A, B, C
	var ids [3]int64
	for i, name := range []string{"A", "B", "C"} {
		resp, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(fmt.Sprintf(`{"project":"p","body":"task %s"}`, name)))
		var res struct {
			ID int64 `json:"id"`
		}
		json.NewDecoder(resp.Body).Decode(&res)
		resp.Body.Close()
		ids[i] = res.ID
	}
	idA, idB, idC := ids[0], ids[1], ids[2]

	getTask := func(id int64) taskDetail {
		resp, err := http.Get(fmt.Sprintf("%s/tasks/%d", srv.URL, id))
		if err != nil {
			t.Fatalf("GET task failed: %v", err)
		}
		defer resp.Body.Close()
		var d taskDetail
		json.NewDecoder(resp.Body).Decode(&d)
		return d
	}

	initialB := getTask(idB)
	if len(initialB.After) != 0 {
		t.Fatalf("initial after expected empty, got %v", initialB.After)
	}
	v0 := initialB.Version

	// PATCH after [A]
	patch1 := fmt.Sprintf(`{"after":[%d]}`, idA)
	req1, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/tasks/%d", srv.URL, idB), bytes.NewBufferString(patch1))
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil || resp1.StatusCode != http.StatusNoContent {
		t.Fatalf("PATCH 1 failed: %v, status: %d", err, resp1.StatusCode)
	}
	resp1.Body.Close()

	d1 := getTask(idB)
	if !reflect.DeepEqual(d1.After, []int64{idA}) {
		t.Fatalf("expected after [%d], got %v", idA, d1.After)
	}
	if d1.Version <= v0 {
		t.Fatalf("expected version to increment from %d, got %d", v0, d1.Version)
	}

	// PATCH after []
	patch2 := `{"after":[]}`
	req2, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/tasks/%d", srv.URL, idB), bytes.NewBufferString(patch2))
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil || resp2.StatusCode != http.StatusNoContent {
		t.Fatalf("PATCH 2 failed: %v, status: %d", err, resp2.StatusCode)
	}
	resp2.Body.Close()

	d2 := getTask(idB)
	if len(d2.After) != 0 {
		t.Fatalf("expected after [], got %v", d2.After)
	}
	if d2.Version <= d1.Version {
		t.Fatalf("expected version to increment from %d, got %d", d1.Version, d2.Version)
	}

	// PATCH after [C]
	patch3 := fmt.Sprintf(`{"after":[%d]}`, idC)
	req3, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/tasks/%d", srv.URL, idB), bytes.NewBufferString(patch3))
	req3.Header.Set("Content-Type", "application/json")
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil || resp3.StatusCode != http.StatusNoContent {
		t.Fatalf("PATCH 3 failed: %v, status: %d", err, resp3.StatusCode)
	}
	resp3.Body.Close()

	d3 := getTask(idB)
	if !reflect.DeepEqual(d3.After, []int64{idC}) {
		t.Fatalf("expected after [%d], got %v", idC, d3.After)
	}
	if d3.Version <= d2.Version {
		t.Fatalf("expected version to increment from %d, got %d", d2.Version, d3.Version)
	}
}

func TestDepsWakeWaiter(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	// Task A on project pA
	postA := `{"project":"pA","body":"task A"}`
	respA, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postA))
	var resA struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respA.Body).Decode(&resA)
	respA.Body.Close()

	// Task B on project pB, waiting on task A
	postB := fmt.Sprintf(`{"project":"pB","body":"task B","after":[%d]}`, resA.ID)
	respB, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postB))
	var resB struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respB.Body).Decode(&resB)
	respB.Body.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		status int
		taskID int64
		err    error
	}
	ch := make(chan result, 1)

	go func() {
		claimBody, _ := json.Marshal(map[string]any{"worker": "w-pB", "project": "pB", "wait": 5})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/tasks/claim", bytes.NewReader(claimBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer resp.Body.Close()
		var item taskItem
		if resp.StatusCode == http.StatusOK {
			_ = json.NewDecoder(resp.Body).Decode(&item)
		}
		ch <- result{status: resp.StatusCode, taskID: item.ID}
	}()

	for db.waiterCount() == 0 {
		time.Sleep(time.Millisecond)
	}

	// Claim A on project pA
	claimABody := `{"worker":"w-pA","project":"pA"}`
	respClaimA, err := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewBufferString(claimABody))
	if err != nil || respClaimA.StatusCode != http.StatusOK {
		t.Fatalf("claim A failed: %v, status %d", err, respClaimA.StatusCode)
	}
	respClaimA.Body.Close()

	// Done A
	doneABody := `{"worker":"w-pA"}`
	respDoneA, err := http.Post(fmt.Sprintf("%s/tasks/%d/done", srv.URL, resA.ID), "application/json", bytes.NewBufferString(doneABody))
	if err != nil || respDoneA.StatusCode != http.StatusNoContent {
		t.Fatalf("done A failed: %v, status %d", err, respDoneA.StatusCode)
	}
	respDoneA.Body.Close()

	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("waiting claim error: %v", res.err)
		}
		if res.status != http.StatusOK {
			t.Fatalf("waiting claim status = %d, want 200", res.status)
		}
		if res.taskID != resB.ID {
			t.Fatalf("claimed task = %d, want %d", res.taskID, resB.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiting claim on project pB did not unblock in time after dependency completed")
	}
}

func TestMigrationV15(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v14.db")
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}

	v14Schema := `
CREATE TABLE tasks (
  id INTEGER PRIMARY KEY,
  status TEXT DEFAULT 'pending',
  worker TEXT CHECK (octet_length(worker) <= 128),
  lease_expires INTEGER,
  primitives JSON,
  body TEXT NOT NULL DEFAULT '',
  priority INTEGER NOT NULL DEFAULT 3 CHECK (priority >= 0),
  project TEXT NOT NULL DEFAULT '',
  claim_count INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL DEFAULT (unixepoch()),
  version INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_tasks_pending ON tasks (priority ASC, created_at ASC) WHERE status = 'pending';
CREATE INDEX idx_tasks_pending_project ON tasks (project, priority ASC, created_at ASC) WHERE status = 'pending';
CREATE INDEX idx_tasks_lease_timeout ON tasks (lease_expires ASC) WHERE status = 'leased';
CREATE INDEX idx_tasks_project ON tasks (project, status, priority ASC);
CREATE INDEX idx_tasks_claim_count ON tasks (status, claim_count) WHERE claim_count > 0;
CREATE INDEX idx_tasks_done ON tasks (project) WHERE status = 'done';
CREATE TABLE notes (
  id INTEGER PRIMARY KEY,
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  author TEXT NOT NULL,
  text TEXT NOT NULL
);
CREATE INDEX idx_notes_task_id ON notes (task_id);
PRAGMA user_version = 14;`

	if _, err := rawDB.Exec(v14Schema); err != nil {
		rawDB.Close()
		t.Fatalf("init v14 schema failed: %v", err)
	}
	if _, err := rawDB.Exec("INSERT INTO tasks (id, body, project) VALUES (1, 'task 1', 'p')"); err != nil {
		rawDB.Close()
		t.Fatalf("insert task failed: %v", err)
	}
	rawDB.Close()

	// Open with taskd store
	store, err := openDB(dbPath, 0)
	if err != nil {
		t.Fatalf("openDB migration failed: %v", err)
	}
	defer store.Close()

	var version int
	if err := store.ro.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version failed: %v", err)
	}
	if version != 15 {
		t.Fatalf("expected user_version 15, got %d", version)
	}

	var hasTable int
	if err := store.ro.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='task_deps'").Scan(&hasTable); err != nil {
		t.Fatalf("check task_deps table failed: %v", err)
	}
	if hasTable != 1 {
		t.Fatalf("task_deps table missing")
	}

	rows, err := store.ro.Query("PRAGMA table_info('task_deps')")
	if err != nil {
		t.Fatalf("table_info failed: %v", err)
	}
	defer rows.Close()

	colNames := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan col info failed: %v", err)
		}
		colNames[name] = true
	}
	if !colNames["task_id"] || !colNames["depends_on_id"] {
		t.Fatalf("expected columns task_id and depends_on_id, got %v", colNames)
	}
}

func TestDepsListBatched(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	// Task 1: no deps
	post1 := `{"project":"p","body":"task 1"}`
	resp1, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(post1))
	if err != nil || resp1.StatusCode != http.StatusCreated {
		t.Fatalf("create task 1 failed: %v, status %d", err, resp1.StatusCode)
	}
	var res1 struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(resp1.Body).Decode(&res1)
	resp1.Body.Close()

	// Task 2: depends on task 1
	post2 := fmt.Sprintf(`{"project":"p","body":"task 2","after":[%d]}`, res1.ID)
	resp2, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(post2))
	if err != nil || resp2.StatusCode != http.StatusCreated {
		t.Fatalf("create task 2 failed: %v, status %d", err, resp2.StatusCode)
	}
	var res2 struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(resp2.Body).Decode(&res2)
	resp2.Body.Close()

	// Task 3: depends on task 1 and task 2
	post3 := fmt.Sprintf(`{"project":"p","body":"task 3","after":[%d,%d]}`, res1.ID, res2.ID)
	resp3, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(post3))
	if err != nil || resp3.StatusCode != http.StatusCreated {
		t.Fatalf("create task 3 failed: %v, status %d", err, resp3.StatusCode)
	}
	var res3 struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(resp3.Body).Decode(&res3)
	resp3.Body.Close()

	// 1. GET /tasks returns correct after arrays
	respList, err := http.Get(srv.URL + "/tasks")
	if err != nil {
		t.Fatalf("GET /tasks failed: %v", err)
	}
	defer respList.Body.Close()
	if respList.StatusCode != http.StatusOK {
		t.Fatalf("GET /tasks expected 200, got %d", respList.StatusCode)
	}
	var tasks []taskItem
	if err := json.NewDecoder(respList.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode tasks failed: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	afterMap := make(map[int64][]int64)
	for _, task := range tasks {
		afterMap[task.ID] = task.After
	}
	if !reflect.DeepEqual(afterMap[res1.ID], []int64{}) {
		t.Fatalf("expected task 1 after to be [], got %v", afterMap[res1.ID])
	}
	if !reflect.DeepEqual(afterMap[res2.ID], []int64{res1.ID}) {
		t.Fatalf("expected task 2 after to be [%d], got %v", res1.ID, afterMap[res2.ID])
	}
	if !reflect.DeepEqual(afterMap[res3.ID], []int64{res1.ID, res2.ID}) {
		t.Fatalf("expected task 3 after to be [%d, %d], got %v", res1.ID, res2.ID, afterMap[res3.ID])
	}

	// 2. GET /tasks?fields=id,status returns no after key
	respProjNoAfter, err := http.Get(srv.URL + "/tasks?fields=id,status")
	if err != nil {
		t.Fatalf("GET /tasks?fields=id,status failed: %v", err)
	}
	defer respProjNoAfter.Body.Close()
	if respProjNoAfter.StatusCode != http.StatusOK {
		t.Fatalf("GET /tasks?fields=id,status expected 200, got %d", respProjNoAfter.StatusCode)
	}
	var rawTasks []map[string]any
	if err := json.NewDecoder(respProjNoAfter.Body).Decode(&rawTasks); err != nil {
		t.Fatalf("decode raw tasks failed: %v", err)
	}
	if len(rawTasks) != 3 {
		t.Fatalf("expected 3 raw tasks, got %d", len(rawTasks))
	}
	for _, raw := range rawTasks {
		if _, ok := raw["after"]; ok {
			t.Fatalf("unexpected 'after' key in projection fields=id,status: %v", raw)
		}
	}

	// 3. GET /tasks?fields=id,after returns it
	respProjAfter, err := http.Get(srv.URL + "/tasks?fields=id,after")
	if err != nil {
		t.Fatalf("GET /tasks?fields=id,after failed: %v", err)
	}
	defer respProjAfter.Body.Close()
	if respProjAfter.StatusCode != http.StatusOK {
		t.Fatalf("GET /tasks?fields=id,after expected 200, got %d", respProjAfter.StatusCode)
	}
	var projAfterTasks []struct {
		ID    int64   `json:"id"`
		After []int64 `json:"after"`
	}
	if err := json.NewDecoder(respProjAfter.Body).Decode(&projAfterTasks); err != nil {
		t.Fatalf("decode proj after tasks failed: %v", err)
	}
	if len(projAfterTasks) != 3 {
		t.Fatalf("expected 3 projected tasks, got %d", len(projAfterTasks))
	}
	projAfterMap := make(map[int64][]int64)
	for _, task := range projAfterTasks {
		projAfterMap[task.ID] = task.After
	}
	if !reflect.DeepEqual(projAfterMap[res1.ID], []int64{}) {
		t.Fatalf("expected task 1 proj after to be [], got %v", projAfterMap[res1.ID])
	}
	if !reflect.DeepEqual(projAfterMap[res2.ID], []int64{res1.ID}) {
		t.Fatalf("expected task 2 proj after to be [%d], got %v", res1.ID, projAfterMap[res2.ID])
	}
	if !reflect.DeepEqual(projAfterMap[res3.ID], []int64{res1.ID, res2.ID}) {
		t.Fatalf("expected task 3 proj after to be [%d, %d], got %v", res1.ID, res2.ID, projAfterMap[res3.ID])
	}
}

func TestDepsDeleteWakesWaiter(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	// Task A on project pA
	postA := `{"project":"pA","body":"task A"}`
	respA, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postA))
	var resA struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respA.Body).Decode(&resA)
	respA.Body.Close()

	// Task B on project pB, waiting on task A
	postB := fmt.Sprintf(`{"project":"pB","body":"task B","after":[%d]}`, resA.ID)
	respB, _ := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(postB))
	var resB struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(respB.Body).Decode(&resB)
	respB.Body.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		status int
		taskID int64
		err    error
	}
	ch := make(chan result, 1)

	go func() {
		claimBody, _ := json.Marshal(map[string]any{"worker": "w-pB", "project": "pB", "wait": 5})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/tasks/claim", bytes.NewReader(claimBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer resp.Body.Close()
		var item taskItem
		if resp.StatusCode == http.StatusOK {
			_ = json.NewDecoder(resp.Body).Decode(&item)
		}
		ch <- result{status: resp.StatusCode, taskID: item.ID}
	}()

	for db.waiterCount() == 0 {
		time.Sleep(time.Millisecond)
	}

	// DELETE A
	delReq, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/tasks/%d", srv.URL, resA.ID), nil)
	respDel, err := http.DefaultClient.Do(delReq)
	if err != nil || respDel.StatusCode != http.StatusNoContent {
		t.Fatalf("delete A failed: %v, status %d", err, respDel.StatusCode)
	}
	respDel.Body.Close()

	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("waiting claim error: %v", res.err)
		}
		if res.status != http.StatusOK {
			t.Fatalf("waiting claim status = %d, want 200", res.status)
		}
		if res.taskID != resB.ID {
			t.Fatalf("claimed task = %d, want %d", res.taskID, resB.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiting claim on project pB did not unblock in time after dependency deleted")
	}
}
