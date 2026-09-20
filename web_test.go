package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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

var queueTableFields = []string{
	"asset_path", "claim_count", "id", "priority", "project", "status", "summary", "worker",
}

func listRequestParams(t *testing.T, raw string) map[string]string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("list request %q is not a URL: %v", raw, err)
	}
	if u.Path != "/tasks" {
		t.Fatalf("list request %q does not hit /tasks", raw)
	}
	q := u.Query()
	fields := strings.Split(q.Get("fields"), ",")
	slices.Sort(fields)
	if !slices.Equal(fields, queueTableFields) {
		t.Errorf("list request %q asks for fields %v, want the fields the queue table feeds on, summary plus its asset_path fallback %v",
			raw, fields, queueTableFields)
	}
	q.Del("fields")
	params := make(map[string]string, len(q))
	for k, v := range q {
		params[k] = strings.Join(v, ",")
	}
	return params
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
		Search struct {
			Entry  string
			URL    string `json:"url"`
			Search string
			List   string
		}
		SearchSelect struct {
			Entry  string
			URL    string `json:"url"`
			Search string
		}
		SearchBack struct {
			URL    string `json:"url"`
			Task   string
			Search string
		}
		SearchClear struct {
			URL    string `json:"url"`
			Search string
		}
		SearchRestore struct {
			ParsedQ    string `json:"parsedQ"`
			InputValue string `json:"inputValue"`
			URL        string `json:"url"`
			List       string
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
	wantLoadList := map[string]string{"limit": "200", "project": "p1", "status": "pending"}
	if params := listRequestParams(t, got.Load.List); !maps.Equal(params, wantLoadList) {
		t.Errorf("first list request = %q (params %v), want both filters applied %v",
			got.Load.List, params, wantLoadList)
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
	wantDownList := map[string]string{"limit": "200", "status": "pending"}
	if params := listRequestParams(t, got.ProjectsDown.List); !maps.Equal(params, wantDownList) {
		t.Errorf("projects down list = %q (params %v), want no project filter",
			got.ProjectsDown.List, params)
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
	wantCardList := map[string]string{"limit": "200", "project": "p1", "status": "pending"}
	if params := listRequestParams(t, got.CardFilterStatus.List); !maps.Equal(params, wantCardList) {
		t.Errorf("status card filter fetch = %q (params %v), want %v",
			got.CardFilterStatus.List, params, wantCardList)
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
	if got.Search.Entry != "replace" || got.Search.URL != "/ui?q=needle" || got.Search.Search != "?q=needle" {
		t.Errorf("search input = %+v, want replace and /ui?q=needle", got.Search)
	}
	if !strings.Contains(got.Search.List, "&q=needle") {
		t.Errorf("search fetch list = %q, want &q=needle", got.Search.List)
	}
	if got.SearchSelect.Entry != "push" || got.SearchSelect.URL != "/ui?q=needle&task=t2" {
		t.Errorf("search select = %+v, want push and /ui?q=needle&task=t2", got.SearchSelect)
	}
	if got.SearchBack.URL != "/ui?q=needle" || got.SearchBack.Task != "" || got.SearchBack.Search != "needle" {
		t.Errorf("search back = %+v, want /ui?q=needle with search restored", got.SearchBack)
	}
	if got.SearchClear.URL != "/ui" || got.SearchClear.Search != "" {
		t.Errorf("search clear = %+v, want /ui and empty search input", got.SearchClear)
	}
	if got.SearchRestore.ParsedQ != "prefilled" || got.SearchRestore.InputValue != "prefilled" || got.SearchRestore.URL != "/ui?q=prefilled" {
		t.Errorf("search restore = %+v, want parsed and input set to prefilled", got.SearchRestore)
	}
	if !strings.Contains(got.SearchRestore.List, "&q=prefilled") {
		t.Errorf("search restore fetch list = %q, want &q=prefilled", got.SearchRestore.List)
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
		{"copy-body-btn", "Copy"},
		{"edit-task-btn", "Edit Task"},
		{"delete-task-btn", "Delete Task"},
		{"touch-task-btn", "Touch Lease"},
		{"release-task-btn", "Release Task"},
		{"complete-task-btn", "Complete Task"},
		{"claim-task-btn", "Claim Task"},
		{"close-task-btn", "Close Task"},
		{"kick-task-btn", "Kick Task"},
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
		PendingHasClaim         bool
		PendingHasClose         bool
		PendingHasTouch         bool
		PendingHasKick          bool
		PendingHasCopy          bool
		PendingHasEdit          bool
		PendingEditDisabled     bool
		CopySuccess             bool
		CopyFailure             bool
		PendingHasCopyBody      bool
		CopyBodySuccess         bool
		CopyBodyFailure         bool
		CancelDeleteAsked       bool
		CancelDeleteCalls       int
		ConfirmDeletePending    *struct{ URL, Method string }
		PendingDeletedPaneReset bool
		LeasedHasDelete         bool
		LeasedDeleteDisabled    bool
		LeasedDeleteTitle       bool
		LeasedDeleteAsked       bool
		LeasedDeleteCalls       int
		LeasedHasComplete       bool
		LeasedHasTouch          bool
		LeasedHasEdit           bool
		LeasedEditDisabled      bool
		LeasedEditTitle         bool
		LeasedEditIgnored       bool
		LeasedHasRelease        bool
		LeasedHasClaim          bool
		LeasedHasClose          bool
		LeasedHasKick           bool
		BuriedHasKick           bool
		BuriedHasDelete         bool
		BuriedDeleteDisabled    bool
		BuriedHasComplete       bool
		BuriedHasTouch          bool
		BuriedHasRelease        bool
		BuriedHasClaim          bool
		BuriedHasClose          bool
		KickCall                *struct{ URL, Method string }
		KickSelected            bool
		KickURLPreserved        bool
		KickPaneHasBadge        bool
		KickPaneNotReset        bool
		KickRowSelected         bool
		LeasedDeletePaneKept    bool
		ErrorBannerSurvivesPoll bool
		TouchCall               *struct {
			URL    string
			Method string
			Body   map[string]any
		}
		TouchSelected       bool
		TouchPaneHasExpires bool
		TouchUpdatedExpires bool
		TouchPaneNotReset   bool
		ReleaseCall         *struct {
			URL    string
			Method string
			Body   map[string]any
		}
		ReleaseSelected     bool
		ReleaseURLPreserved bool
		ReleasePaneHasBadge bool
		ReleasePaneNotReset bool
		ReleaseRowSelected  bool
		CompleteCall        *struct {
			URL    string
			Method string
			Body   map[string]any
		}
		CompleteSelected     bool
		CompleteURLPreserved bool
		CompletePaneHasBadge bool
		CompletePaneNotReset bool
		CompleteRowSelected  bool
		ClaimCall            *struct {
			URL    string
			Method string
			Body   map[string]any
		}
		ClaimPaneKept         bool
		ClaimRefreshedList    bool
		ClaimWorkerRemembered bool
		ClaimConflictBanner   bool
		ClaimConflictPaneKept bool
		CancelClaimAsked      bool
		CancelClaimCalls      int
		CloseCall             *struct {
			URL    string
			Method string
			Body   map[string]any
		}
		ClosePaneKept                        bool
		CancelCloseAsked                     bool
		CancelCloseCalls                     int
		DoneDeleteCall                       *struct{ URL, Method string }
		DoneDeletedPaneReset                 bool
		CompleteExpiredBanner                bool
		CompleteExpiredBannerSurvivesPoll    bool
		ReleaseWrongWorkerBanner             bool
		ReleaseWrongWorkerBannerSurvivesPoll bool
		DeleteLeasedBanner                   bool
		DeleteLeasedBannerSurvivesPoll       bool
		SubmitDuplicateSummary               bool
		SubmitDuplicateFieldError            bool
		SubmitDuplicateAriaInvalid           bool
		SubmitDuplicateNoBanner              bool
		ErrorDismissedOnSelect               bool
		ListFailureBanner                    bool
		StatsFailureBanner                   bool
		ProjectsFailureBanner                bool
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if !got.PendingHasDelete || got.PendingHasComplete || got.PendingHasRelease {
		t.Errorf("pending buttons mismatch: %+v", got)
	}
	if !got.PendingHasClaim || !got.PendingHasClose {
		t.Errorf("pending claim/close buttons missing: %+v", got)
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
	if !got.PendingHasCopyBody {
		t.Error("expected copy-body-btn in task details HTML")
	}
	if !got.CopyBodySuccess {
		t.Error("expected copy-body-btn click to copy task body and display Copied!")
	}
	if !got.CopyBodyFailure {
		t.Error("expected copy body failure to display error banner and not show Copied!")
	}
	if got.ConfirmDeletePending == nil || got.ConfirmDeletePending.URL != "/tasks/t-pending" || got.ConfirmDeletePending.Method != "DELETE" {
		t.Errorf("confirm delete pending call = %+v", got.ConfirmDeletePending)
	}
	if !got.PendingDeletedPaneReset {
		t.Errorf("pane not reset after pending delete")
	}
	if !got.PendingHasEdit || got.PendingEditDisabled {
		t.Errorf("pending edit button mismatch: hasEdit=%v disabled=%v", got.PendingHasEdit, got.PendingEditDisabled)
	}
	if !got.LeasedHasEdit || !got.LeasedEditDisabled || !got.LeasedEditTitle || !got.LeasedEditIgnored {
		t.Errorf("leased edit button mismatch: hasEdit=%v disabled=%v title=%v ignored=%v",
			got.LeasedHasEdit, got.LeasedEditDisabled, got.LeasedEditTitle, got.LeasedEditIgnored)
	}

	if !got.LeasedHasDelete || !got.LeasedDeleteDisabled || !got.LeasedDeleteTitle {
		t.Errorf("leased delete button mismatch: %+v", got)
	}
	if got.LeasedHasClaim || got.LeasedHasClose || got.LeasedHasKick {
		t.Errorf("leased must not offer claim, close, or kick: %+v", got)
	}
	if got.LeasedDeleteAsked || got.LeasedDeleteCalls != 0 || !got.LeasedDeletePaneKept {
		t.Errorf("leased delete should not trigger confirm or call DELETE: asked=%v calls=%d paneKept=%v", got.LeasedDeleteAsked, got.LeasedDeleteCalls, got.LeasedDeletePaneKept)
	}
	if !got.LeasedHasComplete || !got.LeasedHasRelease || !got.LeasedHasTouch {
		t.Errorf("leased buttons mismatch: %+v", got)
	}
	if got.PendingHasTouch || got.PendingHasKick {
		t.Errorf("pending tasks should not offer touch lease or kick: %+v", got)
	}
	if got.TouchCall == nil || got.TouchCall.URL != "/tasks/t-leased/touch" || got.TouchCall.Method != "POST" || got.TouchCall.Body["worker"] != "w-1" {
		t.Errorf("touch call mismatch: %+v", got.TouchCall)
	}
	if !got.TouchSelected {
		t.Errorf("expected task to remain selected after touch")
	}
	if !got.TouchPaneHasExpires {
		t.Errorf("expected details pane to display lease expires after touch")
	}
	if !got.TouchUpdatedExpires {
		t.Errorf("expected details pane to update displayed lease expires after touch")
	}
	if !got.TouchPaneNotReset {
		t.Errorf("details pane was reset after touch")
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
	if !got.ReleaseSelected {
		t.Errorf("expected task to remain selected after release")
	}
	if !got.ReleaseURLPreserved {
		t.Errorf("expected URL to retain selected task after release")
	}
	if !got.ReleasePaneHasBadge {
		t.Errorf("expected details pane to show pending badge after release")
	}
	if !got.ReleasePaneNotReset {
		t.Errorf("details pane was reset after release")
	}
	if !got.ReleaseRowSelected {
		t.Errorf("expected table row to retain aria-selected after release")
	}

	if got.CompleteCall == nil || got.CompleteCall.URL != "/tasks/t-leased/done" || got.CompleteCall.Method != "POST" || got.CompleteCall.Body["worker"] != "w-1" {
		t.Errorf("complete call mismatch: %+v", got.CompleteCall)
	}
	if !got.CompleteSelected {
		t.Errorf("expected task to remain selected after complete")
	}
	if !got.CompleteURLPreserved {
		t.Errorf("expected URL to retain selected task after complete")
	}
	if !got.CompletePaneHasBadge {
		t.Errorf("expected details pane to show done badge after complete")
	}
	if !got.CompletePaneNotReset {
		t.Errorf("details pane was reset after complete")
	}
	if !got.CompleteRowSelected {
		t.Errorf("expected table row to retain aria-selected after complete")
	}

	if !got.CancelClaimAsked || got.CancelClaimCalls != 0 {
		t.Errorf("cancel claim failed: asked=%v calls=%d", got.CancelClaimAsked, got.CancelClaimCalls)
	}
	if !got.ClaimConflictBanner || !got.ClaimConflictPaneKept {
		t.Errorf("claim 409 mismatch: banner=%v paneKept=%v", got.ClaimConflictBanner, got.ClaimConflictPaneKept)
	}
	if got.ClaimCall == nil || got.ClaimCall.URL != "/tasks/t-pending/claim" || got.ClaimCall.Method != "POST" || got.ClaimCall.Body["worker"] != "op-1" {
		t.Errorf("claim call mismatch: %+v", got.ClaimCall)
	}
	if !got.ClaimPaneKept {
		t.Errorf("claim must leave the leased task on screen: %+v", got)
	}
	if !got.ClaimRefreshedList {
		t.Errorf("claim must refresh the list and the stats")
	}
	if !got.ClaimWorkerRemembered {
		t.Errorf("claim must offer the last worker name as the default")
	}

	if !got.CancelCloseAsked || got.CancelCloseCalls != 0 {
		t.Errorf("cancel close failed: asked=%v calls=%d", got.CancelCloseAsked, got.CancelCloseCalls)
	}
	if got.CloseCall == nil || got.CloseCall.URL != "/tasks/t-pending/close" || got.CloseCall.Method != "POST" || got.CloseCall.Body != nil {
		t.Errorf("close call mismatch: %+v", got.CloseCall)
	}
	if !got.ClosePaneKept {
		t.Errorf("close must keep the task on screen with done status")
	}

	if got.DoneDeleteCall == nil || got.DoneDeleteCall.URL != "/tasks/t-done?force=1" || got.DoneDeleteCall.Method != "DELETE" {
		t.Errorf("done delete call mismatch: %+v", got.DoneDeleteCall)
	}
	if !got.DoneDeletedPaneReset {
		t.Errorf("pane not reset after done delete")
	}

	if !got.BuriedHasKick || !got.BuriedHasDelete || got.BuriedDeleteDisabled {
		t.Errorf("buried buttons mismatch: %+v", got)
	}
	if got.BuriedHasComplete || got.BuriedHasTouch || got.BuriedHasRelease || got.BuriedHasClaim || got.BuriedHasClose {
		t.Errorf("buried task has invalid buttons: %+v", got)
	}
	if got.KickCall == nil || got.KickCall.URL != "/tasks/t-buried/kick" || got.KickCall.Method != "POST" {
		t.Errorf("kick call mismatch: %+v", got.KickCall)
	}
	if !got.KickSelected || !got.KickURLPreserved || !got.KickPaneHasBadge || !got.KickPaneNotReset || !got.KickRowSelected {
		t.Errorf("kick transition mismatch: %+v", got)
	}

	if !got.CompleteExpiredBanner || !got.CompleteExpiredBannerSurvivesPoll {
		t.Errorf("complete expired lease error banner mismatch: banner=%v survives=%v", got.CompleteExpiredBanner, got.CompleteExpiredBannerSurvivesPoll)
	}
	if !got.ReleaseWrongWorkerBanner || !got.ReleaseWrongWorkerBannerSurvivesPoll {
		t.Errorf("release wrong worker error banner mismatch: banner=%v survives=%v", got.ReleaseWrongWorkerBanner, got.ReleaseWrongWorkerBannerSurvivesPoll)
	}
	if !got.DeleteLeasedBanner || !got.DeleteLeasedBannerSurvivesPoll {
		t.Errorf("delete leased task error banner mismatch: banner=%v survives=%v", got.DeleteLeasedBanner, got.DeleteLeasedBannerSurvivesPoll)
	}
	if !got.SubmitDuplicateSummary || !got.SubmitDuplicateFieldError || !got.SubmitDuplicateAriaInvalid || !got.SubmitDuplicateNoBanner {
		t.Errorf("submit duplicate custom id mismatch: summary=%v fieldError=%v ariaInvalid=%v noBanner=%v", got.SubmitDuplicateSummary, got.SubmitDuplicateFieldError, got.SubmitDuplicateAriaInvalid, got.SubmitDuplicateNoBanner)
	}
	if !got.ErrorDismissedOnSelect {
		t.Errorf("expected error banner to dismiss on task selection")
	}
	if !got.ListFailureBanner || !got.StatsFailureBanner || !got.ProjectsFailureBanner {
		t.Errorf("loader failures must reach the banner: list=%v stats=%v projects=%v", got.ListFailureBanner, got.StatsFailureBanner, got.ProjectsFailureBanner)
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

	tx, err := db.rw.Begin()
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

func TestWebUIRowSummaryFromServerField(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required to run the web UI harness")
		}
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/summary.js", "web/index.html").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("harness failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("harness failed: %v", err)
	}

	var got struct {
		Summary map[string]string `json:"summary"`
		RowHTML map[string]string `json:"rowHTML"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode harness output failed: %v\n%s", err, out)
	}

	want := map[string]string{
		"serverSummary":                 "Fix the parser",
		"serverTruncated":               strings.Repeat("A", 50) + "\u2026",
		"emptySummaryWithAsset":         "models/car.glb",
		"missingSummaryWithAsset":       "models/car.glb",
		"noSummaryNoAsset":              "",
		"bodyNeverUsed":                 "",
		"bodyIgnoredWhenSummaryPresent": "server summary",
		"markup":                        "<img src=x onerror=alert(1)> & co",
	}
	if len(got.Summary) != len(want) {
		t.Errorf("harness reported %d cases, want %d", len(got.Summary), len(want))
	}
	for name, exp := range want {
		if got.Summary[name] != exp {
			t.Errorf("summary[%s] = %q, want %q", name, got.Summary[name], exp)
		}
	}
	cellRe := regexp.MustCompile(`(?s)<td data-field="summary">(.*?)</td>`)
	for name, html := range got.RowHTML {
		cell := cellRe.FindStringSubmatch(html)
		if cell == nil {
			t.Errorf("no summary cell in row html for %s", name)
			continue
		}
		if strings.ContainsAny(cell[1], "\r\n") {
			t.Errorf("summary cell for %s holds a line break: %q", name, cell[1])
		}
	}
	markup := cellRe.FindStringSubmatch(got.RowHTML["markup"])
	if markup == nil || markup[1] != "&lt;img src=x onerror=alert(1)&gt; &amp; co" {
		t.Errorf("markup summary cell was not escaped: %q", got.RowHTML["markup"])
	}
}

func TestWebUISubmitFieldValidation(t *testing.T) {
	ui := string(uiHTML)

	if !strings.Contains(ui, `id="form-id-error"`) {
		t.Fatal("expected a field error span with id=\"form-id-error\" in web/index.html")
	}
	idInput := regexp.MustCompile(`<input[^>]*id="form-id"[^>]*>`).FindString(ui)
	if !strings.Contains(idInput, `aria-describedby="form-id-error"`) || !strings.Contains(idInput, `aria-invalid=`) {
		t.Fatalf("expected aria-invalid and aria-describedby on #form-id, got %q", idInput)
	}

	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required to run the web UI harness")
		}
		t.Skip("node not installed")
	}

	corpus := []string{
		"", "p1", "PROJ", "proj.1_x-Y", "-", "_", ".", "..", "...",
		"claim", "CLAIM", "Claim", "purge", "Purge",
		"a b", "a/b", "a\\b", "a@b", "a\tb", "a\nb", "a\x00b", "*",
		"\u00e9", "\u00e9\u00e9", "caf\u00e9",
		strings.Repeat("\u00e9", 33), strings.Repeat("a", 64), strings.Repeat("a", 65),
		strings.Repeat("a", 128), strings.Repeat("a", 129),
	}
	bodyCorpus := [][2]string{
		{"", ""}, {"a body", ""}, {"", "/tmp/asset"}, {"a body", "/tmp/asset"},
		{" ", ""}, {" ", "/tmp/asset"}, {"\n\t ", "/tmp/asset"},
		{"", "   "}, {" leading", ""}, {"x", "   "},
	}
	dir := t.TempDir()
	write := func(name string, v any) string {
		blob, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal %s failed: %v", name, err)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, blob, 0o600); err != nil {
			t.Fatalf("write %s failed: %v", name, err)
		}
		return path
	}
	corpusFile := write("corpus.json", corpus)
	bodyFile := write("bodies.json", bodyCorpus)

	out, err := exec.Command(node, "testdata/submit.js", "web/index.html", corpusFile, bodyFile).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("harness failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("harness failed: %v", err)
	}

	type attempt struct {
		Posted         int    `json:"posted"`
		ProjectError   string `json:"projectError"`
		BodyError      string `json:"bodyError"`
		IDError        string `json:"idError"`
		ProjectInvalid string `json:"projectInvalid"`
		IDInvalid      string `json:"idInvalid"`
		AssetInvalid   string `json:"assetInvalid"`
	}
	var got struct {
		Cases    map[string]attempt `json:"cases"`
		Verdicts []struct {
			Project bool `json:"project"`
			ID      bool `json:"id"`
		} `json:"verdicts"`
		BodyVerdicts []bool `json:"bodyVerdicts"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if len(got.Verdicts) != len(corpus) {
		t.Fatalf("harness returned %d verdicts for %d corpus entries", len(got.Verdicts), len(corpus))
	}
	for i, s := range corpus {
		if got.Verdicts[i].Project != validProject(s) {
			t.Errorf("projectError(%q) accepts=%v, validProject says %v", s, got.Verdicts[i].Project, validProject(s))
		}
		if got.Verdicts[i].ID != validTaskID(s) {
			t.Errorf("customIdError(%q) accepts=%v, validTaskID says %v", s, got.Verdicts[i].ID, validTaskID(s))
		}
	}

	if len(got.BodyVerdicts) != len(bodyCorpus) {
		t.Fatalf("harness returned %d body verdicts for %d pairs", len(got.BodyVerdicts), len(bodyCorpus))
	}
	db, err := openDB(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()
	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()
	for i, pair := range bodyCorpus {
		code, _ := post(t, srv.URL+"/tasks", map[string]string{
			"project":    "p1",
			"body":       pair[0],
			"asset_path": pair[1],
		})
		if got.BodyVerdicts[i] != (code == http.StatusCreated) {
			t.Errorf("bodyError(%q, %q) accepts=%v, POST /tasks answered %d", pair[0], pair[1], got.BodyVerdicts[i], code)
		}
	}

	pick := func(name string) attempt {
		a, ok := got.Cases[name]
		if !ok {
			t.Fatalf("harness produced no %s case", name)
		}
		return a
	}

	for _, name := range []string{"spacedProject", "slashProject", "longProject", "emptyProject"} {
		a := pick(name)
		if a.Posted != 0 {
			t.Errorf("%s: expected no POST /tasks, got %d", name, a.Posted)
		}
		if a.ProjectError == "" {
			t.Errorf("%s: expected an inline project error", name)
		}
		if a.ProjectInvalid != "true" {
			t.Errorf("%s: expected aria-invalid=true on #form-project, got %q", name, a.ProjectInvalid)
		}
	}

	for _, name := range []string{"spacedId", "reservedId", "dotId", "longId"} {
		a := pick(name)
		if a.Posted != 0 {
			t.Errorf("%s: expected no POST /tasks, got %d", name, a.Posted)
		}
		if a.IDError == "" {
			t.Errorf("%s: expected an inline custom ID error", name)
		}
		if a.IDInvalid != "true" {
			t.Errorf("%s: expected aria-invalid=true on #form-id, got %q", name, a.IDInvalid)
		}
		if a.ProjectError != "" {
			t.Errorf("%s: expected no project error, got %q", name, a.ProjectError)
		}
	}

	if a := pick("blankBody"); a.Posted != 0 || a.BodyError == "" || a.AssetInvalid != "false" {
		t.Errorf("blankBody: expected a body-only error and no POST, got %+v", a)
	}
	if a := pick("noBodyNoAsset"); a.Posted != 0 || a.BodyError == "" || a.AssetInvalid != "true" {
		t.Errorf("noBodyNoAsset: expected body and asset flagged and no POST, got %+v", a)
	}
	if a := pick("bothBad"); a.ProjectError == "" || a.IDError == "" || a.Posted != 0 {
		t.Errorf("bothBad: expected both fields flagged and no POST, got %+v", a)
	}
	if a := pick("valid"); a.Posted != 1 || a.ProjectError != "" || a.IDError != "" {
		t.Errorf("valid: expected one clean POST /tasks, got %+v", a)
	}
}

func TestWebUICenteredPage(t *testing.T) {
	ui := string(uiHTML)

	rule := regexp.MustCompile(`(?s)\n\s*body\s*\{(.*?)\}`).FindStringSubmatch(ui)
	if rule == nil {
		t.Fatal("expected a body rule in web/index.html")
	}
	decls := rule[1]

	m := regexp.MustCompile(`max-inline-size: *([0-9.]+)rem`).FindStringSubmatch(decls)
	if m == nil {
		t.Fatalf("expected max-inline-size in rem on body, got %q", decls)
	}
	size, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatalf("bad max-inline-size %q: %v", m[1], err)
	}
	px := size * 16
	if px < 1100 || px > 1400 {
		t.Errorf("max-inline-size %srem is %.0fpx: the queue pane wants 1100-1400px total with the 380px details pane beside it", m[1], px)
	}
	if !regexp.MustCompile(`margin-inline: *auto`).MatchString(decls) {
		t.Errorf("expected margin-inline: auto on body, got %q", decls)
	}
	if !regexp.MustCompile(`padding(-inline)?: *[1-9]`).MatchString(decls) {
		t.Errorf("expected body to keep a horizontal gutter, got %q", decls)
	}
}

func TestWebUIConnectionStatusMarkup(t *testing.T) {
	ui := string(uiHTML)

	pill := regexp.MustCompile(`<span[^>]*id="connection-status"[^>]*>`).FindString(ui)
	if pill == "" {
		t.Fatal("expected #connection-status pill in web/index.html")
	}
	if !strings.Contains(pill, `role="status"`) || !strings.Contains(pill, `aria-live="polite"`) {
		t.Fatalf("expected role=status and aria-live=polite on the pill, got %q", pill)
	}

	header := regexp.MustCompile(`(?s)<header>.*?</header>`).FindString(ui)
	if !strings.Contains(header, `id="connection-status"`) {
		t.Fatal("expected the connection pill inside <header>")
	}
	if !strings.Contains(ui, "#connection-status {") {
		t.Fatal("expected a #connection-status style rule in web/index.html")
	}
	if !strings.Contains(ui, "Offline") {
		t.Fatal("expected an offline label in web/index.html")
	}
}

func TestWebUIConnectionRetry(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required to run the web UI harness")
		}
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/connection.js", "web/index.html").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("harness failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("harness failed: %v", err)
	}
	type pill struct {
		Text    string
		Display string
		Banner  string
		Ticking bool
	}
	var got struct {
		Boot               pill
		Backoff            []int
		Countdown          string
		MutationBanner     string `json:"mutationBanner"`
		ValidationBanner   string `json:"validationBanner"`
		ValidationText     string `json:"validationText"`
		OverlapFetches     int    `json:"overlapFetches"`
		LogsWhileOffline   int    `json:"logsWhileOffline"`
		AutoRefreshOff     pill   `json:"autoRefreshOff"`
		Recovered, Relapse pill
		OnlineTicks        int `json:"onlineTicks"`
		RelapseTicks       int `json:"relapseTicks"`
		ManualRefreshTicks int `json:"afterManualRefresh"`
		OutOfBandTicks     int `json:"afterOutOfBandSuccess"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if got.Boot.Display == "none" || !strings.Contains(got.Boot.Text, "Offline") {
		t.Errorf("boot pill = %+v, want a visible offline pill", got.Boot)
	}
	if !strings.Contains(got.Boot.Text, "3s") {
		t.Errorf("boot pill text = %q, want a retry countdown", got.Boot.Text)
	}
	if got.Boot.Banner == "flex" || got.MutationBanner == "flex" {
		t.Errorf("error banner = %q/%q, want the pill to replace it",
			got.Boot.Banner, got.MutationBanner)
	}
	if got.ValidationBanner != "flex" || got.ValidationText == "" {
		t.Errorf("validation banner = %q %q, want form errors while offline",
			got.ValidationBanner, got.ValidationText)
	}
	if got.OverlapFetches != 0 {
		t.Errorf("%d requests started while a refresh was in flight, want 0",
			got.OverlapFetches)
	}
	if !got.Boot.Ticking {
		t.Error("expected a countdown ticker while offline")
	}

	want := []int{3, 6, 12, 24, 30}
	if !slices.Equal(got.Backoff, want) {
		t.Errorf("seconds between retries = %v, want capped backoff %v",
			got.Backoff, want)
	}
	if !strings.Contains(got.Countdown, "30s") {
		t.Errorf("countdown text = %q, want the capped delay", got.Countdown)
	}
	if got.LogsWhileOffline != 1 {
		t.Errorf("console errors during the outage = %d, want one per outage",
			got.LogsWhileOffline)
	}

	if got.AutoRefreshOff.Ticking {
		t.Error("countdown ticker still running with auto-refresh off")
	}
	if got.AutoRefreshOff.Text != "Offline" {
		t.Errorf("pill with auto-refresh off = %q, want a bare Offline",
			got.AutoRefreshOff.Text)
	}

	if got.Recovered.Display != "none" || got.Recovered.Text != "" {
		t.Errorf("recovered pill = %+v, want it cleared", got.Recovered)
	}
	if got.OnlineTicks != 3 {
		t.Errorf("online poll interval = %ds, want 3s", got.OnlineTicks)
	}
	if got.RelapseTicks != 3 || !strings.Contains(got.Relapse.Text, "Offline") {
		t.Errorf("second outage = %d %+v, want backoff reset and the pill back",
			got.RelapseTicks, got.Relapse)
	}
	if got.ManualRefreshTicks != 3 {
		t.Errorf("retry delay after manual refreshes = %ds, want 3s: clicking "+
			"Refresh must not advance the backoff", got.ManualRefreshTicks)
	}
	if got.OutOfBandTicks != 3 {
		t.Errorf("poll resumed %ds after a non-poll request succeeded, "+
			"want 3s", got.OutOfBandTicks)
	}
}

func TestWebUIEditTaskFields(t *testing.T) {
	ui := string(uiHTML)

	for _, check := range []struct {
		id    string
		label string
	}{
		{"edit-task-project", "Project"},
		{"edit-task-asset-path", "Asset Path"},
	} {
		if !strings.Contains(ui, `id="`+check.id+`"`) {
			t.Fatalf("expected control with id=%q in web/index.html", check.id)
		}
		if !strings.Contains(ui, `for="`+check.id+`"`) {
			t.Fatalf("expected a label with for=%q in web/index.html", check.id)
		}
	}

	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required to run the web UI harness")
		}
		t.Skip("node not installed")
	}

	out, err := exec.Command(node, "testdata/edit.js", "web/index.html").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("harness failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("harness failed: %v", err)
	}

	var got struct {
		InitialHasProject             bool
		InitialHasAsset               bool
		EditHasProjectInput           bool
		EditHasAssetInput             bool
		ProjectPrefilled              string
		AssetPrefilled                string
		InvalidProjectRejected        bool
		InvalidProjectError           bool
		WhitespaceBodyNoAssetRejected bool
		PatchSent                     bool
		PatchPayload                  struct {
			Project   string `json:"project"`
			AssetPath string `json:"asset_path"`
			Priority  int    `json:"priority"`
			Body      string `json:"body"`
		}
		IsEditingAfterSave       bool
		DetailsHasUpdatedProject bool
		DetailsHasUpdatedAsset   bool
		TaskUpdated              bool
		ProjectsReloaded         bool
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if !got.InitialHasProject || !got.InitialHasAsset {
		t.Errorf("initial view details missing project or asset: %+v", got)
	}
	if !got.EditHasProjectInput {
		t.Error("expected edit-task-project input in edit mode")
	}
	if !got.EditHasAssetInput {
		t.Error("expected edit-task-asset-path input in edit mode")
	}
	if got.ProjectPrefilled != "orig-proj" {
		t.Errorf("project prefilled = %q, want orig-proj", got.ProjectPrefilled)
	}
	if got.AssetPrefilled != "orig/asset.glb" {
		t.Errorf("asset prefilled = %q, want orig/asset.glb", got.AssetPrefilled)
	}
	if !got.InvalidProjectRejected || !got.InvalidProjectError {
		t.Errorf("invalid project should be rejected: rejected=%v, error=%v",
			got.InvalidProjectRejected, got.InvalidProjectError)
	}
	if !got.WhitespaceBodyNoAssetRejected {
		t.Error("expected save with whitespace body and no asset to be rejected")
	}
	if !got.PatchSent {
		t.Fatal("expected PATCH request to be sent on save")
	}
	if got.PatchPayload.Project != "new-proj" {
		t.Errorf("patch project = %q, want new-proj", got.PatchPayload.Project)
	}
	if got.PatchPayload.AssetPath != "models/updated.glb" {
		t.Errorf("patch asset_path = %q, want models/updated.glb", got.PatchPayload.AssetPath)
	}
	if got.PatchPayload.Priority != 5 {
		t.Errorf("patch priority = %d, want 5", got.PatchPayload.Priority)
	}
	if got.PatchPayload.Body != "updated body" {
		t.Errorf("patch body = %q, want updated body", got.PatchPayload.Body)
	}
	if got.IsEditingAfterSave {
		t.Error("expected isEditingTask to be false after save")
	}
	if !got.DetailsHasUpdatedProject {
		t.Error("expected details pane to show updated project")
	}
	if !got.DetailsHasUpdatedAsset {
		t.Error("expected details pane to show updated asset path")
	}
	if !got.TaskUpdated {
		t.Error("expected task record in table to be updated")
	}
	if !got.ProjectsReloaded {
		t.Error("expected projects list to be reloaded after saving task edit")
	}
}

func TestWebUIRowArrowKeyNavigation(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, "tr.setAttribute('tabindex', '-1')") {
		t.Fatal("expected tr to have tabindex -1 in web/index.html")
	}

	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required to run the web UI harness")
		}
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/navigation.js", "web/index.html").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("harness failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("harness failed: %v", err)
	}
	var got struct {
		DownFromRow1Selected    string
		DownFromRow1Focused     string
		DownFromRow2Selected    string
		DownFromRow2Focused     string
		DownAtBottomSelected    string
		DownAtBottomFocused     string
		UpFromRow3Selected      string
		UpFromRow3Focused       string
		UpFromRow2Selected      string
		UpFromRow2Focused       string
		UpAtTopSelected         string
		UpAtTopFocused          string
		DownFromButton1Selected string
		DownFromButton1Focused  string
		UpFromButton2Selected   string
		UpFromButton2Focused    string
		ModifierIgnored         bool
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if got.DownFromRow1Selected != "task-2" || got.DownFromRow1Focused != "task-2" {
		t.Errorf("ArrowDown from row-1: selected=%q focused=%q, want task-2",
			got.DownFromRow1Selected, got.DownFromRow1Focused)
	}
	if got.DownFromRow2Selected != "task-3" || got.DownFromRow2Focused != "task-3" {
		t.Errorf("ArrowDown from row-2: selected=%q focused=%q, want task-3",
			got.DownFromRow2Selected, got.DownFromRow2Focused)
	}
	if got.DownAtBottomSelected != "task-3" || got.DownAtBottomFocused != "task-3" {
		t.Errorf("ArrowDown at bottom: selected=%q focused=%q, want task-3",
			got.DownAtBottomSelected, got.DownAtBottomFocused)
	}
	if got.UpFromRow3Selected != "task-2" || got.UpFromRow3Focused != "task-2" {
		t.Errorf("ArrowUp from row-3: selected=%q focused=%q, want task-2",
			got.UpFromRow3Selected, got.UpFromRow3Focused)
	}
	if got.UpFromRow2Selected != "task-1" || got.UpFromRow2Focused != "task-1" {
		t.Errorf("ArrowUp from row-2: selected=%q focused=%q, want task-1",
			got.UpFromRow2Selected, got.UpFromRow2Focused)
	}
	if got.UpAtTopSelected != "task-1" || got.UpAtTopFocused != "task-1" {
		t.Errorf("ArrowUp at top: selected=%q focused=%q, want task-1",
			got.UpAtTopSelected, got.UpAtTopFocused)
	}
	if got.DownFromButton1Selected != "task-2" || got.DownFromButton1Focused != "task-2" {
		t.Errorf("ArrowDown from button-1: selected=%q focused=%q, want task-2",
			got.DownFromButton1Selected, got.DownFromButton1Focused)
	}
	if got.UpFromButton2Selected != "task-1" || got.UpFromButton2Focused != "task-1" {
		t.Errorf("ArrowUp from button-2: selected=%q focused=%q, want task-1",
			got.UpFromButton2Selected, got.UpFromButton2Focused)
	}
	if !got.ModifierIgnored {
		t.Error("expected modified arrow key (Ctrl+ArrowDown) to be ignored")
	}
}

func TestWebUIExpandBody(t *testing.T) {
	ui := string(uiHTML)

	if !strings.Contains(ui, `id="expand-body-btn"`) {
		t.Fatal("expected an expand control with id=\"expand-body-btn\"")
	}
	btn := regexp.MustCompile(`<button[^>]*id="expand-body-btn"[^>]*>`).FindString(ui)
	if btn == "" {
		t.Fatal("expected expand-body-btn to be a real <button>")
	}
	if !strings.Contains(btn, `type="button"`) {
		t.Fatalf("expected type=\"button\" on the expand control, got %q", btn)
	}
	if !regexp.MustCompile(`<dialog[^>]*id="body-overlay"`).MatchString(ui) {
		t.Fatal("expected the expanded body in a native <dialog>")
	}

	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required to run the web UI harness")
		}
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/expand.js", "web/index.html").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("harness failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("harness failed: %v", err)
	}
	var got struct {
		ButtonRendered         bool
		ClosedBeforeExpand     bool
		NoWindowKeydown        bool
		HasClickHandler        bool
		OpenedAsModal          bool
		OverlayBody            string
		ModalOpenerWasControl  bool
		ClosedAfterDismiss     bool
		FocusReturned          bool
		HasCloseHandler        bool
		ClosedAfterCloseButton bool
		PanelClickKeepsOpen    bool
		BackdropClickCloses    bool
		OverlayRefreshed       bool
		ClosedOverlayUntouched bool
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	checks := []struct {
		name string
		ok   bool
	}{
		{"expand control rendered in the details pane", got.ButtonRendered},
		{"dialog closed before expanding", got.ClosedBeforeExpand},
		{"Escape left to the dialog, no window keydown handler", got.NoWindowKeydown},
		{"expand control wired to a click handler", got.HasClickHandler},
		{"dialog opened with showModal", got.OpenedAsModal},
		{"the control owned focus when the dialog opened", got.ModalOpenerWasControl},
		{"dialog closed after dismissal", got.ClosedAfterDismiss},
		{"focus returned to the expand control on close", got.FocusReturned},
		{"close button wired", got.HasCloseHandler},
		{"dialog closed by the close button", got.ClosedAfterCloseButton},
		{"click inside the panel keeps the dialog open", got.PanelClickKeepsOpen},
		{"click on the backdrop closes the dialog", got.BackdropClickCloses},
		{"body refreshed while the dialog is open", got.OverlayRefreshed},
		{"closed dialog not refreshed", got.ClosedOverlayUntouched},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("expected %s", c.name)
		}
	}
	wantBody := "line one\n" + strings.Repeat("x", 400) + "\nlast line"
	if got.OverlayBody != wantBody {
		t.Errorf("overlay body = %q, want the full task body", got.OverlayBody)
	}
}

func TestWebUIPollKeepsSummary(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required to run the web UI harness")
		}
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/pollsummary.js", "web/index.html").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("harness failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("harness failed: %v", err)
	}

	var got struct {
		FirstLoad           []string `json:"firstLoad"`
		AfterPoll           []string `json:"afterPoll"`
		FirstURL            string   `json:"firstURL"`
		PollURL             string   `json:"pollURL"`
		AfterCreatePoll     []string `json:"afterCreatePoll"`
		FirstStatuses       []string `json:"firstStatuses"`
		AfterCreateStatuses []string `json:"afterCreateStatuses"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode harness output failed: %v\n%s", err, out)
	}

	want := "web: keep the Summary column populated"
	if len(got.FirstLoad) != 1 || got.FirstLoad[0] != want {
		t.Fatalf("first load summaries = %q, want [%q]", got.FirstLoad, want)
	}
	if len(got.AfterPoll) != 1 || got.AfterPoll[0] != want {
		t.Errorf("after one poll summaries = %q, want [%q]", got.AfterPoll, want)
	}
	if len(got.AfterCreatePoll) != 2 {
		t.Fatalf("after create poll summaries = %q, want 2 rows", got.AfterCreatePoll)
	}
	if got.AfterCreatePoll[0] != want {
		t.Errorf("existing row summary after poll = %q, want %q", got.AfterCreatePoll[0], want)
	}
	if created := "created while the page was open"; got.AfterCreatePoll[1] != created {
		t.Errorf("row created during a poll has summary %q, want %q", got.AfterCreatePoll[1], created)
	}
	if len(got.FirstStatuses) != 1 || got.FirstStatuses[0] != "pending" {
		t.Errorf("first load status cells = %q, want [\"pending\"]", got.FirstStatuses)
	}
	if len(got.AfterCreateStatuses) != 2 || got.AfterCreateStatuses[0] != "leased" || got.AfterCreateStatuses[1] != "pending" {
		t.Errorf("status cells after poll = %q, want [\"leased\" \"pending\"]", got.AfterCreateStatuses)
	}
	firstParams := listRequestParams(t, got.FirstURL)
	pollParams := listRequestParams(t, got.PollURL)
	if !maps.Equal(firstParams, pollParams) {
		t.Errorf("refresh request %q does not match the first paint %q", got.PollURL, got.FirstURL)
	}
}
