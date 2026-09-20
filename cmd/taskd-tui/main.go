package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
)

func parseAndValidateURL(raw string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return "", errors.New("url cannot be empty")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid url scheme %q: must be http or https", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("url missing host")
	}
	return trimmed, nil
}

func parseFlags(args []string) (config, error) {
	defaultURL := os.Getenv("TASKD_URL")
	if defaultURL == "" {
		defaultURL = "http://localhost:8080"
	}
	defaultProject := os.Getenv("TASKD_PROJECT")
	envAscii := strings.ToLower(strings.TrimSpace(os.Getenv("TASKD_ASCII")))
	defaultAscii := envAscii == "1" || envAscii == "true"

	var (
		cfg     config
		rawURL  string
		ascii   bool
		refresh time.Duration
	)

	fs := flag.NewFlagSet("taskd-tui", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		printUsage(os.Stderr)
	}

	fs.StringVar(&rawURL, "url", defaultURL, "taskd daemon URL")
	fs.StringVar(&cfg.project, "project", defaultProject, "filter tasks by project")
	fs.StringVar(&cfg.worker, "worker", defaultWorker(), "worker identifier for claiming tasks")
	fs.BoolVar(&ascii, "ascii", defaultAscii, "use ASCII characters instead of Nerd Font icons")
	fs.DurationVar(&refresh, "refresh", time.Second, "polling interval (min 250ms)")

	if err := fs.Parse(args); err != nil {
		return cfg, err
	}

	if fs.NArg() > 0 {
		return cfg, fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}

	validURL, err := parseAndValidateURL(rawURL)
	if err != nil {
		return cfg, err
	}
	cfg.url = validURL

	if refresh < 250*time.Millisecond {
		return cfg, fmt.Errorf("refresh interval must be at least 250ms: got %v", refresh)
	}
	cfg.refresh = refresh
	cfg.icons = !ascii

	return cfg, nil
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: taskd-tui [options]

Terminal user interface for the taskd task queue.

Options:
  -url <url>          taskd daemon URL (default: http://localhost:8080)
  -project <name>     filter tasks by project
  -worker <name>      worker identifier for claiming tasks
  -ascii              use ASCII characters instead of Nerd Font icons
  -refresh <dur>      polling interval, min 250ms (default: 1s)
  -h, --help          show this help message

Environment variables:
  TASKD_URL           taskd daemon URL
  TASKD_PROJECT       default project filter
  TASKD_WORKER        worker identifier for claiming tasks
  TASKD_ASCII         set to 1 or true to enable ASCII mode

Keyboard shortcuts:
  j/k, Up/Down        move selection
  g/G                 jump to first / last row
  ctrl-d/ctrl-u       move half page down / up
  PgUp/PgDn           move page down / up
  0-4                 filter status (0: all, 1: pending, 2: leased, 3: done)
  p                   cycle project filter
  /                   search / filter by query
  Tab                 switch focus to detail pane
  z                   toggle detail zoom
  n                   create new task
  e                   edit selected task
  +/-                 raise / lower task priority
  c                   claim selected pending task
  u                   release selected leased task
  D                   delete selected task
  x                   complete selected task
  y/Y                 copy task ID / body to clipboard
  ctrl-s              submit form
  r                   force refresh
  ?                   show help overlay
  q                   quit

Examples:
  taskd-tui -url http://localhost:8080 -project taskd
  taskd-tui -ascii -refresh 2s
`)
}

func defaultWorker() string {
	if w := os.Getenv("TASKD_WORKER"); w != "" {
		return w
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "tui"
	}
	checkout := gitCheckoutName()
	if checkout == "" {
		cwd, err := os.Getwd()
		if err == nil && cwd != "" {
			checkout = filepath.Base(cwd)
		}
	}
	if checkout == "" {
		checkout = "tui"
	}
	return host + ":" + checkout
}

func gitCheckoutName() string {
	cur, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		gitPath := filepath.Join(cur, ".git")
		fi, err := os.Stat(gitPath)
		if err == nil {
			if !fi.IsDir() {
				data, err := os.ReadFile(gitPath)
				if err == nil {
					content := strings.TrimSpace(string(data))
					for _, line := range strings.Split(content, "\n") {
						line = strings.TrimSpace(line)
						if gd, ok := strings.CutPrefix(line, "gitdir:"); ok {
							gd = strings.TrimSpace(gd)
							if !filepath.IsAbs(gd) {
								gd = filepath.Clean(filepath.Join(cur, gd))
							}
							slashGD := filepath.ToSlash(gd)
							if before, _, found := strings.Cut(slashGD, "/.git/worktrees/"); found {
								return filepath.Base(before)
							}
							if before, _, found := strings.Cut(slashGD, "/.git"); found {
								return filepath.Base(before)
							}
						}
					}
				}
			}
			return filepath.Base(cur)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return ""
}

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "taskd-tui: %v\n", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	p := tea.NewProgram(newModel(cfg, newClient(cfg.url)), tea.WithContext(ctx))
	if _, err := p.Run(); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "taskd-tui: %v\n", err)
		os.Exit(1)
	}
}
