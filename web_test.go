package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestWebUIFetchTimeout(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required to run the web UI harness")
		}
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/timeout.js", "web/index.html").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("harness failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("harness failed: %v", err)
	}

	var got struct {
		BootRows         []string `json:"bootRows"`
		BootCount        string   `json:"bootCount"`
		BootStatus       string   `json:"bootStatus"`
		BootDisplay      string   `json:"bootDisplay"`
		WindowRequests   int      `json:"windowRequests"`
		StaleRows        []string `json:"staleRows"`
		OfflineStatus    string   `json:"offlineStatus"`
		OfflineDisplay   string   `json:"offlineDisplay"`
		RetryRequests    int      `json:"retryRequests"`
		RecoveredRows    []string `json:"recoveredRows"`
		RecoveredDisplay string   `json:"recoveredDisplay"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if len(got.BootRows) != 3 || got.BootCount != "Showing 1-3 of 3" {
		t.Errorf("boot state = %v, count = %q, want 3 tasks", got.BootRows, got.BootCount)
	}
	if got.WindowRequests != 0 {
		t.Errorf("requests issued during timeout window = %d, want 0", got.WindowRequests)
	}
	if !strings.Contains(got.OfflineStatus, "Offline") || got.OfflineDisplay != "inline-flex" {
		t.Errorf("offline status = %q, display = %q, want visible offline indicator", got.OfflineStatus, got.OfflineDisplay)
	}
	if got.RetryRequests == 0 {
		t.Errorf("requests issued on poll tick after timeout window = 0, want > 0")
	}
	wantRecovered := []string{"41764ca4", "be3024c2", "3642f64f"}
	if !slices.Equal(got.RecoveredRows, wantRecovered) {
		t.Errorf("recovered rows = %v, want %v", got.RecoveredRows, wantRecovered)
	}
	if got.RecoveredDisplay != "none" {
		t.Errorf("recovered status display = %q, want none", got.RecoveredDisplay)
	}
}

func TestWebUIInitialPlaceholdersAndNoscript(t *testing.T) {
	ui := string(uiHTML)
	stats := []string{"stat-pending", "stat-leased", "stat-done", "stat-buried", "stat-total"}
	for _, id := range stats {
		placeholder := `id="` + id + `">-`
		if !strings.Contains(ui, placeholder) {
			t.Errorf("expected placeholder %q in web/index.html", placeholder)
		}
		zero := `id="` + id + `">0`
		if strings.Contains(ui, zero) {
			t.Errorf("found hard-coded zero %q in web/index.html", zero)
		}
	}

	if !strings.Contains(ui, "<noscript") {
		t.Fatal("expected <noscript> block in web/index.html")
	}
	if !strings.Contains(ui, "CLI") || !strings.Contains(ui, "/tasks") {
		t.Error("expected noscript block to name CLI and API alternatives")
	}

	if !strings.Contains(ui, `id="queue-count"`) {
		t.Fatal("expected #queue-count in web/index.html")
	}
	if strings.Contains(ui, `id="queue-count" style="color: var(--text-muted); font-size: 12px;">0 tasks</span>`) {
		t.Error("expected queue-count to not start with hard-coded 0 tasks")
	}

	if !strings.Contains(ui, `id="task-table-body"`) {
		t.Fatal("expected #task-table-body in web/index.html")
	}
	if !strings.Contains(ui, "Not connected") {
		t.Error("expected initial table body to indicate Not connected")
	}
	if strings.Contains(ui, `<tbody id="task-table-body">`+"\n"+`            <tr><td colspan="7" style="text-align: center; color: var(--text-muted);">No tasks</td></tr>`) {
		t.Error("table body should not claim No tasks in initial markup")
	}
}

func TestWebUIExpiredLeaseActions(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, "function isActivelyLeased(t)") {
		t.Fatal("expected isActivelyLeased helper in web/index.html")
	}
	if strings.Contains(ui, `id="edit-task-btn"${t.status === 'leased'`) {
		t.Error("edit-task-btn should not unconditionally disable on leased status")
	}
	if !strings.Contains(ui, `id="edit-task-btn"${isActivelyLeased(t) ? ' disabled aria-disabled="true" title="Actively leased tasks cannot be edited"' : ''}`) {
		t.Error("edit-task-btn should check isActivelyLeased(t)")
	}
	if strings.Contains(ui, `id="delete-task-btn" class="danger"${t.status === 'leased'`) {
		t.Error("delete-task-btn should not unconditionally disable on leased status")
	}
	if !strings.Contains(ui, `id="delete-task-btn" class="danger"${isActivelyLeased(t) ? ' disabled aria-disabled="true" title="Actively leased tasks cannot be deleted"' : ''}`) {
		t.Error("delete-task-btn should check isActivelyLeased(t)")
	}
	if strings.Contains(ui, "if (!id || status === 'leased') return") {
		t.Error("deleteTask should not unconditionally return on leased status")
	}
	if !strings.Contains(ui, "if (!t || !t.id || isActivelyLeased(t)) return") {
		t.Error("deleteTask should guard on active lease expiration")
	}
	if strings.Contains(ui, "currentTask.status === 'leased'") {
		t.Error("task edit handlers should not unconditionally return on leased status")
	}
	if !strings.Contains(ui, "if (!currentTask || currentTask.status === 'done' || isActivelyLeased(currentTask)) return") {
		t.Error("task edit handlers should guard on active lease expiration")
	}
}
func TestWebUIConfirmActions(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, "confirm('Complete this task as done?')") {
		t.Error("expected completeTask to require confirmation before completion")
	}
	if !strings.Contains(ui, "confirm('Close this task as done without a result?')") {
		t.Error("expected closeTask to require confirmation before closing")
	}
}
