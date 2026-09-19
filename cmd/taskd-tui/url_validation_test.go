package main

import (
	"strings"
	"testing"
)

func TestTUIURLValidation(t *testing.T) {
	t.Run("empty url flag rejected", func(t *testing.T) {
		for _, raw := range []string{"", "   ", "\t"} {
			_, err := parseFlags([]string{"-url", raw})
			if err == nil {
				t.Fatalf("expected error for empty -url %q, got nil", raw)
			}
			if !strings.Contains(err.Error(), "url cannot be empty") {
				t.Fatalf("expected error to mention 'url cannot be empty', got %v", err)
			}
		}
	})

	t.Run("schemeless host port normalized", func(t *testing.T) {
		tests := []struct {
			input string
			want  string
		}{
			{"localhost:8080", "http://localhost:8080"},
			{"localhost:8080/foo", "http://localhost:8080/foo"},
			{"localhost", "http://localhost"},
			{"127.0.0.1:8080", "http://127.0.0.1:8080"},
			{"127.0.0.1:8080/foo", "http://127.0.0.1:8080/foo"},
			{"[::1]:8080", "http://[::1]:8080"},
			{"custom-daemon:9090", "http://custom-daemon:9090"},
		}
		for _, tc := range tests {
			cfg, err := parseFlags([]string{"-url", tc.input})
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
			if cfg.url != tc.want {
				t.Fatalf("for %q expected %q, got %q", tc.input, tc.want, cfg.url)
			}
		}
	})

	t.Run("existing scheme preserved", func(t *testing.T) {
		tests := []struct {
			input string
			want  string
		}{
			{"http://localhost:8080", "http://localhost:8080"},
			{"https://localhost:8080", "https://localhost:8080"},
			{"http://custom:9090/prefix", "http://custom:9090/prefix"},
		}
		for _, tc := range tests {
			cfg, err := parseFlags([]string{"-url", tc.input})
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
			if cfg.url != tc.want {
				t.Fatalf("for %q expected %q, got %q", tc.input, tc.want, cfg.url)
			}
		}
	})

	t.Run("schemeless env vars normalized", func(t *testing.T) {
		t.Setenv("TASKD_URL", "localhost:8080")
		t.Setenv("T", "")
		cfg, err := parseFlags(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.url != "http://localhost:8080" {
			t.Fatalf("expected http://localhost:8080 from TASKD_URL, got %q", cfg.url)
		}

		t.Setenv("TASKD_URL", "")
		t.Setenv("T", "127.0.0.1:8080")
		cfg, err = parseFlags(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.url != "http://127.0.0.1:8080" {
			t.Fatalf("expected http://127.0.0.1:8080 from T, got %q", cfg.url)
		}
	})

	t.Run("unsupported scheme rejected", func(t *testing.T) {
		for _, raw := range []string{"ftp://localhost:8080", "ws://localhost:8080"} {
			_, err := parseFlags([]string{"-url", raw})
			if err == nil {
				t.Fatalf("expected error for unsupported scheme %q, got nil", raw)
			}
			if !strings.Contains(err.Error(), "unsupported protocol scheme") {
				t.Fatalf("expected error to mention unsupported protocol scheme for %q, got %v", raw, err)
			}
		}
	})

	t.Run("invalid url rejected", func(t *testing.T) {
		for _, raw := range []string{"http://[invalid:host", "http://"} {
			_, err := parseFlags([]string{"-url", raw})
			if err == nil {
				t.Fatalf("expected error for invalid url %q, got nil", raw)
			}
		}
	})
}
