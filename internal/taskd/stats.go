package taskd

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/IgorAlexey/taskd/internal/version"
)

// statsResponse is the body of GET /stats.
type statsResponse struct {
	Pending      int    `json:"pending"`
	Leased       int    `json:"leased"`
	Done         int    `json:"done"`
	Buried       int    `json:"buried"`
	Total        int    `json:"total"`
	LeaseSeconds int    `json:"lease_seconds"`
	DB           string `json:"db"`
	Version      string `json:"version"`
	MaxClaims    int    `json:"max_claims"`
}

func (s *server) statsHandler(w http.ResponseWriter, r *http.Request) {
	q := requestQuery(r)
	project, ok := validateProjectFilter(w, q)
	if !ok {
		return
	}
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
	if err := s.db.ro.QueryRow(query, args...).Scan(&pending, &leased, &done, &buried, &total); err != nil {
		internalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(statsResponse{
		Pending: pending, Leased: leased, Done: done, Buried: buried,
		Total: total, LeaseSeconds: s.lease, DB: s.dbName,
		Version: version.Version, MaxClaims: s.db.maxClaims,
	})
}

func (s *server) workersHandler(w http.ResponseWriter, r *http.Request) {
	q := requestQuery(r)
	project, ok := validateProjectFilter(w, q)
	if !ok {
		return
	}
	status := q.Get("status")
	conditions := []string{"worker IS NOT NULL", "worker != ''"}
	var args []any
	if q.Has("status") {
		switch status {
		case "leased":
			conditions = append(conditions, "status = 'leased' AND lease_expires >= unixepoch()")
		case "done", "buried", "pending":
			conditions = append(conditions, "status = ?")
			args = append(args, status)
		default:
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
	} else {
		conditions = append(conditions, "(status = 'done' OR (status = 'leased' AND lease_expires >= unixepoch()))")
	}
	if project != "" && project != "*" {
		conditions = append(conditions, "project = ?")
		args = append(args, project)
	}
	query := "SELECT DISTINCT worker FROM tasks WHERE " + strings.Join(conditions, " AND ") + " ORDER BY worker ASC"
	rows, err := s.db.ro.Query(query, args...)
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

func (s *server) healthHandler(w http.ResponseWriter, r *http.Request) {
	if err := s.db.rw.PingContext(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "database ping failed")
		return
	}
	if err := s.db.ro.PingContext(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "database ping failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte("{\"status\":\"ok\"}\n"))
}
