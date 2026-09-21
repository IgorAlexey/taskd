package taskd

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"
)

const buryExhaustedSQL = `UPDATE tasks SET status='buried', worker=NULL, lease_expires=NULL, version = version + 1
WHERE (status='pending' OR (status='leased' AND lease_expires < unixepoch()))
  AND claim_count > 0 AND claim_count >= ?`

func sweepLapsed(rw *sql.DB, maxClaims int, id int64) error {
	if maxClaims > 0 {
		query := buryExhaustedSQL
		args := []any{maxClaims}
		if id > 0 {
			query += " AND id = ?"
			args = append(args, id)
		}
		rows, err := rw.Query(query+" RETURNING id", args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var buried []int64
		for rows.Next() {
			var task int64
			if err := rows.Scan(&task); err != nil {
				return err
			}
			buried = append(buried, task)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(buried) > 0 {
			strs := make([]string, len(buried))
			for i, b := range buried {
				strs[i] = strconv.FormatInt(b, 10)
			}
			log.Printf("buried at the %d claim limit: %s", maxClaims, strings.Join(strs, " "))
		}
	}
	query := "UPDATE tasks SET status='pending', worker=NULL, lease_expires=NULL, version = version + 1 WHERE status='leased' AND lease_expires < unixepoch()"
	var args []any
	if id > 0 {
		query += " AND id = ?"
		args = append(args, id)
	}
	_, err := rw.Exec(query, args...)
	return err
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
    lease_expires = NULL,
    version = version + 1
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
			} else if len(projects) > 0 {
				for _, p := range projects {
					s.notifyPending(p)
				}
				s.events.publish()
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

// sweepLapsed returns one lapsed lease to the queue, or every lapsed lease
// when id is 0, burying those past the claim limit.
func (s *store) sweepLapsed(id int64) error { return sweepLapsed(s.rw, s.maxClaims, id) }

// checkpoint truncates the WAL.
func (s *store) checkpoint(ctx context.Context) error { return checkpointWAL(ctx, s.rw) }

// runCheckpointer checkpoints the WAL and sweeps lapsed leases on a timer
// until ctx ends.
func (s *store) runCheckpointer(ctx context.Context) {
	runCheckpointer(ctx, s.rw, defaultCheckpointInterval, func() { sweepLapsed(s.rw, s.maxClaims, 0) })
}
