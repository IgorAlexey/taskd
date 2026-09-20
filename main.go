package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/IgorAlexey/taskd/internal/version"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
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

var migrations = [...]func(*sql.DB) error{migrateV2, migrateV3, migrateV4, migrateV5, migrateV6, migrateV7, migrateV8, migrateV9}

const schemaVersion = len(migrations) + 1

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
	for i, w := range s.waiters {
		if w.project == project {
			copy(s.waiters[i:], s.waiters[i+1:])
			s.waiters[len(s.waiters)-1] = nil
			s.waiters = s.waiters[:len(s.waiters)-1]
			select {
			case w.ch <- project:
			default:
			}
			return
		}
	}
	for i, w := range s.waiters {
		if w.project == "*" {
			copy(s.waiters[i:], s.waiters[i+1:])
			s.waiters[len(s.waiters)-1] = nil
			s.waiters = s.waiters[:len(s.waiters)-1]
			select {
			case w.ch <- project:
			default:
			}
			return
		}
	}
}

func (s *store) removeWaiter(target *claimWaiter) {
	if s == nil {
		return
	}
	s.waitersMu.Lock()
	defer s.waitersMu.Unlock()
	for i, w := range s.waiters {
		if w == target {
			copy(s.waiters[i:], s.waiters[i+1:])
			s.waiters[len(s.waiters)-1] = nil
			s.waiters = s.waiters[:len(s.waiters)-1]
			return
		}
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

func openDB(path string, maxClaims int) (s *store, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("cannot open database %s: %w", path, err)
		}
	}()
	rw, err := openRW(path)
	if err != nil {
		return nil, err
	}
	var ro *sql.DB
	if _, memory := resolveDBPath(path); memory {
		ro = rw
	} else {
		ro, err = openDBConn(path, true)
		if err != nil {
			rw.Close()
			return nil, err
		}
		ro.SetMaxOpenConns(maxReaders)
		ro.SetMaxIdleConns(maxReaders)
		ro.SetConnMaxIdleTime(30 * time.Second)
		if err := ro.Ping(); err != nil {
			ro.Close()
			rw.Close()
			return nil, err
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s = &store{rw: rw, ro: ro, cancel: cancel, maxClaims: maxClaims}
	if _, err := s.sweep(); err != nil {
		cancel()
		rw.Close()
		ro.Close()
		return nil, err
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		runLeaseSweeper(ctx, s, defaultSweepInterval)
	}()
	return s, nil
}

func openRW(path string) (*sql.DB, error) {
	db, err := openDBConn(path, false)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		db.Close()
		return nil, err
	}
	if version < 0 || version > schemaVersion {
		db.Close()
		return nil, fmt.Errorf("unsupported schema version %d (this binary supports %d)", version, schemaVersion)
	}
	const fullSchema = "CREATE TABLE IF NOT EXISTS tasks (" + taskColumns + `);
CREATE INDEX IF NOT EXISTS idx_tasks_pending ON tasks (priority ASC, id ASC) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_tasks_pending_project ON tasks (project, priority ASC, id ASC) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_tasks_lease_timeout ON tasks (lease_expires ASC) WHERE status = 'leased';
CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks (project, status, priority ASC);
CREATE INDEX IF NOT EXISTS idx_tasks_claim_count ON tasks (status, claim_count) WHERE claim_count > 0;
CREATE INDEX IF NOT EXISTS idx_tasks_done ON tasks (project) WHERE status = 'done';`
	hasTasks, err := rowExists(db, "SELECT 1 FROM sqlite_master WHERE type='table' AND name='tasks'")
	if err != nil {
		db.Close()
		return nil, err
	}
	if !hasTasks {
		if version != 0 {
			db.Close()
			return nil, fmt.Errorf("schema version %d specified but tasks table is missing", version)
		}
		used, err := rowExists(db, "SELECT 1 FROM sqlite_master LIMIT 1")
		if err != nil {
			db.Close()
			return nil, err
		}
		if used {
			db.Close()
			return nil, errors.New("file is not a taskd database")
		}
		if _, err := db.Exec("PRAGMA journal_mode(WAL)"); err != nil {
			db.Close()
			return nil, err
		}
		if _, err := db.Exec(fullSchema + fmt.Sprintf("\nPRAGMA user_version = %d;", schemaVersion)); err != nil {
			db.Close()
			return nil, err
		}
		return db, nil
	}
	if _, err := db.Exec("PRAGMA journal_mode(WAL)"); err != nil {
		db.Close()
		return nil, err
	}
	if version == 0 {
		var hasProject int
		if err := db.QueryRow("SELECT count(*) FROM pragma_table_info('tasks') WHERE name='project'").Scan(&hasProject); err != nil {
			db.Close()
			return nil, err
		}
		if hasProject > 0 {
			version = 2
		} else {
			version = 1
		}
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d;", version)); err != nil {
			db.Close()
			return nil, err
		}
	}
	for i := max(0, version-1); i < len(migrations); i++ {
		if err := migrations[i](db); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

func migrateV2(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query("SELECT name FROM pragma_table_info('tasks')")
	if err != nil {
		return err
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if !cols["body"] {
		if _, err := tx.Exec("ALTER TABLE tasks ADD COLUMN body TEXT NOT NULL DEFAULT '';"); err != nil {
			return err
		}
	}
	if !cols["priority"] {
		if _, err := tx.Exec("ALTER TABLE tasks ADD COLUMN priority INTEGER NOT NULL DEFAULT 0;"); err != nil {
			return err
		}
	}
	if !cols["project"] {
		if _, err := tx.Exec("ALTER TABLE tasks ADD COLUMN project TEXT NOT NULL DEFAULT '';"); err != nil {
			return err
		}
	}

	stmts := []string{
		"DROP INDEX IF EXISTS idx_tasks_claim;",
		"CREATE INDEX IF NOT EXISTS idx_tasks_queue ON tasks (status, priority DESC);",
		"CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks (project, status, priority DESC);",
		"PRAGMA user_version = 2;",
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func migrateV3(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var hasClaimCount int
	if err := tx.QueryRow("SELECT count(*) FROM pragma_table_info('tasks') WHERE name='claim_count'").Scan(&hasClaimCount); err != nil {
		return err
	}
	if hasClaimCount == 0 {
		if _, err := tx.Exec("ALTER TABLE tasks ADD COLUMN claim_count INTEGER NOT NULL DEFAULT 0;"); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("PRAGMA user_version = 3;"); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV4 flips priority from "higher claims first" to "lower claims first"
// (P1 is top) and rebuilds the table from taskColumns so every v4 database has
// the same layout. Old rows are remapped with a clamp: 0 was "unspecified" and
// lands on the new default, anything above the old top of 3 becomes 1, and
// 1..3 mirror so relative order survives. Columns an earlier migration left
// missing or nullable are filled with their schema defaults.
func migrateV4(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query("SELECT name FROM pragma_table_info('tasks')")
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	src := func(name, def string) string {
		if !cols[name] {
			return def
		}
		return "COALESCE(" + name + ", " + def + ")"
	}
	insert := "INSERT INTO tasks_new (rowid, id, asset_path, status, worker, lease_expires, primitives, body, priority, project, claim_count) SELECT rowid, id, " +
		src("asset_path", "''") + ", " +
		src("status", "'pending'") + ", " +
		"CAST(substr(CAST(" + src("worker", "NULL") + " AS BLOB), 1, 128) AS TEXT), " +
		src("lease_expires", "NULL") + ", " +
		src("primitives", "NULL") + ", " +
		src("body", "''") + ", " +
		"CASE WHEN " + src("priority", "0") + " <= 0 THEN 3 WHEN " + src("priority", "0") + " >= 4 THEN 1 ELSE 4 - " + src("priority", "0") + " END, " +
		src("project", "''") + ", " +
		src("claim_count", "0") + " FROM tasks;"

	stmts := []string{
		"CREATE TABLE tasks_new (" + taskColumns + ");",
		insert,
		"DROP TABLE tasks;",
		"ALTER TABLE tasks_new RENAME TO tasks;",
		"CREATE INDEX idx_tasks_queue ON tasks (status, priority ASC);",
		"CREATE INDEX idx_tasks_project ON tasks (project, status, priority ASC);",
		"PRAGMA user_version = 4;",
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func migrateV5(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		"CREATE TABLE tasks_new (" + taskColumns + ");",
		`INSERT INTO tasks_new (rowid, id, asset_path, status, worker, lease_expires, primitives, body, priority, project, claim_count)
SELECT rowid, id, asset_path, status, CAST(substr(CAST(worker AS BLOB), 1, 128) AS TEXT), lease_expires, primitives, body, priority, project, claim_count FROM tasks;`,
		"DROP TABLE tasks;",
		"ALTER TABLE tasks_new RENAME TO tasks;",
		"CREATE INDEX idx_tasks_queue ON tasks (status, priority ASC);",
		"CREATE INDEX idx_tasks_project ON tasks (project, status, priority ASC);",
		"PRAGMA user_version = 5;",
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const taskColumns = `
  id TEXT PRIMARY KEY,
  asset_path TEXT NOT NULL DEFAULT '',
  status TEXT DEFAULT 'pending',
  worker TEXT CHECK (octet_length(worker) <= 128),
  lease_expires INTEGER,
  primitives JSON,
  body TEXT NOT NULL DEFAULT '',
  priority INTEGER NOT NULL DEFAULT 3 CHECK (priority >= 0),
  project TEXT NOT NULL DEFAULT '',
  claim_count INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL DEFAULT (unixepoch())`

func migrateV6(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		"CREATE INDEX IF NOT EXISTS idx_tasks_claim_count ON tasks (status, claim_count) WHERE claim_count > 0;",
		"PRAGMA user_version = 6;",
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func migrateV7(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		"DROP INDEX IF EXISTS idx_tasks_queue;",
		"CREATE INDEX IF NOT EXISTS idx_tasks_pending ON tasks (priority ASC, id ASC) WHERE status = 'pending';",
		"CREATE INDEX IF NOT EXISTS idx_tasks_pending_project ON tasks (project, priority ASC, id ASC) WHERE status = 'pending';",
		"CREATE INDEX IF NOT EXISTS idx_tasks_lease_timeout ON tasks (lease_expires ASC) WHERE status = 'leased';",
		"PRAGMA user_version = 7;",
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func migrateV8(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	stmt := fmt.Sprintf("ALTER TABLE tasks ADD COLUMN created_at INTEGER NOT NULL DEFAULT %d;", now)
	if _, err := tx.Exec(stmt); err != nil {
		return err
	}
	if _, err := tx.Exec("PRAGMA user_version = 8;"); err != nil {
		return err
	}
	return tx.Commit()
}

func migrateV9(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		"CREATE INDEX IF NOT EXISTS idx_tasks_done ON tasks (project) WHERE status = 'done';",
		"PRAGMA user_version = 9;",
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const defaultPriority = 3

const maxBodyBytes = 1 << 20

type apiError struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeAPIError(w, code, apiError{Error: msg})
}

func writeAPIError(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(payload)
}

type route struct {
	handler   http.HandlerFunc
	params    []string
	anyParams bool
}

func handleMethods(mux *http.ServeMux, pattern string, methods map[string]route) {
	var allowed []string
	for m := range methods {
		allowed = append(allowed, m)
	}
	if slices.Contains(allowed, http.MethodGet) && !slices.Contains(allowed, http.MethodHead) {
		allowed = append(allowed, http.MethodHead)
	}
	slices.Sort(allowed)
	allowHeader := strings.Join(allowed, ", ")

	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		rt, ok := methods[r.Method]
		if !ok && r.Method == http.MethodHead {
			rt, ok = methods[http.MethodGet]
		}
		if !ok {
			w.Header().Set("Allow", allowHeader)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if r.URL.RawQuery != "" && !rt.anyParams {
			q, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid query string: %v", err))
				return
			}
			keys := make([]string, 0, len(q))
			for k := range q {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			for _, k := range keys {
				if !slices.Contains(rt.params, k) {
					writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown query parameter: %s", k))
					return
				}
			}
			r = r.WithContext(context.WithValue(r.Context(), queryContextKey{}, q))
		}
		rt.handler(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeBody(w, r, dst, false)
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any, optional bool) bool {
	var maxErr *http.MaxBytesError
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if optional && errors.Is(err, io.EOF) {
			return true
		}
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid request body: unexpected trailing data")
		return false
	}
	return true
}

type queryContextKey struct{}

func requestQuery(r *http.Request) url.Values {
	if q, ok := r.Context().Value(queryContextKey{}).(url.Values); ok {
		return q
	}
	if r.URL.RawQuery == "" {
		return url.Values{}
	}
	return r.URL.Query()
}

type listCursor struct {
	Rowid   int64  `json:"r"`
	Filters string `json:"f"`
}

func listFilterFingerprint(q url.Values) string {
	var b strings.Builder
	for _, k := range []string{"status", "project", "worker", "priority", "asset_path", "q"} {
		if q.Has(k) {
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(q.Get(k))
		}
		b.WriteByte(0)
	}
	if q.Get("order") == "desc" {
		b.WriteString("order=desc")
		b.WriteByte(0)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return base64.RawURLEncoding.EncodeToString(sum[:9])
}

func encodeListCursor(c listCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeListCursor(s, fingerprint string) (listCursor, bool) {
	var c listCursor
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return c, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return c, false
	}
	if c.Rowid < 1 || c.Filters != fingerprint {
		return c, false
	}
	return c, true
}

func internalError(w http.ResponseWriter, err error) {
	log.Print(err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

type leaseConflict struct {
	Error      string `json:"error"`
	Worker     string `json:"worker"`
	ClaimCount int    `json:"claim_count"`
}

func writeTaskStateError(w http.ResponseWriter, q queryer, id, worker string, claim *int) {
	var (
		status string
		holder sql.NullString
		claims int
	)
	err := q.QueryRow("SELECT status, worker, claim_count FROM tasks WHERE id = ?", id).Scan(&status, &holder, &claims)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	switch status {
	case "done":
		writeError(w, http.StatusConflict, "task is done")
	case "leased":
		conflict := leaseConflict{Worker: holder.String, ClaimCount: claims}
		switch {
		case worker == "":
			conflict.Error = "task is leased"
		case holder.String != worker:
			conflict.Error = "task leased by another worker"
		case claim != nil && claims != *claim:
			conflict.Error = "task claimed again"
		default:
			conflict.Error = "lease has expired"
		}
		writeAPIError(w, http.StatusConflict, conflict)
	case "pending":
		writeError(w, http.StatusConflict, "task is pending")
	case "buried":
		writeError(w, http.StatusConflict, "task is buried")
	default:
		writeError(w, http.StatusConflict, "task state conflict")
	}
}

const maxWorkerLen = 128

var errMissingWorker = errors.New("missing worker")
var errWorkerTooLong = errors.New("worker too long")

func cleanWorker(raw string) (string, error) {
	worker := strings.TrimSpace(raw)
	if worker == "" {
		return "", errMissingWorker
	}
	if len(worker) > maxWorkerLen {
		return "", errWorkerTooLong
	}
	return worker, nil
}

func checkWorker(w http.ResponseWriter, raw string) (string, bool) {
	worker, err := cleanWorker(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return worker, true
}

type workerBody struct {
	Worker string `json:"worker"`
}

func decodeWorker(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req workerBody
	if !decodeJSON(w, r, &req) {
		return "", false
	}
	return checkWorker(w, req.Worker)
}

func decodeOptionalWorker(w http.ResponseWriter, r *http.Request) bool {
	var req workerBody
	return decodeBody(w, r, &req, true)
}

func updateLeased(w http.ResponseWriter, db *sql.DB, set, id, worker string, args ...any) (leaseEnvelope, bool) {
	query := "UPDATE tasks SET " + set + " WHERE id=? AND status='leased' AND worker=? AND lease_expires >= unixepoch() RETURNING id, lease_expires, status, project"
	return updateTask(w, db, query, append(args, id, worker), id, worker, nil)
}

func updateLeasedOrLapsed(w http.ResponseWriter, db *sql.DB, set, id, worker string, claim *int, args ...any) (leaseEnvelope, bool) {
	query := "UPDATE tasks SET " + set + " WHERE id=? AND status='leased' AND worker=?"
	fullArgs := append(args, id, worker)
	if claim != nil {
		query += " AND claim_count=?"
		fullArgs = append(fullArgs, *claim)
	}
	query += " RETURNING id, lease_expires, status, project"
	return updateTask(w, db, query, fullArgs, id, worker, claim)
}

func updateTask(w http.ResponseWriter, db *sql.DB, query string, args []any, id, worker string, claim *int) (leaseEnvelope, bool) {
	var (
		env     leaseEnvelope
		expires sql.NullInt64
	)
	tx, err := db.Begin()
	if err != nil {
		internalError(w, err)
		return env, false
	}
	defer tx.Rollback()
	err = tx.QueryRow(query, args...).Scan(&env.ID, &expires, &env.Status, &env.Project)
	if errors.Is(err, sql.ErrNoRows) {
		writeTaskStateError(w, tx, id, worker, claim)
		return env, false
	}
	if err != nil {
		internalError(w, err)
		return env, false
	}
	env.LeaseExpires = expires.Int64
	if err := tx.Commit(); err != nil {
		internalError(w, err)
		return env, false
	}
	return env, true
}

type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

var errTaskNotFound = errors.New("task not found")

const maxPrefixMatches = 50

type ambiguousMatch struct {
	Error     string   `json:"error"`
	Count     int      `json:"count,omitempty"`
	Matches   []string `json:"matches"`
	Truncated bool     `json:"truncated,omitempty"`
}

type errMultipleMatch struct {
	count     int
	truncated bool
	matches   []string
}

func (e *errMultipleMatch) Error() string {
	if e.truncated {
		return fmt.Sprintf("ambiguous id prefix: more than %d tasks match", maxPrefixMatches)
	}
	return fmt.Sprintf("ambiguous id prefix: %d tasks match", e.count)
}

func prefixRange(prefix string) (string, string) {
	b := []byte(prefix)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < 0xff {
			b[i]++
			return prefix, string(b[:i+1])
		}
	}
	return prefix, ""
}

func resolveTaskID(q queryer, id string) (string, error) {
	if id == "" {
		return "", errTaskNotFound
	}
	var exactID string
	err := q.QueryRow("SELECT id FROM tasks WHERE id = ?", id).Scan(&exactID)
	if err == nil {
		return exactID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	lower, upper := prefixRange(id)
	where := "id >= ?"
	args := []any{lower}
	if upper != "" {
		where += " AND id < ?"
		args = append(args, upper)
	}
	args = append(args, maxPrefixMatches+1)
	rows, err := q.Query("SELECT id FROM tasks WHERE "+where+" ORDER BY id ASC LIMIT ?", args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var matches []string
	for rows.Next() {
		var match string
		if err := rows.Scan(&match); err != nil {
			return "", err
		}
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", errTaskNotFound
	}
	if len(matches) > maxPrefixMatches {
		return "", &errMultipleMatch{truncated: true, matches: matches[:maxPrefixMatches]}
	}
	if len(matches) > 1 {
		return "", &errMultipleMatch{count: len(matches), matches: matches}
	}
	return matches[0], nil
}

func resolveTaskIDHTTP(w http.ResponseWriter, q queryer, id string) (string, bool) {
	resolved, err := resolveTaskID(q, id)
	if err == nil {
		return resolved, true
	}
	var mm *errMultipleMatch
	if errors.As(err, &mm) {
		writeAPIError(w, http.StatusConflict, ambiguousMatch{
			Error:     mm.Error(),
			Count:     mm.count,
			Matches:   mm.matches,
			Truncated: mm.truncated,
		})
		return "", false
	}
	if errors.Is(err, errTaskNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return "", false
	}
	internalError(w, err)
	return "", false
}

type taskItem struct {
	ID           string          `json:"id"`
	AssetPath    string          `json:"asset_path"`
	Status       string          `json:"status"`
	Worker       string          `json:"worker"`
	LeaseExpires int64           `json:"lease_expires"`
	Priority     int             `json:"priority"`
	Body         string          `json:"body"`
	Primitives   json.RawMessage `json:"primitives"`
	Project      string          `json:"project"`
	ClaimCount   int             `json:"claim_count"`
	CreatedAt    int64           `json:"created_at"`

	summaryPrefix string
}

const summaryRunes = 50

const summaryTrim = " \t\r\n"

var summaryPrefixCol = fmt.Sprintf("substr(ltrim(body, %s), 1, %d)", sqlCharset(summaryTrim), summaryRunes+1)

func sqlCharset(cut string) string {
	parts := make([]string, 0, len(cut))
	for _, r := range cut {
		parts = append(parts, fmt.Sprintf("char(%d)", r))
	}
	return strings.Join(parts, " || ")
}

func summaryLine(body string) string {
	s := strings.TrimLeft(body, summaryTrim)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > summaryRunes {
		return string(r[:summaryRunes]) + "\u2026"
	}
	return s
}

type leaseEnvelope struct {
	ID           string `json:"id"`
	LeaseExpires int64  `json:"lease_expires"`
	Status       string `json:"status"`
	Project      string `json:"-"`
}

const maxTaskIDLen = 128
const maxProjectLen = 64

func validNameByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-'
}

func validTaskID(id string) bool {
	if id == "" || len(id) > maxTaskIDLen || id == "." || id == ".." {
		return false
	}
	if strings.EqualFold(id, "claim") || strings.EqualFold(id, "purge") {
		return false
	}
	for i := range len(id) {
		if !validNameByte(id[i]) {
			return false
		}
	}
	return true
}

func validProject(p string) bool {
	if p == "" || len(p) > maxProjectLen {
		return false
	}
	for i := range len(p) {
		if !validNameByte(p[i]) {
			return false
		}
	}
	return true
}

//go:embed web/index.html
var uiHTML []byte

func redirectWithQuery(w http.ResponseWriter, r *http.Request, path string, code int) {
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, path, code)
}
func escapeLike(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '%', '_', '\\':
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func workerFilterClause(worker string, now int64) (string, []any) {
	if worker != "" {
		return "worker = ? AND (status = 'done' OR (status = 'leased' AND lease_expires >= ?))", []any{worker, now}
	}
	return "(worker IS NULL OR worker = '' OR (status = 'leased' AND lease_expires < ?))", []any{now}
}

const buryExhaustedSQL = `UPDATE tasks SET status='buried', worker=NULL, lease_expires=NULL
WHERE (status='pending' OR (status='leased' AND lease_expires < unixepoch()))
  AND claim_count > 0 AND claim_count >= ?`

// etagMatches implements If-None-Match: a comma-separated list of
// entity tags, each optionally weak (W/), or the wildcard "*".
func etagMatches(header, etag string) bool {
	for header != "" {
		var tag string
		tag, header, _ = strings.Cut(header, ",")
		tag = strings.TrimSpace(tag)
		if tag == "*" || strings.TrimPrefix(tag, "W/") == etag {
			return true
		}
	}
	return false
}

// statsResponse is the body of GET /stats.
type statsResponse struct {
	Pending      int    `json:"pending"`
	Leased       int    `json:"leased"`
	Done         int    `json:"done"`
	Buried       int    `json:"buried"`
	Total        int    `json:"total"`
	LeaseSeconds int    `json:"lease_seconds"`
	DB           string `json:"db"`
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
func sweepLapsed(rw *sql.DB, maxClaims int, id string) error {
	if maxClaims > 0 {
		query := buryExhaustedSQL
		args := []any{maxClaims}
		if id != "" {
			query += " AND id = ?"
			args = append(args, id)
		}
		rows, err := rw.Query(query+" RETURNING id", args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var buried []string
		for rows.Next() {
			var task string
			if err := rows.Scan(&task); err != nil {
				return err
			}
			buried = append(buried, task)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(buried) > 0 {
			log.Printf("buried at the %d claim limit: %s", maxClaims, strings.Join(buried, " "))
		}
	}
	query := "UPDATE tasks SET status='pending', worker=NULL, lease_expires=NULL WHERE status='leased' AND lease_expires < unixepoch()"
	var args []any
	if id != "" {
		query += " AND id = ?"
		args = append(args, id)
	}
	_, err := rw.Exec(query, args...)
	return err
}

func newHandler(db *store, lease int) http.Handler {
	return newHandlerWithCORS(db, lease, "")
}

func newHandlerWithCORS(db *store, lease int, corsOrigin string) http.Handler {
	mux := http.NewServeMux()
	dbName := mainDBName(db.ro)

	sweepLapsedLeases := func(id string) error {
		return sweepLapsed(db.rw, db.maxClaims, id)
	}
	uiHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(uiHTML)
	}
	handleMethods(mux, "/{$}", map[string]route{
		http.MethodGet: {handler: func(w http.ResponseWriter, r *http.Request) {
			redirectWithQuery(w, r, "/ui", http.StatusFound)
		}, anyParams: true},
	})
	handleMethods(mux, "/ui", map[string]route{
		http.MethodGet: {handler: uiHandler, anyParams: true},
	})
	statsHandler := func(w http.ResponseWriter, r *http.Request) {
		q := requestQuery(r)
		project := q.Get("project")
		now := time.Now().Unix()
		query := `SELECT
  COUNT(CASE WHEN status = 'pending' OR (status = 'leased' AND lease_expires < ?) THEN 1 END),
  COUNT(CASE WHEN status = 'leased' AND lease_expires >= ? THEN 1 END),
  COUNT(CASE WHEN status = 'done' THEN 1 END),
  COUNT(CASE WHEN status = 'buried' THEN 1 END),
  COUNT(*)
FROM tasks`
		var args []any
		args = append(args, now, now)
		var where []string
		if project != "" && project != "*" {
			where = append(where, "project = ?")
			args = append(args, project)
		}
		if q.Has("worker") {
			clause, cargs := workerFilterClause(strings.TrimSpace(q.Get("worker")), now)
			where = append(where, clause)
			args = append(args, cargs...)
		}
		if len(where) > 0 {
			query += " WHERE " + strings.Join(where, " AND ")
		}
		var pending, leased, done, buried, total int
		if err := db.ro.QueryRow(query, args...).Scan(&pending, &leased, &done, &buried, &total); err != nil {
			internalError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(statsResponse{
			Pending: pending, Leased: leased, Done: done, Buried: buried,
			Total: total, LeaseSeconds: lease, DB: dbName,
		})
	}
	createTaskHandler := func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID        string `json:"id"`
			AssetPath string `json:"asset_path"`
			Body      string `json:"body"`
			Priority  *int   `json:"priority"`
			Project   string `json:"project"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		req.AssetPath = strings.TrimSpace(req.AssetPath)
		req.Project = strings.TrimSpace(req.Project)
		if req.Body != "" && strings.TrimSpace(req.Body) == "" {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
		if req.AssetPath == "" && req.Body == "" {
			writeError(w, http.StatusBadRequest, "missing asset_path or body")
			return
		}
		if req.Project == "" {
			writeError(w, http.StatusBadRequest, "missing project")
			return
		}
		if !validProject(req.Project) {
			writeError(w, http.StatusBadRequest, "invalid project")
			return
		}
		priority := defaultPriority
		if req.Priority != nil {
			if *req.Priority < 0 {
				writeError(w, http.StatusBadRequest, "invalid priority")
				return
			}
			priority = *req.Priority
		}
		if req.ID == "" {
			var b [16]byte
			if _, err := rand.Read(b[:]); err != nil {
				internalError(w, err)
				return
			}
			req.ID = hex.EncodeToString(b[:])
		} else if !validTaskID(req.ID) {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		_, err := db.rw.Exec("INSERT INTO tasks (id, asset_path, body, priority, project, created_at) VALUES (?, ?, ?, ?, ?, unixepoch())", req.ID, req.AssetPath, req.Body, priority, req.Project)
		if err != nil {
			var se *sqlite.Error
			if errors.As(err, &se) && (se.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY || se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE) {
				writeError(w, http.StatusConflict, "duplicate id")
				return
			}
			internalError(w, err)
			return
		}
		db.notifyPending(req.Project)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Location", "/tasks/"+req.ID)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": req.ID})
	}

	claimHandler := func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Worker  string   `json:"worker"`
			Project string   `json:"project"`
			Wait    *float64 `json:"wait"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		var ok bool
		if req.Worker, ok = checkWorker(w, req.Worker); !ok {
			return
		}
		if req.Wait != nil && *req.Wait < 0 {
			writeError(w, http.StatusBadRequest, "invalid wait")
			return
		}
		req.Project = strings.TrimSpace(req.Project)
		if req.Project == "" || req.Project == "*" {
			req.Project = "*"
		} else if !validProject(req.Project) {
			writeError(w, http.StatusBadRequest, "invalid project")
			return
		}
		query := `UPDATE tasks SET status='leased', worker=?, lease_expires=unixepoch()+?, claim_count=claim_count+1
WHERE id = (
  SELECT id FROM tasks
  WHERE status='pending'`
		args := []any{req.Worker, lease}
		if db.maxClaims > 0 {
			query += " AND claim_count < ?"
			args = append(args, db.maxClaims)
		}
		if req.Project != "*" {
			query += " AND project = ?"
			args = append(args, req.Project)
		}
		query += `
  ORDER BY priority ASC, id ASC
  LIMIT 1
) RETURNING id, asset_path, status, worker, lease_expires, priority, body, primitives, project, claim_count, created_at`

		tryClaim := func() (*taskItem, error) {
			var (
				item         taskItem
				leasedWorker sql.NullString
				leaseExpires sql.NullInt64
				prim         []byte
			)
			err := db.rw.QueryRow(query, args...).
				Scan(&item.ID, &item.AssetPath, &item.Status, &leasedWorker, &leaseExpires, &item.Priority, &item.Body, &prim, &item.Project, &item.ClaimCount, &item.CreatedAt)
			if err != nil {
				return nil, err
			}
			item.Worker = leasedWorker.String
			item.LeaseExpires = leaseExpires.Int64
			item.Primitives = prim
			return &item, nil
		}

		var (
			timer *time.Timer
			cw    *claimWaiter
		)

		for {
			seq := db.currentSeq(req.Project)

			item, err := tryClaim()
			if err == nil {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(item)
				return
			}
			if !errors.Is(err, sql.ErrNoRows) {
				internalError(w, err)
				return
			}
			if req.Wait == nil || *req.Wait == 0 {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			if timer == nil {
				timer = time.NewTimer(time.Duration(*req.Wait * float64(time.Second)))
				defer timer.Stop()
				cw = &claimWaiter{
					project: req.Project,
					ch:      make(chan string, 1),
				}
			}

			db.waitersMu.Lock()
			if db.closed {
				db.waitersMu.Unlock()
				writeError(w, http.StatusServiceUnavailable, "database closed")
				return
			}
			if db.hasChangedSeqLocked(req.Project, seq) {
				db.waitersMu.Unlock()
				continue
			}
			db.waiters = append(db.waiters, cw)
			db.waitersMu.Unlock()

			select {
			case <-cw.ch:
				db.waitersMu.Lock()
				closed := db.closed
				db.waitersMu.Unlock()
				if closed {
					writeError(w, http.StatusServiceUnavailable, "database closed")
					return
				}
			case <-timer.C:
				db.removeWaiter(cw)
				select {
				case p := <-cw.ch:
					db.notifyPending(p)
				default:
				}
				w.WriteHeader(http.StatusNoContent)
				return
			case <-r.Context().Done():
				db.removeWaiter(cw)
				select {
				case p := <-cw.ch:
					db.notifyPending(p)
				default:
				}
				return
			}
		}
	}
	claimIDHandler := func(w http.ResponseWriter, r *http.Request) {
		worker, ok := decodeWorker(w, r)
		if !ok {
			return
		}
		id, ok := resolveTaskIDHTTP(w, db.ro, r.PathValue("id"))
		if !ok {
			return
		}
		if err := sweepLapsedLeases(id); err != nil {
			internalError(w, err)
			return
		}
		query := `UPDATE tasks
SET status='leased', worker=?, lease_expires=unixepoch()+?, claim_count=claim_count+1
WHERE id = ? AND status='pending'
  AND (? <= 0 OR claim_count < ?)
RETURNING id, asset_path, status, worker, lease_expires, priority, body, primitives, project, claim_count, created_at`
		var (
			item         taskItem
			leasedWorker sql.NullString
			leaseExpires sql.NullInt64
			prim         []byte
		)
		err := db.rw.QueryRow(query, worker, lease, id, db.maxClaims, db.maxClaims).
			Scan(&item.ID, &item.AssetPath, &item.Status, &leasedWorker, &leaseExpires, &item.Priority, &item.Body, &prim, &item.Project, &item.ClaimCount, &item.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			writeTaskStateError(w, db.ro, id, "", nil)
			return
		}
		if err != nil {
			internalError(w, err)
			return
		}
		item.Worker = leasedWorker.String
		item.LeaseExpires = leaseExpires.Int64
		item.Primitives = prim
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(item)
	}

	doneIDHandler := func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Worker     string          `json:"worker"`
			ClaimCount *int            `json:"claim_count"`
			Primitives json.RawMessage `json:"primitives"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		var ok bool
		if req.Worker, ok = checkWorker(w, req.Worker); !ok {
			return
		}
		id, ok := resolveTaskIDHTTP(w, db.ro, r.PathValue("id"))
		if !ok {
			return
		}
		var prim any
		if len(req.Primitives) > 0 {
			prim = string(req.Primitives)
		}
		if _, ok := updateLeasedOrLapsed(w, db.rw, "status='done', primitives=?, lease_expires=NULL", id, req.Worker, req.ClaimCount, prim); !ok {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
	closeIDHandler := func(w http.ResponseWriter, r *http.Request) {
		if !decodeOptionalWorker(w, r) {
			return
		}
		id, ok := resolveTaskIDHTTP(w, db.ro, r.PathValue("id"))
		if !ok {
			return
		}
		res, err := db.rw.Exec(`UPDATE tasks SET status='done', worker=NULL, lease_expires=NULL
WHERE id=? AND status!='done' AND NOT (status='leased' AND lease_expires >= unixepoch())`, id)
		if err != nil {
			internalError(w, err)
			return
		}
		aff, err := res.RowsAffected()
		if err != nil {
			internalError(w, err)
			return
		}
		if aff == 0 {
			writeTaskStateError(w, db.ro, id, "", nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
	touchIDHandler := func(w http.ResponseWriter, r *http.Request) {
		worker, ok := decodeWorker(w, r)
		if !ok {
			return
		}
		id, ok := resolveTaskIDHTTP(w, db.ro, r.PathValue("id"))
		if !ok {
			return
		}
		env, ok := updateLeased(w, db.rw, "lease_expires = unixepoch() + ?", id, worker, lease)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(env)
	}
	releaseIDHandler := func(w http.ResponseWriter, r *http.Request) {
		worker, ok := decodeWorker(w, r)
		if !ok {
			return
		}
		id, ok := resolveTaskIDHTTP(w, db.ro, r.PathValue("id"))
		if !ok {
			return
		}
		env, ok := updateLeasedOrLapsed(w, db.rw, "status='pending', worker=NULL, lease_expires=NULL, claim_count=max(claim_count-1, 0)", id, worker, nil)
		if !ok {
			return
		}
		if env.Project != "" {
			db.notifyPending(env.Project)
		}
		w.WriteHeader(http.StatusNoContent)
	}
	buryIDHandler := func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Worker     string `json:"worker"`
			Priority   *int   `json:"priority"`
			ClaimCount *int   `json:"claim_count"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		worker, ok := checkWorker(w, req.Worker)
		if !ok {
			return
		}
		if req.Priority != nil && *req.Priority < 0 {
			writeError(w, http.StatusBadRequest, "invalid priority")
			return
		}
		id, ok := resolveTaskIDHTTP(w, db.ro, r.PathValue("id"))
		if !ok {
			return
		}
		if _, ok := updateLeasedOrLapsed(w, db.rw, "status='buried', worker=NULL, lease_expires=NULL, priority=COALESCE(?, priority)", id, worker, req.ClaimCount, req.Priority); !ok {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
	kickIDHandler := func(w http.ResponseWriter, r *http.Request) {
		if !decodeOptionalWorker(w, r) {
			return
		}
		id, ok := resolveTaskIDHTTP(w, db.ro, r.PathValue("id"))
		if !ok {
			return
		}
		var project string
		err := db.rw.QueryRow("UPDATE tasks SET status='pending', worker=NULL, lease_expires=NULL, claim_count=0 WHERE id=? AND status='buried' RETURNING project", id).Scan(&project)
		if errors.Is(err, sql.ErrNoRows) {
			writeTaskStateError(w, db.ro, id, "", nil)
			return
		}
		if err != nil {
			internalError(w, err)
			return
		}
		if project != "" {
			db.notifyPending(project)
		}
		w.WriteHeader(http.StatusNoContent)
	}

	listTasksHandler := func(w http.ResponseWriter, r *http.Request) {
		q := requestQuery(r)
		status := q.Get("status")
		if q.Has("status") && status != "pending" && status != "leased" && status != "done" && status != "buried" && status != "live" {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		project := q.Get("project")
		var priorityFilter *int
		if q.Has("priority") {
			v, err := strconv.Atoi(q.Get("priority"))
			if err != nil || v < 0 {
				writeError(w, http.StatusBadRequest, "invalid priority")
				return
			}
			priorityFilter = &v
		}
		limit := 100
		if q.Has("limit") {
			v, err := strconv.Atoi(q.Get("limit"))
			if err != nil || v < 1 || v > 1000 {
				writeError(w, http.StatusBadRequest, "invalid limit")
				return
			}
			limit = v
		}
		if q.Has("after") && q.Has("offset") {
			writeError(w, http.StatusBadRequest, "after and offset are mutually exclusive")
			return
		}
		offset := 0
		if q.Has("offset") {
			v, err := strconv.Atoi(q.Get("offset"))
			if err != nil || v < 0 {
				writeError(w, http.StatusBadRequest, "invalid offset")
				return
			}
			offset = v
		}
		order := "asc"
		if q.Has("order") {
			order = strings.ToLower(strings.TrimSpace(q.Get("order")))
			if order != "asc" && order != "desc" {
				writeError(w, http.StatusBadRequest, "invalid order")
				return
			}
			q.Set("order", order)
		}
		var after *listCursor
		if q.Has("after") {
			c, ok := decodeListCursor(q.Get("after"), listFilterFingerprint(q))
			if !ok {
				writeError(w, http.StatusBadRequest, "invalid after")
				return
			}
			after = &c
		}
		now := time.Now().Unix()
		var requestedFields []string
		fieldsParam := q.Get("fields")
		if fieldsParam == "" && q.Has("columns") {
			fieldsParam = q.Get("columns")
		}
		if q.Has("fields") || q.Has("columns") {
			if strings.TrimSpace(fieldsParam) == "" {
				writeError(w, http.StatusBadRequest, "invalid fields")
				return
			}
			parts := strings.Split(fieldsParam, ",")
			seen := make(map[string]bool, len(parts))
			for _, p := range parts {
				f := strings.TrimSpace(p)
				switch f {
				case "id", "asset_path", "status", "worker", "lease_expires", "priority", "body", "primitives", "project", "claim_count", "summary", "created_at":
					if !seen[f] {
						seen[f] = true
						requestedFields = append(requestedFields, f)
					}
				default:
					writeError(w, http.StatusBadRequest, "invalid fields")
					return
				}
			}
			if len(requestedFields) == 0 {
				writeError(w, http.StatusBadRequest, "invalid fields")
				return
			}
		}
		bodyCol, primCol, summaryCol := "body", "primitives", "''"
		if requestedFields != nil {
			if !slices.Contains(requestedFields, "body") {
				bodyCol = "''"
			}
			if !slices.Contains(requestedFields, "primitives") {
				primCol = "NULL"
			}
			if slices.Contains(requestedFields, "summary") {
				summaryCol = summaryPrefixCol
			}
		}
		query := fmt.Sprintf(`SELECT id, asset_path,
  CASE WHEN status = 'leased' AND lease_expires < ? THEN 'pending' ELSE status END,
  CASE WHEN status = 'leased' AND lease_expires < ? THEN NULL ELSE worker END,
  CASE WHEN status = 'leased' AND lease_expires < ? THEN NULL ELSE lease_expires END,
  priority, %s, %s, %s, project, claim_count, created_at, rowid FROM tasks`, bodyCol, primCol, summaryCol)
		var where []string
		var whereArgs []any
		if status == "pending" {
			where = append(where, "(status = 'pending' OR (status = 'leased' AND lease_expires < ?))")
			whereArgs = append(whereArgs, now)
		} else if status == "leased" {
			where = append(where, "(status = 'leased' AND lease_expires >= ?)")
			whereArgs = append(whereArgs, now)
		} else if status == "live" {
			where = append(where, "(status = 'pending' OR status = 'leased')")
		} else if status != "" {
			where = append(where, "status = ?")
			whereArgs = append(whereArgs, status)
		}
		if project != "" && project != "*" {
			where = append(where, "project = ?")
			whereArgs = append(whereArgs, project)
		}
		if q.Has("worker") {
			clause, cargs := workerFilterClause(strings.TrimSpace(q.Get("worker")), now)
			where = append(where, clause)
			whereArgs = append(whereArgs, cargs...)
		}
		if priorityFilter != nil {
			where = append(where, "priority = ?")
			whereArgs = append(whereArgs, *priorityFilter)
		}
		if q.Has("asset_path") {
			where = append(where, "asset_path = ?")
			whereArgs = append(whereArgs, q.Get("asset_path"))
		}
		if q.Has("q") {
			search := strings.TrimSpace(q.Get("q"))
			if search != "" {
				pat := "%" + escapeLike(search) + "%"
				where = append(where, "(id LIKE ? ESCAPE '\\' OR body LIKE ? ESCAPE '\\' OR project LIKE ? ESCAPE '\\' OR (worker LIKE ? ESCAPE '\\' AND (status = 'done' OR (status = 'leased' AND lease_expires >= ?))) OR asset_path LIKE ? ESCAPE '\\')")
				whereArgs = append(whereArgs, pat, pat, pat, pat, now, pat)
			}
		}
		var whereSQL string
		if len(where) > 0 {
			whereSQL = " WHERE " + strings.Join(where, " AND ")
		}
		dataWhereSQL := whereSQL
		args := make([]any, 0, 3+len(whereArgs)+3)
		args = append(args, now, now, now)
		args = append(args, whereArgs...)
		if after != nil {
			rowidClause := "rowid > ?"
			if order == "desc" {
				rowidClause = "rowid < ?"
			}
			dataWhere := append(slices.Clone(where), rowidClause)
			dataWhereSQL = " WHERE " + strings.Join(dataWhere, " AND ")
			args = append(args, after.Rowid)
		}
		query += dataWhereSQL
		if order == "desc" {
			query += " ORDER BY rowid DESC LIMIT ?"
		} else {
			query += " ORDER BY rowid ASC LIMIT ?"
		}
		args = append(args, limit)
		if offset > 0 {
			query += " OFFSET ?"
			args = append(args, offset)
		}
		rows, err := db.ro.Query(query, args...)
		if err != nil {
			internalError(w, err)
			return
		}
		defer rows.Close()

		tasks := make([]taskItem, 0)
		var next listCursor
		for rows.Next() {
			var (
				item         taskItem
				worker       sql.NullString
				leaseExpires sql.NullInt64
				prim         []byte
				rowid        int64
			)
			if err := rows.Scan(&item.ID, &item.AssetPath, &item.Status, &worker, &leaseExpires, &item.Priority, &item.Body, &prim, &item.summaryPrefix, &item.Project, &item.ClaimCount, &item.CreatedAt, &rowid); err != nil {
				internalError(w, err)
				return
			}
			item.Worker = worker.String
			item.LeaseExpires = leaseExpires.Int64
			item.Primitives = prim
			next = listCursor{Rowid: rowid}
			tasks = append(tasks, item)
		}
		if err := rows.Err(); err != nil {
			internalError(w, err)
			return
		}

		// A short page means the query reached the end of the set, so the
		// rows in hand already give the total; only a full page can hide
		// more. An empty page behind an offset says nothing about what it
		// skipped, so that case still has to count.
		total := offset + len(tasks)
		if after == nil && (len(tasks) == limit || (offset > 0 && len(tasks) == 0)) {
			if err := db.ro.QueryRow("SELECT COUNT(*) FROM tasks"+whereSQL, whereArgs...).Scan(&total); err != nil {
				internalError(w, err)
				return
			}
		}

		var buf bytes.Buffer
		if requestedFields == nil {
			json.NewEncoder(&buf).Encode(tasks)
		} else {
			buf.WriteByte('[')
			for i, item := range tasks {
				if i > 0 {
					buf.WriteByte(',')
				}
				buf.WriteByte('{')
				for j, f := range requestedFields {
					if j > 0 {
						buf.WriteByte(',')
					}
					buf.WriteByte('"')
					buf.WriteString(f)
					buf.WriteString(`":`)
					switch f {
					case "id":
						b, _ := json.Marshal(item.ID)
						buf.Write(b)
					case "asset_path":
						b, _ := json.Marshal(item.AssetPath)
						buf.Write(b)
					case "status":
						b, _ := json.Marshal(item.Status)
						buf.Write(b)
					case "worker":
						b, _ := json.Marshal(item.Worker)
						buf.Write(b)
					case "lease_expires":
						buf.WriteString(strconv.FormatInt(item.LeaseExpires, 10))
					case "priority":
						buf.WriteString(strconv.Itoa(item.Priority))
					case "body":
						b, _ := json.Marshal(item.Body)
						buf.Write(b)
					case "primitives":
						if len(item.Primitives) > 0 {
							buf.Write(item.Primitives)
						} else {
							buf.WriteString("null")
						}
					case "project":
						b, _ := json.Marshal(item.Project)
						buf.Write(b)
					case "claim_count":
						buf.WriteString(strconv.Itoa(item.ClaimCount))
					case "created_at":
						buf.WriteString(strconv.FormatInt(item.CreatedAt, 10))
					case "summary":
						b, _ := json.Marshal(summaryLine(item.summaryPrefix))
						buf.Write(b)
					}
				}
				buf.WriteByte('}')
			}
			buf.WriteString("]\n")
		}

		h := fnv.New64a()
		h.Write(buf.Bytes())
		etag := fmt.Sprintf("\"%x\"", h.Sum64())
		w.Header().Set("Content-Type", "application/json")
		if after == nil {
			w.Header().Set("X-Total-Count", strconv.Itoa(total))
		}
		if len(tasks) == limit {
			next.Filters = listFilterFingerprint(q)
			w.Header().Set("X-Next-Cursor", encodeListCursor(next))
		}
		w.Header().Set("ETag", etag)

		if etagMatches(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Write(buf.Bytes())
	}

	getTaskHandler := func(w http.ResponseWriter, r *http.Request) {
		id, ok := resolveTaskIDHTTP(w, db.ro, r.PathValue("id"))
		if !ok {
			return
		}
		var (
			item         taskItem
			worker       sql.NullString
			leaseExpires sql.NullInt64
			prim         []byte
		)
		now := time.Now().Unix()
		err := db.ro.QueryRow(`SELECT id, asset_path,
  CASE WHEN status = 'leased' AND lease_expires < ? THEN 'pending' ELSE status END,
  CASE WHEN status = 'leased' AND lease_expires < ? THEN NULL ELSE worker END,
  CASE WHEN status = 'leased' AND lease_expires < ? THEN NULL ELSE lease_expires END,
  priority, body, primitives, project, claim_count, created_at FROM tasks WHERE id = ?`, now, now, now, id).
			Scan(&item.ID, &item.AssetPath, &item.Status, &worker, &leaseExpires, &item.Priority, &item.Body, &prim, &item.Project, &item.ClaimCount, &item.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		if err != nil {
			internalError(w, err)
			return
		}
		item.Worker = worker.String
		item.LeaseExpires = leaseExpires.Int64
		item.Primitives = prim

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(item)
	}
	projectsHandler := func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.ro.Query("SELECT DISTINCT project FROM tasks WHERE project != '' ORDER BY project ASC")
		if err != nil {
			internalError(w, err)
			return
		}
		defer rows.Close()

		projects := make([]string, 0)
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				internalError(w, err)
				return
			}
			projects = append(projects, p)
		}
		if err := rows.Err(); err != nil {
			internalError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(projects)
	}
	workersHandler := func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.ro.Query("SELECT DISTINCT worker FROM tasks WHERE worker IS NOT NULL AND worker != '' AND (status = 'done' OR (status = 'leased' AND lease_expires >= ?)) ORDER BY worker ASC", time.Now().Unix())
		if err != nil {
			internalError(w, err)
			return
		}
		defer rows.Close()

		workers := make([]string, 0)
		for rows.Next() {
			var wk string
			if err := rows.Scan(&wk); err != nil {
				internalError(w, err)
				return
			}
			workers = append(workers, wk)
		}
		if err := rows.Err(); err != nil {
			internalError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(workers)
	}
	patchTaskHandler := func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Body      *string `json:"body"`
			Priority  *int    `json:"priority"`
			Project   *string `json:"project"`
			AssetPath *string `json:"asset_path"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.Body == nil && req.Priority == nil && req.Project == nil && req.AssetPath == nil {
			writeError(w, http.StatusBadRequest, "missing fields to update")
			return
		}
		if req.Body != nil && *req.Body != "" && strings.TrimSpace(*req.Body) == "" {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
		if req.AssetPath != nil {
			*req.AssetPath = strings.TrimSpace(*req.AssetPath)
		}
		if req.Priority != nil && *req.Priority < 0 {
			writeError(w, http.StatusBadRequest, "invalid priority")
			return
		}
		if req.Project != nil {
			*req.Project = strings.TrimSpace(*req.Project)
			if !validProject(*req.Project) {
				writeError(w, http.StatusBadRequest, "invalid project")
				return
			}
		}
		clearBody := req.Body != nil && *req.Body == ""
		clearAsset := req.AssetPath != nil && *req.AssetPath == ""
		if clearBody && clearAsset {
			writeError(w, http.StatusBadRequest, "missing asset_path or body")
			return
		}
		id, ok := resolveTaskIDHTTP(w, db.ro, r.PathValue("id"))
		if !ok {
			return
		}
		if (clearBody && req.AssetPath == nil) || (clearAsset && req.Body == nil) {
			var curBody, curAssetPath string
			err := db.rw.QueryRow("SELECT body, asset_path FROM tasks WHERE id = ?", id).Scan(&curBody, &curAssetPath)
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "task not found")
				return
			}
			if err != nil {
				internalError(w, err)
				return
			}
			if (clearBody && curAssetPath == "") || (clearAsset && curBody == "") {
				writeError(w, http.StatusBadRequest, "missing asset_path or body")
				return
			}
		}
		res, err := db.rw.Exec(`UPDATE tasks
SET body = COALESCE(?, body), priority = COALESCE(?, priority), project = COALESCE(?, project), asset_path = COALESCE(?, asset_path)
WHERE id = ? AND status != 'done' AND NOT (status = 'leased' AND lease_expires >= unixepoch())`,
			req.Body, req.Priority, req.Project, req.AssetPath, id)
		if err != nil {
			internalError(w, err)
			return
		}
		n, err := res.RowsAffected()
		if err != nil {
			internalError(w, err)
			return
		}
		if n == 0 {
			writeTaskStateError(w, db.ro, id, "", nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}

	deleteTaskHandler := func(w http.ResponseWriter, r *http.Request) {
		tx, err := db.rw.Begin()
		if err != nil {
			internalError(w, err)
			return
		}
		defer tx.Rollback()

		id, ok := resolveTaskIDHTTP(w, tx, r.PathValue("id"))
		if !ok {
			return
		}

		var (
			status       string
			leaseExpires sql.NullInt64
		)
		err = tx.QueryRow("SELECT status, lease_expires FROM tasks WHERE id = ?", id).Scan(&status, &leaseExpires)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		if err != nil {
			internalError(w, err)
			return
		}
		if status == "leased" && leaseExpires.Int64 >= time.Now().Unix() {
			writeError(w, http.StatusConflict, "task is leased")
			return
		}
		f := requestQuery(r).Get("force")
		force := f == "1" || f == "true"
		if status == "done" && !force {
			writeError(w, http.StatusConflict, "task is done")
			return
		}
		if _, err := tx.Exec("DELETE FROM tasks WHERE id = ?", id); err != nil {
			internalError(w, err)
			return
		}
		if err := tx.Commit(); err != nil {
			internalError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}

	purgeTasksHandler := func(w http.ResponseWriter, r *http.Request) {
		q := requestQuery(r)
		var projectFilter string
		if q.Has("project") {
			p := q.Get("project")
			if p == "" {
				writeError(w, http.StatusBadRequest, "project cannot be empty")
				return
			}
			if p != "*" && !validProject(p) {
				writeError(w, http.StatusBadRequest, "invalid project")
				return
			}
			if p != "*" {
				projectFilter = p
			}
		}

		query := "DELETE FROM tasks WHERE status = 'done'"
		var args []any
		if projectFilter != "" {
			query += " AND project = ?"
			args = append(args, projectFilter)
		}
		res, err := db.rw.Exec(query, args...)
		if err != nil {
			internalError(w, err)
			return
		}
		deleted, err := res.RowsAffected()
		if err != nil {
			internalError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int64{"deleted": deleted})
	}
	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		if err := db.rw.PingContext(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "database ping failed")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{\"status\":\"ok\"}\n"))
	}

	handleMethods(mux, "/health", map[string]route{
		http.MethodGet: {handler: healthHandler, anyParams: true},
	})

	handleMethods(mux, "/stats", map[string]route{
		http.MethodGet: {handler: statsHandler, params: []string{"project", "worker"}},
	})
	handleMethods(mux, "/tasks", map[string]route{
		http.MethodGet: {handler: listTasksHandler, params: []string{
			"status", "project", "worker", "priority", "limit", "offset",
			"asset_path", "q", "fields", "columns", "after", "order",
		}},
		http.MethodPost: {handler: createTaskHandler},
	})
	handleMethods(mux, "/tasks/purge", map[string]route{
		http.MethodPost: {handler: purgeTasksHandler, params: []string{"project"}},
	})
	handleMethods(mux, "/tasks/claim", map[string]route{
		http.MethodPost: {handler: claimHandler},
	})
	handleMethods(mux, "/tasks/{id}/claim", map[string]route{
		http.MethodPost: {handler: claimIDHandler},
	})
	handleMethods(mux, "/tasks/{id}/done", map[string]route{
		http.MethodPost: {handler: doneIDHandler},
	})
	handleMethods(mux, "/tasks/{id}/close", map[string]route{
		http.MethodPost: {handler: closeIDHandler},
	})
	handleMethods(mux, "/tasks/{id}/touch", map[string]route{
		http.MethodPost: {handler: touchIDHandler},
	})
	handleMethods(mux, "/tasks/{id}/release", map[string]route{
		http.MethodPost: {handler: releaseIDHandler},
	})
	handleMethods(mux, "/tasks/{id}/bury", map[string]route{
		http.MethodPost: {handler: buryIDHandler},
	})
	handleMethods(mux, "/tasks/{id}/kick", map[string]route{
		http.MethodPost: {handler: kickIDHandler},
	})
	handleMethods(mux, "/tasks/{id}", map[string]route{
		http.MethodGet:    {handler: getTaskHandler},
		http.MethodPatch:  {handler: patchTaskHandler},
		http.MethodDelete: {handler: deleteTaskHandler, params: []string{"force"}},
	})
	handleMethods(mux, "/projects", map[string]route{
		http.MethodGet: {handler: projectsHandler},
	})
	handleMethods(mux, "/workers", map[string]route{
		http.MethodGet: {handler: workersHandler},
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if corsOrigin != "" && corsOrigin != "*" {
			w.Header().Add("Vary", "Origin")
		}
		origin := r.Header.Get("Origin")
		originMatched := corsOrigin != "" && (corsOrigin == "*" || origin == corsOrigin)
		if originMatched {
			w.Header().Set("Access-Control-Allow-Origin", corsOrigin)
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, If-None-Match")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.Header().Set("Access-Control-Expose-Headers", "X-Total-Count, X-Next-Cursor, ETag, Location")
		}
		if strings.HasSuffix(r.URL.Path, "/") && r.URL.Path != "/" {
			cleanReq := *r
			cleanURL := *r.URL
			cleanURL.Path = strings.TrimSuffix(cleanURL.Path, "/")
			if cleanURL.RawPath != "" {
				cleanURL.RawPath = strings.TrimSuffix(cleanURL.RawPath, "/")
			}
			cleanReq.URL = &cleanURL
			if _, matched := mux.Handler(&cleanReq); matched != "" && matched != "/" {
				target := cleanURL.EscapedPath()
				if strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "//") {
					redirectWithQuery(w, r, target, http.StatusPermanentRedirect)
					return
				}
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		mux.ServeHTTP(w, r)
	})
}

type config struct {
	dbPath     string
	addr       string
	lease      int
	maxClaims  int
	backupPath string
	corsOrigin string
	version    bool
}

func printUsage(w io.Writer) {
	cfg := config{
		dbPath:    defaultDBPath,
		addr:      defaultAddr,
		lease:     defaultLease,
		maxClaims: defaultMaxClaims,
	}
	fs := newFlagSet(&cfg)
	fmt.Fprintf(w, "Usage of %s:\n\n", fs.Name())
	fmt.Fprintf(w, "taskd is a lightweight task queue daemon backed by SQLite.\n\nOptions:\n")
	fs.SetOutput(w)
	fs.PrintDefaults()
	fmt.Fprintf(w, `
Environment variables:
  TASKD_ADDR          listen address (default: 127.0.0.1:8080)
  TASKD_DB            database path (default: taskd.db)
  TASKD_LEASE         lease duration in seconds (default: 300)
  TASKD_MAX_CLAIMS    bury a task after this many claims (default: 0)

HTTP Endpoints:
  GET    /health             daemon readiness and database ping
  GET    /tasks              list tasks
         ?status=            pending | leased | done | buried | live
         ?project=           exact match; project=* matches all projects
         ?worker=            exact match; empty value selects unassigned
         ?priority=          integer >= 0
         ?limit=             1..1000, default 100
         ?offset=            integer >= 0
         ?after=             opaque cursor token from X-Next-Cursor
         ?order=             asc | desc, default asc
         ?asset_path=        exact match
         ?q=                 substring of id, body, project, worker, or asset_path
         ?fields=            comma list from id, asset_path, status, worker,
                             lease_expires, priority, body, primitives,
                             project, claim_count, summary, created_at
         ?columns=           alias for fields
  POST   /tasks              create a task (requires project, body/asset_path)
  POST   /tasks/claim        claim next pending task (requires worker, optional project, optional wait)
  GET    /tasks/{id}         get task details
  PATCH  /tasks/{id}         update task (requires body, priority, project, or asset_path)
  POST   /tasks/{id}/claim   claim a specific task (requires worker)
  POST   /tasks/{id}/done    complete task with primitives (requires worker, optional claim_count)
  POST   /tasks/{id}/close   close task without result
  POST   /tasks/{id}/touch   extend lease, return expiration (requires worker)
  POST   /tasks/{id}/release release task back to pending (requires worker)
  POST   /tasks/{id}/bury    park a blocked task (requires worker, optional priority, optional claim_count)
  POST   /tasks/{id}/kick    return a parked task to pending
  DELETE /tasks/{id}         delete task (?force=1 to delete done task)
  POST   /tasks/purge        bulk-delete completed tasks (optional ?project=)
  GET    /projects           list active projects
  GET    /workers            list active workers
  GET    /stats              task queue statistics
         ?project=           exact match; project=* matches all projects
         ?worker=            exact match; empty value selects unassigned
  GET    /ui                 web interface

Examples:
  taskd                                      run daemon on 127.0.0.1:8080 with taskd.db
  taskd -addr :8080                          expose daemon on all interfaces
  taskd -addr :9090 -db custom.db            run on custom port and database
  taskd -lease 600                           use 10 minute task lease duration
  taskd -max-claims 3                        bury a task after 3 claims
  taskd -backup backup.db                    backup database to file and exit

  # Task lifecycle (create, claim, complete):
  T=${T:-http://localhost:8080}
  curl -s -XPOST $T/tasks -d '{"id":"t1","body":"hello","project":"demo"}'
  curl -s -XPOST $T/tasks/claim -d '{"worker":"me","project":"demo"}'
  curl -s -i -XPOST $T/tasks/t1/done -d '{"worker":"me"}'
`)
}

type usageError struct {
	err error
}

func (e *usageError) Error() string { return e.err.Error() }

func (e *usageError) Unwrap() error { return e.err }

func usagef(format string, a ...any) *usageError {
	return &usageError{err: fmt.Errorf(format, a...)}
}

const undefinedFlagPrefix = "flag provided but not defined: -"

func typedFlag(args []string, name string) string {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		token, _, _ := strings.Cut(arg, "=")
		if len(token) > 1 && token[0] == '-' && strings.TrimLeft(token, "-") == name {
			return token
		}
	}
	return "-" + name
}

const (
	defaultDBPath    = "taskd.db"
	defaultAddr      = "127.0.0.1:8080"
	defaultLease     = 300
	defaultMaxClaims = 0
)

func newFlagSet(cfg *config) *flag.FlagSet {
	fs := flag.NewFlagSet("taskd", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.StringVar(&cfg.dbPath, "db", cfg.dbPath, "database path")
	fs.StringVar(&cfg.addr, "addr", cfg.addr, "listen address (e.g. :8080 to expose on all interfaces)")
	fs.IntVar(&cfg.lease, "lease", cfg.lease, "lease duration in seconds")
	fs.IntVar(&cfg.maxClaims, "max-claims", cfg.maxClaims, "bury a task after this many claims (0 = unlimited)")
	fs.StringVar(&cfg.backupPath, "backup", "", "backup destination path")
	fs.StringVar(&cfg.corsOrigin, "cors-origin", "", "allowed CORS origin")
	fs.BoolVar(&cfg.version, "v", false, "print version and exit")
	fs.BoolVar(&cfg.version, "version", false, "print version and exit")
	return fs
}

const maxLeaseSeconds = 31536000

func parseFlags(args []string) (config, error) {
	cfg := config{
		dbPath:    defaultDBPath,
		addr:      defaultAddr,
		lease:     defaultLease,
		maxClaims: defaultMaxClaims,
	}
	if v := os.Getenv("TASKD_DB"); v != "" {
		cfg.dbPath = v
	}
	if v := os.Getenv("TASKD_ADDR"); v != "" {
		cfg.addr = v
	}
	if raw := os.Getenv("TASKD_LEASE"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return cfg, usagef("invalid value %q for TASKD_LEASE: %w", raw, err)
		}
		if v <= 0 || v > maxLeaseSeconds {
			return cfg, usagef("TASKD_LEASE must be between 1 and %d seconds: got %d", maxLeaseSeconds, v)
		}
		cfg.lease = v
	}
	if raw := os.Getenv("TASKD_MAX_CLAIMS"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return cfg, usagef("invalid value %q for TASKD_MAX_CLAIMS: %w", raw, err)
		}
		if v < 0 {
			return cfg, usagef("TASKD_MAX_CLAIMS cannot be negative: got %d", v)
		}
		cfg.maxClaims = v
	}

	fs := newFlagSet(&cfg)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return cfg, err
		}
		if name, ok := strings.CutPrefix(err.Error(), undefinedFlagPrefix); ok {
			return cfg, usagef("unrecognized flag %s", typedFlag(args, name))
		}
		return cfg, usagef("%w", err)
	}
	if len(fs.Args()) > 0 {
		return cfg, usagef("unexpected argument: %s", fs.Args()[0])
	}
	if cfg.version {
		return cfg, nil
	}
	cfg.dbPath = strings.TrimSpace(cfg.dbPath)
	if cfg.dbPath == "" {
		return cfg, usagef("database path cannot be empty")
	}
	cfg.addr = strings.TrimSpace(cfg.addr)
	if cfg.addr == "" {
		return cfg, usagef("listen address cannot be empty")
	}
	if cfg.lease <= 0 || cfg.lease > maxLeaseSeconds {
		return cfg, usagef("-lease must be between 1 and %d seconds: got %d", maxLeaseSeconds, cfg.lease)
	}
	if cfg.maxClaims < 0 {
		return cfg, usagef("max claims cannot be negative: got %d", cfg.maxClaims)
	}
	return cfg, nil
}

var errCheckpointBusy = errors.New("wal checkpoint busy")

func checkpointWAL(ctx context.Context, db *sql.DB) error {
	var busy, log, ckpt int
	if err := db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &log, &ckpt); err != nil {
		return err
	}
	if busy != 0 {
		return errCheckpointBusy
	}
	return nil
}

func sweepExpired(db *sql.DB, maxClaims int) ([]string, error) {
	const query = `UPDATE tasks
SET status = CASE WHEN ? > 0 AND claim_count >= ? THEN 'buried' ELSE 'pending' END,
    worker = NULL,
    lease_expires = NULL
WHERE (status = 'leased' AND lease_expires < unixepoch())
   OR (? > 0 AND status = 'pending' AND claim_count >= ?)
RETURNING status, project`
	rows, err := db.Query(query, maxClaims, maxClaims, maxClaims, maxClaims)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var projects []string
	for rows.Next() {
		var status, project string
		if err := rows.Scan(&status, &project); err != nil {
			return nil, err
		}
		if status == "pending" {
			projects = append(projects, project)
		}
	}
	return projects, rows.Err()
}

func runLeaseSweeper(ctx context.Context, s *store, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if projects, err := s.sweep(); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("lease sweeper error: %v", err)
			} else {
				for _, p := range projects {
					s.notifyPending(p)
				}
			}
		}
	}
}

func runCheckpointer(ctx context.Context, db *sql.DB, interval time.Duration, onTick func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := checkpointWAL(ctx, db); err != nil && !errors.Is(err, errCheckpointBusy) && !errors.Is(err, context.Canceled) {
				log.Printf("checkpoint error: %v", err)
			}
			if onTick != nil {
				onTick()
			}
		}
	}
}

func runServer(ctx context.Context, l net.Listener, db *store, lease int, corsOrigin string) error {
	srv := &http.Server{
		Handler:           newHandlerWithCORS(db, lease, corsOrigin),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverCtx, cancelServer := context.WithCancel(ctx)
	defer cancelServer()
	sweepFn := func() {
		sweepLapsed(db.rw, db.maxClaims, "")
	}
	go runCheckpointer(serverCtx, db.rw, defaultCheckpointInterval, sweepFn)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
		if err := checkpointWAL(context.Background(), db.rw); err != nil {
			log.Printf("shutdown checkpoint error: %v", err)
		}
		return <-errCh
	}
}

func checkBackupSource(path string) error {
	fsPath, memory := resolveDBPath(path)
	if memory {
		return fmt.Errorf("cannot back up an in-memory database: %s", path)
	}
	if fsPath == "" {
		return fmt.Errorf("cannot resolve database path: %s", path)
	}
	if _, err := os.Stat(fsPath); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("database does not exist: %s", fsPath)
	} else if err != nil {
		return fmt.Errorf("cannot access database %s: %w", fsPath, err)
	}
	return nil
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
	if err := os.Rename(tmpPath, fsPath); err != nil {
		return err
	}
	if err := reportBackup(fsPath, out); err != nil {
		log.Printf("cannot report backup %s: %v", fsPath, err)
	}
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

func reportBackup(fsPath string, out io.Writer) error {
	fi, err := os.Stat(fsPath)
	if err != nil {
		return err
	}
	bk, err := openDBConn(fsPath, true)
	if err != nil {
		return err
	}
	bk.SetMaxOpenConns(1)
	defer bk.Close()
	var tasks int
	if err := bk.QueryRow("SELECT count(*) FROM tasks").Scan(&tasks); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "wrote %s (%d bytes, %d tasks)\n", fsPath, fi.Size(), tasks)
	return nil
}

func listen(addr string) (net.Listener, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	if tcp, ok := l.Addr().(*net.TCPAddr); ok && tcp.IP.IsUnspecified() {
		log.Print("warning: listening on all interfaces; taskd has no authentication")
	}
	log.Printf("listening on %s", l.Addr())
	return l, nil
}
func openReadOnlyDB(path string) (db *sql.DB, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("cannot open database %s: %w", path, err)
		}
	}()
	db, err = openDBConn(path, true)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func run(stdout io.Writer, args []string) error {
	cfg, err := parseFlags(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			return nil
		}
		return err
	}
	if cfg.version {
		fmt.Fprintf(stdout, "taskd %s\n", version.Version)
		return nil
	}

	if cfg.backupPath != "" {
		log.SetFlags(0)
		if err := checkBackupSource(cfg.dbPath); err != nil {
			return err
		}
		db, err := openReadOnlyDB(cfg.dbPath)
		if err != nil {
			return err
		}
		db.SetMaxOpenConns(1)
		defer db.Close()
		return backupDB(db, cfg.backupPath, os.Stdout)
	}

	db, err := openDB(cfg.dbPath, cfg.maxClaims)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	l, err := listen(cfg.addr)
	if err != nil {
		return err
	}

	return runServer(ctx, l, db, cfg.lease, cfg.corsOrigin)
}

func fatal(stderr io.Writer, err error) int {
	var ue *usageError
	if errors.As(err, &ue) {
		fmt.Fprintf(stderr, "taskd: %v\ntry 'taskd -h' for usage\n", err)
		return 2
	}
	fmt.Fprintf(stderr, "taskd: %v\n", err)
	var se *sqlite.Error
	if errors.As(err, &se) && (se.Code() == sqlite3.SQLITE_CANTOPEN || se.Code()&0xff == sqlite3.SQLITE_CANTOPEN) {
		fmt.Fprintln(stderr, "       is -db pointing at a directory, or a path you cannot write?")
	}
	return 1
}

func main() {
	if err := run(os.Stdout, os.Args[1:]); err != nil {
		os.Exit(fatal(os.Stderr, err))
	}
}
