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

func TestCreateFormKeepOpenOnFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]task{})
	})
	mux.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]string{"proj-test"})
	})
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"pending": 0, "leased": 0, "done": 0, "total": 0})
	})
	mux.HandleFunc("POST /tasks", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	u := newUI(srv.URL, "proj-test", false, "")
	u.projects = []string{"proj-test"}
	ts, err := u.fetch("proj-test", "")
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
	t.Cleanup(func() {
		u.app.Stop()
		<-done
	})

	query := func(fn func()) {
		ch := make(chan struct{})
		u.app.QueueUpdate(func() {
			fn()
			close(ch)
		})
		<-ch
	}

	query(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, 'n', 0))
	})

	var form *tview.Form
	eventually(t, func() bool {
		query(func() { form = u.form })
		return form != nil
	})

	typedBody := "first line of body\nsecond line of body"
	var bodyItem *tview.TextArea
	query(func() {
		bodyItem = form.GetFormItem(3).(*tview.TextArea)
		bodyItem.SetText(typedBody, true)
		form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	eventually(t, func() bool {
		var (
			hasForm bool
			hasPage bool
			title   string
			gotBody string
		)
		query(func() {
			hasForm = u.form != nil
			hasPage = u.pages.HasPage("create")
			if u.form != nil {
				title = u.form.GetTitle()
				bodyItem = u.form.GetFormItem(3).(*tview.TextArea)
				gotBody = bodyItem.GetText()
			}
		})
		return hasForm && hasPage && strings.Contains(title, "500") && gotBody == typedBody
	})

	query(func() { form = u.form })
	if form == nil {
		t.Fatal("expected create form to remain open after HTTP 500 rejection")
	}
	if !u.pages.HasPage("create") {
		t.Fatal("expected create page to remain in pages after rejection")
	}
	if got := bodyItem.GetText(); got != typedBody {
		t.Fatalf("expected typed body to remain intact, got %q", got)
	}
	if title := form.GetTitle(); !strings.Contains(title, "500") {
		t.Fatalf("expected form title to contain 500 error, got %q", title)
	}

	query(func() {
		form.GetButton(1).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
	eventually(t, func() bool {
		var modal *tview.Modal
		query(func() { modal = u.modal })
		return modal != nil
	})
	query(func() {
		u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyLeft, 0, 0), nil)
		u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
	eventually(t, func() bool {
		query(func() { form = u.form })
		return form == nil && !u.pages.HasPage("create")
	})
}
