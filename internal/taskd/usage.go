package taskd

import (
	"fmt"
	"io"
)

func printUsage(w io.Writer) {
	fmt.Fprint(w, `Usage of taskd:

taskd is a lightweight task queue daemon backed by SQLite.

Commands talk to a running daemon at TASKD_URL (default http://127.0.0.1:8080)
as the worker named by TASKD_WORKER (default user@host) in TASKD_PROJECT:
  add [body]         create a task; body from stdin when piped or "-"
  claim [id]         claim the next task, or the one named; exit 3 when none
  done ID            finish a claimed task (-result JSON)
  close ID           finish a task without a result
  touch ID           extend the lease on a claimed task
  release ID         put a claimed task back in the queue
  bury ID            park a task that cannot proceed
  kick ID            return a parked task to the queue
  note ID [text]     append a note; text from stdin when absent
  show ID            print one task with its notes
  list               print tasks (-project, -status, -q, -limit)
  serve              run the daemon (the same as no command)
  help [command]     this text, or a command's flags
Each prints the daemon's JSON on stdout; on failure one line on stderr and
exit 1, or 2 for bad usage. Flags: -url, -worker, -project, and -q for the id.

Options:
  -addr <addr>        listen address (e.g. :8080 to expose on all interfaces) (default: 127.0.0.1:8080)
  -backup <path>      backup destination path
  -cors-origin <url>  allowed CORS origin
  -db <path>          database path (default: taskd.db)
  -lease <seconds>    lease duration in seconds (default: 300)
  -log-requests       log completed HTTP requests (default: true)
  -max-claims <count> bury a task after this many claims (0 = unlimited)
  -v, -version        print version and exit
  -h, --help          show this help message

Environment variables:
  TASKD_ADDR          listen address (default: 127.0.0.1:8080)
  TASKD_DB            database path (default: taskd.db)
  TASKD_LEASE         lease duration in seconds (default: 300)
  TASKD_MAX_CLAIMS    bury a task after this many claims (default: 0)
  TASKD_CORS_ORIGIN   allowed CORS origin
  TASKD_LOG_REQUESTS  log completed HTTP requests (default: true)

HTTP Endpoints:
  GET    /health             daemon readiness and database ping
  GET    /tasks              list tasks
         ?status=            pending | leased | done | buried | live
         ?project=           exact match; project=* matches all projects
         ?worker=            exact match; empty value selects unassigned
         ?priority=          integer >= 0
         ?limit=             1..1000, default 100
         ?offset=            integer >= 0
         ?after=             opaque cursor token from X-Next-Cursor
         ?sort=              id | project | status | priority | claim_count | worker | created_at
         ?order=             asc | desc, default asc
         ?q=                 substring of id, body, project, or worker
         ?fields=            comma list from id, status, worker,
                             lease_expires, priority, body, primitives,
                             project, claim_count, summary, created_at, version
         ?columns=           alias for fields
  POST   /tasks              create a task (requires project, body; optional priority)
                             accepts JSON array for atomic batch creation
  POST   /tasks/claim        claim next pending task (requires worker, optional project, optional wait in seconds)
  GET    /tasks/{id}         get task details
         ?fields=            comma list from id, status, worker,
                             lease_expires, priority, body, primitives,
                             project, claim_count, summary, created_at, version
         ?columns=           alias for fields
  PATCH  /tasks/{id}         update task (requires body, priority, or project, optional if_version)
  POST   /tasks/{id}/claim   claim a specific task (requires worker)
  POST   /tasks/{id}/done    complete task with primitives (requires worker, optional claim_count)
  POST   /tasks/{id}/close   close task without result
  POST   /tasks/{id}/touch   extend lease, return expiration (requires worker)
  POST   /tasks/{id}/release release task back to pending (requires worker)
  POST   /tasks/{id}/bury    park a blocked task (requires worker, optional priority, optional claim_count, optional primitives)
  POST   /tasks/{id}/kick    return a parked task to pending
  DELETE /tasks/{id}         delete task (?force=1 to delete done task)
  GET    /tasks/{id}/notes   retrieve task notes collection
  POST   /tasks/{id}/notes   append a note (requires author, text)
  POST   /tasks/purge        bulk-delete completed tasks (optional ?project=)
  POST   /tasks/kick         bulk-unbury tasks (optional project, limit)
  GET    /projects           list active projects
  PATCH  /projects/{name}    rename a project (requires name; 409 if the name is in use)
  DELETE /projects/{name}    delete a project and every task in it (409 while one is claimed)
  GET    /workers            list active workers
         ?project=           exact match; project=* matches all projects
         ?status=            filter by task status (pending, leased, done, buried)
  GET    /stats              task queue statistics
         ?project=           exact match; project=* matches all projects
         ?worker=            exact match; empty value selects unassigned
  GET    /ui                 web interface

Examples:
  taskd                                      run daemon on 127.0.0.1:8080 with taskd.db
  taskd -addr :8080                          expose daemon on all interfaces
  taskd -addr :9090 -db custom.db            run on custom port and database
  taskd -lease 600                           use 10 minute task lease duration
  taskd -max-claims 3                        bury a task after 3 claims
  taskd -backup backup.db                    backup database to file and exit

  # Task lifecycle (create, claim, complete):
  T=${T:-http://localhost:8080}
  curl -s -XPOST $T/tasks -d '{"body":"hello","project":"demo"}'
  curl -s -XPOST $T/tasks -d '[{"body":"batch","project":"demo"}]'
  curl -s -XPOST $T/tasks/claim -d '{"worker":"me","project":"demo"}'
  curl -s -i -XPOST $T/tasks/1/done -d '{"worker":"me"}'
`)
}
