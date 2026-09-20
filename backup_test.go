package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupCreatesParentDir(t *testing.T) {
	srcPath := filepath.Join(t.TempDir(), "source.db")
	db, err := openDB(srcPath)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("INSERT INTO tasks (id, body, project) VALUES ('t1', 'test body', 'p1')"); err != nil {
		t.Fatalf("insert task failed: %v", err)
	}

	backupDir := filepath.Join(t.TempDir(), "deeply", "nested", "backup", "dir")
	backupPath := filepath.Join(backupDir, "backup.db")

	if _, err := os.Stat(backupDir); !os.IsNotExist(err) {
		t.Fatalf("expected backupDir %q to not exist initially", backupDir)
	}

	if err := backupDB(db, backupPath); err != nil {
		t.Fatalf("backupDB failed: %v", err)
	}

	if fi, err := os.Stat(backupDir); err != nil || !fi.IsDir() {
		t.Fatalf("expected backupDir %q to be created as directory: %v", backupDir, err)
	}

	bkDB, err := openDB(backupPath)
	if err != nil {
		t.Fatalf("open backup db failed: %v", err)
	}
	defer bkDB.Close()

	var count int
	if err := bkDB.QueryRow("SELECT count(*) FROM tasks WHERE id = 't1'").Scan(&count); err != nil {
		t.Fatalf("query backup tasks failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 task in backup, got %d", count)
	}
}

func TestRunBackupMissingSource(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.db")
	dest := filepath.Join(dir, "b.db")

	err := run([]string{"-db", missing, "-backup", dest})
	if err == nil {
		t.Fatal("expected -backup of a missing database to fail")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("error %q does not name the database path %q", err, missing)
	}
	for _, p := range []string{missing, dest} {
		if _, statErr := os.Stat(p); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("expected %q to stay absent, stat err %v", p, statErr)
		}
	}
}

func TestRunBackupExistingSource(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "real.db")
	db, err := openDB(srcPath)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO tasks (id, body, project) VALUES ('t1', 'b', 'p1')"); err != nil {
		t.Fatalf("insert task failed: %v", err)
	}
	db.Close()

	if err := os.Link(srcPath, filepath.Join(dir, "my db.db")); err != nil {
		t.Fatalf("link failed: %v", err)
	}
	t.Chdir(dir)

	for i, dbArg := range []string{srcPath, "file:" + srcPath, "file:my%20db.db"} {
		dest := filepath.Join(dir, fmt.Sprintf("ok-%d.db", i))
		if err := run([]string{"-db", dbArg, "-backup", dest}); err != nil {
			t.Fatalf("run -db %q -backup: %v", dbArg, err)
		}
		bkDB, err := openDB(dest)
		if err != nil {
			t.Fatalf("open backup of %q failed: %v", dbArg, err)
		}
		var count int
		err = bkDB.QueryRow("SELECT count(*) FROM tasks WHERE id = 't1'").Scan(&count)
		bkDB.Close()
		if err != nil {
			t.Fatalf("query backup of %q failed: %v", dbArg, err)
		}
		if count != 1 {
			t.Fatalf("backup of %q has %d rows, want 1", dbArg, count)
		}
	}
}

func TestCheckBackupSourceUnparseable(t *testing.T) {
	for _, path := range []string{"file:%zz", "file:bad%2"} {
		err := checkBackupSource(path)
		if err == nil {
			t.Fatalf("expected %q to be refused", path)
		}
		if strings.Contains(err.Error(), "in-memory") {
			t.Fatalf("error for %q blames in-memory: %v", path, err)
		}
	}
}

func TestCheckBackupSourceMemory(t *testing.T) {
	for _, path := range []string{":memory:", "file::memory:", "file:x.db?mode=memory"} {
		if err := checkBackupSource(path); err == nil {
			t.Fatalf("expected -backup of %q to be refused", path)
		}
	}
}
