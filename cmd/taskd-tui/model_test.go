package main

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
