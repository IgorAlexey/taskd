package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestDetailPaneActionKeys(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	for _, initialMode := range []mode{modeDetail, modeZoom} {
		testTask := task{
			ID:       12345,
			Project:  "taskd",
			Status:   "pending",
			Body:     "action keys test task body",
			Priority: 2,
		}

		baseModel := func() model {
			m := newModel(config{
				url:    ts.URL,
				worker: "test-worker",
			}, newClient(ts.URL, ""))
			m.tasks = []task{testTask}
			m.rebuildShown()
			m.mode = initialMode
			m.cursor = 0
			m.lastRow = 0
			m.syncDetail()
			return m
		}

		m := baseModel()
		up, _ := m.Update(tea.KeyPressMsg{Text: "D"})
		mDel := up.(model)
		if mDel.mode != modeConfirm {
			t.Fatalf("mode %v: pressing D got mode %v, want modeConfirm", initialMode, mDel.mode)
		}
		if mDel.confirm.method != "DELETE" || mDel.confirm.path != "/tasks/"+strconv.FormatInt(testTask.ID, 10) {
			t.Fatalf("mode %v: unexpected confirm modal for D: %+v", initialMode, mDel.confirm)
		}

		m = baseModel()
		up, _ = m.Update(tea.KeyPressMsg{Text: "e"})
		mEdit := up.(model)
		if mEdit.mode != modeForm {
			t.Fatalf("mode %v: pressing e got mode %v, want modeForm", initialMode, mEdit.mode)
		}
		if !mEdit.form.editing || mEdit.form.id != testTask.ID {
			t.Fatalf("mode %v: unexpected form state for e: editing=%v id=%v", initialMode, mEdit.form.editing, mEdit.form.id)
		}

		m = baseModel()
		up, _ = m.Update(tea.KeyPressMsg{Text: "x"})
		mComplete := up.(model)
		if mComplete.mode != modeConfirm {
			t.Fatalf("mode %v: pressing x got mode %v, want modeConfirm", initialMode, mComplete.mode)
		}
		if mComplete.confirm.button != "complete" || mComplete.confirm.path != "/tasks/"+strconv.FormatInt(testTask.ID, 10)+"/close" {
			t.Fatalf("mode %v: unexpected confirm modal for x: %+v", initialMode, mComplete.confirm)
		}

		m = baseModel()
		up, cmd := m.Update(tea.KeyPressMsg{Text: "y"})
		_ = up.(model)
		if cmd == nil {
			t.Fatalf("mode %v: expected copyToClipboard cmd on y", initialMode)
		}

		leasedTask := testTask
		leasedTask.Status = "leased"
		leasedTask.Worker = "test-worker"
		leasedTask.LeaseExpires = time.Now().Add(time.Hour).Unix()
		mLeased := baseModel()
		mLeased.tasks = []task{leasedTask}
		mLeased.rebuildShown()

		up, cmd = mLeased.Update(tea.KeyPressMsg{Text: "u"})
		_ = up.(model)
		if cmd == nil {
			t.Fatalf("mode %v: expected release actCmd on u", initialMode)
		}

		up, _ = mLeased.Update(tea.KeyPressMsg{Text: "b"})
		mBury := up.(model)
		if mBury.mode != modeConfirm {
			t.Fatalf("mode %v: pressing b got mode %v, want modeConfirm", initialMode, mBury.mode)
		}
		if mBury.confirm.button != "bury" || mBury.confirm.path != "/tasks/"+strconv.FormatInt(testTask.ID, 10)+"/bury" {
			t.Fatalf("mode %v: unexpected confirm modal for b: %+v", initialMode, mBury.confirm)
		}
	}
}
