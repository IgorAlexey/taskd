package main

import (
	"slices"
	"strings"
	"testing"
)

func touchCallsSnapshot() []string {
	touchMu.Lock()
	defer touchMu.Unlock()
	return slices.Clone(touchCalls)
}

func TestTUITouchLease(t *testing.T) {
	t.Setenv("TASKD_WORKER", "w1")
	h := newTestHarness(t)

	h.selectID("aaaaaaa1")
	h.press('t')
	if msg := h.message(); !strings.Contains(msg, "not leased") {
		t.Fatalf("expected not-leased message, got %q", msg)
	}
	if got := touchCallsSnapshot(); len(got) != 0 {
		t.Fatalf("pending task must not trigger touch call, got %v", got)
	}

	h.selectID("bbbbbbb2")
	h.press('t')
	eventually(t, func() bool { return len(touchCallsSnapshot()) == 1 })
	if got := touchCallsSnapshot()[0]; got != "bbbbbbb2 w1" {
		t.Fatalf("touch call = %q, want %q", got, "bbbbbbb2 w1")
	}
	eventually(t, func() bool { return strings.Contains(h.message(), "touched task bbbbbbb") })

	h.mu.Lock()
	for i := range *h.tasks {
		if (*h.tasks)[i].ID == "bbbbbbb2" {
			(*h.tasks)[i].Worker = "w2"
		}
	}
	h.mu.Unlock()

	h.selectID("bbbbbbb2")
	h.press('t')
	eventually(t, func() bool { return len(touchCallsSnapshot()) == 2 })
	if got := touchCallsSnapshot()[1]; got != "bbbbbbb2 w1" {
		t.Fatalf("touch call = %q, want own worker %q", got, "bbbbbbb2 w1")
	}
	eventually(t, func() bool { return strings.Contains(h.message(), "409") })
}
