package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

func TestOpenDBDirectoryFailure(t *testing.T) {
	dir := t.TempDir()
	_, err := openDB(dir)
	if err == nil {
		t.Fatalf("expected openDB(%s) to fail, got nil", dir)
	}
	want := "cannot open database " + dir + ": unable to open database file (14)"
	if err.Error() != want {
		t.Fatalf("got error %q, want %q", err.Error(), want)
	}
}

func TestOpenDBUnwriteableDirectoryFailure(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "ro")
	if err := os.MkdirAll(parent, 0500); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	dbPath := filepath.Join(parent, "taskd.db")
	_, err := openDB(dbPath)
	if err == nil {
		t.Fatalf("expected openDB(%s) to fail, got nil", dbPath)
	}
	want := "cannot open database " + dbPath + ": unable to open database file (14)"
	if err.Error() != want {
		t.Fatalf("got error %q, want %q", err.Error(), want)
	}
}

func TestOpenDBNotADatabaseFailure(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(filePath, []byte("hi\n"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	_, err := openDB(filePath)
	if err == nil {
		t.Fatalf("expected openDB(%s) to fail, got nil", filePath)
	}
	want := "cannot open database " + filePath + ": file is not a database (26)"
	if err.Error() != want {
		t.Fatalf("got error %q, want %q", err.Error(), want)
	}
}

func TestCLIDatabaseOpenFailureOutput(t *testing.T) {
	dir := t.TempDir()
	err := run([]string{"-db", dir, "-addr", ":18802"})
	if err == nil {
		t.Fatal("expected run to fail for directory db")
	}
	var stderr bytes.Buffer
	code := fatal(&stderr, err)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	wantOut := "taskd: cannot open database " + dir + ": unable to open database file (14)\n       is -db pointing at a directory, or a path you cannot write?\n"
	if stderr.String() != wantOut {
		t.Fatalf("got stderr %q, want %q", stderr.String(), wantOut)
	}

	parent := filepath.Join(t.TempDir(), "ro")
	if err := os.MkdirAll(parent, 0500); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	roPath := filepath.Join(parent, "taskd.db")
	err = run([]string{"-db", roPath, "-addr", ":18802"})
	if err == nil {
		t.Fatal("expected run to fail for unwriteable db path")
	}
	stderr.Reset()
	code = fatal(&stderr, err)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	wantOut = "taskd: cannot open database " + roPath + ": unable to open database file (14)\n       is -db pointing at a directory, or a path you cannot write?\n"
	if stderr.String() != wantOut {
		t.Fatalf("got stderr %q, want %q", stderr.String(), wantOut)
	}

	notesPath := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(notesPath, []byte("hi\n"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	err = run([]string{"-db", notesPath, "-addr", ":18802"})
	if err == nil {
		t.Fatal("expected run to fail for non-db file")
	}
	stderr.Reset()
	code = fatal(&stderr, err)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	wantOut = "taskd: cannot open database " + notesPath + ": file is not a database (26)\n"
	if stderr.String() != wantOut {
		t.Fatalf("got stderr %q, want %q", stderr.String(), wantOut)
	}

	backupDest := filepath.Join(t.TempDir(), "backup.db")
	err = run([]string{"-db", notesPath, "-backup", backupDest})
	if err == nil {
		t.Fatal("expected run to fail for backup with non-db file")
	}
	stderr.Reset()
	code = fatal(&stderr, err)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	wantOut = "taskd: cannot open database " + notesPath + ": file is not a database (26)\n"
	if stderr.String() != wantOut {
		t.Fatalf("got stderr %q, want %q", stderr.String(), wantOut)
	}
}

func TestParseFlagsLeaseBounds(t *testing.T) {
	cases := []struct {
		name    string
		lease   string
		wantErr string
	}{
		{
			name:    "negative",
			lease:   "-1",
			wantErr: "-lease must be between 1 and 31536000 seconds: got -1",
		},
		{
			name:    "zero",
			lease:   "0",
			wantErr: "-lease must be between 1 and 31536000 seconds: got 0",
		},
		{
			name:    "exceeds ceiling",
			lease:   "31536001",
			wantErr: "-lease must be between 1 and 31536000 seconds: got 31536001",
		},
		{
			name:    "max int64",
			lease:   "9223372036854775807",
			wantErr: "-lease must be between 1 and 31536000 seconds: got 9223372036854775807",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseFlags([]string{"-lease", tc.lease})
			if err == nil {
				t.Fatalf("expected error for lease %s, got nil", tc.lease)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
			var buf bytes.Buffer
			if code := fatal(&buf, err); code != 2 {
				t.Fatalf("fatal exit code = %d, want 2", code)
			}
			if !strings.Contains(buf.String(), "taskd: "+tc.wantErr) {
				t.Fatalf("fatal output %q does not contain taskd: %s", buf.String(), tc.wantErr)
			}
		})
	}
	for _, valid := range []string{"1", "300", "31536000"} {
		cfg, err := parseFlags([]string{"-lease", valid})
		if err != nil {
			t.Fatalf("unexpected error for valid lease %s: %v", valid, err)
		}
		want, _ := strconv.Atoi(valid)
		if cfg.lease != want {
			t.Fatalf("cfg.lease = %d, want %d", cfg.lease, want)
		}
	}
}
