package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func drainCmd(m model, cmd tea.Cmd) model {
	if cmd == nil {
		return m
	}
	res := cmd()
	if batch, ok := res.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				m = drainCmd(m, c)
			}
		}
		return m
	}
	up, _ := m.Update(res)
	return up.(model)
}

func TestManualRefreshFeedback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/stats" {
			w.Write([]byte(`{"pending":0,"leased":0,"done":0,"buried":0,"total":0}`))
			return
		}
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	m := newModel(config{worker: "test-worker", url: ts.URL}, newClient(ts.URL, ""))
	m.width = 100
	m.height = 24
	m.mode = modeTable

	up, cmd := m.Update(tea.KeyPressMsg{Text: "r"})
	m = up.(model)

	if !m.manualRefresh {
		t.Fatal("expected manualRefresh flag to be set after pressing r")
	}
	if m.msg != "" {
		t.Fatalf("expected msg to remain empty until poll completes, got %q", m.msg)
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd after pressing r")
	}

	m = drainCmd(m, cmd)
	if m.manualRefresh {
		t.Fatal("expected manualRefresh flag to be cleared after poll completed")
	}
	if m.msg != "queue refreshed" {
		t.Fatalf("expected msg %q after poll completes, got %q", "queue refreshed", m.msg)
	}

	rendered := m.View().Content
	lines := strings.Split(rendered, "\n")
	footerLine := lines[len(lines)-1]
	if !strings.Contains(footerLine, "queue refreshed") {
		t.Fatalf("expected footer to contain message %q, got %q", "queue refreshed", footerLine)
	}

	m.msg = ""
	m.mode = modeDetail
	up, cmd = m.Update(tea.KeyPressMsg{Text: "r"})
	m = up.(model)
	if !m.manualRefresh {
		t.Fatal("expected manualRefresh flag to be set in modeDetail")
	}
	m = drainCmd(m, cmd)
	if m.msg != "queue refreshed" {
		t.Fatalf("expected msg %q after detail refresh, got %q", "queue refreshed", m.msg)
	}
}
