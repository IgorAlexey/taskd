package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTUISortColumn(t *testing.T) {
	now := time.Now()
	t1 := task{
		ID:           "t1",
		Priority:     2,
		Status:       "pending",
		Project:      "proj-z",
		Worker:       "worker-b",
		LeaseExpires: now.Add(2000 * time.Second).Unix(),
		Body:         "pending task z",
	}
	t2 := task{
		ID:           "t2",
		Priority:     0,
		Status:       "leased",
		Project:      "proj-a",
		Worker:       "worker-a",
		LeaseExpires: now.Add(1000 * time.Second).Unix(),
		Body:         "leased task a",
	}
	t3 := task{
		ID:           "t3",
		Priority:     1,
		Status:       "done",
		Project:      "proj-m",
		Worker:       "",
		LeaseExpires: 0,
		Body:         "done task m",
	}

	m := newModel(config{icons: false, refresh: time.Hour}, nil)
	m.width = 120
	m.height = 24
	m.tasks = []task{t1, t2, t3}
	m.rebuildShown()

	if len(m.shown) != 3 {
		t.Fatalf("expected 3 shown tasks, got %d", len(m.shown))
	}
	if m.tasks[m.shown[0]].ID != "t2" || m.tasks[m.shown[1]].ID != "t3" || m.tasks[m.shown[2]].ID != "t1" {
		t.Fatalf("priority sort initial shown = [%s, %s, %s], want [t2, t3, t1]",
			m.tasks[m.shown[0]].ID, m.tasks[m.shown[1]].ID, m.tasks[m.shown[2]].ID)
	}

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "p*") && !strings.Contains(view, "p▼") {
		t.Fatalf("expected priority header indicator in view:\n%s", view)
	}

	up, _ := m.Update(tea.KeyPressMsg{Text: "s"})
	m = up.(model)
	if m.sortCol != sortStatus {
		t.Fatalf("expected sortStatus after 's', got %v", m.sortCol)
	}
	if m.tasks[m.shown[0]].ID != "t2" || m.tasks[m.shown[1]].ID != "t1" || m.tasks[m.shown[2]].ID != "t3" {
		t.Fatalf("status sort shown = [%s, %s, %s], want [t2, t1, t3]",
			m.tasks[m.shown[0]].ID, m.tasks[m.shown[1]].ID, m.tasks[m.shown[2]].ID)
	}
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, "*p") && !strings.Contains(view, "▼p") {
		t.Fatalf("expected status header indicator in view:\n%s", view)
	}

	up, _ = m.Update(tea.KeyPressMsg{Text: "s"})
	m = up.(model)
	if m.sortCol != sortProject {
		t.Fatalf("expected sortProject after 's', got %v", m.sortCol)
	}
	if m.tasks[m.shown[0]].ID != "t2" || m.tasks[m.shown[1]].ID != "t3" || m.tasks[m.shown[2]].ID != "t1" {
		t.Fatalf("project sort shown = [%s, %s, %s], want [t2, t3, t1]",
			m.tasks[m.shown[0]].ID, m.tasks[m.shown[1]].ID, m.tasks[m.shown[2]].ID)
	}
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, "scope*") && !strings.Contains(view, "scope▼") {
		t.Fatalf("expected scope header indicator in view:\n%s", view)
	}

	up, _ = m.Update(tea.KeyPressMsg{Text: "s"})
	m = up.(model)
	if m.sortCol != sortWorker {
		t.Fatalf("expected sortWorker after 's', got %v", m.sortCol)
	}
	if m.tasks[m.shown[0]].ID != "t2" || m.tasks[m.shown[1]].ID != "t1" || m.tasks[m.shown[2]].ID != "t3" {
		t.Fatalf("worker sort shown = [%s, %s, %s], want [t2, t1, t3]",
			m.tasks[m.shown[0]].ID, m.tasks[m.shown[1]].ID, m.tasks[m.shown[2]].ID)
	}
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, "worker*") && !strings.Contains(view, "worker▼") {
		t.Fatalf("expected worker header indicator in view:\n%s", view)
	}

	up, _ = m.Update(tea.KeyPressMsg{Text: "s"})
	m = up.(model)
	if m.sortCol != sortLease {
		t.Fatalf("expected sortLease after 's', got %v", m.sortCol)
	}
	if m.tasks[m.shown[0]].ID != "t2" || m.tasks[m.shown[1]].ID != "t1" || m.tasks[m.shown[2]].ID != "t3" {
		t.Fatalf("lease sort shown = [%s, %s, %s], want [t2, t1, t3]",
			m.tasks[m.shown[0]].ID, m.tasks[m.shown[1]].ID, m.tasks[m.shown[2]].ID)
	}
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, "lease*") && !strings.Contains(view, "lease▼") {
		t.Fatalf("expected lease header indicator in view:\n%s", view)
	}

	up, _ = m.Update(tea.KeyPressMsg{Text: "s"})
	m = up.(model)
	if m.sortCol != sortPriority {
		t.Fatalf("expected sortPriority wrap after 's', got %v", m.sortCol)
	}

	colHeadY := headerRows + tabRows + 1
	up, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 6, Y: colHeadY})
	m = up.(model)
	if m.sortCol != sortProject {
		t.Fatalf("expected sortProject after clicking scope header, got %v", m.sortCol)
	}

	up, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: colHeadY})
	m = up.(model)
	if m.sortCol != sortStatus {
		t.Fatalf("expected sortStatus after clicking status header, got %v", m.sortCol)
	}

	up, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: colHeadY})
	m = up.(model)
	if m.sortCol != sortPriority {
		t.Fatalf("expected sortPriority after clicking priority header, got %v", m.sortCol)
	}
}

func TestParseSortColumnFlag(t *testing.T) {
	cases := []struct {
		in   string
		want sortColumn
		ok   bool
	}{
		{"priority", sortPriority, true},
		{"status", sortStatus, true},
		{"Status", sortStatus, true},
		{"project", sortProject, true},
		{"Project", sortProject, true},
		{"worker", sortWorker, true},
		{"Worker", sortWorker, true},
		{"lease", sortLease, true},
		{"Lease", sortLease, true},
		{"invalid", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseSortColumn(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("parseSortColumn(%q) = (%v, %v), want (%v, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}

	cfg, err := parseFlags([]string{"-s", "status"})
	if err != nil {
		t.Fatalf("unexpected error parsing -s: %v", err)
	}
	if cfg.sortCol != sortStatus {
		t.Fatalf("expected sortStatus for -s flag, got %v", cfg.sortCol)
	}

	cfgLong, err := parseFlags([]string{"-sort", "worker"})
	if err != nil {
		t.Fatalf("unexpected error parsing -sort: %v", err)
	}
	if cfgLong.sortCol != sortWorker {
		t.Fatalf("expected sortWorker for -sort flag, got %v", cfgLong.sortCol)
	}
}

func TestPendingPriorityOrdering(t *testing.T) {
	tPri3 := task{
		ID:        "t-pri3",
		Priority:  3,
		Status:    "pending",
		Project:   "sorttest",
		CreatedAt: 100,
	}
	tPri1 := task{
		ID:        "t-pri1",
		Priority:  1,
		Status:    "pending",
		Project:   "sorttest",
		CreatedAt: 200,
	}
	tPri1Old := task{
		ID:        "t-pri1-old",
		Priority:  1,
		Status:    "pending",
		Project:   "sorttest",
		CreatedAt: 50,
	}

	m := newModel(config{project: "sorttest"}, nil)
	m.tasks = []task{tPri3, tPri1, tPri1Old}
	m.rebuildShown()

	if len(m.shown) != 3 {
		t.Fatalf("expected 3 tasks shown, got %d", len(m.shown))
	}
	got := []string{
		m.tasks[m.shown[0]].ID,
		m.tasks[m.shown[1]].ID,
		m.tasks[m.shown[2]].ID,
	}
	want := []string{"t-pri1-old", "t-pri1", "t-pri3"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("shown[%d] = %s, want %s (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestSortEnvVar(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want sortColumn
	}{
		{"priority", sortPriority},
		{"status", sortStatus},
		{"project", sortProject},
		{"worker", sortWorker},
		{"lease", sortLease},
		{"Worker", sortWorker},
		{"STATUS", sortStatus},
	} {
		t.Setenv("TASKD_SORT", tc.env)
		cfg, err := parseFlags(nil)
		if err != nil {
			t.Fatalf("parseFlags with TASKD_SORT=%q error: %v", tc.env, err)
		}
		if cfg.sortCol != tc.want {
			t.Fatalf("TASKD_SORT=%q: got %v, want %v", tc.env, cfg.sortCol, tc.want)
		}
	}

	t.Setenv("TASKD_SORT", "worker")
	cfgOverride, err := parseFlags([]string{"-s", "status"})
	if err != nil {
		t.Fatalf("unexpected error parsing override: %v", err)
	}
	if cfgOverride.sortCol != sortStatus {
		t.Fatalf("expected -s flag to override TASKD_SORT, got %v", cfgOverride.sortCol)
	}

	t.Setenv("TASKD_SORT", "invalid")
	if _, err := parseFlags(nil); err == nil {
		t.Fatal("expected error for invalid TASKD_SORT, got nil")
	}

	var buf bytes.Buffer
	printUsage(&buf)
	usage := buf.String()
	idxEnv := strings.Index(usage, "Environment variables:")
	if idxEnv == -1 {
		t.Fatal("printUsage missing 'Environment variables:' section")
	}
	if !strings.Contains(usage[idxEnv:], "TASKD_SORT") {
		t.Fatal("printUsage missing TASKD_SORT under Environment variables")
	}
}
