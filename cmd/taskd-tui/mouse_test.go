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
