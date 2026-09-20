package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestLiveStatusTab(t *testing.T) {
	var requestedStatus atomic.Value
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks") {
			requestedStatus.Store(r.URL.Query().Get("status"))
			w.Header().Set("ETag", "test-etag")
			_ = json.NewEncoder(w).Encode([]task{
				{ID: 1, Status: "pending", Priority: 1},
				{ID: 2, Status: "leased", Priority: 2},
			})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/stats") {
			_ = json.NewEncoder(w).Encode(stats{Total: 5, Pending: 3, Leased: 2})
			return
		}
		_ = json.NewEncoder(w).Encode([]string{})
	}))
	defer ts.Close()

	m := newModel(config{icons: true, refresh: time.Hour}, newClient(ts.URL))
	m.width = 120
	m.height = 24
	m.hasStats = true
	m.stats = stats{
		Total:   9,
		Pending: 3,
		Leased:  2,
		Done:    4,
		Buried:  0,
	}

	tabs := m.tabDefs()
	expectedKeys := []string{"0", "1", "2", "3", "4", "5"}
	if len(tabs) != len(expectedKeys) {
		t.Fatalf("expected %d tabs, got %d", len(expectedKeys), len(tabs))
	}
	for i, k := range expectedKeys {
		if tabs[i].key != k {
			t.Fatalf("expected tab %d key %q, got %q", i, k, tabs[i].key)
		}
	}
	foundLive := false
	for _, tab := range m.tabDefs() {
		if tab.filter == "live" {
			foundLive = true
			if tab.key != "5" {
				t.Fatalf("expected live tab key '5', got %q", tab.key)
			}
			if tab.count != m.stats.Pending+m.stats.Leased {
				t.Fatalf("expected live tab count %d, got %d", m.stats.Pending+m.stats.Leased, tab.count)
			}
		}
	}
	if !foundLive {
		t.Fatal("live tab not found in tabDefs")
	}

	tabLine := ansi.Strip(strings.Split(m.View().Content, "\n")[1])
	if !strings.Contains(tabLine, "5") || !strings.Contains(tabLine, "live") {
		t.Fatalf("rendered header missing live tab: %q", tabLine)
	}

	up, cmd := m.Update(tea.KeyPressMsg{Text: "5"})
	m = up.(model)
	if m.filter != "live" {
		t.Fatalf("expected filter 'live' after pressing 5, got %q", m.filter)
	}

	if cc := m.corpusCount(); cc != m.stats.Pending+m.stats.Leased {
		t.Fatalf("expected corpusCount %d for live, got %d", m.stats.Pending+m.stats.Leased, cc)
	}

	if cmd == nil {
		t.Fatal("expected poll command after pressing 5")
	}
	msg := cmd()
	if poll, ok := msg.(pollMsg); ok {
		if poll.err != nil {
			t.Fatalf("unexpected poll error: %v", poll.err)
		}
	}
	if got := requestedStatus.Load(); got != "live" {
		t.Fatalf("expected status=live in query to daemon, got %v", got)
	}

	m.filter = ""
	bounds := m.row1Bounds()
	var liveTarget *tabHitTarget
	for _, target := range bounds.tabs {
		if target.filter == "live" {
			liveTarget = &target
			break
		}
	}
	if liveTarget == nil {
		t.Fatal("live tab target not found in row1Bounds")
	}

	up, _ = m.Update(tea.MouseClickMsg{
		X:      liveTarget.start + 1,
		Y:      1,
		Button: tea.MouseLeft,
	})
	m = up.(model)
	if m.filter != "live" {
		t.Fatalf("expected filter 'live' after clicking tab, got %q", m.filter)
	}
	m.tasks = []task{
		{ID: 1, Status: "pending"},
		{ID: 2, Status: "leased"},
		{ID: 3, Status: "done"},
		{ID: 4, Status: "buried"},
	}
	m.rebuildShown()
	if len(m.shown) != 2 {
		t.Fatalf("expected 2 shown tasks for live filter, got %d", len(m.shown))
	}
	s0 := m.tasks[m.shown[0]].Status
	s1 := m.tasks[m.shown[1]].Status
	if (s0 != "pending" && s0 != "leased") || (s1 != "pending" && s1 != "leased") || s0 == s1 {
		t.Fatalf("expected pending and leased in shown, got statuses: %s, %s", s0, s1)
	}
}
