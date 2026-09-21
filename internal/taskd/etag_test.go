package taskd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestListTasksETag(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	task1Payload := `{"body":"first task","project":"p1"}`
	resp1, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(task1Payload))
	if err != nil {
		t.Fatalf("create task 1: %v", err)
	}
	var created1 struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp1.Body).Decode(&created1); err != nil {
		t.Fatalf("decode created task 1: %v", err)
	}
	resp1.Body.Close()

	task2Payload := `{"body":"second task","project":"p2"}`
	resp2, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(task2Payload))
	if err != nil {
		t.Fatalf("create task 2: %v", err)
	}
	resp2.Body.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/tasks", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /tasks: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatalf("expected non-empty ETag header")
	}

	req, err = http.NewRequest(http.MethodGet, srv.URL+"/tasks", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("If-None-Match", etag)
	resp2nd, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /tasks with If-None-Match: %v", err)
	}
	defer resp2nd.Body.Close()
	body2nd, err := io.ReadAll(resp2nd.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp2nd.StatusCode != http.StatusNotModified {
		t.Fatalf("expected 304, got %d: %s", resp2nd.StatusCode, body2nd)
	}
	if len(body2nd) != 0 {
		t.Fatalf("expected empty body on 304, got %q", string(body2nd))
	}
	if resp2nd.Header.Get("ETag") != etag {
		t.Fatalf("expected ETag %q on 304, got %q", etag, resp2nd.Header.Get("ETag"))
	}
	if resp2nd.Header.Get("X-Total-Count") != "2" {
		t.Fatalf("expected X-Total-Count 2 on 304, got %q", resp2nd.Header.Get("X-Total-Count"))
	}

	reqHead, err := http.NewRequest(http.MethodHead, srv.URL+"/tasks", nil)
	if err != nil {
		t.Fatalf("new HEAD request: %v", err)
	}
	reqHead.Header.Set("If-None-Match", etag)
	respHead, err := http.DefaultClient.Do(reqHead)
	if err != nil {
		t.Fatalf("HEAD /tasks with If-None-Match: %v", err)
	}
	respHead.Body.Close()
	if respHead.StatusCode != http.StatusNotModified {
		t.Fatalf("expected 304 on HEAD, got %d", respHead.StatusCode)
	}

	patchReq, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/tasks/%d", srv.URL, created1.ID), bytes.NewBufferString(`{"priority":1}`))
	if err != nil {
		t.Fatalf("new PATCH request: %v", err)
	}
	patchReq.Header.Set("Content-Type", "application/json")
	patchResp, err := http.DefaultClient.Do(patchReq)
	if err != nil {
		t.Fatalf("PATCH task: %v", err)
	}
	patchResp.Body.Close()
	if patchResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 from PATCH, got %d", patchResp.StatusCode)
	}

	req, err = http.NewRequest(http.MethodGet, srv.URL+"/tasks", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("If-None-Match", etag)
	respAfterPatch, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /tasks after patch: %v", err)
	}
	defer respAfterPatch.Body.Close()
	if respAfterPatch.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after patch, got %d", respAfterPatch.StatusCode)
	}
	newETag := respAfterPatch.Header.Get("ETag")
	if newETag == "" {
		t.Fatalf("expected non-empty new ETag header")
	}
	if newETag == etag {
		t.Fatalf("expected new ETag to differ from old ETag %s, got same", etag)
	}

	req, err = http.NewRequest(http.MethodGet, srv.URL+"/tasks?project=p1", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	respFiltered, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /tasks?project=p1: %v", err)
	}
	defer respFiltered.Body.Close()
	if respFiltered.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for filtered list, got %d", respFiltered.StatusCode)
	}
	filteredETag := respFiltered.Header.Get("ETag")
	if filteredETag == "" {
		t.Fatalf("expected non-empty filtered ETag header")
	}
	if filteredETag == newETag {
		t.Fatalf("expected filtered ETag to differ from unfiltered ETag %s, got same", newETag)
	}
}

func TestGetTaskETag(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	taskPayload := `{"body":"test task for etag","project":"p1"}`
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(taskPayload))
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created task: %v", err)
	}
	resp.Body.Close()

	taskURL := fmt.Sprintf("%s/tasks/%d", srv.URL, created.ID)

	req, err := http.NewRequest(http.MethodGet, taskURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /tasks/{id}: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatalf("expected non-empty ETag header")
	}

	req, err = http.NewRequest(http.MethodGet, taskURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("If-None-Match", etag)
	resp2nd, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /tasks/{id} with If-None-Match: %v", err)
	}
	defer resp2nd.Body.Close()
	body2nd, err := io.ReadAll(resp2nd.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp2nd.StatusCode != http.StatusNotModified {
		t.Fatalf("expected 304, got %d: %s", resp2nd.StatusCode, body2nd)
	}
	if len(body2nd) != 0 {
		t.Fatalf("expected empty body on 304, got %q", string(body2nd))
	}
	if resp2nd.Header.Get("ETag") != etag {
		t.Fatalf("expected ETag %q on 304, got %q", etag, resp2nd.Header.Get("ETag"))
	}

	reqHead, err := http.NewRequest(http.MethodHead, taskURL, nil)
	if err != nil {
		t.Fatalf("new HEAD request: %v", err)
	}
	reqHead.Header.Set("If-None-Match", etag)
	respHead, err := http.DefaultClient.Do(reqHead)
	if err != nil {
		t.Fatalf("HEAD /tasks/{id} with If-None-Match: %v", err)
	}
	respHead.Body.Close()
	if respHead.StatusCode != http.StatusNotModified {
		t.Fatalf("expected 304 on HEAD, got %d", respHead.StatusCode)
	}

	patchReq, err := http.NewRequest(http.MethodPatch, taskURL, bytes.NewBufferString(`{"priority":1}`))
	if err != nil {
		t.Fatalf("new PATCH request: %v", err)
	}
	patchReq.Header.Set("Content-Type", "application/json")
	patchResp, err := http.DefaultClient.Do(patchReq)
	if err != nil {
		t.Fatalf("PATCH task: %v", err)
	}
	patchResp.Body.Close()
	if patchResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 from PATCH, got %d", patchResp.StatusCode)
	}

	req, err = http.NewRequest(http.MethodGet, taskURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("If-None-Match", etag)
	respAfterPatch, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /tasks/{id} after patch: %v", err)
	}
	defer respAfterPatch.Body.Close()
	if respAfterPatch.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after patch, got %d", respAfterPatch.StatusCode)
	}
	newETag := respAfterPatch.Header.Get("ETag")
	if newETag == "" {
		t.Fatalf("expected non-empty new ETag header")
	}
	if newETag == etag {
		t.Fatalf("expected new ETag to differ from old ETag %s, got same", etag)
	}
}

func TestIfNoneMatchList(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   bool
	}{
		{`"abc"`, true},
		{`W/"abc"`, true},
		{`"x", "abc"`, true},
		{`"x",W/"abc"`, true},
		{`*`, true},
		{`"x"`, false},
		{``, false},
	} {
		if got := etagMatches(tc.header, `"abc"`); got != tc.want {
			t.Errorf("etagMatches(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}
