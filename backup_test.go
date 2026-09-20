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

func TestBackupCreatesParentDir(t *testing.T) {
	srcPath := filepath.Join(t.TempDir(), "source.db")
	db, err := openDB(srcPath)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	if _, err := db.rw.Exec("INSERT INTO tasks (id, body, project) VALUES ('t1', 'test body', 'p1')"); err != nil {
		t.Fatalf("insert task failed: %v", err)
	}

	backupDir := filepath.Join(t.TempDir(), "deeply", "nested", "backup", "dir")
	backupPath := filepath.Join(backupDir, "backup.db")

	if _, err := os.Stat(backupDir); !os.IsNotExist(err) {
		t.Fatalf("expected backupDir %q to not exist initially", backupDir)
	}

	if err := backupDB(db.rw, backupPath, io.Discard); err != nil {
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
	if err := bkDB.rw.QueryRow("SELECT count(*) FROM tasks WHERE id = 't1'").Scan(&count); err != nil {
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
	if _, err := db.rw.Exec("INSERT INTO tasks (id, body, project) VALUES ('t1', 'b', 'p1')"); err != nil {
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
		err = bkDB.rw.QueryRow("SELECT count(*) FROM tasks WHERE id = 't1'").Scan(&count)
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
	if _, err := db.rw.Exec("INSERT INTO tasks (id, body, project) VALUES ('t1', 'hello', 'p1')"); err != nil {
		db.Close()
		t.Fatalf("insert task failed: %v", err)
	}
	if _, err := db.rw.Exec("PRAGMA user_version = 2"); err != nil {
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

func TestBackupOverwrite(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "test.db")
	backupPath := filepath.Join(dir, "backup.db")

	db, err := openDB(srcPath)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	if _, err := db.rw.Exec("INSERT INTO tasks (id, body, project) VALUES ('t1', 'first', 'p1')"); err != nil {
		db.Close()
		t.Fatalf("insert task failed: %v", err)
	}
	db.Close()

	if err := run([]string{"-db", srcPath, "-backup", backupPath}); err != nil {
		t.Fatalf("first backup failed: %v", err)
	}

	if err := os.Chmod(backupPath, 0600); err != nil {
		t.Fatalf("chmod backup failed: %v", err)
	}

	db, err = openDB(srcPath)
	if err != nil {
		t.Fatalf("openDB second time failed: %v", err)
	}
	if _, err := db.rw.Exec("INSERT INTO tasks (id, body, project) VALUES ('t2', 'second', 'p1')"); err != nil {
		db.Close()
		t.Fatalf("insert second task failed: %v", err)
	}
	db.Close()

	if err := run([]string{"-db", srcPath, "-backup", backupPath}); err != nil {
		t.Fatalf("second backup failed: %v", err)
	}

	fi, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("stat backup failed: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("expected permissions 0600, got %#o", fi.Mode().Perm())
	}

	bkDB, err := openDB(backupPath)
	if err != nil {
		t.Fatalf("open overwritten backup failed: %v", err)
	}
	defer bkDB.Close()

	var count int
	if err := bkDB.rw.QueryRow("SELECT count(*) FROM tasks").Scan(&count); err != nil {
		t.Fatalf("query overwritten backup failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 tasks in overwritten backup, got %d", count)
	}

	var integrity string
	if err := bkDB.rw.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		t.Fatalf("query integrity_check failed: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("expected integrity_check 'ok', got %q", integrity)
	}
}

func seedBackupSource(t *testing.T, path string, ids ...string) {
	t.Helper()
	db, err := openDB(path)
	if err != nil {
		t.Fatalf("openDB %q failed: %v", path, err)
	}
	defer db.Close()
	for _, id := range ids {
		if _, err := db.rw.Exec("INSERT INTO tasks (id, body, project) VALUES (?, 'body', 'p1')", id); err != nil {
			t.Fatalf("insert %q failed: %v", id, err)
		}
	}
}

func TestRunBackupRefusesSourceFile(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "x.db")
	seedBackupSource(t, srcPath, "t1")

	linkPath := filepath.Join(dir, "link.db")
	if err := os.Symlink(srcPath, linkPath); err != nil {
		t.Fatalf("symlink failed: %v", err)
	}
	hardPath := filepath.Join(dir, "hard.db")
	if err := os.Link(srcPath, hardPath); err != nil {
		t.Fatalf("hardlink failed: %v", err)
	}

	before, err := os.Stat(srcPath)
	if err != nil {
		t.Fatalf("stat source failed: %v", err)
	}

	dests := []string{
		srcPath,
		filepath.Dir(srcPath) + "/./" + filepath.Base(srcPath),
		linkPath,
		hardPath,
		"file:" + srcPath,
	}
	for _, dest := range dests {
		err := run([]string{"-db", srcPath, "-backup", dest})
		if err == nil {
			t.Fatalf("expected -backup %q to be refused", dest)
		}
		if !strings.Contains(err.Error(), "backup destination is the source database") {
			t.Fatalf("error for %q does not name the collision: %v", dest, err)
		}
		after, statErr := os.Stat(srcPath)
		if statErr != nil {
			t.Fatalf("stat source after %q failed: %v", dest, statErr)
		}
		if !os.SameFile(before, after) {
			t.Fatalf("-backup %q replaced the source database", dest)
		}
	}

	db, err := openDB(srcPath)
	if err != nil {
		t.Fatalf("reopen source failed: %v", err)
	}
	defer db.Close()
	if err := backupDB(db.rw, srcPath, io.Discard); err == nil {
		t.Fatal("expected backupDB onto its own source to be refused")
	}
	var count int
	if err := db.rw.QueryRow("SELECT count(*) FROM tasks").Scan(&count); err != nil {
		t.Fatalf("query source failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("source has %d rows, want 1", count)
	}
}

func TestBackupReportsDestination(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.db")
	seedBackupSource(t, srcPath, "t1", "t2", "t3")

	db, err := openDB(srcPath)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	dest := filepath.Join(dir, "out.db")
	var out bytes.Buffer
	if err := backupDB(db.rw, dest, &out); err != nil {
		t.Fatalf("backupDB failed: %v", err)
	}

	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat backup failed: %v", err)
	}
	want := fmt.Sprintf("wrote %s (%d bytes, 3 tasks)\n", dest, fi.Size())
	if out.String() != want {
		t.Fatalf("backup report = %q, want %q", out.String(), want)
	}
}

func TestBackupSurvivesReportFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.db")
	seedBackupSource(t, srcPath, "t1")

	dest := filepath.Join(dir, "out.db")
	if err := os.WriteFile(dest, nil, 0000); err != nil {
		t.Fatalf("create unreadable destination failed: %v", err)
	}

	if err := run([]string{"-db", srcPath, "-backup", dest}); err != nil {
		t.Fatalf("backup failed because its report could not be produced: %v", err)
	}

	if err := os.Chmod(dest, 0600); err != nil {
		t.Fatalf("chmod backup failed: %v", err)
	}
	bkDB, err := openDB(dest)
	if err != nil {
		t.Fatalf("open backup failed: %v", err)
	}
	defer bkDB.Close()
	var count int
	if err := bkDB.rw.QueryRow("SELECT count(*) FROM tasks").Scan(&count); err != nil {
		t.Fatalf("query backup failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("backup has %d rows, want 1", count)
	}
}
