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
	if !strings.Contains(view, "* p") && !strings.Contains(view, "▼ p") && !strings.Contains(view, "*p") && !strings.Contains(view, "▼p") {
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
	if m.sortCol != sortClaims {
		t.Fatalf("expected sortClaims after 's', got %v", m.sortCol)
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
		{"claims", sortClaims, true},
		{"Claims", sortClaims, true},
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
		{"claims", sortClaims},
		{"Claims", sortClaims},
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

func TestSortReverse(t *testing.T) {
	m := newModel(config{icons: false}, nil)
	if m.sortCol != sortPriority {
		t.Fatalf("expected initial sortPriority, got %v", m.sortCol)
	}

	expected := []sortColumn{sortClaims, sortLease, sortWorker, sortProject, sortStatus, sortPriority}
	for _, want := range expected {
		up, _ := m.Update(tea.KeyPressMsg{Text: "S"})
		m = up.(model)
		if m.sortCol != want {
			t.Fatalf("expected sort column %v after 'S', got %v", want, m.sortCol)
		}
	}
}

func TestSortByClaims(t *testing.T) {
	t.Setenv("TASKD_PROJECT", "")
	t.Setenv("TASKD_WORKER", "")
	cfg, err := parseFlags([]string{"-sort", "claims"})
	if err != nil {
		t.Fatalf("unexpected error parsing -sort claims: %v", err)
	}
	if cfg.sortCol != sortClaims {
		t.Fatalf("expected sortClaims, got %v", cfg.sortCol)
	}

	t1 := task{ID: "t1", ClaimCount: 1}
	t2 := task{ID: "t2", ClaimCount: 5}
	t3 := task{ID: "t3", ClaimCount: 2}

	m := newModel(cfg, nil)
	m.width = 120
	m.height = 24
	m.tasks = []task{t1, t2, t3}
	m.rebuildShown()

	if len(m.shown) != 3 {
		t.Fatalf("expected 3 tasks shown, got %d", len(m.shown))
	}
	if m.tasks[m.shown[0]].ID != "t2" || m.tasks[m.shown[1]].ID != "t3" || m.tasks[m.shown[2]].ID != "t1" {
		t.Fatalf("expected tasks sorted descending by claims [t2, t3, t1], got [%s, %s, %s]",
			m.tasks[m.shown[0]].ID, m.tasks[m.shown[1]].ID, m.tasks[m.shown[2]].ID)
	}

	m.sortCol = sortPriority
	m.rebuild()

	colHeadY := headerRows + tabRows + 1
	priHead := "p"
	if m.sortCol == sortPriority {
		priHead += "▼"
	}
	pw := max(m.cols.priority, ansi.StringWidth(priHead))
	priEnd := 4 + pw
	claimsX := priEnd + m.cols.title + 1
	if m.cols.scope > 0 {
		claimsX += m.cols.scope + 1
	}

	up, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: claimsX, Y: colHeadY})
	m = up.(model)
	if m.sortCol != sortClaims {
		t.Fatalf("expected sortClaims after clicking claims header at X=%d, got %v", claimsX, m.sortCol)
	}

	m.sortCol = sortLease
	up, _ = m.Update(tea.KeyPressMsg{Text: "s"})
	m = up.(model)
	if m.sortCol != sortClaims {
		t.Fatalf("expected sortClaims after pressing s on sortLease, got %v", m.sortCol)
	}

	mZero := newModel(cfg, nil)
	mZero.width = 120
	mZero.height = 24
	mZero.tasks = []task{{ID: "z1", ClaimCount: 0}, {ID: "z2", ClaimCount: 1}}
	mZero.rebuildShown()
	if mZero.cols.claims == 0 {
		t.Fatalf("expected claims column visible when sorted by claims without retries")
	}
	viewZero := ansi.Strip(mZero.View().Content)
	if !strings.Contains(viewZero, "c*") && !strings.Contains(viewZero, "c▼") {
		t.Fatalf("expected claims header sort indicator in view, got:\n%s", viewZero)
	}
	if !strings.Contains(viewZero, " 0") || !strings.Contains(viewZero, " 1") {
		t.Fatalf("expected claim counts 0 and 1 rendered under claims sort, got:\n%s", viewZero)
	}
}
func TestToggleSortDirection(t *testing.T) {
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

	m := newModel(config{icons: true, refresh: time.Hour}, nil)
	m.width = 120
	m.height = 24
	m.tasks = []task{t1, t2, t3}
	m.rebuildShown()

	if len(m.shown) != 3 {
		t.Fatalf("expected 3 tasks shown, got %d", len(m.shown))
	}
	if m.tasks[m.shown[0]].ID != "t2" || m.tasks[m.shown[1]].ID != "t3" || m.tasks[m.shown[2]].ID != "t1" {
		t.Fatalf("initial ascending shown = [%s, %s, %s], want [t2, t3, t1]",
			m.tasks[m.shown[0]].ID, m.tasks[m.shown[1]].ID, m.tasks[m.shown[2]].ID)
	}

	colHeadY := headerRows + tabRows + 1
	up, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: colHeadY})
	m = up.(model)

	if !m.sortDesc {
		t.Fatalf("expected m.sortDesc to be true after clicking sorted priority header")
	}
	if m.tasks[m.shown[0]].ID != "t1" || m.tasks[m.shown[1]].ID != "t3" || m.tasks[m.shown[2]].ID != "t2" {
		t.Fatalf("reverse priority shown = [%s, %s, %s], want [t1, t3, t2]",
			m.tasks[m.shown[0]].ID, m.tasks[m.shown[1]].ID, m.tasks[m.shown[2]].ID)
	}

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "p▲") {
		t.Fatalf("expected reverse glyph in colH, got view:\n%s", view)
	}

	up, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: colHeadY})
	m = up.(model)
	if m.sortDesc {
		t.Fatalf("expected m.sortDesc to be false after second click")
	}
	if m.tasks[m.shown[0]].ID != "t2" || m.tasks[m.shown[1]].ID != "t3" || m.tasks[m.shown[2]].ID != "t1" {
		t.Fatalf("ascending priority shown = [%s, %s, %s], want [t2, t3, t1]",
			m.tasks[m.shown[0]].ID, m.tasks[m.shown[1]].ID, m.tasks[m.shown[2]].ID)
	}
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, "p▼") {
		t.Fatalf("expected normal sort glyph in colH, got view:\n%s", view)
	}

	up, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: colHeadY})
	m = up.(model)
	if m.sortCol != sortStatus || m.sortDesc {
		t.Fatalf("expected sortStatus and sortDesc=false after switching column, got %v, %v", m.sortCol, m.sortDesc)
	}

	up, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: colHeadY})
	m = up.(model)
	if !m.sortDesc {
		t.Fatalf("expected sortDesc=true after clicking active status column")
	}
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, "▲ p") && !strings.Contains(view, "▲p") {
		t.Fatalf("expected reverse glyph in status colH, got view:\n%s", view)
	}

	up, _ = m.Update(tea.KeyPressMsg{Text: "i"})
	m = up.(model)
	if m.sortDesc {
		t.Fatalf("expected sortDesc=false after pressing key i")
	}
	up, _ = m.Update(tea.KeyPressMsg{Text: "i"})
	m = up.(model)
	if !m.sortDesc {
		t.Fatalf("expected sortDesc=true after pressing key i")
	}

	tTieA1 := task{ID: "tie-a1", Priority: 0, Project: "proj-a", CreatedAt: 100}
	tTieA2 := task{ID: "tie-a2", Priority: 1, Project: "proj-a", CreatedAt: 200}
	tTieB := task{ID: "tie-b", Priority: 0, Project: "proj-b", CreatedAt: 300}
	mTie := newModel(config{icons: true, refresh: time.Hour}, nil)
	mTie.sortCol = sortProject
	mTie.sortDesc = true
	mTie.tasks = []task{tTieA1, tTieA2, tTieB}
	mTie.rebuildShown()
	if len(mTie.shown) != 3 {
		t.Fatalf("expected 3 tasks shown in tie-breaker test, got %d", len(mTie.shown))
	}
	if mTie.tasks[mTie.shown[0]].ID != "tie-b" || mTie.tasks[mTie.shown[1]].ID != "tie-a1" || mTie.tasks[mTie.shown[2]].ID != "tie-a2" {
		t.Fatalf("expected primary descending with stable secondary tie-breaker [tie-b, tie-a1, tie-a2], got [%s, %s, %s]",
			mTie.tasks[mTie.shown[0]].ID, mTie.tasks[mTie.shown[1]].ID, mTie.tasks[mTie.shown[2]].ID)
	}

	mAscii := newModel(config{icons: false, refresh: time.Hour}, nil)
	mAscii.width = 120
	mAscii.height = 24
	mAscii.tasks = []task{t1, t2, t3}
	mAscii.rebuildShown()

	up, _ = mAscii.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: colHeadY})
	mAscii = up.(model)
	if !mAscii.sortDesc {
		t.Fatalf("expected mAscii.sortDesc to be true")
	}
	if mAscii.tasks[mAscii.shown[0]].ID != "t1" || mAscii.tasks[mAscii.shown[1]].ID != "t3" || mAscii.tasks[mAscii.shown[2]].ID != "t2" {
		t.Fatalf("expected reverse priority order in ascii mode, got [%s, %s, %s]",
			mAscii.tasks[mAscii.shown[0]].ID, mAscii.tasks[mAscii.shown[1]].ID, mAscii.tasks[mAscii.shown[2]].ID)
	}
	viewAscii := ansi.Strip(mAscii.View().Content)
	if !strings.Contains(viewAscii, "p^") && !strings.Contains(viewAscii, "p▲") {
		t.Fatalf("expected reverse glyph in ascii colH, got view:\n%s", viewAscii)
	}
}
