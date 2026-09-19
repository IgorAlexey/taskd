package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	deleteMu    sync.Mutex
	deleteCalls []string
)

func stub(t *testing.T) (*ui, *[]task, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	deleteMu.Lock()
	deleteCalls = nil
	deleteMu.Unlock()
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
		var p struct {
			Priority *int    `json:"priority"`
			Body     *string `json:"body"`
		}
		json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		defer mu.Unlock()
		for i := range tasks {
			if tasks[i].ID == r.PathValue("id") {
				if p.Priority != nil {
					tasks[i].Priority = *p.Priority
				}
				if p.Body != nil {
					tasks[i].Body = *p.Body
				}
			}
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("DELETE /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		mu.Lock()
		defer mu.Unlock()
		deleteMu.Lock()
		deleteCalls = append(deleteCalls, r.URL.RequestURI())
		deleteMu.Unlock()
		for i, tk := range tasks {
			if tk.ID == id {
				if tk.Status == "leased" {
					http.Error(w, "task is leased", http.StatusConflict)
					return
				}
				force := r.URL.Query().Get("force")
				if tk.Status == "done" && force != "true" && force != "1" {
					http.Error(w, "task is done", http.StatusConflict)
					return
				}
				tasks = append(tasks[:i], tasks[i+1:]...)
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		http.Error(w, "task not found", http.StatusNotFound)
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
	u := newUI(srv.URL, "")
	return u, &tasks, &mu
}

func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	for range 50 {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
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
	if got := form.GetFormItem(2).(*tview.TextArea).GetText(); got != "" {
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
		u.form.GetFormItem(2).(*tview.TextArea).SetText("brand new task\n\nWhy: multi-line test\nDone when: ok", true)
		u.form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(*tasks) == 4
	})
	mu.Lock()
	created := (*tasks)[3]
	mu.Unlock()
	if created.Project != "proj-a" || created.Priority != 42 || created.Body != "brand new task\n\nWhy: multi-line test\nDone when: ok" {
		t.Fatalf("created task mismatch: %+v", created)
	}
	u.app.QueueUpdateDraw(func() {
		form = u.form
	})
	if form != nil {
		t.Fatal("expected form to be closed after submit")
	}
}

func TestMouseSupport(t *testing.T) {
	u, _, _ := stub(t)
	ts, err := u.fetch()
	if err != nil {
		t.Fatal(err)
	}
	u.render(ts)

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(80, 25)
	u.app.SetScreen(sim)
	u.app.SetRoot(u.root, true)

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
		var sel task
		var ok bool
		query(func() { sel, ok = u.selected() })
		return ok && sel.ID == "aaaaaaa1"
	})

	// Click row 2 (y=2 is row for bbbbbbb2)
	sim.InjectMouse(5, 2, tcell.ButtonPrimary, 0)
	sim.InjectMouse(5, 2, tcell.ButtonNone, 0)

	eventually(t, func() bool {
		var sel task
		var body string
		query(func() {
			sel, _ = u.selected()
			body = u.body.GetText(true)
		})
		return sel.ID == "bbbbbbb2" && strings.Contains(body, "second")
	})

	// Test wheel scrolling on body pane
	ch := make(chan struct{})
	u.app.QueueUpdateDraw(func() {
		u.body.SetText(strings.Repeat("line\n", 50)).ScrollToBeginning()
		close(ch)
	})
	<-ch

	sim.InjectMouse(5, 18, tcell.WheelDown, 0)
	eventually(t, func() bool {
		var row int
		query(func() { row, _ = u.body.GetScrollOffset() })
		return row > 0
	})

	sim.InjectMouse(5, 18, tcell.WheelUp, 0)
	eventually(t, func() bool {
		var row int
		query(func() { row, _ = u.body.GetScrollOffset() })
		return row == 0
	})
}

func TestPriorityClamp(t *testing.T) {
	var patches int
	var mu sync.Mutex
	tasks := []task{
		{ID: "t1", Project: "p1", Status: "pending", Priority: 1, Body: "task 1"},
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
		if p.Priority < 0 {
			t.Errorf("server received negative priority: %d", p.Priority)
		}
		mu.Lock()
		patches++
		tasks[0].Priority = p.Priority
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	u := newUI(srv.URL, "")
	ts, err := u.fetch()
	if err != nil || len(ts) != 1 {
		t.Fatalf("fetch: %v %d", err, len(ts))
	}
	u.render(ts)
	u.table.Select(1, 0)

	u.keys(tcell.NewEventKey(tcell.KeyRune, '-', 0))
	eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return patches == 1 && tasks[0].Priority == 0
	})

	ts, _ = u.fetch()
	u.render(ts)
	u.table.Select(1, 0)

	u.keys(tcell.NewEventKey(tcell.KeyRune, '-', 0))
	u.keys(tcell.NewEventKey(tcell.KeyRune, '+', 0))

	eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return patches == 2 && tasks[0].Priority == 1
	})
}

func TestBodyFocusAndScroll(t *testing.T) {
	u, _, _ := stub(t)
	ts, err := u.fetch()
	if err != nil {
		t.Fatal(err)
	}
	u.render(ts)

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(80, 25)
	u.app.SetScreen(sim)
	u.app.SetRoot(u.root, true)

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
		var has bool
		query(func() { has = u.table.HasFocus() })
		return has
	})

	sim.InjectKey(tcell.KeyTab, 0, 0)
	eventually(t, func() bool {
		var has bool
		query(func() { has = u.body.HasFocus() })
		return has
	})

	ch := make(chan struct{})
	u.app.QueueUpdateDraw(func() {
		u.body.SetText(strings.Repeat("line\n", 50)).ScrollToBeginning()
		close(ch)
	})
	<-ch

	sim.InjectKey(tcell.KeyRune, 'j', 0)
	sim.InjectKey(tcell.KeyRune, 'j', 0)
	eventually(t, func() bool {
		var row int
		query(func() { row, _ = u.body.GetScrollOffset() })
		return row > 0
	})
	sim.InjectKey(tcell.KeyRune, 'k', 0)
	sim.InjectKey(tcell.KeyRune, 'k', 0)
	eventually(t, func() bool {
		var row int
		query(func() { row, _ = u.body.GetScrollOffset() })
		return row == 0
	})

	sim.InjectKey(tcell.KeyRune, 'j', 0)
	sim.InjectKey(tcell.KeyRune, 'j', 0)
	eventually(t, func() bool {
		var row int
		query(func() { row, _ = u.body.GetScrollOffset() })
		return row > 0
	})
	sim.InjectKey(tcell.KeyEscape, 0, 0)
	eventually(t, func() bool {
		var has bool
		query(func() { has = u.table.HasFocus() })
		return has
	})

	sim.InjectKey(tcell.KeyTab, 0, 0)
	eventually(t, func() bool {
		var has bool
		query(func() { has = u.body.HasFocus() })
		return has
	})

	sim.InjectKey(tcell.KeyTab, 0, 0)
	eventually(t, func() bool {
		var has bool
		query(func() { has = u.table.HasFocus() })
		return has
	})
}

func TestDeleteConfirm(t *testing.T) {
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
	sim.SetSize(80, 25)
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

	query := func(fn func()) {
		ch := make(chan struct{})
		u.app.QueueUpdate(func() {
			fn()
			close(ch)
		})
		<-ch
	}

	screenText := func() string {
		cells, _, _ := sim.GetContents()
		var sb strings.Builder
		for _, c := range cells {
			for _, r := range c.Runes {
				sb.WriteRune(r)
			}
		}
		return sb.String()
	}

	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, 'D', 0))
	})

	var modal *tview.Modal
	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal != nil
	})

	eventually(t, func() bool {
		s := screenText()
		return strings.Contains(s, "aaaaaaa1") && strings.Contains(s, "first task")
	})

	u.app.QueueUpdateDraw(func() {
		u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), nil)
	})

	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal == nil
	})

	mu.Lock()
	count := len(*tasks)
	mu.Unlock()
	if count != 3 {
		t.Fatalf("expected 3 tasks after Esc, got %d", count)
	}

	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, '3', 0))
	})

	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, 'D', 0))
	})

	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal != nil
	})

	eventually(t, func() bool {
		s := screenText()
		return strings.Contains(s, "ccccccc3") && strings.Contains(s, "third")
	})

	u.app.QueueUpdateDraw(func() {
		u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal == nil
	})

	eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, tk := range *tasks {
			if tk.ID == "ccccccc3" {
				return false
			}
		}
		return true
	})

	deleteMu.Lock()
	var lastCall string
	if len(deleteCalls) > 0 {
		lastCall = deleteCalls[len(deleteCalls)-1]
	}
	deleteMu.Unlock()
	if !strings.Contains(lastCall, "force=true") {
		t.Fatalf("expected force=true in delete call for done task, got %q", lastCall)
	}

	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, '0', 0))
	})

	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, 'D', 0))
	})

	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal != nil
	})

	u.app.QueueUpdateDraw(func() {
		u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal == nil
	})

	eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, tk := range *tasks {
			if tk.ID == "aaaaaaa1" {
				return false
			}
		}
		return true
	})

	deleteMu.Lock()
	if len(deleteCalls) > 0 {
		lastCall = deleteCalls[len(deleteCalls)-1]
	}
	deleteMu.Unlock()
	if strings.Contains(lastCall, "force=true") {
		t.Fatalf("did not expect force=true for pending task, got %q", lastCall)
	}
}

func TestEditForm(t *testing.T) {
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
		u.table.Select(1, 0)
		u.keys(tcell.NewEventKey(tcell.KeyRune, 'e', 0))
	})
	var form *tview.Form
	u.app.QueueUpdateDraw(func() {
		form = u.form
	})
	if form == nil {
		t.Fatal("expected form to be open after pressing e")
	}
	bodyItem, ok := form.GetFormItem(0).(*tview.TextArea)
	if !ok {
		t.Fatalf("expected body item to be *tview.TextArea, got %T", form.GetFormItem(0))
	}
	if got := bodyItem.GetText(); got != "first task\n\nWhy: a" {
		t.Fatalf("pre-filled body = %q, want first task\\n\\nWhy: a", got)
	}
	priItem, ok := form.GetFormItem(1).(*tview.InputField)
	if !ok {
		t.Fatalf("expected pri item to be *tview.InputField, got %T", form.GetFormItem(1))
	}
	if got := priItem.GetText(); got != "2" {
		t.Fatalf("pre-filled priority = %q, want 2", got)
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
	bodyUnchanged := (*tasks)[0].Body
	priUnchanged := (*tasks)[0].Priority
	mu.Unlock()
	if bodyUnchanged != "first task\n\nWhy: a" || priUnchanged != 2 {
		t.Fatalf("task was modified on cancel: body=%q pri=%d", bodyUnchanged, priUnchanged)
	}

	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, 'e', 0))
	})
	u.app.QueueUpdateDraw(func() {
		form = u.form
	})
	if form == nil {
		t.Fatal("expected form to be open after pressing e")
	}
	u.app.QueueUpdateDraw(func() {
		priItem = u.form.GetFormItem(1).(*tview.InputField)
		priItem.SetText("")
		u.form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
	u.app.QueueUpdateDraw(func() {
		form = u.form
	})
	if form == nil {
		t.Fatal("expected form to stay open on invalid priority")
	}
	mu.Lock()
	priAfterInvalid := (*tasks)[0].Priority
	mu.Unlock()
	if priAfterInvalid != 2 {
		t.Fatalf("priority modified on invalid input: %d", priAfterInvalid)
	}

	u.app.QueueUpdateDraw(func() {
		bodyItem = u.form.GetFormItem(0).(*tview.TextArea)
		bodyItem.SetText("updated body\nwith multiple lines", false)
		priItem = u.form.GetFormItem(1).(*tview.InputField)
		priItem.SetText("5")
		u.form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return (*tasks)[0].Body == "updated body\nwith multiple lines" && (*tasks)[0].Priority == 5
	})

	u.app.QueueUpdateDraw(func() {
		form = u.form
	})
	if form != nil {
		t.Fatal("expected form to be closed after submit")
	}
}

func TestTUIFlagsAndEnv(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		t.Setenv("TASKD_URL", "")
		t.Setenv("T", "")
		t.Setenv("TASKD_PROJECT", "")
		cfg, err := parseFlags(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.url != "http://localhost:8080" {
			t.Fatalf("expected default url http://localhost:8080, got %q", cfg.url)
		}
		if cfg.project != "" {
			t.Fatalf("expected default project empty, got %q", cfg.project)
		}
	})

	t.Run("env fallback", func(t *testing.T) {
		t.Setenv("TASKD_URL", "")
		t.Setenv("T", "http://daemon-t:8080")
		t.Setenv("TASKD_PROJECT", "myproj")
		cfg, err := parseFlags(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.url != "http://daemon-t:8080" {
			t.Fatalf("expected url from T http://daemon-t:8080, got %q", cfg.url)
		}
		if cfg.project != "myproj" {
			t.Fatalf("expected project myproj, got %q", cfg.project)
		}

		t.Setenv("TASKD_URL", "http://daemon-taskd:8080")
		cfg, err = parseFlags(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.url != "http://daemon-taskd:8080" {
			t.Fatalf("expected url from TASKD_URL http://daemon-taskd:8080, got %q", cfg.url)
		}
	})

	t.Run("flag overrides", func(t *testing.T) {
		t.Setenv("TASKD_URL", "http://daemon-taskd:8080")
		t.Setenv("TASKD_PROJECT", "baseproj")
		cfg, err := parseFlags([]string{"-url", "http://custom:9090", "-project", "overrideproj"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.url != "http://custom:9090" {
			t.Fatalf("expected flag url http://custom:9090, got %q", cfg.url)
		}
		if cfg.project != "overrideproj" {
			t.Fatalf("expected flag project overrideproj, got %q", cfg.project)
		}
	})

	t.Run("help output", func(t *testing.T) {
		var buf bytes.Buffer
		printUsage(&buf)
		out := buf.String()
		for _, want := range []string{
			"Usage: taskd-tui",
			"Keyboard shortcuts:",
			"-url",
			"-project",
			"TASKD_URL",
			"TASKD_PROJECT",
			"T",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("help output missing %q:\n%s", want, out)
			}
		}

		_, err := parseFlags([]string{"-h"})
		if !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("expected ErrHelp for -h, got %v", err)
		}
	})

	t.Run("ui project filter initialization", func(t *testing.T) {
		u := newUI("http://localhost:8080", "proj-a")
		if u.url != "http://localhost:8080" {
			t.Fatalf("expected url http://localhost:8080, got %q", u.url)
		}
		if u.project != "proj-a" {
			t.Fatalf("expected project proj-a, got %q", u.project)
		}

		tasks := []task{
			{ID: "t1", Body: "Task 1", Project: "proj-a", Status: "pending"},
			{ID: "t2", Body: "Task 2", Project: "proj-b", Status: "pending"},
			{ID: "t3", Body: "Task 3", Project: "proj-a", Status: "done"},
		}
		u.render(tasks)
		if len(u.shown) != 2 {
			t.Fatalf("expected 2 tasks shown for proj-a, got %d", len(u.shown))
		}
		for _, tk := range u.shown {
			if tk.Project != "proj-a" {
				t.Fatalf("expected only proj-a tasks, got %q", tk.Project)
			}
		}
	})
}
