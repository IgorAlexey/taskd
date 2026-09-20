package main

import (
	"bytes"
	"log"
	"net"
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
