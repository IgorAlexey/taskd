package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestEmptyWorkerClaimGuard(t *testing.T) {
	var requests int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	t.Run("ClaimWithoutWorker", func(t *testing.T) {
		m := newModel(config{url: ts.URL, worker: ""}, newClient(ts.URL))
		m.tasks = []task{{
			ID:      "task-pending",
			Status:  "pending",
			Project: "taskd",
		}}
		m.rebuildShown()
		m.cursor = 0

		before := atomic.LoadInt32(&requests)
		up, _ := m.Update(tea.KeyPressMsg{Text: "c"})
		m = up.(model)
		wantMsg := "worker required; set via -worker flag or TASKD_WORKER"
		if m.msg != wantMsg {
			t.Fatalf("msg = %q, want %q", m.msg, wantMsg)
		}
		if after := atomic.LoadInt32(&requests); after != before {
			t.Fatalf("expected no HTTP requests, got %d", after-before)
		}
	})

	t.Run("TouchWithoutWorker", func(t *testing.T) {
		m := newModel(config{url: ts.URL, worker: ""}, newClient(ts.URL))
		m.tasks = []task{{
			ID:           "task-leased",
			Status:       "leased",
			Worker:       "worker-1",
			LeaseExpires: time.Now().Add(10 * time.Minute).Unix(),
			Project:      "taskd",
		}}
		m.rebuildShown()
		m.cursor = 0

		before := atomic.LoadInt32(&requests)
		up, _ := m.Update(tea.KeyPressMsg{Text: "t"})
		m = up.(model)
		wantMsg := "worker required; set via -worker flag or TASKD_WORKER"
		if m.msg != wantMsg {
			t.Fatalf("msg = %q, want %q", m.msg, wantMsg)
		}
		if after := atomic.LoadInt32(&requests); after != before {
			t.Fatalf("expected no HTTP requests, got %d", after-before)
		}
	})
}

func TestAssetOnlyTaskTitleAndDetailAndConfirm(t *testing.T) {
	taskItem := task{
		ID:        "abc1234567890",
		Project:   "render",
		AssetPath: "models/hero.blend",
		Body:      "",
		Status:    "pending",
		Priority:  2,
	}

	scope, title := titleOf(taskItem)
	if title != "models/hero.blend" {
		t.Fatalf("titleOf title = %q, want %q", title, "models/hero.blend")
	}
	if scope != "" {
		t.Fatalf("titleOf scope = %q, want empty", scope)
	}

	m := newModel(config{project: "render"}, nil)
	m.tasks = []task{taskItem}
	m.rebuildShown()
	m.cursor = 0
	m.width = 100
	m.height = 24

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "models/hero.blend") {
		t.Fatalf("expected view to contain asset path in title column and detail pane, got:\n%s", view)
	}

	m.confirmTask("Delete", "deleted", "DELETE", "/tasks/"+taskItem.ID, taskItem, nil)
	confirmView := ansi.Strip(m.confirm.View(m.width, m.height, m.theme))
	if !strings.Contains(confirmView, "models/hero.blend") {
		t.Fatalf("expected confirm view to contain asset path, got:\n%s", confirmView)
	}
	if !strings.Contains(m.confirm.text, "models/hero.blend") {
		t.Fatalf("expected confirm.text to contain asset path, got %q", m.confirm.text)
	}
}
