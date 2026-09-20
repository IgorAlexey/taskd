package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func createTestModelWithTask(status string, width, height int) model {
	m := newModel(config{icons: false, refresh: time.Hour, worker: "w1"}, nil)
	m.width = width
	m.height = height
	t := task{
		ID:           "test-task-1",
		Body:         "task body",
		Status:       status,
		Priority:     1,
		Worker:       "w1",
		LeaseExpires: time.Now().Add(time.Hour).Unix(),
	}
	m.tasks = []task{t}
	m.shown = []int{0}
	m.cursor = 0
	m.mode = modeTable
	m.syncDetail()
	return m
}

func TestFooterLifecycleShortcuts(t *testing.T) {
	cases := []struct {
		status      string
		wantTokens  []string
		avoidTokens []string
	}{
		{
			status:      "pending",
			wantTokens:  []string{"c claim", "q quit", "? help"},
			avoidTokens: []string{"t touch", "u release", "b bury", "K kick"},
		},
		{
			status:      "leased",
			wantTokens:  []string{"t touch", "u release", "b bury", "q quit", "? help"},
			avoidTokens: []string{"c claim", "K kick"},
		},
		{
			status:      "buried",
			wantTokens:  []string{"K kick", "q quit", "? help"},
			avoidTokens: []string{"c claim", "t touch", "u release", "b bury"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			m := createTestModelWithTask(tc.status, 80, 24)
			rendered := m.View().Content
			lines := strings.Split(rendered, "\n")
			if len(lines) == 0 {
				t.Fatal("empty view")
			}
			footerLine := ansi.Strip(lines[len(lines)-1])
			if ansi.StringWidth(footerLine) != 80 {
				t.Fatalf("expected footer width 80, got %d:\n%q", ansi.StringWidth(footerLine), footerLine)
			}

			for _, tok := range tc.wantTokens {
				if !strings.Contains(footerLine, tok) {
					t.Fatalf("status %s: footer missing token %q in:\n%s", tc.status, tok, footerLine)
				}
			}
			for _, tok := range tc.avoidTokens {
				if strings.Contains(footerLine, tok) {
					t.Fatalf("status %s: footer should not contain token %q in:\n%s", tc.status, tok, footerLine)
				}
			}
		})
	}
}

func TestFooterTargetsBounds(t *testing.T) {
	m := createTestModelWithTask("leased", 80, 24)
	targets := m.footerTargets()
	if len(targets) == 0 {
		t.Fatal("expected footer targets")
	}

	rendered := m.View().Content
	lines := strings.Split(rendered, "\n")
	footerLine := ansi.Strip(lines[len(lines)-1])

	hasHelp := false
	hasQuit := false
	for _, target := range targets {
		if target.end > 80 {
			t.Fatalf("target %s extends beyond terminal width: %d > 80", target.action, target.end)
		}
		if target.start >= target.end {
			t.Fatalf("target %s invalid range: [%d, %d)", target.action, target.start, target.end)
		}
		if target.action == "help" {
			hasHelp = true
		}
		if target.action == "quit" {
			hasQuit = true
		}
	}
	if !hasHelp {
		t.Fatalf("footerTargets missing 'help' target in 80 cols; footer line:\n%s", footerLine)
	}
	if !hasQuit {
		t.Fatalf("footerTargets missing 'quit' target in 80 cols; footer line:\n%s", footerLine)
	}

	clickRes, cmd := m.handleFooterClick(79)
	if cmd != nil {
		t.Fatalf("expected no command for click at right boundary, got %v", cmd)
	}
	if clickRes.(model).mode != modeTable {
		t.Fatalf("expected modeTable after clicking unmapped margin, got %v", clickRes.(model).mode)
	}
}

func TestFooterLifecycleClicks(t *testing.T) {
	mBuried := createTestModelWithTask("buried", 80, 24)
	targetsBuried := mBuried.footerTargets()
	var kickTarget *footerTarget
	for i := range targetsBuried {
		if targetsBuried[i].action == "kick" {
			kickTarget = &targetsBuried[i]
			break
		}
	}
	if kickTarget == nil {
		t.Fatal("expected kick target in footer for buried task")
	}

	modK, _ := mBuried.handleFooterClick(kickTarget.start)
	if modK.(model).mode != modeConfirm || modK.(model).confirm.button != "kick" {
		t.Fatalf("expected confirm kick modal, got mode %v confirm %+v", modK.(model).mode, modK.(model).confirm)
	}

	mLeased := createTestModelWithTask("leased", 80, 24)
	targetsLeased := mLeased.footerTargets()
	var buryTarget *footerTarget
	for i := range targetsLeased {
		if targetsLeased[i].action == "bury" {
			buryTarget = &targetsLeased[i]
			break
		}
	}
	if buryTarget == nil {
		t.Fatal("expected bury target in footer for leased task")
	}

	modB, _ := mLeased.handleFooterClick(buryTarget.start)
	if modB.(model).mode != modeConfirm || modB.(model).confirm.button != "bury" {
		t.Fatalf("expected confirm bury modal, got mode %v confirm %+v", modB.(model).mode, modB.(model).confirm)
	}

	var lastMethod, lastPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastMethod = r.Method
		lastPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"test-task-1","status":"leased","worker":"w1"}`))
	}))
	defer ts.Close()

	mPending := createTestModelWithTask("pending", 80, 24)
	mPending.client = newClient(ts.URL)
	targetsPending := mPending.footerTargets()
	var claimTarget *footerTarget
	for i := range targetsPending {
		if targetsPending[i].action == "claim" {
			claimTarget = &targetsPending[i]
			break
		}
	}
	if claimTarget == nil {
		t.Fatal("expected claim target in footer for pending task")
	}
	_, cmdClaim := mPending.handleFooterClick(claimTarget.start)
	if cmdClaim == nil {
		t.Fatal("expected command from clicking claim target")
	}
	_ = cmdClaim()
	if lastMethod != "POST" || lastPath != "/tasks/test-task-1/claim" {
		t.Fatalf("expected POST /tasks/test-task-1/claim, got %s %s", lastMethod, lastPath)
	}

	mTouch := createTestModelWithTask("leased", 80, 24)
	mTouch.client = newClient(ts.URL)
	targetsTouch := mTouch.footerTargets()
	var touchTarget *footerTarget
	for i := range targetsTouch {
		if targetsTouch[i].action == "touch" {
			touchTarget = &targetsTouch[i]
			break
		}
	}
	if touchTarget == nil {
		t.Fatal("expected touch target in footer for leased task")
	}
	_, cmdTouch := mTouch.handleFooterClick(touchTarget.start)
	if cmdTouch == nil {
		t.Fatal("expected command from clicking touch target")
	}
	_ = cmdTouch()
	if lastMethod != "POST" || lastPath != "/tasks/test-task-1/touch" {
		t.Fatalf("expected POST /tasks/test-task-1/touch, got %s %s", lastMethod, lastPath)
	}

	mRelease := createTestModelWithTask("leased", 80, 24)
	mRelease.client = newClient(ts.URL)
	targetsRelease := mRelease.footerTargets()
	var releaseTarget *footerTarget
	for i := range targetsRelease {
		if targetsRelease[i].action == "release" {
			releaseTarget = &targetsRelease[i]
			break
		}
	}
	if releaseTarget == nil {
		t.Fatal("expected release target in footer for leased task")
	}
	_, cmdRelease := mRelease.handleFooterClick(releaseTarget.start)
	if cmdRelease == nil {
		t.Fatal("expected command from clicking release target")
	}
	_ = cmdRelease()
	if lastMethod != "POST" || lastPath != "/tasks/test-task-1/release" {
		t.Fatalf("expected POST /tasks/test-task-1/release, got %s %s", lastMethod, lastPath)
	}
}
func TestFooterSearch(t *testing.T) {
	m := createTestModelWithTask("pending", 120, 24)
	items := m.footerItems()
	found := false
	for _, it := range items {
		if it[0] == "/" && it[1] == "search" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected '/ search' in footer items, got %+v", items)
	}

	targets := m.footerTargets()
	var searchTarget *footerTarget
	for i := range targets {
		if targets[i].action == "search" {
			searchTarget = &targets[i]
			break
		}
	}
	if searchTarget == nil {
		t.Fatal("expected search target in footer targets")
	}

	res, cmd := m.handleFooterClick(searchTarget.start)
	if cmd != nil {
		t.Fatalf("expected nil command from clicking search target, got %v", cmd)
	}
	if res.(model).mode != modeSearch {
		t.Fatalf("expected modeSearch after clicking search footer target, got %v", res.(model).mode)
	}
}

func TestFooterErrorDismissAndTimeout(t *testing.T) {
	t.Run("ErrorDisplaysDismissHint", func(t *testing.T) {
		m := createTestModelWithTask("pending", 80, 24)
		cmd := m.setError("network timeout")
		if cmd == nil {
			t.Fatal("expected non-nil timer command from setError")
		}

		view := ansi.Strip(m.View().Content)
		lines := strings.Split(view, "\n")
		footerLine := lines[len(lines)-1]

		if !strings.Contains(footerLine, "network timeout") {
			t.Fatalf("expected footer to contain error text, got: %q", footerLine)
		}
		if !strings.Contains(footerLine, "[Esc dismiss]") {
			t.Fatalf("expected footer to contain '[Esc dismiss]', got: %q", footerLine)
		}

		up, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = up.(model)

		viewAfter := ansi.Strip(m.View().Content)
		linesAfter := strings.Split(viewAfter, "\n")
		footerAfter := linesAfter[len(linesAfter)-1]

		if strings.Contains(footerAfter, "network timeout") {
			t.Fatalf("expected error text cleared after Esc, got: %q", footerAfter)
		}
		if !strings.Contains(footerAfter, "q quit") {
			t.Fatalf("expected action shortcuts restored after dismiss, got: %q", footerAfter)
		}
	})

	t.Run("ErrorTimeoutRestoresShortcuts", func(t *testing.T) {
		m := createTestModelWithTask("pending", 80, 24)
		cmd := m.setError("temporary failure")
		if cmd == nil {
			t.Fatal("expected non-nil timer command from setError")
		}

		msgID := m.msgID
		up, _ := m.Update(clearMsgMsg{id: msgID})
		m = up.(model)

		viewAfter := ansi.Strip(m.View().Content)
		linesAfter := strings.Split(viewAfter, "\n")
		footerAfter := linesAfter[len(linesAfter)-1]

		if strings.Contains(footerAfter, "temporary failure") {
			t.Fatalf("expected error text cleared on timeout message, got: %q", footerAfter)
		}
		if !strings.Contains(footerAfter, "q quit") {
			t.Fatalf("expected action shortcuts restored after timeout, got: %q", footerAfter)
		}
	})

	t.Run("ErrorDismissClick", func(t *testing.T) {
		m := createTestModelWithTask("pending", 80, 24)
		m.setError("action failed")

		targets := m.footerTargets()
		var dismissTarget *footerTarget
		for i := range targets {
			if targets[i].action == "dismiss" {
				dismissTarget = &targets[i]
				break
			}
		}
		if dismissTarget == nil {
			t.Fatal("expected dismiss target in footer targets")
		}

		res, cmd := m.handleFooterClick(dismissTarget.start)
		if cmd != nil {
			t.Fatalf("expected nil command from clicking dismiss target, got %v", cmd)
		}
		if res.(model).msg != "" {
			t.Fatalf("expected msg cleared after clicking dismiss, got %q", res.(model).msg)
		}
	})

	t.Run("ErrorNarrowTerminalTruncatesSafely", func(t *testing.T) {
		m := createTestModelWithTask("pending", 20, 24)
		m.setError("very long error message that cannot fit")

		view := ansi.Strip(m.View().Content)
		lines := strings.Split(view, "\n")
		footerLine := lines[len(lines)-1]
		if ansi.StringWidth(footerLine) > 20 {
			t.Fatalf("footer line width %d exceeds terminal width 20: %q", ansi.StringWidth(footerLine), footerLine)
		}
	})

	t.Run("RegularMessageDoesNotInterceptEscapeInDetail", func(t *testing.T) {
		m := createTestModelWithTask("pending", 80, 24)
		m.mode = modeDetail
		m.setMsg("copied to clipboard")

		up, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = up.(model)

		if m.mode != modeTable {
			t.Fatalf("expected Escape to exit detail mode even when regular msg is present, got %v", m.mode)
		}

		m.mode = modeDetail
		m.setError("action failed")

		up, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = up.(model)

		if m.mode != modeDetail {
			t.Fatalf("expected Escape to dismiss error without leaving detail mode, got %v", m.mode)
		}
		if m.msg != "" || m.msgErr {
			t.Fatalf("expected error msg cleared after Escape, got msg=%q, msgErr=%v", m.msg, m.msgErr)
		}
	})

	t.Run("RegularMessageDoesNotInterceptEscapeInTable", func(t *testing.T) {
		m := createTestModelWithTask("pending", 80, 24)
		m.filter = "done"
		m.setMsg("copied to clipboard")

		up, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = up.(model)

		if m.filter != "" {
			t.Fatalf("expected Escape to clear filter even when regular msg is present, got %q", m.filter)
		}

		m.filter = "done"
		m.setError("action failed")

		up, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = up.(model)

		if m.filter != "done" {
			t.Fatalf("expected Escape to dismiss error first while keeping filter, got %q", m.filter)
		}
		if m.msg != "" || m.msgErr {
			t.Fatalf("expected error cleared after Escape, got msg=%q, msgErr=%v", m.msg, m.msgErr)
		}
	})
}
func TestFooterRefreshClick(t *testing.T) {
	m := createTestModelWithTask("pending", 160, 24)
	items := m.footerItems()
	found := false
	for _, it := range items {
		if it[0] == "r" && it[1] == "refresh" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected {'r', 'refresh'} in footerItems, got %+v", items)
	}

	targets := m.footerTargets()
	var refreshTarget *footerTarget
	for i := range targets {
		if targets[i].action == "refresh" {
			refreshTarget = &targets[i]
			break
		}
	}
	if refreshTarget == nil {
		t.Fatal("expected refresh target in footer targets")
	}

	res, cmd := m.handleFooterClick(refreshTarget.start)
	if cmd == nil {
		t.Fatal("expected poll command returned from clicking refresh target, got nil")
	}
	updated, ok := res.(model)
	if !ok {
		t.Fatalf("expected model type from handleFooterClick, got %T", res)
	}
	if !updated.manualRefresh {
		t.Fatal("expected manualRefresh to be true after clicking refresh target")
	}
}
