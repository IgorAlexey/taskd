package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"
)

func TestTUIFlagErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"-unknown"}, "taskd-tui: unrecognized flag -unknown\ntry 'taskd-tui -h' for usage\n"},
		{"unknown long flag", []string{"--unknown"}, "taskd-tui: unrecognized flag --unknown\ntry 'taskd-tui -h' for usage\n"},
		{"unknown flag with value", []string{"--unknown=val"}, "taskd-tui: unrecognized flag --unknown\ntry 'taskd-tui -h' for usage\n"},
		{"positional", []string{"extra"}, "taskd-tui: unexpected argument: extra\ntry 'taskd-tui -h' for usage\n"},
		{"positional with flag", []string{"-url", "http://localhost:8080", "extra"}, "taskd-tui: unexpected argument: extra\ntry 'taskd-tui -h' for usage\n"},
		{"positional after terminator", []string{"--", "extra"}, "taskd-tui: unexpected argument: extra\ntry 'taskd-tui -h' for usage\n"},
		{"flag value looks like flag", []string{"-project", "--bogus", "-unknown"}, "taskd-tui: unrecognized flag -unknown\ntry 'taskd-tui -h' for usage\n"},
		{"missing value", []string{"-url"}, "taskd-tui: flag needs an argument: -url\ntry 'taskd-tui -h' for usage\n"},
		{"missing long value", []string{"--project"}, "taskd-tui: flag needs an argument: --project\ntry 'taskd-tui -h' for usage\n"},
		{"refresh too short", []string{"-refresh", "100ms"}, "taskd-tui: refresh interval must be at least 250ms: got 100ms\ntry 'taskd-tui -h' for usage\n"},
		{"invalid refresh duration", []string{"-refresh", "bad"}, "taskd-tui: invalid duration \"bad\" for flag -refresh\ntry 'taskd-tui -h' for usage\n"},
		{"invalid ascii bool", []string{"-ascii=maybe"}, "taskd-tui: invalid boolean value \"maybe\" for -ascii\ntry 'taskd-tui -h' for usage\n"},
		{"empty url", []string{"-url", " "}, "taskd-tui: url cannot be empty\ntry 'taskd-tui -h' for usage\n"},
		{"invalid url scheme", []string{"-url", "ftp://localhost"}, "taskd-tui: invalid url scheme \"ftp\": must be http or https\ntry 'taskd-tui -h' for usage\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseFlags(tc.args)
			if err == nil {
				t.Fatalf("expected error for %v, got nil", tc.args)
			}
			var stderr bytes.Buffer
			if code := reportError(&stderr, err); code != 2 {
				t.Fatalf("exit = %d, want 2 (err %v)", code, err)
			}
			if stderr.String() != tc.want {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tc.want)
			}
		})
	}
}

func TestTUIHelpFlags(t *testing.T) {
	for _, arg := range []string{"-h", "-help", "--help"} {
		t.Run(arg, func(t *testing.T) {
			_, err := parseFlags([]string{arg})
			if !errors.Is(err, flag.ErrHelp) {
				t.Fatalf("expected ErrHelp for %q, got: %v", arg, err)
			}
		})
	}
	var buf bytes.Buffer
	printUsage(&buf)
	if !strings.Contains(buf.String(), "Usage: taskd-tui") {
		t.Fatalf("expected usage text, got: %q", buf.String())
	}
}

func TestTUIRuntimeErrorExitsOne(t *testing.T) {
	var stderr bytes.Buffer
	if code := reportError(&stderr, errors.New("terminal disconnected")); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "taskd-tui: terminal disconnected\n") {
		t.Fatalf("expected runtime error on stderr, got %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "try 'taskd-tui -h'") {
		t.Fatalf("runtime error should carry no usage hint, got %q", stderr.String())
	}
}
