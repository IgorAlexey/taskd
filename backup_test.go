package main

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupUninitializedSource(t *testing.T) {
	dir := t.TempDir()
	emptyDB := filepath.Join(dir, "empty.db")
	backupDBPath := filepath.Join(dir, "backup.db")

	if err := os.WriteFile(emptyDB, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create empty db: %v", err)
	}

	var stderr bytes.Buffer
	err := run(io.Discard, []string{"-db", emptyDB, "-backup", backupDBPath})
	if err == nil {
		t.Fatal("expected error from run with uninitialized source db, got nil")
	}

	code := fatal(&stderr, err)
	if code == 0 {
		t.Fatalf("expected non-zero exit code from fatal, got %d", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("expected error message on stderr, got empty")
	}

	if _, err := os.Stat(backupDBPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected backup file not to exist, got err: %v", err)
	}
}

func TestBackupVerifyError(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "other.db")
	backupDBPath := filepath.Join(dir, "backup.db")

	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	if _, err := rawDB.Exec("CREATE TABLE other (id INTEGER PRIMARY KEY)"); err != nil {
		rawDB.Close()
		t.Fatalf("create table failed: %v", err)
	}
	rawDB.Close()

	roDB, err := openReadOnlyDB(dbPath)
	if err != nil {
		t.Fatalf("openReadOnlyDB failed: %v", err)
	}
	defer roDB.Close()

	if err := backupDB(roDB, backupDBPath, io.Discard); err == nil {
		t.Fatal("expected error from backupDB when verify fails, got nil")
	}

	if _, err := os.Stat(backupDBPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected failed backup file not to exist, got err: %v", err)
	}
}

func TestBackupStdout(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "source.db")
	backupPath := filepath.Join(dir, "backup.db")

	db, _, err := openDBInit(dbPath, 0)
	if err != nil {
		t.Fatalf("openDBInit failed: %v", err)
	}
	db.Close()

	var buf bytes.Buffer
	if err := run(&buf, []string{"-db", dbPath, "-backup", backupPath}); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	wantPrefix := fmt.Sprintf("wrote %s (", backupPath)
	if !strings.HasPrefix(buf.String(), wantPrefix) {
		t.Fatalf("expected buf to start with %q, got %q", wantPrefix, buf.String())
	}
	if !strings.Contains(buf.String(), "tasks)") {
		t.Fatalf("expected buf to contain 'tasks)', got %q", buf.String())
	}
}
