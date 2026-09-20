package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormURLEncodedCreateTaskRoundtrip(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 300)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{
		"project":  {"zero-js-project"},
		"body":     {"task created via zero-js form submission"},
		"priority": {"2"},
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/tasks", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("NewRequest failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /tasks failed: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 See Other, got %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "/ui" {
		t.Fatalf("expected Location /ui, got %q", loc)
	}

	tasksRes, err := http.Get(srv.URL + "/tasks?project=zero-js-project")
	if err != nil {
		t.Fatalf("GET /tasks failed: %v", err)
	}
	defer tasksRes.Body.Close()

	var tasks []struct {
		ID       int64  `json:"id"`
		Project  string `json:"project"`
		Body     string `json:"body"`
		Priority int    `json:"priority"`
	}
	if err := json.NewDecoder(tasksRes.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode tasks failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task created, got %d", len(tasks))
	}
	if tasks[0].Project != "zero-js-project" || tasks[0].Body != "task created via zero-js form submission" || tasks[0].Priority != 2 {
		t.Fatalf("unexpected task data: %+v", tasks[0])
	}

	apiForm := url.Values{
		"project": {"api-project"},
		"body":    {"task created via form-encoded api"},
	}
	apiRes, err := http.Post(srv.URL+"/tasks", "application/x-www-form-urlencoded", strings.NewReader(apiForm.Encode()))
	if err != nil {
		t.Fatalf("POST /tasks form API failed: %v", err)
	}
	defer apiRes.Body.Close()

	if apiRes.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", apiRes.StatusCode)
	}
	var created map[string]any
	if err := json.NewDecoder(apiRes.Body).Decode(&created); err != nil {
		t.Fatalf("decode created task failed: %v", err)
	}
	if created["id"] == nil || created["id"] == float64(0) {
		t.Fatalf("expected non-empty id in response, got %+v", created)
	}
}
