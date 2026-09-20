package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestWebUIURLState(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required to run the web UI harness")
		}
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/urlstate.js", "web/index.html").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("harness failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("harness failed: %v", err)
	}

	var got struct {
		Priority struct {
			Search       string `json:"search"`
			ControlValue string `json:"controlValue"`
			QueueCount   string `json:"queueCount"`
			List         string `json:"list"`
		}
		PriorityNegative struct {
			Search       string `json:"search"`
			ControlValue string `json:"controlValue"`
		}
		PriorityChange struct {
			Search       string `json:"search"`
			ControlValue string `json:"controlValue"`
			List         string `json:"list"`
		}
		PriorityRefuse struct {
			Search       string `json:"search"`
			ControlValue string `json:"controlValue"`
		}
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if got.Priority.ControlValue != "6" {
		t.Errorf("priority boot control value = %q, want 6", got.Priority.ControlValue)
	}
	if !strings.Contains(got.Priority.Search, "priority=6") {
		t.Errorf("priority boot search = %q, want priority=6", got.Priority.Search)
	}
	if !strings.Contains(got.Priority.List, "&priority=6") {
		t.Errorf("priority boot list fetch = %q, want &priority=6", got.Priority.List)
	}
	if got.Priority.QueueCount != "Showing 1-1 of 1" {
		t.Errorf("priority queue count = %q, want Showing 1-1 of 1", got.Priority.QueueCount)
	}

	if got.PriorityNegative.ControlValue != "" {
		t.Errorf("priority negative control value = %q, want empty", got.PriorityNegative.ControlValue)
	}
	if strings.Contains(got.PriorityNegative.Search, "priority") {
		t.Errorf("priority negative search = %q, want priority omitted", got.PriorityNegative.Search)
	}

	if got.PriorityChange.ControlValue != "6" || !strings.Contains(got.PriorityChange.Search, "priority=6") {
		t.Errorf("priority change = %+v, want control 6 and priority=6 in search", got.PriorityChange)
	}
	if !strings.Contains(got.PriorityChange.List, "&priority=6") {
		t.Errorf("priority change list fetch = %q, want &priority=6", got.PriorityChange.List)
	}

	if got.PriorityRefuse.ControlValue != "" || strings.Contains(got.PriorityRefuse.Search, "priority") {
		t.Errorf("priority refuse = %+v, want control empty and priority omitted", got.PriorityRefuse)
	}
}
