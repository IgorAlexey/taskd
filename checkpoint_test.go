package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJournalSizeLimitPragma(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "jsl.db")
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	var rwLimit int64
	if err := db.rw.QueryRow("PRAGMA journal_size_limit").Scan(&rwLimit); err != nil {
		t.Fatalf("PRAGMA journal_size_limit on rw failed: %v", err)
	}
	if rwLimit <= 0 {
		t.Fatalf("expected positive journal_size_limit on rw, got %d", rwLimit)
	}
	if rwLimit != walJournalSizeLimit {
		t.Fatalf("expected journal_size_limit %d, got %d", walJournalSizeLimit, rwLimit)
	}

	var roLimit int64
	if err := db.ro.QueryRow("PRAGMA journal_size_limit").Scan(&roLimit); err != nil {
		t.Fatalf("PRAGMA journal_size_limit on ro failed: %v", err)
	}
	if roLimit <= 0 {
		t.Fatalf("expected positive journal_size_limit on ro, got %d", roLimit)
	}
}

func TestCheckpointTickerFiresAndShrinksWAL(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ckpt.db")
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	walPath := dbPath + "-wal"
	tx, err := db.rw.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	stmt, err := tx.Prepare("INSERT INTO tasks (id, body, project) VALUES (?, ?, 'test')")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	for i := 0; i < 200; i++ {
		if _, err := stmt.Exec(fmt.Sprintf("%032x", i), "payload body string padding to occupy WAL pages"); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	fiBefore, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("stat wal before: %v", err)
	}
	if fiBefore.Size() == 0 {
		t.Fatalf("expected non-empty WAL before checkpoint, got 0")
	}

	fired := make(chan struct{}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go runCheckpointer(ctx, db.rw, 20*time.Millisecond, func() {
		select {
		case fired <- struct{}{}:
		default:
		}
	})

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("checkpoint ticker did not fire within timeout")
	}

	fiAfter, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("stat wal after: %v", err)
	}
	if fiAfter.Size() >= fiBefore.Size() {
		t.Fatalf("expected WAL to shrink after checkpoint: before=%d after=%d", fiBefore.Size(), fiAfter.Size())
	}
}

func TestCheckpointMemoryDB(t *testing.T) {
	db, err := openDB(":memory:")
	if err != nil {
		t.Fatalf("openDB :memory: failed: %v", err)
	}
	defer db.Close()

	if err := checkpointWAL(context.Background(), db.rw); err != nil {
		t.Fatalf("checkpointWAL on :memory: failed: %v", err)
	}
}

func TestRunServerCheckpointsOnShutdown(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "shutdown_ckpt.db")
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	tx, err := db.rw.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	stmt, err := tx.Prepare("INSERT INTO tasks (id, body, project) VALUES (?, ?, 'test')")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	for i := 0; i < 200; i++ {
		if _, err := stmt.Exec(fmt.Sprintf("%032x", i), "body content to generate WAL data"); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	walPath := dbPath + "-wal"
	fiBefore, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("stat wal: %v", err)
	}
	if fiBefore.Size() == 0 {
		t.Fatalf("expected WAL to have data before shutdown")
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runServer(ctx, l, db, 300, 0, "")
	}()

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runServer returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runServer did not shut down in time")
	}

	fiAfter, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("stat wal after shutdown: %v", err)
	}
	if fiAfter.Size() >= fiBefore.Size() {
		t.Fatalf("expected WAL to shrink on shutdown: before=%d after=%d", fiBefore.Size(), fiAfter.Size())
	}
}

func TestRunServerCheckpointerStopsOnEarlyError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "early_err.db")
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_ = l.Close()

	ctx := context.Background()
	err = runServer(ctx, l, db, 300, 0, "")
	if err == nil {
		t.Fatal("expected runServer to error on closed listener")
	}
}
