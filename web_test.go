package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestWebUIFetchTimeout(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required to run the web UI harness")
		}
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/timeout.js", "web/index.html").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("harness failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("harness failed: %v", err)
	}

	var got struct {
		BootRows        []string `json:"bootRows"`
		BootCount       string   `json:"bootCount"`
		BootStatus      string   `json:"bootStatus"`
		BootDisplay     string   `json:"bootDisplay"`
		WindowRequests  int      `json:"windowRequests"`
		StaleRows       []string `json:"staleRows"`
		OfflineStatus   string   `json:"offlineStatus"`
		OfflineHidden   bool     `json:"offlineHidden"`
		RetryRequests   int      `json:"retryRequests"`
		RecoveredRows   []string `json:"recoveredRows"`
		RecoveredHidden bool     `json:"recoveredHidden"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if len(got.BootRows) != 3 || got.BootCount != "Showing 1-3 of 3" {
		t.Errorf("boot state = %v, count = %q, want 3 tasks", got.BootRows, got.BootCount)
	}
	if got.WindowRequests != 0 {
		t.Errorf("requests issued during timeout window = %d, want 0", got.WindowRequests)
	}
	if !strings.Contains(got.OfflineStatus, "Offline") || got.OfflineHidden {
		t.Errorf("offline status = %q, hidden = %v, want visible offline indicator", got.OfflineStatus, got.OfflineHidden)
	}
	if got.RetryRequests == 0 {
		t.Errorf("requests issued on poll tick after timeout window = 0, want > 0")
	}
	wantRecovered := []string{"41764ca4", "be3024c2", "3642f64f"}
	if !slices.Equal(got.RecoveredRows, wantRecovered) {
		t.Errorf("recovered rows = %v, want %v", got.RecoveredRows, wantRecovered)
	}
	if !got.RecoveredHidden {
		t.Errorf("recovered status hidden = %v, want true", got.RecoveredHidden)
	}
}

func TestWebUIInitialPlaceholdersAndNoscript(t *testing.T) {
	ui := string(uiHTML)
	stats := []string{"stat-pending", "stat-leased", "stat-done", "stat-buried", "stat-total"}
	for _, id := range stats {
		placeholder := `id="` + id + `">-`
		if !strings.Contains(ui, placeholder) {
			t.Errorf("expected placeholder %q in web/index.html", placeholder)
		}
		zero := `id="` + id + `">0`
		if strings.Contains(ui, zero) {
			t.Errorf("found hard-coded zero %q in web/index.html", zero)
		}
	}

	if !strings.Contains(ui, "<noscript") {
		t.Fatal("expected <noscript> block in web/index.html")
	}
	if !strings.Contains(ui, "CLI") || !strings.Contains(ui, "/tasks") {
		t.Error("expected noscript block to name CLI and API alternatives")
	}

	if !strings.Contains(ui, `id="queue-count"`) {
		t.Fatal("expected #queue-count in web/index.html")
	}
	if strings.Contains(ui, `id="queue-count" style="color: var(--text-muted); font-size: 12px;">0 tasks</span>`) {
		t.Error("expected queue-count to not start with hard-coded 0 tasks")
	}

	if !strings.Contains(ui, `id="task-table-body"`) {
		t.Fatal("expected #task-table-body in web/index.html")
	}
	if !strings.Contains(ui, "Not connected") {
		t.Error("expected initial table body to indicate Not connected")
	}
	if strings.Contains(ui, `<tbody id="task-table-body">`+"\n"+`            <tr><td colspan="7" style="text-align: center; color: var(--text-muted);">No tasks</td></tr>`) {
		t.Error("table body should not claim No tasks in initial markup")
	}

	if !strings.Contains(ui, "<main") || !strings.Contains(ui, "</main>") {
		t.Error("expected <main> landmark in web/index.html")
	}
	if !strings.Contains(ui, "<aside") || !strings.Contains(ui, "</aside>") {
		t.Error("expected <aside> landmark in web/index.html")
	}
	if !strings.Contains(ui, `<dl class="task-metadata">`) {
		t.Error("expected <dl class=\"task-metadata\"> definition list in web/index.html")
	}
	if !strings.Contains(ui, "@media (prefers-color-scheme: light)") {
		t.Error("expected prefers-color-scheme light media query in web/index.html")
	}
	if !strings.Contains(ui, "@media (prefers-reduced-motion: reduce)") {
		t.Error("expected prefers-reduced-motion media query in web/index.html")
	}
}

func TestWebUIExpiredLeaseActions(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, "function isActivelyLeased(t)") {
		t.Fatal("expected isActivelyLeased helper in web/index.html")
	}
	if strings.Contains(ui, `id="edit-task-btn"${t.status === 'leased'`) {
		t.Error("edit-task-btn should not unconditionally disable on leased status")
	}
	if !strings.Contains(ui, `id="edit-task-btn"${isActivelyLeased(t) ? ' disabled aria-disabled="true" title="Actively leased tasks cannot be edited"' : ''}`) {
		t.Error("edit-task-btn should check isActivelyLeased(t)")
	}
	if strings.Contains(ui, `id="delete-task-btn" data-variant="warning"${t.status === 'leased'`) {
		t.Error("delete-task-btn should not unconditionally disable on leased status")
	}
	if !strings.Contains(ui, `id="delete-task-btn" data-variant="warning"${isActivelyLeased(t) ? ' disabled aria-disabled="true" title="Actively leased tasks cannot be deleted"' : ''}`) {
		t.Error("delete-task-btn should check isActivelyLeased(t)")
	}
	if strings.Contains(ui, "if (!id || status === 'leased') return") {
		t.Error("deleteTask should not unconditionally return on leased status")
	}
	if !strings.Contains(ui, "if (!t || !t.id || isActivelyLeased(t)) return") {
		t.Error("deleteTask should guard on active lease expiration")
	}
	if strings.Contains(ui, "currentTask.status === 'leased'") {
		t.Error("task edit handlers should not unconditionally return on leased status")
	}
	if !strings.Contains(ui, "if (!currentTask || currentTask.status === 'done' || isActivelyLeased(currentTask)) return") {
		t.Error("task edit handlers should guard on active lease expiration")
	}
}
func TestWebUIConfirmActions(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, "confirm('Complete this task as done?')") {
		t.Error("expected completeTask to require confirmation before completion")
	}
	if !strings.Contains(ui, "confirm('Close this task as done without a result?')") {
		t.Error("expected closeTask to require confirmation before closing")
	}
}
func TestWebUIWorkerStats(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, `<select id="filter-worker" onchange="onFilterChange()">`) {
		t.Fatal("expected filter-worker select to trigger onFilterChange")
	}
	if !strings.Contains(ui, "const workerEl = document.getElementById('filter-worker');") {
		t.Fatal("expected loadStats to read filter-worker element")
	}
	if !strings.Contains(ui, "'worker=' + encodeURIComponent(worker)") {
		t.Fatal("expected loadStats to pass encoded worker to /stats")
	}
}
func TestWebUINotesTimelineAndForm(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, `id="add-note-form"`) {
		t.Error("expected #add-note-form in web/index.html")
	}
	if strings.Contains(ui, `<form id="add-note-form" onsubmit=`) {
		t.Error("add-note-form should not use inline onsubmit attribute")
	}
	if !strings.Contains(ui, `id="note-author"`) {
		t.Error("expected #note-author input in web/index.html")
	}
	if !strings.Contains(ui, `id="note-text"`) {
		t.Error("expected #note-text textarea in web/index.html")
	}
	if !strings.Contains(ui, `id="task-notes-list"`) {
		t.Error("expected #task-notes-list container in web/index.html")
	}
	if !strings.Contains(ui, "renderNotesList(t.notes)") {
		t.Error("expected renderNotesList call in task details pane")
	}
	if !strings.Contains(ui, "note-author") || !strings.Contains(ui, "note-timestamp") || !strings.Contains(ui, "note-text") {
		t.Error("expected note author, timestamp, and text markup in web/index.html")
	}
	if !strings.Contains(ui, "/notes") {
		t.Error("expected note endpoint call in web/index.html")
	}
	if !strings.Contains(ui, "noteForm.addEventListener('submit'") {
		t.Error("expected noteForm submit event listener in web/index.html")
	}
	if !strings.Contains(ui, "form.requestSubmit") {
		t.Error("expected form.requestSubmit call on Enter keydown in note textarea")
	}
	if !strings.Contains(ui, "renderTaskDetails(currentTask, true)") {
		t.Error("expected re-render of task details without full page reload")
	}
}
func TestWebUITaskSubmitErrorMapping(t *testing.T) {
	db, err := openDB(t.TempDir()+"/test.db", 300)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	cases := []struct {
		payload   string
		wantCode  int
		wantError string
		wantField string
	}{
		{`{"project":"bad/proj","body":"test"}`, http.StatusBadRequest, "invalid project", "project"},
		{`{"body":"test"}`, http.StatusBadRequest, "missing project", "project"},
		{`{"project":"p","body":"test","priority":-1}`, http.StatusBadRequest, "invalid priority -1, must be 0 or greater", "priority"},
		{`{"project":"p","body":"test","id":"bad id!"}`, http.StatusBadRequest, "invalid id", "id"},
		{`{"project":"p"}`, http.StatusBadRequest, "missing asset_path or body", "body"},
		{`{"project":"p","body":"   "}`, http.StatusBadRequest, "invalid body", "body"},
	}

	for _, tc := range cases {
		resp, err := http.Post(srv.URL+"/tasks", "application/json", strings.NewReader(tc.payload))
		if err != nil {
			t.Fatalf("POST /tasks failed: %v", err)
		}
		if resp.StatusCode != tc.wantCode {
			resp.Body.Close()
			t.Fatalf("POST %s status = %d, want %d", tc.payload, resp.StatusCode, tc.wantCode)
		}
		var apiErr struct {
			Error string `json:"error"`
			Field string `json:"field"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
			resp.Body.Close()
			t.Fatalf("decode response failed: %v", err)
		}
		resp.Body.Close()
		if apiErr.Error != tc.wantError || apiErr.Field != tc.wantField {
			t.Errorf("POST %s got error=%q field=%q, want error=%q field=%q",
				tc.payload, apiErr.Error, apiErr.Field, tc.wantError, tc.wantField)
		}
	}
}
func TestWebUISaveTaskEditVersionConflict(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available: " + err.Error())
	}
	out, err := exec.Command(node, "testdata/task_edit_cas.js", "web/index.html").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("cas harness failed: %v\nstderr:\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("cas harness failed: %v", err)
	}

	var got struct {
		SentVersion  int    `json:"sentVersion"`
		BannerHidden bool   `json:"bannerHidden"`
		BannerText   string `json:"bannerText"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if got.SentVersion != 3 {
		t.Errorf("expected PATCH payload if_version = 3, got %d", got.SentVersion)
	}
	if got.BannerHidden || got.BannerText != "version conflict" {
		t.Errorf("expected visible 409 error banner with 'version conflict', got hidden=%v text=%q",
			got.BannerHidden, got.BannerText)
	}
}
func TestWebUISubmitBusyState(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available: " + err.Error())
	}
	out, err := exec.Command(node, "testdata/submit_busy.js", "web/index.html").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("harness failed: %v\nstderr:\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("harness failed: %v", err)
	}

	var got struct {
		InFlightDisabled bool   `json:"inFlightDisabled"`
		InFlightBusy     string `json:"inFlightBusy"`
		SettledDisabled  bool   `json:"settledDisabled"`
		SettledBusy      string `json:"settledBusy"`
		FetchCount       int    `json:"fetchCount"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if !got.InFlightDisabled {
		t.Errorf("expected submit button disabled while in-flight, got %v", got.InFlightDisabled)
	}
	if got.InFlightBusy != "true" {
		t.Errorf("expected aria-busy 'true' while in-flight, got %q", got.InFlightBusy)
	}
	if got.SettledDisabled {
		t.Errorf("expected submit button re-enabled on settle, got %v", got.SettledDisabled)
	}
	if got.SettledBusy != "false" {
		t.Errorf("expected aria-busy 'false' on settle, got %q", got.SettledBusy)
	}
	if got.FetchCount != 1 {
		t.Errorf("expected 1 fetch, got %d (duplicate submission was not blocked)", got.FetchCount)
	}
}

func TestWebUIDetailsPaneFocusOnSelection(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, `id="task-details"`) || !strings.Contains(ui, `tabindex="-1"`) {
		t.Error("expected details container with id=\"task-details\" and tabindex=\"-1\" in web/index.html")
	}

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available: " + err.Error())
	}
	out, err := exec.Command(node, "testdata/details_focus.js", "web/index.html").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("details focus harness failed: %v\nstderr:\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("details focus harness failed: %v", err)
	}

	var got struct {
		ClickFocus     bool `json:"clickFocus"`
		CtrlIgnored    bool `json:"ctrlIgnored"`
		InputIgnored   bool `json:"inputIgnored"`
		EnterFocus     bool `json:"enterFocus"`
		EnterPrevented bool `json:"enterPrevented"`
		ArrowFocus     bool `json:"arrowFocus"`
		JFocus         bool `json:"jFocus"`
		KFocus         bool `json:"kFocus"`
		ArrowUpFocus   bool `json:"arrowUpFocus"`
		HomeFocus      bool `json:"homeFocus"`
		EndFocus       bool `json:"endFocus"`
		PageDownFocus  bool `json:"pageDownFocus"`
		PageUpFocus    bool `json:"pageUpFocus"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if !got.ClickFocus {
		t.Errorf("expected click to keep focus on clicked row, got %v", got.ClickFocus)
	}
	if !got.CtrlIgnored {
		t.Errorf("expected Ctrl+Enter to be ignored by row activation, got %v", got.CtrlIgnored)
	}
	if !got.InputIgnored {
		t.Errorf("expected keydown on inputs inside row to be ignored, got %v", got.InputIgnored)
	}
	if !got.EnterFocus {
		t.Errorf("expected enter on row to move focus to details container, got %v", got.EnterFocus)
	}
	if !got.EnterPrevented {
		t.Errorf("expected enter keydown default to be prevented, got %v", got.EnterPrevented)
	}
	if !got.ArrowFocus {
		t.Errorf("expected arrow navigation to focus adjacent row, got %v", got.ArrowFocus)
	}
	if !got.JFocus {
		t.Errorf("expected j keydown to focus next row, got %v", got.JFocus)
	}
	if !got.KFocus {
		t.Errorf("expected k keydown to focus previous row, got %v", got.KFocus)
	}
	if !got.ArrowUpFocus {
		t.Errorf("expected ArrowUp keydown to focus previous row, got %v", got.ArrowUpFocus)
	}
	if !got.HomeFocus {
		t.Errorf("expected Home keydown to focus first row, got %v", got.HomeFocus)
	}
	if !got.EndFocus {
		t.Errorf("expected End keydown to focus last row, got %v", got.EndFocus)
	}
	if !got.PageDownFocus {
		t.Errorf("expected PageDown keydown to focus lower row, got %v", got.PageDownFocus)
	}
	if !got.PageUpFocus {
		t.Errorf("expected PageUp keydown to focus upper row, got %v", got.PageUpFocus)
	}
}
func TestWebUIFieldHintsAndCharacterCount(t *testing.T) {
	ui := string(uiHTML)
	hints := []string{
		`class="hint" id="form-project-hint"`,
		`class="hint" id="form-priority-hint"`,
		`class="hint" id="form-body-hint"`,
		`class="hint" id="form-asset-hint"`,
		`class="hint" id="form-id-hint"`,
		`class="hint" id="form-body-count"`,
	}
	for _, h := range hints {
		if !strings.Contains(ui, h) {
			t.Errorf("expected hint markup %q in web/index.html", h)
		}
	}
	if !strings.Contains(ui, `aria-describedby="form-project-hint form-project-error"`) {
		t.Error("expected form-project to link hint in aria-describedby")
	}
	if !strings.Contains(ui, `aria-describedby="form-body-hint form-body-count form-body-error"`) {
		t.Error("expected form-body to link hint and count in aria-describedby")
	}
	if strings.Contains(ui, `id="form-body-count" aria-live=`) {
		t.Error("form-body-count should not have aria-live to avoid screen reader chatter on keystrokes")
	}
	if !strings.Contains(ui, `function updateBodyCount()`) {
		t.Error("expected updateBodyCount helper in web/index.html")
	}
}

func TestWebUIServe(t *testing.T) {
	db, err := openDB(t.TempDir()+"/test.db", 300)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui")
	if err != nil {
		t.Fatalf("GET /ui: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestWebUIButtonVariants(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, `button[data-variant="warning"]`) {
		t.Error("expected button[data-variant=\"warning\"] in web/index.html")
	}
	if !strings.Contains(ui, `button[data-variant="secondary"]`) {
		t.Error("expected button[data-variant=\"secondary\"] in web/index.html")
	}
	if !strings.Contains(ui, `button[data-variant="primary"]`) {
		t.Error("expected button[data-variant=\"primary\"] in web/index.html")
	}
	if !strings.Contains(ui, `id="submit-task-btn" data-variant="primary"`) {
		t.Error("expected submit-task-btn to have data-variant=\"primary\"")
	}
	if !strings.Contains(ui, `id="refresh-btn" data-variant="secondary"`) {
		t.Error("expected refresh-btn to have data-variant=\"secondary\"")
	}
	if !strings.Contains(ui, `id="delete-task-btn" data-variant="warning"`) {
		t.Error("expected delete-task-btn to have data-variant=\"warning\"")
	}
}

func TestWebUIPurgeDone(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, `id="purge-done-btn"`) {
		t.Fatal("expected #purge-done-btn in web/index.html")
	}
	if !strings.Contains(ui, `onclick="openPurgeModal()"`) {
		t.Fatal("expected #purge-done-btn to trigger openPurgeModal()")
	}
	if !strings.Contains(ui, `aria-haspopup="dialog"`) {
		t.Fatal("expected #purge-done-btn to declare aria-haspopup=dialog")
	}
	if !strings.Contains(ui, `<dialog id="purge-modal"`) {
		t.Fatal("expected #purge-modal dialog in web/index.html")
	}
	if !strings.Contains(ui, `data-state="closed"`) {
		t.Fatal("expected #purge-modal to start with data-state=closed")
	}
	if !strings.Contains(ui, `id="purge-confirm-btn"`) {
		t.Fatal("expected #purge-confirm-btn in web/index.html")
	}
	if !strings.Contains(ui, `onclick="confirmPurge()"`) {
		t.Fatal("expected #purge-confirm-btn to trigger confirmPurge()")
	}
	if !strings.Contains(ui, `id="purge-cancel-btn"`) {
		t.Fatal("expected #purge-cancel-btn in web/index.html")
	}
	if !strings.Contains(ui, `onclick="closePurgeModal()"`) {
		t.Fatal("expected #purge-cancel-btn to trigger closePurgeModal()")
	}
}
func TestWebUIGlobalErrorBoundary(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available: " + err.Error())
	}
	out, err := exec.Command(node, "testdata/error_boundary.js", "web/index.html").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("error boundary harness failed: %v\nstderr:\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("error boundary harness failed: %v", err)
	}

	var got struct {
		ErrorCaptured struct {
			Hidden bool   `json:"hidden"`
			Text   string `json:"text"`
		} `json:"errorCaptured"`
		RejectionCaptured struct {
			Hidden bool   `json:"hidden"`
			Text   string `json:"text"`
		} `json:"rejectionCaptured"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if got.ErrorCaptured.Hidden || got.ErrorCaptured.Text != "test uncaught error" {
		t.Errorf("error banner not shown on uncaught error: %+v", got.ErrorCaptured)
	}
	if got.RejectionCaptured.Hidden || got.RejectionCaptured.Text != "test unhandled rejection" {
		t.Errorf("error banner not shown on unhandled rejection: %+v", got.RejectionCaptured)
	}
}

func TestWebUIRowDOMCreation(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available: " + err.Error())
	}
	out, err := exec.Command(node, "testdata/create_row.js", "web/index.html").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("create_row harness failed: %v\nstderr:\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("create_row harness failed: %v", err)
	}

	var got struct {
		BtnTitle          string `json:"btnTitle"`
		BtnText           string `json:"btnText"`
		ProjText          string `json:"projText"`
		ProjChildCount    int    `json:"projChildCount"`
		StatusText        string `json:"statusText"`
		StatusAttr        string `json:"statusAttr"`
		PrioText          string `json:"prioText"`
		ClaimText         string `json:"claimText"`
		WorkerText        string `json:"workerText"`
		WorkerChildCount  int    `json:"workerChildCount"`
		SummaryText       string `json:"summaryText"`
		SummaryChildCount int    `json:"summaryChildCount"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if got.BtnTitle != "task-12345678" || got.BtnText != "task-123…" {
		t.Errorf("unexpected button title/text: %q / %q", got.BtnTitle, got.BtnText)
	}
	if got.ProjText != "<script>bad()</script>" || got.ProjChildCount != 0 {
		t.Errorf("project cell not treated as textContent: text=%q, children=%d", got.ProjText, got.ProjChildCount)
	}
	if got.SummaryText != "<b>summary</b>" || got.SummaryChildCount != 0 {
		t.Errorf("summary cell not treated as textContent: text=%q, children=%d", got.SummaryText, got.SummaryChildCount)
	}
	if got.WorkerText != "worker<1>" || got.WorkerChildCount != 0 {
		t.Errorf("worker cell not treated as textContent: text=%q, children=%d", got.WorkerText, got.WorkerChildCount)
	}
	if got.StatusText != "pending" || got.StatusAttr != "pending" {
		t.Errorf("badge not configured: text=%q, attr=%q", got.StatusText, got.StatusAttr)
	}
}

func TestWebUISelectPollReconciliation(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available: " + err.Error())
	}
	out, err := exec.Command(node, "testdata/select_reconcile.js", "web/index.html").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("select reconcile harness failed: %v\nstderr:\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("select reconcile harness failed: %v", err)
	}

	var got struct {
		WorkerNodePreservedOnSame    bool    `json:"workerNodePreservedOnSame"`
		WorkerNodePreservedOnChange  bool    `json:"workerNodePreservedOnChange"`
		ProjectNodePreservedOnSame   bool    `json:"projectNodePreservedOnSame"`
		ProjectNodePreservedOnChange bool    `json:"projectNodePreservedOnChange"`
		UnassignedIndex              int     `json:"unassignedIndex"`
		UnassignedWorker             *string `json:"unassignedWorker"`
		UnassignedSyncURL            string  `json:"unassignedSyncURL"`
		AllIndex                     int     `json:"allIndex"`
		AllWorker                    *string `json:"allWorker"`
		NamedIndex                   int     `json:"namedIndex"`
		NamedWorker                  *string `json:"namedWorker"`
		IdleIndexAfter               int     `json:"idleIndexAfter"`
		IdleWorkerAfter              *string `json:"idleWorkerAfter"`
		IdleWorkerValue              string  `json:"idleWorkerValue"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if !got.WorkerNodePreservedOnSame {
		t.Error("worker option DOM node replaced when workers list unchanged")
	}
	if !got.WorkerNodePreservedOnChange {
		t.Error("worker option DOM node not preserved when workers list updated")
	}
	if !got.ProjectNodePreservedOnSame {
		t.Error("project option DOM node replaced when projects list unchanged")
	}
	if !got.ProjectNodePreservedOnChange {
		t.Error("project option DOM node not preserved when projects list updated")
	}
	if got.UnassignedIndex != 1 || got.UnassignedWorker == nil || *got.UnassignedWorker != "" {
		t.Errorf("expected unassigned worker state at index 1 with '', got index %d, worker %v", got.UnassignedIndex, got.UnassignedWorker)
	}
	if got.AllIndex != 0 || got.AllWorker != nil {
		t.Errorf("expected all worker state at index 0 with null, got index %d, worker %v", got.AllIndex, got.AllWorker)
	}
	if got.NamedIndex != 2 || got.NamedWorker == nil || *got.NamedWorker != "w1" {
		t.Errorf("expected named worker state at index 2 with 'w1', got index %d, worker %v", got.NamedIndex, got.NamedWorker)
	}
	if !strings.Contains(got.UnassignedSyncURL, "worker=") || strings.Contains(got.UnassignedSyncURL, "worker=none") {
		t.Errorf("expected unassigned sync URL to set empty worker query, got %q", got.UnassignedSyncURL)
	}
	if got.IdleWorkerAfter == nil || *got.IdleWorkerAfter != "idle-worker" {
		t.Errorf("expected idle-worker to be retained after empty workers poll, got %v", got.IdleWorkerAfter)
	}
	if got.IdleWorkerValue != "idle-worker" {
		t.Errorf("expected worker select value to be idle-worker, got %q", got.IdleWorkerValue)
	}
}

func TestWebUIUnassignedWorkerFilter(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	postJSON := func(endpoint string, body any) {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}
		resp, err := http.Post(srv.URL+endpoint, "application/json", strings.NewReader(string(data)))
		if err != nil {
			t.Fatalf("POST %s failed: %v", endpoint, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("POST %s status %d: %s", endpoint, resp.StatusCode, b)
		}
	}

	postJSON("/tasks", map[string]string{"id": "t-unassigned", "project": "p", "body": "unclaimed task"})
	postJSON("/tasks", map[string]string{"id": "t-claimed", "project": "p", "body": "claimed task"})
	postJSON("/tasks/t-claimed/claim", map[string]string{"worker": "worker-1"})

	resp, err := http.Get(srv.URL + "/tasks?worker=")
	if err != nil {
		t.Fatalf("GET /tasks?worker= failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /tasks?worker=: got %d, want 200", resp.StatusCode)
	}
	var tasks []struct {
		ID     string `json:"id"`
		Worker string `json:"worker"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "t-unassigned" || tasks[0].Worker != "" {
		t.Fatalf("expected only unclaimed tasks from ?worker=, got %+v", tasks)
	}
}

func TestWebUIEditTaskPrefill(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available: " + err.Error())
	}
	out, err := exec.Command(node, "testdata/task_edit_prefill.js", "web/index.html").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("edit prefill harness failed: %v\nstderr:\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("edit prefill harness failed: %v", err)
	}

	var got struct {
		IsEditing bool   `json:"isEditing"`
		Project   string `json:"project"`
		Priority  string `json:"priority"`
		AssetPath string `json:"assetPath"`
		Body      string `json:"body"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if !got.IsEditing {
		t.Errorf("expected isEditing = true after clicking Edit Task, got %v", got.IsEditing)
	}
	if got.Project != "proj-alpha" {
		t.Errorf("expected project %q, got %q", "proj-alpha", got.Project)
	}
	if got.Priority != "15" {
		t.Errorf("expected priority %q, got %q", "15", got.Priority)
	}
	if got.AssetPath != "/images/test.png" {
		t.Errorf("expected asset path %q, got %q", "/images/test.png", got.AssetPath)
	}
	if got.Body != "Fix the widget layout" {
		t.Errorf("expected body %q, got %q", "Fix the widget layout", got.Body)
	}
}

func TestWebUICopyPrimitivesButton(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, `id="copy-primitives-btn"`) {
		t.Fatal("expected #copy-primitives-btn in web/index.html")
	}
	if !strings.Contains(ui, `id="detail-task-prim-row"`) {
		t.Fatal("expected #detail-task-prim-row in web/index.html")
	}
	if !strings.Contains(ui, `copyToClipboard(primStr, copyPrimBtn, 'primitives')`) {
		t.Fatal("expected copyToClipboard call for primitives in web/index.html")
	}
	if !strings.Contains(ui, `primRow.hidden = false`) {
		t.Fatal("expected primRow visibility toggled in web/index.html")
	}
}

func TestWebUICanonicalizeSelectedTaskIDFromPrefix(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, "if (t && t.id && t.id !== id)") {
		t.Fatal("expected prefix canonicalization check in loadTaskDetails")
	}
	if !strings.Contains(ui, "selectedTaskId = t.id") {
		t.Fatal("expected selectedTaskId update to canonical ID")
	}

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available: " + err.Error())
	}

	script := `
const fs = require("fs");
const src = fs.readFileSync(process.argv[1], "utf8");
const code = src.slice(src.indexOf("<script>") + 8, src.lastIndexOf("</script>"));

const fullId = "81b0a368767072bdcde9d92ff31814e6";
const prefix = "81b0a36";
const attrs = {};
const tr = {
  dataset: { taskId: fullId },
  setAttribute(k, v) { attrs[k] = String(v); },
  getAttribute(k) { return attrs[k] || null; },
  removeAttribute(k) { delete attrs[k]; },
};
const tbody = { querySelectorAll: () => [tr], addEventListener() {} };
const els = {
  "task-table-body": tbody,
  "task-details-content": {},
  "filter-project": { options: [] },
  "filter-worker": { options: [] },
};
const defaultEl = { options: [], value: "", setAttribute() {}, removeAttribute() {}, addEventListener() {} };
const document = {
  getElementById: id => els[id] || defaultEl,
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener() {},
};
let replacedUrl = "";
const locationObj = { pathname: "/ui", search: "?task=" + prefix, hash: "" };
const historyObj = {
  replaceState(state, title, url) { replacedUrl = url; locationObj.search = url.slice(url.indexOf("?")); },
  pushState() {},
};
const fetchStub = async (url) => ({
  ok: true,
  status: 200,
  headers: { get: () => "application/json" },
  text: async () => JSON.stringify({ id: fullId, project: "p", priority: 1, body: "b" }),
  json: async () => (url && (url.includes("/projects") || url.includes("/workers") || url.includes("/tasks") ? [] : {})),
});

const api = new Function("document", "location", "history", "window", "fetch", "console",
  "setTimeout", "clearTimeout", "setInterval", "clearInterval", "Date", "AbortSignal",
  code + "\nreturn { applyURLState, getSelectedTaskId: () => selectedTaskId, getCurrentTask: () => currentTask };"
)(document, locationObj, historyObj, { addEventListener() {} }, fetchStub, console,
  () => 0, () => {}, () => 1, () => {}, Date, { timeout: () => ({}) });

(async () => {
  api.applyURLState();
  await new Promise(r => setTimeout(r, 15));
  process.stdout.write(JSON.stringify({
    selectedTaskId: api.getSelectedTaskId(),
    rowSelected: tr.getAttribute("aria-selected") === "true",
    urlUpdated: locationObj.search === "?task=" + fullId || replacedUrl.includes("task=" + fullId),
    detailsLoaded: Boolean(api.getCurrentTask() && api.getCurrentTask().id === fullId),
  }));
})();
`

	out, err := exec.Command(node, "-e", script, "web/index.html").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("node harness failed: %v\nstderr:\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("node harness failed: %v", err)
	}

	var got struct {
		SelectedTaskId string `json:"selectedTaskId"`
		RowSelected    bool   `json:"rowSelected"`
		URLUpdated     bool   `json:"urlUpdated"`
		DetailsLoaded  bool   `json:"detailsLoaded"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	wantID := "81b0a368767072bdcde9d92ff31814e6"
	if got.SelectedTaskId != wantID {
		t.Errorf("expected selectedTaskId = %q, got %q", wantID, got.SelectedTaskId)
	}
	if !got.RowSelected {
		t.Error("expected table row to have aria-selected=\"true\"")
	}
	if !got.URLUpdated {
		t.Error("expected URL to update to full task ID")
	}
	if !got.DetailsLoaded {
		t.Error("expected task details to be loaded")
	}
}
func TestWebUICloseBuriedTask(t *testing.T) {
	ui := string(uiHTML)
	re := regexp.MustCompile(`status\s*===\s*'buried'[\s\S]*?id="kick-task-btn"[\s\S]*?id="close-task-btn"`)
	if !re.MatchString(ui) {
		t.Fatal("expected #close-task-btn alongside #kick-task-btn for buried tasks in web/index.html")
	}
}
func TestWebSubmitCtrlEnter(t *testing.T) {
	ui := string(uiHTML)
	re := regexp.MustCompile(`const\s+bodyEl\s*=\s*document\.getElementById\('form-body'\);[\s\S]*?bodyEl\.addEventListener\('keydown',\s*\(?e\)?\s*=>\s*\{[\s\S]*?e\.key\s*===\s*'Enter'\s*&&\s*\(e\.ctrlKey\s*\|\|\s*e\.metaKey\)[\s\S]*?e\.preventDefault\(\)[\s\S]*?submitTask\(\)`)
	if !re.MatchString(ui) {
		t.Fatal("expected #form-body keydown listener for Ctrl+Enter or Cmd+Enter invoking submitTask in web/index.html")
	}
}
