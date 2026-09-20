package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func sampleTasksForMouse() []task {
	body := strings.Repeat("line in task body\n", 30)
	tasks := make([]task, 5)
	for i := range tasks {
		tasks[i] = task{
			ID:       string(rune('a' + i)),
			Body:     body,
			Status:   "pending",
			Priority: 1,
		}
	}
	return tasks
}

func setupTestModel() model {
	m := newModel(config{icons: false, refresh: time.Hour}, nil)
	m.width = 100
	m.height = 24
	tasks := sampleTasksForMouse()
	m.tasks = tasks
	m.shown = []int{0, 1, 2, 3, 4}
	m.cursor = 0
	m.mode = modeTable
	m.stats = stats{
		Total:   5,
		Pending: 5,
		Leased:  0,
		Done:    0,
		Buried:  1,
	}
	m.hasStats = true
	m.syncDetail()
	return m
}

func TestMouseWheelHoverRouting(t *testing.T) {
	m := setupTestModel()
	panes := m.panes()

	if panes.detailRows <= 0 {
		t.Fatalf("expected detailRows > 0, got %d", panes.detailRows)
	}

	res, _ := m.Update(tea.MouseWheelMsg{
		X:      10,
		Y:      panes.detailTop + 2,
		Button: tea.MouseWheelDown,
	})
	m = res.(model)

	if m.cursor != 0 {
		t.Fatalf("cursor moved to %d; expected cursor to stay 0 on detail hover scroll", m.cursor)
	}
	if m.detail.YOffset() == 0 {
		t.Fatalf("expected detail viewport to scroll down, got YOffset 0")
	}

	offsetAfterDown := m.detail.YOffset()
	res, _ = m.Update(tea.MouseWheelMsg{
		X:      10,
		Y:      panes.detailTop + 2,
		Button: tea.MouseWheelUp,
	})
	m = res.(model)

	if m.cursor != 0 {
		t.Fatalf("cursor moved to %d on scroll up", m.cursor)
	}
	if m.detail.YOffset() >= offsetAfterDown {
		t.Fatalf("expected detail viewport to scroll up, got offset %d", m.detail.YOffset())
	}

	res, _ = m.Update(tea.MouseWheelMsg{
		X:      10,
		Y:      panes.tableTop + 1,
		Button: tea.MouseWheelDown,
	})
	m = res.(model)

	if m.cursor == 0 {
		t.Fatalf("expected table cursor to move on table scroll down, got 0")
	}

	m.mode = modeDetail
	m.cursor = 0
	res, _ = m.Update(tea.MouseWheelMsg{
		X:      10,
		Y:      panes.tableTop + 1,
		Button: tea.MouseWheelDown,
	})
	m = res.(model)

	if m.cursor == 0 {
		t.Fatalf("expected table cursor to move when scrolling over table in modeDetail, got 0")
	}
}

func TestMouseInteraction(t *testing.T) {
	m := setupTestModel()
	panes := m.panes()

	res, _ := m.Update(tea.MouseClickMsg{
		X:      5,
		Y:      panes.detailTop + 1,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.mode != modeDetail {
		t.Fatalf("expected modeDetail after click on detail pane, got %v", m.mode)
	}

	targetRow := 2
	res, _ = m.Update(tea.MouseClickMsg{
		X:      5,
		Y:      panes.tableTop + targetRow,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.mode != modeTable {
		t.Fatalf("expected modeTable after clicking table row, got %v", m.mode)
	}
	if m.cursor != targetRow {
		t.Fatalf("expected cursor %d after clicking row, got %d", targetRow, m.cursor)
	}
}

func TestClickFilterTabs(t *testing.T) {
	m := setupTestModel()
	m.projects = []string{"alpha", "beta"}

	renderedLines := strings.Split(m.View().Content, "\n")
	if len(renderedLines) < 2 {
		t.Fatalf("expected at least 2 lines rendered, got %d", len(renderedLines))
	}
	tabLineClean := ansi.Strip(renderedLines[1])

	bounds := m.row1Bounds()
	tabDefs := m.tabDefs()
	for i, target := range bounds.tabs {
		if target.end > len(tabLineClean) {
			t.Fatalf("tab %q end %d out of bounds for tab line %q", target.filter, target.end, tabLineClean)
		}
		tabSub := tabLineClean[target.start:target.end]
		def := tabDefs[i]
		if !strings.Contains(tabSub, def.name) || !strings.Contains(tabSub, def.key) {
			t.Fatalf("expected tab %q (%s) in slice %v, got %q in line %q", def.name, def.key, target, tabSub, tabLineClean)
		}
	}

	projSub := tabLineClean[bounds.proj[0]:bounds.proj[1]]
	if !strings.Contains(projSub, "project") || !strings.Contains(projSub, "p") {
		t.Fatalf("expected project label within %v, got %q in full line %q", bounds.proj, projSub, tabLineClean)
	}
	workerSub := tabLineClean[bounds.worker[0]:bounds.worker[1]]
	if !strings.Contains(workerSub, "worker") || !strings.Contains(workerSub, "w") {
		t.Fatalf("expected worker label within %v, got %q in full line %q", bounds.worker, workerSub, tabLineClean)
	}

	clickPoints := []struct {
		x          int
		wantFilter string
	}{
		{x: 12, wantFilter: "pending"},
		{x: 25, wantFilter: "leased"},
		{x: 37, wantFilter: "done"},
		{x: 47, wantFilter: "buried"},
		{x: 2, wantFilter: ""},
	}

	for _, cp := range clickPoints {
		res, _ := m.Update(tea.MouseClickMsg{
			X:      cp.x,
			Y:      1,
			Button: tea.MouseLeft,
		})
		m = res.(model)

		if m.filter != cp.wantFilter {
			t.Fatalf("expected filter %q after clicking at column %d, got %q", cp.wantFilter, cp.x, m.filter)
		}
	}

	m.mode = modeDetail
	res, _ := m.Update(tea.MouseClickMsg{
		X:      25,
		Y:      1,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.mode != modeTable {
		t.Fatalf("expected click on tab to switch from modeDetail to modeTable, got %v", m.mode)
	}
	if m.filter != "leased" {
		t.Fatalf("expected filter leased, got %q", m.filter)
	}

	res, _ = m.Update(tea.MouseClickMsg{
		X:      bounds.proj[0] + 5,
		Y:      1,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.project != "alpha" {
		t.Fatalf("expected project alpha after click on project, got %q", m.project)
	}

	res, _ = m.Update(tea.MouseClickMsg{
		X:      bounds.proj[0] + 5,
		Y:      1,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.project != "beta" {
		t.Fatalf("expected project beta after second click on project, got %q", m.project)
	}

	res, _ = m.Update(tea.MouseClickMsg{
		X:      bounds.proj[0] + 5,
		Y:      1,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.project != "" {
		t.Fatalf("expected empty project after third click on project, got %q", m.project)
	}
	res, _ = m.Update(tea.MouseClickMsg{
		X:      bounds.proj[1] + 1,
		Y:      1,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.project != "" {
		t.Fatalf("click in gap between project and worker should not cycle project, got %q", m.project)
	}
}

func TestClickScrollbarTrack(t *testing.T) {
	m := setupTestModel()
	totalTasks := 40
	tasks := make([]task, totalTasks)
	for i := range tasks {
		tasks[i] = task{
			ID:       string(rune('a' + (i % 26))),
			Body:     "line\n",
			Status:   "pending",
			Priority: 1,
		}
	}
	m.tasks = tasks
	shown := make([]int, totalTasks)
	for i := range shown {
		shown[i] = i
	}
	m.shown = shown
	m.cursor = 0
	m.offset = 0
	m.clamp()

	panes := m.panes()
	tRows := panes.tableRows
	if len(m.shown) <= tRows {
		t.Fatalf("expected shown (%d) > tableRows (%d)", len(m.shown), tRows)
	}

	sb := tableScrollbar(len(m.shown), m.offset, tRows)
	if !sb.hasScrollbar {
		t.Fatalf("expected scrollbar for 40 tasks in %d rows", tRows)
	}

	step := tRows / 2
	if step < 1 {
		step = 1
	}

	clickBelowY := panes.tableTop + sb.thumbStart + sb.thumbSize + 1
	if clickBelowY >= panes.tableTop+tRows {
		clickBelowY = panes.tableTop + tRows - 1
	}

	res, _ := m.Update(tea.MouseClickMsg{
		X:      m.width - 1,
		Y:      clickBelowY,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.offset != step {
		t.Fatalf("expected offset %d after clicking below thumb, got offset %d", step, m.offset)
	}

	res, _ = m.Update(tea.MouseClickMsg{
		X:      m.width - 1,
		Y:      clickBelowY,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.offset != 2*step {
		t.Fatalf("expected offset %d after second click below thumb, got %d", 2*step, m.offset)
	}

	sbAfterDown := tableScrollbar(len(m.shown), m.offset, tRows)
	if sbAfterDown.thumbStart <= sb.thumbStart {
		t.Fatalf("expected thumb to advance down, was %d, now %d", sb.thumbStart, sbAfterDown.thumbStart)
	}

	clickAboveY := panes.tableTop + sbAfterDown.thumbStart - 1
	res, _ = m.Update(tea.MouseClickMsg{
		X:      m.width - 1,
		Y:      clickAboveY,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.offset != step {
		t.Fatalf("expected offset %d after clicking above thumb, got %d", step, m.offset)
	}

	m.offset = 16
	m.cursor = 16
	m.clamp()
	sbMid := tableScrollbar(len(m.shown), m.offset, tRows)
	thumbCenterY := panes.tableTop + sbMid.thumbStart + sbMid.thumbSize/2
	res, _ = m.Update(tea.MouseClickMsg{
		X:      m.width - 1,
		Y:      thumbCenterY,
		Button: tea.MouseLeft,
	})
	m = res.(model)
	if m.offset != 16 {
		t.Fatalf("expected offset 16 to remain unchanged on thumb click, got %d", m.offset)
	}
	sbCenter := tableScrollbar(len(m.shown), m.offset, tRows)
	clickRow := thumbCenterY - panes.tableTop
	if clickRow < sbCenter.thumbStart || clickRow >= sbCenter.thumbStart+sbCenter.thumbSize {
		t.Fatalf("thumb ran away from click: clickRow %d not in [%d, %d)",
			clickRow, sbCenter.thumbStart, sbCenter.thumbStart+sbCenter.thumbSize)
	}

	m.mode = modeDetail
	res, _ = m.Update(tea.MouseClickMsg{
		X:      m.width - 1,
		Y:      thumbCenterY,
		Button: tea.MouseLeft,
	})
	m = res.(model)
	if m.mode != modeTable {
		t.Fatalf("expected modeTable after scrollbar click in modeDetail, got %v", m.mode)
	}

	shortModel := setupTestModel()
	shortPanes := shortModel.panes()
	if len(shortModel.shown) <= shortPanes.tableRows {
		res, _ = shortModel.Update(tea.MouseClickMsg{
			X:      shortModel.width - 1,
			Y:      shortPanes.tableTop + 2,
			Button: tea.MouseLeft,
		})
		shortModel = res.(model)
		if shortModel.cursor != 2 {
			t.Fatalf("expected row 2 selected when no scrollbar, got cursor %d", shortModel.cursor)
		}
	}
}
