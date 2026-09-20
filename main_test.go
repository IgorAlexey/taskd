package main

import (
	"bytes"
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
