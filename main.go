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
	"strconv"
	"strings"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func openDB(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", strings.TrimPrefix(path, "file:"))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	const schema = `CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  asset_path TEXT NOT NULL DEFAULT '',
  status TEXT DEFAULT 'pending',
  worker TEXT,
  lease_expires INTEGER,
  primitives JSON,
  body TEXT NOT NULL DEFAULT '',
  priority INTEGER NOT NULL DEFAULT 0
);
DROP INDEX IF EXISTS idx_tasks_claim;
CREATE INDEX IF NOT EXISTS idx_tasks_queue ON tasks (status, priority DESC);`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
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

func newHandler(db *sql.DB, lease int) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /tasks", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID        string `json:"id"`
			AssetPath string `json:"asset_path"`
			Body      string `json:"body"`
			Priority  int    `json:"priority"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.AssetPath == "" && req.Body == "" {
			http.Error(w, "missing asset_path or body", http.StatusBadRequest)
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
		_, err := db.Exec("INSERT INTO tasks (id, asset_path, body, priority) VALUES (?, ?, ?, ?)", req.ID, req.AssetPath, req.Body, req.Priority)
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
			Worker string `json:"worker"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.Worker == "" {
			http.Error(w, "missing worker", http.StatusBadRequest)
			return
		}
		query := `UPDATE tasks SET status='leased', worker=?, lease_expires=unixepoch()+?
WHERE id = (
  SELECT id FROM tasks
  WHERE status='pending' OR (status='leased' AND lease_expires < unixepoch())
  ORDER BY priority DESC, rowid ASC
  LIMIT 1
) RETURNING id, asset_path, body, priority`
		var (
			id, assetPath, body string
			priority            int
		)
		err := db.QueryRow(query, req.Worker, lease).Scan(&id, &assetPath, &body, &priority)
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
		}{
			ID:        id,
			AssetPath: assetPath,
			Body:      body,
			Priority:  priority,
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
		status := r.URL.Query().Get("status")
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil || limit <= 0 {
			limit = 100
		}
		query := "SELECT id, asset_path, status, worker, lease_expires, priority, body, primitives FROM tasks"
		args := []any{limit}
		if status != "" {
			query += " WHERE status = ?"
			args = []any{status, limit}
		}
		rows, err := db.Query(query+" ORDER BY priority DESC, rowid ASC LIMIT ?", args...)
		if err != nil {
			internalError(w, err)
			return
		}
		defer rows.Close()

		type taskItem struct {
			ID           string          `json:"id"`
			AssetPath    string          `json:"asset_path"`
			Status       string          `json:"status"`
			Worker       string          `json:"worker"`
			LeaseExpires int64           `json:"lease_expires"`
			Priority     int             `json:"priority"`
			Body         string          `json:"body"`
			Primitives   json.RawMessage `json:"primitives"`
		}

		tasks := make([]taskItem, 0)
		for rows.Next() {
			var (
				item         taskItem
				worker       sql.NullString
				leaseExpires sql.NullInt64
				prim         []byte
			)
			if err := rows.Scan(&item.ID, &item.AssetPath, &item.Status, &worker, &leaseExpires, &item.Priority, &item.Body, &prim); err != nil {
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
		res, err := db.Exec("UPDATE tasks SET body = COALESCE(?, body), priority = COALESCE(?, priority) WHERE id = ?", req.Body, req.Priority, r.PathValue("id"))
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
			http.Error(w, "task not found", http.StatusNotFound)
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

		var status string
		err = tx.QueryRow("SELECT status FROM tasks WHERE id = ?", r.PathValue("id")).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "task not found", http.StatusNotFound)
			return
		}
		if err != nil {
			internalError(w, err)
			return
		}
		if status == "leased" {
			http.Error(w, "task is leased", http.StatusConflict)
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

func main() {
	var (
		dbPath string
		addr   string
		lease  int
	)
	flag.StringVar(&dbPath, "db", "taskd.db", "database path")
	flag.StringVar(&addr, "addr", ":8080", "listen address")
	flag.IntVar(&lease, "lease", 300, "lease duration in seconds")
	flag.Parse()

	db, err := openDB(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	log.Fatal(http.ListenAndServe(addr, newHandler(db, lease)))
}
