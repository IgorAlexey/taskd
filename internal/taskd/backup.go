package taskd

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// backupTo copies the database at dbPath to dest with VACUUM INTO and reports
// what it wrote on out.
func backupTo(dbPath, dest string, out io.Writer) error {
	if err := checkBackupSource(dbPath); err != nil {
		return err
	}
	db, err := openReadOnlyDB(dbPath)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	var dummy int
	if err := db.QueryRow("SELECT 1 FROM tasks LIMIT 1").Scan(&dummy); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("database is uninitialized: %s: %w", dbPath, err)
	}
	return backupDB(db, dest, out)
}

func checkBackupSource(path string) error {
	fsPath, memory := resolveDBPath(path)
	if memory {
		return fmt.Errorf("cannot back up an in-memory database: %s", path)
	}
	if fsPath == "" {
		return fmt.Errorf("cannot resolve database path: %s", path)
	}
	fi, err := os.Stat(fsPath)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("database does not exist: %s", fsPath)
	} else if err != nil {
		return fmt.Errorf("cannot access database %s: %w", fsPath, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("database is a directory: %s", fsPath)
	}
	return nil
}

func countTasks(path string) (int, error) {
	bk, err := openDBConn(path, true)
	if err != nil {
		return 0, err
	}
	bk.SetMaxOpenConns(1)
	defer bk.Close()
	var tasks int
	if err := bk.QueryRow("SELECT count(*) FROM tasks").Scan(&tasks); err != nil {
		return 0, err
	}
	return tasks, nil
}

func backupDB(db *sql.DB, path string, out io.Writer) error {
	fsPath, memory := resolveDBPath(path)
	if memory {
		return fmt.Errorf("cannot back up to an in-memory database: %s", path)
	}
	if fsPath == "" {
		fsPath = path
	}
	destInfo, statErr := os.Stat(fsPath)
	if statErr == nil {
		if destInfo.IsDir() {
			return fmt.Errorf("backup destination is a directory: %s", fsPath)
		}
		if err := checkNotSource(db, fsPath, destInfo); err != nil {
			return err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("cannot access backup destination %s: %w", fsPath, statErr)
	}
	dir := filepath.Dir(fsPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	} else {
		dir = "."
	}
	tmpDir, err := os.MkdirTemp(dir, ".taskd-backup-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()
	tmpPath := filepath.Join(tmpDir, filepath.Base(fsPath))
	if _, err := db.Exec("VACUUM INTO ?", tmpPath); err != nil {
		return err
	}
	if destInfo != nil {
		if err := os.Chmod(tmpPath, destInfo.Mode().Perm()); err != nil {
			return err
		}
	}
	tasks, err := countTasks(tmpPath)
	if err != nil {
		return fmt.Errorf("cannot verify backup: %w", err)
	}
	fi, err := os.Stat(tmpPath)
	if err != nil {
		return fmt.Errorf("cannot verify backup: %w", err)
	}
	if err := os.Rename(tmpPath, fsPath); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "wrote %s (%d bytes, %d tasks)\n", fsPath, fi.Size(), tasks)
	return nil
}

func checkNotSource(db *sql.DB, destPath string, destInfo os.FileInfo) error {
	var srcPath string
	if err := db.QueryRow("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&srcPath); err != nil {
		return fmt.Errorf("cannot locate the source database: %w", err)
	}
	if srcPath == "" {
		return nil
	}
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("cannot verify backup destination %s: %w", destPath, err)
	}
	if os.SameFile(srcInfo, destInfo) {
		return fmt.Errorf("backup destination is the source database: %s", destPath)
	}
	return nil
}

func openReadOnlyDB(path string) (*sql.DB, error) {
	db, err := openDBConn(path, true)
	if err != nil {
		return nil, fmt.Errorf("cannot open database %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("cannot open database %s: %w", path, err)
	}
	return db, nil
}
