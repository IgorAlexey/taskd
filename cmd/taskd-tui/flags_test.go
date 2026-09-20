package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
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
		{"invalid status", []string{"-status", "bad"}, "taskd-tui: invalid status \"bad\" for -status, must be one of [all, pending, leased, done, buried, live]\ntry 'taskd-tui -h' for usage\n"},
		{"invalid status choice", []string{"-status", "invalid"}, "taskd-tui: invalid status \"invalid\" for -status, must be one of [all, pending, leased, done, buried, live]\ntry 'taskd-tui -h' for usage\n"},
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

func TestTUIVersionFlags(t *testing.T) {
	for _, flag := range []string{"-v", "-version", "--v", "--version"} {
		t.Run(flag, func(t *testing.T) {
			var buf bytes.Buffer
			if err := run(&buf, []string{flag}); err != nil {
				t.Fatalf("run(%q) returned error: %v", flag, err)
			}
			out := buf.String()
			if !strings.Contains(out, "taskd-tui") {
				t.Fatalf("run(%q) output = %q, want containing 'taskd-tui'", flag, out)
			}
		})
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

func TestParseFlags_RefreshEnv(t *testing.T) {
	t.Setenv("TASKD_REFRESH", "500ms")
	cfg, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.refresh != 500*time.Millisecond {
		t.Fatalf("refresh: got %v, want 500ms", cfg.refresh)
	}

	cfgOverride, err := parseFlags([]string{"-refresh", "2s"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfgOverride.refresh != 2*time.Second {
		t.Fatalf("refresh override: got %v, want 2s", cfgOverride.refresh)
	}

	t.Setenv("TASKD_REFRESH", "invalid")
	if _, err := parseFlags(nil); err == nil {
		t.Fatal("expected error for invalid TASKD_REFRESH, got nil")
	}

	t.Setenv("TASKD_REFRESH", "100ms")
	if _, err := parseFlags(nil); err == nil {
		t.Fatal("expected error for TASKD_REFRESH < 250ms, got nil")
	}

	var buf bytes.Buffer
	printUsage(&buf)
	usage := buf.String()
	if !strings.Contains(usage, "TASKD_REFRESH") {
		t.Fatal("printUsage missing TASKD_REFRESH")
	}
	idxEnv := strings.Index(usage, "Environment variables:")
	if idxEnv == -1 {
		t.Fatal("printUsage missing 'Environment variables:' section")
	}
	if !strings.Contains(usage[idxEnv:], "TASKD_REFRESH") {
		t.Fatal("printUsage missing TASKD_REFRESH under Environment variables")
	}
}

func TestParseAndValidateURL_DefaultScheme(t *testing.T) {
	cases := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "localhost:8080", want: "http://localhost:8080"},
		{input: "127.0.0.1:9090", want: "http://127.0.0.1:9090"},
		{input: ":8080", want: "http://127.0.0.1:8080"},
		{input: "https://localhost:8080", want: "https://localhost:8080"},
		{input: "https://127.0.0.1:9090", want: "https://127.0.0.1:9090"},
		{input: "http://localhost:8080", want: "http://localhost:8080"},
		{input: "http://:8080", want: "http://127.0.0.1:8080"},
		{input: "https://:8080", want: "https://127.0.0.1:8080"},
		{input: "localhost:8080/", want: "http://localhost:8080"},
		{input: ":8080/", want: "http://127.0.0.1:8080"},
		{input: "ftp://localhost", wantErr: true},
		{input: "", wantErr: true},
		{input: "   ", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseAndValidateURL(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseAndValidateURL(%q) succeeded, want error", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseAndValidateURL(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("parseAndValidateURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestFlagsInvalidProject(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		env     map[string]string
		wantErr string
	}{
		{
			name:    "invalid flag characters",
			args:    []string{"-project", "invalid name!"},
			wantErr: "taskd-tui: invalid project name \"invalid name!\": must contain only [A-Za-z0-9._-]\ntry 'taskd-tui -h' for usage\n",
		},
		{
			name:    "invalid flag chars with equal",
			args:    []string{"-project=bad/name"},
			wantErr: "taskd-tui: invalid project name \"bad/name\": must contain only [A-Za-z0-9._-]\ntry 'taskd-tui -h' for usage\n",
		},
		{
			name:    "invalid env characters",
			env:     map[string]string{"TASKD_PROJECT": "bad name?"},
			wantErr: "taskd-tui: invalid project name \"bad name?\": must contain only [A-Za-z0-9._-]\ntry 'taskd-tui -h' for usage\n",
		},
		{
			name:    "project name exceeds 64 chars",
			args:    []string{"-project", strings.Repeat("a", 65)},
			wantErr: "taskd-tui: invalid project name \"" + strings.Repeat("a", 65) + "\": must not exceed 64 characters\ntry 'taskd-tui -h' for usage\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := parseFlags(tc.args)
			if err == nil {
				t.Fatalf("expected error for %v, got nil", tc.args)
			}
			var stderr bytes.Buffer
			if code := reportError(&stderr, err); code != 2 {
				t.Fatalf("exit = %d, want 2 (err %v)", code, err)
			}
			if stderr.String() != tc.wantErr {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tc.wantErr)
			}
		})
	}

	t.Run("valid project flag", func(t *testing.T) {
		cfg, err := parseFlags([]string{"-project", "valid-project.123"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.project != "valid-project.123" {
			t.Fatalf("project = %q, want valid-project.123", cfg.project)
		}
	})

	t.Run("valid flag overrides invalid env", func(t *testing.T) {
		t.Setenv("TASKD_PROJECT", "invalid project!")
		cfg, err := parseFlags([]string{"-project", "override-valid"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.project != "override-valid" {
			t.Fatalf("project = %q, want override-valid", cfg.project)
		}
	})
}

func TestFlagsInvalidWorker(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		env     map[string]string
		wantErr string
	}{
		{
			name:    "invalid flag characters",
			args:    []string{"-worker", "invalid worker!"},
			wantErr: "taskd-tui: invalid worker \"invalid worker!\": must contain only [A-Za-z0-9._-:/]\ntry 'taskd-tui -h' for usage\n",
		},
		{
			name:    "invalid flag chars with equal",
			args:    []string{"-worker=bad worker?"},
			wantErr: "taskd-tui: invalid worker \"bad worker?\": must contain only [A-Za-z0-9._-:/]\ntry 'taskd-tui -h' for usage\n",
		},
		{
			name:    "invalid alias flag characters",
			args:    []string{"-w", "bad worker@"},
			wantErr: "taskd-tui: invalid worker \"bad worker@\": must contain only [A-Za-z0-9._-:/]\ntry 'taskd-tui -h' for usage\n",
		},
		{
			name:    "invalid env characters",
			env:     map[string]string{"TASKD_WORKER": "invalid worker!"},
			wantErr: "taskd-tui: invalid worker \"invalid worker!\": must contain only [A-Za-z0-9._-:/]\ntry 'taskd-tui -h' for usage\n",
		},
		{
			name:    "empty worker flag",
			args:    []string{"-worker", ""},
			wantErr: "taskd-tui: invalid worker \"\": must not be empty\ntry 'taskd-tui -h' for usage\n",
		},
		{
			name:    "worker exceeds 128 chars",
			args:    []string{"-worker", strings.Repeat("a", 129)},
			wantErr: "taskd-tui: invalid worker \"" + strings.Repeat("a", 129) + "\": must not exceed 128 characters\ntry 'taskd-tui -h' for usage\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := parseFlags(tc.args)
			if err == nil {
				t.Fatalf("expected error for %v, got nil", tc.args)
			}
			var stderr bytes.Buffer
			if code := reportError(&stderr, err); code != 2 {
				t.Fatalf("exit = %d, want 2 (err %v)", code, err)
			}
			if stderr.String() != tc.wantErr {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tc.wantErr)
			}
		})
	}

	t.Run("valid worker flag with slashes and colons", func(t *testing.T) {
		cfg, err := parseFlags([]string{"-worker", "host.domain.com:/path/to/worktree-1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.worker != "host.domain.com:/path/to/worktree-1" {
			t.Fatalf("worker = %q, want host.domain.com:/path/to/worktree-1", cfg.worker)
		}
	})

	t.Run("valid flag overrides invalid env", func(t *testing.T) {
		t.Setenv("TASKD_WORKER", "invalid worker!")
		cfg, err := parseFlags([]string{"-worker", "override-valid"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.worker != "override-valid" {
			t.Fatalf("worker = %q, want override-valid", cfg.worker)
		}
	})

	t.Run("zero flags uses valid default worker", func(t *testing.T) {
		t.Setenv("TASKD_WORKER", "")
		cfg, err := parseFlags(nil)
		if err != nil {
			t.Fatalf("unexpected error with zero flags: %v", err)
		}
		if cfg.worker == "" {
			t.Fatalf("expected non-empty default worker")
		}
		if err := validateWorker(cfg.worker); err != nil {
			t.Fatalf("default worker %q failed validation: %v", cfg.worker, err)
		}
	})
}

func TestQueryFlagAndEnv(t *testing.T) {
	t.Setenv("TASKD_PROJECT", "")
	t.Setenv("TASKD_QUERY", "")

	t.Run("short flag", func(t *testing.T) {
		cfg, err := parseFlags([]string{"-q", "alpha"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.query != "alpha" {
			t.Fatalf("cfg.query = %q, want alpha", cfg.query)
		}
		m := newModel(cfg, nil)
		if m.query != "alpha" {
			t.Fatalf("m.query = %q, want alpha", m.query)
		}
	})

	t.Run("long flag", func(t *testing.T) {
		cfg, err := parseFlags([]string{"-query", "alpha"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.query != "alpha" {
			t.Fatalf("cfg.query = %q, want alpha", cfg.query)
		}
	})

	t.Run("flag with equal", func(t *testing.T) {
		cfg, err := parseFlags([]string{"--query=alpha"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.query != "alpha" {
			t.Fatalf("cfg.query = %q, want alpha", cfg.query)
		}
	})

	t.Run("env variable", func(t *testing.T) {
		t.Setenv("TASKD_QUERY", "alpha")
		cfg, err := parseFlags(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.query != "alpha" {
			t.Fatalf("cfg.query = %q, want alpha", cfg.query)
		}
		m := newModel(cfg, nil)
		if m.query != "alpha" {
			t.Fatalf("m.query = %q, want alpha", m.query)
		}
	})

	t.Run("flag overrides env", func(t *testing.T) {
		t.Setenv("TASKD_QUERY", "env-value")
		cfg, err := parseFlags([]string{"-q", "alpha"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.query != "alpha" {
			t.Fatalf("cfg.query = %q, want alpha", cfg.query)
		}
	})

	t.Run("missing value", func(t *testing.T) {
		_, err := parseFlags([]string{"-q"})
		if err == nil {
			t.Fatal("expected error for missing -q value, got nil")
		}
		_, errLong := parseFlags([]string{"--query"})
		if errLong == nil {
			t.Fatal("expected error for missing --query value, got nil")
		}
	})

	t.Run("displays filtered task list on startup", func(t *testing.T) {
		cfg, err := parseFlags([]string{"-q", "alpha"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := newModel(cfg, nil)
		m.width = 80
		m.height = 24
		m.mode = modeTable
		m.tasks = []task{
			{ID: 1, Status: "pending", Body: "alpha task"},
		}
		m.rebuildShown()
		rendered := ansi.Strip(m.View().Content)
		if !strings.Contains(rendered, `filter "alpha" [Esc clear]`) {
			t.Fatalf("view missing filter indicator:\n%s", rendered)
		}
		if !strings.Contains(rendered, "alpha task") {
			t.Fatalf("view missing 'alpha task':\n%s", rendered)
		}
		lf := m.listFilter()
		if lf.query != "alpha" {
			t.Fatalf("listFilter().query = %q, want alpha", lf.query)
		}
	})

	t.Run("documented in usage", func(t *testing.T) {
		var buf bytes.Buffer
		printUsage(&buf)
		usage := buf.String()
		if !strings.Contains(usage, "-q") || !strings.Contains(usage, "-query") {
			t.Fatalf("usage missing -q/-query under Options:\n%s", usage)
		}
		idxEnv := strings.Index(usage, "Environment variables:")
		if idxEnv == -1 {
			t.Fatal("usage missing Environment variables section")
		}
		if !strings.Contains(usage[idxEnv:], "TASKD_QUERY") {
			t.Fatalf("usage missing TASKD_QUERY under Environment variables:\n%s", usage)
		}
	})
}

func TestStatusFlagAndEnv(t *testing.T) {
	t.Setenv("TASKD_PROJECT", "")
	t.Setenv("TASKD_STATUS", "")

	t.Run("status flag pending", func(t *testing.T) {
		cfg, err := parseFlags([]string{"-status", "pending"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.status != "pending" {
			t.Fatalf("cfg.status = %q, want pending", cfg.status)
		}
		m := newModel(cfg, nil)
		if m.filter != "pending" {
			t.Fatalf("m.filter = %q, want pending", m.filter)
		}
	})

	t.Run("status flag leased", func(t *testing.T) {
		cfg, err := parseFlags([]string{"--status=leased"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.status != "leased" {
			t.Fatalf("cfg.status = %q, want leased", cfg.status)
		}
		m := newModel(cfg, nil)
		if m.filter != "leased" {
			t.Fatalf("m.filter = %q, want leased", m.filter)
		}
	})

	t.Run("status flag all", func(t *testing.T) {
		cfg, err := parseFlags([]string{"-status", "all"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.status != "" {
			t.Fatalf("cfg.status = %q, want empty", cfg.status)
		}
		m := newModel(cfg, nil)
		if m.filter != "" {
			t.Fatalf("m.filter = %q, want empty", m.filter)
		}
	})

	t.Run("env variable leased", func(t *testing.T) {
		t.Setenv("TASKD_STATUS", "leased")
		cfg, err := parseFlags(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.status != "leased" {
			t.Fatalf("cfg.status = %q, want leased", cfg.status)
		}
		m := newModel(cfg, nil)
		if m.filter != "leased" {
			t.Fatalf("m.filter = %q, want leased", m.filter)
		}
	})

	t.Run("flag overrides env", func(t *testing.T) {
		t.Setenv("TASKD_STATUS", "leased")
		cfg, err := parseFlags([]string{"-status", "pending"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.status != "pending" {
			t.Fatalf("cfg.status = %q, want pending", cfg.status)
		}
		m := newModel(cfg, nil)
		if m.filter != "pending" {
			t.Fatalf("m.filter = %q, want pending", m.filter)
		}
	})

	t.Run("invalid flag status exits 2", func(t *testing.T) {
		_, err := parseFlags([]string{"-status", "unknown_status"})
		if err == nil {
			t.Fatal("expected error for invalid status, got nil")
		}
		var stderr bytes.Buffer
		code := reportError(&stderr, err)
		if code != 2 {
			t.Fatalf("exit code = %d, want 2", code)
		}
		want := "taskd-tui: invalid status \"unknown_status\" for -status, must be one of [all, pending, leased, done, buried, live]\ntry 'taskd-tui -h' for usage\n"
		if stderr.String() != want {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	})

	t.Run("invalid env status exits 2", func(t *testing.T) {
		t.Setenv("TASKD_STATUS", "bogus")
		_, err := parseFlags(nil)
		if err == nil {
			t.Fatal("expected error for invalid TASKD_STATUS, got nil")
		}
		var stderr bytes.Buffer
		code := reportError(&stderr, err)
		if code != 2 {
			t.Fatalf("exit code = %d, want 2", code)
		}
		want := "taskd-tui: invalid status \"bogus\" for TASKD_STATUS, must be one of [all, pending, leased, done, buried, live]\ntry 'taskd-tui -h' for usage\n"
		if stderr.String() != want {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	})

	t.Run("missing value", func(t *testing.T) {
		_, err := parseFlags([]string{"-status"})
		if err == nil {
			t.Fatal("expected error for missing -status value, got nil")
		}
		var stderr bytes.Buffer
		if code := reportError(&stderr, err); code != 2 {
			t.Fatalf("exit code = %d, want 2", code)
		}
	})

	t.Run("displays preselected status filter on startup", func(t *testing.T) {
		cfg, err := parseFlags([]string{"-status", "pending"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := newModel(cfg, nil)
		m.width = 120
		m.height = 24
		m.mode = modeTable
		m.hasStats = true
		m.stats = stats{Pending: 2, Leased: 1, Total: 3}
		m.tasks = []task{
			{ID: 1, Status: "pending", Body: "pending task"},
			{ID: 2, Status: "leased", Body: "leased task"},
		}
		m.rebuildShown()
		rendered := ansi.Strip(m.View().Content)
		if !strings.Contains(rendered, "1 pending") {
			t.Fatalf("view missing '1 pending' tab indicator:\n%s", rendered)
		}
		if !strings.Contains(rendered, "pending task") {
			t.Fatalf("view missing 'pending task':\n%s", rendered)
		}
		if strings.Contains(rendered, "leased task") {
			t.Fatalf("view unexpectedly contains 'leased task':\n%s", rendered)
		}
		lf := m.listFilter()
		if lf.status != "pending" {
			t.Fatalf("listFilter().status = %q, want pending", lf.status)
		}
	})

	t.Run("documented in usage", func(t *testing.T) {
		var buf bytes.Buffer
		printUsage(&buf)
		usage := buf.String()
		if !strings.Contains(usage, "-status") {
			t.Fatalf("usage missing -status under Options:\n%s", usage)
		}
		idxEnv := strings.Index(usage, "Environment variables:")
		if idxEnv == -1 {
			t.Fatal("usage missing Environment variables section")
		}
		if !strings.Contains(usage[idxEnv:], "TASKD_STATUS") {
			t.Fatalf("usage missing TASKD_STATUS under Environment variables:\n%s", usage)
		}
	})
}
