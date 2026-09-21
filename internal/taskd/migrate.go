package taskd

import (
	"database/sql"
	"fmt"
	"time"
)

var migrations = [...]func(*sql.DB) error{migrateV2, migrateV3, migrateV4, migrateV5, migrateV6, migrateV7, migrateV8, migrateV9, migrateV10, migrateV11, migrateV12, migrateV13, migrateV14, migrateV15}

// schemaVersion is the user_version a fresh database gets.
const schemaVersion = len(migrations) + 1

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
		"CREATE TABLE tasks_new (" + taskColumnsV12 + ");",
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
		"CREATE TABLE tasks_new (" + taskColumnsV12 + ");",
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

// taskColumnsV12 is the layout migrations V4 through V12 rebuilt into; it is
// frozen so those migrations keep producing the schema they did at the time.
const taskColumnsV12 = `
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
  created_at INTEGER NOT NULL DEFAULT (unixepoch()),
  version INTEGER NOT NULL DEFAULT 1`

// taskColumnsV13 is the layout migration V13 rebuilt into; it is
// frozen so that migration keeps producing the schema it did at the time.
const taskColumnsV13 = `
  id TEXT PRIMARY KEY,
  status TEXT DEFAULT 'pending',
  worker TEXT CHECK (octet_length(worker) <= 128),
  lease_expires INTEGER,
  primitives JSON,
  body TEXT NOT NULL DEFAULT '',
  priority INTEGER NOT NULL DEFAULT 3 CHECK (priority >= 0),
  project TEXT NOT NULL DEFAULT '',
  claim_count INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL DEFAULT (unixepoch()),
  version INTEGER NOT NULL DEFAULT 1`

const taskColumns = `
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  status TEXT DEFAULT 'pending',
  worker TEXT CHECK (octet_length(worker) <= 128),
  lease_expires INTEGER,
  primitives JSON,
  body TEXT NOT NULL DEFAULT '',
  priority INTEGER NOT NULL DEFAULT 3 CHECK (priority >= 0),
  project TEXT NOT NULL DEFAULT '',
  claim_count INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL DEFAULT (unixepoch()),
  version INTEGER NOT NULL DEFAULT 1`

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

func migrateV10(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		"DROP INDEX IF EXISTS idx_tasks_pending;",
		"DROP INDEX IF EXISTS idx_tasks_pending_project;",
		"CREATE INDEX IF NOT EXISTS idx_tasks_pending ON tasks (priority ASC, created_at ASC) WHERE status = 'pending';",
		"CREATE INDEX IF NOT EXISTS idx_tasks_pending_project ON tasks (project, priority ASC, created_at ASC) WHERE status = 'pending';",
		"PRAGMA user_version = 10;",
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func migrateV11(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS notes (
  id INTEGER PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  author TEXT NOT NULL,
  text TEXT NOT NULL
);`,
		"CREATE INDEX IF NOT EXISTS idx_notes_task_id ON notes (task_id);",
		"PRAGMA user_version = 11;",
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func migrateV12(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("ALTER TABLE tasks ADD COLUMN version INTEGER NOT NULL DEFAULT 1;"); err != nil {
		return err
	}
	if _, err := tx.Exec("PRAGMA user_version = 12;"); err != nil {
		return err
	}
	return tx.Commit()
}

func migrateV13(db *sql.DB) error {
	if _, err := db.Exec("PRAGMA foreign_keys = OFF;"); err != nil {
		return err
	}
	defer db.Exec("PRAGMA foreign_keys = ON;")

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		"CREATE TABLE tasks_new (" + taskColumnsV13 + ");",
		`INSERT INTO tasks_new (rowid, id, status, worker, lease_expires, primitives, body, priority, project, claim_count, created_at, version)
SELECT rowid, id, status, worker, lease_expires, primitives,
  CASE WHEN trim(body) = '' THEN asset_path ELSE body END,
  priority, project, claim_count, created_at, version FROM tasks;`,
		"DROP TABLE tasks;",
		"ALTER TABLE tasks_new RENAME TO tasks;",
		"CREATE INDEX idx_tasks_pending ON tasks (priority ASC, created_at ASC) WHERE status = 'pending';",
		"CREATE INDEX idx_tasks_pending_project ON tasks (project, priority ASC, created_at ASC) WHERE status = 'pending';",
		"CREATE INDEX idx_tasks_lease_timeout ON tasks (lease_expires ASC) WHERE status = 'leased';",
		"CREATE INDEX idx_tasks_project ON tasks (project, status, priority ASC);",
		"CREATE INDEX idx_tasks_claim_count ON tasks (status, claim_count) WHERE claim_count > 0;",
		"CREATE INDEX idx_tasks_done ON tasks (project) WHERE status = 'done';",
		"PRAGMA user_version = 13;",
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func migrateV14(db *sql.DB) error {
	if _, err := db.Exec("PRAGMA foreign_keys = OFF;"); err != nil {
		return err
	}
	defer db.Exec("PRAGMA foreign_keys = ON;")

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		"CREATE TABLE tasks_new (" + taskColumns + ");",
		`CREATE TEMP TABLE idmap AS
SELECT id AS old_id, row_number() OVER (ORDER BY rowid) AS new_id FROM tasks;`,
		`INSERT INTO tasks_new (id, status, worker, lease_expires, primitives, body, priority, project, claim_count, created_at, version)
SELECT m.new_id, t.status, t.worker, t.lease_expires, t.primitives, t.body, t.priority, t.project, t.claim_count, t.created_at, t.version
FROM tasks t JOIN idmap m ON t.id = m.old_id;`,
		`CREATE TABLE notes_new (
  id INTEGER PRIMARY KEY,
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  author TEXT NOT NULL,
  text TEXT NOT NULL
);`,
		`INSERT INTO notes_new (id, task_id, created_at, author, text)
SELECT n.id, m.new_id, n.created_at, n.author, n.text
FROM notes n
JOIN idmap m ON n.task_id = m.old_id;`,
		"DROP TABLE notes;",
		"ALTER TABLE notes_new RENAME TO notes;",
		"DROP TABLE tasks;",
		"ALTER TABLE tasks_new RENAME TO tasks;",
		"DROP TABLE idmap;",
		"CREATE INDEX idx_tasks_pending ON tasks (priority ASC, created_at ASC) WHERE status = 'pending';",
		"CREATE INDEX idx_tasks_pending_project ON tasks (project, priority ASC, created_at ASC) WHERE status = 'pending';",
		"CREATE INDEX idx_tasks_lease_timeout ON tasks (lease_expires ASC) WHERE status = 'leased';",
		"CREATE INDEX idx_tasks_project ON tasks (project, status, priority ASC);",
		"CREATE INDEX idx_tasks_claim_count ON tasks (status, claim_count) WHERE claim_count > 0;",
		"CREATE INDEX idx_tasks_done ON tasks (project) WHERE status = 'done';",
		"CREATE INDEX idx_notes_task_id ON notes (task_id);",
		"PRAGMA user_version = 14;",
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func migrateV15(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		`CREATE TABLE task_deps (
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  depends_on_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  PRIMARY KEY (task_id, depends_on_id)
);`,
		"CREATE INDEX idx_task_deps_depends_on ON task_deps (depends_on_id);",
		"PRAGMA user_version = 15;",
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}
