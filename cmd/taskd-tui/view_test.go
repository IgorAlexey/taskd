package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestViewLayoutAndRendering(t *testing.T) {
	fixedNow := time.Unix(1700000000, 0)
	tasks := []task{
		{
			ID:       "task0011111",
			Project:  "taskd",
			Status:   "pending",
			Priority: 1,
			Body:     "taskd: first pending task",
		},
		{
			ID:           "task0022222",
			Project:      "taskd",
			Status:       "leased",
			Worker:       "host1:main",
			LeaseExpires: fixedNow.Unix() + 1800,
			Priority:     2,
			Body:         "taskd: second leased task",
		},
		{
			ID:       "task0033333",
			Project:  "taskd",
			Status:   "done",
			Priority: 0,
			Body:     "taskd: third done task",
		},
	}

	m := model{
		cfg: config{
			url:     "http://localhost:8080",
			refresh: 2 * time.Second,
		},
		theme:  newTheme(true),
		glyph:  asciiGlyphs,
		width:  100,
		height: 30,
		now:    fixedNow,
		tasks:  tasks,
		shown:  []int{0, 1, 2},
		cursor: 1,
		offset: 0,
		stats: stats{
			Pending:      1,
			Leased:       1,
			Done:         1,
			Total:        3,
			LeaseSeconds: 3600,
		},
		connected: true,
	}

	v := m.View()
	stripped := ansi.Strip(v.Content)
	lines := strings.Split(stripped, "\n")

	// 1. line count == 30
	if len(lines) != 30 {
		t.Fatalf("expected 30 lines, got %d", len(lines))
	}

	// 2. every line has display width == 100 (no wrapping)
	for i, line := range lines {
		w := ansi.StringWidth(line)
		if w != 100 {
			t.Errorf("line %d has display width %d, want 100: %q", i, w, line)
		}
	}

	// 3. header contains "taskd" and "connected"
	headerLine := lines[0]
	if !strings.Contains(headerLine, "taskd") {
		t.Errorf("header line does not contain %q: %q", "taskd", headerLine)
	}
	if !strings.Contains(headerLine, "connected") {
		t.Errorf("header line does not contain %q: %q", "connected", headerLine)
	}

	// 4. tabs contain "0 all" and "1 pending"
	tabsLine := lines[1]
	if !strings.Contains(tabsLine, "0 all") {
		t.Errorf("tabs line does not contain %q: %q", "0 all", tabsLine)
	}
	if !strings.Contains(tabsLine, "1 pending") {
		t.Errorf("tabs line does not contain %q: %q", "1 pending", tabsLine)
	}

	// 5. selected row starts with ">"
	selectedRow := lines[4+m.cursor]
	if !strings.HasPrefix(selectedRow, ">") {
		t.Errorf("selected row does not start with '>', got: %q", selectedRow)
	}

	// 6. a leased task row shows "m" in left and "=" bar chars
	if !strings.Contains(selectedRow, "m") {
		t.Errorf("leased row does not contain 'm' in left: %q", selectedRow)
	}
	if !strings.Contains(selectedRow, "=") {
		t.Errorf("leased row does not contain '=' bar chars: %q", selectedRow)
	}

	// 7. footer ends with "2/3"
	footerLine := lines[len(lines)-1]
	if !strings.HasSuffix(footerLine, "2/3") {
		t.Errorf("footer line does not end with '2/3', got: %q", footerLine)
	}

	// 8. with len(shown) > tableRows the last column contains "#" (thumb)
	mScroll := m
	manyTasks := make([]task, 40)
	manyShown := make([]int, 40)
	for i := 0; i < 40; i++ {
		manyTasks[i] = task{
			ID:      fmt.Sprintf("task%03d", i),
			Project: "taskd",
			Status:  "pending",
			Body:    fmt.Sprintf("taskd: task %d", i),
		}
		manyShown[i] = i
	}
	mScroll.tasks = manyTasks
	mScroll.shown = manyShown
	mScroll.cursor = 0
	vScroll := mScroll.View()
	scrollLines := strings.Split(ansi.Strip(vScroll.Content), "\n")
	hasThumb := false
	for i := 4; i < 4+mScroll.tableRows(); i++ {
		if len(scrollLines[i]) > 0 && strings.HasSuffix(scrollLines[i], "#") {
			hasThumb = true
			break
		}
	}
	if !hasThumb {
		t.Errorf("expected table row to have '#' thumb in last column when len(shown) > tableRows")
	}

	// 9. zoom mode has no column header
	mZoom := m
	mZoom.mode = modeZoom
	vZoom := mZoom.View()
	zoomLines := strings.Split(ansi.Strip(vZoom.Content), "\n")
	if len(zoomLines) != 30 {
		t.Errorf("zoom mode line count %d, want 30", len(zoomLines))
	}
	for _, line := range zoomLines {
		if strings.Contains(line, " p ") && strings.Contains(line, "scope") && strings.Contains(line, "title") {
			t.Errorf("zoom mode should have no column header, found: %q", line)
		}
	}

	// 10. an empty shown with a query shows the Esc hint
	mEmpty := m
	mEmpty.shown = nil
	mEmpty.query = "findnothing"
	vEmpty := mEmpty.View()
	emptyStripped := ansi.Strip(vEmpty.Content)
	if !strings.Contains(emptyStripped, "Esc") {
		t.Errorf("empty shown with query should show Esc hint, got: %q", emptyStripped)
	}

	// 11. titles longer than the column are cut with "..."
	mLong := m
	mLong.tasks = []task{
		{
			ID:      "tasklong",
			Project: "taskd",
			Status:  "pending",
			Body:    "taskd: This is an extraordinarily long title that will certainly exceed the width of the title column in the table",
		},
	}
	mLong.shown = []int{0}
	mLong.cursor = 0
	vLong := mLong.View()
	longStripped := ansi.Strip(vLong.Content)
	if !strings.Contains(longStripped, "...") {
		t.Errorf("titles longer than column should be cut with '...', got: %q", longStripped)
	}
}

func TestHelpers(t *testing.T) {
	th := newTheme(true)

	// highlightCode
	codeStr := "hello `code1` world `code2`"
	highlighted := highlightCode(codeStr, th)
	if !strings.Contains(highlighted, "code1") || !strings.Contains(highlighted, "code2") {
		t.Errorf("highlightCode failed: %q", highlighted)
	}

	// trunc
	if got := trunc("hello world", 8, "..."); got != "hello..." {
		t.Errorf("trunc(\"hello world\", 8, \"...\") = %q, want %q", got, "hello...")
	}
	if got := trunc("short", 10, "..."); got != "short" {
		t.Errorf("trunc(\"short\", 10, \"...\") = %q, want %q", got, "short")
	}
	if got := trunc("hello world", 0, "..."); got != "" {
		t.Errorf("trunc(\"hello world\", 0, \"...\") = %q, want \"\"", got)
	}

	// leaseLeft
	now := time.Unix(1700000000, 0)
	taskNotLeased := task{Status: "pending"}
	if got := leaseLeft(taskNotLeased, now); got != "" {
		t.Errorf("leaseLeft pending = %q, want \"\"", got)
	}
	taskExpired := task{Status: "leased", LeaseExpires: now.Unix() - 10}
	if got := leaseLeft(taskExpired, now); got != "expired" {
		t.Errorf("leaseLeft expired = %q, want \"expired\"", got)
	}
	taskMinutes := task{Status: "leased", LeaseExpires: now.Unix() + 3345}
	if got := leaseLeft(taskMinutes, now); got != "55m45s" {
		t.Errorf("leaseLeft 55m45s = %q, want \"55m45s\"", got)
	}
	taskHours := task{Status: "leased", LeaseExpires: now.Unix() + 3665}
	if got := leaseLeft(taskHours, now); got != "1h01m" {
		t.Errorf("leaseLeft 1h01m = %q, want \"1h01m\"", got)
	}
}

// A two digit claim count needs four cells ("~ 12"); a hardcoded three wide
// column used to overrun the row and clip the id.
func TestClaimsColumnFitsTwoDigitCount(t *testing.T) {
	fixedNow := time.Unix(1700000000, 0)
	m := model{
		cfg:    config{url: "http://localhost:8080", refresh: 2 * time.Second},
		theme:  newTheme(true),
		glyph:  asciiGlyphs,
		width:  100,
		height: 30,
		now:    fixedNow,
		tasks: []task{
			{ID: "task0011111", Project: "taskd", Status: "pending", Priority: 1, Body: "taskd: first pending task"},
			{ID: "task0022222", Project: "taskd", Status: "pending", Priority: 2, ClaimCount: 12, Body: "taskd: task retried twelve times"},
		},
		shown:     []int{0, 1},
		cursor:    0,
		stats:     stats{Pending: 2, Total: 2, LeaseSeconds: 3600},
		connected: true,
	}

	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != 100 {
			t.Errorf("line %d has display width %d, want 100: %q", i, w, line)
		}
	}

	row := lines[4+1] // second table row: the task with ClaimCount 12
	if !strings.Contains(row, asciiGlyphs.refresh+" 12") {
		t.Errorf("row does not show the claim count %q: %q", asciiGlyphs.refresh+" 12", row)
	}
	if !strings.HasSuffix(row, "task002") {
		t.Errorf("row does not end with the 7 char id: %q", row)
	}
}

func TestGlyphModesUseTheirOwnTextGlyphs(t *testing.T) {
	fixedNow := time.Unix(1700000000, 0)
	base := model{
		cfg:    config{url: "http://localhost:8080", refresh: 2 * time.Second},
		theme:  newTheme(true),
		width:  100,
		height: 30,
		now:    fixedNow,
		tasks: []task{
			{
				ID:       "task0011111",
				Project:  "taskd",
				Status:   "pending",
				Priority: 1,
				Body:     "taskd: an extraordinarily long title that certainly exceeds the width of the title column in the table",
			},
		},
		shown:     []int{0},
		cursor:    0,
		stats:     stats{Pending: 1, Total: 1, LeaseSeconds: 3600},
		connected: true,
	}

	cases := []struct {
		name  string
		glyph glyphs
		other glyphs
	}{
		{"ascii", asciiGlyphs, nerdGlyphs},
		{"nerd", nerdGlyphs, asciiGlyphs},
	}
	for _, c := range cases {
		m := base
		m.glyph = c.glyph
		out := ansi.Strip(m.View().Content)
		if !strings.Contains(out, c.glyph.ellipsis) {
			t.Errorf("%s: truncated title does not use ellipsis %q", c.name, c.glyph.ellipsis)
		}
		if strings.Contains(out, c.other.ellipsis) {
			t.Errorf("%s: output uses the other mode's ellipsis %q", c.name, c.other.ellipsis)
		}
		if !strings.Contains(out, strings.Repeat(c.glyph.rule, 2)) {
			t.Errorf("%s: detail rule does not use %q", c.name, c.glyph.rule)
		}

		mSearch := m
		mSearch.mode = modeSearch
		mSearch.query = "abc"
		searchOut := ansi.Strip(mSearch.View().Content)
		if !strings.Contains(searchOut, "/abc"+c.glyph.caret) {
			t.Errorf("%s: search footer does not use caret %q: %q", c.name, c.glyph.caret, searchOut)
		}
	}
}

func TestFrameNeverExceedsTerminalHeight(t *testing.T) {
	modes := []mode{modeTable, modeSearch, modeDetail, modeZoom, modeForm, modeConfirm, modeHelp}
	for _, w := range []int{12, 20, 30, 50, 80, 100} {
		for h := 1; h <= 40; h++ {
			for _, md := range modes {
				m := newModel(config{}, nil)
				m.glyph = asciiGlyphs
				m, _ = send(t, m, tea.WindowSizeMsg{Width: w, Height: h})
				m, _ = send(t, m, pollMsg{tasks: []task{{ID: "a", Project: "p", Status: "pending", Body: "p: t\n\nbody"}}, changed: true})
				m.mode = md
				m.form, _ = newCreateForm("p")
				m.form.errText = "project cannot be blank"
				m.form.fit(w, h, m.theme)
				m.confirm = confirmModel{text: "Delete?", button: "delete"}
				lines := strings.Split(ansi.Strip(m.View().Content), "\n")
				if len(lines) != h {
					t.Fatalf("%dx%d mode %d rendered %d lines", w, h, md, len(lines))
				}
				if md == modeTable && !strings.HasPrefix(lines[h-1], "j/k move") {
					t.Fatalf("%dx%d lost the footer: %q", w, h, lines[h-1])
				}
				// The form opens with focus on the body; whatever else is
				// dropped, the field being typed into stays on screen.
				if md == modeForm && w >= 20 && h >= 4 && !strings.Contains(strings.Join(lines, "\n"), "first line") {
					t.Fatalf("%dx%d form hides the focused body field:\n%s", w, h, strings.Join(lines, "\n"))
				}
				// A button without its error would invite a save that
				// silently fails: wherever the button shows, so does the
				// error, and from eight rows both do.
				if md == modeForm {
					all := strings.Join(lines, "\n")
					if strings.Contains(all, "[ save ]") && !strings.Contains(all, "cannot") {
						t.Fatalf("%dx%d form shows the button without the error:\n%s", w, h, all)
					}
					if w >= 30 && h >= 8 && !strings.Contains(all, "[ save ]") {
						t.Fatalf("%dx%d form hides the button:\n%s", w, h, all)
					}
				}
				for _, l := range lines {
					if lw := ansi.StringWidth(l); lw > w {
						t.Fatalf("%dx%d mode %d line is %d wide: %q", w, h, md, lw, l)
					}
				}
				if md == modeConfirm && w >= 20 && h >= 3 && !strings.Contains(strings.Join(lines, "\n"), "[y]") {
					t.Fatalf("%dx%d confirm hides its actions:\n%s", w, h, strings.Join(lines, "\n"))
				}
				if md == modeHelp && w >= 20 && h >= 5 && !strings.Contains(strings.Join(lines, "\n"), "Press ?") {
					t.Fatalf("%dx%d help hides the closing hint", w, h)
				}
			}
		}
	}
}
func TestNarrowTableDropsColumnsAndPreservesTitle(t *testing.T) {
	fixedNow := time.Unix(1700000000, 0)
	tasks := []task{
		{
			ID:           "task0011111",
			Project:      "frontend-web-ui",
			Status:       "leased",
			Worker:       "worker-linux-runner:/tmp/taskd-long-checkout-path",
			LeaseExpires: fixedNow.Unix() + 1800,
			ClaimCount:   5,
			Priority:     1,
			Body:         "taskd: fix critical layout bug in narrow terminals",
		},
	}
	m := model{
		cfg: config{
			url:     "http://localhost:8080",
			refresh: 2 * time.Second,
		},
		theme:  newTheme(true),
		glyph:  asciiGlyphs,
		width:  70,
		height: 24,
		now:    fixedNow,
		tasks:  tasks,
		shown:  []int{0},
		cursor: 0,
		stats: stats{
			Pending:      0,
			Leased:       1,
			Done:         0,
			Total:        1,
			LeaseSeconds: 3600,
		},
		connected: true,
	}

	v := m.View()
	stripped := ansi.Strip(v.Content)
	lines := strings.Split(stripped, "\n")

	for i, line := range lines {
		if w := ansi.StringWidth(line); w != 70 {
			t.Fatalf("line %d display width %d != 70: %q", i, w, line)
		}
	}

	headerLine := lines[3]
	if strings.Contains(headerLine, "worker") {
		t.Fatalf("expected worker column header to be dropped at 70 cols: %q", headerLine)
	}

	row := lines[4]
	if strings.Contains(row, "worker-linux") {
		t.Fatalf("expected worker name to be dropped at 70 cols: %q", row)
	}
	if strings.Contains(row, asciiGlyphs.refresh) {
		t.Fatalf("expected claims to be dropped at 70 cols: %q", row)
	}
	if !strings.Contains(row, "30m00s") {
		t.Fatalf("expected lease remaining time to be retained: %q", row)
	}
	if !strings.Contains(row, "task001") {
		t.Fatalf("expected task id to be retained: %q", row)
	}
	if !strings.Contains(row, "frontend-web") {
		t.Fatalf("expected scope to be retained: %q", row)
	}
	if !strings.Contains(row, "taskd: fix critical") {
		t.Fatalf("expected title to have 20+ visible chars: %q", row)
	}

	m60 := m
	m60.width = 60
	v60 := m60.View()
	lines60 := strings.Split(ansi.Strip(v60.Content), "\n")
	for i, line := range lines60 {
		if w := ansi.StringWidth(line); w != 60 {
			t.Fatalf("line %d display width %d != 60: %q", i, w, line)
		}
	}
	header60 := lines60[3]
	if strings.Contains(header60, "lease") {
		t.Fatalf("expected lease bar header to be dropped at 60 cols: %q", header60)
	}
	row60 := lines60[4]
	if strings.Contains(row60, "=") {
		t.Fatalf("expected lease bar to be dropped at 60 cols: %q", row60)
	}
	if !strings.Contains(row60, "taskd: fix critical") {
		t.Fatalf("expected title to have 20+ visible chars at 60 cols: %q", row60)
	}
}

func TestFormVisibleAtVariousTerminalHeights(t *testing.T) {
	th := newTheme(true)
	sampleTask := task{
		ID:       "task1234567",
		Project:  "taskd",
		Status:   "pending",
		Priority: 1,
		Body:     "existing body line",
	}

	for _, h := range []int{16, 24, 50} {
		for _, w := range []int{70, 80, 100} {
			cf, _ := newCreateForm("testproj")
			cf.fit(w, h, th)
			cv := ansi.Strip(cf.View())

			if !strings.Contains(cv, "project:") {
				t.Fatalf("create form at %dx%d hides project field:\n%s", w, h, cv)
			}
			if !strings.Contains(cv, "[ save ]") {
				t.Fatalf("create form at %dx%d hides submit button:\n%s", w, h, cv)
			}
			if !strings.Contains(cv, "Esc cancel") {
				t.Fatalf("create form at %dx%d hides cancel hint:\n%s", w, h, cv)
			}

			ef, _ := newEditForm(sampleTask)
			ef.fit(w, h, th)
			ev := ansi.Strip(ef.View())

			if !strings.Contains(ev, "project:") {
				t.Fatalf("edit form at %dx%d hides project field:\n%s", w, h, ev)
			}
			if !strings.Contains(ev, "[ save ]") {
				t.Fatalf("edit form at %dx%d hides submit button:\n%s", w, h, ev)
			}
			if !strings.Contains(ev, "Esc cancel") {
				t.Fatalf("edit form at %dx%d hides cancel hint:\n%s", w, h, ev)
			}
		}
	}
}
