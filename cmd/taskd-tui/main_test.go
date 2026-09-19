package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func stub(t *testing.T) (*ui, *[]task, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	tasks := []task{
		{ID: "aaaaaaa1", Project: "proj-b", Status: "pending", Priority: 2, Body: "first task\n\nWhy: a"},
		{ID: "bbbbbbb2", Project: "proj-a", Status: "leased", Worker: "w1", LeaseExpires: 1 << 40, Priority: 1, Body: "second"},
		{ID: "ccccccc3", Project: "proj-b", Status: "done", Body: "third", Primitives: json.RawMessage(`{"commit":"x"}`)},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		json.NewEncoder(w).Encode(tasks)
	})
	mux.HandleFunc("PATCH /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		var p struct{ Priority int }
		json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		defer mu.Unlock()
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
	mux.HandleFunc("POST /tasks", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Body     string `json:"body"`
			Priority int    `json:"priority"`
			Project  string `json:"project"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		tasks = append(tasks, task{
			ID:       "ddddddd4",
			Project:  req.Project,
			Status:   "pending",
			Priority: req.Priority,
			Body:     req.Body,
		})
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": "ddddddd4"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u := &ui{url: srv.URL, app: tview.NewApplication()}
	u.table = tview.NewTable().SetFixed(1, 0).SetSelectable(true, false)
	u.body = tview.NewTextView()
	u.status = tview.NewTextView()
	u.root = u.table
	return u, &tasks, &mu
}

func TestRenderAndKeys(t *testing.T) {
	u, tasks, _ := stub(t)
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

func TestCreateForm(t *testing.T) {
	u, tasks, mu := stub(t)
	ts, err := u.fetch()
	if err != nil {
		t.Fatal(err)
	}
	u.render(ts)

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	u.app.SetScreen(sim)
	done := make(chan struct{})
	go func() {
		u.app.Run()
		close(done)
	}()
	defer func() {
		u.app.Stop()
		<-done
	}()

	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, 'n', 0))
	})
	var form *tview.Form
	u.app.QueueUpdateDraw(func() {
		form = u.form
	})
	if form == nil {
		t.Fatal("expected form to be open after pressing n")
	}
	if got := form.GetFormItem(0).(*tview.InputField).GetText(); got != "taskd" {
		t.Fatalf("default project = %q, want taskd", got)
	}
	if got := form.GetFormItem(1).(*tview.InputField).GetText(); got != "0" {
		t.Fatalf("default priority = %q, want 0", got)
	}
	if got := form.GetFormItem(2).(*tview.InputField).GetText(); got != "" {
		t.Fatalf("default body = %q, want empty", got)
	}
	u.app.QueueUpdateDraw(func() {
		u.form.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), nil)
	})
	u.app.QueueUpdateDraw(func() {
		form = u.form
	})
	if form != nil {
		t.Fatal("expected form to be closed after cancel")
	}
	mu.Lock()
	count := len(*tasks)
	mu.Unlock()
	if count != 3 {
		t.Fatalf("cancel created task: %d", count)
	}

	u.app.QueueUpdateDraw(func() {
		u.project = "proj-a"
		u.keys(tcell.NewEventKey(tcell.KeyRune, 'n', 0))
	})
	u.app.QueueUpdateDraw(func() {
		form = u.form
	})
	if form == nil {
		t.Fatal("expected form to be open after pressing n with project filter")
	}
	if got := form.GetFormItem(0).(*tview.InputField).GetText(); got != "proj-a" {
		t.Fatalf("pre-filled project = %q, want proj-a", got)
	}
	u.app.QueueUpdateDraw(func() {
		u.form.GetFormItem(1).(*tview.InputField).SetText("42")
		u.form.GetFormItem(2).(*tview.InputField).SetText("brand new task")
		u.form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	for range 50 {
		mu.Lock()
		count = len(*tasks)
		mu.Unlock()
		if count == 4 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if count != 4 {
		t.Fatalf("task was not created, count = %d", count)
	}
	mu.Lock()
	created := (*tasks)[3]
	mu.Unlock()
	if created.Project != "proj-a" || created.Priority != 42 || created.Body != "brand new task" {
		t.Fatalf("created task mismatch: %+v", created)
	}
	u.app.QueueUpdateDraw(func() {
		form = u.form
	})
	if form != nil {
		t.Fatal("expected form to be closed after submit")
	}
}
