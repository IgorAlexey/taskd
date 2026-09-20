package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const deletedFirst = "deleted task aaaaaaa"

// actResultUI brings up a polling TUI against url, painted once from a
// healthy round, and hands back a function that runs fn on the UI thread.
func actResultUI(t *testing.T, url string) (*ui, func(func())) {
	t.Helper()
	u := newUI(url, "", false, "")
	u.pollInterval = 20 * time.Millisecond
	u.msgTimeout = 300 * time.Millisecond
	ts, total, err := u.fetch("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	u.total = total
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
	ctx, cancel := context.WithCancel(context.Background())
	polled := make(chan struct{})
	go func() {
		u.poll(ctx)
		close(polled)
	}()
	t.Cleanup(func() {
		cancel()
		<-polled
		u.app.Stop()
		<-done
	})

	return u, func(fn func()) {
		ch := make(chan struct{})
		u.app.QueueUpdate(func() {
			fn()
			close(ch)
		})
		<-ch
	}
}

// deleteFirstRow presses D on the selected task and confirms the modal.
func deleteFirstRow(t *testing.T, u *ui, query func(func())) {
	t.Helper()
	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, 'D', 0))
	})
	var modal *tview.Modal
	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal != nil
	})
	u.app.QueueUpdateDraw(func() {
		u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyLeft, 0, 0), nil)
		u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
}

// actResultDaemon answers the queue from one pending task. GET /tasks
// stalls while stalling is set, and /projects fails while broken is.
func actResultDaemon(t *testing.T) (url string, deletes *atomic.Int64, stalling, broken *atomic.Bool) {
	t.Helper()
	tasks := []task{
		{ID: "aaaaaaa1", Project: "proj-b", Status: "pending", Body: "first task"},
	}
	deletes = &atomic.Int64{}
	stalling, broken = &atomic.Bool{}, &atomic.Bool{}
	stall := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		deletes.Add(1)
	})
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		if stalling.Load() {
			select {
			case <-stall:
			case <-r.Context().Done():
			}
			return
		}
		json.NewEncoder(w).Encode(tasks)
	})
	mux.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		if broken.Load() {
			http.Error(w, "projects exploded", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode([]string{"proj-b"})
	})
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]int{"pending": 1, "total": 1})
	})
	srv := httptest.NewServer(mux)

	saved := client
	client = &http.Client{Timeout: 150 * time.Millisecond}
	t.Cleanup(func() {
		client = saved
		close(stall)
		srv.Close()
	})
	return srv.URL, deletes, stalling, broken
}

// watchFooter samples the footer for d and fails if it ever holds anything
// but want or the empty string it decays to.
func watchFooter(t *testing.T, u *ui, query func(func()), want string, d time.Duration) (sawExpiry bool) {
	t.Helper()
	var msg string
	for deadline := time.Now().Add(d); time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
		query(func() { msg = u.msg })
		switch msg {
		case want:
		case "":
			sawExpiry = true
		default:
			t.Fatalf("background poll claimed the action's outcome: %q", msg)
		}
	}
	return sawExpiry
}

// TestActionResultSurvivesStalledRefetch drives a daemon that executes the
// DELETE and then stalls every GET /tasks behind it. The operator must be
// told the delete happened, must never be told it timed out -- not while
// the message is up, not after it expires -- and staleness must travel on
// the [disconnected] marker instead. Once a round lands, poll failures own
// the footer again.
func TestActionResultSurvivesStalledRefetch(t *testing.T) {
	url, deletes, stalling, _ := actResultDaemon(t)
	u, query := actResultUI(t, url)
	stalling.Store(true)
	deleteFirstRow(t, u, query)

	var msg string
	eventually(t, func() bool {
		query(func() { msg = u.msg })
		return msg == deletedFirst
	})
	if got := deletes.Load(); got != 1 {
		t.Fatalf("DELETE requests executed = %d, want 1", got)
	}

	eventually(t, func() bool { return u.disconnected.Load() })
	var status string
	query(func() { status = u.status.GetText(true) })
	if !strings.Contains(status, "[disconnected]") {
		t.Fatalf("status line lacks the staleness marker: %q", status)
	}
	if !strings.Contains(status, deletedFirst) {
		t.Fatalf("status line lost the action result: %q", status)
	}

	if !watchFooter(t, u, query, deletedFirst, time.Second) {
		t.Fatal("action message never expired, expiry boundary not exercised")
	}

	stalling.Store(false)
	eventually(t, func() bool { return !u.disconnected.Load() })
	stalling.Store(true)
	eventually(t, func() bool {
		query(func() { msg = u.msg })
		return strings.Contains(msg, "deadline exceeded")
	})
}

// TestProjectsErrorDoesNotStompActionResult holds the other half of the
// rule. /tasks is healthy throughout, so the action's own refetch returns
// clean; only /projects keeps failing. The round still has not landed, so
// the delete the operator just committed keeps the footer instead of being
// relabelled with a 500 by the poll behind it. Crossing the expiry is the
// sibling test's job; the discarded watch result is deliberate.
func TestProjectsErrorDoesNotStompActionResult(t *testing.T) {
	url, deletes, _, broken := actResultDaemon(t)
	u, query := actResultUI(t, url)
	broken.Store(true)
	deleteFirstRow(t, u, query)

	var msg string
	eventually(t, func() bool {
		query(func() { msg = u.msg })
		return msg == deletedFirst
	})
	if got := deletes.Load(); got != 1 {
		t.Fatalf("DELETE requests executed = %d, want 1", got)
	}

	_ = watchFooter(t, u, query, deletedFirst, 700*time.Millisecond)
}
