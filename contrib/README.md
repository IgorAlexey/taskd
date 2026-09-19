# Contrib

Operator scripts that ship with taskd but are not part of the daemon.

## worker

A POSIX sh runner, GNU coreutils and util-linux flavoured, that claims
tasks from a taskd queue and executes an agent inside an isolated,
reusable git worktree slot. Slots are locked with `flock(1)` and acquired
automatically, so workers can share one repository without stepping on
each other.

Prerequisites in `PATH`: `git`, `curl`, `flock`, `seq`, `readlink`,
`timeout`, `awk`, `sed`, `grep`, and the `omp` agent CLI, plus a reachable
taskd instance.

```sh
contrib/worker -h                       # usage
contrib/worker status                   # inspect slots and recent activity
contrib/worker "fix the failing test"   # run one agent in the first free slot
```

Environment: `TASKD_URL` (daemon address, falling back to `T`, then
`http://localhost:8080`), `TASKD_PROJECT` (default queue name),
`TASKD_WT_BASE` (worktree root).
