package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestUsageErrorDiagnostic(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"-verbose"}, "taskd: unrecognized flag -verbose\ntry 'taskd -h' for usage\n"},
		{"unknown long flag", []string{"--verbose"}, "taskd: unrecognized flag --verbose\ntry 'taskd -h' for usage\n"},
		{"unknown flag with value", []string{"--verbose=2"}, "taskd: unrecognized flag --verbose\ntry 'taskd -h' for usage\n"},
		{"positional", []string{"tasks"}, "taskd: unexpected argument: tasks\ntry 'taskd -h' for usage\n"},
		{"missing value", []string{"-db"}, "taskd: flag needs an argument: -db\ntry 'taskd -h' for usage\n"},
		{"bad lease", []string{"-lease", "0"}, "taskd: lease duration must be greater than 0: got 0\ntry 'taskd -h' for usage\n"},
		{"empty db", []string{"-db", " "}, "taskd: database path cannot be empty\ntry 'taskd -h' for usage\n"},
		{"empty addr", []string{"-addr", " "}, "taskd: listen address cannot be empty\ntry 'taskd -h' for usage\n"},
		{"negative max claims", []string{"-max-claims", "-1"}, "taskd: max claims cannot be negative: got -1\ntry 'taskd -h' for usage\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseFlags(tc.args)
			var stderr bytes.Buffer
			if code := fatal(&stderr, err); code != 2 {
				t.Fatalf("exit = %d, want 2 (err %v)", code, err)
			}
			if stderr.String() != tc.want {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tc.want)
			}
		})
	}
}

func TestUsageErrorBadValueIsOneLine(t *testing.T) {
	_, err := parseFlags([]string{"-lease", "abc"})
	var stderr bytes.Buffer
	if code := fatal(&stderr, err); code != 2 {
		t.Fatalf("exit = %d, want 2 (err %v)", code, err)
	}
	lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), stderr.String())
	}
	if !strings.HasPrefix(lines[0], `taskd: invalid value "abc" for flag -lease`) {
		t.Fatalf("unexpected reason line: %q", lines[0])
	}
	if lines[1] != "try 'taskd -h' for usage" {
		t.Fatalf("unexpected hint line: %q", lines[1])
	}
}

func TestRuntimeErrorExitsOne(t *testing.T) {
	var stderr bytes.Buffer
	if code := fatal(&stderr, errors.New("database is locked")); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "database is locked") {
		t.Fatalf("expected runtime error on stderr, got %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "try 'taskd -h'") {
		t.Fatalf("runtime error should carry no usage hint, got %q", stderr.String())
	}
}
