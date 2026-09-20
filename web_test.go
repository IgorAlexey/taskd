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
