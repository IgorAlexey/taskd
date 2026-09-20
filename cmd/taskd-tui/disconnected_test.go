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
)

func TestTUIDisconnectedState(t *testing.T) {
	var online atomic.Bool

	tasks := []task{
		{ID: "t1", Project: "proj-recovered", Status: "pending", Body: "recovered task content"},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(tasks)
	})
	mux.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]string{"proj-recovered"})
	})
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]int{
			"pending": 1,
			"leased":  0,
			"done":    0,
			"total":   1,
		})
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !online.Load() {
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, _ := hj.Hijack()
				conn.Close()
				return
			}
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	defer srv.Close()

	u := newUI(srv.URL, "", false)
	u.pollInterval = 20 * time.Millisecond

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(80, 25)
	u.app.SetScreen(sim)
	u.app.SetRoot(u.pages, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go u.poll(ctx)

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

	eventually(t, func() bool {
		var bodyText, statusText string
		query(func() {
			bodyText = u.body.GetText(true)
			statusText = u.status.GetText(true)
		})

		if strings.Contains(bodyText, "No tasks yet") || strings.Contains(bodyText, "Press 'n'") {
			return false
		}
		if !strings.Contains(bodyText, "Disconnected") {
			return false
		}
		if !strings.Contains(statusText, "disconnected") {
			return false
		}
		if strings.Contains(statusText, "pending 0") {
			return false
		}
		return u.disconnected.Load()
	})

	online.Store(true)

	eventually(t, func() bool {
		var bodyText, statusText string
		query(func() {
			bodyText = u.body.GetText(true)
			statusText = u.status.GetText(true)
		})

		if strings.Contains(bodyText, "Disconnected") {
			return false
		}
		if !strings.Contains(bodyText, "recovered task content") {
			return false
		}
		if strings.Contains(statusText, "disconnected") {
			return false
		}
		if !strings.Contains(statusText, "pending 1") {
			return false
		}
		return !u.disconnected.Load()
	})

	online.Store(false)

	eventually(t, func() bool {
		var bodyText, statusText string
		query(func() {
			bodyText = u.body.GetText(true)
			statusText = u.status.GetText(true)
		})
		if !strings.Contains(bodyText, "Disconnected") {
			return false
		}
		if !strings.Contains(bodyText, "recovered task content") {
			return false
		}
		if !strings.Contains(statusText, "disconnected") {
			return false
		}
		return u.disconnected.Load()
	})

	query(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, '/', 0))
		u.keys(tcell.NewEventKey(tcell.KeyRune, 'r', 0))
		u.keys(tcell.NewEventKey(tcell.KeyEscape, 0, 0))
	})

	var bodyAfterKeys, statusAfterKeys string
	query(func() {
		bodyAfterKeys = u.body.GetText(true)
		statusAfterKeys = u.status.GetText(true)
	})
	if !u.disconnected.Load() {
		t.Fatal("local UI interaction cleared disconnected state")
	}
	if !strings.Contains(bodyAfterKeys, "Disconnected") {
		t.Fatalf("local UI interaction clobbered disconnected body guidance: %q", bodyAfterKeys)
	}
	if !strings.Contains(statusAfterKeys, "disconnected") {
		t.Fatalf("local UI interaction clobbered disconnected status bar: %q", statusAfterKeys)
	}
}
