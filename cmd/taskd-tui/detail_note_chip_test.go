package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestDetailNoteCountChip(t *testing.T) {
	now := time.Unix(1700000000, 0)

	tests := []struct {
		name        string
		mode        mode
		taskNotes   []taskNote
		cachedNotes []taskNote
		hasCache    bool
		wantChip    string
		wantAbsent  string
	}{
		{
			name:       "omits note chip when no notes exist",
			mode:       modeTable,
			wantAbsent: "note",
		},
		{
			name:      "renders single note chip from task Notes",
			mode:      modeTable,
			taskNotes: []taskNote{{Author: "alice", Text: "first"}},
			wantChip:  "1 note",
		},
		{
			name:      "renders plural notes chip from task Notes",
			mode:      modeTable,
			taskNotes: []taskNote{{Author: "alice", Text: "first"}, {Author: "bob", Text: "second"}},
			wantChip:  "2 notes",
		},
		{
			name:        "renders note chip from notesCache",
			mode:        modeTable,
			hasCache:    true,
			cachedNotes: []taskNote{{Author: "alice", Text: "c1"}, {Author: "bob", Text: "c2"}, {Author: "carol", Text: "c3"}},
			wantChip:    "3 notes",
		},
		{
			name:        "prefers notesCache over task Notes when present",
			mode:        modeTable,
			taskNotes:   []taskNote{{Author: "alice", Text: "old"}},
			hasCache:    true,
			cachedNotes: []taskNote{},
			wantAbsent:  "note",
		},
		{
			name:      "renders note chip in zoom mode",
			mode:      modeZoom,
			taskNotes: []taskNote{{Author: "alice", Text: "first"}},
			wantChip:  "1 note",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tsk := task{
				ID:        1,
				Project:   "taskd",
				Priority:  1,
				CreatedAt: now.Unix() - 60,
				Body:      "title\nbody",
				Notes:     tc.taskNotes,
			}
			m := newModel(config{icons: false}, nil)
			m.width = 80
			m.height = 24
			m.now = now
			m.mode = tc.mode
			m.tasks = []task{tsk}
			if tc.hasCache {
				m.notesCache[1] = tc.cachedNotes
			}
			m.rebuildShown()
			m.cursor = 0
			m.syncDetail()

			rendered := ansi.Strip(m.View().Content)
			lines := strings.Split(rendered, "\n")
			panes := m.panes()
			if panes.detailRows < 3 {
				t.Fatalf("detailRows=%d, want >= 3", panes.detailRows)
			}
			chipsLine := lines[panes.detailTop+2]

			if tc.wantChip != "" && !strings.Contains(chipsLine, tc.wantChip) {
				t.Fatalf("expected %q in chips line, got: %q", tc.wantChip, chipsLine)
			}
			if tc.wantAbsent != "" && strings.Contains(chipsLine, tc.wantAbsent) {
				t.Fatalf("expected %q absent from chips line, got: %q", tc.wantAbsent, chipsLine)
			}
		})
	}
}
