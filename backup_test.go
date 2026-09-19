package main

import (
	"os"
	"path/filepath"
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
