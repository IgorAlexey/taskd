package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDetailPaneBodyVisibleAt80x24(t *testing.T) {
	var bodyLines []string
	bodyLines = append(bodyLines, "task title")
	for i := 1; i <= 41; i++ {
		bodyLines = append(bodyLines, fmt.Sprintf("body-marker-%02d", i))
	}
	body := strings.Join(bodyLines, "\n")

	m := newModel(config{icons: true, refresh: time.Hour}, nil)
	m.width = 80
	m.height = 24
	m.tasks = []task{{
		ID:       "b25cfde112233445566",
		Project:  "web",
		Status:   "pending",
		Priority: 3,
		Body:     body,
	}}
	m.shown = []int{0}
	m.cursor = 0
	m.mode = modeTable
	m.syncDetail()

	view := m.View().Content

	count := strings.Count(view, "body-marker-")
	if count < 5 {
		t.Fatalf("expected at least 5 visible body marker lines at 80x24 without z, got %d\nView:\n%s", count, view)
	}

	wantIndicator := "[line 1/41 0%]"
	if !strings.Contains(view, wantIndicator) {
		t.Fatalf("expected exact indicator %q in rule header\nView:\n%s", wantIndicator, view)
	}

	if !strings.Contains(view, m.glyph.thumb) {
		t.Fatalf("expected scrollbar thumb %q in view\nView:\n%s", m.glyph.thumb, view)
	}
	if !strings.Contains(view, m.glyph.track) {
		t.Fatalf("expected scrollbar track %q in view\nView:\n%s", m.glyph.track, view)
	}

	m.detail.ScrollDown(4)
	viewScrolled := m.View().Content
	if !strings.Contains(viewScrolled, "[line 5/41") {
		t.Fatalf("expected [line 5/41 after scrolling down 4 lines\nView:\n%s", viewScrolled)
	}

	m.detail.GotoBottom()
	viewBottom := m.View().Content
	if !strings.Contains(viewBottom, "100%]") {
		t.Fatalf("expected 100%%] at bottom of scrollable pane\nView:\n%s", viewBottom)
	}
}

func TestAccordionLayoutGivesTableRowsToDetail(t *testing.T) {
	m := newModel(config{icons: true, refresh: time.Hour}, nil)
	m.width = 80
	m.height = 24
	m.tasks = []task{{
		ID:      "t1",
		Project: "web",
		Status:  "pending",
		Body:    "single task\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10",
	}}
	m.shown = []int{0}
	m.cursor = 0
	m.mode = modeTable
	m.syncDetail()

	tRows, dRows := m.layout()
	if tRows != 1 {
		t.Fatalf("expected table to shrink to 1 needed row, got %d", tRows)
	}
	if dRows != 17 {
		t.Fatalf("expected detail pane to receive 17 rows, got %d", dRows)
	}

	m.mode = modeDetail
	tDetail, dDetail := m.layout()
	if tDetail != tRows || dDetail != dRows {
		t.Fatalf("Tab must not jump layout: got %d,%d, want %d,%d", tDetail, dDetail, tRows, dRows)
	}

	manyShown := make([]int, 20)
	for i := range 20 {
		manyShown[i] = 0
	}
	m.shown = manyShown
	m.mode = modeTable
	tMany, dMany := m.layout()
	if tMany != 9 || dMany != 9 {
		t.Fatalf("expected 20 tasks to split 9/9, got %d/%d", tMany, dMany)
	}
}
