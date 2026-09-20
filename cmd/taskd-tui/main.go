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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/IgorAlexey/taskd/internal/version"

	tea "charm.land/bubbletea/v2"
)

func parseAndValidateURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("url cannot be empty")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "http://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid url scheme %q: must be http or https", u.Scheme)
	}
	if strings.HasPrefix(u.Host, ":") {
		u.Host = "127.0.0.1" + u.Host
	}
	if u.Host == "" {
		return "", errors.New("url missing host")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

type usageError struct {
	err error
}

func (e *usageError) Error() string { return e.err.Error() }

func (e *usageError) Unwrap() error { return e.err }

func usagef(format string, a ...any) *usageError {
	return &usageError{err: fmt.Errorf(format, a...)}
}

const maxProjectLen = 64

func validateProject(p string) error {
	if len(p) > maxProjectLen {
		return fmt.Errorf("invalid project name %q: must not exceed 64 characters", p)
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-') {
			return fmt.Errorf("invalid project name %q: must contain only [A-Za-z0-9._-]", p)
		}
	}
	return nil
}

func parseFlags(args []string) (config, error) {
	defaultURL := os.Getenv("TASKD_URL")
	if defaultURL == "" {
		defaultURL = "http://localhost:8080"
	}
	defaultProject := os.Getenv("TASKD_PROJECT")
	defaultQuery := os.Getenv("TASKD_QUERY")
	envAscii := strings.ToLower(strings.TrimSpace(os.Getenv("TASKD_ASCII")))
	defaultAscii := envAscii == "1" || envAscii == "true"

	var (
		cfg     config
		rawURL  = defaultURL
		ascii   = defaultAscii
		refresh = time.Second
	)
	if raw := os.Getenv("TASKD_REFRESH"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return cfg, usagef("invalid duration %q for TASKD_REFRESH", raw)
		}
		refresh = d
	}
	cfg.project = defaultProject
	cfg.worker = defaultWorker()
	cfg.query = defaultQuery

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				return cfg, usagef("unexpected argument: %s", args[i+1])
			}
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			return cfg, usagef("unexpected argument: %s", arg)
		}

		token, val, hasVal := strings.Cut(arg, "=")
		name := token
		if strings.HasPrefix(name, "--") {
			name = name[2:]
		} else {
			name = name[1:]
		}

		switch name {
		case "h", "help":
			return cfg, flag.ErrHelp
		case "v", "version":
			cfg.version = true
		case "ascii":
			if hasVal {
				b, err := strconv.ParseBool(val)
				if err != nil {
					return cfg, usagef("invalid boolean value %q for %s", val, token)
				}
				ascii = b
			} else {
				ascii = true
			}
		case "url":
			if !hasVal {
				if i+1 >= len(args) {
					return cfg, usagef("flag needs an argument: %s", token)
				}
				i++
				val = args[i]
			}
			rawURL = val
		case "project":
			if !hasVal {
				if i+1 >= len(args) {
					return cfg, usagef("flag needs an argument: %s", token)
				}
				i++
				val = args[i]
			}
			cfg.project = val
		case "worker":
			if !hasVal {
				if i+1 >= len(args) {
					return cfg, usagef("flag needs an argument: %s", token)
				}
				i++
				val = args[i]
			}
			cfg.worker = val
		case "q", "query":
			if !hasVal {
				if i+1 >= len(args) {
					return cfg, usagef("flag needs an argument: %s", token)
				}
				i++
				val = args[i]
			}
			cfg.query = val
		case "refresh":
			if !hasVal {
				if i+1 >= len(args) {
					return cfg, usagef("flag needs an argument: %s", token)
				}
				i++
				val = args[i]
			}
			d, err := time.ParseDuration(val)
			if err != nil {
				return cfg, usagef("invalid duration %q for flag %s", val, token)
			}
			refresh = d
		case "s", "sort":
			if !hasVal {
				if i+1 >= len(args) {
					return cfg, usagef("flag needs an argument: %s", token)
				}
				i++
				val = args[i]
			}
			col, ok := parseSortColumn(val)
			if !ok {
				return cfg, usagef("invalid sort column %q for %s", val, token)
			}
			cfg.sortCol = col
		default:
			return cfg, usagef("unrecognized flag %s", token)
		}
	}

	if cfg.version {
		return cfg, nil
	}

	validURL, err := parseAndValidateURL(rawURL)
	if err != nil {
		return cfg, usagef("%w", err)
	}
	cfg.url = validURL

	if refresh < 250*time.Millisecond {
		return cfg, usagef("refresh interval must be at least 250ms: got %v", refresh)
	}
	cfg.refresh = refresh
	cfg.icons = !ascii

	if cfg.project != "" {
		if err := validateProject(cfg.project); err != nil {
			return cfg, usagef("%w", err)
		}
	}
	return cfg, nil
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: taskd-tui [options]

Terminal user interface for the taskd task queue.

Options:
  -url <url>          taskd daemon URL (default: http://localhost:8080)
  -project <name>     filter tasks by project
  -worker <name>      worker identifier for claiming tasks
  -q, -query <query>  filter tasks by search query
  -ascii              use ASCII characters instead of Nerd Font icons
  -refresh <dur>      polling interval, min 250ms (default: 1s)
  -s, -sort <col>     initial sort column: priority, status, project, worker, lease
  -v, -version        print version and exit
  -h, --help          show this help message

Environment variables:
  TASKD_URL           taskd daemon URL
  TASKD_PROJECT       default project filter
  TASKD_WORKER        worker identifier for claiming tasks
  TASKD_QUERY         default search query filter
  TASKD_ASCII         set to 1 or true to enable ASCII mode
  TASKD_REFRESH       polling interval, min 250ms (default: 1s)

Keyboard shortcuts:
  j/k, Up/Down        move selection
  g/G                 jump to first / last row
  ctrl-d/ctrl-u       move half page down / up
  PgUp/PgDn           move page down / up
  0-5                 filter status (0: all, 1: pending, 2: leased, 3: done, 4: buried, 5: live)
  s                   cycle sort (lower case 's': priority, status, project, worker, lease)
  p, P                cycle project filter forward / backward
  w, W                cycle worker filter forward / backward
  /                   search / filter by query
  Tab                 switch focus to detail pane
  z                   toggle detail zoom
  n                   create new task
  e                   edit selected task
  a                   add note
  +/-                 raise / lower task priority
  c                   claim selected pending task
  u                   release selected leased task
  t                   touch (extend lease) selected task
  b                   bury selected leased task
  K                   kick selected buried task
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

func parseSortColumn(val string) (sortColumn, bool) {
	switch strings.ToLower(val) {
	case "priority":
		return sortPriority, true
	case "status":
		return sortStatus, true
	case "project":
		return sortProject, true
	case "worker":
		return sortWorker, true
	case "lease":
		return sortLease, true
	default:
		return 0, false
	}
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

func reportError(stderr io.Writer, err error) int {
	var ue *usageError
	if errors.As(err, &ue) {
		fmt.Fprintf(stderr, "taskd-tui: %v\ntry 'taskd-tui -h' for usage\n", err)
		return 2
	}
	fmt.Fprintf(stderr, "taskd-tui: %v\n", err)
	return 1
}

func run(stdout io.Writer, args []string) error {
	cfg, err := parseFlags(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			return nil
		}
		return err
	}
	if cfg.version {
		fmt.Fprintf(stdout, "taskd-tui %s\n", version.Version)
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	p := tea.NewProgram(newModel(cfg, newClient(cfg.url)), tea.WithContext(ctx))
	if _, err := p.Run(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func main() {
	if err := run(os.Stdout, os.Args[1:]); err != nil {
		os.Exit(reportError(os.Stderr, err))
	}
}
