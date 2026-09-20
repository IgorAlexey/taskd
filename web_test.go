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
