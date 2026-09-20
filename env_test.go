package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFlagsEnvDefaults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "env.db")
	t.Setenv("TASKD_ADDR", ":9991")
	t.Setenv("TASKD_DB", dbPath)
	t.Setenv("TASKD_LEASE", "450")
	t.Setenv("TASKD_MAX_CLAIMS", "7")

	cfg, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.addr != ":9991" {
		t.Fatalf("expected addr :9991, got %q", cfg.addr)
	}
	if cfg.dbPath != dbPath {
		t.Fatalf("expected dbPath %q, got %q", dbPath, cfg.dbPath)
	}
	if cfg.lease != 450 {
		t.Fatalf("expected lease 450, got %d", cfg.lease)
	}
	if cfg.maxClaims != 7 {
		t.Fatalf("expected maxClaims 7, got %d", cfg.maxClaims)
	}
}

func TestParseFlagsFlagPrecedence(t *testing.T) {
	dbPathEnv := filepath.Join(t.TempDir(), "env.db")
	dbPathFlag := filepath.Join(t.TempDir(), "flag.db")
	t.Setenv("TASKD_ADDR", ":9991")
	t.Setenv("TASKD_DB", dbPathEnv)
	t.Setenv("TASKD_LEASE", "450")
	t.Setenv("TASKD_MAX_CLAIMS", "7")

	args := []string{
		"-addr", ":9992",
		"-db", dbPathFlag,
		"-lease", "600",
		"-max-claims", "3",
	}

	cfg, err := parseFlags(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.addr != ":9992" {
		t.Fatalf("expected addr :9992, got %q", cfg.addr)
	}
	if cfg.dbPath != dbPathFlag {
		t.Fatalf("expected dbPath %q, got %q", dbPathFlag, cfg.dbPath)
	}
	if cfg.lease != 600 {
		t.Fatalf("expected lease 600, got %d", cfg.lease)
	}
	if cfg.maxClaims != 3 {
		t.Fatalf("expected maxClaims 3, got %d", cfg.maxClaims)
	}
}

func TestParseFlagsEnvInvalid(t *testing.T) {
	t.Run("invalid lease", func(t *testing.T) {
		t.Setenv("TASKD_LEASE", "notanumber")
		if _, err := parseFlags(nil); err == nil {
			t.Fatal("expected error for invalid TASKD_LEASE, got nil")
		}
	})

	t.Run("invalid max claims", func(t *testing.T) {
		t.Setenv("TASKD_MAX_CLAIMS", "xyz")
		if _, err := parseFlags(nil); err == nil {
			t.Fatal("expected error for invalid TASKD_MAX_CLAIMS, got nil")
		}
	})

	t.Run("out of bounds lease", func(t *testing.T) {
		t.Setenv("TASKD_LEASE", "0")
		if _, err := parseFlags(nil); err == nil {
			t.Fatal("expected error for TASKD_LEASE <= 0, got nil")
		}
	})

	t.Run("negative max claims", func(t *testing.T) {
		t.Setenv("TASKD_MAX_CLAIMS", "-1")
		if _, err := parseFlags(nil); err == nil {
			t.Fatal("expected error for negative TASKD_MAX_CLAIMS, got nil")
		}
	})
}

func TestPrintUsageEnvironmentVariables(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	out := buf.String()

	required := []string{
		"Environment variables:",
		"TASKD_ADDR",
		"TASKD_DB",
		"TASKD_LEASE",
		"TASKD_MAX_CLAIMS",
	}
	for _, term := range required {
		if !strings.Contains(out, term) {
			t.Fatalf("expected usage text to contain %q, but was missing:\n%s", term, out)
		}
	}
}
