package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
