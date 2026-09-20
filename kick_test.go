package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestBulkKick(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	for i := 0; i < 15; i++ {
		_, err := db.rw.Exec(
			"INSERT INTO tasks (id, project, status, body, priority, claim_count, worker, created_at) VALUES (?, 'test', 'buried', ?, 3, 2, 'w1', ?)",
			"test-"+string(rune('a'+i)), "task", int64(1000+i),
		)
		if err != nil {
			t.Fatalf("insert test task %d failed: %v", i, err)
		}
	}

	for i := 0; i < 3; i++ {
		_, err := db.rw.Exec(
			"INSERT INTO tasks (id, project, status, body, priority, claim_count, worker, created_at) VALUES (?, 'other', 'buried', ?, 3, 1, 'w2', ?)",
			"other-"+string(rune('a'+i)), "task", int64(2000+i),
		)
		if err != nil {
			t.Fatalf("insert other task %d failed: %v", i, err)
		}
	}

	payload := []byte(`{"project":"test","limit":10}`)
	resp, err := http.Post(srv.URL+"/tasks/kick", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /tasks/kick failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var res struct {
		Kicked int `json:"kicked"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res.Kicked != 10 {
		t.Fatalf("expected kicked 10, got %d", res.Kicked)
	}

	var pendingCount, buriedTestCount, buriedOtherCount int
	if err := db.ro.QueryRow("SELECT COUNT(*) FROM tasks WHERE project = 'test' AND status = 'pending'").Scan(&pendingCount); err != nil {
		t.Fatalf("query pending failed: %v", err)
	}
	if pendingCount != 10 {
		t.Fatalf("expected 10 pending tasks in test, got %d", pendingCount)
	}

	if err := db.ro.QueryRow("SELECT COUNT(*) FROM tasks WHERE project = 'test' AND status = 'buried'").Scan(&buriedTestCount); err != nil {
		t.Fatalf("query buried test failed: %v", err)
	}
	if buriedTestCount != 5 {
		t.Fatalf("expected 5 buried tasks in test, got %d", buriedTestCount)
	}

	if err := db.ro.QueryRow("SELECT COUNT(*) FROM tasks WHERE project = 'other' AND status = 'buried'").Scan(&buriedOtherCount); err != nil {
		t.Fatalf("query buried other failed: %v", err)
	}
	if buriedOtherCount != 3 {
		t.Fatalf("expected 3 buried tasks in other, got %d", buriedOtherCount)
	}

	var claimsReset, workersReset int
	if err := db.ro.QueryRow("SELECT COUNT(*) FROM tasks WHERE project = 'test' AND status = 'pending' AND claim_count = 0").Scan(&claimsReset); err != nil {
		t.Fatalf("query claims reset failed: %v", err)
	}
	if claimsReset != 10 {
		t.Fatalf("expected claim_count reset to 0 for all 10 kicked tasks, got %d", claimsReset)
	}

	if err := db.ro.QueryRow("SELECT COUNT(*) FROM tasks WHERE project = 'test' AND status = 'pending' AND worker IS NULL").Scan(&workersReset); err != nil {
		t.Fatalf("query workers reset failed: %v", err)
	}
	if workersReset != 10 {
		t.Fatalf("expected worker reset to NULL for all 10 kicked tasks, got %d", workersReset)
	}

	respAll, err := http.Post(srv.URL+"/tasks/kick", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("POST /tasks/kick all failed: %v", err)
	}
	defer respAll.Body.Close()

	if respAll.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for all kick, got %d", respAll.StatusCode)
	}

	var resAll struct {
		Kicked int `json:"kicked"`
	}
	if err := json.NewDecoder(respAll.Body).Decode(&resAll); err != nil {
		t.Fatalf("failed to decode all response: %v", err)
	}
	if resAll.Kicked != 8 {
		t.Fatalf("expected kicked 8 (5 test + 3 other), got %d", resAll.Kicked)
	}

	respZero, err := http.Post(srv.URL+"/tasks/kick", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("POST /tasks/kick zero failed: %v", err)
	}
	defer respZero.Body.Close()
	var resZero struct {
		Kicked int `json:"kicked"`
	}
	if err := json.NewDecoder(respZero.Body).Decode(&resZero); err != nil {
		t.Fatalf("failed to decode zero response: %v", err)
	}
	if resZero.Kicked != 0 {
		t.Fatalf("expected kicked 0 when no buried tasks, got %d", resZero.Kicked)
	}

	respNeg, err := http.Post(srv.URL+"/tasks/kick", "application/json", bytes.NewReader([]byte(`{"limit":-1}`)))
	if err != nil {
		t.Fatalf("POST /tasks/kick neg limit failed: %v", err)
	}
	defer respNeg.Body.Close()
	if respNeg.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 for negative limit, got %d", respNeg.StatusCode)
	}

	respInvalidProj, err := http.Post(srv.URL+"/tasks/kick", "application/json", bytes.NewReader([]byte(`{"project":"bad proj!"}`)))
	if err != nil {
		t.Fatalf("POST /tasks/kick invalid proj failed: %v", err)
	}
	defer respInvalidProj.Body.Close()
	if respInvalidProj.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 for invalid project, got %d", respInvalidProj.StatusCode)
	}

	respGarbageLimit, err := http.Post(srv.URL+"/tasks/kick?limit=garbage", "application/json", bytes.NewReader([]byte(`{"project":"test"}`)))
	if err != nil {
		t.Fatalf("POST /tasks/kick garbage limit failed: %v", err)
	}
	defer respGarbageLimit.Body.Close()
	if respGarbageLimit.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 for garbage limit, got %d", respGarbageLimit.StatusCode)
	}

	respConfLimit, err := http.Post(srv.URL+"/tasks/kick?limit=5", "application/json", bytes.NewReader([]byte(`{"limit":10}`)))
	if err != nil {
		t.Fatalf("POST /tasks/kick conf limit failed: %v", err)
	}
	defer respConfLimit.Body.Close()
	if respConfLimit.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 for conflicting limit, got %d", respConfLimit.StatusCode)
	}

	respConfProj, err := http.Post(srv.URL+"/tasks/kick?project=p1", "application/json", bytes.NewReader([]byte(`{"project":"p2"}`)))
	if err != nil {
		t.Fatalf("POST /tasks/kick conf proj failed: %v", err)
	}
	defer respConfProj.Body.Close()
	if respConfProj.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 for conflicting project, got %d", respConfProj.StatusCode)
	}
}
func TestBulkKickWakesWaiters(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	for i := 1; i <= 2; i++ {
		_, err := db.rw.Exec(
			"INSERT INTO tasks (id, project, status, body, priority, claim_count, worker, created_at) VALUES (?, 'wake-proj', 'buried', 'task', 3, 1, 'w1', ?)",
			"wake-"+string(rune('0'+i)), int64(3000+i),
		)
		if err != nil {
			t.Fatalf("insert buried task failed: %v", err)
		}
	}

	var wg sync.WaitGroup
	claimed := make(chan string, 2)

	for i := 1; i <= 2; i++ {
		wg.Add(1)
		workerName := "worker-" + string(rune('0'+i))
		go func(worker string) {
			defer wg.Done()
			claimPayload := []byte(`{"worker":"` + worker + `","project":"wake-proj","wait":5}`)
			resp, err := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimPayload))
			if err != nil {
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return
			}
			var item struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&item); err == nil && item.ID != "" {
				claimed <- item.ID
			}
		}(workerName)
	}

	for i := 0; i < 50; i++ {
		if db.waiterCount() >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	kickPayload := []byte(`{"project":"wake-proj","limit":2}`)
	resp, err := http.Post(srv.URL+"/tasks/kick", "application/json", bytes.NewReader(kickPayload))
	if err != nil {
		t.Fatalf("POST /tasks/kick failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	wg.Wait()
	close(claimed)

	var count int
	for range claimed {
		count++
	}
	if count != 2 {
		t.Fatalf("expected both waiters to be woken and claim tasks, got %d", count)
	}
}

func TestBulkKickResetsPrimitives(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createPayload := []byte(`{"project":"kick-proj","body":"task to bury"}`)
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createPayload))
	if err != nil {
		t.Fatalf("POST /tasks failed: %v", err)
	}
	defer resp.Body.Close()

	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created task: %v", err)
	}

	claimPayload := []byte(`{"worker":"w1","project":"kick-proj"}`)
	claimResp, err := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimPayload))
	if err != nil {
		t.Fatalf("POST /tasks/claim failed: %v", err)
	}
	claimResp.Body.Close()

	buryPayload := []byte(`{"worker":"w1","primitives":{"error":"oom"}}`)
	buryResp, err := http.Post(srv.URL+"/tasks/"+created.ID+"/bury", "application/json", bytes.NewReader(buryPayload))
	if err != nil {
		t.Fatalf("POST /tasks/{id}/bury failed: %v", err)
	}
	buryResp.Body.Close()

	kickResp, err := http.Post(srv.URL+"/tasks/kick", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("POST /tasks/kick failed: %v", err)
	}
	defer kickResp.Body.Close()

	if kickResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", kickResp.StatusCode)
	}

	var kickResult struct {
		Kicked int `json:"kicked"`
	}
	if err := json.NewDecoder(kickResp.Body).Decode(&kickResult); err != nil {
		t.Fatalf("decode kick result: %v", err)
	}
	if kickResult.Kicked != 1 {
		t.Fatalf("expected kicked 1, got %d", kickResult.Kicked)
	}

	getResp, err := http.Get(srv.URL + "/tasks/" + created.ID)
	if err != nil {
		t.Fatalf("GET /tasks/{id} failed: %v", err)
	}
	defer getResp.Body.Close()

	var fetched struct {
		Status     string          `json:"status"`
		Primitives json.RawMessage `json:"primitives"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode fetched task: %v", err)
	}

	if fetched.Status != "pending" {
		t.Fatalf("expected status pending, got %q", fetched.Status)
	}
	if len(fetched.Primitives) > 0 && string(fetched.Primitives) != "null" {
		t.Fatalf("expected primitives to be null, got %s", string(fetched.Primitives))
	}

	claimResp2, err := http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimPayload))
	if err != nil {
		t.Fatalf("POST /tasks/claim 2 failed: %v", err)
	}
	claimResp2.Body.Close()

	limitBuryPayload := []byte(`{"worker":"w1","primitives":{"error":"oom2"}}`)
	buryResp2, err := http.Post(srv.URL+"/tasks/"+created.ID+"/bury", "application/json", bytes.NewReader(limitBuryPayload))
	if err != nil {
		t.Fatalf("POST /tasks/{id}/bury 2 failed: %v", err)
	}
	buryResp2.Body.Close()

	kickLimitResp, err := http.Post(srv.URL+"/tasks/kick", "application/json", bytes.NewReader([]byte(`{"limit":1}`)))
	if err != nil {
		t.Fatalf("POST /tasks/kick with limit failed: %v", err)
	}
	defer kickLimitResp.Body.Close()

	getResp2, err := http.Get(srv.URL + "/tasks/" + created.ID)
	if err != nil {
		t.Fatalf("GET /tasks/{id} 2 failed: %v", err)
	}
	defer getResp2.Body.Close()

	var fetched2 struct {
		Status     string          `json:"status"`
		Primitives json.RawMessage `json:"primitives"`
	}
	if err := json.NewDecoder(getResp2.Body).Decode(&fetched2); err != nil {
		t.Fatalf("decode fetched task 2: %v", err)
	}
	if fetched2.Status != "pending" {
		t.Fatalf("expected status pending, got %q", fetched2.Status)
	}
	if len(fetched2.Primitives) > 0 && string(fetched2.Primitives) != "null" {
		t.Fatalf("expected primitives to be null after limit kick, got %s", string(fetched2.Primitives))
	}
}
