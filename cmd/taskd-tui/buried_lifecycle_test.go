package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func drainPoll(m model, cmd tea.Cmd) model {
	if cmd == nil {
		return m
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		return m
	}
	pollCmd := batch[len(batch)-1]
	if pollCmd == nil {
		return m
	}
	pollResult := pollCmd()
	up, _ := m.Update(pollResult)
	return up.(model)
}

func TestBuriedLifecycle(t *testing.T) {
	type serverState struct {
		mu    sync.Mutex
		task  task
		stats stats
	}
	st := &serverState{
		task: task{
			ID:           "test-task-12345",
			Project:      "taskd",
			Status:       "leased",
			Worker:       "worker-a",
			LeaseExpires: time.Now().Add(3600 * time.Second).Unix(),
			Body:         "sample task to bury and kick",
			Priority:     0,
		},
		stats: stats{
			Total:  1,
			Leased: 1,
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		defer st.mu.Unlock()

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tasks":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("ETag", `"etag-1"`)
			w.Header().Set("X-Total-Count", "1")
			_ = json.NewEncoder(w).Encode([]task{st.task})
		case r.Method == http.MethodGet && r.URL.Path == "/stats":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(st.stats)
		case r.Method == http.MethodGet && r.URL.Path == "/projects":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]string{"taskd"})
		case r.Method == http.MethodPost && r.URL.Path == "/tasks/"+st.task.ID+"/bury":
			var body struct {
				Worker string `json:"worker"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if body.Worker != "worker-a" {
				http.Error(w, "worker mismatch", http.StatusConflict)
				return
			}
			st.task.Status = "buried"
			st.task.Worker = ""
			st.stats.Leased = 0
			st.stats.Buried = 1
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/tasks/"+st.task.ID+"/kick":
			st.task.Status = "pending"
			st.task.Worker = ""
			st.stats.Buried = 0
			st.stats.Pending = 1
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	help := newHelpModel(100, 30, modeTable, newTheme(true)).View(30, newTheme(true))
	plainHelp := ansi.Strip(help)
	if !strings.Contains(plainHelp, "[b]") || !strings.Contains(plainHelp, "bury") {
		t.Fatal("help overlay missing bury key documentation")
	}
	if !strings.Contains(plainHelp, "[K]") || !strings.Contains(plainHelp, "kick") {
		t.Fatal("help overlay missing kick key documentation")
	}

	m := newModel(config{
		url:     ts.URL,
		worker:  "worker-a",
		refresh: time.Hour,
	}, newClient(ts.URL))
	m.tasks = []task{st.task}
	m.rebuildShown()
	m.cursor = 0

	mOther := m
	mOther.cfg.worker = "worker-b"
	upOther, _ := mOther.Update(tea.KeyPressMsg{Text: "b"})
	mOther = upOther.(model)
	if mOther.mode == modeConfirm {
		t.Fatal("expected b not to open confirmation when task worker does not match cfg.worker")
	}

	up, _ := m.Update(tea.KeyPressMsg{Text: "b"})
	m = up.(model)
	if m.mode != modeConfirm {
		t.Fatalf("mode after pressing b = %v, want modeConfirm", m.mode)
	}
	if m.confirm.button != "bury" || m.confirm.method != "POST" || m.confirm.path != "/tasks/"+st.task.ID+"/bury" {
		t.Fatalf("unexpected confirm state for bury: %+v", m.confirm)
	}

	up, cmd := m.Update(tea.KeyPressMsg{Text: "y"})
	m = up.(model)
	if m.mode != modeTable {
		t.Fatalf("mode after confirming bury = %v, want modeTable", m.mode)
	}
	if cmd == nil {
		t.Fatal("expected actCmd after confirming bury")
	}
	actResult := cmd()
	act, ok := actResult.(actMsg)
	if !ok || act.err != nil {
		t.Fatalf("bury action failed: %+v", actResult)
	}
	up, batchCmd := m.Update(act)
	m = up.(model)
	m = drainPoll(m, batchCmd)

	if len(m.tasks) == 0 || m.tasks[0].Status != "buried" {
		t.Fatalf("task status = %q, want buried after live poll round-trip", m.tasks[0].Status)
	}

	up, _ = m.Update(tea.KeyPressMsg{Text: "b"})
	mGuard := up.(model)
	if mGuard.mode == modeConfirm {
		t.Fatal("expected b on buried task not to open confirmation")
	}

	up, _ = m.Update(tea.KeyPressMsg{Text: "K"})
	m = up.(model)
	if m.mode != modeConfirm {
		t.Fatalf("mode after pressing K = %v, want modeConfirm", m.mode)
	}
	if m.confirm.button != "kick" || m.confirm.method != "POST" || m.confirm.path != "/tasks/"+st.task.ID+"/kick" {
		t.Fatalf("unexpected confirm state for kick: %+v", m.confirm)
	}

	up, cmd = m.Update(tea.KeyPressMsg{Text: "y"})
	m = up.(model)
	if m.mode != modeTable {
		t.Fatalf("mode after confirming kick = %v, want modeTable", m.mode)
	}
	if cmd == nil {
		t.Fatal("expected actCmd after confirming kick")
	}
	actResult = cmd()
	act, ok = actResult.(actMsg)
	if !ok || act.err != nil {
		t.Fatalf("kick action failed: %+v", actResult)
	}
	up, batchCmd = m.Update(act)
	m = up.(model)

	wantID := shortID(st.task.ID)
	if !strings.Contains(m.msg, "kicked task "+wantID) {
		t.Fatalf("status feedback %q does not announce kicked task %s", m.msg, wantID)
	}

	m = drainPoll(m, batchCmd)
	if len(m.tasks) == 0 || m.tasks[0].Status != "pending" {
		t.Fatalf("task status = %q, want pending after live poll round-trip", m.tasks[0].Status)
	}

	up, _ = m.Update(tea.KeyPressMsg{Text: "K"})
	mGuard = up.(model)
	if mGuard.mode == modeConfirm {
		t.Fatal("expected K on pending task not to open confirmation")
	}
}
