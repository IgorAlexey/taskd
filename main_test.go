package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCLIUsageHelpQueryParameters(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	usage := buf.String()

	required := []string{
		"buried", "live", "1..1000", "project=*",
		"?status=", "?project=", "?worker=", "?priority=",
		"?limit=", "?offset=", "?after=", "?asset_path=",
		"?q=", "?fields=", "?columns=", "?force=",
	}
	for _, token := range required {
		if !strings.Contains(usage, token) {
			t.Fatalf("usage output missing %q:\n%s", token, usage)
		}
	}
}

func TestWebUIProjectDatalist(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, `<datalist id="form-project-list">`) {
		t.Fatal("expected <datalist id=\"form-project-list\"> in web/index.html")
	}
	projInput := regexp.MustCompile(`<input[^>]*id="form-project"[^>]*>`).FindString(ui)
	if projInput == "" {
		t.Fatal("expected #form-project input in web/index.html")
	}
	if !strings.Contains(projInput, `list="form-project-list"`) {
		t.Fatalf("expected list attribute on #form-project, got %q", projInput)
	}
	if !strings.Contains(ui, `document.getElementById('form-project-list')`) {
		t.Fatal("expected script to populate form-project-list")
	}
}

func TestParseFlagsDefaultAddrLoopback(t *testing.T) {
	cfg, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("parseFlags failed: %v", err)
	}
	host, _, err := net.SplitHostPort(cfg.addr)
	if err != nil {
		t.Fatalf("failed to split default addr %q: %v", cfg.addr, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		t.Fatalf("expected default addr %q to be loopback", cfg.addr)
	}
}

func TestExplicitNonLoopbackAddrWarning(t *testing.T) {
	var buf bytes.Buffer
	origWriter := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(origWriter)

	l, err := listen(":0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer l.Close()

	out := buf.String()
	const wantWarning = "warning: listening on all interfaces; taskd has no authentication"
	if !strings.Contains(out, wantWarning) {
		t.Fatalf("expected log output to contain %q, got %q", wantWarning, out)
	}
	if !strings.Contains(out, "listening on ") {
		t.Fatalf("expected log output to contain listening on, got %q", out)
	}
}

func TestExplicitLoopbackAddrNoWarning(t *testing.T) {
	var buf bytes.Buffer
	origWriter := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(origWriter)

	l, err := listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer l.Close()

	out := buf.String()
	const wantWarning = "warning: listening on all interfaces; taskd has no authentication"
	if strings.Contains(out, wantWarning) {
		t.Fatalf("did not expect warning for loopback address, got %q", out)
	}
	if !strings.Contains(out, "listening on 127.0.0.1:") {
		t.Fatalf("expected log output to contain listening on 127.0.0.1:, got %q", out)
	}
}

func TestUsageMentionsExpose(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	usage := buf.String()

	if !strings.Contains(usage, "127.0.0.1:8080") {
		t.Fatalf("expected usage to mention 127.0.0.1:8080, got:\n%s", usage)
	}
	if !strings.Contains(usage, "-addr :8080") {
		t.Fatalf("expected usage to mention -addr :8080, got:\n%s", usage)
	}
}

type testClient struct {
	t   *testing.T
	srv *httptest.Server
}

func (c *testClient) post(path, body string) (*http.Response, taskItem) {
	c.t.Helper()
	res, err := c.srv.Client().Post(c.srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	var item taskItem
	if strings.Contains(res.Header.Get("Content-Type"), "application/json") {
		json.NewDecoder(res.Body).Decode(&item)
	}
	res.Body.Close()
	return res, item
}

func (c *testClient) get(path string) taskItem {
	c.t.Helper()
	res, err := c.srv.Client().Get(c.srv.URL + path)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	var item taskItem
	json.NewDecoder(res.Body).Decode(&item)
	return item
}

func TestVoluntaryReleaseRefundsMaxClaims(t *testing.T) {
	st, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	srv := httptest.NewServer(newHandlerWithCORS(st, 300, 2, ""))
	defer srv.Close()
	c := testClient{t: t, srv: srv}

	c.post("/tasks", `{"id":"healthy01","project":"mc","body":"healthy"}`)
	_, itemA := c.post("/tasks/claim", `{"worker":"A","project":"mc"}`)
	if itemA.ClaimCount != 1 {
		t.Fatalf("worker A claim_count = %d, want 1", itemA.ClaimCount)
	}
	c.post("/tasks/healthy01/release", `{"worker":"A"}`)

	_, itemB := c.post("/tasks/claim", `{"worker":"B","project":"mc"}`)
	if itemB.ClaimCount != 1 {
		t.Fatalf("worker B claim_count = %d, want 1", itemB.ClaimCount)
	}
	c.post("/tasks/healthy01/release", `{"worker":"B"}`)

	task := c.get("/tasks/healthy01")
	if task.Status != "pending" || task.ClaimCount != 0 {
		t.Fatalf("task = %+v, want pending with claim_count 0", task)
	}

	resC, itemC := c.post("/tasks/claim", `{"worker":"C","project":"mc"}`)
	if resC.StatusCode != http.StatusOK || itemC.ID != "healthy01" {
		t.Fatalf("worker C claim status = %d, id = %q", resC.StatusCode, itemC.ID)
	}
}

func TestLeaseExpirationExhaustsMaxClaims(t *testing.T) {
	st, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	srv := httptest.NewServer(newHandlerWithCORS(st, -1, 2, ""))
	defer srv.Close()
	c := testClient{t: t, srv: srv}

	c.post("/tasks", `{"id":"expired01","project":"mc","body":"failing"}`)
	_, itemA := c.post("/tasks/claim", `{"worker":"A","project":"mc"}`)
	if itemA.ClaimCount != 1 {
		t.Fatalf("worker A claim_count = %d, want 1", itemA.ClaimCount)
	}

	_, itemB := c.post("/tasks/claim", `{"worker":"B","project":"mc"}`)
	if itemB.ClaimCount != 2 {
		t.Fatalf("worker B claim_count = %d, want 2", itemB.ClaimCount)
	}

	resC, _ := c.post("/tasks/claim", `{"worker":"C","project":"mc"}`)
	if resC.StatusCode != http.StatusNoContent {
		t.Fatalf("worker C status = %d, want 204 No Content", resC.StatusCode)
	}

	task := c.get("/tasks/expired01")
	if task.Status != "buried" || task.ClaimCount != 2 {
		t.Fatalf("task = %+v, want buried with claim_count 2", task)
	}
}
