package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenDBFutureSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vfuture.db")
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO tasks (id, body, project) VALUES ('keep-1','written by current','p')"); err != nil {
		db.Close()
		t.Fatalf("seed insert: %v", err)
	}
	db.Close()

	future := schemaVersion + 1
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("setup open: %v", err)
	}
	if _, err := raw.Exec(fmt.Sprintf("PRAGMA journal_mode = DELETE; PRAGMA user_version = %d;", future)); err != nil {
		raw.Close()
		t.Fatalf("setup pragma: %v", err)
	}
	raw.Close()

	db2, err := openDB(dbPath)
	if err == nil {
		db2.Close()
		t.Fatalf("openDB accepted schema version %d, want an error", future)
	}
	msg := err.Error()
	for _, want := range []string{dbPath, fmt.Sprint(future), fmt.Sprint(schemaVersion)} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not name %q", msg, want)
		}
	}

	raw, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("verify open: %v", err)
	}
	defer raw.Close()
	var count int
	if err := raw.QueryRow("SELECT count(*) FROM tasks").Scan(&count); err != nil {
		t.Fatalf("verify count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 task left untouched, got %d", count)
	}
	var version int
	if err := raw.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("verify user_version: %v", err)
	}
	if version != future {
		t.Fatalf("expected user_version %d left untouched, got %d", future, version)
	}
	var mode string
	if err := raw.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("verify journal_mode: %v", err)
	}
	if mode != "delete" {
		t.Fatalf("expected journal_mode 'delete' left untouched, got %q", mode)
	}
}

func TestOpenDBNegativeSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vneg.db")
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("setup open: %v", err)
	}
	if _, err := raw.Exec("PRAGMA user_version = -1;"); err != nil {
		raw.Close()
		t.Fatalf("setup pragma: %v", err)
	}
	raw.Close()

	db, err := openDB(dbPath)
	if err == nil {
		db.Close()
		t.Fatal("openDB accepted schema version -1, want an error")
	}
	msg := err.Error()
	for _, want := range []string{dbPath, "-1", fmt.Sprint(schemaVersion)} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not name %q", msg, want)
		}
	}
}

func TestOpenDBCurrentSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "current.db")
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		db.Close()
		t.Fatalf("query user_version: %v", err)
	}
	db.Close()
	if version != schemaVersion {
		t.Fatalf("fresh db has user_version %d, want schemaVersion %d", version, schemaVersion)
	}
	db2, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("reopen at schemaVersion failed: %v", err)
	}
	defer db2.Close()
	var mode string
	if err := db2.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("expected journal_mode 'wal', got %q", mode)
	}
}
