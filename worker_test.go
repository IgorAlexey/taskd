package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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
func TestWorkerStatus(t *testing.T) {
	workerPath, err := filepath.Abs("worker")
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}

	rundir := t.TempDir()
	username := "teststatus"
	lockdir := filepath.Join(rundir, "taskd-"+username)
	if err := os.MkdirAll(lockdir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", lockdir, err)
	}

	lock1Path := filepath.Join(lockdir, "projA-1.lock")
	f1, err := os.OpenFile(lock1Path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("create lock1: %v", err)
	}
	defer f1.Close()

	if err := syscall.Flock(int(f1.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("flock f1: %v", err)
	}

	lock2Path := filepath.Join(lockdir, "projA-2.lock")
	if err := os.WriteFile(lock2Path, nil, 0o644); err != nil {
		t.Fatalf("write lock2: %v", err)
	}

	lock3Path := filepath.Join(lockdir, "projB-1.lock")
	if err := os.WriteFile(lock3Path, nil, 0o644); err != nil {
		t.Fatalf("write lock3: %v", err)
	}

	log1Path := filepath.Join(lockdir, "worker-projA-1.log")
	if err := os.WriteFile(log1Path, []byte("compiling assets\n"), 0o644); err != nil {
		t.Fatalf("write log1: %v", err)
	}

	log2Path := filepath.Join(lockdir, "worker-projA-2.log")
	if err := os.WriteFile(log2Path, []byte("idle backoff\n"), 0o644); err != nil {
		t.Fatalf("write log2: %v", err)
	}

	cmd := exec.Command("/bin/sh", workerPath, "status")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"USER=" + username,
		"XDG_RUNTIME_DIR=" + rundir,
		"TASKD_WORKER_ACTIVE=1",
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("worker status failed: %v: %s", err, string(out))
	}
	sOut := string(out)
	if !strings.Contains(sOut, "SLOT") || !strings.Contains(sOut, "PROJECT") || !strings.Contains(sOut, "STATUS") || !strings.Contains(sOut, "ACTIVITY") {
		t.Fatalf("expected status table headers, got: %s", sOut)
	}
	if !strings.Contains(sOut, "projA") || !strings.Contains(sOut, "projB") {
		t.Fatalf("expected projects projA and projB, got: %s", sOut)
	}
	if !strings.Contains(sOut, "active") || !strings.Contains(sOut, "idle") {
		t.Fatalf("expected active and idle statuses, got: %s", sOut)
	}
	if !strings.Contains(sOut, "compiling assets") {
		t.Fatalf("expected recent activity summary, got: %s", sOut)
	}

	cmd = exec.Command("/bin/sh", workerPath, "status", "-p", "projA")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"USER=" + username,
		"XDG_RUNTIME_DIR=" + rundir,
	}
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("worker status -p projA failed: %v: %s", err, string(out))
	}
	sOut = string(out)
	if !strings.Contains(sOut, "projA") {
		t.Fatalf("expected filtered output to contain projA, got: %s", sOut)
	}
	if strings.Contains(sOut, "projB") {
		t.Fatalf("expected filtered output to exclude projB, got: %s", sOut)
	}
}

func TestWorkerPreserveUnpushedCommits(t *testing.T) {
	workerPath, err := filepath.Abs("worker")
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}

	originRepo := t.TempDir()
	cmd := exec.Command("git", "-C", originRepo, "init", "-b", "main", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init origin: %v: %s", err, out)
	}
	cmd = exec.Command("git", "-C", originRepo, "-c", "core.hookspath=", "-c", "sendpatch.enabled=false", "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "chore: initial", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit origin: %v: %s", err, out)
	}

	wtRepo := t.TempDir()
	cmd = exec.Command("git", "clone", originRepo, wtRepo, "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v: %s", err, out)
	}
	cmd = exec.Command("git", "-C", wtRepo, "checkout", "--detach", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout detach: %v: %s", err, out)
	}

	cmd = exec.Command("/bin/sh", workerPath, "--preserve-unpushed", "main", "test/proj name", "1")
	cmd.Dir = wtRepo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("preserve-unpushed with no commits failed: %v: %s", err, out)
	}

	branchesOut, err := exec.Command("git", "-C", wtRepo, "branch", "--list", "rescue-*").CombinedOutput()
	if err != nil {
		t.Fatalf("git branch list: %v: %s", err, branchesOut)
	}
	if strings.TrimSpace(string(branchesOut)) != "" {
		t.Fatalf("expected no rescue branch when 0 unpushed commits, got: %s", string(branchesOut))
	}

	cmd = exec.Command("git", "-C", wtRepo, "-c", "core.hookspath=", "-c", "sendpatch.enabled=false", "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "feat: local work", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit local: %v: %s", err, out)
	}

	headShaBytes, err := exec.Command("git", "-C", wtRepo, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v: %s", err, headShaBytes)
	}
	headSha := strings.TrimSpace(string(headShaBytes))

	cmd = exec.Command("/bin/sh", workerPath, "--preserve-unpushed", "main", "test/proj name", "1")
	cmd.Dir = wtRepo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("preserve-unpushed failed: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "rescue-test_proj_name-1-") {
		t.Fatalf("expected warning with sanitized rescue branch name, got: %s", string(out))
	}

	branchesOut, err = exec.Command("git", "-C", wtRepo, "branch", "--list", "rescue-test_proj_name-1-*").CombinedOutput()
	if err != nil {
		t.Fatalf("git branch list: %v: %s", err, branchesOut)
	}
	rescueBranch := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(branchesOut)), "* "))
	if rescueBranch == "" {
		t.Fatalf("expected rescue branch to exist, got: %s", string(branchesOut))
	}

	rescueShaBytes, err := exec.Command("git", "-C", wtRepo, "rev-parse", rescueBranch).CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse rescue branch: %v: %s", err, rescueShaBytes)
	}
	rescueSha := strings.TrimSpace(string(rescueShaBytes))
	if rescueSha != headSha {
		t.Fatalf("expected rescue branch SHA %s to match HEAD %s", rescueSha, headSha)
	}

	cmd = exec.Command("/bin/sh", workerPath, "--preserve-unpushed", "nonexistent-branch-xyz", "testproj", "1")
	cmd.Dir = wtRepo
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected preserve-unpushed with invalid branch to fail, got success")
	}
	if !strings.Contains(string(out), "failed to inspect rev-list") {
		t.Fatalf("expected rev-list failure message, got: %s", string(out))
	}
}

func TestWorkerExportTaskdWorker(t *testing.T) {
	workerPath, err := filepath.Abs("worker")
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tasks" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("[]"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	binDir := t.TempDir()
	ompScript := filepath.Join(binDir, "omp")
	scriptContent := "#!/bin/sh\necho \"CHILD_TASKD_WORKER=$TASKD_WORKER\"\nexit 130\n"
	if err := os.WriteFile(ompScript, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("write omp script: %v", err)
	}

	repoDir := t.TempDir()
	if out, err := exec.Command("git", "-C", repoDir, "init", "-b", "main", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repoDir, "config", "user.name", "test").CombinedOutput(); err != nil {
		t.Fatalf("git config user.name: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repoDir, "config", "user.email", "test@test.com").CombinedOutput(); err != nil {
		t.Fatalf("git config user.email: %v: %s", err, out)
	}
	dummyFile := filepath.Join(repoDir, "file.txt")
	if err := os.WriteFile(dummyFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write dummy file: %v", err)
	}
	if out, err := exec.Command("git", "-C", repoDir, "add", "file.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repoDir, "commit", "-m", "test: initial commit", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repoDir, "remote", "add", "origin", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v: %s", err, out)
	}

	wtBase := t.TempDir()
	wtPath := filepath.Join(wtBase, "wt-test-export-5")
	runDir := t.TempDir()

	cmd := exec.Command("/bin/sh", workerPath, "--run-locked", "5", wtPath, "customproj", "main", "0", "test prompt")
	cmd.Dir = repoDir
	cmd.Env = []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"USER=testuser",
		"XDG_RUNTIME_DIR=" + runDir,
		"TASKD_WT_BASE=" + wtBase,
		"TASKD_URL=" + srv.URL,
	}
	out, _ := cmd.CombinedOutput()
	outStr := string(out)

	if !strings.Contains(outStr, "CHILD_TASKD_WORKER=customproj-5") {
		t.Fatalf("expected child process to have TASKD_WORKER=customproj-5, got: %s", outStr)
	}
	if !strings.Contains(outStr, "Worker customproj-5 running in") {
		t.Fatalf("expected startup log to contain 'Worker customproj-5 running in', got: %s", outStr)
	}
	if !strings.Contains(outStr, "slot 5") {
		t.Fatalf("expected startup log to contain 'slot 5', got: %s", outStr)
	}
}
