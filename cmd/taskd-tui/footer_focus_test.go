package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func newTestModel() model {
	fixedNow := time.Unix(1700000000, 0)
	tasks := []task{
		{
			ID:       "task0011111",
			Project:  "taskd",
			Status:   "pending",
			Priority: 1,
			Body:     "first pending task body\nline two of task",
		},
		{
			ID:           "task0022222",
			Project:      "taskd",
			Status:       "leased",
			Worker:       "host1:main",
			LeaseExpires: fixedNow.Unix() + 1800,
			Priority:     2,
			Body:         "second leased task body",
		},
	}

	m := model{
		cfg: config{
			url:     "http://localhost:8080",
			refresh: 2 * time.Second,
		},
		theme:     newTheme(true),
		glyph:     asciiGlyphs,
		width:     100,
		height:    30,
		now:       fixedNow,
		tasks:     tasks,
		shown:     []int{0, 1},
		cursor:    0,
		offset:    0,
		mode:      modeTable,
		connected: true,
		stats: stats{
			Pending: 1,
			Leased:  1,
			Total:   2,
		},
	}
	vh := m.detailRows() - 4
	if vh < 1 {
		vh = 1
	}
	m.detail.SetWidth(m.width - 2)
	m.detail.SetHeight(vh)
	if t, ok := m.selected(); ok {
		m.detail.SetContent(m.renderBody(t))
	}
	return m
}

func renderedFooter(m model) string {
	v := m.View()
	stripped := ansi.Strip(v.Content)
	lines := strings.Split(stripped, "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

func TestFooterDiffersBetweenTableAndBodyFocus(t *testing.T) {
	m := newTestModel()

	tableFooter := renderedFooter(m)
	if !strings.Contains(tableFooter, "filter") || !strings.Contains(tableFooter, "0-4") {
		t.Fatalf("table footer %q should advertise filter keys", tableFooter)
	}

	m.mode = modeDetail
	bodyFooter := renderedFooter(m)

	if bodyFooter == tableFooter {
		t.Fatalf("rendered footer should differ between table and body focus, got %q for both", tableFooter)
	}
	if strings.Contains(bodyFooter, "filter") || strings.Contains(bodyFooter, "0-4") {
		t.Fatalf("body footer %q should not advertise filter keys", bodyFooter)
	}
	if !strings.Contains(bodyFooter, "scroll") || !strings.Contains(bodyFooter, "back") {
		t.Fatalf("body footer %q should advertise body actions (scroll, back)", bodyFooter)
	}
}

func TestBodyFocusViaTabAndEnter(t *testing.T) {
	m := newTestModel()

	tableFooter := renderedFooter(m)

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	mTab := next.(model)
	if mTab.mode != modeDetail {
		t.Fatalf("mode after Tab = %v, want modeDetail", mTab.mode)
	}
	tabFooter := renderedFooter(mTab)
	if tabFooter == tableFooter {
		t.Fatalf("footer after Tab should differ from table footer")
	}
	if strings.Contains(tabFooter, "filter") {
		t.Fatalf("footer after Tab should not advertise filter: %q", tabFooter)
	}

	back, _ := mTab.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	mBack := back.(model)
	if mBack.mode != modeTable {
		t.Fatalf("mode after second Tab = %v, want modeTable", mBack.mode)
	}
	if renderedFooter(mBack) != tableFooter {
		t.Fatalf("footer after returning to table should match original table footer")
	}

	nextEnter, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mEnter := nextEnter.(model)
	if mEnter.mode != modeDetail {
		t.Fatalf("mode after Enter = %v, want modeDetail", mEnter.mode)
	}
	enterFooter := renderedFooter(mEnter)
	if enterFooter != tabFooter {
		t.Fatalf("footer after Enter should match detail footer, got %q want %q", enterFooter, tabFooter)
	}

	backEsc, _ := mEnter.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	mBackEsc := backEsc.(model)
	if mBackEsc.mode != modeTable {
		t.Fatalf("mode after Escape = %v, want modeTable", mBackEsc.mode)
	}
	if renderedFooter(mBackEsc) != tableFooter {
		t.Fatalf("footer after Escape should match original table footer")
	}
}

func TestBodyFocusViaMouseClick(t *testing.T) {
	m := newTestModel()
	tableFooter := renderedFooter(m)

	tr, dr := m.layout()
	bandTop := headerRows + tabRows + 1 + colHeadRows
	detailTop := bandTop + tr + 1

	clickDetail := tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      10,
		Y:      detailTop + dr/2,
	}
	next, _ := m.Update(clickDetail)
	mDetail := next.(model)
	if mDetail.mode != modeDetail {
		t.Fatalf("mode after click in detail pane = %v, want modeDetail", mDetail.mode)
	}
	detailFooter := renderedFooter(mDetail)
	if detailFooter == tableFooter {
		t.Fatalf("footer after clicking detail pane should differ from table footer")
	}
	if strings.Contains(detailFooter, "filter") {
		t.Fatalf("footer after clicking detail pane should not advertise filter: %q", detailFooter)
	}

	clickTable := tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      10,
		Y:      bandTop,
	}
	nextTable, _ := mDetail.Update(clickTable)
	mTable := nextTable.(model)
	if mTable.mode != modeTable {
		t.Fatalf("mode after clicking table = %v, want modeTable", mTable.mode)
	}
	if mTable.cursor != 0 {
		t.Fatalf("cursor after clicking row 0 = %d, want 0", mTable.cursor)
	}
	if renderedFooter(mTable) != tableFooter {
		t.Fatalf("footer after clicking table should match original table footer")
	}

	clickRow1 := tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      10,
		Y:      bandTop + 1,
	}
	nextRow1, _ := mTable.Update(clickRow1)
	mRow1 := nextRow1.(model)
	if mRow1.cursor != 1 {
		t.Fatalf("cursor after clicking row 1 = %d, want 1", mRow1.cursor)
	}
	row1Footer := renderedFooter(mRow1)
	if !strings.Contains(row1Footer, "filter") || !strings.Contains(row1Footer, "2/2") {
		t.Fatalf("row 1 footer %q should advertise filter and position 2/2", row1Footer)
	}
}

func TestMouseClickIgnoredInOtherModes(t *testing.T) {
	m := newTestModel()
	m.mode = modeHelp

	tr, dr := m.layout()
	bandTop := headerRows + tabRows + 1 + colHeadRows
	detailTop := bandTop + tr + 1

	clickDetail := tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      10,
		Y:      detailTop + dr/2,
	}
	next, _ := m.Update(clickDetail)
	mNext := next.(model)
	if mNext.mode != modeHelp {
		t.Fatalf("mode after click in help = %v, want modeHelp", mNext.mode)
	}

	clickTable := tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      10,
		Y:      bandTop + 1,
	}
	nextTable, _ := mNext.Update(clickTable)
	mTable := nextTable.(model)
	if mTable.mode != modeHelp {
		t.Fatalf("mode after click in help = %v, want modeHelp", mTable.mode)
	}
	if mTable.cursor != 0 {
		t.Fatalf("cursor in help should not change, got %d", mTable.cursor)
	}
}
