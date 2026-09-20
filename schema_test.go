package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
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
	if _, err := db.rw.Exec("INSERT INTO tasks (id, body, project) VALUES ('keep-1','written by current','p')"); err != nil {
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
	if err := db.rw.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
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
	if err := db2.rw.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("expected journal_mode 'wal', got %q", mode)
	}
}

func TestOpenDBRefusesForeignFile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "foreign.db")
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("setup open: %v", err)
	}
	if _, err := raw.Exec("CREATE TABLE notes(id INTEGER PRIMARY KEY, txt TEXT); INSERT INTO notes VALUES(1,'my important data');"); err != nil {
		raw.Close()
		t.Fatalf("setup exec: %v", err)
	}
	raw.Close()

	before, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	db, err := openDB(dbPath)
	if err == nil {
		db.Close()
		t.Fatal("openDB adopted a foreign database, want an error")
	}
	if !strings.Contains(err.Error(), dbPath) {
		t.Fatalf("error %q does not name %q", err, dbPath)
	}

	after, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("openDB wrote to a foreign database before refusing it")
	}
}

func TestOpenDBRefusesMissingTasksTable(t *testing.T) {
	for version := 1; version <= schemaVersion; version++ {
		dbPath := filepath.Join(t.TempDir(), fmt.Sprintf("v%d.db", version))
		raw, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("setup open: %v", err)
		}
		if _, err := raw.Exec(fmt.Sprintf("PRAGMA user_version = %d;", version)); err != nil {
			raw.Close()
			t.Fatalf("setup pragma: %v", err)
		}
		raw.Close()

		db, err := openDB(dbPath)
		if err == nil {
			db.Close()
			t.Fatalf("openDB accepted version %d without a tasks table, want an error", version)
		}
		msg := err.Error()
		for _, want := range []string{dbPath, fmt.Sprint(version)} {
			if !strings.Contains(msg, want) {
				t.Fatalf("error %q does not name %q", msg, want)
			}
		}
	}
}

func TestOpenDBAdoptsLegacyUnversioned(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("setup open: %v", err)
	}
	if _, err := raw.Exec("CREATE TABLE tasks (id TEXT PRIMARY KEY, asset_path TEXT NOT NULL DEFAULT '', status TEXT DEFAULT 'pending', worker TEXT, lease_expires INTEGER, primitives JSON);"); err != nil {
		raw.Close()
		t.Fatalf("setup exec: %v", err)
	}
	raw.Close()

	db, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("openDB refused a legacy unversioned taskd database: %v", err)
	}
	defer db.Close()

	var version int
	if err := db.rw.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("query user_version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("legacy db migrated to %d, want %d", version, schemaVersion)
	}
}

func TestClaimIndexUsedBySweep(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*sql.DB) error
	}{
		{"fresh", func(*sql.DB) error { return nil }},
		{"migrated", func(raw *sql.DB) error {
			_, err := raw.Exec("CREATE TABLE tasks (id TEXT PRIMARY KEY, asset_path TEXT NOT NULL DEFAULT '', status TEXT DEFAULT 'pending', worker TEXT, lease_expires INTEGER, primitives JSON);")
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "t.db")
			raw, err := sql.Open("sqlite", dbPath)
			if err != nil {
				t.Fatalf("setup open: %v", err)
			}
			if err := tc.setup(raw); err != nil {
				raw.Close()
				t.Fatalf("setup: %v", err)
			}
			raw.Close()

			db, err := openDB(dbPath)
			if err != nil {
				t.Fatalf("openDB failed: %v", err)
			}
			defer db.Close()

			rows, err := db.rw.Query("EXPLAIN QUERY PLAN "+buryExhaustedSQL+" RETURNING id", 2)
			if err != nil {
				t.Fatalf("query plan: %v", err)
			}
			defer rows.Close()
			var plan []string
			for rows.Next() {
				var detail string
				if err := rows.Scan(new(int), new(int), new(int), &detail); err != nil {
					t.Fatalf("scan plan: %v", err)
				}
				plan = append(plan, detail)
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("plan rows: %v", err)
			}
			joined := strings.Join(plan, " | ")
			if !strings.Contains(joined, "idx_tasks_claim_count") {
				t.Fatalf("sweep does not use idx_tasks_claim_count: %s", joined)
			}
			if strings.Contains(joined, "SCAN tasks") {
				t.Fatalf("sweep scans the table: %s", joined)
			}
		})
	}
}
