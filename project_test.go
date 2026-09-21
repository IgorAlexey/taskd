package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestDeleteProject(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 300)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()
	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	do := func(method, path, body string) int {
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		res.Body.Close()
		return res.StatusCode
	}

	if got := do("DELETE", "/projects/nothing", ""); got != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown project, got %d", got)
	}
	for _, b := range []string{"one", "two"} {
		if got := do("POST", "/tasks", `{"project":"p1","body":"`+b+`"}`); got != http.StatusCreated {
			t.Fatalf("create: %d", got)
		}
	}
	if got := do("POST", "/tasks", `{"project":"p2","body":"stays"}`); got != http.StatusCreated {
		t.Fatalf("create: %d", got)
	}
	if got := do("POST", "/tasks/claim", `{"worker":"w","project":"p1"}`); got != http.StatusOK {
		t.Fatalf("claim: %d", got)
	}
	if got := do("DELETE", "/projects/p1", ""); got != http.StatusConflict {
		t.Fatalf("expected 409 while a task is claimed, got %d", got)
	}
	if _, err := db.rw.Exec("UPDATE tasks SET status = 'pending', worker = NULL, lease_expires = NULL WHERE project = 'p1'"); err != nil {
		t.Fatal(err)
	}
	// a note on a p1 task, a p1 task waiting on another p1 task, and a p2
	// task waiting on a p1 task: the first two go with the project, the
	// third becomes claimable and wakes a worker waiting on p2
	var p1a, p1b, p2 int64
	db.ro.QueryRow("SELECT id FROM tasks WHERE project = 'p1' AND body = 'one'").Scan(&p1a)
	db.ro.QueryRow("SELECT id FROM tasks WHERE project = 'p1' AND body = 'two'").Scan(&p1b)
	db.ro.QueryRow("SELECT id FROM tasks WHERE project = 'p2'").Scan(&p2)
	if got := do("POST", fmt.Sprintf("/tasks/%d/notes", p1a), `{"author":"a","text":"n"}`); got != http.StatusCreated {
		t.Fatalf("note: %d", got)
	}
	if got := do("PATCH", fmt.Sprintf("/tasks/%d", p1b), fmt.Sprintf(`{"after":[%d]}`, p1a)); got != http.StatusNoContent {
		t.Fatalf("dep in p1: %d", got)
	}
	if got := do("PATCH", fmt.Sprintf("/tasks/%d", p2), fmt.Sprintf(`{"after":[%d]}`, p1a)); got != http.StatusNoContent {
		t.Fatalf("dep from p2: %d", got)
	}
	if got := do("POST", "/tasks/claim", `{"worker":"w2","project":"p2"}`); got != http.StatusNoContent {
		t.Fatalf("expected p2 to be blocked before the delete, got %d", got)
	}
	woke := make(chan int, 1)
	go func() { woke <- do("POST", "/tasks/claim", `{"worker":"w2","project":"p2","wait":5}`) }()
	for db.waiterCount() == 0 {
		time.Sleep(5 * time.Millisecond)
	}

	if got := do("DELETE", "/projects/p1", ""); got != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", got)
	}
	select {
	case got := <-woke:
		if got != http.StatusOK {
			t.Fatalf("expected the waiting p2 claim to land, got %d", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("the p2 worker was not woken")
	}
	counts := map[string]string{
		"tasks in p1":       "SELECT COUNT(*) FROM tasks WHERE project = 'p1'",
		"notes":             "SELECT COUNT(*) FROM notes",
		"deps":              "SELECT COUNT(*) FROM task_deps",
		"tasks in p2 minus": "SELECT COUNT(*) - 1 FROM tasks WHERE project = 'p2'",
	}
	for what, q := range counts {
		var n int
		db.ro.QueryRow(q).Scan(&n)
		if n != 0 {
			t.Fatalf("%s: expected 0 left, got %d", what, n)
		}
	}
	if got := do("DELETE", "/projects/p1", ""); got != http.StatusNotFound {
		t.Fatalf("expected 404 after deletion, got %d", got)
	}
}

func TestRenameProject(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 300)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()
	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	do := func(method, path, body string) int {
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	count := func(project string) int {
		var n int
		db.ro.QueryRow("SELECT COUNT(*) FROM tasks WHERE project = ?", project).Scan(&n)
		return n
	}

	do("POST", "/tasks", `{"project":"old","body":"a"}`)
	do("POST", "/tasks", `{"project":"old","body":"b"}`)
	do("POST", "/tasks", `{"project":"other","body":"c"}`)
	cases := []struct {
		path, body string
		want       int
	}{
		{"/projects/nothing", `{"name":"x"}`, http.StatusNotFound},
		{"/projects/nothing", `{"name":"nothing"}`, http.StatusNotFound},
		{"/projects/old", `{"name":"bad name"}`, http.StatusBadRequest},
		{"/projects/old", `{"name":"other"}`, http.StatusConflict},
		{"/projects/old", `{"name":"old"}`, http.StatusNoContent},
		{"/projects/old", `{"name":"new"}`, http.StatusNoContent},
	}
	for _, c := range cases {
		if got := do("PATCH", c.path, c.body); got != c.want {
			t.Fatalf("PATCH %s %s: expected %d, got %d", c.path, c.body, c.want, got)
		}
	}
	if count("old") != 0 || count("new") != 2 || count("other") != 1 {
		t.Fatalf("after rename: old=%d new=%d other=%d", count("old"), count("new"), count("other"))
	}
	if got := do("POST", "/tasks/claim", `{"worker":"w","project":"new"}`); got != http.StatusOK {
		t.Fatalf("expected a claim under the new name to land, got %d", got)
	}
}
