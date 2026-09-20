package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// projectStub serves /tasks scoped to ?project= and runs a ui against it on
// a simulation screen. When gated, /tasks blocks until the returned release
// is called, giving a test a deterministic in-flight window.
func projectStub(t *testing.T, gated bool) (*ui, func()) {
	t.Helper()
	all := []task{
		{ID: "aaaaaaa1", Project: "proj-a", Status: "pending"},
		{ID: "bbbbbbb2", Project: "proj-b", Status: "pending"},
	}
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	if !gated {
		release()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		<-gate
		project := r.URL.Query().Get("project")
		out := []task{}
		for _, tk := range all {
			if project == "" || tk.Project == project {
				out = append(out, tk)
			}
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]string{"proj-a", "proj-b", "proj-empty"})
	})
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]int{"pending": 1, "leased": 0, "done": 0, "total": 1})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	// A failed assertion must not leave srv.Close waiting on a held request.
	t.Cleanup(release)

	u := newUI(srv.URL, "proj-a", false)
	u.projects = []string{"proj-a", "proj-b", "proj-empty"}
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
	return u, release
}

// cycleProject presses p on the event goroutine, as a real screen would.
func cycleProject(u *ui) {
	pressed := make(chan struct{})
	u.app.QueueUpdate(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, 'p', 0))
		close(pressed)
	})
	<-pressed
}

// bodyText reads the detail pane from the event goroutine.
func bodyText(u *ui) string {
	var body string
	queried := make(chan struct{})
	u.app.QueueUpdate(func() {
		body = u.shownBody
		close(queried)
	})
	<-queried
	return body
}

// waitShown fails unless the queue settles on exactly want.
func waitShown(t *testing.T, u *ui, want ...string) {
	t.Helper()
	var ids []string
	eventually(t, func() bool {
		ids = nil
		queried := make(chan struct{})
		u.app.QueueUpdate(func() {
			for _, tk := range u.shown {
				ids = append(ids, tk.ID)
			}
			close(queried)
		})
		<-queried
		return slices.Equal(ids, want)
	})
}
