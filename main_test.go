package main

import (
	"net/http"
	"net/http/httptest"
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
