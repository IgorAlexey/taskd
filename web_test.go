package main

import (
	"encoding/json"
	"errors"
	"fmt"
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

func TestWebUIFormLabels(t *testing.T) {
	ui := string(uiHTML)

	for _, id := range []string{
		"auto-refresh",
		"filter-project",
		"filter-status",
		"filter-priority",
		"filter-worker",
		"filter-search",
		"form-project",
		"form-priority",
		"form-body",
		"form-asset",
		"form-id",
	} {
		if !strings.Contains(ui, `id="`+id+`"`) {
			t.Fatalf("expected control with id=%q in web/index.html", id)
		}
		if !strings.Contains(ui, `for="`+id+`"`) {
			t.Fatalf("expected a label with for=%q in web/index.html", id)
		}
	}

	if strings.Contains(ui, "<label>") {
		t.Fatal("found orphan <label> without attributes in web/index.html")
	}

	labels := regexp.MustCompile(`<label[^>]*>`).FindAllString(ui, -1)
	if len(labels) == 0 {
		t.Fatal("expected labels in web/index.html")
	}
	seen := map[string]bool{}
	forAttr := regexp.MustCompile(`for="([^"]*)"`)
	for _, l := range labels {
		m := forAttr.FindStringSubmatch(l)
		if m == nil {
			t.Fatalf("label %q has no for attribute", l)
		}
		if seen[m[1]] {
			t.Fatalf("control %q is targeted by more than one label", m[1])
		}
		seen[m[1]] = true
	}

	req := regexp.MustCompile(`<input[^>]*id="form-project"[^>]*>`).FindString(ui)
	if req == "" {
		t.Fatal("expected #form-project input in web/index.html")
	}
	if !strings.Contains(req, `required`) || !strings.Contains(req, `aria-required="true"`) {
		t.Fatalf("expected required and aria-required=\"true\" on #form-project, got %q", req)
	}
}

func TestWebUISkipLinkAndLandmarks(t *testing.T) {
	ui := string(uiHTML)

	body := regexp.MustCompile(`<body[^>]*>`).FindStringIndex(ui)
	if body == nil {
		t.Fatal("no <body> in web/index.html")
	}
	first := regexp.MustCompile(`<[a-zA-Z][^>]*>`).FindString(ui[body[1]:])
	if !strings.Contains(first, `class="skip-link"`) {
		t.Fatalf("expected the skip link first in <body>, got %q", first)
	}
	if !strings.Contains(first, `href="#queue"`) {
		t.Fatalf("expected skip link to target #queue, got %q", first)
	}

	hidden := regexp.MustCompile(`\.skip-link\s*\{([^}]*)\}`).FindStringSubmatch(ui)
	if hidden == nil || !strings.Contains(hidden[1], "clip-path: inset(50%)") {
		t.Fatal("expected .skip-link to be clipped out of view until focused")
	}
	shown := regexp.MustCompile(`\.skip-link:focus\s*\{([^}]*)\}`).FindStringSubmatch(ui)
	if shown == nil || !strings.Contains(shown[1], "clip-path: none") {
		t.Fatal("expected .skip-link:focus to unclip the link")
	}

	target := regexp.MustCompile(`<[a-zA-Z]+[^>]*id="queue"[^>]*>`).FindStringIndex(ui)
	if target == nil {
		t.Fatal("expected an element with id=\"queue\" in web/index.html")
	}
	if !strings.Contains(ui[target[0]:target[1]], `tabindex="-1"`) {
		t.Fatalf("expected tabindex=\"-1\" on the skip link target, got %q",
			ui[target[0]:target[1]])
	}
	if i := strings.Index(ui[target[1]:], "<table>"); i < 0 || i > 40 {
		t.Fatal("expected the skip link to land past the filters, on the table")
	}
	if strings.Contains(ui[target[1]:], `class="filter-bar"`) {
		t.Fatal("expected the filter bar before the skip link target, not after")
	}

	main := regexp.MustCompile(`<main[^>]*>`).FindStringIndex(ui)
	if main == nil || main[0] > target[0] || main[0] < body[1] {
		t.Fatal("expected a <main> landmark in <body> wrapping the queue")
	}

	sections := regexp.MustCompile(`<section[^>]*>`).FindAllString(ui, -1)
	if len(sections) < 3 {
		t.Fatalf("expected the queue, details and submit regions, got %d", len(sections))
	}
	labelledBy := regexp.MustCompile(`aria-labelledby="([^"]+)"`)
	for _, sec := range sections {
		m := labelledBy.FindStringSubmatch(sec)
		if m == nil {
			t.Fatalf("section %q has no aria-labelledby", sec)
		}
		name := regexp.MustCompile(`<h2[^>]*id="` + m[1] + `"[^>]*>([^<]*)</h2>`).
			FindStringSubmatch(ui)
		if name == nil {
			t.Fatalf("no <h2> carries id %q", m[1])
		}
		if strings.TrimSpace(name[1]) == "" {
			t.Fatalf("heading %q is empty", m[1])
		}
	}
	if regexp.MustCompile(`(?s)<h2[^>]*>[^<]*<span`).MatchString(ui) {
		t.Fatal("expected no live counters nested inside a heading")
	}

	group := regexp.MustCompile(`<[a-zA-Z]+[^>]*role="group"[^>]*>`).FindString(ui)
	if group == "" || !strings.Contains(group, `aria-label="Task filters"`) {
		t.Fatalf("expected a labelled role=\"group\" filter region, got %q", group)
	}

	if regexp.MustCompile(`aria-label(?:ledby)?="\s*"`).MatchString(ui) {
		t.Fatal("found an empty accessible name in web/index.html")
	}
}

func TestWebUIURLState(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required to run the web UI harness")
		}
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/urlstate.js", "web/index.html").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("harness failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("harness failed: %v", err)
	}
	var got struct {
		Load struct {
			State       map[string]string
			URL         string `json:"url"`
			Pane        string
			List        string
			FormProject string `json:"formProject"`
		}
		Noise struct {
			Status  string
			Project string
			Options []string
			URL     string `json:"url"`
		}
		Filter, Select struct {
			Entry string
			URL   string `json:"url"`
			Pane  string
		}
		Reselect               string
		Back, OffPage, Missing struct {
			URL  string `json:"url"`
			Task string
			Pane string
		}
		ProjectsDown struct {
			Project string
			URL     string `json:"url"`
			List    string
		}
		ProjectFilterStats struct {
			URL     string `json:"url"`
			Pending int
			Leased  int
			Done    int
			Total   int
		}
		CardFilterStatus struct {
			Status string
			URL    string `json:"url"`
			List   string
		}
		CardFilterTotal struct {
			Status string
			URL    string `json:"url"`
			List   string
		}
		ProjectPrefill struct {
			FormProject string `json:"formProject"`
		}
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	for k, want := range map[string]string{"project": "p1", "status": "pending", "task": "t1"} {
		if got.Load.State[k] != want {
			t.Errorf("load %s = %q, want %q", k, got.Load.State[k], want)
		}
	}
	if got.Load.Pane != "details" {
		t.Errorf("load pane = %q, want the task from the URL", got.Load.Pane)
	}
	if got.Load.List != "/tasks?limit=200&project=p1&status=pending" {
		t.Errorf("first list request = %q, want both filters applied", got.Load.List)
	}

	if got.Noise.Status != "" || got.Noise.Project != "" {
		t.Errorf("unknown filters leaked: status=%q project=%q",
			got.Noise.Status, got.Noise.Project)
	}
	if slices.Contains(got.Noise.Options, "ghost") {
		t.Errorf("URL grew the project list: %v", got.Noise.Options)
	}
	if got.Noise.URL != "/ui?task=t1" {
		t.Errorf("noise url = %q, want the unknown filters dropped", got.Noise.URL)
	}

	if got.Filter.Entry != "replace" || got.Filter.URL != "/ui?status=done" {
		t.Errorf("filter change = %q %q, want a replaced /ui?status=done",
			got.Filter.Entry, got.Filter.URL)
	}
	if got.Select.Entry != "push" || got.Select.URL != "/ui?status=done&task=t2" {
		t.Errorf("select = %q %q, want a pushed /ui?status=done&task=t2",
			got.Select.Entry, got.Select.URL)
	}
	if got.Reselect != "replace" {
		t.Errorf("re-selecting the same task added a %q entry", got.Reselect)
	}
	if got.Back.URL != "/ui?status=done" || got.Back.Task != "" || got.Back.Pane != "idle" {
		t.Errorf("back = %+v, want the filtered list with no selection", got.Back)
	}

	if got.OffPage.URL != "/ui?status=pending&task=t2" || got.OffPage.Task != "t2" {
		t.Errorf("off-page refresh = %+v, want the selection kept", got.OffPage)
	}
	if got.OffPage.Pane != "details" {
		t.Errorf("off-page pane = %q, want the task the server still has", got.OffPage.Pane)
	}
	if got.Missing.URL != "/ui" || got.Missing.Pane != "idle" || got.Missing.Task != "" {
		t.Errorf("missing task = %+v, want the URL and pane cleared on 404",
			got.Missing)
	}

	if got.ProjectsDown.Project != "" || got.ProjectsDown.URL != "/ui?status=pending&task=t2" {
		t.Errorf("projects down = %+v, want the unconfirmed project gone",
			got.ProjectsDown)
	}
	if got.ProjectsDown.List != "/tasks?limit=200&status=pending" {
		t.Errorf("projects down list = %q, want no project filter",
			got.ProjectsDown.List)
	}

	if got.ProjectFilterStats.URL != "/stats?project=p1" {
		t.Errorf("project filter stats request = %q, want /stats?project=p1", got.ProjectFilterStats.URL)
	}
	if got.ProjectFilterStats.Pending != 4 || got.ProjectFilterStats.Total != 7 {
		t.Errorf("project filter stats cards = %+v, want pending: 4, total: 7", got.ProjectFilterStats)
	}
	if got.CardFilterStatus.Status != "pending" || got.CardFilterStatus.URL != "/ui?project=p1&status=pending" {
		t.Errorf("status card filter = %+v, want pending and /ui?project=p1&status=pending", got.CardFilterStatus)
	}
	if got.CardFilterStatus.List != "/tasks?limit=200&project=p1&status=pending" {
		t.Errorf("status card filter fetch = %q, want project=p1&status=pending query", got.CardFilterStatus.List)
	}
	if got.CardFilterTotal.Status != "" || got.CardFilterTotal.URL != "/ui?project=p1" {
		t.Errorf("total card filter = %+v, want empty status and /ui?project=p1", got.CardFilterTotal)
	}
	if got.Load.FormProject != "p1" {
		t.Errorf("boot form project = %q, want p1", got.Load.FormProject)
	}
	if got.ProjectPrefill.FormProject != "p1" {
		t.Errorf("filter change form project = %q, want p1", got.ProjectPrefill.FormProject)
	}
}
func TestWebUITaskDetailsGuard(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required to run the web UI harness")
		}
		t.Skip("node not installed")
	}

	cmd := exec.Command(node, "testdata/details.js", "web/index.html")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("task details guard harness failed: %v\n%s", err, out)
	}
}

func TestWebUITaskActions(t *testing.T) {
	ui := string(uiHTML)

	for _, check := range []struct {
		id   string
		text string
	}{
		{"copy-id-btn", "Copy"},
		{"delete-task-btn", "Delete Task"},
		{"release-task-btn", "Release Task"},
		{"complete-task-btn", "Complete Task"},
	} {
		if !strings.Contains(ui, `id="`+check.id+`"`) {
			t.Fatalf("expected button with id=%q in web/index.html", check.id)
		}
		if !strings.Contains(ui, check.text) {
			t.Fatalf("expected button text %q in web/index.html", check.text)
		}
	}

	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required to run the web UI harness")
		}
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/actions.js", "web/index.html").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("harness failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("harness failed: %v", err)
	}
	var got struct {
		PendingHasDelete        bool
		PendingHasComplete      bool
		PendingHasRelease       bool
		PendingHasCopy          bool
		CopySuccess             bool
		CopyFailure             bool
		CancelDeleteAsked       bool
		CancelDeleteCalls       int
		ConfirmDeletePending    *struct{ URL, Method string }
		PendingDeletedPaneReset bool
		LeasedHasDelete         bool
		LeasedHasComplete       bool
		LeasedHasRelease        bool
		LeasedDeleteErrorBanner bool
		LeasedDeletePaneKept    bool
		ErrorBannerSurvivesPoll bool
		ReleaseCall             *struct {
			URL    string
			Method string
			Body   map[string]any
		}
		ReleasePaneReset bool
		CompleteCall     *struct {
			URL    string
			Method string
			Body   map[string]any
		}
		CompletePaneReset    bool
		DoneDeleteCall       *struct{ URL, Method string }
		DoneDeletedPaneReset bool
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if !got.PendingHasDelete || got.PendingHasComplete || got.PendingHasRelease {
		t.Errorf("pending buttons mismatch: %+v", got)
	}
	if !got.CancelDeleteAsked || got.CancelDeleteCalls != 0 {
		t.Errorf("cancel delete failed: asked=%v calls=%d", got.CancelDeleteAsked, got.CancelDeleteCalls)
	}
	if !got.PendingHasCopy {
		t.Error("expected copy-id-btn in task details HTML")
	}
	if !got.CopySuccess {
		t.Error("expected copy-id-btn click to copy task ID and display Copied!")
	}
	if !got.CopyFailure {
		t.Error("expected copy failure to display error banner and not show Copied!")
	}
	if got.ConfirmDeletePending == nil || got.ConfirmDeletePending.URL != "/tasks/t-pending" || got.ConfirmDeletePending.Method != "DELETE" {
		t.Errorf("confirm delete pending call = %+v", got.ConfirmDeletePending)
	}
	if !got.PendingDeletedPaneReset {
		t.Errorf("pane not reset after pending delete")
	}

	if !got.LeasedHasDelete || !got.LeasedHasComplete || !got.LeasedHasRelease {
		t.Errorf("leased buttons mismatch: %+v", got)
	}
	if !got.LeasedDeleteErrorBanner || !got.LeasedDeletePaneKept {
		t.Errorf("leased delete 409 mismatch: banner=%v paneKept=%v", got.LeasedDeleteErrorBanner, got.LeasedDeletePaneKept)
	}
	if !got.ErrorBannerSurvivesPoll {
		t.Error("expected error banner to survive subsequent successful fetch")
	}
	if !regexp.MustCompile(`<div[^>]*id="error-banner"[^>]*role="alert"`).MatchString(ui) {
		t.Fatalf("expected role=alert on #error-banner in web/index.html")
	}
	if !strings.Contains(ui, `class="error-banner-dismiss"`) {
		t.Fatalf("expected dismiss button on #error-banner in web/index.html")
	}

	if got.ReleaseCall == nil || got.ReleaseCall.URL != "/tasks/t-leased/release" || got.ReleaseCall.Method != "POST" || got.ReleaseCall.Body["worker"] != "w-1" {
		t.Errorf("release call mismatch: %+v", got.ReleaseCall)
	}
	if !got.ReleasePaneReset {
		t.Errorf("pane not reset after release")
	}

	if got.CompleteCall == nil || got.CompleteCall.URL != "/tasks/t-leased/done" || got.CompleteCall.Method != "POST" || got.CompleteCall.Body["worker"] != "w-1" {
		t.Errorf("complete call mismatch: %+v", got.CompleteCall)
	}
	if !got.CompletePaneReset {
		t.Errorf("pane not reset after complete")
	}

	if got.DoneDeleteCall == nil || got.DoneDeleteCall.URL != "/tasks/t-done?force=1" || got.DoneDeleteCall.Method != "DELETE" {
		t.Errorf("done delete call mismatch: %+v", got.DoneDeleteCall)
	}
	if !got.DoneDeletedPaneReset {
		t.Errorf("pane not reset after done delete")
	}
}
func TestWebUIPagination(t *testing.T) {
	ui := string(uiHTML)

	if !strings.Contains(ui, `id="prev-page-btn"`) {
		t.Fatal("expected id=\"prev-page-btn\" in web/index.html")
	}
	if !strings.Contains(ui, `id="next-page-btn"`) {
		t.Fatal("expected id=\"next-page-btn\" in web/index.html")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required to run the web UI harness")
		}
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/pagination.js", "web/index.html").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("harness failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("harness failed: %v", err)
	}

	type pageState struct {
		CountText    string `json:"countText"`
		PrevDisabled bool   `json:"prevDisabled"`
		NextDisabled bool   `json:"nextDisabled"`
		RowCount     int    `json:"rowCount"`
		FirstTaskId  string `json:"firstTaskId"`
		FetchURL     string `json:"fetchURL"`
		URL          string `json:"url"`
	}
	var got struct {
		Initial     pageState `json:"initial"`
		Page2       pageState `json:"page2"`
		BackToPage1 pageState `json:"backToPage1"`
		DirectPage2 pageState `json:"directPage2"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if got.Initial.CountText != "Showing 1-200 of 250" {
		t.Errorf("initial count = %q, want %q", got.Initial.CountText, "Showing 1-200 of 250")
	}
	if !got.Initial.PrevDisabled {
		t.Errorf("initial prev button should be disabled")
	}
	if got.Initial.NextDisabled {
		t.Errorf("initial next button should be enabled")
	}
	if got.Initial.RowCount != 200 {
		t.Errorf("initial row count = %d, want 200", got.Initial.RowCount)
	}
	if got.Initial.FirstTaskId != "task-000" {
		t.Errorf("initial first task = %q, want task-000", got.Initial.FirstTaskId)
	}
	if got.Initial.URL != "/ui" {
		t.Errorf("initial URL = %q, want /ui", got.Initial.URL)
	}

	if got.Page2.CountText != "Showing 201-250 of 250" {
		t.Errorf("page2 count = %q, want %q", got.Page2.CountText, "Showing 201-250 of 250")
	}
	if got.Page2.PrevDisabled {
		t.Errorf("page2 prev button should be enabled")
	}
	if !got.Page2.NextDisabled {
		t.Errorf("page2 next button should be disabled")
	}
	if got.Page2.RowCount != 50 {
		t.Errorf("page2 row count = %d, want 50", got.Page2.RowCount)
	}
	if got.Page2.FirstTaskId != "task-200" {
		t.Errorf("page2 first task = %q, want task-200", got.Page2.FirstTaskId)
	}
	if !strings.Contains(got.Page2.FetchURL, "offset=200") {
		t.Errorf("page2 fetch URL = %q, want offset=200", got.Page2.FetchURL)
	}

	if got.Page2.URL != "/ui?page=2" {
		t.Errorf("page2 URL = %q, want /ui?page=2", got.Page2.URL)
	}
	if got.BackToPage1.CountText != "Showing 1-200 of 250" {
		t.Errorf("backToPage1 count = %q, want %q", got.BackToPage1.CountText, "Showing 1-200 of 250")
	}
	if !got.BackToPage1.PrevDisabled {
		t.Errorf("backToPage1 prev button should be disabled")
	}
	if got.BackToPage1.NextDisabled {
		t.Errorf("backToPage1 next button should be enabled")
	}
	if got.BackToPage1.RowCount != 200 {
		t.Errorf("backToPage1 row count = %d, want 200", got.BackToPage1.RowCount)
	}
	if got.BackToPage1.FirstTaskId != "task-000" {
		t.Errorf("backToPage1 first task = %q, want task-000", got.BackToPage1.FirstTaskId)
	}
	if got.BackToPage1.URL != "/ui" {
		t.Errorf("backToPage1 URL = %q, want /ui", got.BackToPage1.URL)
	}
	if got.DirectPage2.URL != "/ui?page=2" || got.DirectPage2.CountText != "Showing 201-250 of 250" || got.DirectPage2.FirstTaskId != "task-200" {
		t.Errorf("direct load with ?page=2 failed: %+v", got.DirectPage2)
	}
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin failed: %v", err)
	}
	for i := 0; i < 250; i++ {
		id := fmt.Sprintf("task-%03d", i)
		if _, err := tx.Exec("INSERT INTO tasks (id, project, status, priority, body) VALUES (?, 'p1', 'pending', 1, 'b')", id); err != nil {
			t.Fatalf("insert task %d failed: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit failed: %v", err)
	}

	resp, err := http.Get(srv.URL + "/tasks?limit=200")
	if err != nil {
		t.Fatalf("GET /tasks?limit=200 failed: %v", err)
	}
	defer resp.Body.Close()
	if totalHdr := resp.Header.Get("X-Total-Count"); totalHdr != "250" {
		t.Fatalf("expected X-Total-Count: 250, got %q", totalHdr)
	}
	var page1 []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page1); err != nil {
		t.Fatalf("decode page 1 failed: %v", err)
	}
	if len(page1) != 200 {
		t.Fatalf("expected 200 tasks on page 1, got %d", len(page1))
	}

	resp2, err := http.Get(srv.URL + "/tasks?limit=50&offset=200")
	if err != nil {
		t.Fatalf("GET /tasks?limit=50&offset=200 failed: %v", err)
	}
	defer resp2.Body.Close()
	var page2 []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&page2); err != nil {
		t.Fatalf("decode page 2 failed: %v", err)
	}
	if len(page2) != 50 {
		t.Fatalf("expected 50 tasks on page 2, got %d", len(page2))
	}
	if page2[0].ID != "task-200" {
		t.Fatalf("expected first task of tail page to be task-200, got %q", page2[0].ID)
	}
}
