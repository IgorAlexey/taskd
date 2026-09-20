package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestFlagAliases(t *testing.T) {
	t.Run("short flags parse and set config", func(t *testing.T) {
		cfg, err := parseFlags([]string{"-p", "demo", "-w", "myworker", "-u", "http://127.0.0.1:8080"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.project != "demo" {
			t.Fatalf("cfg.project = %q, want demo", cfg.project)
		}
		if cfg.worker != "myworker" {
			t.Fatalf("cfg.worker = %q, want myworker", cfg.worker)
		}
		if cfg.url != "http://127.0.0.1:8080" {
			t.Fatalf("cfg.url = %q, want http://127.0.0.1:8080", cfg.url)
		}
	})

	t.Run("short flags with equals", func(t *testing.T) {
		cfg, err := parseFlags([]string{"-p=demo2", "-w=myworker2", "-u=http://127.0.0.1:9090"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.project != "demo2" {
			t.Fatalf("cfg.project = %q, want demo2", cfg.project)
		}
		if cfg.worker != "myworker2" {
			t.Fatalf("cfg.worker = %q, want myworker2", cfg.worker)
		}
		if cfg.url != "http://127.0.0.1:9090" {
			t.Fatalf("cfg.url = %q, want http://127.0.0.1:9090", cfg.url)
		}
	})

	t.Run("missing short flag argument", func(t *testing.T) {
		for _, arg := range []string{"-p", "-w", "-u"} {
			_, err := parseFlags([]string{arg})
			if err == nil {
				t.Fatalf("expected error for missing argument to %s, got nil", arg)
			}
		}
	})

	t.Run("documented in help output", func(t *testing.T) {
		var buf bytes.Buffer
		printUsage(&buf)
		out := buf.String()
		for _, want := range []string{
			"-p, -project",
			"-w, -worker",
			"-u, -url",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("help output missing %q:\n%s", want, out)
			}
		}
	})
}

func TestTUIFlags(t *testing.T) {
	t.Run("all aliases combined", func(t *testing.T) {
		cfg, err := parseFlags([]string{
			"-p", "proj1",
			"-w", "work1",
			"-u", "http://127.0.0.1:8888",
			"-q", "search-term",
			"-s", "id",
			"-status", "pending",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.project != "proj1" {
			t.Fatalf("cfg.project = %q, want proj1", cfg.project)
		}
		if cfg.worker != "work1" {
			t.Fatalf("cfg.worker = %q, want work1", cfg.worker)
		}
		if cfg.url != "http://127.0.0.1:8888" {
			t.Fatalf("cfg.url = %q, want http://127.0.0.1:8888", cfg.url)
		}
		if cfg.query != "search-term" {
			t.Fatalf("cfg.query = %q, want search-term", cfg.query)
		}
		if cfg.sortCol != sortID {
			t.Fatalf("cfg.sortCol = %v, want sortID", cfg.sortCol)
		}
		if cfg.status != "pending" {
			t.Fatalf("cfg.status = %q, want pending", cfg.status)
		}
	})
}
