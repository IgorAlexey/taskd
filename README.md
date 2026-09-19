# taskd

the task tracker for the modern era.

taskd is an unapologetically simple, single-binary daemon backed by SQLite
in WAL mode with atomic leases.

It doesn't care if your workers are five
local git worktrees, fifty servers on a LAN, or actual humans. 

A worker claims a task, holds a lease, and either finishes or lets it expire.
Just simple SQL and UI that works.
