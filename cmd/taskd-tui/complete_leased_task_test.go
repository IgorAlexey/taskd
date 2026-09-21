package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestCompleteLeasedTask(t *testing.T) {
	type doneReq struct {
		path   string
		worker string
	}
	var received doneReq
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Worker string `json:"worker"`
		}
		_ = json.Unmarshal(body, &payload)
		received = doneReq{path: r.URL.Path, worker: payload.Worker}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	now := time.Now()
	tOwn := task{
		ID:           1,
		Status:       "leased",
		Worker:       "worker-me",
		LeaseExpires: now.Add(time.Hour).Unix(),
	}
	tOther := task{
		ID:           2,
		Status:       "leased",
		Worker:       "worker-other",
		LeaseExpires: now.Add(time.Hour).Unix(),
	}

	m := newModel(config{
		url:    ts.URL,
		worker: "worker-me",
	}, newClient(ts.URL, ""))
	m.tasks = []task{tOwn, tOther}
	m.rebuildShown()

	m.cursor = 1
	up, _ := m.Update(tea.KeyPressMsg{Text: "x"})
	mOther := up.(model)
	if mOther.mode == modeConfirm {
		t.Fatal("expected x on task leased by different worker not to open confirmation")
	}
	if mOther.msg != "task leased by another worker" {
		t.Fatalf("msg = %q, want %q", mOther.msg, "task leased by another worker")
	}

	mNoWorker := m
	mNoWorker.cfg.worker = ""
	mNoWorker.cursor = 0
	up, _ = mNoWorker.Update(tea.KeyPressMsg{Text: "x"})
	mNoWorker = up.(model)
	if mNoWorker.mode == modeConfirm {
		t.Fatal("expected x with unset cfg.worker not to open confirmation")
	}
	if mNoWorker.msg != "worker required; set via -worker flag or TASKD_WORKER" {
		t.Fatalf("msg = %q, want %q", mNoWorker.msg, "worker required; set via -worker flag or TASKD_WORKER")
	}

	m.cursor = 0
	up, _ = m.Update(tea.KeyPressMsg{Text: "x"})
	m = up.(model)
	if m.mode != modeConfirm {
		t.Fatalf("mode after pressing x = %v, want modeConfirm", m.mode)
	}
	if m.confirm.method != "POST" || m.confirm.path != "/tasks/1/done" || m.confirm.button != "complete" {
		t.Fatalf("unexpected confirm modal: %+v", m.confirm)
	}
	wantBody := map[string]any{"worker": "worker-me"}
	if !reflect.DeepEqual(m.confirm.body, wantBody) {
		t.Fatalf("confirm body = %#v, want %#v", m.confirm.body, wantBody)
	}

	up, cmd := m.Update(tea.KeyPressMsg{Text: "y"})
	m = up.(model)
	if m.mode != modeTable {
		t.Fatalf("mode after confirming = %v, want modeTable", m.mode)
	}
	if cmd == nil {
		t.Fatal("expected actCmd after confirming complete")
	}
	actResult := cmd()
	act, ok := actResult.(actMsg)
	if !ok || act.err != nil {
		t.Fatalf("action failed: %+v", actResult)
	}

	if received.path != "/tasks/1/done" || received.worker != "worker-me" {
		t.Fatalf("server received %+v, want path /tasks/1/done and worker worker-me", received)
	}
}
