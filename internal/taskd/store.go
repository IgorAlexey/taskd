package taskd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

func resolveDBPath(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	if path == ":memory:" {
		return "", true
	}
	if !strings.HasPrefix(path, "file:") {
		return path, false
	}
	u, err := url.Parse(path)
	if err != nil {
		return "", false
	}
	if u.Query().Get("mode") == "memory" {
		return "", true
	}
	p := u.Path
	if p == "" {
		unescaped, err := url.PathUnescape(u.Opaque)
		if err != nil {
			return "", false
		}
		p = unescaped
	}
	if p == ":memory:" {
		return "", true
	}
	return p, false
}

func dbDir(path string) string {
	p, _ := resolveDBPath(path)
	if p == "" {
		return ""
	}
	return filepath.Dir(p)
}

func rowExists(db *sql.DB, query string) (bool, error) {
	var dummy int
	err := db.QueryRow(query).Scan(&dummy)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func openDBConn(path string, readOnly bool) (*sql.DB, error) {
	if !readOnly {
		if dir := dbDir(path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, err
			}
		}
	}
	var u *url.URL
	if strings.HasPrefix(path, "file:") {
		var err error
		u, err = url.Parse(path)
		if err != nil {
			return nil, err
		}
	} else if path == ":memory:" {
		u = &url.URL{Scheme: "file", Opaque: ":memory:"}
	} else {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		u = &url.URL{Scheme: "file", Path: abs}
	}
	q := u.Query()
	if readOnly {
		q.Set("mode", "ro")
	}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", fmt.Sprintf("journal_size_limit(%d)", walJournalSizeLimit))
	q.Add("_pragma", "foreign_keys(1)")
	u.RawQuery = q.Encode()
	return sql.Open("sqlite", u.String())
}

const (
	maxReaders                = 8
	walJournalSizeLimit       = 67108864
	defaultCheckpointInterval = 2 * time.Second
	defaultSweepInterval      = 1 * time.Second
)

type claimWaiter struct {
	project string
	ch      chan string
}

type store struct {
	rw          *sql.DB
	ro          *sql.DB
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	maxClaims   int
	waitersMu   sync.Mutex
	waiters     []*claimWaiter
	projectSeq  map[string]uint64
	wildcardSeq uint64
	closed      bool
	events      *broadcaster
}

func (s *store) currentSeq(project string) uint64 {
	if s == nil {
		return 0
	}
	s.waitersMu.Lock()
	defer s.waitersMu.Unlock()
	if project == "*" {
		return s.wildcardSeq
	}
	return s.projectSeq[project]
}

func (s *store) hasChangedSeqLocked(project string, seq uint64) bool {
	if project == "*" {
		return s.wildcardSeq != seq
	}
	return s.projectSeq[project] != seq
}

func (s *store) notifyPending(project string) {
	if s == nil || project == "" {
		return
	}
	s.waitersMu.Lock()
	defer s.waitersMu.Unlock()
	if s.projectSeq == nil {
		s.projectSeq = make(map[string]uint64)
	}
	s.projectSeq[project]++
	s.wildcardSeq++
	s.notifyPendingLocked(project)
}

func (s *store) notifyPendingLocked(project string) {
	if s.closed || project == "" {
		return
	}
	// the first waiter on this project, else the first on any project
	for _, want := range []string{project, "*"} {
		for i, w := range s.waiters {
			if w.project == want {
				s.waiters = slices.Delete(s.waiters, i, i+1)
				select {
				case w.ch <- project:
				default:
				}
				return
			}
		}
	}
}

func (s *store) removeWaiter(target *claimWaiter) {
	if s == nil {
		return
	}
	s.waitersMu.Lock()
	defer s.waitersMu.Unlock()
	if i := slices.Index(s.waiters, target); i >= 0 {
		s.waiters = slices.Delete(s.waiters, i, i+1)
	}
}

func (s *store) waiterCount() int {
	if s == nil {
		return 0
	}
	s.waitersMu.Lock()
	defer s.waitersMu.Unlock()
	return len(s.waiters)
}

var (
	errStoreClosed = errors.New("database closed")
	errWaitTimeout = errors.New("wait timed out")
)

// waitPending blocks until a task may have become pending in project since
// seq was read (nil: try the claim again), or the deadline passes
// (errWaitTimeout), or the context ends, or the store closes (errStoreClosed).
func (s *store) waitPending(ctx context.Context, project string, seq uint64, deadline <-chan time.Time) error {
	cw := &claimWaiter{project: project, ch: make(chan string, 1)}
	s.waitersMu.Lock()
	if s.closed {
		s.waitersMu.Unlock()
		return errStoreClosed
	}
	if s.hasChangedSeqLocked(project, seq) {
		s.waitersMu.Unlock()
		return nil
	}
	s.waiters = append(s.waiters, cw)
	s.waitersMu.Unlock()

	// a wake-up that arrives as we leave is handed to the next waiter
	handOff := func() {
		s.removeWaiter(cw)
		select {
		case p, ok := <-cw.ch:
			if ok {
				s.notifyPending(p)
			}
		default:
		}
	}
	select {
	case _, ok := <-cw.ch:
		if !ok { // Close closed it
			return errStoreClosed
		}
		return nil
	case <-deadline:
		handOff()
		return errWaitTimeout
	case <-ctx.Done():
		handOff()
		return ctx.Err()
	}
}

func (s *store) Close() error {
	if s.cancel != nil {
		s.cancel()
		s.wg.Wait()
	}
	s.waitersMu.Lock()
	s.closed = true
	for i, w := range s.waiters {
		close(w.ch)
		s.waiters[i] = nil
	}
	s.waiters = nil
	s.waitersMu.Unlock()
	var errs []error
	if s.ro != s.rw {
		if err := s.ro.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if err := checkpointWAL(context.Background(), s.rw); err != nil {
		errs = append(errs, err)
	}
	if err := s.rw.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (s *store) sweep() ([]string, error) {
	return sweepExpired(s.rw, s.maxClaims)
}

func openDB(path string, maxClaims int) (*store, error) {
	s, _, err := openDBInit(path, maxClaims)
	return s, err
}

func openDBInit(path string, maxClaims int) (*store, bool, error) {
	fail := func(err error) (*store, bool, error) {
		return nil, false, fmt.Errorf("cannot open database %s: %w", path, err)
	}
	rw, isNew, err := openRW(path)
	if err != nil {
		return fail(err)
	}
	var ro *sql.DB
	if _, memory := resolveDBPath(path); memory {
		ro = rw
	} else {
		ro, err = openDBConn(path, true)
		if err != nil {
			rw.Close()
			return fail(err)
		}
		ro.SetMaxOpenConns(maxReaders)
		ro.SetMaxIdleConns(maxReaders)
		ro.SetConnMaxIdleTime(30 * time.Second)
		if err := ro.Ping(); err != nil {
			ro.Close()
			rw.Close()
			return fail(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &store{rw: rw, ro: ro, cancel: cancel, maxClaims: maxClaims, events: newBroadcaster()}
	if _, err := s.sweep(); err != nil {
		s.Close()
		return fail(err)
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		runLeaseSweeper(ctx, s, defaultSweepInterval)
	}()
	return s, isNew, nil
}

func openRW(path string) (*sql.DB, bool, error) {
	db, err := openDBConn(path, false)
	if err != nil {
		return nil, false, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		db.Close()
		return nil, false, err
	}
	if version < 0 || version > schemaVersion {
		db.Close()
		return nil, false, fmt.Errorf("unsupported schema version %d (this binary supports %d)", version, schemaVersion)
	}
	const fullSchema = "CREATE TABLE IF NOT EXISTS tasks (" + taskColumns + `);
CREATE INDEX IF NOT EXISTS idx_tasks_pending ON tasks (priority ASC, created_at ASC) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_tasks_pending_project ON tasks (project, priority ASC, created_at ASC) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_tasks_lease_timeout ON tasks (lease_expires ASC) WHERE status = 'leased';
CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks (project, status, priority ASC);
CREATE INDEX IF NOT EXISTS idx_tasks_claim_count ON tasks (status, claim_count) WHERE claim_count > 0;
CREATE INDEX IF NOT EXISTS idx_tasks_done ON tasks (project) WHERE status = 'done';
CREATE TABLE IF NOT EXISTS notes (
  id INTEGER PRIMARY KEY,
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  author TEXT NOT NULL,
  text TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notes_task_id ON notes (task_id);
CREATE TABLE IF NOT EXISTS task_deps (
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  depends_on_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  PRIMARY KEY (task_id, depends_on_id)
);
CREATE INDEX IF NOT EXISTS idx_task_deps_depends_on ON task_deps (depends_on_id);`
	hasTasks, err := rowExists(db, "SELECT 1 FROM sqlite_master WHERE type='table' AND name='tasks'")
	if err != nil {
		db.Close()
		return nil, false, err
	}
	if !hasTasks {
		if version != 0 {
			db.Close()
			return nil, false, fmt.Errorf("schema version %d specified but tasks table is missing", version)
		}
		used, err := rowExists(db, "SELECT 1 FROM sqlite_master LIMIT 1")
		if err != nil {
			db.Close()
			return nil, false, err
		}
		if used {
			db.Close()
			return nil, false, errors.New("file is not a taskd database")
		}
		if _, err := db.Exec("PRAGMA journal_mode(WAL)"); err != nil {
			db.Close()
			return nil, false, err
		}
		if _, err := db.Exec(fullSchema + fmt.Sprintf("\nPRAGMA user_version = %d;", schemaVersion)); err != nil {
			db.Close()
			return nil, false, err
		}
		return db, true, nil
	}
	if _, err := db.Exec("PRAGMA journal_mode(WAL)"); err != nil {
		db.Close()
		return nil, false, err
	}
	if version == 0 {
		var hasProject int
		if err := db.QueryRow("SELECT count(*) FROM pragma_table_info('tasks') WHERE name='project'").Scan(&hasProject); err != nil {
			db.Close()
			return nil, false, err
		}
		if hasProject > 0 {
			version = 2
		} else {
			version = 1
		}
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d;", version)); err != nil {
			db.Close()
			return nil, false, err
		}
	}
	for i := max(0, version-1); i < len(migrations); i++ {
		if err := migrations[i](db); err != nil {
			db.Close()
			return nil, false, err
		}
	}
	return db, false, nil
}

// mainDBName returns the basename of the file behind the main schema,
// or "" for an in-memory database.
func mainDBName(db *sql.DB) string {
	rows, err := db.Query("PRAGMA database_list")
	if err != nil {
		return ""
	}
	defer rows.Close()
	for rows.Next() {
		var seq int
		var name string
		var file sql.NullString
		if err := rows.Scan(&seq, &name, &file); err == nil && name == "main" {
			if file.Valid && file.String != "" {
				return filepath.Base(file.String)
			}
			return ""
		}
	}
	return ""
}
