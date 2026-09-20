package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseFlags_Defaults(t *testing.T) {
	t.Setenv("TASKD_URL", "")
	t.Setenv("TASKD_PROJECT", "")
	t.Setenv("TASKD_WORKER", "")
	t.Setenv("TASKD_ASCII", "")

	cfg, err := parseFlags([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.url != "http://localhost:8080" {
		t.Errorf("url: got %q, want http://localhost:8080", cfg.url)
	}
	if cfg.project != "" {
		t.Errorf("project: got %q, want empty", cfg.project)
	}
	if cfg.worker != defaultWorker() {
		t.Errorf("worker: got %q, want %q", cfg.worker, defaultWorker())
	}
	if !cfg.icons {
		t.Errorf("icons: got false, want true")
	}
	if cfg.refresh != time.Second {
		t.Errorf("refresh: got %v, want 1s", cfg.refresh)
	}
}

func TestParseFlags_EnvFallbacks(t *testing.T) {
	t.Setenv("TASKD_URL", "http://custom-host:9090")
	t.Setenv("TASKD_PROJECT", "proj-alpha")
	t.Setenv("TASKD_WORKER", "worker-beta")
	t.Setenv("TASKD_ASCII", "1")

	cfg, err := parseFlags([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.url != "http://custom-host:9090" {
		t.Errorf("url: got %q, want http://custom-host:9090", cfg.url)
	}
	if cfg.project != "proj-alpha" {
		t.Errorf("project: got %q, want proj-alpha", cfg.project)
	}
	if cfg.worker != "worker-beta" {
		t.Errorf("worker: got %q, want worker-beta", cfg.worker)
	}
	if cfg.icons {
		t.Errorf("icons: got true, want false (TASKD_ASCII=1)")
	}

	t.Setenv("TASKD_ASCII", "true")
	cfg2, err := parseFlags([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg2.icons {
		t.Errorf("icons: got true, want false (TASKD_ASCII=true)")
	}
}

func TestParseFlags_AsciiFlipsIcons(t *testing.T) {
	t.Setenv("TASKD_ASCII", "")
	cfg, err := parseFlags([]string{"-ascii"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.icons {
		t.Errorf("expected icons=false when -ascii passed, got %v", cfg.icons)
	}

	t.Setenv("TASKD_ASCII", "1")
	cfg2, err := parseFlags([]string{"-ascii=false"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg2.icons {
		t.Errorf("expected icons=true when -ascii=false passed, got %v", cfg2.icons)
	}
}

func TestParseFlags_RefreshBelow250msErrors(t *testing.T) {
	cases := []string{"249ms", "100ms", "0s", "0ms", "-1s"}
	for _, tc := range cases {
		_, err := parseFlags([]string{"-refresh", tc})
		if err == nil {
			t.Errorf("expected error for refresh=%s, got nil", tc)
		}
	}

	validCases := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, 2 * time.Second}
	for _, d := range validCases {
		cfg, err := parseFlags([]string{"-refresh", d.String()})
		if err != nil {
			t.Errorf("unexpected error for valid refresh=%v: %v", d, err)
		}
		if cfg.refresh != d {
			t.Errorf("refresh: got %v, want %v", cfg.refresh, d)
		}
	}
}

func TestParseFlags_PositionalArgErrors(t *testing.T) {
	_, err := parseFlags([]string{"extra-arg"})
	if err == nil {
		t.Fatal("expected error on positional argument, got nil")
	}
	if !strings.Contains(err.Error(), "extra-arg") {
		t.Errorf("error %q should name the positional argument 'extra-arg'", err.Error())
	}

	_, err = parseFlags([]string{"-project", "test", "first-pos", "second-pos"})
	if err == nil {
		t.Fatal("expected error on positional argument, got nil")
	}
	if !strings.Contains(err.Error(), "first-pos") {
		t.Errorf("error %q should name the first positional argument 'first-pos'", err.Error())
	}
}

func TestParseFlags_BadURL(t *testing.T) {
	badURLs := []string{
		"ftp://localhost:8080",
		"http://",
		"https://",
		"not-a-url",
		"://bad",
		"",
		"   ",
		"localhost:8080",
	}
	for _, u := range badURLs {
		_, err := parseFlags([]string{"-url", u})
		if err == nil {
			t.Errorf("expected error for bad url %q, got nil", u)
		}
	}

	cfg, err := parseFlags([]string{"-url", "http://localhost:8080/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.url != "http://localhost:8080" {
		t.Errorf("url: got %q, want http://localhost:8080 (trailing slash trimmed)", cfg.url)
	}

	cfg, err = parseFlags([]string{"-url", "https://example.com/api///"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.url != "https://example.com/api" {
		t.Errorf("url: got %q, want https://example.com/api", cfg.url)
	}
}

func TestParseFlags_Help(t *testing.T) {
	for _, hflag := range []string{"-h", "--help", "-help"} {
		_, err := parseFlags([]string{hflag})
		if !errors.Is(err, flag.ErrHelp) {
			t.Errorf("for %s: got %v, want flag.ErrHelp", hflag, err)
		}
	}
}

func TestPrintUsage(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	output := buf.String()

	if !strings.Contains(output, "0-4") {
		t.Error("usage output missing '0-4'")
	}
	if !strings.Contains(output, "TASKD_URL") {
		t.Error("usage output missing 'TASKD_URL'")
	}
	if !strings.Contains(output, "ctrl-s") {
		t.Error("usage output missing 'ctrl-s'")
	}

	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if len(line) > 79 {
			t.Errorf("line %d exceeds 79 columns (%d chars): %q", i+1, len(line), line)
		}
	}
}

func TestDefaultWorker_Env(t *testing.T) {
	t.Setenv("TASKD_WORKER", "custom-worker:verbatim")
	if got := defaultWorker(); got != "custom-worker:verbatim" {
		t.Fatalf("got %q, want custom-worker:verbatim", got)
	}
}

func TestDefaultWorker_WorktreeGitdir(t *testing.T) {
	t.Setenv("TASKD_WORKER", "")
	tmp := t.TempDir()
	gitFile := filepath.Join(tmp, ".git")
	content := "gitdir: /path/to/myrepo/.git/worktrees/my-feature\n"
	if err := os.WriteFile(gitFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(tmp)

	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "tui"
	}

	got := defaultWorker()
	want := host + ":myrepo"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
