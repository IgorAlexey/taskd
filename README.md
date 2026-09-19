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

An operator runner for local worktree slots lives in `contrib/worker`; see
[contrib/README.md](contrib/README.md).
