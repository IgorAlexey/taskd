package taskd

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type config struct {
	dbPath      string
	addr        string
	lease       int
	maxClaims   int
	backupPath  string
	corsOrigin  string
	token       string
	version     bool
	logRequests bool
}

type usageError struct {
	err error
}

func (e *usageError) Error() string { return e.err.Error() }

func (e *usageError) Unwrap() error { return e.err }

func usagef(format string, a ...any) *usageError {
	return &usageError{err: fmt.Errorf(format, a...)}
}

const undefinedFlagPrefix = "flag provided but not defined: -"

func typedFlag(args []string, name string) string {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		token, _, _ := strings.Cut(arg, "=")
		if len(token) > 1 && token[0] == '-' && strings.TrimLeft(token, "-") == name {
			return token
		}
	}
	return "-" + name
}

const (
	defaultDBPath      = "taskd.db"
	defaultAddr        = "127.0.0.1:8080"
	defaultLease       = 300
	defaultMaxClaims   = 0
	defaultLogRequests = true
)

func newFlagSet(cfg *config) *flag.FlagSet {
	fs := flag.NewFlagSet("taskd", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.StringVar(&cfg.dbPath, "db", cfg.dbPath, "database path")
	fs.StringVar(&cfg.addr, "addr", cfg.addr, "listen address (e.g. :8080 to expose on all interfaces)")
	fs.IntVar(&cfg.lease, "lease", cfg.lease, "lease duration in seconds")
	fs.IntVar(&cfg.maxClaims, "max-claims", cfg.maxClaims, "bury a task after this many claims (0 = unlimited)")
	fs.StringVar(&cfg.backupPath, "backup", "", "backup destination path")
	fs.StringVar(&cfg.corsOrigin, "cors-origin", cfg.corsOrigin, "allowed CORS origin")
	fs.StringVar(&cfg.token, "token", cfg.token, "require this bearer token on every request")
	fs.BoolVar(&cfg.logRequests, "log-requests", cfg.logRequests, "log completed HTTP requests")
	fs.BoolVar(&cfg.version, "v", false, "print version and exit")
	fs.BoolVar(&cfg.version, "version", false, "print version and exit")
	return fs
}

const maxLeaseSeconds = 31536000

func parseFlags(args []string) (config, error) {
	cfg := config{
		dbPath:      defaultDBPath,
		addr:        defaultAddr,
		lease:       defaultLease,
		maxClaims:   defaultMaxClaims,
		logRequests: defaultLogRequests,
	}
	if v := os.Getenv("TASKD_DB"); v != "" {
		cfg.dbPath = v
	}
	if v := os.Getenv("TASKD_ADDR"); v != "" {
		cfg.addr = v
	}
	if raw := os.Getenv("TASKD_LEASE"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return cfg, usagef("invalid value %q for TASKD_LEASE: %w", raw, err)
		}
		if v <= 0 || v > maxLeaseSeconds {
			return cfg, usagef("TASKD_LEASE must be between 1 and %d seconds: got %d", maxLeaseSeconds, v)
		}
		cfg.lease = v
	}
	if raw := os.Getenv("TASKD_MAX_CLAIMS"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return cfg, usagef("invalid value %q for TASKD_MAX_CLAIMS: %w", raw, err)
		}
		if v < 0 {
			return cfg, usagef("TASKD_MAX_CLAIMS cannot be negative: got %d", v)
		}
		cfg.maxClaims = v
	}
	if v := os.Getenv("TASKD_CORS_ORIGIN"); v != "" {
		cfg.corsOrigin = v
	}
	if v := os.Getenv("TASKD_TOKEN"); v != "" {
		cfg.token = v
	}
	if v := os.Getenv("TASKD_LOG_REQUESTS"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return cfg, usagef("invalid value %q for TASKD_LOG_REQUESTS: %w", v, err)
		}
		cfg.logRequests = b
	}

	fs := newFlagSet(&cfg)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return cfg, err
		}
		if name, ok := strings.CutPrefix(err.Error(), undefinedFlagPrefix); ok {
			return cfg, usagef("unrecognized flag %s", typedFlag(args, name))
		}
		return cfg, usagef("%w", err)
	}
	if len(fs.Args()) > 0 {
		return cfg, usagef("unexpected argument: %s", fs.Args()[0])
	}
	if cfg.version {
		return cfg, nil
	}
	cfg.dbPath = strings.TrimSpace(cfg.dbPath)
	if cfg.dbPath == "" {
		return cfg, usagef("database path cannot be empty")
	}
	cfg.addr = strings.TrimSpace(cfg.addr)
	if cfg.addr == "" {
		return cfg, usagef("listen address cannot be empty")
	}
	if cfg.lease <= 0 || cfg.lease > maxLeaseSeconds {
		return cfg, usagef("-lease must be between 1 and %d seconds: got %d", maxLeaseSeconds, cfg.lease)
	}
	if cfg.maxClaims < 0 {
		return cfg, usagef("max claims cannot be negative: got %d", cfg.maxClaims)
	}
	return cfg, nil
}
