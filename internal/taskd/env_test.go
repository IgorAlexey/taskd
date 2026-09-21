package taskd

import (
	"path/filepath"
	"testing"
)

func TestParseFlagsEnvDefaults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "env.db")
	t.Setenv("TASKD_ADDR", ":9991")
	t.Setenv("TASKD_DB", dbPath)
	t.Setenv("TASKD_LEASE", "450")
	t.Setenv("TASKD_MAX_CLAIMS", "7")
	t.Setenv("TASKD_CORS_ORIGIN", "https://example.com")
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
	if cfg.corsOrigin != "https://example.com" {
		t.Fatalf("expected corsOrigin %q, got %q", "https://example.com", cfg.corsOrigin)
	}
}

func TestParseFlagsFlagPrecedence(t *testing.T) {
	dbPathEnv := filepath.Join(t.TempDir(), "env.db")
	dbPathFlag := filepath.Join(t.TempDir(), "flag.db")
	t.Setenv("TASKD_ADDR", ":9991")
	t.Setenv("TASKD_DB", dbPathEnv)
	t.Setenv("TASKD_LEASE", "450")
	t.Setenv("TASKD_MAX_CLAIMS", "7")
	t.Setenv("TASKD_CORS_ORIGIN", "https://env.example.com")
	args := []string{
		"-addr", ":9992",
		"-db", dbPathFlag,
		"-lease", "600",
		"-max-claims", "3",
		"-cors-origin", "https://flag.example.com",
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
	if cfg.corsOrigin != "https://flag.example.com" {
		t.Fatalf("expected corsOrigin %q, got %q", "https://flag.example.com", cfg.corsOrigin)
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
