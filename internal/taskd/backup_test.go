package taskd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
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

func TestBackupDestinationDirectory(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := openDB(dbPath, 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	db.Close()

	destDir := filepath.Join(dir, "backup-dir")
	if err := os.Mkdir(destDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	err = run(io.Discard, []string{"-db", dbPath, "-backup", destDir})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	wantErr := "backup destination is a directory: " + destDir
	if err.Error() != wantErr {
		t.Fatalf("expected error %q, got %q", wantErr, err.Error())
	}

	if err := backupTo(dbPath, destDir, io.Discard); err == nil || err.Error() != wantErr {
		t.Fatalf("backupTo expected %q, got %v", wantErr, err)
	}
}
