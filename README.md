<h1 align="center">taskd</h1>

<p align="center">
  <strong>the task tracker for the modern era.</strong>
  <br>
  <strong><a href="https://igoralexey.com/taskd">igoralexey.com/taskd</a></strong>
</p>


taskd is an unapologetically simple, single-binary daemon backed by SQLite
in WAL mode with atomic leases.

taskd doesn't care if your workers are five
local git worktrees, fifty servers on a LAN, or actual humans. 

A worker claims a task, holds a lease, and either finishes or lets it expire.
Just simple SQL and UI that works.

Each task has a numeric priority and lower numbers are claimed first: 1 is
the top, 0 is reserved for emergencies, and a task filed without a
priority gets 3. Ties are broken by insertion order.

## Quickstart

Build and start the daemon, enqueue a task, and claim it:

```sh
# Build the binary
go build -o taskd .

# Start the daemon with an explicit database path (default is taskd.db in cwd)
./taskd -db taskd.db &

# Enqueue a task
curl -s -XPOST http://localhost:8080/tasks -H 'Content-Type: application/json' -d '{"body":"my first task","project":"demo"}'
# Output:
# {"id":"b248c17131a98e3739387b4f7002e8b4"}

# Claim the next pending task
curl -s -XPOST http://localhost:8080/tasks/claim -H 'Content-Type: application/json' -d '{"worker":"me","project":"demo"}'
# Output:
# {"id":"b248c17131a98e3739387b4f7002e8b4","asset_path":"","status":"leased","worker":"me","lease_expires":1789818122,"priority":3,"body":"my first task","primitives":null,"project":"demo","claim_count":1}
```

The `-db` flag defaults to `taskd.db` relative to the current working directory;
starting the daemon from another directory without `-db` opens a different
database. Always provide an explicit path to keep data in a predictable place.

Once running, view and manage tasks in your browser via `GET /ui` at
`http://localhost:8080/ui`, or launch the terminal user interface in
`cmd/taskd-tui` with `go run ./cmd/taskd-tui`.

## Lease conflicts

`done`, `touch`, `release`, and `bury` require a live lease held by the
calling worker; `claim`, `close`, `kick`, and `PATCH /tasks/{id}` require
a task in the matching state. `close` and `kick` hold no lease, so they
take no input: an absent body, `{}`, `null`, and `{"worker":...}` all
work, and the worker id is ignored rather than recorded. A refused call
answers `404 Not Found` with `task not found`, or `409 Conflict` with
one of these `{"error": ...}` messages:

- `lease has expired`: the lease was yours and ran out.
- `task not leased by worker`: the lease belongs to another worker, live
  or expired.
- `task is leased`: another worker is on the task right now.
- `task is pending`, `task is buried`, `task is done`: nobody leases the
  task right now.
- `task state conflict`: unknown status, worth a bug report.

Whichever one you get, the instruction is the same: you do not hold the
task, so drop the work. The wording tells a human reading the log which
way it went, and is not a stable signal, since a worker that claims the
task between your call and the answer changes it.

A task id also resolves from a unique prefix. A prefix matching more than
one task answers `409 Conflict` with a wider body,
`{"error": "ambiguous id prefix: 12 tasks match", "count": 12, "matches":
[...]}`, where `count` is the number of matching tasks and `matches`
names all of them. Past 50 matches the daemon stops counting: the body
then has `"truncated": true`, no `count`, and the first 50 ids, so a cut
off list is never mistaken for the whole answer.

A worker identifier is trimmed of surrounding whitespace and must then be
between 1 and 128 bytes, not characters, since it is stored as-is in the
`worker` column of every task it touches. An empty one answers `400 Bad
Request` with `missing worker`, a longer one with `worker too long`; a
non-ASCII hostname reaches the bound sooner than its character count
suggests.

## Claim limits

By default a task is handed out again every time its lease expires, so a
task that kills its worker is retried forever. Start the daemon with
`-max-claims N` to cap that: once a task has been claimed `N` times,
whatever the outcome of those claims, the next claim buries it instead
of handing it out, logs its id, and keeps serving the rest of the queue.
A worker that releases work cleanly spends the budget too. The sweep is
daemon-wide, so a claim scoped to one project also buries exhausted
tasks of another. A buried task is out of the pool until
`POST /tasks/{id}/kick`, which returns it to pending with its claim
count reset to zero. The default `0` means no limit and keeps the old
behaviour.

## Paging

`GET /tasks` is ordered by insertion order, every page of it, and priority is
a filter and the claim order rather than a list order. A full page, one
holding as many rows as `limit` asked for, sets an `X-Next-Cursor` header
with an opaque token for its last row. A short page sets no header, and
that absence is how a walk ends; no trailing empty request is needed. Pass
the token back as `?after=<cursor>` for the rows after that point. Unlike
`&offset=`, a cursor does not skip rows when workers claim tasks in the
middle of a walk.

The token names an insertion sequence, which nothing rewrites, so
re-prioritising a task in the middle of a walk can neither hide it nor hand
it out twice; the old priority-keyed token lost one promoted past the cursor
and repeated one demoted behind it. It does not fix everything: a row that
enters the filtered set behind the cursor is missed, because a release, a
kick or a lease expiring puts a task back in `pending` at its old insertion
place.

A token is bound to the filters that produced it, which catches a cursor
replayed against the wrong query, not a forged one: it is an unkeyed
digest of public inputs, so treat it as a typo detector. Replaying one
under a different `status`, `project`, `worker`, `priority`,
`asset_path` or `q` answers `400 Bad Request` with
`{"error":"invalid after"}`, as does an unparseable one. A different
`limit` is fine. `offset` still works when `after` is omitted, but the two
cannot be combined. With `after` the response has no `X-Total-Count`: the
first call, the one without a cursor, is where the size of the set comes
from. Paging this way buys correctness, not speed.

```sh
# First page: the set size, and a cursor because the page is full
curl -si 'http://localhost:8080/tasks?project=demo&limit=2'
# Output:
# X-Total-Count: 5
# X-Next-Cursor: eyJyIjoyLCJmIjoiZl9wS2Y0Y09Td3VGIn0

# Second page: full again, so another cursor, and no X-Total-Count
curl -si 'http://localhost:8080/tasks?project=demo&limit=2&after=eyJyIjoyLCJmIjoiZl9wS2Y0Y09Td3VGIn0'
# Output:
# X-Next-Cursor: eyJyIjo0LCJmIjoiZl9wS2Y0Y09Td3VGIn0

# Last page: one row, no X-Next-Cursor, the walk is done
curl -si 'http://localhost:8080/tasks?project=demo&limit=2&after=eyJyIjo0LCJmIjoiZl9wS2Y0Y09Td3VGIn0'
# Output:
# X-Total-Count and X-Next-Cursor both absent
```

## Backup

A plain cp of the .db is not a backup: WAL mode leaves data in the wal file.
Take a safe online snapshot with `taskd -backup <path>`:

```sh
taskd -db taskd.db -backup /backups/taskd.db
```

This runs SQLite `VACUUM INTO` to write a consistent copy, then reports it:

```
wrote /backups/taskd.db (20480 bytes, 5 tasks)
```

A destination that resolves to the source database itself, through `.`, a
symlink or a hardlink, is refused with exit status 1 and nothing is written.
