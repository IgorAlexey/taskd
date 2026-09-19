package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchProjectQuery(t *testing.T) {
	allTasks := []task{
		{ID: "t1", Project: "proj-a", Status: "pending"},
		{ID: "t2", Project: "proj-b", Status: "pending"},
		{ID: "t3", Project: "proj-b", Status: "done"},
	}

	var lastRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastRawQuery = r.URL.RawQuery
		project := r.URL.Query().Get("project")
		var matched []task
		for _, t := range allTasks {
			if project == "" || t.Project == project {
				matched = append(matched, t)
			}
		}
		json.NewEncoder(w).Encode(matched)
	}))
	defer srv.Close()

	u := newUI(srv.URL, "", false)

	// Default: no project filter queries limit=500 and returns all tasks
	tasks, err := u.fetch()
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	if lastRawQuery != "limit=500" {
		t.Fatalf("expected query 'limit=500', got %q", lastRawQuery)
	}

	// Active project: queries limit=500 and project=proj-b
	u.project = "proj-b"
	tasks, err = u.fetch()
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks for proj-b, got %d", len(tasks))
	}
	u.render(tasks)
	if u.pending != 1 || u.done != 1 || u.leased != 0 {
		t.Fatalf("status counts corrupted: pending=%d leased=%d done=%d", u.pending, u.leased, u.done)
	}

	// Active status filter in UI must NOT be passed to server to preserve counts
	u.filter = "pending"
	tasks, err = u.fetch()
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("fetch must return all statuses for project, got %d tasks", len(tasks))
	}
	u.render(tasks)
	if u.pending != 1 || u.done != 1 {
		t.Fatalf("status counts must reflect project total even with filter active")
	}
	if len(u.shown) != 1 || u.shown[0].ID != "t2" {
		t.Fatalf("shown tasks must filter locally: got %+v", u.shown)
	}
}
