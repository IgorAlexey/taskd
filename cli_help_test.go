package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCLIHelp(t *testing.T) {
	for _, flag := range []string{"-h", "-help", "--help"} {
		t.Run(flag, func(t *testing.T) {
			var buf bytes.Buffer
			if err := run(&buf, []string{flag}); err != nil {
				t.Fatalf("run(%q) returned error: %v", flag, err)
			}
			out := buf.String()
			if !strings.Contains(out, "Options:") {
				t.Fatalf("expected Options: in help, got:\n%s", out)
			}
			if !strings.Contains(out, "-h, --help") {
				t.Fatalf("expected -h, --help in help, got:\n%s", out)
			}
			if !strings.Contains(out, "-v, -version") {
				t.Fatalf("expected -v, -version in help, got:\n%s", out)
			}
			if strings.Contains(out, "  -v\t") {
				t.Fatalf("unexpected separate -v entry in help, got:\n%s", out)
			}
		})
	}
}

func TestCLIHelpPrefixDocs(t *testing.T) {
	var buf bytes.Buffer
	if err := run(&buf, []string{"-h"}); err != nil {
		t.Fatalf("run(-h): %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "{id} accepts unique prefixes") {
		t.Fatalf("expected '{id} accepts unique prefixes' in help output, got:\n%s", out)
	}
	if !strings.Contains(out, "409") {
		t.Fatalf("expected 409 collision note in help output, got:\n%s", out)
	}
}
