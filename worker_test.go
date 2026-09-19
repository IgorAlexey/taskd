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
	"time"
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
	cmd = exec.Command("/bin/sh", workerPath, "--project=")
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected --project= without argument to fail")
	}
	if !strings.Contains(string(out), "requires an argument") {
		t.Fatalf("expected error message to contain 'requires an argument', got: %s", string(out))
	}

	cmd = exec.Command("/bin/sh", workerPath, "--slot=")
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected --slot= without argument to fail")
	}
	if !strings.Contains(string(out), "requires an argument") {
		t.Fatalf("expected error message to contain 'requires an argument', got: %s", string(out))
	}
	cmd = exec.Command("/bin/sh", workerPath, "--project=test-proj", "-h")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected --project=test-proj -h to succeed, got error %v: %s", err, string(out))
	}
	if !strings.Contains(string(out), "Usage: worker") {
		t.Fatalf("expected help output to contain 'Usage: worker', got: %s", string(out))
	}

	cmd = exec.Command("/bin/sh", workerPath, "--slot=1", "-h")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected --slot=1 -h to succeed, got error %v: %s", err, string(out))
	}
	if !strings.Contains(string(out), "Usage: worker") {
		t.Fatalf("expected help output to contain 'Usage: worker', got: %s", string(out))
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
	if !strings.Contains(string(out), "--once") {
		t.Fatalf("expected help output to contain '--once', got: %s", string(out))
	}
	if !strings.Contains(string(out), "-1") {
		t.Fatalf("expected help output to contain '-1', got: %s", string(out))
	}

	for _, flag := range []string{"-h", "--help", "help"} {
		cmd = exec.Command("/bin/sh", workerPath, flag)
		cmd.Env = append(os.Environ(), "TASKD_WORKER_ACTIVE=1")
		out, err = cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("expected %s with TASKD_WORKER_ACTIVE=1 to exit 0, got error %v: %s", flag, err, string(out))
		}
		if !strings.Contains(string(out), "Usage: worker") {
			t.Fatalf("expected help output with TASKD_WORKER_ACTIVE=1 to contain 'Usage: worker', got: %s", string(out))
		}
	}

	cmd = exec.Command("/bin/sh", workerPath)
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected worker with no arguments to fail")
	}
	if !strings.Contains(string(out), "prompt or command is required") {
		t.Fatalf("expected prompt required message, got: %s", string(out))
	}

	cmd = exec.Command("/bin/sh", workerPath, "--once")
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected worker --once with no arguments to fail")
	}
	if !strings.Contains(string(out), "prompt or command is required") {
		t.Fatalf("expected prompt required message, got: %s", string(out))
	}

	cmd = exec.Command("/bin/sh", workerPath, "-1")
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected worker -1 with no arguments to fail")
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
	cmd = exec.Command("/bin/sh", workerPath, "status", "--project=projA")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"USER=" + username,
		"XDG_RUNTIME_DIR=" + rundir,
	}
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("worker status --project=projA failed: %v: %s", err, string(out))
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
func TestWorkerPreserveExecutionLogsAcrossIterations(t *testing.T) {
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
	counterFile := filepath.Join(binDir, "iteration.txt")
	_ = os.WriteFile(counterFile, []byte("0"), 0o644)

	ompScript := filepath.Join(binDir, "omp")
	scriptContent := `#!/bin/sh
cnt=$(cat "` + counterFile + `" 2>/dev/null || echo 0)
cnt=$((cnt + 1))
echo "$cnt" > "` + counterFile + `"
if [ "$cnt" -eq 1 ]; then
    echo "ITERATION_1_FAILURE_DIAGNOSTICS"
    exit 1
else
    echo "ITERATION_2_SUCCESS_OUTPUT"
    exit 130
fi
`
	if err := os.WriteFile(ompScript, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("write omp script: %v", err)
	}

	sleepScript := filepath.Join(binDir, "sleep")
	if err := os.WriteFile(sleepScript, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write sleep script: %v", err)
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
	wtPath := filepath.Join(wtBase, "wt-test-log-7")
	runDir := t.TempDir()

	cmd := exec.Command("/bin/sh", workerPath, "--run-locked", "7", wtPath, "logproj", "main", "0", "test prompt")
	cmd.Dir = repoDir
	cmd.Env = []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"USER=testuser",
		"XDG_RUNTIME_DIR=" + runDir,
		"TASKD_WT_BASE=" + wtBase,
		"TASKD_URL=" + srv.URL,
	}
	_, _ = cmd.CombinedOutput()

	lockDir := filepath.Join(runDir, "taskd-testuser")
	prevLog := filepath.Join(lockDir, "worker-logproj-7.prev.log")
	curLog := filepath.Join(lockDir, "worker-logproj-7.log")

	prevBytes, err := os.ReadFile(prevLog)
	if err != nil {
		t.Fatalf("expected prev.log to exist at %s: %v", prevLog, err)
	}
	if !strings.Contains(string(prevBytes), "ITERATION_1_FAILURE_DIAGNOSTICS") {
		t.Fatalf("expected prev.log to contain iteration 1 failure trace, got: %s", string(prevBytes))
	}

	curBytes, err := os.ReadFile(curLog)
	if err != nil {
		t.Fatalf("expected runlog to exist at %s: %v", curLog, err)
	}
	if !strings.Contains(string(curBytes), "ITERATION_2_SUCCESS_OUTPUT") {
		t.Fatalf("expected runlog to contain iteration 2 output, got: %s", string(curBytes))
	}
}
func TestWorkerHonorTaskdProjectEnv(t *testing.T) {
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
	scriptContent := "#!/bin/sh\necho \"CHILD_TASKD_PROJECT=$TASKD_PROJECT\"\nexit 130\n"
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
	runDir := t.TempDir()

	// 1. When TASKD_PROJECT is set in environment, worker defaults to that project.
	cmd := exec.Command("/bin/sh", workerPath, "--slot", "1", "test prompt")
	cmd.Dir = repoDir
	cmd.Env = []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"USER=testuser",
		"XDG_RUNTIME_DIR=" + runDir,
		"TASKD_WT_BASE=" + wtBase,
		"TASKD_URL=" + srv.URL,
		"TASKD_PROJECT=envproj",
	}
	out, _ := cmd.CombinedOutput()
	outStr := string(out)
	if !strings.Contains(outStr, "CHILD_TASKD_PROJECT=envproj") {
		t.Fatalf("expected child to receive TASKD_PROJECT=envproj, got: %s", outStr)
	}
	if !strings.Contains(outStr, "project envproj") {
		t.Fatalf("expected startup message to contain 'project envproj', got: %s", outStr)
	}

	// 2. When -p flag is explicitly passed, it overrides TASKD_PROJECT.
	cmd = exec.Command("/bin/sh", workerPath, "--slot", "1", "-p", "flagoverride", "test prompt")
	cmd.Dir = repoDir
	cmd.Env = []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"USER=testuser",
		"XDG_RUNTIME_DIR=" + runDir,
		"TASKD_WT_BASE=" + wtBase,
		"TASKD_URL=" + srv.URL,
		"TASKD_PROJECT=envproj",
	}
	out, _ = cmd.CombinedOutput()
	outStr = string(out)
	if !strings.Contains(outStr, "CHILD_TASKD_PROJECT=flagoverride") {
		t.Fatalf("expected -p flag to override TASKD_PROJECT, got: %s", outStr)
	}
	if !strings.Contains(outStr, "project flagoverride") {
		t.Fatalf("expected startup message to contain 'project flagoverride', got: %s", outStr)
	}
	cmd = exec.Command("/bin/sh", workerPath, "--slot=2", "--project=flagoverride", "test prompt")
	cmd.Dir = repoDir
	cmd.Env = []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"USER=testuser",
		"XDG_RUNTIME_DIR=" + runDir,
		"TASKD_WT_BASE=" + wtBase,
		"TASKD_URL=" + srv.URL,
		"TASKD_PROJECT=envproj",
	}
	out, _ = cmd.CombinedOutput()
	outStr = string(out)
	if !strings.Contains(outStr, "CHILD_TASKD_PROJECT=flagoverride") {
		t.Fatalf("expected --project=flagoverride to override TASKD_PROJECT, got: %s", outStr)
	}
	if !strings.Contains(outStr, "project flagoverride") {
		t.Fatalf("expected startup message to contain 'project flagoverride', got: %s", outStr)
	}
	if !strings.Contains(outStr, "slot 2") {
		t.Fatalf("expected startup message to contain 'slot 2', got: %s", outStr)
	}

	// 3. When TASKD_PROJECT is unset, worker falls back to repo root basename.
	cmd = exec.Command("/bin/sh", workerPath, "--slot", "1", "test prompt")
	cmd.Dir = repoDir
	cmd.Env = []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"USER=testuser",
		"XDG_RUNTIME_DIR=" + runDir,
		"TASKD_WT_BASE=" + wtBase,
		"TASKD_URL=" + srv.URL,
	}
	out, _ = cmd.CombinedOutput()
	outStr = string(out)
	expectedDefault := filepath.Base(repoDir)
	if !strings.Contains(outStr, "CHILD_TASKD_PROJECT="+expectedDefault) {
		t.Fatalf("expected fallback to repo root basename %s, got: %s", expectedDefault, outStr)
	}
	if !strings.Contains(outStr, "project "+expectedDefault) {
		t.Fatalf("expected startup message to contain 'project "+expectedDefault+"', got: %s", outStr)
	}
}
func TestWorkerOnceExecution(t *testing.T) {
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
	counterFile := filepath.Join(binDir, "iterations.txt")
	_ = os.WriteFile(counterFile, []byte("0"), 0o644)

	ompScript := filepath.Join(binDir, "omp")
	scriptContent := "#!/bin/sh\n" +
		"cnt=$(cat \"" + counterFile + "\" 2>/dev/null || echo 0)\n" +
		"cnt=$((cnt + 1))\n" +
		"echo \"$cnt\" > \"" + counterFile + "\"\n" +
		"exit 42\n"
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
	if out, err := exec.Command("git", "-C", repoDir, "-c", "core.hookspath=", "-c", "sendpatch.enabled=false", "commit", "-m", "chore: initial commit", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repoDir, "remote", "add", "origin", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v: %s", err, out)
	}

	wtBase := t.TempDir()
	wtPath := filepath.Join(wtBase, "wt-test-once-1")
	runDir := t.TempDir()

	cmd := exec.Command("/bin/sh", workerPath, "--run-locked", "1", wtPath, "onceproj", "main", "0", "test prompt", "1")
	cmd.Dir = repoDir
	cmd.Env = []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"USER=testuser",
		"XDG_RUNTIME_DIR=" + runDir,
		"TASKD_WT_BASE=" + wtBase,
		"TASKD_URL=" + srv.URL,
	}
	out, err := cmd.CombinedOutput()
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected command to exit with non-zero exit error, got %v: %s", err, string(out))
	}
	if exitErr.ExitCode() != 42 {
		t.Fatalf("expected exit code 42, got %d: %s", exitErr.ExitCode(), string(out))
	}

	cntBytes, err := os.ReadFile(counterFile)
	if err != nil {
		t.Fatalf("read counter file: %v", err)
	}
	if strings.TrimSpace(string(cntBytes)) != "1" {
		t.Fatalf("expected exactly 1 iteration, got %s", strings.TrimSpace(string(cntBytes)))
	}

	cmd = exec.Command("/bin/sh", workerPath, "test prompt", "--once", "--slot", "2")
	cmd.Dir = repoDir
	cmd.Env = []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"USER=testuser",
		"XDG_RUNTIME_DIR=" + runDir,
		"TASKD_WT_BASE=" + wtBase,
		"TASKD_URL=" + srv.URL,
	}
	out, err = cmd.CombinedOutput()
	exitErr, ok = err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected worker --once to exit non-zero, got %v: %s", err, string(out))
	}
	if exitErr.ExitCode() != 42 {
		t.Fatalf("expected exit code 42 for --once, got %d: %s", exitErr.ExitCode(), string(out))
	}

	cmd = exec.Command("/bin/sh", workerPath, "-1", "test prompt", "--slot", "3")
	cmd.Dir = repoDir
	cmd.Env = []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"USER=testuser",
		"XDG_RUNTIME_DIR=" + runDir,
		"TASKD_WT_BASE=" + wtBase,
		"TASKD_URL=" + srv.URL,
	}
	out, err = cmd.CombinedOutput()
	exitErr, ok = err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected worker -1 to exit non-zero, got %v: %s", err, string(out))
	}
	if exitErr.ExitCode() != 42 {
		t.Fatalf("expected exit code 42 for -1, got %d: %s", exitErr.ExitCode(), string(out))
	}
	cntBytes, err = os.ReadFile(counterFile)
	if err != nil {
		t.Fatalf("read counter file: %v", err)
	}
	if strings.TrimSpace(string(cntBytes)) != "3" {
		t.Fatalf("expected exactly 3 iterations, got %s", strings.TrimSpace(string(cntBytes)))
	}

	ompScript99 := "#!/bin/sh\n" +
		"cnt=$(cat \"" + counterFile + "\" 2>/dev/null || echo 0)\n" +
		"cnt=$((cnt + 1))\n" +
		"echo \"$cnt\" > \"" + counterFile + "\"\n" +
		"exit 99\n"
	if err := os.WriteFile(ompScript, []byte(ompScript99), 0o755); err != nil {
		t.Fatalf("write omp script 99: %v", err)
	}

	cmd = exec.Command("/bin/sh", workerPath, "test prompt", "--once")
	cmd.Dir = repoDir
	cmd.Env = []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"USER=testuser",
		"XDG_RUNTIME_DIR=" + runDir,
		"TASKD_WT_BASE=" + wtBase,
		"TASKD_URL=" + srv.URL,
	}
	out, err = cmd.CombinedOutput()
	exitErr, ok = err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected worker --once to exit 99, got %v: %s", err, string(out))
	}
	if exitErr.ExitCode() != 99 {
		t.Fatalf("expected exit code 99 without slot retry, got %d: %s", exitErr.ExitCode(), string(out))
	}
	cntBytes, err = os.ReadFile(counterFile)
	if err != nil {
		t.Fatalf("read counter file: %v", err)
	}
	if strings.TrimSpace(string(cntBytes)) != "4" {
		t.Fatalf("expected exactly 4 iterations without slot cascade, got %s", strings.TrimSpace(string(cntBytes)))
	}

	fetchWt := filepath.Join(wtBase, "wt-test-once-fetch")
	if out, err := exec.Command("git", "-C", repoDir, "worktree", "add", "--detach", "--force", fetchWt, "main").CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v: %s", err, out)
	}

	cmd = exec.Command("/bin/sh", workerPath, "--run-locked", "1", fetchWt, "onceproj", "nonexistent-branch-xyz", "0", "test prompt", "1")
	cmd.Dir = repoDir
	cmd.Env = []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"USER=testuser",
		"XDG_RUNTIME_DIR=" + runDir,
		"TASKD_WT_BASE=" + wtBase,
		"TASKD_URL=" + srv.URL,
	}
	out, err = cmd.CombinedOutput()
	exitErr, ok = err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected fetch failure in once mode to fail immediately, got %v: %s", err, string(out))
	}
	if exitErr.ExitCode() != 2 {
		t.Fatalf("expected infrastructure exit code 2 on fetch failure, got %d: %s", exitErr.ExitCode(), string(out))
	}
	if !strings.Contains(string(out), "git fetch failed in once mode") {
		t.Fatalf("expected git fetch failure message, got: %s", string(out))
	}
}

func TestWorkerSlotContention(t *testing.T) {
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
	startedFile := filepath.Join(binDir, "started")
	ompScript := filepath.Join(binDir, "omp")
	scriptContent := "#!/bin/sh\n" +
		"case \"$TASKD_WORKER\" in\n" +
		"*-1) touch \"" + startedFile + "\"; sleep 120 ;;\n" +
		"esac\n" +
		"exit 0\n"
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
	if err := os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write dummy file: %v", err)
	}
	if out, err := exec.Command("git", "-C", repoDir, "add", "file.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repoDir, "-c", "core.hookspath=", "-c", "sendpatch.enabled=false", "commit", "-m", "chore: initial commit", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repoDir, "remote", "add", "origin", repoDir).CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v: %s", err, out)
	}

	wtBase := t.TempDir()
	runDir := t.TempDir()
	env := []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"USER=testuser",
		"XDG_RUNTIME_DIR=" + runDir,
		"TASKD_WT_BASE=" + wtBase,
		"TASKD_URL=" + srv.URL,
	}

	holder := exec.Command("/bin/sh", workerPath, "test prompt", "--once", "--slot", "1")
	holder.Dir = repoDir
	holder.Env = env
	if err := holder.Start(); err != nil {
		t.Fatalf("start holder worker: %v", err)
	}
	defer func() {
		_ = holder.Process.Kill()
		_ = holder.Wait()
	}()

	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(startedFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for holder worker to claim slot 1")
		}
		time.Sleep(20 * time.Millisecond)
	}

	contender := exec.Command("/bin/sh", workerPath, "test prompt", "--once", "--slot", "1")
	contender.Dir = repoDir
	contender.Env = env
	out, err := contender.CombinedOutput()
	if err == nil {
		t.Fatalf("expected contender on locked slot to fail, got success: %s", string(out))
	}
	if !strings.Contains(string(out), "slot 1 is already locked") {
		t.Fatalf("expected slot contention message, got: %s", string(out))
	}

	cascade := exec.Command("/bin/sh", workerPath, "test prompt", "--once")
	cascade.Dir = repoDir
	cascade.Env = env
	cascadeOut, err := cascade.CombinedOutput()
	if err != nil {
		t.Fatalf("expected cascade worker to succeed on a free slot: %v: %s", err, string(cascadeOut))
	}
	if !strings.Contains(string(cascadeOut), "slot 2") {
		t.Fatalf("expected cascade worker to take slot 2, got: %s", string(cascadeOut))
	}
}
