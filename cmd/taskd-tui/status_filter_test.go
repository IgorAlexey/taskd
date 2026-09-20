package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestAgedProjectFilterSendsStatus(t *testing.T) {
	var lastTasksURL string

	all := make([]task, 600)
	for i := range all {
		all[i] = task{
			ID:      fmt.Sprintf("task%04d", i),
			Project: "ops",
			Status:  "pending",
			Body:    fmt.Sprintf("ops task %04d", i),
		}
		if i < 500 {
			all[i].Status = "done"
		}
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/tasks") {
			lastTasksURL = r.URL.RequestURI()
			status := r.URL.Query().Get("status")
			var out []task
			for _, tk := range all {
				if status == "" || tk.Status == status {
					out = append(out, tk)
				}
			}
			if len(out) > 500 {
				out = out[:500]
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/stats") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(stats{Pending: 100, Done: 500, Total: 600})
			return
		}
	}))
	defer ts.Close()

	cfg := config{url: ts.URL, project: "ops", refresh: time.Second, icons: false}
	m := newModel(cfg, newClient(ts.URL))
	m.width, m.height = 110, 24

	cmd := m.startPoll()
	msg := cmd()
	updated, _ := m.Update(msg)
	m = updated.(model)

	updated, poll := m.Update(tea.KeyPressMsg{Text: "1"})
	m = updated.(model)
	if m.filter != "pending" {
		t.Fatalf("expected filter pending, got %q", m.filter)
	}
	if poll == nil {
		t.Fatalf("expected poll command on filter change")
	}

	pollMsgVal := poll()
	if lastTasksURL != "/tasks?limit=500&project=ops&status=pending" {
		t.Fatalf("expected request URL with status=pending, got %q", lastTasksURL)
	}

	updated, _ = m.Update(pollMsgVal)
	m = updated.(model)

	if len(m.shown) != 100 {
		t.Fatalf("expected 100 pending tasks shown, got %d", len(m.shown))
	}
	view := m.View().Content
	if !strings.Contains(view, "1/100") {
		t.Fatalf("expected 1/100 in footer, got:\n%s", view)
	}
}

func TestExpiredLeaseNormalize(t *testing.T) {
	now := time.Now().Unix()
	expired := task{ID: "t1", Status: "leased", Worker: "w1", LeaseExpires: now - 60}
	active := task{ID: "t2", Status: "leased", Worker: "w2", LeaseExpires: now + 600}
	pending := task{ID: "t3", Status: "pending"}

	expired.normalize(now)
	if expired.Status != "pending" || expired.Worker != "" || expired.LeaseExpires != 0 {
		t.Errorf("expired lease should normalize to pending, got %+v", expired)
	}

	active.normalize(now)
	if active.Status != "leased" || active.Worker != "w2" || active.LeaseExpires != now+600 {
		t.Errorf("active lease should stay leased, got %+v", active)
	}

	pending.normalize(now)
	if pending.Status != "pending" {
		t.Errorf("pending task should stay pending, got %+v", pending)
	}
}
