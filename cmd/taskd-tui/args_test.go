package main

import (
	"strings"
	"testing"
)

func TestTUIPositionalArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "single project argument",
			args: []string{"myproject"},
			want: "unexpected argument: myproject",
		},
		{
			name: "single url argument",
			args: []string{"http://localhost:8080"},
			want: "unexpected argument: http://localhost:8080",
		},
		{
			name: "argument after flag",
			args: []string{"-url", "http://localhost:8080", "extra"},
			want: "unexpected argument: extra",
		},
		{
			name: "argument before flag",
			args: []string{"bogus", "-project", "proj"},
			want: "unexpected argument: bogus",
		},
		{
			name: "multiple arguments",
			args: []string{"first", "second", "third"},
			want: "unexpected argument: first",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseFlags(tc.args)
			if err == nil {
				t.Fatalf("expected error for args %v, got nil", tc.args)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %q", tc.want, err.Error())
			}
		})
	}

	t.Run("valid invocation without positional args", func(t *testing.T) {
		cfg, err := parseFlags([]string{"-url", "http://custom:9090", "-project", "proj"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.url != "http://custom:9090" {
			t.Fatalf("expected url http://custom:9090, got %q", cfg.url)
		}
		if cfg.project != "proj" {
			t.Fatalf("expected project proj, got %q", cfg.project)
		}
	})
}
