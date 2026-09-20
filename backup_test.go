package main

import (
	"bytes"
	"database/sql"
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

func TestRunBackupDoesNotMigrateOrMutateSource(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.db")
	backupPath := filepath.Join(dir, "out.db")

	db, err := openDB(srcPath)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO tasks (id, body, project) VALUES ('t1', 'hello', 'p1')"); err != nil {
		db.Close()
		t.Fatalf("insert task failed: %v", err)
	}
	if _, err := db.Exec("PRAGMA user_version = 2"); err != nil {
		db.Close()
		t.Fatalf("set user_version failed: %v", err)
	}
	db.Close()

	beforeBytes, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read src.db before backup: %v", err)
	}

	if err := run([]string{"-db", srcPath, "-backup", backupPath}); err != nil {
		t.Fatalf("run -backup failed: %v", err)
	}

	afterBytes, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read src.db after backup: %v", err)
	}

	if !bytes.Equal(beforeBytes, afterBytes) {
		t.Fatalf("src.db was mutated by -backup (size %d -> %d)", len(beforeBytes), len(afterBytes))
	}

	srcCheck, err := sql.Open("sqlite", "file:"+srcPath+"?mode=ro")
	if err != nil {
		t.Fatalf("open src.db: %v", err)
	}
	defer srcCheck.Close()
	var srcVer int
	if err := srcCheck.QueryRow("PRAGMA user_version").Scan(&srcVer); err != nil {
		t.Fatalf("query src.db user_version: %v", err)
	}
	if srcVer != 2 {
		t.Fatalf("src.db user_version = %d, want 2", srcVer)
	}

	outDB, err := sql.Open("sqlite", "file:"+backupPath+"?mode=ro")
	if err != nil {
		t.Fatalf("open out.db: %v", err)
	}
	defer outDB.Close()

	var integrity string
	if err := outDB.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		t.Fatalf("query out.db integrity_check: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("out.db integrity_check = %q, want ok", integrity)
	}

	var outVer int
	if err := outDB.QueryRow("PRAGMA user_version").Scan(&outVer); err != nil {
		t.Fatalf("query out.db user_version: %v", err)
	}
	if outVer != 2 {
		t.Fatalf("out.db user_version = %d, want 2", outVer)
	}

	var srcCount, outCount int
	if err := srcCheck.QueryRow("SELECT count(*) FROM tasks").Scan(&srcCount); err != nil {
		t.Fatalf("query src.db count: %v", err)
	}
	if err := outDB.QueryRow("SELECT count(*) FROM tasks").Scan(&outCount); err != nil {
		t.Fatalf("query out.db count: %v", err)
	}
	if outCount != srcCount {
		t.Fatalf("out.db row count = %d, want %d", outCount, srcCount)
	}
}
