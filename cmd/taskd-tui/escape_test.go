package main

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// tagTasks carry style-tag-looking text in every string the table renders.
var tagTasks = []task{
	{ID: "aaaaaaa1", Project: "[proj]", Status: "leased", Worker: "[w1]",
		LeaseExpires: 1 << 40, Body: "[URGENT] rebuild index\nsecond line"},
	{ID: "bbbbbbb2", Project: "demo", Status: "pending",
		AssetPath: "[stage] asset.gltf"},
}

func TestEscapeTableCells(t *testing.T) {
	u := newUI("http://127.0.0.1:1", "", false)
	u.render(tagTasks)

	for _, tc := range []struct {
		name     string
		row, col int
		want     string
	}{
		{"title", 1, 7, "[URGENT] rebuild index"},
		{"project", 1, 2, "[proj]"},
		{"worker", 1, 4, "[w1]"},
		{"asset title", 2, 7, "[stage] asset.gltf"},
	} {
		cell := u.table.GetCell(tc.row, tc.col)
		if got := tview.Unescape(cell.Text); got != tc.want {
			t.Errorf("%s cell = %q, unescapes to %q, want %q", tc.name, cell.Text, got, tc.want)
		}
		if got := tview.TaggedStringWidth(cell.Text); got != len(tc.want) {
			t.Errorf("%s cell %q renders %d columns, want %d", tc.name, cell.Text, got, len(tc.want))
		}
	}
}

// TestEscapeOnScreen is the pty reproduction: the marker has to survive all
// the way onto the terminal, both in the table row and in the delete prompt.
func TestEscapeOnScreen(t *testing.T) {
	u := newUI("http://127.0.0.1:1", "", false)
	u.render(tagTasks)

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(200, 30)
	u.app.SetScreen(sim)
	u.app.SetRoot(u.pages, true)

	done := make(chan struct{})
	go func() {
		u.app.Run()
		close(done)
	}()
	defer func() {
		u.app.Stop()
		<-done
	}()

	query := func(fn func()) {
		ch := make(chan struct{})
		u.app.QueueUpdate(func() {
			fn()
			close(ch)
		})
		<-ch
	}

	screenText := func() string {
		var text string
		query(func() {
			cells, w, h := sim.GetContents()
			var sb strings.Builder
			for y := range h {
				for x := range w {
					if c := cells[y*w+x]; len(c.Runes) > 0 {
						sb.WriteRune(c.Runes[0])
					} else {
						sb.WriteByte(' ')
					}
				}
				sb.WriteByte('\n')
			}
			text = sb.String()
		})
		return text
	}

	u.app.QueueUpdateDraw(func() {})
	row := lineWith(screenText(), "aaaaaaa ")
	if !strings.Contains(row, "[URGENT] rebuild index") {
		t.Fatalf("table row lost its marker: %q", row)
	}

	before := strings.Count(screenText(), "[URGENT]")
	u.app.QueueUpdateDraw(func() { u.showDeleteConfirm(tagTasks[0]) })
	if after := strings.Count(screenText(), "[URGENT]"); after != before+1 {
		t.Fatalf("delete prompt lost its marker, %d markers on screen, want %d:\n%s",
			after, before+1, screenText())
	}
	query(func() { u.pages.RemovePage("delete") })
}

// lineWith returns the first screen line containing needle, or "".
func lineWith(screen, needle string) string {
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}
