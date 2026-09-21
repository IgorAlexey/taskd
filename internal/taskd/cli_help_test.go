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
			if !strings.Contains(out, "Options of taskd serve:") {
				t.Fatalf("expected the serve options in help, got:\n%s", out)
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

func TestBareCommandDoesNotServe(t *testing.T) {
	for _, args := range [][]string{nil, {"-h"}, {"--help"}} {
		var out, errb bytes.Buffer
		if code := Main(&out, &errb, strings.NewReader(""), args); code != 0 || !strings.Contains(out.String(), "Usage of taskd") {
			t.Fatalf("taskd %v: exit %d, stdout %q stderr %q", args, code, out.String(), errb.String())
		}
	}
	var out, errb bytes.Buffer
	if code := Main(&out, &errb, strings.NewReader(""), []string{"-addr", "127.0.0.1:0"}); code != 2 || !strings.Contains(errb.String(), "unknown command") {
		t.Fatalf("taskd -addr: exit %d, stderr %q", code, errb.String())
	}
	out.Reset()
	if code := Main(&out, &errb, strings.NewReader(""), []string{"-v"}); code != 0 || !strings.HasPrefix(out.String(), "taskd ") {
		t.Fatalf("taskd -v: exit %d, stdout %q", code, out.String())
	}
}
