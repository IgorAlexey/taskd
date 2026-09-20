package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestNewClient(t *testing.T) {
	c := newClient("http://127.0.0.1:9999/")
	if c.base != "http://127.0.0.1:9999" {
		t.Fatalf("expected trimmed base, got %q", c.base)
	}
	if c.http.Timeout != 3*time.Second {
		t.Fatalf("expected 3s timeout, got %v", c.http.Timeout)
	}
}

func TestClientListETagAnd304(t *testing.T) {
	var reqCount atomic.Int32
	var receivedETag string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := reqCount.Add(1)
		receivedETag = r.Header.Get("If-None-Match")
		if n == 1 {
			w.Header().Set("ETag", `"etag-123"`)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]task{{ID: "task-1"}})
			return
		}
		if n == 2 {
			if r.Header.Get("If-None-Match") != `"etag-123"` {
				t.Errorf("expected If-None-Match %q, got %q", `"etag-123"`, r.Header.Get("If-None-Match"))
			}
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := newClient(ts.URL)

	// First call: 200 OK with ETag
	tasks, etag, changed, err := c.list("", "")
	if err != nil {
		t.Fatalf("first list failed: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true on 200")
	}
	if len(tasks) != 1 || tasks[0].ID != "task-1" {
		t.Fatalf("unexpected tasks: %v", tasks)
	}
	if etag != `"etag-123"` {
		t.Fatalf("expected returned etag %q, got %q", `"etag-123"`, etag)
	}

	// Second call with that tag: 304 Not Modified, tag echoed back
	tasks2, etag2, changed2, err2 := c.list("", etag)
	if err2 != nil {
		t.Fatalf("second list failed: %v", err2)
	}
	if changed2 || etag2 != etag {
		t.Fatalf("expected changed=false and same etag on 304, got %v %q", changed2, etag2)
	}
	if tasks2 != nil {
		t.Fatalf("expected nil tasks on 304, got %v", tasks2)
	}
	if receivedETag != `"etag-123"` {
		t.Fatalf("server did not receive If-None-Match: %q", receivedETag)
	}
}

func TestClientListErrorSurfacesDaemonMessage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "database down"})
	}))
	defer ts.Close()

	c := newClient(ts.URL)
	_, etag, changed, err := c.list("", `"old"`)
	if err == nil || err.Error() != "database down" {
		t.Fatalf("expected daemon error text, got %v", err)
	}
	if changed || etag != "" {
		t.Fatalf("error must report no change and no tag, got %v %q", changed, etag)
	}
}

func TestErrorBodySurfaces(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json-err" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "task is already leased"})
			return
		}
		if r.URL.Path == "/raw-err" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("plain text error"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := newClient(ts.URL)

	// Decodable {"error": ...}
	err := c.do(http.MethodPost, "/json-err", map[string]string{"foo": "bar"})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if err.Error() != "task is already leased" {
		t.Fatalf("expected error message %q, got %q", "task is already leased", err.Error())
	}

	// Non-decodable fallback: "<method> <path>: <status>"
	err = c.do(http.MethodDelete, "/raw-err", nil)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	expected := "DELETE /raw-err: 500 Internal Server Error"
	if err.Error() != expected {
		t.Fatalf("expected %q, got %q", expected, err.Error())
	}
}

func TestClientDoMethodAndBody(t *testing.T) {
	var receivedMethod string
	var receivedContentType string
	var receivedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := newClient(ts.URL)
	payload := map[string]any{"priority": 5, "project": "demo"}
	err := c.do(http.MethodPatch, "/tasks/task-1", payload)
	if err != nil {
		t.Fatalf("do failed: %v", err)
	}

	if receivedMethod != http.MethodPatch {
		t.Errorf("expected PATCH, got %s", receivedMethod)
	}
	if receivedContentType != "application/json" {
		t.Errorf("expected application/json, got %s", receivedContentType)
	}
	if val, ok := receivedBody["priority"].(float64); !ok || val != 5 {
		t.Errorf("expected priority 5, got %v", receivedBody["priority"])
	}
	if val, ok := receivedBody["project"].(string); !ok || val != "demo" {
		t.Errorf("expected project demo, got %v", receivedBody["project"])
	}
}

func TestPollCmdToleratesProjects500(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/tasks":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]task{{ID: "t-42", Project: "proj-a"}})
		case r.URL.Path == "/stats":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(stats{Pending: 3, Total: 10})
		case r.URL.Path == "/projects":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "projects database failure"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	c := newClient(ts.URL)
	cmd := pollCmd(c, "", "")
	if cmd == nil {
		t.Fatalf("pollCmd returned nil cmd")
	}

	msg := cmd()
	poll, ok := msg.(pollMsg)
	if !ok {
		t.Fatalf("expected pollMsg, got %T", msg)
	}

	if poll.err != nil {
		t.Fatalf("pollCmd did not tolerate /projects 500: %v", poll.err)
	}
	if len(poll.tasks) != 1 || poll.tasks[0].ID != "t-42" {
		t.Fatalf("expected tasks delivered, got %v", poll.tasks)
	}
	if !poll.changed {
		t.Fatalf("expected changed=true")
	}
	if poll.stats.Pending != 3 || poll.stats.Total != 10 {
		t.Fatalf("expected stats delivered, got %+v", poll.stats)
	}
	if poll.projects != nil {
		t.Fatalf("expected nil projects, got %v", poll.projects)
	}
}

func TestActCmd(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "cannot claim"})
	}))
	defer ts.Close()

	c := newClient(ts.URL)

	// Success case
	cmdSuccess := actCmd(c, http.MethodPost, "/ok", nil, "task claimed")
	msgSuccess := cmdSuccess()
	act, ok := msgSuccess.(actMsg)
	if !ok {
		t.Fatalf("expected actMsg, got %T", msgSuccess)
	}
	if act.err != nil {
		t.Fatalf("unexpected error: %v", act.err)
	}
	if act.msg != "task claimed" {
		t.Fatalf("expected msg %q, got %q", "task claimed", act.msg)
	}

	// Error case
	cmdFail := actCmd(c, http.MethodPost, "/fail", nil, "task claimed")
	msgFail := cmdFail()
	actFail, ok := msgFail.(actMsg)
	if !ok {
		t.Fatalf("expected actMsg, got %T", msgFail)
	}
	if actFail.err == nil {
		t.Fatalf("expected error, got nil")
	}
	if actFail.err.Error() != "cannot claim" {
		t.Fatalf("expected error %q, got %q", "cannot claim", actFail.err.Error())
	}
}

func TestCopyToClipboard(t *testing.T) {
	cmd := copyToClipboard("test-text")
	if cmd == nil {
		t.Fatalf("copyToClipboard returned nil cmd")
	}

	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg, got %T", msg)
	}

	var foundActMsg bool
	for _, child := range batch {
		if child == nil {
			continue
		}
		childMsg := child()
		if act, ok := childMsg.(actMsg); ok {
			if act.msg == "copied to clipboard" && act.err == nil {
				foundActMsg = true
			}
		}
	}
	if !foundActMsg {
		t.Fatalf("expected actMsg with 'copied to clipboard' in batch")
	}
}

func TestListAndStatsQueryEscaping(t *testing.T) {
	var requestedTasksURL string
	var requestedStatsURL string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks") {
			requestedTasksURL = r.URL.RequestURI()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]task{})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/stats") {
			requestedStatsURL = r.URL.RequestURI()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(stats{})
			return
		}
	}))
	defer ts.Close()

	c := newClient(ts.URL)
	_, _, _, _ = c.list("proj with/special", "")
	_, _ = c.getStats("proj with/special")

	if requestedTasksURL != "/tasks?limit=500&project=proj+with%2Fspecial" {
		t.Errorf("unexpected tasks URL: %q", requestedTasksURL)
	}
	if requestedStatsURL != "/stats?project=proj+with%2Fspecial" {
		t.Errorf("unexpected stats URL: %q", requestedStatsURL)
	}
}
