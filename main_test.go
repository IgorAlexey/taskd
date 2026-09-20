package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCORSHeaders(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandlerWithCORS(db, 300, 0, "http://example.com"))
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
	for _, want := range []string{"X-Total-Count", "X-Next-Cursor", "ETag"} {
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
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	db.Close()

	destDir := filepath.Join(dir, "backup-dir")
	if err := os.Mkdir(destDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	err = run([]string{"-db", dbPath, "-backup", destDir})
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
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
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
