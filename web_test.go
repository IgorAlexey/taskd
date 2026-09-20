package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func runNodeHarness(t *testing.T, testName string) []byte {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required to run the web UI harness")
		}
		t.Skip("node not installed")
	}
	cmd := exec.Command(node, "testdata/ui.js", testName, "web/index.html")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("harness failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("harness failed: %v", err)
	}
	return out
}

func TestWebUIRendersSections(t *testing.T) {
	out := runNodeHarness(t, "renders_sections")
	var got struct {
		Sections   []string `json:"sections"`
		StatusText string   `json:"statusText"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	wantSections := []string{"stuck", "claimed", "queued", "done"}
	if !slices.Equal(got.Sections, wantSections) {
		t.Errorf("sections = %v, want %v", got.Sections, wantSections)
	}
	if !strings.Contains(got.StatusText, "1 is stuck") {
		t.Errorf("statusText = %q, want it to contain '1 is stuck'", got.StatusText)
	}
}

func TestWebUIEventTriggersReload(t *testing.T) {
	out := runNodeHarness(t, "event_reload")
	var got struct {
		InitialFetches     int `json:"initialFetches"`
		BeforeFlushFetches int `json:"beforeFlushFetches"`
		TotalFetches       int `json:"totalFetches"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if got.InitialFetches != 1 {
		t.Errorf("initialFetches = %d, want 1", got.InitialFetches)
	}
	if got.BeforeFlushFetches != 1 {
		t.Errorf("beforeFlushFetches = %d, want 1 (reload was scheduled, not immediate)", got.BeforeFlushFetches)
	}
	if got.TotalFetches != 2 {
		t.Errorf("totalFetches = %d, want 2 (exactly one GET /tasks after two rapid change events)", got.TotalFetches)
	}
}

func TestWebUIAddTaskPosts(t *testing.T) {
	out := runNodeHarness(t, "add_task")
	var got struct {
		Posted struct {
			Body     string `json:"body"`
			Project  string `json:"project"`
			Priority int    `json:"priority"`
			After    []int  `json:"after"`
		} `json:"posted"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if got.Posted.Body != "New Task Title\nDetailed instructions" {
		t.Errorf("body = %q, want 'New Task Title\\nDetailed instructions'", got.Posted.Body)
	}
	if got.Posted.Project != "taskd" {
		t.Errorf("project = %q, want 'taskd'", got.Posted.Project)
	}
	if got.Posted.Priority != 2 {
		t.Errorf("priority = %d, want 2", got.Posted.Priority)
	}
	if len(got.Posted.After) != 0 {
		t.Errorf("after = %v, want empty", got.Posted.After)
	}
}

func TestWebUIReplyPostsNoteAndKicks(t *testing.T) {
	out := runNodeHarness(t, "reply_kick")
	var got struct {
		Requests []struct {
			Method string          `json:"method"`
			URL    string          `json:"url"`
			Body   json.RawMessage `json:"body"`
		} `json:"requests"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	var posts []struct {
		url  string
		body string
	}
	for _, req := range got.Requests {
		if req.Method == "POST" {
			posts = append(posts, struct {
				url  string
				body string
			}{url: req.URL, body: string(req.Body)})
		}
	}
	if len(posts) < 2 {
		t.Fatalf("got %d POST requests, want at least 2: %v", len(posts), got.Requests)
	}
	if posts[0].url != "/tasks/42/notes" {
		t.Errorf("first POST = %q, want '/tasks/42/notes'", posts[0].url)
	}
	if !strings.Contains(posts[0].body, `"author":"you"`) || !strings.Contains(posts[0].body, `"text":"I fixed the issue"`) {
		t.Errorf("first POST body = %s, want author 'you' and text", posts[0].body)
	}
	if posts[1].url != "/tasks/42/kick" {
		t.Errorf("second POST = %q, want '/tasks/42/kick'", posts[1].url)
	}
}

func TestWebUIBlockedNotClaimable(t *testing.T) {
	out := runNodeHarness(t, "blocked_not_claimable")
	var got struct {
		Position string `json:"position"`
		Why      string `json:"why"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if got.Position != "" {
		t.Errorf("position = %q, want empty", got.Position)
	}
	if !strings.Contains(got.Why, "Not claimable until this is done: Prereq task") {
		t.Errorf("why = %q, want it to contain 'Not claimable until this is done: Prereq task'", got.Why)
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
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}
}

func TestWebUINoPolling(t *testing.T) {
	ui := string(uiHTML)
	if strings.Contains(strings.ToLower(ui), "asset") {
		t.Error("web/index.html contains 'asset'")
	}
	if strings.Contains(strings.ToLower(ui), "budget") {
		t.Error("web/index.html contains 'budget'")
	}
	if strings.Contains(strings.ToLower(ui), "tokens") {
		t.Error("web/index.html contains 'tokens'")
	}
	if strings.Contains(ui, "db.tasks") {
		t.Error("web/index.html contains 'db.tasks'")
	}
	if strings.Contains(ui, "HOURLY") {
		t.Error("web/index.html contains 'HOURLY'")
	}
	if strings.Contains(ui, "#type") {
		t.Error("web/index.html contains '#type'")
	}
	re := regexp.MustCompile(`setInterval\s*\([^)]*fetch`)
	if re.MatchString(ui) {
		t.Error("web/index.html contains setInterval calling fetch directly")
	}
}
