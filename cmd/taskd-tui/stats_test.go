package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func TestFetchStatsDirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("project")
		if p == "p1" {
			json.NewEncoder(w).Encode(map[string]int{
				"pending": 555,
				"leased":  11,
				"done":    22,
				"total":   588,
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]int{
			"pending": 888,
			"leased":  33,
			"done":    44,
			"total":   965,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	u := newUI(srv.URL, "", false, "")
	st, err := u.fetchStats("")
	if err != nil {
		t.Fatalf("fetchStats failed: %v", err)
	}
	if st.Pending != 888 || st.Leased != 33 || st.Done != 44 {
		t.Fatalf("unscoped stats expected 888/33/44, got %d/%d/%d", st.Pending, st.Leased, st.Done)
	}

	stP1, err := u.fetchStats("p1")
	if err != nil {
		t.Fatalf("fetchStats with project failed: %v", err)
	}
	if stP1.Pending != 555 || stP1.Leased != 11 || stP1.Done != 22 {
		t.Fatalf("project stats expected 555/11/22, got %d/%d/%d", stP1.Pending, stP1.Leased, stP1.Done)
	}
}

func TestStatsBacklogExceedsPagination(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		ts := make([]task, 500)
		for i := range 500 {
			ts[i] = task{
				ID:      "task",
				Project: "proj",
				Status:  "pending",
				Body:    "body",
			}
		}
		json.NewEncoder(w).Encode(ts)
	})
	mux.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]string{"proj"})
	})
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]int{
			"pending": 600,
			"leased":  25,
			"done":    50,
			"total":   675,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	u := newUI(srv.URL, "", false, "")
	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(120, 25)
	u.width = 120
	u.app.SetScreen(sim)
	u.app.SetRoot(u.pages, true)

	done := make(chan struct{})
	go func() {
		u.app.Run()
		close(done)
	}()
	defer func() {
		u.app.Stop()
		<-done
	}()

	query := func(fn func()) {
		ch := make(chan struct{})
		u.app.QueueUpdate(func() {
			fn()
			close(ch)
		})
		<-ch
	}

	u.refresh("")

	for range 50 {
		var pending, leased, doneCount int
		var line string
		query(func() {
			pending = u.stats.Pending
			leased = u.stats.Leased
			doneCount = u.stats.Done
			line = strings.SplitN(u.status.GetText(true), "\n", 2)[0]
		})
		if pending == 600 && leased == 25 && doneCount == 50 && strings.Contains(line, "pending 600  leased 25") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	var pending, leased, doneCount int
	query(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, 'l', 0))
		pending, leased, doneCount = u.stats.Pending, u.stats.Leased, u.stats.Done
	})
	if pending != 600 || leased != 25 || doneCount != 50 {
		t.Fatalf("keystroke 'l' vaporized stats: %d/%d/%d", pending, leased, doneCount)
	}

	query(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, '/', 0))
		u.keys(tcell.NewEventKey(tcell.KeyRune, 'a', 0))
		pending, leased, doneCount = u.stats.Pending, u.stats.Leased, u.stats.Done
	})
	if pending != 600 || leased != 25 || doneCount != 50 {
		t.Fatalf("search keystrokes vaporized stats: %d/%d/%d", pending, leased, doneCount)
	}

	query(func() {
		u.keys(tcell.NewEventKey(tcell.KeyEscape, 0, 0))
		u.keys(tcell.NewEventKey(tcell.KeyRune, '0', 0))
		pending, leased, doneCount = u.stats.Pending, u.stats.Leased, u.stats.Done
	})
	if pending != 600 || leased != 25 || doneCount != 50 {
		t.Fatalf("keystroke '0' vaporized stats: %d/%d/%d", pending, leased, doneCount)
	}
}

func TestStatsFallbackOnServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]task{
			{ID: "t1", Project: "proj", Status: "pending", Body: "task 1"},
			{ID: "t2", Project: "proj", Status: "leased", Body: "task 2"},
		})
	})
	mux.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]string{"proj"})
	})
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "stats unavailable", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	u := newUI(srv.URL, "", false, "")
	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(120, 25)
	u.width = 120
	u.app.SetScreen(sim)
	u.app.SetRoot(u.pages, true)

	done := make(chan struct{})
	go func() {
		u.app.Run()
		close(done)
	}()
	defer func() {
		u.app.Stop()
		<-done
	}()

	query := func(fn func()) {
		ch := make(chan struct{})
		u.app.QueueUpdate(func() {
			fn()
			close(ch)
		})
		<-ch
	}

	u.refresh("")

	for range 50 {
		var pending, leased, doneCount int
		query(func() {
			pending = u.stats.Pending
			leased = u.stats.Leased
			doneCount = u.stats.Done
		})
		if pending == 1 && leased == 1 && doneCount == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	var pending, leased, doneCount int
	query(func() {
		pending = u.stats.Pending
		leased = u.stats.Leased
		doneCount = u.stats.Done
	})
	t.Fatalf("expected fallback in-memory stats (1/1/0), got %d/%d/%d", pending, leased, doneCount)
}
