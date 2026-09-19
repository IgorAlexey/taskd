package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerScriptCLI(t *testing.T) {
	workerPath, err := filepath.Abs("worker")
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}
	if _, err := os.Stat(workerPath); err != nil {
		t.Fatalf("worker script not found at %s: %v", workerPath, err)
	}

	cmd := exec.Command("/bin/sh", workerPath, "--invalid-flag")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected --invalid-flag to fail, got success")
	}
	if !strings.Contains(string(out), "Error: unknown option") {
		t.Fatalf("expected error message to contain 'Error: unknown option', got: %s", string(out))
	}

	cmd = exec.Command("/bin/sh", workerPath, "--project")
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected --project without argument to fail")
	}
	if !strings.Contains(string(out), "requires an argument") {
		t.Fatalf("expected error message to contain 'requires an argument', got: %s", string(out))
	}

	cmd = exec.Command("/bin/sh", workerPath, "--slot")
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected --slot without argument to fail")
	}
	if !strings.Contains(string(out), "requires an argument") {
		t.Fatalf("expected error message to contain 'requires an argument', got: %s", string(out))
	}

	cmd = exec.Command("/bin/sh", workerPath, "--help")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected --help to exit 0, got error %v: %s", err, string(out))
	}
	if !strings.Contains(string(out), "Usage: worker") {
		t.Fatalf("expected help output to contain 'Usage: worker', got: %s", string(out))
	}
	if !strings.Contains(string(out), "Examples:") {
		t.Fatalf("expected help output to contain 'Examples:', got: %s", string(out))
	}
	if !strings.Contains(string(out), "/fulfill") {
		t.Fatalf("expected help output to contain '/fulfill', got: %s", string(out))
	}

	cmd = exec.Command("/bin/sh", workerPath)
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected worker with no arguments to fail")
	}
	if !strings.Contains(string(out), "prompt or command is required") {
		t.Fatalf("expected prompt required message, got: %s", string(out))
	}

	cmd = exec.Command("/bin/sh", workerPath, "fix", "the", "build", "failure")
	cmd.Env = append(os.Environ(), "TASKD_WORKER_ACTIVE=1")
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected nested worker invocation to exit non-zero")
	}
	if !strings.Contains(string(out), "refusing to nest") {
		t.Fatalf("expected nesting guard message, got: %s", string(out))
	}

	wtbase := filepath.Join("/tmp", "taskd-"+os.Getenv("USER"))
	if err := os.MkdirAll(wtbase, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", wtbase, err)
	}
	fake, err := os.MkdirTemp(wtbase, "wt-test-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer os.RemoveAll(fake)
	if out, err := exec.Command("git", "-C", fake, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	cmd = exec.Command("/bin/sh", workerPath, "fix", "the", "build", "failure")
	cmd.Dir = fake
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "USER=" + os.Getenv("USER")}
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected worker started inside a worker worktree to exit non-zero")
	}
	if !strings.Contains(string(out), "refusing to start inside a worker worktree") {
		t.Fatalf("expected worktree guard message, got: %s", string(out))
	}

	outside, err := os.MkdirTemp("", "outside-test-")
	if err != nil {
		t.Fatalf("mkdtemp outside: %v", err)
	}
	defer os.RemoveAll(outside)
	if out, err := exec.Command("git", "-C", outside, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}

	cmd = exec.Command("/bin/sh", workerPath, "test prompt")
	cmd.Dir = outside
	cmd.Env = []string{"PATH=/usr/bin:/bin", "USER=" + os.Getenv("USER")}
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected missing omp to exit non-zero")
	}
	if !strings.Contains(string(out), "omp is not installed") {
		t.Fatalf("expected missing omp error message, got: %s", string(out))
	}

	cmd = exec.Command("/bin/sh", workerPath, "test prompt")
	cmd.Dir = outside
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "USER=" + os.Getenv("USER"), "TASKD_URL=http://127.0.0.1:1"}
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected unreachable taskd to exit non-zero")
	}
	if !strings.Contains(string(out), "taskd is not reachable") {
		t.Fatalf("expected unreachable taskd error message, got: %s", string(out))
	}
}

func TestWorkerBranchDetection(t *testing.T) {
	workerPath, err := filepath.Abs("worker")
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}

	masterRepo, err := os.MkdirTemp("", "outside-master-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer os.RemoveAll(masterRepo)

	if out, err := exec.Command("git", "-C", masterRepo, "init", "-b", "master", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	_ = exec.Command("git", "-C", masterRepo, "checkout", "--detach", "-q").Run()

	cmd := exec.Command(workerPath, "--detect-branch")
	cmd.Dir = masterRepo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("detect_default_branch failed: %v: %s", err, out)
	}
	branch := strings.TrimSpace(string(out))
	if branch != "master" {
		t.Fatalf("expected detected branch to be 'master', got %q", branch)
	}
}
