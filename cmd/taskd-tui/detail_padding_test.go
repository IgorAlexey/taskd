package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestDetailPaneInnerPadding(t *testing.T) {
	t.Run("padLineIndentBounds", func(t *testing.T) {
		line := padLineIndent("hello", 20)
		if ansi.StringWidth(line) != 20 {
			t.Fatalf("width = %d, want 20", ansi.StringWidth(line))
		}
		if !strings.HasPrefix(line, detailIndentSpaces+"hello") {
			t.Fatalf("unexpected indented prefix: %q", line)
		}

		longLine := padLineIndent(strings.Repeat("a", 50), 20)
		if ansi.StringWidth(longLine) != 20 {
			t.Fatalf("truncated width = %d, want 20", ansi.StringWidth(longLine))
		}
		if !strings.HasPrefix(longLine, detailIndentSpaces+"a") {
			t.Fatalf("unexpected truncated prefix: %q", longLine)
		}

		emptyLine := padLineIndent("", 20)
		if ansi.StringWidth(emptyLine) != 20 {
			t.Fatalf("empty line width = %d, want 20", ansi.StringWidth(emptyLine))
		}

		collapsedLine := padLineIndent("hello", detailIndent)
		if ansi.StringWidth(collapsedLine) != detailIndent {
			t.Fatalf("collapsed width = %d, want %d", ansi.StringWidth(collapsedLine), detailIndent)
		}
	})

	t.Run("viewportGeometryDerivation", func(t *testing.T) {
		m := newModel(config{icons: false}, nil)
		wantWidth := m.width - detailIndent - scrollbarWidth
		if m.detailViewportWidth() != wantWidth {
			t.Fatalf("detailViewportWidth = %d, want %d", m.detailViewportWidth(), wantWidth)
		}
		if m.detail.Width() != wantWidth {
			t.Fatalf("detail.Width() = %d, want %d", m.detail.Width(), wantWidth)
		}

		up, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		m = up.(model)
		wantWidth100 := 100 - detailIndent - scrollbarWidth
		if m.detailViewportWidth() != wantWidth100 {
			t.Fatalf("detailViewportWidth after resize = %d, want %d", m.detailViewportWidth(), wantWidth100)
		}
		if m.detail.Width() != wantWidth100 {
			t.Fatalf("detail.Width() after resize = %d, want %d", m.detail.Width(), wantWidth100)
		}
	})

	now := time.Unix(1700000000, 0)
	sampleTask := task{
		ID:           "abcdef123456",
		Project:      "proj",
		Status:       "leased",
		Worker:       "host1:main",
		LeaseExpires: now.Unix() + 1800,
		Priority:     2,
		Body:         "detail title\nfirst detail body line\nsecond detail body line",
	}

	for _, mde := range []mode{modeTable, modeZoom} {
		m := newModel(config{icons: false}, nil)
		m.width = 80
		m.height = 24
		m.now = now
		m.mode = mde
		m.tasks = []task{sampleTask}
		m.rebuildShown()
		m.cursor = 0
		m.syncDetail()

		rendered := ansi.Strip(m.View().Content)
		lines := strings.Split(rendered, "\n")
		panes := m.panes()

		if panes.detailRows < 4 {
			t.Fatalf("mode %v: detailRows=%d, want >= 4", mde, panes.detailRows)
		}

		detailLines := lines[panes.detailTop : panes.detailTop+panes.detailRows]
		for i, line := range detailLines {
			if ansi.StringWidth(line) != m.width {
				t.Fatalf("mode %v: detail row %d width = %d, want %d", mde, i, ansi.StringWidth(line), m.width)
			}
		}

		titleLine := detailLines[1]
		if !strings.HasPrefix(titleLine, detailIndentSpaces+"detail title") {
			t.Fatalf("mode %v: title line = %q, want prefix %q", mde, titleLine, detailIndentSpaces+"detail title")
		}

		chipsLine := detailLines[2]
		if !strings.HasPrefix(chipsLine, detailIndentSpaces+"/ proj") {
			t.Fatalf("mode %v: chips line = %q, want prefix %q", mde, chipsLine, detailIndentSpaces+"/ proj")
		}

		bodyLine := detailLines[3]
		if !strings.HasPrefix(bodyLine, detailIndentSpaces+"detail title") {
			t.Fatalf("mode %v: body line = %q, want prefix %q", mde, bodyLine, detailIndentSpaces+"detail title")
		}
	}
}
