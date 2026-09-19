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
	"github.com/rivo/uniseg"
	"unicode/utf8"
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
			AssetPath string `json:"asset_path"`
			Body      string `json:"body"`
			Priority  int    `json:"priority"`
			Project   string `json:"project"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		tasks = append(tasks, task{
			ID:        "ddddddd4",
			Project:   req.Project,
			AssetPath: req.AssetPath,
			Status:    "pending",
			Priority:  req.Priority,
			Body:      req.Body,
		})
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": "ddddddd4"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u := newUI(srv.URL, "", false)
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
		t.Fatalf("default asset path = %q, want empty", got)
	}
	if got := form.GetFormItem(3).(*tview.TextArea).GetText(); got != "" {
		t.Fatalf("default body = %q, want empty", got)
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
	eventually(t, func() bool {
		s := screenText()
		return strings.Contains(s, "aaaaaaa") && strings.Contains(s, "first task")
	})
	fx, fy, fw, fh := form.GetRect()
	if fx <= 0 || fy <= 0 || fw >= 80 || fh >= 25 {
		t.Fatalf("expected centered bounded form, got rect (%d, %d, %d, %d)", fx, fy, fw, fh)
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
		u.form.GetFormItem(3).(*tview.TextArea).SetText("brand new task\n\nWhy: multi-line test\nDone when: ok", true)
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

	u := newUI(srv.URL, "", false)
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

	eventually(t, func() bool {
		var count int
		query(func() { count = len(u.all) })
		return count == 2
	})
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
	fx, fy, fw, fh := form.GetRect()
	if fx <= 0 || fy <= 0 || fw >= 80 || fh >= 25 {
		t.Fatalf("expected centered bounded form, got rect (%d, %d, %d, %d)", fx, fy, fw, fh)
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
		u := newUI("http://localhost:8080", "proj-a", false)
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

func TestGotoTopAndBottom(t *testing.T) {
	u, _, _ := stub(t)
	ts, err := u.fetch()
	if err != nil || len(ts) != 3 {
		t.Fatalf("fetch: %v %d", err, len(ts))
	}
	u.render(ts)

	r, _ := u.table.GetSelection()
	if r != 1 {
		t.Fatalf("initial selection = %d, want 1", r)
	}

	u.keys(tcell.NewEventKey(tcell.KeyRune, 'G', 0))
	r, _ = u.table.GetSelection()
	if r != 3 {
		t.Fatalf("after G selection = %d, want 3", r)
	}
	if sel, ok := u.selected(); !ok || sel.ID != "ccccccc3" {
		t.Fatalf("after G selected task = %+v", sel)
	}

	u.keys(tcell.NewEventKey(tcell.KeyRune, 'g', 0))
	r, _ = u.table.GetSelection()
	if r != 1 {
		t.Fatalf("after g selection = %d, want 1", r)
	}
	if sel, ok := u.selected(); !ok || sel.ID != "aaaaaaa1" {
		t.Fatalf("after g selected task = %+v", sel)
	}

	u.shown = nil
	u.keys(tcell.NewEventKey(tcell.KeyRune, 'g', 0))
	u.keys(tcell.NewEventKey(tcell.KeyRune, 'G', 0))
}

func TestNerdFontIcons(t *testing.T) {
	t.Run("parseFlags and env detection", func(t *testing.T) {
		t.Setenv("TASKD_TUI_ICONS", "")
		cfg, err := parseFlags(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.icons {
			t.Fatalf("expected default icons to be false, got true")
		}

		t.Setenv("TASKD_TUI_ICONS", "1")
		cfg, err = parseFlags(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.icons {
			t.Fatalf("expected TASKD_TUI_ICONS=1 to enable icons")
		}

		t.Setenv("TASKD_TUI_ICONS", "true")
		cfg, err = parseFlags(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.icons {
			t.Fatalf("expected TASKD_TUI_ICONS=true to enable icons")
		}

		t.Setenv("TASKD_TUI_ICONS", "1")
		cfg, err = parseFlags([]string{"-icons=false"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.icons {
			t.Fatalf("expected -icons=false to override env")
		}

		t.Setenv("TASKD_TUI_ICONS", "")
		cfg, err = parseFlags([]string{"-icons"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.icons {
			t.Fatalf("expected -icons flag to enable icons")
		}
	})

	t.Run("usage documentation", func(t *testing.T) {
		var buf bytes.Buffer
		printUsage(&buf)
		out := buf.String()
		for _, want := range []string{"-icons", "TASKD_TUI_ICONS", "Nerd Font"} {
			if !strings.Contains(out, want) {
				t.Fatalf("help output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("ascii fallback rendering", func(t *testing.T) {
		u := newUI("http://localhost:8080", "", false)
		tasks := []task{
			{ID: "t1", Status: "pending", Priority: 0, Body: "task 1"},
			{ID: "t2", Status: "leased", Priority: 1, Body: "task 2"},
			{ID: "t3", Status: "done", Priority: 2, Body: "task 3"},
		}
		u.render(tasks)
		if got := u.table.GetCell(1, 0).Text; got != "pending" {
			t.Fatalf("status cell = %q, want %q", got, "pending")
		}
		if got := u.table.GetCell(1, 1).Text; got != "0" {
			t.Fatalf("priority cell = %q, want %q", got, "0")
		}
		if got := u.table.GetCell(2, 0).Text; got != "leased" {
			t.Fatalf("status cell = %q, want %q", got, "leased")
		}
		if got := u.table.GetCell(2, 1).Text; got != "1" {
			t.Fatalf("priority cell = %q, want %q", got, "1")
		}
		if got := u.table.GetCell(3, 0).Text; got != "done" {
			t.Fatalf("status cell = %q, want %q", got, "done")
		}
		if got := u.table.GetCell(3, 1).Text; got != "2" {
			t.Fatalf("priority cell = %q, want %q", got, "2")
		}
	})

	t.Run("nerd font glyph rendering", func(t *testing.T) {
		u := newUI("http://localhost:8080", "", true)
		tasks := []task{
			{ID: "t1", Status: "pending", Priority: 0, Body: "task 1"},
			{ID: "t2", Status: "leased", Priority: 1, Body: "task 2"},
			{ID: "t3", Status: "done", Priority: 2, Body: "task 3"},
			{ID: "t4", Status: "pending", Priority: 5, Body: "task 4"},
		}
		u.render(tasks)
		if got := u.table.GetCell(1, 0).Text; got != "\uf017 pending" {
			t.Fatalf("status cell = %q, want %q", got, "\uf017 pending")
		}
		if got := u.table.GetCell(1, 1).Text; got != "\uf107 0" {
			t.Fatalf("priority cell = %q, want %q", got, "\uf107 0")
		}
		if got := u.table.GetCell(2, 0).Text; got != "\uf021 leased" {
			t.Fatalf("status cell = %q, want %q", got, "\uf021 leased")
		}
		if got := u.table.GetCell(2, 1).Text; got != "\uf106 1" {
			t.Fatalf("priority cell = %q, want %q", got, "\uf106 1")
		}
		if got := u.table.GetCell(3, 0).Text; got != "\uf00c done" {
			t.Fatalf("status cell = %q, want %q", got, "\uf00c done")
		}
		if got := u.table.GetCell(3, 1).Text; got != "\uf102 2" {
			t.Fatalf("priority cell = %q, want %q", got, "\uf102 2")
		}
		if got := u.table.GetCell(4, 1).Text; got != "\uf06d 5" {
			t.Fatalf("priority cell = %q, want %q", got, "\uf06d 5")
		}
	})
}

func TestSelectionClamping(t *testing.T) {
	u := newUI("http://localhost:8080", "", false)
	t1 := task{ID: "task-1", Status: "pending", Priority: 1, Body: "first"}
	t2 := task{ID: "task-2", Status: "pending", Priority: 2, Body: "second"}
	t3 := task{ID: "task-3", Status: "pending", Priority: 3, Body: "third"}
	t4 := task{ID: "task-4", Status: "pending", Priority: 4, Body: "fourth"}
	t5 := task{ID: "task-5", Status: "pending", Priority: 5, Body: "fifth"}

	u.render([]task{t1, t2, t3, t4, t5})
	r, _ := u.table.GetSelection()
	if r != 1 {
		t.Fatalf("expected initial row 1, got %d", r)
	}

	u.table.Select(3, 0)
	if sel, _ := u.selected(); sel.ID != "task-3" {
		t.Fatalf("expected task-3 selected, got %s", sel.ID)
	}

	u.render([]task{t1, t2, t4, t5})
	r, _ = u.table.GetSelection()
	if r != 3 {
		t.Fatalf("expected cursor to remain at row 3 after middle task deleted, got %d", r)
	}
	if sel, _ := u.selected(); sel.ID != "task-4" {
		t.Fatalf("expected task-4 at clamped row 3, got %s", sel.ID)
	}

	u.table.Select(4, 0)
	if sel, _ := u.selected(); sel.ID != "task-5" {
		t.Fatalf("expected task-5 selected, got %s", sel.ID)
	}

	u.render([]task{t1, t2, t4})
	r, _ = u.table.GetSelection()
	if r != 3 {
		t.Fatalf("expected cursor clamped to row 3 after last task deleted, got %d", r)
	}
	if sel, _ := u.selected(); sel.ID != "task-4" {
		t.Fatalf("expected task-4 at clamped row 3, got %s", sel.ID)
	}

	u.filter = "pending"
	claimedT2 := task{ID: "task-2", Status: "leased", Priority: 2, Body: "second"}
	u.table.Select(2, 0)
	u.render([]task{t1, claimedT2, t4})
	r, _ = u.table.GetSelection()
	if r != 2 {
		t.Fatalf("expected cursor clamped to row 2 after task claimed, got %d", r)
	}
	if sel, _ := u.selected(); sel.ID != "task-4" {
		t.Fatalf("expected task-4 at row 2, got %s", sel.ID)
	}

	u.filter = ""
	u.render([]task{t1, claimedT2, t4})
	u.table.Select(3, 0)
	u.keys(tcell.NewEventKey(tcell.KeyRune, '3', 0))
	r, _ = u.table.GetSelection()
	if r != 0 {
		t.Fatalf("expected row 0 for empty filtered view, got %d", r)
	}
	u.keys(tcell.NewEventKey(tcell.KeyRune, '0', 0))
	r, _ = u.table.GetSelection()
	if r != 1 {
		t.Fatalf("expected row 1 after restoring filter from empty, got %d", r)
	}
}

func TestTUIStatusLayout(t *testing.T) {
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

	screenText := func() string {
		var text string
		query(func() {
			cells, w, h := sim.GetContents()
			var sb strings.Builder
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					c := cells[y*w+x]
					if len(c.Runes) > 0 {
						sb.WriteRune(c.Runes[0])
					} else {
						sb.WriteByte(' ')
					}
				}
				sb.WriteByte('\n')
			}
			text = sb.String()
		})
		return text
	}

	assertStatusWidth := func() {
		var st string
		query(func() { st = u.status.GetText(true) })
		for _, line := range strings.Split(strings.TrimRight(st, "\n"), "\n") {
			if w := uniseg.StringWidth(line); w > 80 {
				t.Fatalf("status line exceeds 80 columns (%d cols): %q", w, line)
			}
			if !utf8.ValidString(line) {
				t.Fatalf("status line contains invalid UTF-8: %q", line)
			}
		}
	}

	// 1. Initial idle status layout: fits <= 80 columns and contains hotkey menu
	eventually(t, func() bool {
		var st string
		query(func() { st = u.status.GetText(true) })
		return strings.Contains(st, "project all") && strings.Contains(st, "[n] new")
	})
	assertStatusWidth()

	// 2. Action failure feedback: attempt to delete leased task bbbbbbb2
	sim.InjectKey(tcell.KeyRune, 'j', 0)
	sim.InjectKey(tcell.KeyRune, 'D', 0)
	eventually(t, func() bool {
		var modal *tview.Modal
		query(func() { modal = u.modal })
		return modal != nil
	})
	query(func() {
		u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	eventually(t, func() bool {
		var st string
		query(func() { st = u.status.GetText(true) })
		return strings.Contains(st, "409") && strings.Contains(st, "task is leased")
	})
	assertStatusWidth()
	if !strings.Contains(screenText(), "409") {
		t.Fatalf("error feedback 409 not visible on 80x25 screen:\n%s", screenText())
	}

	// 3. Action success feedback: increment priority (+)
	sim.InjectKey(tcell.KeyRune, '+', 0)
	eventually(t, func() bool {
		var st string
		query(func() { st = u.status.GetText(true) })
		return strings.Contains(st, "priority")
	})
	assertStatusWidth()

	// 4. Action success feedback: delete a non-leased task
	sim.InjectKey(tcell.KeyRune, 'k', 0)
	sim.InjectKey(tcell.KeyRune, 'D', 0)
	eventually(t, func() bool {
		var modal *tview.Modal
		query(func() { modal = u.modal })
		return modal != nil
	})
	query(func() {
		u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
	eventually(t, func() bool {
		var st string
		query(func() { st = u.status.GetText(true) })
		return strings.Contains(st, "deleted")
	})
	assertStatusWidth()

	// 5. Action success feedback: create a new task
	sim.InjectKey(tcell.KeyRune, 'n', 0)
	eventually(t, func() bool {
		var open bool
		query(func() { open = u.form != nil })
		return open
	})
	query(func() {
		u.form.GetFormItem(3).(*tview.TextArea).SetText("layout test task", false)
		u.form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
	eventually(t, func() bool {
		var st string
		query(func() { st = u.status.GetText(true) })
		return strings.Contains(st, "created")
	})
	assertStatusWidth()

	// 6. Multi-byte UTF-8 feedback is displayed and does not wrap to extra lines
	u.app.QueueUpdateDraw(func() {
		u.setMsg("error: 日本語 text with 🚀 feedback")
	})
	assertStatusWidth()
	eventually(t, func() bool {
		s := screenText()
		return strings.Contains(s, "error:") && strings.Contains(s, "feedback")
	})
	// 7. Long project names and long error messages fit within 80 columns
	query(func() {
		u.project = "infrastructure-deployment-long-project-name"
		u.setMsg("DELETE /tasks/1234567890: 409 Conflict: task is currently leased by worker-agent-node-us-east")
	})
	assertStatusWidth()
	// 8. Full-width CJK characters fit within 80 visual columns
	query(func() {
		u.project = strings.Repeat("世界", 20)
		u.setMsg(strings.Repeat("日本語", 30))
	})
	assertStatusWidth()
	// 9. Small width truncation bounds check
	for w := 0; w <= 5; w++ {
		if got := truncWidth("hello world", w); uniseg.StringWidth(got) > w {
			t.Fatalf("truncWidth for max %d exceeded width: %q (width %d)", w, got, uniseg.StringWidth(got))
		}
	}
}

func TestCopyTaskID(t *testing.T) {
	u, _, _ := stub(t)
	ts, err := u.fetch()
	if err != nil || len(ts) != 3 {
		t.Fatalf("fetch: %v %d", err, len(ts))
	}
	u.render(ts)

	var copied string
	origCopy := copyToClipboard
	copyToClipboard = func(text string) {
		copied = text
	}
	t.Cleanup(func() { copyToClipboard = origCopy })

	u.keys(tcell.NewEventKey(tcell.KeyRune, 'y', 0))
	if copied != "aaaaaaa1" {
		t.Fatalf("copied ID = %q, want aaaaaaa1", copied)
	}
	if !strings.Contains(u.status.GetText(true), "copied aaaaaaa1") {
		t.Fatalf("status line missing copied confirmation: %q", u.status.GetText(true))
	}

	u.table.Select(2, 0)
	u.keys(tcell.NewEventKey(tcell.KeyRune, 'y', 0))
	if copied != "bbbbbbb2" {
		t.Fatalf("copied ID = %q, want bbbbbbb2", copied)
	}
	if !strings.Contains(u.status.GetText(true), "copied bbbbbbb2") {
		t.Fatalf("status line missing copied confirmation: %q", u.status.GetText(true))
	}

	u.table.Select(3, 0)
	u.bodyKeys(tcell.NewEventKey(tcell.KeyRune, 'y', 0))
	if copied != "ccccccc3" {
		t.Fatalf("copied ID = %q, want ccccccc3", copied)
	}
	if !strings.Contains(u.status.GetText(true), "copied ccccccc3") {
		t.Fatalf("status line missing copied confirmation: %q", u.status.GetText(true))
	}

	u.shown = nil
	copied = ""
	u.keys(tcell.NewEventKey(tcell.KeyRune, 'y', 0))
	if copied != "" {
		t.Fatalf("copied on empty list = %q, want empty", copied)
	}

	var buf bytes.Buffer
	origOut := clipboardOut
	clipboardOut = &buf
	t.Cleanup(func() { clipboardOut = origOut })
	t.Setenv("TMUX", "")
	defaultCopyToClipboard("test-id-12345")
	if buf.String() != "\x1b]52;c;dGVzdC1pZC0xMjM0NQ==\x07" {
		t.Fatalf("osc52 sequence = %q", buf.String())
	}

	t.Setenv("TMUX", "/tmp/tmux-1000/default,1234,0")
	buf.Reset()
	defaultCopyToClipboard("test-id-12345")
	if buf.String() != "\x1bPtmux;\x1b\x1b]52;c;dGVzdC1pZC0xMjM0NQ==\x07\x1b\\" {
		t.Fatalf("tmux osc52 sequence = %q", buf.String())
	}
}

func TestFetchServerError(t *testing.T) {
	t.Run("internal server error with text body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		u := newUI(srv.URL, "", false)
		ts, err := u.fetch()
		if err == nil {
			t.Fatal("expected error from non-200 response, got nil")
		}
		if ts != nil {
			t.Fatalf("expected nil tasks on error, got %v", ts)
		}
		if strings.Contains(err.Error(), "invalid character") {
			t.Fatalf("expected HTTP status error instead of JSON parse error: %v", err)
		}
		if !strings.Contains(err.Error(), "500") {
			t.Fatalf("expected status code 500 in error, got: %v", err)
		}
		if !strings.Contains(err.Error(), "internal error") {
			t.Fatalf("expected server response body in error, got: %v", err)
		}
	})

	t.Run("bad gateway", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte("bad gateway"))
		}))
		t.Cleanup(srv.Close)

		u := newUI(srv.URL, "", false)
		_, err := u.fetch()
		if err == nil {
			t.Fatal("expected error from 502 response, got nil")
		}
		if !strings.Contains(err.Error(), "502") {
			t.Fatalf("expected status code 502 in error, got: %v", err)
		}
		if !strings.Contains(err.Error(), "bad gateway") {
			t.Fatalf("expected server body in error, got: %v", err)
		}
	})

	t.Run("empty error body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv.Close)

		u := newUI(srv.URL, "", false)
		_, err := u.fetch()
		if err == nil {
			t.Fatal("expected error from 503 response, got nil")
		}
		if !strings.Contains(err.Error(), "503") {
			t.Fatalf("expected status code 503 in error, got: %v", err)
		}
	})

	t.Run("refresh surfaces error in status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "upstream timeout", http.StatusGatewayTimeout)
		}))
		t.Cleanup(srv.Close)

		u := newUI(srv.URL, "", false)
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

		u.refresh()
		eventually(t, func() bool {
			var msg, status string
			query(func() {
				msg = u.msg
				status = u.status.GetText(true)
			})
			return strings.Contains(msg, "504") && strings.Contains(msg, "upstream timeout") &&
				strings.Contains(status, "504") && strings.Contains(status, "upstream timeout")
		})
	})
	t.Run("bounded error body read", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write(bytes.Repeat([]byte("x"), 10000))
		}))
		t.Cleanup(srv.Close)

		u := newUI(srv.URL, "", false)
		_, err := u.fetch()
		if err == nil {
			t.Fatal("expected error from 500 response, got nil")
		}
		if len(err.Error()) > 3000 {
			t.Fatalf("expected bounded error length, got %d", len(err.Error()))
		}
	})
}

func TestCreateFormValidation(t *testing.T) {
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

	u.app.QueueUpdateDraw(func() {
		u.form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
	u.app.QueueUpdateDraw(func() {
		form = u.form
	})
	if form == nil {
		t.Fatal("expected form to stay open on empty body")
	}
	if title := form.GetTitle(); !strings.Contains(title, "body") {
		t.Fatalf("expected title to indicate body error, got %q", title)
	}
	mu.Lock()
	count := len(*tasks)
	mu.Unlock()
	if count != 3 {
		t.Fatalf("task was created on empty body: %d", count)
	}

	testBody := "brand new task\n\nWhy: multi-line test\nDone when: ok"
	u.app.QueueUpdateDraw(func() {
		u.form.GetFormItem(3).(*tview.TextArea).SetText(testBody, true)
		u.form.GetFormItem(1).(*tview.InputField).SetText("-1")
		u.form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
	u.app.QueueUpdateDraw(func() {
		form = u.form
	})
	if form == nil {
		t.Fatal("expected form to stay open on negative priority")
	}
	if title := form.GetTitle(); !strings.Contains(title, "priority") {
		t.Fatalf("expected title to indicate priority error, got %q", title)
	}
	if got := form.GetFormItem(3).(*tview.TextArea).GetText(); got != testBody {
		t.Fatalf("body buffer lost on invalid priority: %q", got)
	}
	mu.Lock()
	count = len(*tasks)
	mu.Unlock()
	if count != 3 {
		t.Fatalf("task was created on negative priority: %d", count)
	}

	u.app.QueueUpdateDraw(func() {
		u.form.GetFormItem(1).(*tview.InputField).SetText("7")
		u.form.GetFormItem(0).(*tview.InputField).SetText("")
		u.form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
	u.app.QueueUpdateDraw(func() {
		form = u.form
	})
	if form == nil {
		t.Fatal("expected form to stay open on empty project")
	}
	if title := form.GetTitle(); !strings.Contains(title, "project") {
		t.Fatalf("expected title to indicate project error, got %q", title)
	}
	if got := form.GetFormItem(3).(*tview.TextArea).GetText(); got != testBody {
		t.Fatalf("body buffer lost on empty project: %q", got)
	}
	if got := form.GetFormItem(1).(*tview.InputField).GetText(); got != "7" {
		t.Fatalf("priority buffer lost on empty project: %q", got)
	}
	mu.Lock()
	count = len(*tasks)
	mu.Unlock()
	if count != 3 {
		t.Fatalf("task was created on empty project: %d", count)
	}

	u.app.QueueUpdateDraw(func() {
		u.form.GetFormItem(0).(*tview.InputField).SetText(" * ")
		u.form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
	u.app.QueueUpdateDraw(func() {
		form = u.form
	})
	if form == nil {
		t.Fatal("expected form to stay open on wildcard project")
	}
	if title := form.GetTitle(); !strings.Contains(title, "project") {
		t.Fatalf("expected title to indicate project error, got %q", title)
	}

	u.app.QueueUpdateDraw(func() {
		u.form.GetFormItem(0).(*tview.InputField).SetText("proj-test")
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
	if created.Project != "proj-test" || created.Priority != 7 || created.Body != testBody {
		t.Fatalf("created task mismatch: %+v", created)
	}
	u.app.QueueUpdateDraw(func() {
		form = u.form
	})
	if form != nil {
		t.Fatal("expected form to be closed after valid submit")
	}
}

func TestManualRefresh(t *testing.T) {
	u, tasks, mu := stub(t)
	ts, err := u.fetch()
	if err != nil || len(ts) != 3 {
		t.Fatalf("fetch: %v %d", err, len(ts))
	}
	u.render(ts)

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
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

	mu.Lock()
	*tasks = append(*tasks, task{
		ID:       "ddddddd4",
		Project:  "proj-c",
		Status:   "pending",
		Priority: 3,
		Body:     "fourth task",
	})
	mu.Unlock()

	if len(u.all) != 3 {
		t.Fatalf("expected 3 tasks before refresh, got %d", len(u.all))
	}

	ev := tcell.NewEventKey(tcell.KeyRune, 'r', 0)
	if ret := u.keys(ev); ret != nil {
		t.Fatalf("expected nil return for 'r' key, got %v", ret)
	}

	eventually(t, func() bool {
		var count int
		ch := make(chan struct{})
		u.app.QueueUpdate(func() {
			count = len(u.all)
			close(ch)
		})
		<-ch
		return count == 4
	})

	mu.Lock()
	*tasks = append(*tasks, task{
		ID:       "eeeeeee5",
		Project:  "proj-c",
		Status:   "pending",
		Priority: 4,
		Body:     "fifth task",
	})
	mu.Unlock()

	evCap := tcell.NewEventKey(tcell.KeyRune, 'R', 0)
	if ret := u.keys(evCap); ret != nil {
		t.Fatalf("expected nil return for 'R' key, got %v", ret)
	}

	eventually(t, func() bool {
		var count int
		ch := make(chan struct{})
		u.app.QueueUpdate(func() {
			count = len(u.all)
			close(ch)
		})
		<-ch
		return count == 5
	})

	for range 20 {
		if ret := u.keys(ev); ret != nil {
			t.Fatalf("expected nil return for r key, got %v", ret)
		}
	}
}
func TestClearErrorOnReconnect(t *testing.T) {
	var mu sync.Mutex
	fail := true
	tasks := []task{
		{ID: "aaaaaaa1", Project: "proj-a", Status: "pending", Priority: 1, Body: "reconnected task"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			http.Error(w, "daemon unavailable", http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(tasks)
	}))
	t.Cleanup(srv.Close)

	u := newUI(srv.URL, "", false)
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

	u.refresh()
	eventually(t, func() bool {
		var msg, status string
		query(func() {
			msg = u.msg
			status = u.status.GetText(true)
		})
		return strings.Contains(msg, "502") && strings.Contains(msg, "daemon unavailable") &&
			strings.Contains(status, "502") && strings.Contains(status, "daemon unavailable")
	})

	mu.Lock()
	fail = false
	mu.Unlock()

	u.refresh()
	eventually(t, func() bool {
		var msg, status string
		var count int
		query(func() {
			msg = u.msg
			status = u.status.GetText(true)
			count = len(u.shown)
		})
		return count == 1 && msg == "" && !strings.Contains(status, "daemon unavailable")
	})
}
func TestCreateFormAssetPath(t *testing.T) {
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

	if form.GetFormItemCount() < 4 {
		t.Fatalf("expected at least 4 form items, got %d", form.GetFormItemCount())
	}
	assetItem, ok := form.GetFormItem(2).(*tview.InputField)
	if !ok {
		t.Fatalf("expected item 2 to be *tview.InputField, got %T", form.GetFormItem(2))
	}
	if got := assetItem.GetText(); got != "" {
		t.Fatalf("default asset path = %q, want empty", got)
	}

	u.app.QueueUpdateDraw(func() {
		form.GetFormItem(0).(*tview.InputField).SetText("pipeline")
		form.GetFormItem(1).(*tview.InputField).SetText("9")
		form.GetFormItem(2).(*tview.InputField).SetText("assets/model.gltf")
		form.GetFormItem(3).(*tview.TextArea).SetText("", true)
		form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(*tasks) == 4
	})
	mu.Lock()
	created := (*tasks)[3]
	mu.Unlock()
	if created.Project != "pipeline" || created.Priority != 9 || created.AssetPath != "assets/model.gltf" || created.Body != "" {
		t.Fatalf("created task mismatch: %+v", created)
	}
	u.app.QueueUpdateDraw(func() {
		form = u.form
	})
	if form != nil {
		t.Fatal("expected form to be closed after submit with asset path")
	}
}
