package taskd

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"
)

// The same binary is the client: `taskd claim` talks to a running daemon.
// Every command prints the daemon's JSON on stdout as it came, errors are
// one line on stderr, and the exit code says what happened: 0 done, 1 the
// daemon said no or is unreachable, 2 bad usage, 3 nothing to claim.

const exitNoTask = 3

// what each command takes; the daemon usage lists them all
var synopsis = map[string]string{
	"add":     "[body]",
	"claim":   "[id]",
	"done":    "ID",
	"close":   "ID",
	"touch":   "[ID]",
	"release": "ID",
	"bury":    "ID",
	"kick":    "ID",
	"note":    "ID [text]",
	"show":    "ID",
	"list":    "",
}

type client struct {
	url    string
	token  string
	worker string
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func runClient(stdout, stderr io.Writer, stdin io.Reader, args []string) int {
	c := &client{stdin: stdin, stdout: stdout, stderr: stderr, token: os.Getenv("TASKD_TOKEN")}
	cmd, rest := args[0], args[1:]
	if cmd == "help" {
		if len(rest) == 0 || rest[0] == "serve" || rest[0] == "help" {
			printUsage(stdout)
			return 0
		}
		cmd, rest = rest[0], []string{"-h"}
	}
	if _, ok := synopsis[cmd]; !ok {
		fmt.Fprintf(stderr, "taskd: unknown command %q\ntry 'taskd help'\n", cmd)
		return 2
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	usage := func(w io.Writer) {
		fmt.Fprintf(w, "Usage: taskd %s [flags] %s\n", cmd, synopsis[cmd])
		fs.SetOutput(w)
		fs.PrintDefaults()
	}
	fs.StringVar(&c.url, "url", envOr("TASKD_URL", "http://127.0.0.1:8080"), "daemon address")
	var pri *int // nil unless -p was given
	priority := func() {
		fs.Func("p", "priority, lower runs first", func(s string) error {
			v, err := strconv.Atoi(s)
			pri = &v
			return err
		})
	}
	worker := func() { fs.StringVar(&c.worker, "worker", envOr("TASKD_WORKER", defaultWorker()), "worker name") }
	project := func() *string { return fs.String("project", os.Getenv("TASKD_PROJECT"), "project name") }
	quiet := func() *bool { return fs.Bool("q", false, "print only the id") }

	var run func(args []string) error
	switch cmd {
	case "add":
		p, q := project(), quiet()
		priority()
		after := fs.String("after", "", "comma-separated task ids this one waits for")
		run = func(args []string) error { return c.add(args, *p, pri, *after, *q) }
	case "claim":
		worker()
		p, q := project(), quiet()
		wait := fs.Float64("wait", 0, "seconds to wait for a task")
		run = func(args []string) error { return c.claim(args, *p, *wait, *q) }
	case "done":
		worker()
		result := fs.String("result", "", "JSON result stored with the task; \"-\" reads stdin")
		run = func(args []string) error { return c.done(args, *result) }
	case "close", "kick":
		run = func(args []string) error { return c.act(cmd, args, map[string]any{}) }
	case "touch":
		worker()
		run = func(args []string) error {
			if len(args) == 0 {
				return c.print("POST", "/tasks/touch", map[string]any{"worker": c.worker}, false)
			}
			return c.act(cmd, args, map[string]any{"worker": c.worker})
		}
	case "release":
		worker()
		run = func(args []string) error { return c.act(cmd, args, map[string]any{"worker": c.worker}) }
	case "bury":
		worker()
		priority()
		run = func(args []string) error {
			body := map[string]any{"worker": c.worker}
			if pri != nil {
				body["priority"] = *pri
			}
			return c.act(cmd, args, body)
		}
	case "note":
		worker()
		run = c.note
	case "show":
		run = c.show
	case "list":
		p := project()
		status := fs.String("status", "", "pending | leased | done | buried | live")
		query := fs.String("q", "", "substring of id, body, project, or worker")
		limit := fs.Int("limit", 100, "at most this many tasks")
		run = func(args []string) error { return c.list(args, *p, *status, *query, *limit) }
	}
	// flag stops at the first bare word, and `done 7 -result x` is the natural
	// order; sort argv the way flag itself reads it, -- ending the flags
	var flags, pos []string
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if a == "--" {
			pos = append(pos, rest[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			pos = append(pos, a)
			continue
		}
		flags = append(flags, a)
		name, _, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if f := fs.Lookup(name); f != nil && !hasValue && i+1 < len(rest) {
			if b, ok := f.Value.(interface{ IsBoolFlag() bool }); !ok || !b.IsBoolFlag() {
				i++
				flags = append(flags, rest[i])
			}
		}
	}
	if err := fs.Parse(flags); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			usage(stdout)
			return 0
		}
		fmt.Fprintf(stderr, "taskd: %v\n", err)
		usage(stderr)
		return 2
	}
	err := run(pos)
	var ue *usageError
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errNoTask):
		return exitNoTask
	case errors.As(err, &ue):
		fmt.Fprintf(stderr, "taskd: %v\n", err)
		usage(stderr)
		return 2
	default:
		fmt.Fprintf(stderr, "taskd: %v\n", err)
		return 1
	}
}

var errNoTask = errors.New("no task")

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// defaultWorker is user.host with anything the daemon would refuse in a
// worker name dropped, or "worker" when nothing is left.
func defaultWorker() string {
	host, _ := os.Hostname()
	name := host
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username + "." + host
	}
	name = strings.Map(func(r rune) rune {
		if r < 128 && validNameOrPathByte(byte(r)) {
			return r
		}
		return -1
	}, name)
	if w, err := cleanWorker(name); err == nil {
		return w
	}
	return "worker"
}

func (c *client) add(args []string, project string, pri *int, after string, quiet bool) error {
	if len(args) > 1 {
		return usagef("add takes one body")
	}
	body, err := c.text(args)
	if err != nil {
		return err
	}
	if body == "" {
		return usagef("add needs a body")
	}
	if project == "" {
		return usagef("add needs -project or TASKD_PROJECT")
	}
	req := map[string]any{"project": project, "body": body}
	if pri != nil {
		req["priority"] = *pri
	}
	if after != "" {
		var ids []int64
		for _, s := range strings.Split(after, ",") {
			id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			if err != nil {
				return usagef("bad -after id %q", s)
			}
			ids = append(ids, id)
		}
		req["after"] = ids
	}
	return c.print("POST", "/tasks", req, quiet)
}

func (c *client) claim(args []string, project string, wait float64, quiet bool) error {
	if len(args) > 1 {
		return usagef("claim takes at most one id")
	}
	if len(args) == 1 {
		id, err := taskID(args[0])
		if err != nil {
			return err
		}
		return c.print("POST", "/tasks/"+id+"/claim", map[string]any{"worker": c.worker}, quiet)
	}
	req := map[string]any{"worker": c.worker}
	if project != "" {
		req["project"] = project
	}
	if wait > 0 {
		req["wait"] = wait
	}
	return c.print("POST", "/tasks/claim", req, quiet)
}

func (c *client) done(args []string, result string) error {
	if len(args) != 1 {
		return usagef("done takes one id")
	}
	req := map[string]any{"worker": c.worker}
	if result == "-" {
		b, err := io.ReadAll(c.stdin)
		if err != nil {
			return err
		}
		result = string(b)
	}
	if strings.TrimSpace(result) != "" {
		if !json.Valid([]byte(result)) {
			return usagef("-result is not JSON")
		}
		req["primitives"] = json.RawMessage(result)
	}
	return c.act("done", args, req)
}

func (c *client) act(verb string, args []string, req map[string]any) error {
	if len(args) != 1 {
		return usagef("%s takes one id", verb)
	}
	id, err := taskID(args[0])
	if err != nil {
		return err
	}
	return c.print("POST", "/tasks/"+id+"/"+verb, req, false)
}

func (c *client) note(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return usagef("note takes an id and text")
	}
	id, err := taskID(args[0])
	if err != nil {
		return err
	}
	text, err := c.text(args[1:])
	if err != nil {
		return err
	}
	if text == "" {
		return usagef("note needs text")
	}
	return c.print("POST", "/tasks/"+id+"/notes", map[string]any{"author": c.worker, "text": text}, false)
}

func (c *client) show(args []string) error {
	if len(args) != 1 {
		return usagef("show takes one id")
	}
	id, err := taskID(args[0])
	if err != nil {
		return err
	}
	return c.print("GET", "/tasks/"+id, nil, false)
}

func (c *client) list(args []string, project, status, query string, limit int) error {
	if len(args) != 0 {
		return usagef("list takes no arguments")
	}
	q := url.Values{}
	if project != "" {
		q.Set("project", project)
	}
	if status != "" {
		q.Set("status", status)
	}
	if query != "" {
		q.Set("q", query)
	}
	q.Set("limit", strconv.Itoa(limit))
	return c.print("GET", "/tasks?"+q.Encode(), nil, false)
}

// text is the body or note: the argument, or stdin when the argument is
// "-" or absent and something was piped in.
func (c *client) text(args []string) (string, error) {
	if len(args) == 1 && args[0] != "-" {
		return args[0], nil
	}
	if len(args) == 0 {
		if f, ok := c.stdin.(*os.File); ok {
			if st, err := f.Stat(); err != nil || st.Mode()&os.ModeCharDevice != 0 {
				return "", nil
			}
		}
	}
	b, err := io.ReadAll(c.stdin)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

func taskID(s string) (string, error) {
	if _, err := strconv.ParseInt(s, 10, 64); err != nil {
		return "", usagef("bad task id %q", s)
	}
	return s, nil
}

// print does the round trip and writes the reply as it came. A 204 prints
// nothing; a 204 from claim means there was nothing to claim.
func (c *client) print(method, path string, req any, quiet bool) error {
	var body io.Reader
	if req != nil {
		b, err := json.Marshal(req)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	hr, err := http.NewRequest(method, c.url+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		hr.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		hr.Header.Set("Authorization", "Bearer "+c.token)
	}
	timeout := 30 * time.Second
	if m, ok := req.(map[string]any); ok {
		if w, ok := m["wait"].(float64); ok {
			timeout += time.Duration(w * float64(time.Second))
		}
	}
	res, err := (&http.Client{Timeout: timeout}).Do(hr)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("%s: %w", c.url, err)
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode == http.StatusNoContent {
		if strings.HasSuffix(path, "/claim") {
			return errNoTask
		}
		return nil
	}
	if res.StatusCode == http.StatusUnauthorized {
		if c.token == "" {
			return errors.New("unauthorized: the daemon requires TASKD_TOKEN")
		}
		return errors.New("unauthorized: the daemon rejected TASKD_TOKEN")
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(out, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("%s %s: %s", method, path, res.Status)
	}
	if quiet {
		var t struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(out, &t); err != nil {
			return err
		}
		fmt.Fprintln(c.stdout, t.ID)
		return nil
	}
	c.stdout.Write(bytes.TrimRight(out, "\n"))
	fmt.Fprintln(c.stdout)
	return nil
}
