package taskd

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

func TestUsageDocumentsRoutes(t *testing.T) {
	var usage bytes.Buffer
	printUsage(&usage)
	for _, want := range []string{"POST   /tasks/purge", "?worker=", "  claim [id]"} {
		if !strings.Contains(usage.String(), want) {
			t.Fatalf("usage does not mention %q", want)
		}
	}
}
