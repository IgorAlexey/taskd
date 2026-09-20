package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWALCheckpointOnClose(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := openDB(dbPath, 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})

	_, err = s.rw.Exec("INSERT INTO tasks (id, body, priority, project, created_at) VALUES (?, ?, ?, ?, unixepoch())", "test-id", "wal test task", 3, "proj")
	if err != nil {
		t.Fatalf("insert task failed: %v", err)
	}

	walPath := dbPath + "-wal"
	info, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("stat WAL failed: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("expected non-empty WAL before close, got 0 bytes")
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	info, err = os.Stat(walPath)
	if err == nil && info.Size() > 0 {
		t.Fatalf("expected WAL file to be truncated or removed on Close, got size %d", info.Size())
	}
}
