package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func pagingTasks(n int) []task {
	all := make([]task, n)
	for i := range all {
		all[i] = task{
			ID:       fmt.Sprintf("task-%04d", i),
			Project:  "proj-a",
			Status:   "pending",
			Priority: 1,
			Body:     fmt.Sprintf("row %d body", i),
		}
	}
	return all
}

// pagingServer answers like the daemon: a cursor whenever the page it
// returned was full, and the total only on the first page of a walk.
func pagingServer(t *testing.T, all []task) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var requests atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		q := r.URL.Query()
		limit, err := strconv.Atoi(q.Get("limit"))
		if err != nil || limit <= 0 {
			http.Error(w, "bad limit", http.StatusBadRequest)
			return
		}
		start := 0
		if after := q.Get("after"); after != "" {
			if start, err = strconv.Atoi(after); err != nil || start > len(all) {
				http.Error(w, "bad cursor", http.StatusBadRequest)
				return
			}
		} else {
			w.Header().Set("X-Total-Count", strconv.Itoa(len(all)))
		}
		end := min(start+limit, len(all))
		if end-start == limit {
			w.Header().Set("X-Next-Cursor", strconv.Itoa(end))
		}
		_ = json.NewEncoder(w).Encode(all[start:end])
	})
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(stats{Pending: len(all)})
	})
	mux.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]string{"proj-a"})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, &requests
}

func pagingModel(t *testing.T, url string) model {
	t.Helper()
	m := newModel(config{refresh: time.Hour}, newClient(url))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return updated.(model)
}

func send(t *testing.T, m model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	next, ok := updated.(model)
	if !ok {
		t.Fatalf("Update returned %T, want model", updated)
	}
	return next, cmd
}

// A poll stays one request; the depth the operator asks for reaches the
// tasks behind the first window.
func TestListWalksOnlyTheRequestedDepth(t *testing.T) {
	all := pagingTasks(1200)
	all[len(all)-1].Body = "last row holds the NEEDLE"
	ts, requests := pagingServer(t, all)
	c := newClient(ts.URL)

	res, err := c.list(listScope{}, "")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(res.tasks) != tasksPageLimit || requests.Load() != 1 {
		t.Fatalf("default list took %d rows in %d requests", len(res.tasks), requests.Load())
	}
	if res.total != 1200 || !res.more {
		t.Fatalf("list reported total=%d more=%v", res.total, res.more)
	}

	res, err = c.list(listScope{pages: tasksMaxPages}, "")
	if err != nil {
		t.Fatalf("deep list failed: %v", err)
	}
	if len(res.tasks) != 1200 || res.more {
		t.Fatalf("deep list took %d rows (more=%v)", len(res.tasks), res.more)
	}
	if !strings.Contains(res.tasks[len(res.tasks)-1].Body, "NEEDLE") {
		t.Fatal("the task behind the first window never arrived")
	}
}

// The daemon sends a cursor for any full page, so a queue of exactly one
// page must not be reported as having rows behind it.
func TestListExactPageIsComplete(t *testing.T) {
	ts, requests := pagingServer(t, pagingTasks(tasksPageLimit))
	res, err := newClient(ts.URL).list(listScope{}, "")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if res.more || requests.Load() != 1 {
		t.Fatalf("more=%v after %d requests", res.more, requests.Load())
	}
}

// A page that came back empty ends the walk, cursor or not.
func TestListStopsOnEmptyPage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Next-Cursor", "always")
		_ = json.NewEncoder(w).Encode([]task{})
	}))
	defer ts.Close()
	res, err := newClient(ts.URL).list(listScope{pages: tasksMaxPages}, "")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(res.tasks) != 0 || res.more {
		t.Fatalf("empty page returned %d rows (more=%v)", len(res.tasks), res.more)
	}
}

// Priority is mutable and part of the cursor ordering, so a repriced task
// can land on two pages. It must be counted once.
func TestListDropsRowsRepeatedAcrossPages(t *testing.T) {
	page := pagingTasks(tasksPageLimit)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after") == "" {
			w.Header().Set("X-Total-Count", strconv.Itoa(len(page)+1))
			w.Header().Set("X-Next-Cursor", "next")
			_ = json.NewEncoder(w).Encode(page)
			return
		}
		_ = json.NewEncoder(w).Encode(append(page[len(page)-1:],
			task{ID: "fresh001", Project: "proj-a", Status: "pending"}))
	}))
	defer ts.Close()

	res, err := newClient(ts.URL).list(listScope{pages: tasksMaxPages}, "")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(res.tasks) != tasksPageLimit+1 {
		t.Fatalf("walk kept %d rows, want %d", len(res.tasks), tasksPageLimit+1)
	}
}

// A walk that breaks halfway hands back the pages it did get, and the
// model applies them before reporting the error.
func TestPartialWalkKeepsItsRows(t *testing.T) {
	page := pagingTasks(tasksPageLimit)
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) > 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-Total-Count", "2000")
		w.Header().Set("X-Next-Cursor", "c1")
		_ = json.NewEncoder(w).Encode(page)
	}))
	defer ts.Close()

	res, err := newClient(ts.URL).list(listScope{pages: tasksMaxPages}, "")
	if err == nil {
		t.Fatal("the broken page must surface an error")
	}
	if len(res.tasks) != tasksPageLimit || !res.changed {
		t.Fatalf("partial walk kept %d rows (changed=%v)", len(res.tasks), res.changed)
	}

	m := pagingModel(t, ts.URL)
	m, _ = send(t, m, pollMsg{seq: m.seq, scope: listScope{pages: tasksMaxPages},
		tasks: res.tasks, changed: res.changed, total: res.total, more: res.more, err: err})
	if len(m.tasks) != tasksPageLimit {
		t.Fatalf("model dropped the partial walk: %d rows", len(m.tasks))
	}
	if m.connected {
		t.Fatal("a broken walk must still read as disconnected")
	}
}

// A walk that is superseded stops on the wire.
func TestSupersededWalkIsCancelled(t *testing.T) {
	page := pagingTasks(tasksPageLimit)
	release := make(chan struct{})
	var served atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after") != "" {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		served.Add(1)
		w.Header().Set("X-Total-Count", "9000")
		w.Header().Set("X-Next-Cursor", "c1")
		_ = json.NewEncoder(w).Encode(page)
	}))
	defer ts.Close()
	defer close(release)

	c := newClient(ts.URL)
	done := make(chan error, 1)
	go func() {
		_, err := c.list(listScope{pages: tasksMaxPages}, "")
		done <- err
	}()
	for served.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	if _, err := c.list(listScope{}, ""); err != nil {
		t.Fatalf("the superseding fetch failed: %v", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the superseded walk should have been cancelled")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the superseded walk kept pulling pages")
	}
}

// G buys another walk and lands on the last task; g puts the live poll
// back, and a paged snapshot costs the daemon only its counters.
func TestEndKeyWalksAndHomeResumes(t *testing.T) {
	ts, _ := pagingServer(t, pagingTasks(530))
	m := pagingModel(t, ts.URL)

	m, poll := send(t, m, tickMsg(time.Now()))
	m, _ = send(t, m, poll().(tea.BatchMsg)[1]().(pollMsg))
	if len(m.tasks) != tasksPageLimit || !m.more || m.total != 530 {
		t.Fatalf("first answer: %d rows more=%v total=%d", len(m.tasks), m.more, m.total)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "500 of 530") {
		t.Fatal("the footer hides the rows it never fetched")
	}

	m, cmd := send(t, m, tea.KeyPressMsg{Text: "G"})
	if cmd == nil || m.pages != 1+tasksMaxPages || m.endPages != m.pages {
		t.Fatalf("G did not buy a walk: pages=%d endPages=%d", m.pages, m.endPages)
	}
	m, _ = send(t, m, cmd().(pollMsg))
	if len(m.tasks) != 530 || m.cursor != len(m.shown)-1 {
		t.Fatalf("G landed on row %d of %d rows", m.cursor, len(m.tasks))
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "530/530") || !strings.Contains(view, "paged (g live)") {
		t.Fatalf("footer after G: %q", lastLine(view))
	}

	m, tickCmd := send(t, m, tickMsg(time.Now()))
	if _, ok := tickCmd().(tea.BatchMsg)[1]().(statsMsg); !ok {
		t.Fatal("a paged snapshot must cost only its counters")
	}
	if m.pages == 1 {
		t.Fatal("the tick dropped the snapshot the operator asked for")
	}

	m, cmd = send(t, m, tea.KeyPressMsg{Text: "g"})
	if cmd == nil || m.pages != 1 || m.etag != "" {
		t.Fatalf("g must resume the live poll: pages=%d etag=%q", m.pages, m.etag)
	}
}

// Ordinary cursor movement must not throw away pages already loaded.
func TestScrollingUpKeepsTheSnapshot(t *testing.T) {
	ts, _ := pagingServer(t, pagingTasks(530))
	for _, key := range []tea.KeyPressMsg{{Text: "k"}, {Code: tea.KeyUp}, {Code: tea.KeyPgUp}} {
		m := pagingModel(t, ts.URL)
		m.tasks = pagingTasks(20)
		m.rebuild()
		m.pages, m.etag, m.cursor = 11, `"v1"`, 1
		m, cmd := send(t, m, key)
		if m.pages != 11 || m.etag != `"v1"` {
			t.Fatalf("%v dropped the snapshot: pages=%d etag=%q", key, m.pages, m.etag)
		}
		if cmd != nil {
			t.Fatalf("%v polled behind the operator", key)
		}
	}
}

// Switching project starts that project live rather than at the depth the
// last one was read at.
func TestProjectSwitchStartsLive(t *testing.T) {
	ts, _ := pagingServer(t, pagingTasks(10))
	m := pagingModel(t, ts.URL)
	m.projects = []string{"alpha", "beta"}
	m.pages, m.etag = 11, `"v1"`
	m, cmd := send(t, m, tea.KeyPressMsg{Text: "p"})
	if m.pages != 1 || m.etag != "" || cmd == nil {
		t.Fatalf("new project inherited the old walk: pages=%d etag=%q", m.pages, m.etag)
	}
}

// A narrow terminal loses key hints, not the count of rows left out.
func TestNarrowFooterKeepsTheDisclosure(t *testing.T) {
	ts, _ := pagingServer(t, pagingTasks(530))
	m := newModel(config{refresh: time.Hour}, newClient(ts.URL))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	m = updated.(model)
	m.tasks = pagingTasks(tasksPageLimit)
	m.rebuild()
	m.total, m.more = 530, true
	if line := lastLine(ansi.Strip(m.View().Content)); !strings.Contains(line, "500 of 530") {
		t.Fatalf("narrow footer dropped the disclosure: %q", line)
	}
}

func lastLine(view string) string {
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	return lines[len(lines)-1]
}
