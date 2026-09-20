package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// workerStub answers /tasks with the daemon's own worker semantics: a
// worker owns a task only while it holds the lease or has finished it.
func workerStub(t *testing.T, all []task, workers []string) (*ui, func() string) {
	t.Helper()
	var mu sync.Mutex
	var lastQuery string

	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		lastQuery = r.URL.RawQuery
		mu.Unlock()
		worker := r.URL.Query().Get("worker")
		out := []task{}
		for _, tk := range all {
			owned := tk.Worker == worker && (tk.Status == "done" || tk.Status == "leased")
			if worker == "" || owned {
				out = append(out, tk)
			}
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("GET /workers", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(workers)
	})
	mux.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]string{})
	})
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]int{"pending": 9, "leased": 9, "done": 9, "total": 27})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	u := newUI(srv.URL, "", false, "")
	u.msgTimeout = time.Hour
	u.render(nil)

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

	query := func() string {
		mu.Lock()
		defer mu.Unlock()
		return lastQuery
	}
	return u, query
}

// cycleWorkerKey presses w on the event goroutine, as a real screen would.
func cycleWorkerKey(u *ui) {
	pressed := make(chan struct{})
	u.app.QueueUpdate(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, 'w', 0))
		close(pressed)
	})
	<-pressed
}

// statusPane reads the status bar from the event goroutine.
func statusPane(u *ui) string {
	var text string
	queried := make(chan struct{})
	u.app.QueueUpdate(func() {
		text = u.status.GetText(true)
		close(queried)
	})
	<-queried
	return text
}

func workerScope(u *ui) string {
	_, worker := u.scope()
	return worker
}

func shownIDs(u *ui) []string {
	var ids []string
	queried := make(chan struct{})
	u.app.QueueUpdate(func() {
		for _, tk := range u.shown {
			ids = append(ids, tk.ID)
		}
		close(queried)
	})
	<-queried
	return ids
}

var workerTasks = []task{
	{ID: "aaaaaaa1", Status: "leased", Worker: "w1", LeaseExpires: 1 << 40, Body: "w1 leased"},
	{ID: "bbbbbbb2", Status: "leased", Worker: "w2", LeaseExpires: 1 << 40, Body: "w2 leased"},
	{ID: "ccccccc3", Status: "pending", Body: "unclaimed"},
}

func TestTUIWorkerFilter(t *testing.T) {
	// The daemon owns the filter: w has to reach it through the query
	// string, or the 500-row window silently hides a busy worker's tail.
	t.Run("w scopes the query server side", func(t *testing.T) {
		u, lastQuery := workerStub(t, workerTasks, []string{"w1", "w2"})

		cycleWorkerKey(u)
		eventually(t, func() bool { return workerScope(u) == "w1" })
		eventually(t, func() bool {
			return strings.Contains(lastQuery(), "worker=w1")
		})
		eventually(t, func() bool {
			ids := shownIDs(u)
			return len(ids) == 1 && ids[0] == "aaaaaaa1"
		})
		if got := statusPane(u); !strings.Contains(got, "worker w1") {
			t.Fatalf("status line missing the active worker: %q", got)
		}
	})

	t.Run("cycle walks every worker then clears", func(t *testing.T) {
		u, lastQuery := workerStub(t, workerTasks, []string{"w1", "w2"})

		cycleWorkerKey(u)
		eventually(t, func() bool { return workerScope(u) == "w1" })
		cycleWorkerKey(u)
		eventually(t, func() bool { return workerScope(u) == "w2" })
		eventually(t, func() bool {
			ids := shownIDs(u)
			return len(ids) == 1 && ids[0] == "bbbbbbb2"
		})

		cycleWorkerKey(u)
		eventually(t, func() bool { return workerScope(u) == "" })
		eventually(t, func() bool { return len(shownIDs(u)) == 3 })
		if q := lastQuery(); strings.Contains(q, "worker=") {
			t.Fatalf("unfiltered query must not scope a worker, got %q", q)
		}
		if got := statusPane(u); strings.Contains(got, "worker w") {
			t.Fatalf("status line must drop the worker filter: %q", got)
		}
	})

	// /workers keeps a worker that only has finished tasks, so the default
	// live filter hides every row. The empty state has to name the filter
	// the operator just set, not the one they never touched.
	t.Run("empty state names the worker under the live filter", func(t *testing.T) {
		tasks := []task{
			{ID: "ddddddd4", Status: "done", Worker: "w-done", Body: "finished"},
		}
		u, _ := workerStub(t, tasks, []string{"w-done"})

		cycleWorkerKey(u)
		eventually(t, func() bool { return workerScope(u) == "w-done" })
		eventually(t, func() bool {
			return strings.Contains(bodyText(u), "No live tasks for worker w-done")
		})
		if body := bodyText(u); !strings.Contains(body, "'w' to cycle worker") {
			t.Fatalf("empty state must offer the way out, got %q", body)
		}
	})

	// A lease expires and the worker leaves /workers. Widening the table
	// back to every task is a change the operator must be told about.
	t.Run("stale worker is reported and cleared", func(t *testing.T) {
		u, _ := workerStub(t, workerTasks, []string{"w1"})

		cycleWorkerKey(u)
		eventually(t, func() bool { return workerScope(u) == "w1" })

		u.app.QueueUpdate(func() { u.setWorker("gone-worker") })
		cycleWorkerKey(u)
		eventually(t, func() bool { return workerScope(u) == "" })
		eventually(t, func() bool {
			return strings.Contains(statusPane(u), "gone-worker is no longer active")
		})
	})

	t.Run("no active workers", func(t *testing.T) {
		u, _ := workerStub(t, workerTasks, []string{})

		cycleWorkerKey(u)
		eventually(t, func() bool {
			return strings.Contains(statusPane(u), "no active workers")
		})
		if got := workerScope(u); got != "" {
			t.Fatalf("worker filter must stay unset, got %q", got)
		}
	})

	// Server stats are project scoped, so they cannot describe a worker
	// scoped table; the counts have to come from the rows on screen.
	t.Run("counts follow the worker scope", func(t *testing.T) {
		u, _ := workerStub(t, workerTasks, []string{"w1", "w2"})

		cycleWorkerKey(u)
		eventually(t, func() bool { return workerScope(u) == "w1" })
		eventually(t, func() bool {
			var got string
			queried := make(chan struct{})
			u.app.QueueUpdate(func() {
				got = fmt.Sprintf("%d %d %d %v",
					u.stats.Pending, u.stats.Leased, u.stats.Done, u.hasServerStats)
				close(queried)
			})
			<-queried
			return got == "0 1 0 false"
		})
	})

	t.Run("fetch carries project and worker", func(t *testing.T) {
		var lastQuery string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lastQuery = r.URL.RawQuery
			json.NewEncoder(w).Encode([]task{})
		}))
		t.Cleanup(srv.Close)

		u := newUI(srv.URL, "", false, "")
		if _, err := u.fetch("proj-a", "w1"); err != nil {
			t.Fatalf("fetch: %v", err)
		}
		if lastQuery != "limit=500&project=proj-a&worker=w1" {
			t.Fatalf("unexpected query %q", lastQuery)
		}
		if _, err := u.fetch("", ""); err != nil {
			t.Fatalf("fetch: %v", err)
		}
		if lastQuery != "limit=500" {
			t.Fatalf("unscoped fetch must stay bare, got %q", lastQuery)
		}
	})

	t.Run("nextWorker", func(t *testing.T) {
		for _, tc := range []struct {
			workers []string
			current string
			want    string
			gone    bool
		}{
			{[]string{"a", "b"}, "", "a", false},
			{[]string{"a", "b"}, "a", "b", false},
			{[]string{"a", "b"}, "b", "", false},
			{[]string{"a", "b"}, "c", "", true},
			{nil, "", "", false},
			{nil, "a", "", true},
		} {
			got, gone := nextWorker(tc.workers, tc.current)
			if got != tc.want || gone != tc.gone {
				t.Errorf("nextWorker(%v, %q) = (%q, %v), want (%q, %v)",
					tc.workers, tc.current, got, gone, tc.want, tc.gone)
			}
		}
	})
}
