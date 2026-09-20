package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func sampleTasksForMouse() []task {
	body := strings.Repeat("line in task body\n", 30)
	tasks := make([]task, 5)
	for i := range tasks {
		tasks[i] = task{
			ID:       string(rune('a' + i)),
			Body:     body,
			Status:   "pending",
			Priority: 1,
		}
	}
	return tasks
}

func setupTestModel() model {
	m := newModel(config{icons: false, refresh: time.Hour}, nil)
	m.width = 100
	m.height = 24
	tasks := sampleTasksForMouse()
	m.tasks = tasks
	m.shown = []int{0, 1, 2, 3, 4}
	m.cursor = 0
	m.mode = modeTable
	m.stats = stats{
		Total:   5,
		Pending: 5,
		Leased:  0,
		Done:    0,
		Buried:  1,
	}
	m.hasStats = true
	m.syncDetail()
	return m
}

func TestMouseWheelHoverRouting(t *testing.T) {
	m := setupTestModel()
	panes := m.panes()

	if panes.detailRows <= 0 {
		t.Fatalf("expected detailRows > 0, got %d", panes.detailRows)
	}

	res, _ := m.Update(tea.MouseWheelMsg{
		X:      10,
		Y:      panes.detailTop + 2,
		Button: tea.MouseWheelDown,
	})
	m = res.(model)

	if m.cursor != 0 {
		t.Fatalf("cursor moved to %d; expected cursor to stay 0 on detail hover scroll", m.cursor)
	}
	if m.detail.YOffset() == 0 {
		t.Fatalf("expected detail viewport to scroll down, got YOffset 0")
	}

	offsetAfterDown := m.detail.YOffset()
	res, _ = m.Update(tea.MouseWheelMsg{
		X:      10,
		Y:      panes.detailTop + 2,
		Button: tea.MouseWheelUp,
	})
	m = res.(model)

	if m.cursor != 0 {
		t.Fatalf("cursor moved to %d on scroll up", m.cursor)
	}
	if m.detail.YOffset() >= offsetAfterDown {
		t.Fatalf("expected detail viewport to scroll up, got offset %d", m.detail.YOffset())
	}

	res, _ = m.Update(tea.MouseWheelMsg{
		X:      10,
		Y:      panes.tableTop + 1,
		Button: tea.MouseWheelDown,
	})
	m = res.(model)

	if m.cursor == 0 {
		t.Fatalf("expected table cursor to move on table scroll down, got 0")
	}

	m.mode = modeDetail
	m.cursor = 0
	res, _ = m.Update(tea.MouseWheelMsg{
		X:      10,
		Y:      panes.tableTop + 1,
		Button: tea.MouseWheelDown,
	})
	m = res.(model)

	if m.cursor == 0 {
		t.Fatalf("expected table cursor to move when scrolling over table in modeDetail, got 0")
	}
}

func TestMouseInteraction(t *testing.T) {
	m := setupTestModel()
	panes := m.panes()

	res, _ := m.Update(tea.MouseClickMsg{
		X:      5,
		Y:      panes.detailTop + 1,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.mode != modeDetail {
		t.Fatalf("expected modeDetail after click on detail pane, got %v", m.mode)
	}

	targetRow := 2
	res, _ = m.Update(tea.MouseClickMsg{
		X:      5,
		Y:      panes.tableTop + targetRow,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.mode != modeTable {
		t.Fatalf("expected modeTable after clicking table row, got %v", m.mode)
	}
	if m.cursor != targetRow {
		t.Fatalf("expected cursor %d after clicking row, got %d", targetRow, m.cursor)
	}
}

func TestClickFilterTabs(t *testing.T) {
	m := setupTestModel()
	m.projects = []string{"alpha", "beta"}

	renderedLines := strings.Split(m.View().Content, "\n")
	if len(renderedLines) < 2 {
		t.Fatalf("expected at least 2 lines rendered, got %d", len(renderedLines))
	}
	tabLineClean := ansi.Strip(renderedLines[1])

	bounds := m.row1Bounds()
	tabDefs := m.tabDefs()
	for i, target := range bounds.tabs {
		if target.end > len(tabLineClean) {
			t.Fatalf("tab %q end %d out of bounds for tab line %q", target.filter, target.end, tabLineClean)
		}
		tabSub := tabLineClean[target.start:target.end]
		def := tabDefs[i]
		if !strings.Contains(tabSub, def.name) || !strings.Contains(tabSub, def.key) {
			t.Fatalf("expected tab %q (%s) in slice %v, got %q in line %q", def.name, def.key, target, tabSub, tabLineClean)
		}
	}

	projSub := tabLineClean[bounds.proj[0]:bounds.proj[1]]
	if !strings.Contains(projSub, "project") || !strings.Contains(projSub, "p") {
		t.Fatalf("expected project label within %v, got %q in full line %q", bounds.proj, projSub, tabLineClean)
	}
	workerSub := tabLineClean[bounds.worker[0]:bounds.worker[1]]
	if !strings.Contains(workerSub, "worker") || !strings.Contains(workerSub, "w") {
		t.Fatalf("expected worker label within %v, got %q in full line %q", bounds.worker, workerSub, tabLineClean)
	}

	clickPoints := []struct {
		x          int
		wantFilter string
	}{
		{x: 12, wantFilter: "pending"},
		{x: 25, wantFilter: "leased"},
		{x: 37, wantFilter: "done"},
		{x: 47, wantFilter: "buried"},
		{x: 2, wantFilter: ""},
	}

	for _, cp := range clickPoints {
		res, _ := m.Update(tea.MouseClickMsg{
			X:      cp.x,
			Y:      1,
			Button: tea.MouseLeft,
		})
		m = res.(model)

		if m.filter != cp.wantFilter {
			t.Fatalf("expected filter %q after clicking at column %d, got %q", cp.wantFilter, cp.x, m.filter)
		}
	}

	m.mode = modeDetail
	res, _ := m.Update(tea.MouseClickMsg{
		X:      25,
		Y:      1,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.mode != modeTable {
		t.Fatalf("expected click on tab to switch from modeDetail to modeTable, got %v", m.mode)
	}
	if m.filter != "leased" {
		t.Fatalf("expected filter leased, got %q", m.filter)
	}

	res, _ = m.Update(tea.MouseClickMsg{
		X:      bounds.proj[0] + 5,
		Y:      1,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.project != "alpha" {
		t.Fatalf("expected project alpha after click on project, got %q", m.project)
	}

	res, _ = m.Update(tea.MouseClickMsg{
		X:      bounds.proj[0] + 5,
		Y:      1,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.project != "beta" {
		t.Fatalf("expected project beta after second click on project, got %q", m.project)
	}

	res, _ = m.Update(tea.MouseClickMsg{
		X:      bounds.proj[0] + 5,
		Y:      1,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.project != "" {
		t.Fatalf("expected empty project after third click on project, got %q", m.project)
	}
	res, _ = m.Update(tea.MouseClickMsg{
		X:      bounds.proj[1] + 1,
		Y:      1,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.project != "" {
		t.Fatalf("click in gap between project and worker should not cycle project, got %q", m.project)
	}
}

func TestClickScrollbarTrack(t *testing.T) {
	m := setupTestModel()
	totalTasks := 40
	tasks := make([]task, totalTasks)
	for i := range tasks {
		tasks[i] = task{
			ID:       string(rune('a' + (i % 26))),
			Body:     "line\n",
			Status:   "pending",
			Priority: 1,
		}
	}
	m.tasks = tasks
	shown := make([]int, totalTasks)
	for i := range shown {
		shown[i] = i
	}
	m.shown = shown
	m.cursor = 0
	m.offset = 0
	m.clamp()

	panes := m.panes()
	tRows := panes.tableRows
	if len(m.shown) <= tRows {
		t.Fatalf("expected shown (%d) > tableRows (%d)", len(m.shown), tRows)
	}

	sb := calcScrollbar(len(m.shown), m.offset, tRows)
	if !sb.hasScrollbar {
		t.Fatalf("expected scrollbar for 40 tasks in %d rows", tRows)
	}

	step := tRows / 2
	if step < 1 {
		step = 1
	}

	clickBelowY := panes.tableTop + sb.thumbStart + sb.thumbSize + 1
	if clickBelowY >= panes.tableTop+tRows {
		clickBelowY = panes.tableTop + tRows - 1
	}

	res, _ := m.Update(tea.MouseClickMsg{
		X:      m.width - 1,
		Y:      clickBelowY,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.offset != step {
		t.Fatalf("expected offset %d after clicking below thumb, got offset %d", step, m.offset)
	}

	res, _ = m.Update(tea.MouseClickMsg{
		X:      m.width - 1,
		Y:      clickBelowY,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.offset != 2*step {
		t.Fatalf("expected offset %d after second click below thumb, got %d", 2*step, m.offset)
	}

	sbAfterDown := calcScrollbar(len(m.shown), m.offset, tRows)
	if sbAfterDown.thumbStart <= sb.thumbStart {
		t.Fatalf("expected thumb to advance down, was %d, now %d", sb.thumbStart, sbAfterDown.thumbStart)
	}

	clickAboveY := panes.tableTop + sbAfterDown.thumbStart - 1
	res, _ = m.Update(tea.MouseClickMsg{
		X:      m.width - 1,
		Y:      clickAboveY,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.offset != step {
		t.Fatalf("expected offset %d after clicking above thumb, got %d", step, m.offset)
	}

	m.offset = 16
	m.cursor = 16
	m.clamp()
	sbMid := calcScrollbar(len(m.shown), m.offset, tRows)
	thumbCenterY := panes.tableTop + sbMid.thumbStart + sbMid.thumbSize/2
	res, _ = m.Update(tea.MouseClickMsg{
		X:      m.width - 1,
		Y:      thumbCenterY,
		Button: tea.MouseLeft,
	})
	m = res.(model)
	if m.offset != 16 {
		t.Fatalf("expected offset 16 to remain unchanged on thumb click, got %d", m.offset)
	}
	sbCenter := calcScrollbar(len(m.shown), m.offset, tRows)
	clickRow := thumbCenterY - panes.tableTop
	if clickRow < sbCenter.thumbStart || clickRow >= sbCenter.thumbStart+sbCenter.thumbSize {
		t.Fatalf("thumb ran away from click: clickRow %d not in [%d, %d)",
			clickRow, sbCenter.thumbStart, sbCenter.thumbStart+sbCenter.thumbSize)
	}

	m.mode = modeDetail
	res, _ = m.Update(tea.MouseClickMsg{
		X:      m.width - 1,
		Y:      thumbCenterY,
		Button: tea.MouseLeft,
	})
	m = res.(model)
	if m.mode != modeTable {
		t.Fatalf("expected modeTable after scrollbar click in modeDetail, got %v", m.mode)
	}

	shortModel := setupTestModel()
	shortPanes := shortModel.panes()
	if len(shortModel.shown) <= shortPanes.tableRows {
		res, _ = shortModel.Update(tea.MouseClickMsg{
			X:      shortModel.width - 1,
			Y:      shortPanes.tableTop + 2,
			Button: tea.MouseLeft,
		})
		shortModel = res.(model)
		if shortModel.cursor != 2 {
			t.Fatalf("expected row 2 selected when no scrollbar, got cursor %d", shortModel.cursor)
		}
	}
}
func TestClickDetailScrollbarTrack(t *testing.T) {
	m := setupTestModel()
	m.tasks[0].Body = strings.Repeat("detail line for scrollbar test\n", 60)
	m.syncDetail()

	panes := m.panes()
	vpMax := panes.detailViewportRows()
	if vpMax <= 0 {
		t.Fatalf("expected detail viewport height > 0, got %d", vpMax)
	}

	sb := calcScrollbar(m.detail.TotalLineCount(), m.detail.YOffset(), vpMax)
	if !sb.hasScrollbar {
		t.Fatalf("expected scrollbar for 60 lines in %d viewport rows", vpMax)
	}

	step := vpMax / 2
	if step < 1 {
		step = 1
	}

	clickBelowY := panes.detailViewportTop() + sb.thumbStart + sb.thumbSize + 1
	if clickBelowY >= panes.detailTop+panes.detailRows {
		clickBelowY = panes.detailTop + panes.detailRows - 1
	}

	res, _ := m.Update(tea.MouseClickMsg{
		X:      m.width - 1,
		Y:      clickBelowY,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.mode != modeDetail {
		t.Fatalf("expected modeDetail after scrollbar click, got %v", m.mode)
	}

	if m.detail.YOffset() != step {
		t.Fatalf("expected detail YOffset %d after clicking below thumb, got %d", step, m.detail.YOffset())
	}

	res, _ = m.Update(tea.MouseClickMsg{
		X:      m.width - 1,
		Y:      clickBelowY,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.detail.YOffset() != 2*step {
		t.Fatalf("expected detail YOffset %d after second click below thumb, got %d", 2*step, m.detail.YOffset())
	}

	sbAfterDown := calcScrollbar(m.detail.TotalLineCount(), m.detail.YOffset(), vpMax)
	if sbAfterDown.thumbStart <= sb.thumbStart {
		t.Fatalf("expected thumb to advance down, was %d, now %d", sb.thumbStart, sbAfterDown.thumbStart)
	}

	headerClickY := panes.detailTop + 1
	res, _ = m.Update(tea.MouseClickMsg{
		X:      m.width - 1,
		Y:      headerClickY,
		Button: tea.MouseLeft,
	})
	m = res.(model)
	if m.detail.YOffset() != 2*step {
		t.Fatalf("expected clicking header row to not scroll, got offset %d", m.detail.YOffset())
	}

	clickAboveY := panes.detailViewportTop() + sbAfterDown.thumbStart - 1
	res, _ = m.Update(tea.MouseClickMsg{
		X:      m.width - 1,
		Y:      clickAboveY,
		Button: tea.MouseLeft,
	})
	m = res.(model)

	if m.detail.YOffset() != step {
		t.Fatalf("expected detail YOffset %d after clicking above thumb, got %d", step, m.detail.YOffset())
	}

	sbCurrent := calcScrollbar(m.detail.TotalLineCount(), m.detail.YOffset(), vpMax)
	thumbCenterY := panes.detailViewportTop() + sbCurrent.thumbStart + sbCurrent.thumbSize/2
	res, _ = m.Update(tea.MouseClickMsg{
		X:      m.width - 1,
		Y:      thumbCenterY,
		Button: tea.MouseLeft,
	})
	m = res.(model)
	if m.detail.YOffset() != step {
		t.Fatalf("expected detail YOffset %d to remain unchanged on thumb click, got %d", step, m.detail.YOffset())
	}

	shortModel := setupTestModel()
	shortModel.tasks[0].Body = "short line"
	shortModel.syncDetail()
	shortPanes := shortModel.panes()
	res, _ = shortModel.Update(tea.MouseClickMsg{
		X:      shortModel.width - 1,
		Y:      shortPanes.detailViewportTop() + 1,
		Button: tea.MouseLeft,
	})
	shortModel = res.(model)
	if shortModel.detail.YOffset() != 0 {
		t.Fatalf("expected YOffset 0 when no scrollbar, got %d", shortModel.detail.YOffset())
	}
}

func TestClickFooterShortcuts(t *testing.T) {
	m := setupTestModel()
	m.width = 140

	renderedLines := strings.Split(m.View().Content, "\n")
	if len(renderedLines) == 0 {
		t.Fatal("expected rendered lines")
	}
	footerLine := ansi.Strip(renderedLines[len(renderedLines)-1])

	tokens := []string{
		"n new",
		"e edit",
		"+/- pri",
		"D delete",
		"x complete",
		"y/Y copy",
		"z zoom",
		"q quit",
		"? help",
	}

	for _, tok := range tokens {
		if !strings.Contains(footerLine, tok) {
			t.Fatalf("footer missing token %q in:\n%s", tok, footerLine)
		}
	}

	clickAt := func(md model, x int) (model, tea.Cmd) {
		res, cmd := md.Update(tea.MouseClickMsg{
			X:      x,
			Y:      md.height - 1,
			Button: tea.MouseLeft,
		})
		return res.(model), cmd
	}

	idxN := strings.Index(footerLine, "n new")
	modN, _ := clickAt(m, idxN)
	if modN.mode != modeForm || modN.form.editing {
		t.Fatalf("expected create form mode after clicking 'n new', got mode %v", modN.mode)
	}

	idxE := strings.Index(footerLine, "e edit")
	modE, _ := clickAt(m, idxE)
	if modE.mode != modeForm || !modE.form.editing {
		t.Fatalf("expected edit form mode after clicking 'e edit', got mode %v", modE.mode)
	}

	idxD := strings.Index(footerLine, "D delete")
	modD, _ := clickAt(m, idxD)
	if modD.mode != modeConfirm || modD.confirm.button != "delete" {
		t.Fatalf("expected confirm delete mode after clicking 'D delete', got mode %v confirm %+v", modD.mode, modD.confirm)
	}

	idxX := strings.Index(footerLine, "x complete")
	modX, _ := clickAt(m, idxX)
	if modX.mode != modeConfirm || modX.confirm.button != "complete" {
		t.Fatalf("expected confirm complete mode after clicking 'x complete', got mode %v confirm %+v", modX.mode, modX.confirm)
	}

	idxZ := strings.Index(footerLine, "z zoom")
	modZ, _ := clickAt(m, idxZ)
	if modZ.mode != modeZoom {
		t.Fatalf("expected zoom mode after clicking 'z zoom', got mode %v", modZ.mode)
	}
	footerZoom := ansi.Strip(strings.Split(modZ.View().Content, "\n")[len(renderedLines)-1])
	idxZInZoom := strings.Index(footerZoom, "z zoom")
	modZBack, _ := clickAt(modZ, idxZInZoom)
	if modZBack.mode != modeTable {
		t.Fatalf("expected table mode after clicking 'z zoom' again, got mode %v", modZBack.mode)
	}

	idxHelp := strings.Index(footerLine, "? help")
	modHelp, _ := clickAt(m, idxHelp)
	if modHelp.mode != modeHelp {
		t.Fatalf("expected help mode after clicking '? help', got mode %v", modHelp.mode)
	}

	idxPri := strings.Index(footerLine, "+/- pri")
	var lastPatchedPri int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/tasks/") {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if p, ok := body["priority"].(float64); ok {
				lastPatchedPri = int(p)
			}
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer ts.Close()

	mPri := setupTestModel()
	mPri.width = 140
	mPri.client = newClient(ts.URL)
	mPri.tasks[0].Priority = 5
	_, cmdPriUp := clickAt(mPri, idxPri)
	if cmdPriUp == nil {
		t.Fatal("expected actCmd after clicking '+' in priority shortcut")
	}
	cmdPriUp()
	if lastPatchedPri != 4 {
		t.Fatalf("expected priority 4 after clicking '+', got %d", lastPatchedPri)
	}
	_, cmdPriDown := clickAt(mPri, idxPri+2)
	if cmdPriDown == nil {
		t.Fatal("expected actCmd after clicking '-' in priority shortcut")
	}
	cmdPriDown()
	if lastPatchedPri != 6 {
		t.Fatalf("expected priority 6 after clicking '-', got %d", lastPatchedPri)
	}

	idxCopy := strings.Index(footerLine, "y/Y copy")
	_, cmdCopyID := clickAt(m, idxCopy)
	if cmdCopyID == nil {
		t.Fatal("expected clipboard cmd after clicking 'y' in copy shortcut")
	}
	_, cmdCopyBody := clickAt(m, idxCopy+2)
	if cmdCopyBody == nil {
		t.Fatal("expected clipboard cmd after clicking 'Y' in copy shortcut")
	}
	_, cmdCopyLabel := clickAt(m, idxCopy+4)
	if cmdCopyLabel == nil {
		t.Fatal("expected clipboard cmd after clicking label in copy shortcut")
	}

	idxP := strings.Index(footerLine, "p project")
	modP, _ := clickAt(m, idxP)
	if modP.project == m.project && len(m.projects) > 0 {
		t.Fatalf("expected project to cycle on click, got %q", modP.project)
	}

	idxW := strings.Index(footerLine, "w worker")
	modW, _ := clickAt(m, idxW)
	if modW.worker == m.worker && len(m.workers) > 0 {
		t.Fatalf("expected worker to cycle on click, got %q", modW.worker)
	}
	idxJK := strings.Index(footerLine, "j/k move")
	_, cmdJK := clickAt(m, idxJK)
	if cmdJK != nil {
		t.Fatal("expected no action when clicking 'j/k move' shortcut")
	}

	idxFilt := strings.Index(footerLine, "0-4 filter")
	_, cmdFilt := clickAt(m, idxFilt)
	if cmdFilt != nil {
		t.Fatal("expected no action when clicking '0-4 filter' shortcut")
	}

	idxQ := strings.Index(footerLine, "q quit")
	_, cmdQuit := clickAt(m, idxQ)
	if cmdQuit == nil {
		t.Fatal("expected quit cmd after clicking 'q quit'")
	}
	quitMsg := cmdQuit()
	if _, ok := quitMsg.(tea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg after clicking 'q quit', got %T (%v)", quitMsg, quitMsg)
	}
	idxGap := idxN + len("n new")
	modGap, _ := clickAt(m, idxGap)
	if modGap.mode != modeTable {
		t.Fatalf("expected click in gap between tokens ignored, got mode %v", modGap.mode)
	}

	mZero := setupTestModel()
	mZero.height = 0
	modZero, _ := mZero.Update(tea.MouseClickMsg{
		X:      idxN,
		Y:      23,
		Button: tea.MouseLeft,
	})
	if modZero.(model).mode != modeTable {
		t.Fatalf("expected click ignored when height <= 0, got mode %v", modZero.(model).mode)
	}

	mHelp := setupTestModel()
	mHelp.mode = modeHelp
	modHelpClick, _ := mHelp.Update(tea.MouseClickMsg{
		X:      idxD,
		Y:      mHelp.height - 1,
		Button: tea.MouseLeft,
	})
	if modHelpClick.(model).mode != modeHelp {
		t.Fatalf("expected click on footer ignored in modeHelp, got mode %v", modHelpClick.(model).mode)
	}
}
func TestClickWorkerChip(t *testing.T) {
	m := setupTestModel()
	m.workers = []string{"worker-1", "worker-2"}

	clickChip := func(cur model) (model, tea.Cmd) {
		bounds := cur.row1Bounds()
		if bounds.worker[0] >= bounds.worker[1] {
			t.Fatalf("expected valid worker chip bounds, got %v", bounds.worker)
		}
		res, cmd := cur.Update(tea.MouseClickMsg{
			X:      (bounds.worker[0] + bounds.worker[1]) / 2,
			Y:      headerRows,
			Button: tea.MouseLeft,
		})
		return res.(model), cmd
	}

	var cmd tea.Cmd
	for _, wantWorker := range []string{"worker-1", "worker-2", ""} {
		m, cmd = clickChip(m)
		if cmd == nil {
			t.Fatalf("expected non-nil rescope command after clicking worker chip")
		}
		if m.worker != wantWorker {
			t.Fatalf("expected worker %q, got %q", wantWorker, m.worker)
		}
	}

	for _, startMode := range []mode{modeDetail, modeZoom} {
		m.mode = startMode
		m, cmd = clickChip(m)
		if cmd == nil {
			t.Fatalf("expected non-nil rescope command after clicking worker chip from mode %v", startMode)
		}
		if m.mode != modeTable {
			t.Fatalf("expected modeTable after click from mode %v, got %v", startMode, m.mode)
		}
	}

	bounds := m.row1Bounds()
	if bounds.proj[1] >= bounds.worker[0] {
		t.Fatalf("expected gap between project and worker chip, got proj=%v worker=%v", bounds.proj, bounds.worker)
	}
	gapX := (bounds.proj[1] + bounds.worker[0]) / 2
	prevWorker := m.worker
	res, gapCmd := m.Update(tea.MouseClickMsg{
		X:      gapX,
		Y:      headerRows,
		Button: tea.MouseLeft,
	})
	m = res.(model)
	if gapCmd != nil {
		t.Fatalf("expected nil command on click in gap, got %v", gapCmd)
	}
	if m.worker != prevWorker {
		t.Fatalf("expected worker unchanged on click in gap, got %q", m.worker)
	}
}
