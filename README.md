<h1 align="center">taskd</h1>

<p align="center">
  <strong>a lightweight task queue and tracker in a single binary.</strong>
  <br>
  <strong><a href="https://igoralexey.com/taskd">igoralexey.com/taskd</a></strong>
</p>

<table>
  <tr>
    <td width="50%"><img src="docs/webui.png" alt="taskd web UI: a project ledger with stuck, claimed, queued and done tasks"></td>
    <td width="50%"><img src="docs/tui.png" alt="taskd terminal UI: the queue table with a task and its notes below"></td>
  </tr>
</table>

taskd is a task queue daemon with atomic leases, dependency tracking, and
embedded web and terminal interfaces.

Workers claim tasks under time-limited leases and either mark them done or
let them return to the queue. Tasks can wait on other tasks before becoming
claimable.

## Quickstart

Start the daemon:

```sh
taskd -addr 127.0.0.1:8080
```

Add and work tasks from the command line:

```sh
export TASKD_URL=http://127.0.0.1:8080 TASKD_PROJECT=myapp
taskd add 'Fix the login redirect'
taskd claim
taskd note 1 'root cause is the cookie path'
taskd done 1
```

Open `http://127.0.0.1:8080/ui` in a browser for the web ledger, or launch
the terminal UI:

```sh
taskd-tui
```

## Install

```sh
go install github.com/IgorAlexey/taskd@latest
go install github.com/IgorAlexey/taskd/cmd/taskd-tui@latest
```
