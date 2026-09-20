package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFlagsPriority(t *testing.T) {
	t.Setenv("TASKD_PRIORITY", "")

	t.Run("flag accepts integer", func(t *testing.T) {
		cfg, err := parseFlags([]string{"-priority", "1"})
		if err != nil {
			t.Fatalf("unexpected error parsing -priority 1: %v", err)
		}
		if !cfg.hasPriority || cfg.priority != 1 {
			t.Fatalf("got priority %d (has=%v), want 1", cfg.priority, cfg.hasPriority)
		}

		cfgEq, err := parseFlags([]string{"-priority=2"})
		if err != nil {
			t.Fatalf("unexpected error parsing -priority=2: %v", err)
		}
		if !cfgEq.hasPriority || cfgEq.priority != 2 {
			t.Fatalf("got priority %d (has=%v), want 2", cfgEq.priority, cfgEq.hasPriority)
		}
	})

	t.Run("flag rejects invalid integer", func(t *testing.T) {
		_, err := parseFlags([]string{"-priority", "not-a-number"})
		if err == nil {
			t.Fatal("expected error parsing non-integer priority, got nil")
		}
	})

	t.Run("environment variable", func(t *testing.T) {
		t.Setenv("TASKD_PRIORITY", "5")
		cfg, err := parseFlags(nil)
		if err != nil {
			t.Fatalf("unexpected error parsing TASKD_PRIORITY=5: %v", err)
		}
		if !cfg.hasPriority || cfg.priority != 5 {
			t.Fatalf("got priority %d (has=%v), want 5", cfg.priority, cfg.hasPriority)
		}

		cfgOverride, err := parseFlags([]string{"-priority", "3"})
		if err != nil {
			t.Fatalf("unexpected error parsing flag override: %v", err)
		}
		if !cfgOverride.hasPriority || cfgOverride.priority != 3 {
			t.Fatalf("got priority %d (has=%v), want 3", cfgOverride.priority, cfgOverride.hasPriority)
		}
	})

	t.Run("environment variable rejects invalid integer", func(t *testing.T) {
		t.Setenv("TASKD_PRIORITY", "invalid")
		_, err := parseFlags(nil)
		if err == nil {
			t.Fatal("expected error parsing invalid TASKD_PRIORITY, got nil")
		}
	})

	t.Run("usage documents flag and env", func(t *testing.T) {
		var buf bytes.Buffer
		printUsage(&buf)
		usage := buf.String()

		if !strings.Contains(usage, "-priority <int>") {
			t.Fatalf("usage missing '-priority <int>':\n%s", usage)
		}
		idxEnv := strings.Index(usage, "Environment variables:")
		if idxEnv == -1 {
			t.Fatalf("usage missing Environment variables section:\n%s", usage)
		}
		if !strings.Contains(usage[idxEnv:], "TASKD_PRIORITY") {
			t.Fatalf("usage missing TASKD_PRIORITY under Environment variables:\n%s", usage)
		}
	})

	t.Run("listFilter comparable by value", func(t *testing.T) {
		f1 := listFilter{project: "p", worker: "w", status: "s", query: "q", priority: 1, hasPriority: true}
		f2 := listFilter{project: "p", worker: "w", status: "s", query: "q", priority: 1, hasPriority: true}
		if f1 != f2 {
			t.Fatal("expected identical listFilter values to compare equal by value")
		}
		f3 := listFilter{project: "p", worker: "w", status: "s", query: "q", priority: 2, hasPriority: true}
		if f1 == f3 {
			t.Fatal("expected different priority listFilter values to not compare equal")
		}
	})

	t.Run("client filter dispatches query to daemon", func(t *testing.T) {
		var receivedPriority string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/tasks" {
				receivedPriority = r.URL.Query().Get("priority")
				_, _ = w.Write([]byte(`[]`))
				return
			}
			if r.URL.Path == "/stats" {
				_, _ = w.Write([]byte(`{}`))
				return
			}
			if r.URL.Path == "/projects" || r.URL.Path == "/workers" {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		cfg, err := parseFlags([]string{"-url", ts.URL, "-priority", "1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		c := newClient(ts.URL)
		m := newModel(cfg, c)

		lf := m.listFilter()
		if !lf.hasPriority || lf.priority != 1 {
			t.Fatalf("listFilter priority = %d (has=%v), want 1", lf.priority, lf.hasPriority)
		}

		cmd := m.startPoll()
		if cmd == nil {
			t.Fatal("expected startPoll cmd, got nil")
		}
		res := cmd()
		poll, ok := res.(pollMsg)
		if !ok || poll.err != nil {
			t.Fatalf("poll failed: %+v", res)
		}

		if receivedPriority != "1" {
			t.Fatalf("received query priority = %q, want '1'", receivedPriority)
		}
	})
}
