package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnknownQueryParams(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	taskID := createTask(t, srv.URL, "p1")

	invalidCases := []struct {
		method string
		url    string
		body   any
		param  string
	}{
		{http.MethodGet, srv.URL + "/stats?%zz", nil, "invalid query string"},
		{http.MethodGet, srv.URL + "/tasks?%zz", nil, "invalid query string"},
		{http.MethodGet, srv.URL + "/stats?z=1&a=2", nil, "unknown query parameter: a"},
		{http.MethodGet, srv.URL + "/stats?proejct=x", nil, "proejct"},
		{http.MethodGet, srv.URL + "/stats?proj=test", nil, "proj"},
		{http.MethodGet, srv.URL + "/stats?project=p1&extra=1", nil, "extra"},
		{http.MethodGet, srv.URL + "/projects?foo=1", nil, "foo"},
		{http.MethodGet, srv.URL + "/tasks/" + taskID + "?foo=1", nil, "foo"},
		{http.MethodGet, srv.URL + "/tasks/any?foo=1", nil, "foo"},
		{http.MethodPost, srv.URL + "/tasks?foo=1", map[string]any{"body": "t"}, "foo"},
		{http.MethodPost, srv.URL + "/tasks?project=other", map[string]any{"body": "t"}, "project"},
		{http.MethodPost, srv.URL + "/tasks/claim?foo=1", map[string]any{"worker": "w1"}, "foo"},
		{http.MethodPost, srv.URL + "/tasks/" + taskID + "/claim?foo=1", map[string]any{"worker": "w1"}, "foo"},
		{http.MethodPost, srv.URL + "/tasks/any/claim?foo=1", map[string]any{"worker": "w1"}, "foo"},
		{http.MethodPost, srv.URL + "/tasks/" + taskID + "/done?foo=1", map[string]any{"worker": "w1"}, "foo"},
		{http.MethodPost, srv.URL + "/tasks/any/done?foo=1", map[string]any{"worker": "w1"}, "foo"},
		{http.MethodPost, srv.URL + "/tasks/" + taskID + "/close?foo=1", nil, "foo"},
		{http.MethodPost, srv.URL + "/tasks/any/close?foo=1", nil, "foo"},
		{http.MethodPost, srv.URL + "/tasks/" + taskID + "/touch?foo=1", map[string]any{"worker": "w1"}, "foo"},
		{http.MethodPost, srv.URL + "/tasks/any/touch?foo=1", map[string]any{"worker": "w1"}, "foo"},
		{http.MethodPost, srv.URL + "/tasks/" + taskID + "/release?foo=1", map[string]any{"worker": "w1"}, "foo"},
		{http.MethodPost, srv.URL + "/tasks/any/release?foo=1", map[string]any{"worker": "w1"}, "foo"},
		{http.MethodPatch, srv.URL + "/tasks/" + taskID + "?foo=1", map[string]any{"priority": 2}, "foo"},
		{http.MethodPatch, srv.URL + "/tasks/" + taskID + "?priority=1", map[string]any{"priority": 2}, "priority"},
		{http.MethodPatch, srv.URL + "/tasks/any?foo=1", map[string]any{"priority": 2}, "foo"},
		{http.MethodDelete, srv.URL + "/tasks/" + taskID + "?foo=1", nil, "foo"},
		{http.MethodDelete, srv.URL + "/tasks/" + taskID + "?forced=true", nil, "forced"},
		{http.MethodDelete, srv.URL + "/tasks/" + taskID + "?force=true&extra=1", nil, "extra"},
		{http.MethodDelete, srv.URL + "/tasks/any?foo=1", nil, "foo"},
	}

	for _, tc := range invalidCases {
		var code int
		var respBody []byte
		if tc.body != nil {
			data, err := json.Marshal(tc.body)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}
			code, respBody = do(t, tc.method, tc.url, data)
		} else {
			code, respBody = do(t, tc.method, tc.url, nil)
		}
		if code != http.StatusBadRequest {
			t.Fatalf("%s %s expected 400, got %d: %s", tc.method, tc.url, code, respBody)
		}
		var expectedMsg string
		if strings.HasPrefix(tc.param, "invalid ") {
			expectedMsg = tc.param
		} else if strings.HasPrefix(tc.param, "unknown query parameter: ") {
			expectedMsg = tc.param
		} else {
			expectedMsg = "unknown query parameter: " + tc.param
		}
		if !strings.Contains(string(respBody), expectedMsg) {
			t.Fatalf("%s %s expected error message containing %q, got: %s", tc.method, tc.url, expectedMsg, respBody)
		}
	}

	validStatsURL := srv.URL + "/stats?project=p1"
	code, respBody := do(t, http.MethodGet, validStatsURL, nil)
	if code != http.StatusOK {
		t.Fatalf("GET %s expected 200, got %d: %s", validStatsURL, code, respBody)
	}

	code, respBody = do(t, http.MethodGet, srv.URL+"/projects", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /projects expected 200, got %d: %s", code, respBody)
	}

	t2 := createTask(t, srv.URL, "p1")
	code, respBody = do(t, http.MethodGet, srv.URL+"/tasks/"+t2, nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks/%s expected 200, got %d: %s", t2, code, respBody)
	}

	code, respBody = post(t, srv.URL+"/tasks/"+t2+"/claim", map[string]any{"worker": "w1"})
	if code != http.StatusOK {
		t.Fatalf("claim task %s expected 200, got %d: %s", t2, code, respBody)
	}

	code, respBody = post(t, srv.URL+"/tasks/"+t2+"/touch", map[string]any{"worker": "w1"})
	if code != http.StatusNoContent {
		t.Fatalf("touch task %s expected 204, got %d: %s", t2, code, respBody)
	}

	code, respBody = post(t, srv.URL+"/tasks/"+t2+"/release", map[string]any{"worker": "w1"})
	if code != http.StatusNoContent {
		t.Fatalf("release task %s expected 204, got %d: %s", t2, code, respBody)
	}

	code, respBody = do(t, http.MethodPatch, srv.URL+"/tasks/"+t2, []byte(`{"priority":1}`))
	if code != http.StatusNoContent {
		t.Fatalf("patch task %s expected 204, got %d: %s", t2, code, respBody)
	}

	code, respBody = post(t, srv.URL+"/tasks/"+t2+"/close", nil)
	if code != http.StatusNoContent {
		t.Fatalf("close task %s expected 204, got %d: %s", t2, code, respBody)
	}

	deleteURL := srv.URL + "/tasks/" + t2 + "?force=true"
	code, respBody = do(t, http.MethodDelete, deleteURL, nil)
	if code != http.StatusNoContent {
		t.Fatalf("DELETE %s expected 204, got %d: %s", deleteURL, code, respBody)
	}
}
