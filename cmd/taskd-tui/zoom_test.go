package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func screenLines(sim tcell.SimulationScreen, width, height int) []string {
	lines := make([]string, height)
	for y := 0; y < height; y++ {
		var row []rune
		for x := 0; x < width; x++ {
			r, _, _, _ := sim.GetContent(x, y)
			row = append(row, r)
		}
		lines[y] = string(row)
	}
	return lines
}

func countScreenMatches(sim tcell.SimulationScreen, width, height int, substr string) int {
	var count int
	for _, line := range screenLines(sim, width, height) {
		if strings.Contains(line, substr) {
			count++
		}
	}
	return count
}

func TestZoomTaskBody(t *testing.T) {
	u, tasksPtr, mu := stub(t)

	var b strings.Builder
	b.WriteString("rebuild manifest #4\n\n")
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&b, "Paragraph %d\ncontent line %d\n\n", i, i)
	}
	b.WriteString("last line 39")
	bodyText := b.String()
	lines := strings.Split(bodyText, "\n")
	if len(lines) != 39 {
		t.Fatalf("expected 39 body lines, got %d", len(lines))
	}

	taskList := make([]task, 25)
	taskList[0] = task{
		ID:        "task0001",
		Project:   "proj-a",
		Status:    "leased",
		Worker:    "w1",
		AssetPath: "a.blend",
		Priority:  1,
		Body:      bodyText,
	}
	for i := 1; i < 25; i++ {
		taskList[i] = task{
			ID:       fmt.Sprintf("task%04d", i+1),
			Project:  "proj-a",
			Status:   "pending",
			Priority: 2,
			Body:     fmt.Sprintf("task %d", i+1),
		}
	}

	mu.Lock()
	*tasksPtr = taskList
	mu.Unlock()

	ts, err := u.fetch("", "")
	if err != nil {
		t.Fatal(err)
	}
	u.render(ts)

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	u.app.SetScreen(sim)

	finished := make(chan struct{})
	go func() {
		u.app.Run()
		close(finished)
	}()
	defer func() {
		u.app.Stop()
		<-finished
	}()

	query := func(fn func()) {
		ch := make(chan struct{})
		u.app.QueueUpdate(func() {
			fn()
			close(ch)
		})
		<-ch
	}

	u.app.QueueUpdateDraw(func() {
		sim.SetSize(80, 24)
	})

	press := func(r rune) {
		u.app.QueueUpdateDraw(func() {
			u.keys(tcell.NewEventKey(tcell.KeyRune, r, 0))
		})
	}
	pressKey := func(k tcell.Key) {
		u.app.QueueUpdateDraw(func() {
			if u.app.GetFocus() == u.body {
				if ret := u.bodyKeys(tcell.NewEventKey(k, 0, 0)); ret != nil {
					u.body.InputHandler()(ret, nil)
				}
			} else {
				if ret := u.keys(tcell.NewEventKey(k, 0, 0)); ret != nil {
					u.table.InputHandler()(ret, nil)
				}
			}
		})
	}
	pressBodyRune := func(r rune) {
		u.app.QueueUpdateDraw(func() {
			if ret := u.bodyKeys(tcell.NewEventKey(tcell.KeyRune, r, 0)); ret != nil {
				u.body.InputHandler()(ret, nil)
			}
		})
	}

	eventually(t, func() bool {
		var ready bool
		query(func() {
			ready = u.selectedRow() == 1
		})
		return ready
	})

	var initialMatches int
	var screenRow23 string
	query(func() {
		initialMatches = countScreenMatches(sim, 80, 24, "Paragraph")
		rows := screenLines(sim, 80, 24)
		screenRow23 = rows[23]
	})
	if initialMatches != 0 {
		t.Fatalf("expected 0 Paragraph matches initially, got %d", initialMatches)
	}
	if !strings.Contains(screenRow23, "[z] zoom") || !strings.Contains(screenRow23, "[+/-] pri") || !strings.Contains(screenRow23, "[D] del") {
		t.Fatalf("screen footer missing [z] zoom, [+/-] pri, or [D] del: %q", screenRow23)
	}

	press('z')

	eventually(t, func() bool {
		var zoomed bool
		var pCount int
		var focusMatch bool
		var hasUnzoomFooter bool
		query(func() {
			zoomed = u.zoomed
			pCount = countScreenMatches(sim, 80, 24, "Paragraph")
			focusMatch = u.app.GetFocus() == u.body
			rows := screenLines(sim, 80, 24)
			hasUnzoomFooter = strings.Contains(rows[23], "[z/Esc] unzoom")
		})
		return zoomed && pCount >= 3 && focusMatch && hasUnzoomFooter
	})

	var zoomedMatches int
	var bodyH int
	query(func() {
		zoomedMatches = countScreenMatches(sim, 80, 24, "Paragraph")
		_, _, _, bodyH = u.body.GetInnerRect()
		rows := screenLines(sim, 80, 24)
		screenRow23 = rows[23]
	})
	if zoomedMatches < 3 {
		t.Fatalf("expected >= 3 Paragraph lines visible when zoomed, got %d", zoomedMatches)
	}
	if bodyH != 23 {
		t.Fatalf("expected zoomed body inner height 23, got %d", bodyH)
	}
	if !strings.Contains(screenRow23, "[z/Esc] unzoom") {
		t.Fatalf("screen row 23 missing [z/Esc] unzoom footer: %q", screenRow23)
	}

	pressKey(tcell.KeyPgDn)

	eventually(t, func() bool {
		var scrolled bool
		query(func() {
			for _, line := range screenLines(sim, 80, 24) {
				if strings.Contains(line, "Paragraph 10") {
					scrolled = true
					break
				}
			}
		})
		return scrolled
	})

	pressBodyRune('z')

	eventually(t, func() bool {
		var unzoomed bool
		var focusTable bool
		var selRow int
		var hasNormalFooter bool
		query(func() {
			unzoomed = !u.zoomed
			focusTable = u.app.GetFocus() == u.table
			selRow = u.selectedRow()
			rows := screenLines(sim, 80, 24)
			hasNormalFooter = strings.Contains(rows[23], "[z] zoom")
		})
		return unzoomed && focusTable && selRow == 1 && hasNormalFooter
	})

	press('z')
	eventually(t, func() bool {
		var zoomed bool
		query(func() { zoomed = u.zoomed })
		return zoomed
	})

	pressKey(tcell.KeyEscape)

	eventually(t, func() bool {
		var unzoomed bool
		var focusTable bool
		var selRow int
		query(func() {
			unzoomed = !u.zoomed
			focusTable = u.app.GetFocus() == u.table
			selRow = u.selectedRow()
		})
		return unzoomed && focusTable && selRow == 1
	})

	var modal *tview.Modal
	press('?')
	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal != nil
	})
	modalLines := strings.Join(screenLines(sim, 80, 24), "\n")
	if !strings.Contains(modalLines, "[z] zoom task body") {
		t.Fatalf("help modal missing [z] zoom task body: %q", modalLines)
	}
	u.app.QueueUpdateDraw(func() {
		u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), nil)
	})
	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal == nil
	})

	var buf bytes.Buffer
	printUsage(&buf)
	helpText := buf.String()
	if !strings.Contains(helpText, "z") || !strings.Contains(strings.ToLower(helpText), "zoom") {
		t.Fatalf("help text missing z zoom shortcut:\n%s", helpText)
	}
}
