package taskd

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionFlags(t *testing.T) {
	for _, flag := range []string{"-v", "-version", "--v", "--version"} {
		t.Run(flag, func(t *testing.T) {
			var buf bytes.Buffer
			if err := run(&buf, []string{flag}); err != nil {
				t.Fatalf("run(%q) returned error: %v", flag, err)
			}
			out := buf.String()
			if !strings.Contains(out, "taskd") {
				t.Fatalf("run(%q) output = %q, want containing 'taskd'", flag, out)
			}
		})
	}
}
