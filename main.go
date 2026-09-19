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
  asset_path TEXT NOT NULL,
  status TEXT DEFAULT 'pending',
  worker TEXT,
  lease_expires INTEGER,
  primitives JSON
);
CREATE INDEX IF NOT EXISTS idx_tasks_claim ON tasks (status, lease_expires);`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func newHandler(db *sql.DB, lease int) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /tasks", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID        string `json:"id"`
			AssetPath string `json:"asset_path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AssetPath == "" {
			http.Error(w, "missing asset_path", http.StatusBadRequest)
			return
		}
		if req.ID == "" {
			var b [16]byte
			if _, err := rand.Read(b[:]); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			req.ID = hex.EncodeToString(b[:])
		}
		_, err := db.Exec("INSERT INTO tasks (id, asset_path) VALUES (?, ?)", req.ID, req.AssetPath)
		if err != nil {
			var se *sqlite.Error
			if errors.As(err, &se) && (se.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY || se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE) {
				http.Error(w, "duplicate id", http.StatusConflict)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Worker == "" {
			http.Error(w, "missing worker", http.StatusBadRequest)
			return
		}
		query := `UPDATE tasks SET status='leased', worker=?, lease_expires=unixepoch()+?
WHERE id = (
  SELECT id FROM tasks
  WHERE status='pending' OR (status='leased' AND lease_expires < unixepoch())
  ORDER BY rowid ASC
  LIMIT 1
) RETURNING id, asset_path`
		var id, assetPath string
		err := db.QueryRow(query, req.Worker, lease).Scan(&id, &assetPath)
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":         id,
			"asset_path": assetPath,
		})
	})

	mux.HandleFunc("POST /tasks/{id}/done", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Worker     string          `json:"worker"`
			Primitives json.RawMessage `json:"primitives"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Worker == "" {
			http.Error(w, "missing worker", http.StatusBadRequest)
			return
		}
		var prim any
		if len(req.Primitives) > 0 {
			prim = string(req.Primitives)
		}
		res, err := db.Exec("UPDATE tasks SET status='done', primitives=? WHERE id=? AND status='leased' AND worker=?", prim, r.PathValue("id"), req.Worker)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		n, err := res.RowsAffected()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if n == 0 {
			http.Error(w, "task not found or not leased by worker", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	return mux
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
