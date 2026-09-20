package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProjectFlagWildcard(t *testing.T) {
	t.Run("flag wildcard parses cleanly", func(t *testing.T) {
		cfg, err := parseFlags([]string{"-project", "*"})
		if err != nil {
			t.Fatalf("unexpected error for -project '*': %v", err)
		}
		m := newModel(cfg, nil)
		if m.project != "" {
			t.Fatalf("expected m.project to be empty (all projects), got %q", m.project)
		}
	})

	t.Run("env wildcard parses cleanly", func(t *testing.T) {
		t.Setenv("TASKD_PROJECT", "*")
		cfg, err := parseFlags(nil)
		if err != nil {
			t.Fatalf("unexpected error for TASKD_PROJECT='*': %v", err)
		}
		m := newModel(cfg, nil)
		if m.project != "" {
			t.Fatalf("expected m.project to be empty (all projects), got %q", m.project)
		}
	})

	t.Run("flag wildcard overrides invalid env", func(t *testing.T) {
		t.Setenv("TASKD_PROJECT", "invalid project!")
		cfg, err := parseFlags([]string{"-project", "*"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := newModel(cfg, nil)
		if m.project != "" {
			t.Fatalf("expected m.project to be empty, got %q", m.project)
		}
	})

	t.Run("partial wildcards still rejected", func(t *testing.T) {
		for _, bad := range []string{"bad*name", "*proj", "proj*", "**"} {
			_, err := parseFlags([]string{"-project", bad})
			if err == nil {
				t.Fatalf("expected error for project %q, got nil", bad)
			}
		}
	})

	t.Run("queries daemon across all projects", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/tasks":
				tasks := []task{
					{ID: 1, Project: "alpha", Status: "pending", Body: "task one"},
					{ID: 2, Project: "beta", Status: "pending", Body: "task two"},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tasks)
			case "/stats":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(stats{Pending: 2, Total: 2})
			case "/projects":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode([]string{"alpha", "beta"})
			case "/workers":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode([]string{})
			default:
				http.NotFound(w, r)
			}
		}))
		defer ts.Close()

		cfg, err := parseFlags([]string{"-url", ts.URL, "-project", "*"})
		if err != nil {
			t.Fatalf("parseFlags failed: %v", err)
		}

		c := newClient(cfg.url)
		m := newModel(cfg, c)
		res, err := c.list(m.listScope(), "")
		if err != nil {
			t.Fatalf("c.list failed: %v", err)
		}
		m.tasks = res.tasks
		m.rebuildShown()
		if len(m.shown) != 2 {
			t.Fatalf("shown tasks = %d, want 2", len(m.shown))
		}
	})
}
