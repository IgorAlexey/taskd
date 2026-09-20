package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func newTestModel(t *testing.T) model {
	t.Helper()
	cfg := config{
		refresh: time.Second,
		worker:  "testworker",
		icons:   true,
	}
	m := newModel(cfg, nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(model)

	now := time.Now().Unix()
	tasks := make([]task, 30)
	for i := 0; i < 30; i++ {
		status := "pending"
		var leaseExp int64
		worker := ""
		switch {
		case i < 10:
			status = "pending"
		case i < 20:
			status = "leased"
			worker = "laptop:checkout"
			leaseExp = now + 3600 // actively leased
		case i < 25:
			status = "done"
		default:
			status = "buried"
		}
		tasks[i] = task{
			ID:           fmt.Sprintf("task-%02d", i),
			Project:      fmt.Sprintf("proj%d", i%3),
			Status:       status,
			Worker:       worker,
			LeaseExpires: leaseExp,
			Priority:     i % 5, // 0, 1, 2, 3, 4
			Body:         fmt.Sprintf("proj%d: Title of task %d\nBody line 2 for task %d with `code`", i%3, i, i),
		}
	}

	updated, _ = m.Update(pollMsg{
		tasks:    tasks,
		changed:  true,
		projects: []string{"proj0", "proj1", "proj2"},
	})
	return updated.(model)
}

func TestNavigationHardStopAndOffset(t *testing.T) {
	m := newTestModel(t)
	if len(m.shown) != 30 {
		t.Fatalf("expected 30 tasks shown, got %d", len(m.shown))
	}

	// k at row 0 stays at row 0 (hard stop)
	if m.cursor != 0 {
		t.Fatalf("expected cursor at 0, got %d", m.cursor)
	}
	updated, _ := m.Update(tea.KeyPressMsg{Text: "k"})
	m = updated.(model)
	if m.cursor != 0 {
		t.Errorf("k at row 0 should stay at 0, got %d", m.cursor)
	}

	// G moves to last row
	updated, _ = m.Update(tea.KeyPressMsg{Text: "G"})
	m = updated.(model)
	if m.cursor != 29 {
		t.Errorf("G should move to last row (29), got %d", m.cursor)
	}

	// j at last row stays at last row (hard stop)
	updated, _ = m.Update(tea.KeyPressMsg{Text: "j"})
	m = updated.(model)
	if m.cursor != 29 {
		t.Errorf("j at last row should stay at 29, got %d", m.cursor)
	}

	// g moves to first row
	updated, _ = m.Update(tea.KeyPressMsg{Text: "g"})
	m = updated.(model)
	if m.cursor != 0 {
		t.Errorf("g should move to first row (0), got %d", m.cursor)
	}

	// ctrl-d and ctrl-u clamped
	tr := m.tableRows()
	if tr <= 0 {
		t.Fatalf("tableRows should be > 0, got %d", tr)
	}
	half := tr / 2
	if half < 1 {
		half = 1
	}

	// ctrl-d moves down by tableRows/2
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	m = updated.(model)
	if m.cursor != half {
		t.Errorf("ctrl-d should advance by %d, got cursor %d", half, m.cursor)
	}

	// Move all the way down with ctrl-d; must clamp at 29
	for i := 0; i < 10; i++ {
		updated, _ = m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
		m = updated.(model)
	}
	if m.cursor != 29 {
		t.Errorf("repeated ctrl-d should clamp at 29, got %d", m.cursor)
	}

	// ctrl-u moves up; repeatedly must clamp at 0
	for i := 0; i < 10; i++ {
		updated, _ = m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
		m = updated.(model)
	}
	if m.cursor != 0 {
		t.Errorf("repeated ctrl-u should clamp at 0, got %d", m.cursor)
	}

	// offset follows cursor and never exceeds len(shown)-tableRows
	maxOffset := len(m.shown) - tr
	if maxOffset < 0 {
		maxOffset = 0
	}

	for i := 0; i < 29; i++ {
		updated, _ = m.Update(tea.KeyPressMsg{Text: "j"})
		m = updated.(model)
		if m.cursor < m.offset || m.cursor >= m.offset+tr {
			t.Errorf("cursor %d not visible with offset %d and tableRows %d", m.cursor, m.offset, tr)
		}
		if m.offset > maxOffset {
			t.Errorf("offset %d exceeds maxOffset %d", m.offset, maxOffset)
		}
		if m.offset < 0 {
			t.Errorf("offset %d should be >= 0", m.offset)
		}
	}
	if m.offset != maxOffset {
		t.Errorf("at bottom cursor 29, offset should be maxOffset %d, got %d", maxOffset, m.offset)
	}

	// Move back up; offset follows
	for i := 0; i < 29; i++ {
		updated, _ = m.Update(tea.KeyPressMsg{Text: "k"})
		m = updated.(model)
		if m.cursor < m.offset || m.cursor >= m.offset+tr {
			t.Errorf("cursor %d not visible with offset %d and tableRows %d", m.cursor, m.offset, tr)
		}
		if m.offset < 0 {
			t.Errorf("offset %d should be >= 0", m.offset)
		}
	}
	if m.offset != 0 {
		t.Errorf("at top cursor 0, offset should be 0, got %d", m.offset)
	}
}

func TestFilterKeys(t *testing.T) {
	m := newTestModel(t)

	// Filter '1' -> pending
	updated, _ := m.Update(tea.KeyPressMsg{Text: "1"})
	m = updated.(model)
	if m.filter != "pending" {
		t.Errorf("expected filter pending, got %q", m.filter)
	}
	if len(m.shown) != 10 {
		t.Errorf("expected 10 pending tasks, got %d", len(m.shown))
	}
	for _, idx := range m.shown {
		if m.tasks[idx].Status != "pending" {
			t.Errorf("expected pending task, got status %q", m.tasks[idx].Status)
		}
	}

	// Filter '2' -> leased
	updated, _ = m.Update(tea.KeyPressMsg{Text: "2"})
	m = updated.(model)
	if m.filter != "leased" {
		t.Errorf("expected filter leased, got %q", m.filter)
	}
	if len(m.shown) != 10 {
		t.Errorf("expected 10 leased tasks, got %d", len(m.shown))
	}

	// Filter '3' -> done
	updated, _ = m.Update(tea.KeyPressMsg{Text: "3"})
	m = updated.(model)
	if m.filter != "done" {
		t.Errorf("expected filter done, got %q", m.filter)
	}
	if len(m.shown) != 5 {
		t.Errorf("expected 5 done tasks, got %d", len(m.shown))
	}

	// Filter '4' -> buried
	updated, _ = m.Update(tea.KeyPressMsg{Text: "4"})
	m = updated.(model)
	if m.filter != "buried" {
		t.Errorf("expected filter buried, got %q", m.filter)
	}
	if len(m.shown) != 5 {
		t.Errorf("expected 5 buried tasks, got %d", len(m.shown))
	}

	// Filter '0' -> all
	updated, _ = m.Update(tea.KeyPressMsg{Text: "0"})
	m = updated.(model)
	if m.filter != "" {
		t.Errorf("expected filter empty (all), got %q", m.filter)
	}
	if len(m.shown) != 30 {
		t.Errorf("expected 30 tasks for all filter, got %d", len(m.shown))
	}
}

func TestSearchNarrowsAndEscRestores(t *testing.T) {
	m := newTestModel(t)

	// Enter search mode with '/'
	updated, _ := m.Update(tea.KeyPressMsg{Text: "/"})
	m = updated.(model)
	if m.mode != modeSearch {
		t.Fatalf("expected modeSearch, got %v", m.mode)
	}

	// Type "task-05"
	for _, r := range "task-05" {
		updated, _ = m.Update(tea.KeyPressMsg{Text: string(r), Code: r})
		m = updated.(model)
	}
	if m.query != "task-05" {
		t.Errorf("expected query task-05, got %q", m.query)
	}
	if len(m.shown) != 1 {
		t.Fatalf("expected 1 shown task for query task-05, got %d", len(m.shown))
	}
	if m.tasks[m.shown[0]].ID != "task-05" {
		t.Errorf("expected task-05, got %s", m.tasks[m.shown[0]].ID)
	}

	// Esc restores query and exits search mode
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(model)
	if m.mode != modeTable {
		t.Errorf("expected modeTable after Esc, got %v", m.mode)
	}
	if m.query != "" {
		t.Errorf("expected empty query after Esc, got %q", m.query)
	}
	if len(m.shown) != 30 {
		t.Errorf("expected 30 tasks after Esc restores, got %d", len(m.shown))
	}
}

func TestPollMsgReorderAnd304(t *testing.T) {
	m := newTestModel(t)

	// Move cursor to row 2 (task-02)
	updated, _ := m.Update(tea.KeyPressMsg{Text: "j"})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyPressMsg{Text: "j"})
	m = updated.(model)
	sel, ok := m.selected()
	if !ok || sel.ID != "task-02" {
		t.Fatalf("expected selected task-02, got %v", sel.ID)
	}

	// Reverse task list so task-02 moves to a different index
	reversed := make([]task, len(m.tasks))
	for i := range m.tasks {
		reversed[i] = m.tasks[len(m.tasks)-1-i]
	}

	updated, _ = m.Update(pollMsg{
		tasks:    reversed,
		changed:  true,
		projects: []string{"proj0", "proj1", "proj2"},
	})
	m = updated.(model)

	// Cursor should stay on task-02
	selAfter, okAfter := m.selected()
	if !okAfter {
		t.Fatalf("expected task selected after reorder")
	}
	if selAfter.ID != "task-02" {
		t.Errorf("cursor should stay on task-02 after pollMsg reorders, got %s", selAfter.ID)
	}

	// 304 pollMsg (changed=false) does not touch tasks or cursor
	curIndex := m.cursor
	updated, _ = m.Update(pollMsg{
		tasks:   nil,
		changed: false,
	})
	m = updated.(model)
	if len(m.tasks) != 30 {
		t.Errorf("304 pollMsg should not clear tasks, len=%d", len(m.tasks))
	}
	if m.cursor != curIndex {
		t.Errorf("304 pollMsg should not touch cursor, expected %d, got %d", curIndex, m.cursor)
	}
}

func TestRefusals(t *testing.T) {
	m := newTestModel(t)

	// task-00 is pending
	m.cursor = 0
	m.clamp()

	// c on pending is allowed (cmd != nil)
	updated, cmd := m.Update(tea.KeyPressMsg{Text: "c"})
	m = updated.(model)
	if cmd == nil {
		t.Errorf("expected actCmd for claim on pending task")
	}

	// u on pending is refused: "task is not leased"
	updated, _ = m.Update(tea.KeyPressMsg{Text: "u"})
	m = updated.(model)
	if m.msg != "task is not leased" {
		t.Errorf("expected refusal 'task is not leased', got %q", m.msg)
	}

	// task-10 is leased (and actively leased since LeaseExpires is in future)
	m.cursor = 10
	m.clamp()

	// c on leased is refused: "task is not pending"
	updated, _ = m.Update(tea.KeyPressMsg{Text: "c"})
	m = updated.(model)
	if m.msg != "task is not pending" {
		t.Errorf("expected refusal 'task is not pending', got %q", m.msg)
	}

	// e on actively leased is refused: "cannot edit actively leased task"
	updated, _ = m.Update(tea.KeyPressMsg{Text: "e"})
	m = updated.(model)
	if m.msg != "cannot edit actively leased task" {
		t.Errorf("expected refusal 'cannot edit actively leased task', got %q", m.msg)
	}

	// D on actively leased is refused: "cannot delete actively leased task"
	updated, _ = m.Update(tea.KeyPressMsg{Text: "D"})
	m = updated.(model)
	if m.msg != "cannot delete actively leased task" {
		t.Errorf("expected refusal 'cannot delete actively leased task', got %q", m.msg)
	}

	// x on actively leased is refused: "cannot complete actively leased task"
	updated, _ = m.Update(tea.KeyPressMsg{Text: "x"})
	m = updated.(model)
	if m.msg != "cannot complete actively leased task" {
		t.Errorf("expected refusal 'cannot complete actively leased task', got %q", m.msg)
	}

	// task-20 is done
	m.cursor = 20
	m.clamp()

	// e on done is refused: "cannot edit done task"
	updated, _ = m.Update(tea.KeyPressMsg{Text: "e"})
	m = updated.(model)
	if m.msg != "cannot edit done task" {
		t.Errorf("expected refusal 'cannot edit done task', got %q", m.msg)
	}

	// x on done is refused: "task is already done"
	updated, _ = m.Update(tea.KeyPressMsg{Text: "x"})
	m = updated.(model)
	if m.msg != "task is already done" {
		t.Errorf("expected refusal 'task is already done', got %q", m.msg)
	}
}

func TestPriorityKeyMath(t *testing.T) {
	m := newTestModel(t)

	// task-00 has Priority == 0: '+' and '=' are no-ops
	m.cursor = 0
	m.clamp()
	sel, _ := m.selected()
	if sel.Priority != 0 {
		t.Fatalf("expected priority 0 for task-00, got %d", sel.Priority)
	}
	_, cmdPlus0 := m.Update(tea.KeyPressMsg{Text: "+"})
	if cmdPlus0 != nil {
		t.Errorf("+ on priority 0 should be no-op, got non-nil cmd")
	}
	_, cmdEq0 := m.Update(tea.KeyPressMsg{Text: "="})
	if cmdEq0 != nil {
		t.Errorf("= on priority 0 should be no-op, got non-nil cmd")
	}

	// task-01 has Priority == 1: '+' and '=' are no-ops because floor is 1
	m.cursor = 1
	m.clamp()
	sel, _ = m.selected()
	if sel.Priority != 1 {
		t.Fatalf("expected priority 1 for task-01, got %d", sel.Priority)
	}
	_, cmdPlus1 := m.Update(tea.KeyPressMsg{Text: "+"})
	if cmdPlus1 != nil {
		t.Errorf("+ on priority 1 should be no-op (floor 1), got non-nil cmd")
	}

	// task-03 has Priority == 3: '+' raises urgency to 2 (pri != Priority -> cmd != nil)
	m.cursor = 3
	m.clamp()
	sel, _ = m.selected()
	if sel.Priority != 3 {
		t.Fatalf("expected priority 3 for task-03, got %d", sel.Priority)
	}
	_, cmdPlus3 := m.Update(tea.KeyPressMsg{Text: "+"})
	if cmdPlus3 == nil {
		t.Errorf("+ on priority 3 should return an actCmd to set priority to 2")
	}

	// task-02 has Priority == 2: '-' lowers urgency to 3 (pri != Priority -> cmd != nil)
	m.cursor = 2
	m.clamp()
	sel, _ = m.selected()
	if sel.Priority != 2 {
		t.Fatalf("expected priority 2 for task-02, got %d", sel.Priority)
	}
	_, cmdMinus2 := m.Update(tea.KeyPressMsg{Text: "-"})
	if cmdMinus2 == nil {
		t.Errorf("- on priority 2 should return an actCmd to set priority to 3")
	}
}

func TestDetailViewportNotResetByTick(t *testing.T) {
	m := newTestModel(t)
	detailID := m.detailID
	if detailID == "" {
		t.Fatalf("expected initial detailID to be non-empty")
	}

	// Sending a tickMsg should not reset detail viewport or change detailID
	updated, _ := m.Update(tickMsg(time.Now()))
	m = updated.(model)
	if m.detailID != detailID {
		t.Errorf("tickMsg changed detailID from %q to %q", detailID, m.detailID)
	}
}

func TestTitleOfAndWorkerParts(t *testing.T) {
	// titleOf strips project prefix
	scope, title := titleOf(task{
		Project: "frontend",
		Body:    "frontend: Fix header layout\nLine 2 details",
	})
	if scope != "frontend" {
		t.Errorf("expected scope 'frontend', got %q", scope)
	}
	if title != "Fix header layout" {
		t.Errorf("expected title 'Fix header layout', got %q", title)
	}

	// titleOf without project prefix
	scope, title = titleOf(task{
		Project: "backend",
		Body:    "Database schema update",
	})
	if scope != "backend" {
		t.Errorf("expected scope 'backend', got %q", scope)
	}
	if title != "Database schema update" {
		t.Errorf("expected title 'Database schema update', got %q", title)
	}

	// workerParts with colon
	host, checkout := workerParts("myhost:mycheckout")
	if host != "myhost" || checkout != "mycheckout" {
		t.Errorf("workerParts('myhost:mycheckout') = (%q, %q), want ('myhost', 'mycheckout')", host, checkout)
	}

	// workerParts without colon
	host, checkout = workerParts("soloworker")
	if host != "" || checkout != "soloworker" {
		t.Errorf("workerParts('soloworker') = (%q, %q), want ('', 'soloworker')", host, checkout)
	}
}

// send applies one message through Update and casts the result back.
func send(t *testing.T, m model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	next, ok := updated.(model)
	if !ok {
		t.Fatalf("Update returned %T, want model", updated)
	}
	return next, cmd
}

func TestPollWithErrorStillAppliesChangedTasksAndKeepsCursorByID(t *testing.T) {
	m := newTestModel(t)
	m, _ = send(t, m, pollMsg{changed: false, stats: stats{Pending: 7, Total: 30}})
	m, _ = send(t, m, tea.KeyPressMsg{Text: "j"})
	m, _ = send(t, m, tea.KeyPressMsg{Text: "j"})
	if sel, ok := m.selected(); !ok || sel.ID != "task-02" {
		t.Fatalf("expected task-02 selected, got %q", sel.ID)
	}

	// The client stored the new ETag before the stats leg failed, so the
	// body must land even though the poll reports an error.
	reversed := make([]task, len(m.tasks))
	for i := range m.tasks {
		reversed[i] = m.tasks[len(m.tasks)-1-i]
	}
	m, _ = send(t, m, pollMsg{tasks: reversed, changed: true, err: fmt.Errorf("stats: connection refused")})

	sel, ok := m.selected()
	if !ok {
		t.Fatalf("expected a selected task after the failed poll")
	}
	if sel.ID != "task-02" {
		t.Errorf("cursor should follow task-02 across the reorder, got %q", sel.ID)
	}
	if m.tasks[0].ID != "task-29" {
		t.Errorf("failed poll dropped the new list: tasks[0] = %q", m.tasks[0].ID)
	}
	if m.connected {
		t.Errorf("failed poll should mark the model disconnected")
	}
	if m.lastErr != "stats: connection refused" {
		t.Errorf("lastErr = %q, want the poll error", m.lastErr)
	}
	if m.stats.Pending != 7 {
		t.Errorf("failed poll should keep the last good stats, got %+v", m.stats)
	}
	if len(m.projects) != 3 {
		t.Errorf("failed poll should keep the last good projects, got %v", m.projects)
	}
}

func TestInitTicksAndOnlyOnePollIsInFlightAtATime(t *testing.T) {
	m := newModel(config{refresh: time.Second}, nil)
	if m.polling {
		t.Fatalf("nothing is in flight before Init")
	}
	msg := m.Init()()
	if _, ok := msg.(tickMsg); !ok {
		t.Fatalf("Init must produce a tickMsg, got %T", msg)
	}
	m, cmd := send(t, m, msg)
	if !m.polling || cmd == nil {
		t.Fatalf("first tick must start a poll")
	}
	m, _ = send(t, m, tickMsg(time.Now()))
	if !m.polling {
		t.Errorf("tick before the pollMsg must not start a second poll")
	}
	m, _ = send(t, m, pollMsg{tasks: []task{{ID: "task-00", Status: "pending"}}, etag: `"e1"`, changed: true})
	if m.polling {
		t.Fatalf("pollMsg must clear the in-flight flag")
	}
	if m.etag != `"e1"` {
		t.Fatalf("model must keep the tag of the list it holds, got %q", m.etag)
	}
	m, cmd = send(t, m, tickMsg(time.Now()))
	if !m.polling || cmd == nil {
		t.Fatalf("next tick must poll again")
	}
}

func TestDeleteKeyOpensConfirmWhereEnterCancelsAndYConfirms(t *testing.T) {
	m := newTestModel(t)
	confirming, _ := send(t, m, tea.KeyPressMsg{Text: "D"})
	if confirming.mode != modeConfirm {
		t.Fatalf("D should open the confirm overlay, got mode %v", confirming.mode)
	}
	if confirming.confirm.button != "delete" || confirming.confirm.method != "DELETE" {
		t.Fatalf("unexpected confirm target: %+v", confirming.confirm)
	}

	cancels := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}},
		{"n", tea.KeyPressMsg{Text: "n"}},
		{"esc", tea.KeyPressMsg{Code: tea.KeyEscape}},
		{"q", tea.KeyPressMsg{Text: "q"}},
	}
	for _, c := range cancels {
		after, cmd := send(t, confirming, c.key)
		if after.mode != modeTable {
			t.Errorf("%s should close the confirm overlay, got mode %v", c.name, after.mode)
		}
		if cmd != nil {
			t.Errorf("%s must not run the destructive action", c.name)
		}
	}

	for _, key := range []tea.KeyPressMsg{{Text: "y"}, {Text: "Y"}} {
		after, cmd := send(t, confirming, key)
		if after.mode != modeTable {
			t.Errorf("%s should close the confirm overlay, got mode %v", key.Text, after.mode)
		}
		if cmd == nil {
			t.Errorf("%s must run the destructive action", key.Text)
		}
	}
}

func TestCompleteKeyOnPendingTaskOpensCompleteConfirm(t *testing.T) {
	m := newTestModel(t)
	m, cmd := send(t, m, tea.KeyPressMsg{Text: "x"})
	if m.mode != modeConfirm {
		t.Fatalf("x on a pending task should open the confirm overlay, got mode %v", m.mode)
	}
	if cmd != nil {
		t.Errorf("x must not act before the overlay is confirmed")
	}
	if m.confirm.button != "complete" {
		t.Errorf("confirm button = %q, want %q", m.confirm.button, "complete")
	}
	if m.confirm.path != "/tasks/task-00/close" {
		t.Errorf("confirm path = %q", m.confirm.path)
	}
}

func TestHelpOverlayQuitsOnCtrlCAndClosesOnAnyOtherKey(t *testing.T) {
	m := newTestModel(t)
	help, _ := send(t, m, tea.KeyPressMsg{Text: "?"})
	if help.mode != modeHelp {
		t.Fatalf("? should open help, got mode %v", help.mode)
	}

	_, cmd := send(t, help, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatalf("ctrl-c in help must quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("ctrl-c in help produced %T, want tea.QuitMsg", cmd())
	}

	closed, cmd := send(t, help, tea.KeyPressMsg{Text: "j"})
	if closed.mode != modeTable {
		t.Errorf("any other key should close help, got mode %v", closed.mode)
	}
	if cmd != nil {
		t.Errorf("closing help should not emit a command")
	}
}

func TestSearchEditingKeysTrimQueryAndEnterKeepsIt(t *testing.T) {
	m := newTestModel(t)
	m, _ = send(t, m, tea.KeyPressMsg{Text: "/"})
	for _, r := range "task 05" {
		m, _ = send(t, m, tea.KeyPressMsg{Text: string(r), Code: r})
	}
	if m.query != "task 05" {
		t.Fatalf("typed query = %q", m.query)
	}

	m, _ = send(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.query != "task 0" {
		t.Errorf("backspace should drop one rune, got %q", m.query)
	}

	m, _ = send(t, m, tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	if m.query != "task " {
		t.Errorf("ctrl-w should drop the last word, got %q", m.query)
	}

	m, _ = send(t, m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if m.query != "" {
		t.Errorf("ctrl-u should clear the query, got %q", m.query)
	}
	if len(m.shown) != 30 {
		t.Errorf("cleared query should show all 30 tasks, got %d", len(m.shown))
	}

	for _, r := range "task-1" {
		m, _ = send(t, m, tea.KeyPressMsg{Text: string(r), Code: r})
	}
	m, cmd := send(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != modeTable {
		t.Errorf("Enter should leave search mode, got %v", m.mode)
	}
	if cmd != nil {
		t.Errorf("Enter in search should not emit a command")
	}
	if m.query != "task-1" {
		t.Errorf("Enter should keep the query, got %q", m.query)
	}
	if len(m.shown) != 10 {
		t.Errorf("query task-1 should keep 10 matches, got %d", len(m.shown))
	}
}

func TestTabAndZoomEnterDetailModesAndEscReturnsToTable(t *testing.T) {
	m := newTestModel(t)

	detail, _ := send(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	if detail.mode != modeDetail {
		t.Fatalf("Tab should enter detail mode, got %v", detail.mode)
	}
	back, _ := send(t, detail, tea.KeyPressMsg{Code: tea.KeyEscape})
	if back.mode != modeTable {
		t.Errorf("Esc should leave detail mode, got %v", back.mode)
	}

	zoom, _ := send(t, m, tea.KeyPressMsg{Text: "z"})
	if zoom.mode != modeZoom {
		t.Fatalf("z should enter zoom mode, got %v", zoom.mode)
	}
	back, _ = send(t, zoom, tea.KeyPressMsg{Code: tea.KeyEscape})
	if back.mode != modeTable {
		t.Errorf("Esc should leave zoom mode, got %v", back.mode)
	}
}

func TestNewKeyOpensFormAndEscCloses(t *testing.T) {
	m := newTestModel(t)
	form, _ := send(t, m, tea.KeyPressMsg{Text: "n"})
	if form.mode != modeForm {
		t.Fatalf("n should open the create form, got mode %v", form.mode)
	}
	closed, _ := send(t, form, tea.KeyPressMsg{Code: tea.KeyEscape})
	if closed.mode != modeTable {
		t.Errorf("Esc should close the form, got mode %v", closed.mode)
	}
}

func TestYankKeysCopySelectedTask(t *testing.T) {
	m := newTestModel(t)
	if _, cmd := send(t, m, tea.KeyPressMsg{Text: "y"}); cmd == nil {
		t.Errorf("y should copy the task id")
	}
	if _, cmd := send(t, m, tea.KeyPressMsg{Text: "Y"}); cmd == nil {
		t.Errorf("Y should copy the task body")
	}
}

func TestMouseWheelMovesCursorByThreeAndClamps(t *testing.T) {
	m := newTestModel(t)
	m, _ = send(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if m.cursor != 3 {
		t.Errorf("wheel down should move three rows, got %d", m.cursor)
	}
	m, _ = send(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	m, _ = send(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if m.cursor != 0 {
		t.Errorf("wheel up should clamp at the first row, got %d", m.cursor)
	}
	for range 15 {
		m, _ = send(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	}
	if m.cursor != 29 {
		t.Errorf("wheel down should clamp at the last row, got %d", m.cursor)
	}
}

func TestMouseClickSelectsTheClickedTableRow(t *testing.T) {
	m := newTestModel(t)
	bandTop := headerRows + tabRows + 1 + colHeadRows

	m, _ = send(t, m, tea.MouseClickMsg{Button: tea.MouseLeft, Y: bandTop + 7})
	if m.cursor != 7 {
		t.Errorf("click on the eighth row should select it, got cursor %d", m.cursor)
	}

	above, _ := send(t, m, tea.MouseClickMsg{Button: tea.MouseLeft, Y: bandTop - 1})
	if above.cursor != 7 {
		t.Errorf("click above the table should not move the cursor, got %d", above.cursor)
	}

	below, _ := send(t, m, tea.MouseClickMsg{Button: tea.MouseLeft, Y: bandTop + m.tableRows()})
	if below.cursor != 7 {
		t.Errorf("click below the table should not move the cursor, got %d", below.cursor)
	}
}

func TestPageAndHomeEndKeysClampAtBothEnds(t *testing.T) {
	m := newTestModel(t)

	m, _ = send(t, m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.cursor != 0 {
		t.Errorf("PgUp at the top should stay at row 0, got %d", m.cursor)
	}

	m, _ = send(t, m, tea.KeyPressMsg{Code: tea.KeyEnd})
	if m.cursor != 29 {
		t.Errorf("End should select the last row, got %d", m.cursor)
	}

	m, _ = send(t, m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.cursor != 29 {
		t.Errorf("PgDown at the bottom should stay at row 29, got %d", m.cursor)
	}

	m, _ = send(t, m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	step := m.tableRows() / 2
	if step < 1 {
		step = 1
	}
	if m.cursor != 29-step {
		t.Errorf("PgUp should move half a page up to %d, got %d", 29-step, m.cursor)
	}

	m, _ = send(t, m, tea.KeyPressMsg{Code: tea.KeyHome})
	if m.cursor != 0 || m.offset != 0 {
		t.Errorf("Home should return to the top, got cursor %d offset %d", m.cursor, m.offset)
	}
}

func TestProjectKeyCyclesEveryProjectAndBackToAll(t *testing.T) {
	m := newTestModel(t)
	if m.project != "" {
		t.Fatalf("expected the all-projects view, got %q", m.project)
	}
	for _, want := range []string{"proj0", "proj1", "proj2", ""} {
		m, _ = send(t, m, tea.KeyPressMsg{Text: "p"})
		if m.project != want {
			t.Fatalf("p should select %q, got %q", want, m.project)
		}
		if want != "" {
			for _, idx := range m.shown {
				if m.tasks[idx].Project != want {
					t.Fatalf("project %q shows task from %q", want, m.tasks[idx].Project)
				}
			}
		} else if len(m.shown) != 30 {
			t.Fatalf("all-projects view should show 30 tasks, got %d", len(m.shown))
		}
	}
}

func TestFailedActionShowsErrorInFooter(t *testing.T) {
	m := newTestModel(t)
	m, _ = send(t, m, actMsg{err: fmt.Errorf("409 conflict")})
	if m.msg != "error: 409 conflict" {
		t.Errorf("footer message = %q, want %q", m.msg, "error: 409 conflict")
	}
}

func TestStaleClearMsgLeavesTheNewerMessageAlone(t *testing.T) {
	m := newTestModel(t)
	m, _ = send(t, m, actMsg{msg: "first"})
	staleID := m.msgID
	m, _ = send(t, m, actMsg{msg: "second"})

	m, _ = send(t, m, clearMsgMsg{id: staleID})
	if m.msg != "second" {
		t.Errorf("stale clear wiped the newer message, msg = %q", m.msg)
	}

	m, _ = send(t, m, clearMsgMsg{id: m.msgID})
	if m.msg != "" {
		t.Errorf("current clear should empty the message, got %q", m.msg)
	}
}

func TestRefreshKeyStartsAPollOnlyWhenNoneIsInFlight(t *testing.T) {
	m := newTestModel(t)
	m, cmd := send(t, m, tea.KeyPressMsg{Text: "r"})
	if cmd == nil {
		t.Fatalf("r should start a poll")
	}
	if !m.polling {
		t.Errorf("r should mark the poll in flight")
	}
	if _, cmd = send(t, m, tea.KeyPressMsg{Text: "r"}); cmd != nil {
		t.Errorf("r should not stack a second poll")
	}
}

func TestQuitKeysQuitFromTheTable(t *testing.T) {
	m := newTestModel(t)
	for _, key := range []tea.KeyPressMsg{{Text: "q"}, {Code: 'c', Mod: tea.ModCtrl}} {
		_, cmd := send(t, m, key)
		if cmd == nil {
			t.Fatalf("%v should quit", key)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%v produced %T, want tea.QuitMsg", key, cmd())
		}
	}
}

func TestPollReplyForAnotherProjectIsDropped(t *testing.T) {
	m := newModel(config{refresh: time.Second}, nil)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = send(t, m, pollMsg{project: "", tasks: []task{{ID: "a1", Project: "alpha", Status: "pending"}}, etag: `"alpha"`, changed: true, projects: []string{"alpha", "beta"}})
	m, _ = send(t, m, tea.KeyPressMsg{Code: 'p', Text: "p"}) // -> alpha
	m, _ = send(t, m, tea.KeyPressMsg{Code: 'p', Text: "p"}) // -> beta
	if m.project != "beta" {
		t.Fatalf("project = %q, want beta", m.project)
	}
	m, _ = send(t, m, pollMsg{project: "alpha", tasks: []task{{ID: "a2", Project: "alpha", Status: "pending"}}, etag: `"alpha-v2"`, changed: true})
	if m.polling || m.etag != "" || len(m.tasks) != 1 || m.tasks[0].ID != "a1" {
		t.Fatalf("reply for alpha must not touch a beta model: polling=%v etag=%q tasks=%v", m.polling, m.etag, m.tasks)
	}
	m, _ = send(t, m, pollMsg{project: "beta", tasks: []task{{ID: "b1", Project: "beta", Status: "pending"}}, etag: `"beta"`, changed: true})
	if m.etag != `"beta"` || len(m.shown) != 1 {
		t.Fatalf("beta reply must apply: etag=%q shown=%d", m.etag, len(m.shown))
	}
}

func TestTabsShowDashUntilStatsArrive(t *testing.T) {
	m := newModel(config{refresh: time.Second}, nil)
	m.glyph = asciiGlyphs
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	line := strings.Split(ansi.Strip(m.View().Content), "\n")[1]
	if !strings.Contains(line, "[0 all] -") || !strings.Contains(line, "1 pending -") {
		t.Fatalf("tabs before the first poll: %q", line)
	}
	m, _ = send(t, m, pollMsg{stats: stats{Total: 3, Pending: 3}})
	line = strings.Split(ansi.Strip(m.View().Content), "\n")[1]
	if !strings.Contains(line, "0 all 3") {
		t.Fatalf("tabs after stats: %q", line)
	}
}
