package taskd

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationV12(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v11.db")
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	v11Schema := `
CREATE TABLE tasks (
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
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE TABLE notes (
  id INTEGER PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  author TEXT NOT NULL,
  text TEXT NOT NULL
);
PRAGMA user_version = 11;
`
	if _, err := rawDB.Exec(v11Schema); err != nil {
		rawDB.Close()
		t.Fatalf("init v11 schema failed: %v", err)
	}
	if _, err := rawDB.Exec("INSERT INTO tasks (id, body, project) VALUES ('v11-task', 'old task', 'p1')"); err != nil {
		rawDB.Close()
		t.Fatalf("insert v11 task failed: %v", err)
	}
	rawDB.Close()

	db, err := openDB(dbPath, 0)
	if err != nil {
		t.Fatalf("openDB v11 database failed: %v", err)
	}
	defer db.Close()

	var version int
	if err := db.ro.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("query user_version failed: %v", err)
	}
	if version < 12 {
		t.Fatalf("expected schema version >= 12, got %d", version)
	}

	var taskVer int
	if err := db.ro.QueryRow("SELECT version FROM tasks WHERE body = 'old task'").Scan(&taskVer); err != nil {
	}
	if taskVer != 1 {
		t.Fatalf("expected default version 1, got %d", taskVer)
	}
}

func TestMigrationV15(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v14.db")
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}

	v14Schema := `
CREATE TABLE tasks (
  id INTEGER PRIMARY KEY,
  status TEXT DEFAULT 'pending',
  worker TEXT CHECK (octet_length(worker) <= 128),
  lease_expires INTEGER,
  primitives JSON,
  body TEXT NOT NULL DEFAULT '',
  priority INTEGER NOT NULL DEFAULT 3 CHECK (priority >= 0),
  project TEXT NOT NULL DEFAULT '',
  claim_count INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL DEFAULT (unixepoch()),
  version INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_tasks_pending ON tasks (priority ASC, created_at ASC) WHERE status = 'pending';
CREATE INDEX idx_tasks_pending_project ON tasks (project, priority ASC, created_at ASC) WHERE status = 'pending';
CREATE INDEX idx_tasks_lease_timeout ON tasks (lease_expires ASC) WHERE status = 'leased';
CREATE INDEX idx_tasks_project ON tasks (project, status, priority ASC);
CREATE INDEX idx_tasks_claim_count ON tasks (status, claim_count) WHERE claim_count > 0;
CREATE INDEX idx_tasks_done ON tasks (project) WHERE status = 'done';
CREATE TABLE notes (
  id INTEGER PRIMARY KEY,
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  author TEXT NOT NULL,
  text TEXT NOT NULL
);
CREATE INDEX idx_notes_task_id ON notes (task_id);
PRAGMA user_version = 14;`

	if _, err := rawDB.Exec(v14Schema); err != nil {
		rawDB.Close()
		t.Fatalf("init v14 schema failed: %v", err)
	}
	if _, err := rawDB.Exec("INSERT INTO tasks (id, body, project) VALUES (1, 'task 1', 'p')"); err != nil {
		rawDB.Close()
		t.Fatalf("insert task failed: %v", err)
	}
	rawDB.Close()

	// openDB with taskd store
	db, err := openDB(dbPath, 0)
	if err != nil {
		t.Fatalf("openDB migration failed: %v", err)
	}
	defer db.Close()

	var version int
	if err := db.ro.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version failed: %v", err)
	}
	if version != 15 {
		t.Fatalf("expected user_version 15, got %d", version)
	}

	var hasTable int
	if err := db.ro.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='task_deps'").Scan(&hasTable); err != nil {
		t.Fatalf("check task_deps table failed: %v", err)
	}
	if hasTable != 1 {
		t.Fatalf("task_deps table missing")
	}

	rows, err := db.ro.Query("PRAGMA table_info('task_deps')")
	if err != nil {
		t.Fatalf("table_info failed: %v", err)
	}
	defer rows.Close()

	colNames := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan col info failed: %v", err)
		}
		colNames[name] = true
	}
	if !colNames["task_id"] || !colNames["depends_on_id"] {
		t.Fatalf("expected columns task_id and depends_on_id, got %v", colNames)
	}
}

func TestMigrationV14(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v13.db")
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}

	v13Schema := `
CREATE TABLE tasks (
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
  version INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_tasks_pending ON tasks (priority ASC, created_at ASC) WHERE status = 'pending';
CREATE INDEX idx_tasks_pending_project ON tasks (project, priority ASC, created_at ASC) WHERE status = 'pending';
CREATE INDEX idx_tasks_lease_timeout ON tasks (lease_expires ASC) WHERE status = 'leased';
CREATE INDEX idx_tasks_project ON tasks (project, status, priority ASC);
CREATE INDEX idx_tasks_claim_count ON tasks (status, claim_count) WHERE claim_count > 0;
CREATE INDEX idx_tasks_done ON tasks (project) WHERE status = 'done';
CREATE TABLE notes (
  id INTEGER PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  author TEXT NOT NULL,
  text TEXT NOT NULL
);
CREATE INDEX idx_notes_task_id ON notes (task_id);
PRAGMA user_version = 13;`

	if _, err := rawDB.Exec(v13Schema); err != nil {
		rawDB.Close()
		t.Fatalf("init v13 schema failed: %v", err)
	}

	if _, err := rawDB.Exec(`
INSERT INTO tasks (id, body, project) VALUES ('task-b', 'body b', 'p1');
INSERT INTO tasks (id, body, project) VALUES ('task-c', 'body c', 'p1');
INSERT INTO tasks (id, body, project) VALUES ('task-a', 'body a', 'p1');
`); err != nil {
		rawDB.Close()
		t.Fatalf("insert tasks failed: %v", err)
	}

	if _, err := rawDB.Exec(`
INSERT INTO notes (task_id, created_at, author, text) VALUES ('task-b', unixepoch(), 'author-b', 'note b');
INSERT INTO notes (task_id, created_at, author, text) VALUES ('task-c', unixepoch(), 'author-c', 'note c');
INSERT INTO notes (task_id, created_at, author, text) VALUES ('task-a', unixepoch(), 'author-a', 'note a');
`); err != nil {
		rawDB.Close()
		t.Fatalf("insert notes failed: %v", err)
	}
	rawDB.Close()

	db, err := openDB(dbPath, 0)
	if err != nil {
		t.Fatalf("openDB v13 database failed: %v", err)
	}
	defer db.Close()

	var ver int
	if err := db.ro.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		t.Fatalf("query user_version failed: %v", err)
	}
	if ver != schemaVersion {
		t.Fatalf("expected schema version %d, got %d", schemaVersion, ver)
	}

	type taskRow struct {
		ID   int64
		Body string
	}
	rows, err := db.ro.Query("SELECT id, body FROM tasks ORDER BY id ASC")
	if err != nil {
		t.Fatalf("query tasks failed: %v", err)
	}
	defer rows.Close()
	var tasks []taskRow
	for rows.Next() {
		var tr taskRow
		if err := rows.Scan(&tr.ID, &tr.Body); err != nil {
			t.Fatalf("scan task failed: %v", err)
		}
		tasks = append(tasks, tr)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	if tasks[0].ID != 1 || tasks[0].Body != "body b" {
		t.Fatalf("expected task 1 = body b, got %+v", tasks[0])
	}
	if tasks[1].ID != 2 || tasks[1].Body != "body c" {
		t.Fatalf("expected task 2 = body c, got %+v", tasks[1])
	}
	if tasks[2].ID != 3 || tasks[2].Body != "body a" {
		t.Fatalf("expected task 3 = body a, got %+v", tasks[2])
	}

	type noteRow struct {
		TaskID int64
		Text   string
	}
	nrows, err := db.ro.Query("SELECT task_id, text FROM notes ORDER BY id ASC")
	if err != nil {
		t.Fatalf("query notes failed: %v", err)
	}
	defer nrows.Close()
	var notes []noteRow
	for nrows.Next() {
		var nr noteRow
		if err := nrows.Scan(&nr.TaskID, &nr.Text); err != nil {
			t.Fatalf("scan note failed: %v", err)
		}
		notes = append(notes, nr)
	}
	if len(notes) != 3 {
		t.Fatalf("expected 3 notes, got %d", len(notes))
	}
	if notes[0].TaskID != 1 || notes[0].Text != "note b" {
		t.Fatalf("expected note 1 for task 1, got %+v", notes[0])
	}
	if notes[1].TaskID != 2 || notes[1].Text != "note c" {
		t.Fatalf("expected note 2 for task 2, got %+v", notes[1])
	}
	if notes[2].TaskID != 3 || notes[2].Text != "note a" {
		t.Fatalf("expected note 3 for task 3, got %+v", notes[2])
	}

	trows, err := db.ro.Query("PRAGMA table_info('tasks')")
	if err != nil {
		t.Fatalf("PRAGMA table_info failed: %v", err)
	}
	defer trows.Close()
	var foundIDPK bool
	for trows.Next() {
		var (
			cid     int
			name    string
			colType string
			notNull int
			dflt    sql.NullString
			pk      int
		)
		if err := trows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if name == "id" && strings.ToUpper(colType) == "INTEGER" && pk == 1 {
			foundIDPK = true
		}
	}
	if !foundIDPK {
		t.Fatal("expected tasks.id to be INTEGER PRIMARY KEY")
	}
}

func TestPurgeMigrationV9(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v8.db")
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	schema := `
CREATE TABLE tasks (
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
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
PRAGMA user_version = 8;`
	if _, err := rawDB.Exec(schema); err != nil {
		t.Fatalf("exec schema failed: %v", err)
	}
	rawDB.Close()

	db, err := openDB(dbPath, 0)
	if err != nil {
		t.Fatalf("openDB v8 database failed: %v", err)
	}
	defer db.Close()

	var version int
	if err := db.ro.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("query user_version failed: %v", err)
	}
	if version < 9 {
		t.Fatalf("expected schema version >= 9, got %d", version)
	}

	var indexExists int
	if err := db.ro.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name='idx_tasks_done'").Scan(&indexExists); err != nil {
		t.Fatalf("query idx_tasks_done index failed: %v", err)
	}
	if indexExists != 1 {
		t.Fatalf("expected idx_tasks_done index to exist, got count %d", indexExists)
	}
}

func TestMigrationV13(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v12.db")
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}

	v12Schema := `
CREATE TABLE tasks (
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
  version INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_tasks_pending ON tasks (priority ASC, created_at ASC) WHERE status = 'pending';
CREATE INDEX idx_tasks_pending_project ON tasks (project, priority ASC, created_at ASC) WHERE status = 'pending';
CREATE INDEX idx_tasks_lease_timeout ON tasks (lease_expires ASC) WHERE status = 'leased';
CREATE INDEX idx_tasks_project ON tasks (project, status, priority ASC);
CREATE INDEX idx_tasks_claim_count ON tasks (status, claim_count) WHERE claim_count > 0;
CREATE INDEX idx_tasks_done ON tasks (project) WHERE status = 'done';
CREATE TABLE notes (
  id INTEGER PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  author TEXT NOT NULL,
  text TEXT NOT NULL
);
CREATE INDEX idx_notes_task_id ON notes (task_id);
PRAGMA user_version = 12;
`

	if _, err := rawDB.Exec(v12Schema); err != nil {
		rawDB.Close()
		t.Fatalf("init v12 schema failed: %v", err)
	}

	insertSQL := "INSERT INTO tasks (id, asset_path, body, project) VALUES ('v12-1', 'val', 'task body', 'p1'), ('v12-2', 'models/box.glb', '', 'p1')"
	if _, err := rawDB.Exec(insertSQL); err != nil {
		rawDB.Close()
		t.Fatalf("insert v12 task failed: %v", err)
	}
	rawDB.Close()

	db, err := openDB(dbPath, 0)
	if err != nil {
		t.Fatalf("openDB v12 database failed: %v", err)
	}
	defer db.Close()

	var version int
	if err := db.ro.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("query user_version failed: %v", err)
	}
	if version < 13 {
		t.Fatalf("expected schema version >= 13, got %d", version)
	}

	rows, err := db.ro.Query("PRAGMA table_info('tasks')")
	if err != nil {
		t.Fatalf("PRAGMA table_info failed: %v", err)
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var (
			cid       int
			name      string
			colType   string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("scan pragma_table_info failed: %v", err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info iteration failed: %v", err)
	}

	if cols["asset_path"] {
		t.Fatal("expected asset_path to be dropped, but it is still present")
	}

	var body, project string
	if err := db.ro.QueryRow("SELECT body, project FROM tasks WHERE body = 'task body'").Scan(&body, &project); err != nil {
		t.Fatalf("query task data failed: %v", err)
	}
	if body != "task body" || project != "p1" {
		t.Fatalf("expected task body='task body' project='p1', got body=%q project=%q", body, project)
	}
	if err := db.ro.QueryRow("SELECT body FROM tasks WHERE body = 'models/box.glb'").Scan(&body); err != nil {
		t.Fatalf("query asset-only task failed: %v", err)
	}
	if body != "models/box.glb" {
		t.Fatalf("expected asset-only body to fall back to asset_path, got %q", body)
	}
}
