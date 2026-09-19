package main

import (
	"strings"
	"testing"

	"github.com/rivo/tview"
	"github.com/rivo/uniseg"
)

func TestTableColumnWidthLimits(t *testing.T) {
	u := newUI("http://127.0.0.1:1", "", false)
	longTitle := "important task title that must not be truncated"
	u.render([]task{
		{
			ID:      "aaaaaaa1",
			Project: "extremely-long-project-name-exceeding-limit",
			Status:  "leased",
			Worker:  "runner-linux-node-1-worker-slot-4",
			Body:    longTitle,
		},
		{
			ID:      "bbbbbbb2",
			Project: "short",
			Status:  "pending",
			Worker:  "w1",
			Body:    "another task",
		},
	})

	for _, tc := range []struct {
		name     string
		row, col int
		max      int
	}{
		{"long project", 1, 2, maxMetaWidth},
		{"long worker", 1, 4, maxMetaWidth},
		{"short project", 2, 2, maxMetaWidth},
		{"short worker", 2, 4, maxMetaWidth},
	} {
		cell := u.table.GetCell(tc.row, tc.col)
		text := tview.Unescape(cell.Text)
		if w := uniseg.StringWidth(text); w > tc.max {
			t.Errorf("%s cell visual width %d exceeds max %d: %q", tc.name, w, tc.max, text)
		}
	}

	if got := tview.Unescape(u.table.GetCell(1, 6).Text); got != longTitle {
		t.Fatalf("task title was truncated: got %q, want %q", got, longTitle)
	}
	if got := tview.Unescape(u.table.GetCell(2, 2).Text); got != "short" {
		t.Fatalf("short project was altered: got %q", got)
	}
}

func TestTruncateColumns(t *testing.T) {
	u := newUI("http://127.0.0.1:1", "", false)
	u.render([]task{
		{
			ID:      "t1",
			Project: "infrastructure-monitoring-platform",
			Worker:  "runner-linux-node-1-worker-slot-4",
			Status:  "pending",
		},
		{
			ID:      "t2",
			Project: "taskd",
			Worker:  "worker-1",
			Status:  "pending",
		},
		{
			ID:      "t3",
			Project: "这是一个很长的中文项目名称测试",
			Worker:  "slot",
			Status:  "pending",
		},
	})

	if got := tview.Unescape(u.table.GetCell(1, 2).Text); got != "infrastructur..." {
		t.Errorf("project truncation = %q, want %q", got, "infrastructur...")
	}
	if got := tview.Unescape(u.table.GetCell(1, 4).Text); got != "runner-linux-..." {
		t.Errorf("worker truncation = %q, want %q", got, "runner-linux-...")
	}
	if got := tview.Unescape(u.table.GetCell(2, 2).Text); got != "taskd" {
		t.Errorf("short project = %q, want %q", got, "taskd")
	}
	cjk := tview.Unescape(u.table.GetCell(3, 2).Text)
	if w := uniseg.StringWidth(cjk); w > maxMetaWidth {
		t.Errorf("cjk project visual width %d exceeds %d", w, maxMetaWidth)
	}
	if !strings.HasSuffix(cjk, "...") {
		t.Errorf("cjk project %q missing ellipsis suffix", cjk)
	}
}
