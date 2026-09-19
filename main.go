package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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
	"syscall"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func dbDir(path string) string {
	if path == "" || path == ":memory:" {
		return ""
	}
	if !strings.HasPrefix(path, "file:") {
		return filepath.Dir(path)
	}
	u, err := url.Parse(path)
	if err != nil {
		return ""
	}
	if u.Query().Get("mode") == "memory" {
		return ""
	}
	p := u.Path
	if p == "" {
		p = u.Opaque
	}
	if p == "" || p == ":memory:" {
		return ""
	}
	return filepath.Dir(p)
}

var migrations = [...]func(*sql.DB) error{migrateV2, migrateV3, migrateV4}

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

func openDB(path string) (*sql.DB, error) {
	if dir := dbDir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
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
	q.Add("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		db.Close()
		return nil, err
	}
	if version < 0 || version > schemaVersion {
		db.Close()
		return nil, fmt.Errorf("database %s has schema version %d, this binary supports %d", path, version, schemaVersion)
	}
	const fullSchema = "CREATE TABLE IF NOT EXISTS tasks (" + taskColumns + `);
DROP INDEX IF EXISTS idx_tasks_claim;
CREATE INDEX IF NOT EXISTS idx_tasks_queue ON tasks (status, priority ASC);
CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks (project, status, priority ASC);`
	hasTasks, err := rowExists(db, "SELECT 1 FROM sqlite_master WHERE type='table' AND name='tasks'")
	if err != nil {
		db.Close()
		return nil, err
	}
	if !hasTasks {
		if version != 0 {
			db.Close()
			return nil, fmt.Errorf("database %s has schema version %d but no tasks table", path, version)
		}
		used, err := rowExists(db, "SELECT 1 FROM sqlite_master LIMIT 1")
		if err != nil {
			db.Close()
			return nil, err
		}
		if used {
			db.Close()
			return nil, fmt.Errorf("database %s is not a taskd database", path)
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
		src("worker", "NULL") + ", " +
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

const taskColumns = `
  id TEXT PRIMARY KEY,
  asset_path TEXT NOT NULL DEFAULT '',
  status TEXT DEFAULT 'pending',
  worker TEXT,
  lease_expires INTEGER,
  primitives JSON,
  body TEXT NOT NULL DEFAULT '',
  priority INTEGER NOT NULL DEFAULT 3 CHECK (priority >= 0),
  project TEXT NOT NULL DEFAULT '',
  claim_count INTEGER NOT NULL DEFAULT 0`

const defaultPriority = 3

const maxBodyBytes = 1 << 20

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	var maxErr *http.MaxBytesError
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.As(err, &maxErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return false
		}
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

func internalError(w http.ResponseWriter, err error) {
	log.Print(err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func cleanWorker(raw string) (string, bool) {
	worker := strings.TrimSpace(raw)
	return worker, worker != ""
}

func decodeWorker(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		Worker string `json:"worker"`
	}
	if !decodeJSON(w, r, &req) {
		return "", false
	}
	worker, ok := cleanWorker(req.Worker)
	if !ok {
		http.Error(w, "missing worker", http.StatusBadRequest)
		return "", false
	}
	return worker, true
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
}

func (t *taskItem) normalize(now int64) {
	if t.Status == "leased" && t.LeaseExpires < now {
		t.Status = "pending"
	}
	if t.Status == "pending" {
		t.Worker = ""
		t.LeaseExpires = 0
	}
}

const maxTaskIDLen = 128
const maxProjectLen = 64

func validTaskID(id string) bool {
	if id == "" || len(id) > maxTaskIDLen || id == "." || id == ".." {
		return false
	}
	if strings.EqualFold(id, "claim") || strings.EqualFold(id, "purge") {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func validProject(p string) bool {
	return p != "" && p != "*" && len(p) <= maxProjectLen
}

//go:embed web/index.html
var uiHTML []byte

func newHandler(db *sql.DB, lease int) http.Handler {
	return newHandlerWithCORS(db, lease, "")
}

func newHandlerWithCORS(db *sql.DB, lease int, corsOrigin string) http.Handler {
	mux := http.NewServeMux()

	uiHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(uiHTML)
	}
	mux.HandleFunc("GET /ui", uiHandler)
	mux.HandleFunc("GET /ui/", uiHandler)
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		project := r.URL.Query().Get("project")
		now := time.Now().Unix()
		query := `SELECT
  COUNT(CASE WHEN status = 'pending' OR (status = 'leased' AND lease_expires < ?) THEN 1 END),
  COUNT(CASE WHEN status = 'leased' AND lease_expires >= ? THEN 1 END),
  COUNT(CASE WHEN status = 'done' THEN 1 END),
  COUNT(*)
FROM tasks`
		var args []any
		args = append(args, now, now)
		if project != "" && project != "*" {
			query += " WHERE project = ?"
			args = append(args, project)
		}
		var pending, leased, done, total int
		if err := db.QueryRow(query, args...).Scan(&pending, &leased, &done, &total); err != nil {
			internalError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{
			"pending": pending,
			"leased":  leased,
			"done":    done,
			"total":   total,
		})
	})
	mux.HandleFunc("POST /tasks", func(w http.ResponseWriter, r *http.Request) {
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
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if req.AssetPath == "" && req.Body == "" {
			http.Error(w, "missing asset_path or body", http.StatusBadRequest)
			return
		}
		if !validProject(req.Project) {
			http.Error(w, "invalid project", http.StatusBadRequest)
			return
		}
		priority := defaultPriority
		if req.Priority != nil {
			if *req.Priority < 0 {
				http.Error(w, "invalid priority", http.StatusBadRequest)
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
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		_, err := db.Exec("INSERT INTO tasks (id, asset_path, body, priority, project) VALUES (?, ?, ?, ?, ?)", req.ID, req.AssetPath, req.Body, priority, req.Project)
		if err != nil {
			var se *sqlite.Error
			if errors.As(err, &se) && (se.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY || se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE) {
				http.Error(w, "duplicate id", http.StatusConflict)
				return
			}
			internalError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": req.ID})
	})

	mux.HandleFunc("POST /tasks/claim", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Worker  string `json:"worker"`
			Project string `json:"project"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		var ok bool
		if req.Worker, ok = cleanWorker(req.Worker); !ok {
			http.Error(w, "missing worker", http.StatusBadRequest)
			return
		}
		req.Project = strings.TrimSpace(req.Project)
		if req.Project == "" {
			req.Project = "*"
		}
		query := `UPDATE tasks SET status='leased', worker=?, lease_expires=unixepoch()+?, claim_count=claim_count+1
WHERE id = (
  SELECT id FROM tasks
  WHERE (status='pending' OR (status='leased' AND lease_expires < unixepoch()))`
		args := []any{req.Worker, lease}
		if req.Project != "*" {
			query += " AND project = ?"
			args = append(args, req.Project)
		}
		query += `
  ORDER BY priority ASC, rowid ASC
  LIMIT 1
) RETURNING id, asset_path, body, priority, project, claim_count, status, lease_expires`
		var (
			id, assetPath, body, project, status string
			priority, claimCount                 int
			leaseExpires                         int64
		)
		err := db.QueryRow(query, args...).Scan(&id, &assetPath, &body, &priority, &project, &claimCount, &status, &leaseExpires)
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err != nil {
			internalError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			ID           string `json:"id"`
			AssetPath    string `json:"asset_path"`
			Body         string `json:"body"`
			Priority     int    `json:"priority"`
			Project      string `json:"project"`
			ClaimCount   int    `json:"claim_count"`
			Status       string `json:"status"`
			LeaseExpires int64  `json:"lease_expires"`
		}{
			ID:           id,
			AssetPath:    assetPath,
			Body:         body,
			Priority:     priority,
			Project:      project,
			ClaimCount:   claimCount,
			Status:       status,
			LeaseExpires: leaseExpires,
		})
	})
	mux.HandleFunc("POST /tasks/{id}/claim", func(w http.ResponseWriter, r *http.Request) {
		worker, ok := decodeWorker(w, r)
		if !ok {
			return
		}
		id := r.PathValue("id")
		query := `UPDATE tasks
SET status='leased', worker=?, lease_expires=unixepoch()+?, claim_count=claim_count+1
WHERE id = ? AND (status='pending' OR (status='leased' AND lease_expires < unixepoch()))
RETURNING id, asset_path, status, worker, lease_expires, priority, body, primitives, project, claim_count`
		var (
			item         taskItem
			leasedWorker sql.NullString
			leaseExpires sql.NullInt64
			prim         []byte
		)
		err := db.QueryRow(query, worker, lease, id).
			Scan(&item.ID, &item.AssetPath, &item.Status, &leasedWorker, &leaseExpires, &item.Priority, &item.Body, &prim, &item.Project, &item.ClaimCount)
		if errors.Is(err, sql.ErrNoRows) {
			var status string
			err := db.QueryRow("SELECT status FROM tasks WHERE id = ?", id).Scan(&status)
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "task not found", http.StatusNotFound)
				return
			}
			if err != nil {
				internalError(w, err)
				return
			}
			if status == "done" {
				http.Error(w, "task is done", http.StatusConflict)
				return
			}
			http.Error(w, "task is leased", http.StatusConflict)
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
	})

	mux.HandleFunc("POST /tasks/{id}/done", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Worker     string          `json:"worker"`
			Primitives json.RawMessage `json:"primitives"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		var ok bool
		if req.Worker, ok = cleanWorker(req.Worker); !ok {
			http.Error(w, "missing worker", http.StatusBadRequest)
			return
		}
		var prim any
		if len(req.Primitives) > 0 {
			prim = string(req.Primitives)
		}
		res, err := db.Exec("UPDATE tasks SET status='done', primitives=? WHERE id=? AND status='leased' AND worker=? AND lease_expires >= unixepoch()", prim, r.PathValue("id"), req.Worker)
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
			var exists int
			err := db.QueryRow("SELECT 1 FROM tasks WHERE id = ?", r.PathValue("id")).Scan(&exists)
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "task not found", http.StatusNotFound)
				return
			}
			if err != nil {
				internalError(w, err)
				return
			}
			http.Error(w, "task not leased by worker", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /tasks/{id}/touch", func(w http.ResponseWriter, r *http.Request) {
		worker, ok := decodeWorker(w, r)
		if !ok {
			return
		}
		res, err := db.Exec("UPDATE tasks SET lease_expires = unixepoch() + ? WHERE id = ? AND status = 'leased' AND worker = ? AND lease_expires >= unixepoch()", lease, r.PathValue("id"), worker)
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
			http.Error(w, "task not found or not leased by worker", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /tasks/{id}/release", func(w http.ResponseWriter, r *http.Request) {
		worker, ok := decodeWorker(w, r)
		if !ok {
			return
		}
		res, err := db.Exec("UPDATE tasks SET status='pending', worker=NULL, lease_expires=NULL WHERE id=? AND status='leased' AND worker=? AND lease_expires >= unixepoch()", r.PathValue("id"), worker)
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
			http.Error(w, "task not found or not leased by worker", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		for k := range q {
			switch k {
			case "status", "project", "worker", "priority", "limit", "offset", "asset_path":
			default:
				http.Error(w, fmt.Sprintf("unknown query parameter: %s", k), http.StatusBadRequest)
				return
			}
		}
		status := q.Get("status")
		if q.Has("status") && status != "pending" && status != "leased" && status != "done" {
			http.Error(w, "invalid status", http.StatusBadRequest)
			return
		}
		project := q.Get("project")
		var priorityFilter *int
		if q.Has("priority") {
			v, err := strconv.Atoi(q.Get("priority"))
			if err != nil || v < 0 {
				http.Error(w, "invalid priority", http.StatusBadRequest)
				return
			}
			priorityFilter = &v
		}
		limit := 100
		if q.Has("limit") {
			v, err := strconv.Atoi(q.Get("limit"))
			if err != nil || v < 1 || v > 1000 {
				http.Error(w, "invalid limit", http.StatusBadRequest)
				return
			}
			limit = v
		}
		offset := 0
		if q.Has("offset") {
			v, err := strconv.Atoi(q.Get("offset"))
			if err != nil || v < 0 {
				http.Error(w, "invalid offset", http.StatusBadRequest)
				return
			}
			offset = v
		}
		now := time.Now().Unix()
		query := "SELECT id, asset_path, status, worker, lease_expires, priority, body, primitives, project, claim_count FROM tasks"
		var where []string
		var args []any
		if status == "pending" {
			where = append(where, "(status = 'pending' OR (status = 'leased' AND lease_expires < ?))")
			args = append(args, now)
		} else if status == "leased" {
			where = append(where, "(status = 'leased' AND lease_expires >= ?)")
			args = append(args, now)
		} else if status != "" {
			where = append(where, "status = ?")
			args = append(args, status)
		}
		if project != "" && project != "*" {
			where = append(where, "project = ?")
			args = append(args, project)
		}
		if q.Has("worker") {
			worker := strings.TrimSpace(q.Get("worker"))
			if worker != "" {
				where = append(where, "worker = ? AND (status = 'done' OR (status = 'leased' AND lease_expires >= ?))")
				args = append(args, worker, now)
			} else {
				where = append(where, "(status = 'pending' OR (status = 'leased' AND lease_expires < ?))")
				args = append(args, now)
			}
		}
		if priorityFilter != nil {
			where = append(where, "priority = ?")
			args = append(args, *priorityFilter)
		}
		if q.Has("asset_path") {
			where = append(where, "asset_path = ?")
			args = append(args, q.Get("asset_path"))
		}
		var whereSQL string
		if len(where) > 0 {
			whereSQL = " WHERE " + strings.Join(where, " AND ")
		}
		countArgs := slices.Clone(args)
		query += whereSQL
		query += " ORDER BY priority ASC, rowid ASC LIMIT ?"
		args = append(args, limit)
		if offset > 0 {
			query += " OFFSET ?"
			args = append(args, offset)
		}
		rows, err := db.Query(query, args...)
		if err != nil {
			internalError(w, err)
			return
		}
		defer rows.Close()

		tasks := make([]taskItem, 0)
		for rows.Next() {
			var (
				item         taskItem
				worker       sql.NullString
				leaseExpires sql.NullInt64
				prim         []byte
			)
			if err := rows.Scan(&item.ID, &item.AssetPath, &item.Status, &worker, &leaseExpires, &item.Priority, &item.Body, &prim, &item.Project, &item.ClaimCount); err != nil {
				internalError(w, err)
				return
			}
			item.Worker = worker.String
			item.LeaseExpires = leaseExpires.Int64
			item.Primitives = prim
			item.normalize(now)
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
		if len(tasks) == limit || (offset > 0 && len(tasks) == 0) {
			if err := db.QueryRow("SELECT COUNT(*) FROM tasks"+whereSQL, countArgs...).Scan(&total); err != nil {
				internalError(w, err)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Total-Count", strconv.Itoa(total))
		json.NewEncoder(w).Encode(tasks)
	})

	mux.HandleFunc("GET /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		var (
			item         taskItem
			worker       sql.NullString
			leaseExpires sql.NullInt64
			prim         []byte
		)
		err := db.QueryRow("SELECT id, asset_path, status, worker, lease_expires, priority, body, primitives, project, claim_count FROM tasks WHERE id = ?", r.PathValue("id")).
			Scan(&item.ID, &item.AssetPath, &item.Status, &worker, &leaseExpires, &item.Priority, &item.Body, &prim, &item.Project, &item.ClaimCount)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "task not found", http.StatusNotFound)
			return
		}
		if err != nil {
			internalError(w, err)
			return
		}
		item.Worker = worker.String
		item.LeaseExpires = leaseExpires.Int64
		item.Primitives = prim
		item.normalize(time.Now().Unix())

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(item)
	})
	mux.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query("SELECT DISTINCT project FROM tasks WHERE project != '' ORDER BY project ASC")
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
	})
	mux.HandleFunc("PATCH /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
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
			http.Error(w, "missing fields to update", http.StatusBadRequest)
			return
		}
		if req.Body != nil && *req.Body != "" && strings.TrimSpace(*req.Body) == "" {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if req.AssetPath != nil {
			*req.AssetPath = strings.TrimSpace(*req.AssetPath)
		}
		if req.Priority != nil && *req.Priority < 0 {
			http.Error(w, "invalid priority", http.StatusBadRequest)
			return
		}
		if req.Project != nil {
			*req.Project = strings.TrimSpace(*req.Project)
			if !validProject(*req.Project) {
				http.Error(w, "invalid project", http.StatusBadRequest)
				return
			}
		}
		clearBody := req.Body != nil && *req.Body == ""
		clearAsset := req.AssetPath != nil && *req.AssetPath == ""
		if clearBody && clearAsset {
			http.Error(w, "missing asset_path or body", http.StatusBadRequest)
			return
		}
		if (clearBody && req.AssetPath == nil) || (clearAsset && req.Body == nil) {
			var curBody, curAssetPath string
			err := db.QueryRow("SELECT body, asset_path FROM tasks WHERE id = ?", r.PathValue("id")).Scan(&curBody, &curAssetPath)
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "task not found", http.StatusNotFound)
				return
			}
			if err != nil {
				internalError(w, err)
				return
			}
			if (clearBody && curAssetPath == "") || (clearAsset && curBody == "") {
				http.Error(w, "missing asset_path or body", http.StatusBadRequest)
				return
			}
		}
		res, err := db.Exec(`UPDATE tasks
SET body = COALESCE(?, body), priority = COALESCE(?, priority), project = COALESCE(?, project), asset_path = COALESCE(?, asset_path)
WHERE id = ? AND status != 'done' AND NOT (status = 'leased' AND lease_expires >= unixepoch())`,
			req.Body, req.Priority, req.Project, req.AssetPath, r.PathValue("id"))
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
			var status string
			err := db.QueryRow("SELECT status FROM tasks WHERE id = ?", r.PathValue("id")).Scan(&status)
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "task not found", http.StatusNotFound)
				return
			}
			if err != nil {
				internalError(w, err)
				return
			}
			if status == "done" {
				http.Error(w, "task is done", http.StatusConflict)
				return
			}
			http.Error(w, "task is leased", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		tx, err := db.Begin()
		if err != nil {
			internalError(w, err)
			return
		}
		defer tx.Rollback()

		var (
			status       string
			leaseExpires sql.NullInt64
		)
		err = tx.QueryRow("SELECT status, lease_expires FROM tasks WHERE id = ?", r.PathValue("id")).Scan(&status, &leaseExpires)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "task not found", http.StatusNotFound)
			return
		}
		if err != nil {
			internalError(w, err)
			return
		}
		if status == "leased" && leaseExpires.Int64 >= time.Now().Unix() {
			http.Error(w, "task is leased", http.StatusConflict)
			return
		}
		f := r.URL.Query().Get("force")
		force := f == "1" || f == "true"
		if status == "done" && !force {
			http.Error(w, "task is done", http.StatusConflict)
			return
		}
		if _, err := tx.Exec("DELETE FROM tasks WHERE id = ?", r.PathValue("id")); err != nil {
			internalError(w, err)
			return
		}
		if err := tx.Commit(); err != nil {
			internalError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if corsOrigin != "" && corsOrigin != "*" {
			w.Header().Add("Vary", "Origin")
		}
		origin := r.Header.Get("Origin")
		originMatched := corsOrigin != "" && (corsOrigin == "*" || origin == corsOrigin)
		if r.Method == http.MethodOptions && originMatched {
			w.Header().Set("Access-Control-Allow-Origin", corsOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Expose-Headers", "X-Total-Count")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if originMatched {
			w.Header().Set("Access-Control-Allow-Origin", corsOrigin)
			w.Header().Set("Access-Control-Expose-Headers", "X-Total-Count")
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		mux.ServeHTTP(w, r)
	})
}

type config struct {
	dbPath     string
	addr       string
	lease      int
	backupPath string
	corsOrigin string
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("taskd", flag.ContinueOnError)
	fs.StringVar(&cfg.dbPath, "db", "taskd.db", "database path")
	fs.StringVar(&cfg.addr, "addr", ":8080", "listen address")
	fs.IntVar(&cfg.lease, "lease", 300, "lease duration in seconds")
	fs.StringVar(&cfg.backupPath, "backup", "", "backup destination path")
	fs.StringVar(&cfg.corsOrigin, "cors-origin", "", "allowed CORS origin")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	cfg.dbPath = strings.TrimSpace(cfg.dbPath)
	if cfg.dbPath == "" {
		return cfg, errors.New("database path cannot be empty")
	}
	cfg.addr = strings.TrimSpace(cfg.addr)
	if cfg.addr == "" {
		return cfg, errors.New("listen address cannot be empty")
	}
	if cfg.lease <= 0 {
		return cfg, fmt.Errorf("lease duration must be greater than 0: got %d", cfg.lease)
	}
	return cfg, nil
}

func runServer(ctx context.Context, l net.Listener, db *sql.DB, lease int, corsOrigin string) error {
	srv := &http.Server{
		Handler:           newHandlerWithCORS(db, lease, corsOrigin),
		ReadHeaderTimeout: 5 * time.Second,
	}

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
		return <-errCh
	}
}

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		log.Fatal(err)
	}

	db, err := openDB(cfg.dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if cfg.backupPath != "" {
		if _, err := db.Exec("VACUUM INTO ?", cfg.backupPath); err != nil {
			log.Fatal(err)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	l, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("listening on %s", l.Addr())

	if err := runServer(ctx, l, db, cfg.lease, cfg.corsOrigin); err != nil {
		log.Fatal(err)
	}
}
