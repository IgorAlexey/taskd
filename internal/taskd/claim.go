package taskd

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *server) claimHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Worker  string   `json:"worker"`
		Project string   `json:"project"`
		Wait    *float64 `json:"wait"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Wait != nil && !(*req.Wait >= 0 && *req.Wait <= 86400) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid wait %v, must be between 0 and 86400 seconds", *req.Wait))
		return
	}
	var ok bool
	if req.Worker, ok = checkWorker(w, req.Worker); !ok {
		return
	}
	req.Project = strings.TrimSpace(req.Project)
	if req.Project == "" || req.Project == "*" {
		req.Project = "*"
	} else if msg, ok := checkProject(req.Project); !ok {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	query := `UPDATE tasks SET status='leased', worker=?, lease_expires=unixepoch()+?, claim_count=claim_count+1, version = version + 1
WHERE id = (
  SELECT id FROM tasks
  WHERE status='pending'
    AND NOT EXISTS (SELECT 1 FROM task_deps d JOIN tasks t ON t.id = d.depends_on_id WHERE d.task_id = tasks.id AND t.status != 'done')`
	args := []any{req.Worker, s.lease}
	if s.db.maxClaims > 0 {
		query += " AND claim_count < ?"
		args = append(args, s.db.maxClaims)
	}
	if req.Project != "*" {
		query += " AND project = ?"
		args = append(args, req.Project)
	}
	query += `
  ORDER BY priority ASC, created_at ASC
  LIMIT 1
) RETURNING id, status, worker, lease_expires, priority, version, body, primitives, project, claim_count, created_at`

	tryClaim := func() (*taskItem, error) {
		var (
			item         taskItem
			leasedWorker sql.NullString
			leaseExpires sql.NullInt64
			prim         []byte
		)
		err := s.db.rw.QueryRow(query, args...).
			Scan(&item.ID, &item.Status, &leasedWorker, &leaseExpires, &item.Priority, &item.Version, &item.Body, &prim, &item.Project, &item.ClaimCount, &item.CreatedAt)
		if err != nil {
			return nil, err
		}
		item.Worker = leasedWorker.String
		item.LeaseExpires = leaseExpires.Int64
		item.Primitives = prim
		item.After, err = fetchAfter(s.db.rw, item.ID)
		if err != nil {
			return nil, err
		}
		return &item, nil
	}

	var timer *time.Timer
	for {
		seq := s.db.currentSeq(req.Project)

		item, err := tryClaim()
		if err == nil {
			s.db.events.publish()
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
		}
		switch err := s.db.waitPending(r.Context(), req.Project, seq, timer.C); {
		case err == nil:
		case errors.Is(err, errWaitTimeout):
			w.WriteHeader(http.StatusNoContent)
			return
		case errors.Is(err, errStoreClosed):
			writeError(w, http.StatusServiceUnavailable, "database closed")
			return
		default:
			return
		}
	}
}

func (s *server) claimIDHandler(w http.ResponseWriter, r *http.Request) {
	worker, ok := decodeWorker(w, r)
	if !ok {
		return
	}
	id, ok := pathTaskID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err := s.db.sweepLapsed(id); err != nil {
		internalError(w, err)
		return
	}
	query := `UPDATE tasks
SET status='leased', worker=?, lease_expires=unixepoch()+?, claim_count=claim_count+1, version = version + 1
WHERE id = ? AND status='pending'
  AND (? <= 0 OR claim_count < ?)
  AND NOT EXISTS (SELECT 1 FROM task_deps d JOIN tasks t ON t.id = d.depends_on_id WHERE d.task_id = tasks.id AND t.status != 'done')
RETURNING id, status, worker, lease_expires, priority, version, body, primitives, project, claim_count, created_at`
	var (
		item         taskItem
		leasedWorker sql.NullString
		leaseExpires sql.NullInt64
		prim         []byte
	)
	err := s.db.rw.QueryRow(query, worker, s.lease, id, s.db.maxClaims, s.db.maxClaims).
		Scan(&item.ID, &item.Status, &leasedWorker, &leaseExpires, &item.Priority, &item.Version, &item.Body, &prim, &item.Project, &item.ClaimCount, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		var (
			status   string
			hasUnmet int
		)
		if err := s.db.ro.QueryRow(`SELECT status, EXISTS(
			SELECT 1 FROM task_deps d JOIN tasks t ON t.id = d.depends_on_id WHERE d.task_id = tasks.id AND t.status != 'done'
		) FROM tasks WHERE id = ?`, id).Scan(&status, &hasUnmet); err == nil {
			if status == "pending" && hasUnmet == 1 {
				writeError(w, http.StatusConflict, "task is waiting on other tasks")
				return
			}
		}
		writeTaskStateError(w, s.db.ro, id, "", nil)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	item.Worker = leasedWorker.String
	item.LeaseExpires = leaseExpires.Int64
	item.Primitives = prim
	item.After, err = fetchAfter(s.db.rw, item.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	s.db.events.publish()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(item)
}

func (s *server) doneIDHandler(w http.ResponseWriter, r *http.Request) {
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
	id, ok := pathTaskID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	var prim any
	if len(req.Primitives) > 0 && !bytes.Equal(bytes.TrimSpace(req.Primitives), []byte("null")) {
		prim = string(req.Primitives)
	}
	if _, ok := updateLeasedOrLapsed(w, s.db.rw, "status='done', primitives=?, lease_expires=NULL, version = version + 1", id, req.Worker, req.ClaimCount, prim); !ok {
		return
	}
	s.db.events.publish()
	if projs, err := blockedProjects(s.db.ro, id); err == nil {
		for _, p := range projs {
			s.db.notifyPending(p)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) closeIDHandler(w http.ResponseWriter, r *http.Request) {
	if !decodeOptionalWorker(w, r) {
		return
	}
	id, ok := pathTaskID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	res, err := s.db.rw.Exec(`UPDATE tasks SET status='done', worker=NULL, lease_expires=NULL, version = version + 1
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
		writeTaskStateError(w, s.db.ro, id, "", nil)
		return
	}
	s.db.events.publish()
	if projs, err := blockedProjects(s.db.ro, id); err == nil {
		for _, p := range projs {
			s.db.notifyPending(p)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) touchIDHandler(w http.ResponseWriter, r *http.Request) {
	worker, ok := decodeWorker(w, r)
	if !ok {
		return
	}
	id, ok := pathTaskID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	env, ok := updateLeased(w, s.db.rw, "lease_expires = unixepoch() + ?", id, worker, s.lease)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(env)
}

func (s *server) touchWorkerHandler(w http.ResponseWriter, r *http.Request) {
	worker, ok := decodeWorker(w, r)
	if !ok {
		return
	}
	res, err := s.db.rw.Exec("UPDATE tasks SET lease_expires = unixepoch() + ? WHERE status='leased' AND worker=? AND lease_expires >= unixepoch()", s.lease, worker)
	if err != nil {
		internalError(w, err)
		return
	}
	n, _ := res.RowsAffected()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int64{"touched": n})
}

func (s *server) releaseIDHandler(w http.ResponseWriter, r *http.Request) {
	worker, ok := decodeWorker(w, r)
	if !ok {
		return
	}
	id, ok := pathTaskID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	env, ok := updateLeasedOrLapsed(w, s.db.rw, "status='pending', worker=NULL, lease_expires=NULL, claim_count=max(claim_count-1, 0), version = version + 1", id, worker, nil)
	if !ok {
		return
	}
	s.db.events.publish()
	if env.Project != "" {
		s.db.notifyPending(env.Project)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) releaseWorkerHandler(w http.ResponseWriter, r *http.Request) {
	worker, ok := decodeWorker(w, r)
	if !ok {
		return
	}
	rows, err := s.db.rw.Query("UPDATE tasks SET status='pending', worker=NULL, lease_expires=NULL, claim_count=max(claim_count-1, 0), version = version + 1 WHERE status='leased' AND worker=? RETURNING project", worker)
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()
	projects := map[string]bool{}
	var n int64
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			internalError(w, err)
			return
		}
		projects[p] = true
		n++
	}
	if err := rows.Err(); err != nil {
		internalError(w, err)
		return
	}
	if n > 0 {
		s.db.events.publish()
	}
	for p := range projects {
		s.db.notifyPending(p)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int64{"released": n})
}

func (s *server) buryIDHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Worker     string          `json:"worker"`
		Priority   *int            `json:"priority"`
		ClaimCount *int            `json:"claim_count"`
		Primitives json.RawMessage `json:"primitives"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	worker, ok := checkWorker(w, req.Worker)
	if !ok {
		return
	}
	if req.Priority != nil && *req.Priority < 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid priority %d, must be 0 or greater", *req.Priority))
		return
	}
	id, ok := pathTaskID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	var prim any
	if len(req.Primitives) > 0 {
		prim = string(req.Primitives)
	}
	if _, ok := updateLeasedOrLapsed(w, s.db.rw, "status='buried', worker=NULL, lease_expires=NULL, priority=COALESCE(?, priority), primitives=?, version = version + 1", id, worker, req.ClaimCount, req.Priority, prim); !ok {
		return
	}
	s.db.events.publish()
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) kickIDHandler(w http.ResponseWriter, r *http.Request) {
	if !decodeOptionalWorker(w, r) {
		return
	}
	id, ok := pathTaskID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	var project string
	err := s.db.rw.QueryRow("UPDATE tasks SET status='pending', worker=NULL, lease_expires=NULL, claim_count=0, primitives=NULL, version = version + 1 WHERE id=? AND status='buried' RETURNING project", id).Scan(&project)
	if errors.Is(err, sql.ErrNoRows) {
		writeTaskStateError(w, s.db.ro, id, "", nil)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	s.db.events.publish()
	if project != "" {
		s.db.notifyPending(project)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) bulkKickTasksHandler(w http.ResponseWriter, r *http.Request) {
	q := requestQuery(r)
	var req struct {
		Project *string `json:"project"`
		Limit   *int    `json:"limit"`
	}
	if !decodeBody(w, r, &req, true) {
		return
	}

	if q.Has("project") && req.Project != nil && q.Get("project") != *req.Project {
		writeError(w, http.StatusBadRequest, "conflicting project parameter")
		return
	}

	var project string
	if req.Project != nil {
		project = *req.Project
	} else if q.Has("project") {
		project = q.Get("project")
	}

	var projectFilter string
	if project != "" {
		if project != "*" {
			if msg, ok := checkProject(project); !ok {
				writeError(w, http.StatusBadRequest, msg)
				return
			}
			projectFilter = project
		}
	} else if q.Has("project") || req.Project != nil {
		writeError(w, http.StatusBadRequest, "project cannot be empty")
		return
	}

	var qLimit *int
	if q.Has("limit") {
		v, err := strconv.Atoi(q.Get("limit"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		qLimit = &v
	}
	if qLimit != nil && req.Limit != nil && *qLimit != *req.Limit {
		writeError(w, http.StatusBadRequest, "conflicting limit parameter")
		return
	}

	var limit *int
	if req.Limit != nil {
		limit = req.Limit
	} else {
		limit = qLimit
	}
	if limit != nil && (*limit < 1 || *limit > 1000) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid limit %d, must be between 1 and 1000", *limit))
		return
	}

	var query string
	var args []any
	if limit == nil {
		query = "UPDATE tasks SET status='pending', worker=NULL, lease_expires=NULL, claim_count=0, primitives=NULL, version = version + 1 WHERE status = 'buried'"
		if projectFilter != "" {
			query += " AND project = ?"
			args = append(args, projectFilter)
		}
		query += " RETURNING project"
	} else {
		query = "UPDATE tasks SET status='pending', worker=NULL, lease_expires=NULL, claim_count=0, primitives=NULL, version = version + 1 WHERE id IN (SELECT id FROM tasks WHERE status = 'buried'"
		if projectFilter != "" {
			query += " AND project = ?"
			args = append(args, projectFilter)
		}
		query += " ORDER BY priority ASC, created_at ASC LIMIT ?) RETURNING project"
		args = append(args, *limit)
	}

	rows, err := s.db.rw.Query(query, args...)
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()

	var kicked int
	for rows.Next() {
		var proj string
		if err := rows.Scan(&proj); err != nil {
			internalError(w, err)
			return
		}
		kicked++
		if proj != "" {
			s.db.notifyPending(proj)
		}
	}
	if err := rows.Err(); err != nil {
		internalError(w, err)
		return
	}
	if kicked > 0 {
		s.db.events.publish()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"kicked": kicked})
}
