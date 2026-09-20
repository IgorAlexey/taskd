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

	tea "charm.land/bubbletea/v2"
)

// The term and the status filter go down together, so one WHERE clause
// answers both and a page of matching done tasks cannot hide the live one.
func TestListSendsQueryAndStatus(t *testing.T) {
	var rawQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode([]task{{ID: "needle01", Status: "pending"}})
	}))
	defer ts.Close()

	res, err := newClient(ts.URL).list(listScope{
		filter: listFilter{status: "pending", query: "needle"},
	}, "")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if rawQuery != "limit=500&q=needle&status=pending" {
		t.Fatalf("raw query = %q, want the term and the status", rawQuery)
	}
	if len(res.tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(res.tasks))
	}
}

// searchStub answers q the way the daemon does, over rows the client never
// prefetched, and records every question it was asked.
func searchStub(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	window := []task{{ID: "local001", Project: "proj-a", Status: "pending", Body: "prefetched window"}}
	matched := []task{
		{ID: "remote01", Project: "proj-b", Status: "pending", Body: "lives past the window"},
		{ID: "remote02", Project: "proj-b", Status: "pending", Body: "also past it"},
	}
	var mu sync.Mutex
	var asked []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.RawQuery)
		mu.Unlock()
		out := window
		switch q := r.URL.Query().Get("q"); {
		case q == "":
		case q == "proj-b":
			out = matched
		default:
			out = nil
		}
		w.Header().Set("X-Total-Count", fmt.Sprint(len(out)))
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(stats{Pending: 1})
	})
	mux.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]string{"proj-a", "proj-b"})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), asked...)
	}
}

// Typing waits out the debounce and then asks the daemon, so a task past
// the prefetched window is found, and rows whose only match is the project
// reach the table.
func TestSearchTypingAsksTheDaemonOnce(t *testing.T) {
	ts, asked := searchStub(t)
	m := pagingModel(t, ts.URL)
	m, _ = send(t, m, tea.KeyPressMsg{Text: "/"})

	var last tea.Cmd
	for _, r := range "proj-b" {
		m, last = send(t, m, tea.KeyPressMsg{Text: string(r), Code: r})
	}
	if m.query != "proj-b" {
		t.Fatalf("query = %q", m.query)
	}
	if got := len(asked()); got != 0 {
		t.Fatalf("typing asked the daemon %d times before the pause", got)
	}
	tick, ok := last().(searchMsg)
	if !ok {
		t.Fatalf("typing produced %T, want searchMsg", last())
	}

	m, cmd := send(t, m, tick)
	if cmd == nil {
		t.Fatal("the pause must ask the daemon")
	}
	m, _ = send(t, m, cmd().(pollMsg))
	if len(m.shown) != 2 {
		t.Fatalf("the daemon's matches never reached the table: %d shown", len(m.shown))
	}
	got := asked()
	if len(got) != 1 || !strings.Contains(got[0], "q=proj-b") {
		t.Fatalf("questions asked: %v", got)
	}
}

// A keystroke after the tick was armed invalidates it, so only the pause
// reaches the daemon.
func TestSearchDebounceDropsStaleGenerations(t *testing.T) {
	ts, asked := searchStub(t)
	m := pagingModel(t, ts.URL)
	m, _ = send(t, m, tea.KeyPressMsg{Text: "/"})
	m, first := send(t, m, tea.KeyPressMsg{Text: "n", Code: 'n'})
	m, _ = send(t, m, tea.KeyPressMsg{Text: "e", Code: 'e'})

	m, cmd := send(t, m, first().(searchMsg))
	if cmd != nil {
		t.Fatal("a superseded keystroke must not reach the daemon")
	}
	if got := len(asked()); got != 0 {
		t.Fatalf("daemon asked %d times for a stale term", got)
	}
}

// Escape and Enter are decisions, not keystrokes: they drop the pending
// tick, and they ask the daemon exactly when the question changed.
func TestSearchCancelDropsTheTickAndAsksOnlyOnChange(t *testing.T) {
	ts, asked := searchStub(t)
	settle := func() model {
		m := pagingModel(t, ts.URL)
		m, poll := send(t, m, tickMsg(time.Now()))
		m, _ = send(t, m, poll().(tea.BatchMsg)[1]().(pollMsg))
		return m
	}

	m := settle()
	m, _ = send(t, m, tea.KeyPressMsg{Text: "/"})
	m, armed := send(t, m, tea.KeyPressMsg{Text: "x", Code: 'x'})
	stale := armed().(searchMsg)
	before := len(asked())
	m, cmd := send(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Fatal("Escape re-asked a question the daemon already answered")
	}
	if _, dropped := send(t, m, stale); dropped != nil {
		t.Fatal("Escape left the debounce armed")
	}
	if len(asked()) != before {
		t.Fatalf("Escape asked %d extra times", len(asked())-before)
	}

	m = settle()
	m, _ = send(t, m, tea.KeyPressMsg{Text: "/"})
	m, armed = send(t, m, tea.KeyPressMsg{Text: "n", Code: 'n'})
	stale = armed().(searchMsg)
	before = len(asked())
	m, cmd = send(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter must flush the pending term")
	}
	m, _ = send(t, m, cmd().(pollMsg))
	if len(asked()) != before+1 {
		t.Fatalf("Enter asked %d times, want one", len(asked())-before)
	}
	if _, dropped := send(t, m, stale); dropped != nil {
		t.Fatal("Enter left the debounce armed")
	}
	m.mode = modeSearch
	if _, again := send(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}); again != nil {
		t.Fatal("Enter on a term already answered must not ask again")
	}
}

// A reply for a question the operator has since changed is dropped even
// when it is the newest poll on the wire.
func TestReplyForAChangedQuestionIsDropped(t *testing.T) {
	ts, _ := searchStub(t)
	m := pagingModel(t, ts.URL)
	m.project = "beta"
	m, _ = send(t, m, pollMsg{
		seq:     m.seq,
		scope:   listScope{filter: listFilter{project: "alpha"}},
		tasks:   pagingTasks(3),
		changed: true,
		total:   3,
	})
	if len(m.tasks) != 0 {
		t.Fatalf("rows for another project landed: %d", len(m.tasks))
	}
	if m.polling {
		t.Fatal("the reply still owns the in-flight flag it clears")
	}
}
