package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

type scopeServer struct {
	mu    sync.Mutex
	tasks []task
	reqs  []url.Values
	raws  []string
}

func (s *scopeServer) record(r *http.Request) url.Values {
	q := r.URL.Query()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, q)
	s.raws = append(s.raws, r.URL.RawQuery)
	return q
}

func (s *scopeServer) snapshot() []task {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.tasks)
}

func (s *scopeServer) queries() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.reqs)
}

func (s *scopeServer) rawQueries() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.raws)
}

func (s *scopeServer) sawQuery(needle, limit string) bool {
	for _, q := range s.queries() {
		if q.Get("q") == needle && q.Get("limit") == limit {
			return true
		}
	}
	return false
}

func scopeHaystack(t task) string {
	return strings.ToLower(t.ID + "\x00" + t.Body + "\x00" + t.Project + "\x00" + t.Worker + "\x00" + t.AssetPath)
}

func scopeIntParam(v string, fallback int) int {
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func newScopeUI(t *testing.T, tasks []task) (*ui, *scopeServer) {
	t.Helper()
	s := &scopeServer{tasks: tasks}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		q := s.record(r)
		needle := strings.ToLower(q.Get("q"))
		project, worker := q.Get("project"), q.Get("worker")
		var matched []task
		for _, tk := range s.snapshot() {
			if project != "" && tk.Project != project {
				continue
			}
			if worker != "" && tk.Worker != worker {
				continue
			}
			if needle != "" && !strings.Contains(scopeHaystack(tk), needle) {
				continue
			}
			matched = append(matched, tk)
		}
		total := len(matched)
		limit := scopeIntParam(q.Get("limit"), 500)
		if limit < 1 || limit > 1000 {
			http.Error(w, `{"error":"invalid limit"}`, http.StatusBadRequest)
			return
		}
		if len(matched) > limit {
			matched = matched[:limit]
		}
		w.Header().Set("X-Total-Count", strconv.Itoa(total))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(matched)
	})
	mux.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		var projects []string
		for _, tk := range s.snapshot() {
			if tk.Project != "" && !slices.Contains(projects, tk.Project) {
				projects = append(projects, tk.Project)
			}
		}
		slices.Sort(projects)
		json.NewEncoder(w).Encode(projects)
	})
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		project := r.URL.Query().Get("project")
		var pending, leased, done int
		for _, tk := range s.snapshot() {
			if project != "" && tk.Project != project {
				continue
			}
			switch tk.Status {
			case "pending":
				pending++
			case "leased":
				leased++
			case "done":
				done++
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"pending": pending, "leased": leased, "done": done})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u := newUI(srv.URL, "", false, "")
	u.filter = ""
	return u, s
}

func seedScopeTasks(n int) []task {
	ts := make([]task, n)
	for i := range n {
		ts[i] = task{
			ID:      fmt.Sprintf("task%05d", i),
			Project: "queue",
			Status:  "pending",
			Body:    fmt.Sprintf("filler body %05d", i),
		}
	}
	return ts
}

func startScopeApp(t *testing.T, u *ui, width, height int) func(func()) {
	t.Helper()
	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(width, height)
	u.width = width
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
	return func(fn func()) {
		ch := make(chan struct{})
		u.app.QueueUpdate(func() {
			fn()
			close(ch)
		})
		<-ch
	}
}

func scopeAwait(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 400 {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func scopeStatusLine1(u *ui, query func(func())) string {
	var line string
	query(func() {
		line = strings.SplitN(u.status.GetText(true), "\n", 2)[0]
	})
	return line
}

func scopeShownIDs(u *ui, query func(func())) []string {
	var ids []string
	query(func() {
		for _, tk := range u.shown {
			ids = append(ids, tk.ID)
		}
	})
	return ids
}

func TestFetchAsksForOneDefaultWindow(t *testing.T) {
	u, srv := newScopeUI(t, seedScopeTasks(530))

	ts, total, err := u.fetch("", "", "")
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if len(ts) != 500 {
		t.Fatalf("fetch returned %d tasks, want the 500-row default window", len(ts))
	}
	if total != 530 {
		t.Fatalf("fetch reported total %d, want 530", total)
	}
	if ts[499].ID != "task00499" {
		t.Fatalf("last windowed task is %q, want task00499", ts[499].ID)
	}

	raws := srv.rawQueries()
	if len(raws) != 1 {
		t.Fatalf("fetch issued %d requests, want exactly 1: %v", len(raws), raws)
	}
	if raws[0] != "limit=500" {
		t.Fatalf("request query is %q, want %q", raws[0], "limit=500")
	}
}

func TestSearchReachesTaskBeyondFetchCap(t *testing.T) {
	const needle = "NEEDLE-7x9"
	tasks := seedScopeTasks(601)
	tasks[600].ID = "deepbeef"
	tasks[600].Body = "haystack " + needle + " tail"
	u, srv := newScopeUI(t, tasks)
	u.maxRows = 600
	query := startScopeApp(t, u, 120, 30)

	u.refresh("")
	scopeAwait(t, "initial window to paint", func() bool {
		var n int
		query(func() { n = len(u.all) })
		return n == 500
	})
	if slices.Contains(scopeShownIDs(u, query), "deepbeef") {
		t.Fatal("needle task was inside the first window; test cannot prove server-side search")
	}

	query(func() { u.keys(tcell.NewEventKey(tcell.KeyRune, '/', 0)) })
	for _, r := range needle {
		query(func() { u.keys(tcell.NewEventKey(tcell.KeyRune, r, 0)) })
	}

	scopeAwait(t, "needle task to become visible", func() bool {
		return slices.Contains(scopeShownIDs(u, query), "deepbeef")
	})
	scopeAwait(t, "the full query to reach the daemon", func() bool {
		return srv.sawQuery(needle, strconv.Itoa(u.maxRows))
	})
}

func TestClearingQueryRefetchesWindow(t *testing.T) {
	const needle = "UNIQUE-Q1"
	tasks := seedScopeTasks(530)
	tasks[512].ID = "farbeef1"
	tasks[512].Body = "haystack " + needle + " tail"
	u, _ := newScopeUI(t, tasks)
	u.maxRows = 600
	query := startScopeApp(t, u, 120, 30)

	u.refresh("")
	scopeAwait(t, "initial window to paint", func() bool {
		var n int
		query(func() { n = len(u.shown) })
		return n == 500
	})

	query(func() { u.keys(tcell.NewEventKey(tcell.KeyRune, '/', 0)) })
	for _, r := range needle {
		query(func() { u.keys(tcell.NewEventKey(tcell.KeyRune, r, 0)) })
	}
	scopeAwait(t, "search to narrow the table to the needle", func() bool {
		return slices.Equal(scopeShownIDs(u, query), []string{"farbeef1"})
	})

	query(func() { u.keys(tcell.NewEventKey(tcell.KeyEscape, 0, 0)) })

	scopeAwait(t, "cleared query to restore the default window", func() bool {
		var shown, all int
		query(func() { shown, all = len(u.shown), len(u.all) })
		return shown == 500 && all == 500
	})
}

func TestStatusDisclosesTruncatedQueue(t *testing.T) {
	u, _ := newScopeUI(t, seedScopeTasks(530))
	query := startScopeApp(t, u, 120, 30)

	u.refresh("")
	scopeAwait(t, "truncation disclosure in the footer", func() bool {
		return strings.Contains(scopeStatusLine1(u, query), "fetched 500 of 530")
	})
}

func TestBottomOfQueueReachable(t *testing.T) {
	u, srv := newScopeUI(t, seedScopeTasks(530))
	u.maxRows = 600
	query := startScopeApp(t, u, 100, 30)

	u.refresh("")
	scopeAwait(t, "the default window to paint", func() bool {
		var n int
		query(func() { n = len(u.shown) })
		return n == 500
	})

	query(func() { u.keys(tcell.NewEventKey(tcell.KeyRune, 'G', 0)) })

	scopeAwait(t, "footer to report row 530 of 530", func() bool {
		return strings.Contains(scopeStatusLine1(u, query), "row 530 of 530")
	})
	if !srv.sawQuery("", strconv.Itoa(u.maxRows)) {
		t.Fatalf("no widened request carried limit=%d: %v", u.maxRows, srv.rawQueries())
	}
}

func TestJumpToBottomDoesNotLatch(t *testing.T) {
	u, _ := newScopeUI(t, seedScopeTasks(1400))
	u.maxRows = 1000
	query := startScopeApp(t, u, 100, 30)

	u.refresh("")
	scopeAwait(t, "the default window to paint", func() bool {
		var n int
		query(func() { n = len(u.shown) })
		return n == 500
	})

	query(func() { u.keys(tcell.NewEventKey(tcell.KeyRune, 'G', 0)) })
	scopeAwait(t, "the widened window to paint", func() bool {
		var n int
		query(func() { n = len(u.all) })
		return n == 1000
	})

	query(func() { u.table.Select(3, 0) })
	for range 3 {
		query(func() { u.render(u.all) })
		var row int
		query(func() { row = u.selectedRow() })
		if row != 3 {
			t.Fatalf("repaint dragged the cursor to row %d, jumpBottom latched", row)
		}
	}
}

func TestStaleQueryAnswerIsDropped(t *testing.T) {
	tasks := seedScopeTasks(20)
	tasks[7].ID = "onlyhit1"
	tasks[7].Body = "haystack ONLY-ME tail"
	u, _ := newScopeUI(t, tasks)
	query := startScopeApp(t, u, 100, 30)

	u.refresh("")
	scopeAwait(t, "the queue to paint", func() bool {
		var n int
		query(func() { n = len(u.shown) })
		return n == 20
	})

	u.refreshOnce("", "", "ONLY-ME")

	var shown int
	query(func() { shown = len(u.shown) })
	if shown != 20 {
		t.Fatalf("an answer for a query the operator never typed painted %d rows", shown)
	}
}

func TestJumpToBottomSurvivesNarrowRepaint(t *testing.T) {
	u, _ := newScopeUI(t, seedScopeTasks(530))
	u.maxRows = 600
	query := startScopeApp(t, u, 100, 30)

	u.refresh("")
	scopeAwait(t, "the default window to paint", func() bool {
		var n int
		query(func() { n = len(u.shown) })
		return n == 500
	})

	query(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, 'G', 0))
		u.render(u.all)
	})

	scopeAwait(t, "footer to report row 530 of 530", func() bool {
		return strings.Contains(scopeStatusLine1(u, query), "row 530 of 530")
	})
}

func TestTopOfQueueDropsWideWindow(t *testing.T) {
	u, srv := newScopeUI(t, seedScopeTasks(530))
	u.maxRows = 600
	query := startScopeApp(t, u, 100, 30)

	u.refresh("")
	scopeAwait(t, "the default window to paint", func() bool {
		var n int
		query(func() { n = len(u.shown) })
		return n == 500
	})
	query(func() { u.keys(tcell.NewEventKey(tcell.KeyRune, 'G', 0)) })
	scopeAwait(t, "the widened window to paint", func() bool {
		var n int
		query(func() { n = len(u.all) })
		return n == 530
	})

	query(func() { u.keys(tcell.NewEventKey(tcell.KeyRune, 'g', 0)) })
	before := len(srv.rawQueries())
	u.refresh("")
	raws := srv.rawQueries()
	if len(raws) <= before {
		t.Fatal("refresh after g issued no request")
	}
	if last := raws[len(raws)-1]; last != "limit=500" {
		t.Fatalf("poll after g asked for %q, want the default window", last)
	}
}

func TestEscapeKeepsWideWindow(t *testing.T) {
	u, srv := newScopeUI(t, seedScopeTasks(530))
	u.maxRows = 600
	query := startScopeApp(t, u, 100, 30)

	u.refresh("")
	scopeAwait(t, "the default window to paint", func() bool {
		var n int
		query(func() { n = len(u.shown) })
		return n == 500
	})
	query(func() { u.keys(tcell.NewEventKey(tcell.KeyRune, 'G', 0)) })
	scopeAwait(t, "the widened window to paint", func() bool {
		var n int
		query(func() { n = len(u.all) })
		return n == 530
	})

	query(func() { u.keys(tcell.NewEventKey(tcell.KeyRune, '/', 0)) })
	query(func() { u.keys(tcell.NewEventKey(tcell.KeyRune, 'f', 0)) })

	before := len(srv.rawQueries())
	query(func() { u.keys(tcell.NewEventKey(tcell.KeyEscape, 0, 0)) })

	scopeAwait(t, "cancelling the search to refetch the wide window", func() bool {
		for _, raw := range srv.rawQueries()[before:] {
			if raw == "limit=600" {
				return true
			}
		}
		return false
	})
	scopeAwait(t, "the whole queue to stay on screen", func() bool {
		var shown int
		query(func() { shown = len(u.shown) })
		return shown == 530
	})
}

func TestJumpToBottomStopsAtTheCap(t *testing.T) {
	u, srv := newScopeUI(t, seedScopeTasks(1400))
	u.maxRows = 1000
	query := startScopeApp(t, u, 100, 30)

	u.refresh("")
	scopeAwait(t, "the default window to paint", func() bool {
		var n int
		query(func() { n = len(u.shown) })
		return n == 500
	})
	query(func() { u.keys(tcell.NewEventKey(tcell.KeyRune, 'G', 0)) })
	scopeAwait(t, "the widened window to paint", func() bool {
		var n int
		query(func() { n = len(u.all) })
		return n == 1000
	})

	before := len(srv.rawQueries())
	for range 5 {
		query(func() { u.keys(tcell.NewEventKey(tcell.KeyRune, 'G', 0)) })
	}
	time.Sleep(50 * time.Millisecond)
	if after := len(srv.rawQueries()); after != before {
		t.Fatalf("G at the capped bottom issued %d more requests", after-before)
	}
}
