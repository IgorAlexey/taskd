package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestActionResultPreservedOverPollError(t *testing.T) {
	m := newTestModel()

	mUpdated, _ := m.Update(actMsg{msg: "touched task 0022222"})
	m = mUpdated.(model)

	view := m.View().Content
	if !strings.Contains(view, "touched task 0022222") {
		t.Fatalf("expected footer to contain success message, got:\n%s", view)
	}

	mUpdated, _ = m.Update(pollMsg{seq: m.seq, scope: m.listScope(), err: errors.New("connection lost")})
	m = mUpdated.(model)

	view = m.View().Content
	if !strings.Contains(view, "touched task 0022222") {
		t.Fatalf("expected footer to preserve success outcome over poll error, got:\n%s", view)
	}

	mUpdated, _ = m.Update(actMsg{err: errors.New("task not leased by worker")})
	m = mUpdated.(model)

	view = m.View().Content
	if !strings.Contains(view, "task not leased by worker") {
		t.Fatalf("expected footer to contain error message, got:\n%s", view)
	}

	mUpdated, _ = m.Update(pollMsg{seq: m.seq, scope: m.listScope(), err: errors.New("connection lost")})
	m = mUpdated.(model)

	view = m.View().Content
	if !strings.Contains(view, "task not leased by worker") {
		t.Fatalf("expected footer to preserve failure outcome over poll error, got:\n%s", view)
	}

	mUpdated, _ = m.Update(tickMsg(time.Now().Add(5 * time.Second)))
	m = mUpdated.(model)

	view = m.View().Content
	if !strings.Contains(view, "task not leased by worker") {
		t.Fatalf("expected error outcome to persist past 5s ticks, got:\n%s", view)
	}

	mUpdated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = mUpdated.(model)

	view = m.View().Content
	if strings.Contains(view, "task not leased by worker") {
		t.Fatalf("expected error outcome to be dismissed on Escape, got:\n%s", view)
	}
}

func TestTouchKeyBinding(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotWorker string
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		var body struct {
			Worker string `json:"worker"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotWorker = body.Worker
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	m := newTestModel()
	m.client = newClient(ts.URL)
	m.cfg.worker = "my-test-worker"

	m.cursor = 0
	mUpdated, cmd := m.Update(tea.KeyPressMsg{Text: "t"})
	m = mUpdated.(model)
	if cmd == nil {
		t.Fatal("expected cmd when touching non-leased task")
	}
	view := m.View().Content
	if !strings.Contains(view, "task is not leased") {
		t.Fatalf("expected advisory message for non-leased task, got:\n%s", view)
	}

	m.cursor = 1
	mUpdated, cmd = m.Update(tea.KeyPressMsg{Text: "t"})
	m = mUpdated.(model)
	if cmd == nil {
		t.Fatal("expected actCmd when touching leased task")
	}

	msg := cmd()
	if gotMethod != "POST" || gotPath != "/tasks/task0022222/touch" {
		t.Fatalf("unexpected touch request: %s %s", gotMethod, gotPath)
	}
	if gotWorker != "my-test-worker" {
		t.Fatalf("touch request worker = %q, want %q", gotWorker, "my-test-worker")
	}
	act, ok := msg.(actMsg)
	if !ok || act.err != nil || act.msg != "touched task task002" {
		t.Fatalf("unexpected actMsg: %+v", msg)
	}
}
