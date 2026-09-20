package main

import (
	"encoding/json"
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
	sim   tcell.SimulationScreen
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()
	u, tasks, mu := stub(t)
	ts, err := u.fetch("")
	if err != nil {
		t.Fatal(err)
	}
	u.render(ts)

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(80, 25)
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
	return &testHarness{u: u, tasks: tasks, mu: mu, sim: sim}
}

func (h *testHarness) screenText() string {
	h.query(func() {
		h.u.app.ForceDraw()
	})
	cells, _, _ := h.sim.GetContents()
	var sb strings.Builder
	for _, c := range cells {
		for _, r := range c.Runes {
			sb.WriteRune(r)
		}
	}
	return sb.String()
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

func TestClaimCountDisplay(t *testing.T) {
	u := newUI("http://127.0.0.1:1", "", false)
	tasks := []task{
		{
			ID:         "task-0",
			Project:    "proj-a",
			Status:     "pending",
			ClaimCount: 0,
			Body:       "unclaimed task",
		},
		{
			ID:         "task-1",
			Project:    "proj-a",
			Status:     "leased",
			Worker:     "w1",
			ClaimCount: 1,
			Body:       "claimed once",
		},
		{
			ID:         "task-retried",
			Project:    "proj-b",
			Status:     "pending",
			ClaimCount: 5,
			Body:       "poison pill retry backlog",
		},
	}
	u.render(tasks)

	if got := u.table.GetCell(0, 6).Text; got != "CLAIMS" {
		t.Fatalf("claims header = %q, want CLAIMS", got)
	}
	if got := u.table.GetCell(1, 6).Text; got != "0" {
		t.Fatalf("row 1 claims = %q, want 0", got)
	}
	if got := u.table.GetCell(2, 6).Text; got != "1" {
		t.Fatalf("row 2 claims = %q, want 1", got)
	}
	if got := u.table.GetCell(3, 6).Text; got != "5" {
		t.Fatalf("row 3 claims = %q, want 5", got)
	}

	u.table.Select(1, 0)
	u.showBody()
	body0 := u.body.GetText(true)
	if !strings.Contains(body0, "Claims:    0") {
		t.Fatalf("expected Claims:    0 in detail pane, got: %q", body0)
	}

	u.table.Select(2, 0)
	u.showBody()
	body1 := u.body.GetText(true)
	if !strings.Contains(body1, "Claims:    1") {
		t.Fatalf("expected Claims:    1 in detail pane, got: %q", body1)
	}

	u.table.Select(3, 0)
	u.showBody()
	body5 := u.body.GetText(true)
	if !strings.Contains(body5, "Claims:    5") {
		t.Fatalf("expected Claims:    5 in detail pane, got: %q", body5)
	}
}

func TestTaskJSONDeserializationClaimCount(t *testing.T) {
	raw := `{"id":"t-1","project":"p","asset_path":"","status":"pending","worker":"","lease_expires":0,"priority":2,"claim_count":7,"body":"hello"}`
	var tk task
	if err := json.Unmarshal([]byte(raw), &tk); err != nil {
		t.Fatalf("unmarshal task JSON: %v", err)
	}
	if tk.ClaimCount != 7 {
		t.Fatalf("deserialized claim_count = %d, want 7", tk.ClaimCount)
	}
}
