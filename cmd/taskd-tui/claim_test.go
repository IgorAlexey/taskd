package main

import (
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gdamore/tcell/v2"
)

type testHarness struct {
	u     *ui
	tasks *[]task
	mu    *sync.Mutex
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()
	u, tasks, mu := stub(t)
	ts, err := u.fetch()
	if err != nil {
		t.Fatal(err)
	}
	u.render(ts)

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	u.app.SetScreen(sim)
	u.app.SetRoot(u.pages, true)
	done := make(chan struct{})
	go func() {
		u.app.Run()
		close(done)
	}()
	t.Cleanup(func() {
		u.app.Stop()
		<-done
	})
	return &testHarness{u: u, tasks: tasks, mu: mu}
}

func (h *testHarness) query(fn func()) {
	ch := make(chan struct{})
	h.u.app.QueueUpdate(func() {
		fn()
		close(ch)
	})
	<-ch
}

func (h *testHarness) selectID(id string) {
	h.query(func() {
		for i, tk := range h.u.shown {
			if tk.ID == id {
				h.u.table.Select(i+1, 0)
				return
			}
		}
	})
}

func (h *testHarness) press(r rune) {
	h.u.keys(tcell.NewEventKey(tcell.KeyRune, r, 0))
}

func (h *testHarness) message() string {
	var msg string
	h.query(func() { msg = h.u.msg })
	return msg
}

func claimCallsSnapshot() []string {
	claimMu.Lock()
	defer claimMu.Unlock()
	return slices.Clone(claimCalls)
}

func TestClaimKey(t *testing.T) {
	t.Setenv("TASKD_WORKER", "worker-claimed")
	h := newTestHarness(t)

	h.selectID("bbbbbbb2")
	h.press('c')
	if msg := h.message(); !strings.Contains(msg, "not pending") {
		t.Fatalf("expected not-pending message, got %q", msg)
	}
	if got := claimCallsSnapshot(); len(got) != 0 {
		t.Fatalf("leased task must not trigger claim call, got %v", got)
	}

	h.selectID("aaaaaaa1")
	h.press('c')
	eventually(t, func() bool { return len(claimCallsSnapshot()) == 1 })
	if got := claimCallsSnapshot()[0]; got != "aaaaaaa1 worker-claimed" {
		t.Fatalf("claim call = %q, want %q", got, "aaaaaaa1 worker-claimed")
	}
	eventually(t, func() bool {
		var status string
		h.query(func() {
			for _, tk := range h.u.all {
				if tk.ID == "aaaaaaa1" {
					status = tk.Status
				}
			}
		})
		return status == "leased"
	})
	if msg := h.message(); !strings.Contains(msg, "claimed task aaaaaaa") {
		t.Fatalf("expected claim confirmation, got %q", msg)
	}
}

func TestClaimKeyDefaultWorker(t *testing.T) {
	t.Setenv("TASKD_WORKER", "")
	t.Setenv("USER", "testoperator")
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "localhost"
	}
	wantWorker := host + ":testoperator"
	if got := defaultWorker(); got != wantWorker {
		t.Fatalf("defaultWorker() = %q, want %q", got, wantWorker)
	}

	h := newTestHarness(t)
	h.selectID("aaaaaaa1")
	h.press('c')

	eventually(t, func() bool { return len(claimCallsSnapshot()) == 1 })
	if got := claimCallsSnapshot()[0]; got != "aaaaaaa1 "+wantWorker {
		t.Fatalf("claim call = %q, want %q", got, "aaaaaaa1 "+wantWorker)
	}
}

func TestClaimKeyConflict(t *testing.T) {
	t.Setenv("TASKD_WORKER", "worker-c")
	h := newTestHarness(t)

	h.mu.Lock()
	for i := range *h.tasks {
		if (*h.tasks)[i].ID == "aaaaaaa1" {
			(*h.tasks)[i].Status = "leased"
			(*h.tasks)[i].Worker = "other-worker"
		}
	}
	h.mu.Unlock()

	h.selectID("aaaaaaa1")
	h.press('c')

	eventually(t, func() bool { return strings.Contains(h.message(), "409") })
}
