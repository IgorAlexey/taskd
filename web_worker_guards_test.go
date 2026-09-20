package main

import (
	"encoding/json"
	"errors"
	"os/exec"
	"testing"
)

func TestWebUIWorkerGuards(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available: " + err.Error())
	}

	out, err := exec.Command(node, "testdata/worker_guards.js", "web/index.html").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("worker_guards harness failed: %v\nstderr:\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("worker_guards harness failed: %v", err)
	}

	type btnState struct {
		Disabled     bool   `json:"disabled"`
		AriaDisabled bool   `json:"ariaDisabled"`
		State        string `json:"state"`
		Title        string `json:"title"`
	}
	var got struct {
		WithoutWorker struct {
			Complete btnState `json:"complete"`
			Release  btnState `json:"release"`
		} `json:"withoutWorker"`
		WithWorker struct {
			Complete btnState `json:"complete"`
			Release  btnState `json:"release"`
		} `json:"withWorker"`
		CompleteError string `json:"completeError"`
		ReleaseError  string `json:"releaseError"`
	}

	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	const wantTitle = "No active worker on leased task"
	if !got.WithoutWorker.Complete.Disabled || got.WithoutWorker.Complete.State != "disabled" || got.WithoutWorker.Complete.Title != wantTitle {
		t.Errorf("complete-task-btn without worker: %+v, want disabled with title %q", got.WithoutWorker.Complete, wantTitle)
	}
	if !got.WithoutWorker.Release.Disabled || got.WithoutWorker.Release.State != "disabled" || got.WithoutWorker.Release.Title != wantTitle {
		t.Errorf("release-task-btn without worker: %+v, want disabled with title %q", got.WithoutWorker.Release, wantTitle)
	}

	if got.WithWorker.Complete.Disabled || got.WithWorker.Complete.State != "ready" {
		t.Errorf("complete-task-btn with worker unexpectedly disabled: %+v", got.WithWorker.Complete)
	}
	if got.WithWorker.Release.Disabled || got.WithWorker.Release.State != "ready" {
		t.Errorf("release-task-btn with worker unexpectedly disabled: %+v", got.WithWorker.Release)
	}

	if got.CompleteError != "Cannot complete task: missing worker" {
		t.Errorf("completeTask missing worker error = %q, want %q", got.CompleteError, "Cannot complete task: missing worker")
	}
	if got.ReleaseError != "Cannot release task: missing worker" {
		t.Errorf("releaseTask missing worker error = %q, want %q", got.ReleaseError, "Cannot release task: missing worker")
	}
}
