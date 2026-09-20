package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartupLogNewAndExisting(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	var buf bytes.Buffer
	origWriter := log.Writer()
	origFlags := log.Flags()
	defer func() {
		log.SetOutput(origWriter)
		log.SetFlags(origFlags)
	}()
	log.SetOutput(&buf)
	log.SetFlags(0)

	db1, isNew1, err := openDBInit(dbPath, 0)
	if err != nil {
		t.Fatalf("failed to open new db: %v", err)
	}
	if !isNew1 {
		t.Fatal("expected isNew = true for fresh database")
	}
	logStartupDB(dbPath, 300, isNew1)
	db1.Close()

	out := buf.String()
	wantCreated := fmt.Sprintf("created new database %s (lease 300s)", dbPath)
	if !strings.Contains(out, wantCreated) {
		t.Fatalf("expected log %q, got %q", wantCreated, out)
	}

	buf.Reset()
	db2, isNew2, err := openDBInit(dbPath, 0)
	if err != nil {
		t.Fatalf("failed to open existing db: %v", err)
	}
	if isNew2 {
		t.Fatal("expected isNew = false for existing database")
	}
	logStartupDB(dbPath, 300, isNew2)
	db2.Close()

	out2 := buf.String()
	wantOpened := fmt.Sprintf("opened database %s (lease 300s)", dbPath)
	if !strings.Contains(out2, wantOpened) {
		t.Fatalf("expected log %q, got %q", wantOpened, out2)
	}
}

func TestStartupListenerBindsBeforeDBCreated(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	busyAddr := ln.Addr().String()
	dir := t.TempDir()
	strayDB := filepath.Join(dir, "stray.db")

	err = run(os.Stdout, []string{"-addr", busyAddr, "-db", strayDB})
	if err == nil {
		t.Fatal("expected run to fail when port is busy, got nil error")
	}

	if _, err := os.Stat(strayDB); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database file %s must not be created when listener fails to bind", strayDB)
	}
}
