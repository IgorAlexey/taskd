// Package taskd is the whole binary: the daemon, its HTTP API and web
// page, and the command line that talks to it.
package taskd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/IgorAlexey/taskd/internal/version"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func runServer(ctx context.Context, l net.Listener, db *store, lease int, corsOrigin string, logRequests bool) error {
	handler := newHandlerWithCORS(db, lease, corsOrigin)
	if logRequests {
		handler = withRequestLogging(handler)
	}
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverCtx, cancelServer := context.WithCancel(ctx)
	defer cancelServer()
	go db.runCheckpointer(serverCtx)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
		if err := db.checkpoint(context.Background()); err != nil {
			log.Printf("shutdown checkpoint error: %v", err)
		}
		return <-errCh
	}
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
		fmt.Fprintf(stdout, "taskd %s\n", version.Version)
		return nil
	}

	if cfg.backupPath != "" {
		log.SetFlags(0)
		return backupTo(cfg.dbPath, cfg.backupPath, stdout)
	}

	l, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return err
	}
	defer l.Close()

	db, isNew, err := openDBInit(cfg.dbPath, cfg.maxClaims)
	if err != nil {
		return err
	}
	defer db.Close()

	logStartupDB(cfg.dbPath, cfg.lease, isNew)

	if tcp, ok := l.Addr().(*net.TCPAddr); ok && tcp.IP.IsUnspecified() {
		log.Print("warning: listening on all interfaces; taskd has no authentication")
	}
	log.Printf("listening on %s", l.Addr())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return runServer(ctx, l, db, cfg.lease, cfg.corsOrigin, cfg.logRequests)
}

func logStartupDB(dbPath string, lease int, isNew bool) {
	if isNew {
		log.Printf("created new database %s (lease %ds)", dbPath, lease)
	} else {
		log.Printf("opened database %s (lease %ds)", dbPath, lease)
	}
}

func fatal(stderr io.Writer, err error) int {
	var ue *usageError
	if errors.As(err, &ue) {
		fmt.Fprintf(stderr, "taskd: %v\ntry 'taskd -h' for usage\n", err)
		return 2
	}
	fmt.Fprintf(stderr, "taskd: %v\n", err)
	var se *sqlite.Error
	if errors.As(err, &se) && (se.Code() == sqlite3.SQLITE_CANTOPEN || se.Code()&0xff == sqlite3.SQLITE_CANTOPEN) {
		fmt.Fprintln(stderr, "       is -db pointing at a directory, or a path you cannot write?")
	}
	return 1
}

// Main runs the binary: a bare word is a client command, anything else
// is the daemon.
func Main(stdout, stderr io.Writer, stdin io.Reader, args []string) int {
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	} else if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return runClient(stdout, stderr, stdin, args)
	}
	if err := run(stdout, args); err != nil {
		return fatal(stderr, err)
	}
	return 0
}
