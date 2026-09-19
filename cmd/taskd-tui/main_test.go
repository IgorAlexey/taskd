package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func stub(t *testing.T) (*ui, *[]task) {
	t.Helper()
	tasks := []task{
		{ID: "aaaaaaa1", Project: "proj-b", Status: "pending", Priority: 2, Body: "first task\n\nWhy: a"},
		{ID: "bbbbbbb2", Project: "proj-a", Status: "leased", Worker: "w1", LeaseExpires: 1 << 40, Priority: 1, Body: "second"},
		{ID: "ccccccc3", Project: "proj-b", Status: "done", Body: "third", Primitives: json.RawMessage(`{"commit":"x"}`)},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(tasks) })
	mux.HandleFunc("PATCH /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		var p struct{ Priority int }
		json.NewDecoder(r.Body).Decode(&p)
		for i := range tasks {
			if tasks[i].ID == r.PathValue("id") {
				tasks[i].Priority = p.Priority
			}
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("DELETE /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "task is leased", http.StatusConflict)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u := &ui{url: srv.URL, app: tview.NewApplication()}
	u.table = tview.NewTable().SetFixed(1, 0).SetSelectable(true, false)
	u.body = tview.NewTextView()
	u.status = tview.NewTextView()
	return u, &tasks
}

func TestRenderAndKeys(t *testing.T) {
	u, tasks := stub(t)
	ts, err := u.fetch()
	if err != nil || len(ts) != 3 {
		t.Fatalf("fetch: %v %d", err, len(ts))
	}
	u.render(ts)
	if got := u.table.GetCell(0, 2).Text; got != "PROJECT" {
		t.Fatalf("header cell = %q", got)
	}
	if got := u.table.GetCell(1, 2).Text; got != "proj-b" {
		t.Fatalf("project cell = %q", got)
	}
	if got := u.table.GetCell(1, 6).Text; got != "first task" {
		t.Fatalf("title cell = %q", got)
	}
	if got := u.table.GetCell(2, 3).Text; !strings.HasSuffix(got, "s") || got == "expired" {
		t.Fatalf("lease cell = %q", got)
	}
	if !strings.Contains(u.status.GetText(true), "project all") {
		t.Fatalf("status line project = %q", u.status.GetText(true))
	}
	if !strings.Contains(u.status.GetText(true), "pending 1  leased 1  done 1") {
		t.Fatalf("status = %q", u.status.GetText(true))
	}

	u.keys(tcell.NewEventKey(tcell.KeyRune, 'p', 0))
	if u.project != "proj-a" || len(u.shown) != 1 || u.shown[0].ID != "bbbbbbb2" {
		t.Fatalf("filter project proj-a: project=%q shown=%+v", u.project, u.shown)
	}
	if !strings.Contains(u.status.GetText(true), "project proj-a") {
		t.Fatalf("status line project = %q", u.status.GetText(true))
	}

	u.keys(tcell.NewEventKey(tcell.KeyRune, 'p', 0))
	if u.project != "proj-b" || len(u.shown) != 2 {
		t.Fatalf("filter project proj-b: project=%q shown=%+v", u.project, u.shown)
	}
	if !strings.Contains(u.status.GetText(true), "project proj-b") {
		t.Fatalf("status line project = %q", u.status.GetText(true))
	}

	u.keys(tcell.NewEventKey(tcell.KeyRune, 'p', 0))
	if u.project != "" || len(u.shown) != 3 {
		t.Fatalf("filter project all: project=%q shown=%+v", u.project, u.shown)
	}
	if !strings.Contains(u.status.GetText(true), "project all") {
		t.Fatalf("status line project = %q", u.status.GetText(true))
	}

	u.keys(tcell.NewEventKey(tcell.KeyRune, '3', 0))
	if len(u.shown) != 1 || u.shown[0].Status != "done" {
		t.Fatalf("filter done: %+v", u.shown)
	}
	if !strings.Contains(u.body.GetText(true), `result: {"commit":"x"}`) {
		t.Fatalf("body = %q", u.body.GetText(true))
	}
	u.keys(tcell.NewEventKey(tcell.KeyRune, '0', 0))
	u.table.Select(2, 0)
	sel, _ := u.selected()
	if err := u.call("PATCH", "/tasks/"+sel.ID, map[string]int{"priority": 7}); err != nil {
		t.Fatal(err)
	}
	if (*tasks)[1].Priority != 7 {
		t.Fatalf("priority not patched: %+v", (*tasks)[1])
	}
	ts, _ = u.fetch()
	u.render(ts)
	if sel, _ = u.selected(); sel.ID != "bbbbbbb2" {
		t.Fatalf("cursor moved to %s", sel.ID)
	}
	err = u.call("DELETE", "/tasks/"+sel.ID, nil)
	if err == nil || !strings.Contains(err.Error(), "409 task is leased") {
		t.Fatalf("delete err = %v", err)
	}
}
