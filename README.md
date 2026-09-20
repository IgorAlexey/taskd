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

By default, taskd listens on 127.0.0.1:8080 with no authentication. To expose
it on all interfaces, pass `-addr :8080` or an explicit host and port.
