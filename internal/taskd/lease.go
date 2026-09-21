package taskd

import (
	"database/sql"
	"errors"
	"net/http"
)

type leaseConflict struct {
	Error      string `json:"error"`
	Worker     string `json:"worker"`
	ClaimCount int    `json:"claim_count"`
}

func writeTaskStateError(w http.ResponseWriter, q queryer, id int64, worker string, claim *int) {
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

func updateLeased(w http.ResponseWriter, db *sql.DB, set string, id int64, worker string, args ...any) (leaseEnvelope, bool) {
	query := "UPDATE tasks SET " + set + " WHERE id=? AND status='leased' AND worker=? AND lease_expires >= unixepoch() RETURNING id, lease_expires, status, project"
	return updateTask(w, db, query, append(args, id, worker), id, worker, nil)
}

func updateLeasedOrLapsed(w http.ResponseWriter, db *sql.DB, set string, id int64, worker string, claim *int, args ...any) (leaseEnvelope, bool) {
	query := "UPDATE tasks SET " + set + " WHERE id=? AND status='leased' AND worker=?"
	fullArgs := append(args, id, worker)
	if claim != nil {
		query += " AND claim_count=?"
		fullArgs = append(fullArgs, *claim)
	}
	query += " RETURNING id, lease_expires, status, project"
	return updateTask(w, db, query, fullArgs, id, worker, claim)
}

func updateTask(w http.ResponseWriter, db *sql.DB, query string, args []any, id int64, worker string, claim *int) (leaseEnvelope, bool) {
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

type leaseEnvelope struct {
	ID           int64  `json:"id"`
	LeaseExpires int64  `json:"lease_expires"`
	Status       string `json:"status"`
	Project      string `json:"-"`
}
