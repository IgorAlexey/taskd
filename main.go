package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func openDB(path string) (*sql.DB, error) {
	if dir := dbDir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}
	clean := strings.TrimPrefix(path, "file:")
	sep := "?"
	if strings.Contains(clean, "?") {
		sep = "&"
	}
	dsn := fmt.Sprintf("file:%s%s_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", clean, sep)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		db.Close()
		return nil, err
	}
	const fullSchema = `CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  asset_path TEXT NOT NULL DEFAULT '',
  status TEXT DEFAULT 'pending',
  worker TEXT,
  lease_expires INTEGER,
  primitives JSON,
  body TEXT NOT NULL DEFAULT '',
  priority INTEGER NOT NULL DEFAULT 0,
  project TEXT NOT NULL DEFAULT ''
);
DROP INDEX IF EXISTS idx_tasks_claim;
CREATE INDEX IF NOT EXISTS idx_tasks_queue ON tasks (status, priority DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks (project, status, priority DESC);`
	if version == 0 {
		var tableExists int
		if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='tasks'").Scan(&tableExists); err != nil {
			db.Close()
			return nil, err
		}
		if tableExists == 0 {
			if _, err := db.Exec(fullSchema + "\nPRAGMA user_version = 2;"); err != nil {
				db.Close()
				return nil, err
			}
			return db, nil
		}
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
	if version < 2 {
		if err := migrateV2(db); err != nil {
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

const maxBodyBytes = 1 << 20

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	var maxErr *http.MaxBytesError
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
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
}

func newHandler(db *sql.DB, lease int) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /tasks", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID        string `json:"id"`
			AssetPath string `json:"asset_path"`
			Body      string `json:"body"`
			Priority  int    `json:"priority"`
			Project   string `json:"project"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.AssetPath == "" && req.Body == "" {
			http.Error(w, "missing asset_path or body", http.StatusBadRequest)
			return
		}
		if req.Project == "" || req.Project == "*" {
			http.Error(w, "missing project", http.StatusBadRequest)
			return
		}
		if req.ID == "" {
			var b [16]byte
			if _, err := rand.Read(b[:]); err != nil {
				internalError(w, err)
				return
			}
			req.ID = hex.EncodeToString(b[:])
		}
		_, err := db.Exec("INSERT INTO tasks (id, asset_path, body, priority, project) VALUES (?, ?, ?, ?, ?)", req.ID, req.AssetPath, req.Body, req.Priority, req.Project)
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
		if req.Worker == "" {
			http.Error(w, "missing worker", http.StatusBadRequest)
			return
		}
		if req.Project == "" {
			req.Project = "*"
		}
		query := `UPDATE tasks SET status='leased', worker=?, lease_expires=unixepoch()+?
WHERE id = (
  SELECT id FROM tasks
  WHERE (status='pending' OR (status='leased' AND lease_expires < unixepoch()))`
		args := []any{req.Worker, lease}
		if req.Project != "*" {
			query += " AND project = ?"
			args = append(args, req.Project)
		}
		query += `
  ORDER BY priority DESC, rowid ASC
  LIMIT 1
) RETURNING id, asset_path, body, priority, project`
		var (
			id, assetPath, body, project string
			priority                     int
		)
		err := db.QueryRow(query, args...).Scan(&id, &assetPath, &body, &priority, &project)
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
			ID        string `json:"id"`
			AssetPath string `json:"asset_path"`
			Body      string `json:"body"`
			Priority  int    `json:"priority"`
			Project   string `json:"project"`
		}{
			ID:        id,
			AssetPath: assetPath,
			Body:      body,
			Priority:  priority,
			Project:   project,
		})
	})

	mux.HandleFunc("POST /tasks/{id}/done", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Worker     string          `json:"worker"`
			Primitives json.RawMessage `json:"primitives"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.Worker == "" {
			http.Error(w, "missing worker", http.StatusBadRequest)
			return
		}
		var prim any
		if len(req.Primitives) > 0 {
			prim = string(req.Primitives)
		}
		res, err := db.Exec("UPDATE tasks SET status='done', primitives=? WHERE id=? AND status='leased' AND worker=?", prim, r.PathValue("id"), req.Worker)
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
		status := q.Get("status")
		project := q.Get("project")
		limit := 100
		if q.Has("limit") {
			v, err := strconv.Atoi(q.Get("limit"))
			if err != nil || v < 1 || v > 1000 {
				http.Error(w, "invalid limit", http.StatusBadRequest)
				return
			}
			limit = v
		}
		query := "SELECT id, asset_path, status, worker, lease_expires, priority, body, primitives, project FROM tasks"
		var where []string
		var args []any
		if status != "" {
			where = append(where, "status = ?")
			args = append(args, status)
		}
		if project != "" && project != "*" {
			where = append(where, "project = ?")
			args = append(args, project)
		}
		if len(where) > 0 {
			query += " WHERE " + strings.Join(where, " AND ")
		}
		query += " ORDER BY priority DESC, rowid ASC LIMIT ?"
		args = append(args, limit)
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
			if err := rows.Scan(&item.ID, &item.AssetPath, &item.Status, &worker, &leaseExpires, &item.Priority, &item.Body, &prim, &item.Project); err != nil {
				internalError(w, err)
				return
			}
			item.Worker = worker.String
			item.LeaseExpires = leaseExpires.Int64
			item.Primitives = prim
			tasks = append(tasks, item)
		}
		if err := rows.Err(); err != nil {
			internalError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tasks)
	})

	mux.HandleFunc("GET /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		var (
			item         taskItem
			worker       sql.NullString
			leaseExpires sql.NullInt64
			prim         []byte
		)
		err := db.QueryRow("SELECT id, asset_path, status, worker, lease_expires, priority, body, primitives, project FROM tasks WHERE id = ?", r.PathValue("id")).
			Scan(&item.ID, &item.AssetPath, &item.Status, &worker, &leaseExpires, &item.Priority, &item.Body, &prim, &item.Project)
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

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(item)
	})
	mux.HandleFunc("PATCH /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Body     *string `json:"body"`
			Priority *int    `json:"priority"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.Body == nil && req.Priority == nil {
			http.Error(w, "missing fields to update", http.StatusBadRequest)
			return
		}
		res, err := db.Exec(`UPDATE tasks
SET body = COALESCE(?, body), priority = COALESCE(?, priority)
WHERE id = ? AND NOT (status = 'leased' AND lease_expires >= unixepoch())`,
			req.Body, req.Priority, r.PathValue("id"))
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
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		mux.ServeHTTP(w, r)
	})
}

type config struct {
	dbPath string
	addr   string
	lease  int
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("taskd", flag.ContinueOnError)
	fs.StringVar(&cfg.dbPath, "db", "taskd.db", "database path")
	fs.StringVar(&cfg.addr, "addr", ":8080", "listen address")
	fs.IntVar(&cfg.lease, "lease", 300, "lease duration in seconds")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if cfg.lease <= 0 {
		return cfg, fmt.Errorf("lease duration must be greater than 0: got %d", cfg.lease)
	}
	return cfg, nil
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

	log.Fatal(http.ListenAndServe(cfg.addr, newHandler(db, cfg.lease)))
}
