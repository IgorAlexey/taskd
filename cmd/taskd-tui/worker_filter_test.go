package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestWorkerFilter(t *testing.T) {
	t.Run("CycleForwardAndBackward", func(t *testing.T) {
		m := newModel(config{icons: false, refresh: time.Hour}, nil)
		m.workers = []string{"worker-alpha", "worker-beta"}
		m.worker = ""

		res, _ := m.Update(tea.KeyPressMsg{Text: "w"})
		m = res.(model)
		if m.worker != "worker-alpha" {
			t.Fatalf("expected worker-alpha after first w, got %q", m.worker)
		}

		res, _ = m.Update(tea.KeyPressMsg{Text: "w"})
		m = res.(model)
		if m.worker != "worker-beta" {
			t.Fatalf("expected worker-beta after second w, got %q", m.worker)
		}

		res, _ = m.Update(tea.KeyPressMsg{Text: "w"})
		m = res.(model)
		if m.worker != "" {
			t.Fatalf("expected worker filter cleared to empty after third w, got %q", m.worker)
		}

		res, _ = m.Update(tea.KeyPressMsg{Text: "W"})
		m = res.(model)
		if m.worker != "worker-beta" {
			t.Fatalf("expected worker-beta after W from empty, got %q", m.worker)
		}

		res, _ = m.Update(tea.KeyPressMsg{Text: "W"})
		m = res.(model)
		if m.worker != "worker-alpha" {
			t.Fatalf("expected worker-alpha after second W, got %q", m.worker)
		}

		res, _ = m.Update(tea.KeyPressMsg{Text: "W"})
		m = res.(model)
		if m.worker != "" {
			t.Fatalf("expected worker filter cleared to empty after third W, got %q", m.worker)
		}
	})

	t.Run("DisappearedWorkerFromActiveList", func(t *testing.T) {
		m := newModel(config{icons: false, refresh: time.Hour}, nil)
		m.workers = []string{"worker-alpha", "worker-beta"}
		m.worker = "dead-worker"

		res, _ := m.Update(tea.KeyPressMsg{Text: "w"})
		m1 := res.(model)
		if m1.worker != "worker-alpha" {
			t.Fatalf("expected forward cycle from dead worker to reach worker-alpha, got %q", m1.worker)
		}

		m.worker = "dead-worker"
		res, _ = m.Update(tea.KeyPressMsg{Text: "W"})
		m2 := res.(model)
		if m2.worker != "worker-beta" {
			t.Fatalf("expected backward cycle from dead worker to reach worker-beta, got %q", m2.worker)
		}
	})

	t.Run("TabsRenderActiveWorkerFilter", func(t *testing.T) {
		m := newModel(config{icons: false, refresh: time.Hour}, nil)
		m.width = 120
		m.height = 24
		m.project = "proj1"
		m.worker = "worker-alpha"

		content := ansi.Strip(m.View().Content)
		lines := strings.Split(content, "\n")
		if len(lines) < 2 {
			t.Fatalf("expected at least 2 lines, got %d", len(lines))
		}
		tabsLine := lines[1]
		if !strings.Contains(tabsLine, "project proj1") {
			t.Fatalf("expected project filter in tabs line, got: %s", tabsLine)
		}
		if !strings.Contains(tabsLine, "worker worker-alpha") {
			t.Fatalf("expected active worker filter in tabs line, got: %s", tabsLine)
		}
		if !strings.Contains(tabsLine, "w") {
			t.Fatalf("expected w key indicator in tabs line, got: %s", tabsLine)
		}

		m.worker = ""
		contentCleared := ansi.Strip(m.View().Content)
		tabsLineCleared := strings.Split(contentCleared, "\n")[1]
		if !strings.Contains(tabsLineCleared, "worker all") {
			t.Fatalf("expected cleared worker filter to show worker all, got: %s", tabsLineCleared)
		}
	})

	t.Run("QueryParamsIncludeWorker", func(t *testing.T) {
		var gotWorkerParam string
		var gotWorkerKey bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/tasks") {
				gotWorkerKey = r.URL.Query().Has("worker")
				gotWorkerParam = r.URL.Query().Get("worker")
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte("[]"))
				return
			}
			if r.URL.Path == "/stats" {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"pending":0,"leased":0,"done":0,"buried":0,"total":0}`))
				return
			}
			if r.URL.Path == "/projects" || r.URL.Path == "/workers" {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte("[]"))
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		cl := newClient(srv.URL)
		sc := listScope{
			filter: listFilter{
				worker: "worker-alpha",
			},
			pages: 1,
		}
		_, err := cl.list(sc, "")
		if err != nil {
			t.Fatalf("list error: %v", err)
		}
		if !gotWorkerKey {
			t.Fatalf("expected query parameters to include 'worker='")
		}
		if gotWorkerParam != "worker-alpha" {
			t.Fatalf("expected worker=worker-alpha, got %q", gotWorkerParam)
		}
	})

	t.Run("MemoryFilterWithoutPlaceholder", func(t *testing.T) {
		m := newModel(config{icons: false, refresh: time.Hour}, nil)
		m.width = 120
		m.height = 24
		m.workers = []string{"worker-alpha", "worker-beta"}
		future := time.Now().Unix() + 3600
		m.tasks = []task{
			{ID: "t1", Status: "leased", Worker: "worker-alpha", Body: "alpha task title", LeaseExpires: future},
			{ID: "t2", Status: "leased", Worker: "worker-beta", Body: "beta task title", LeaseExpires: future},
		}
		m.rebuild()
		if len(m.shown) != 2 {
			t.Fatalf("expected 2 tasks shown initially, got %d", len(m.shown))
		}

		res, _ := m.Update(tea.KeyPressMsg{Text: "w"})
		m = res.(model)

		if len(m.shown) != 1 {
			t.Fatalf("expected 1 task shown after worker filter, got %d", len(m.shown))
		}
		if m.tasks == nil {
			t.Fatalf("m.tasks should not be cleared to nil")
		}
		if m.tasks[m.shown[0]].ID != "t1" {
			t.Fatalf("expected task t1, got %s", m.tasks[m.shown[0]].ID)
		}

		content := ansi.Strip(m.View().Content)
		if !strings.Contains(content, "alpha task title") {
			t.Fatalf("expected view to contain cached task row, got:\n%s", content)
		}
		if strings.Contains(content, "No tasks") {
			t.Fatalf("expected view not to display empty placeholder, got:\n%s", content)
		}
	})

	t.Run("GetWorkersFromDaemon", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/workers" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]string{"w-one", "w-two"})
				return
			}
			http.NotFound(w, r)
		}))
		defer srv.Close()

		cl := newClient(srv.URL)
		workers, err := cl.getWorkers()
		if err != nil {
			t.Fatalf("getWorkers failed: %v", err)
		}
		if len(workers) != 2 || workers[0] != "w-one" || workers[1] != "w-two" {
			t.Fatalf("unexpected workers: %v", workers)
		}
	})
}
