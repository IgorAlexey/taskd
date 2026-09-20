package main

import (
	"encoding/json"
	"time"

	"charm.land/bubbles/v2/viewport"
)

// task mirrors the daemon's JSON representation of a queue entry.
type task struct {
	ID           string          `json:"id"`
	Project      string          `json:"project"`
	AssetPath    string          `json:"asset_path"`
	Status       string          `json:"status"`
	Worker       string          `json:"worker"`
	LeaseExpires int64           `json:"lease_expires"`
	Priority     int             `json:"priority"`
	ClaimCount   int             `json:"claim_count"`
	Body         string          `json:"body"`
	Primitives   json.RawMessage `json:"primitives"`
}

// stats mirrors GET /stats. LeaseSeconds and DB are additive fields the
// daemon gained for the TUI; zero values mean an older daemon.
type stats struct {
	Pending      int    `json:"pending"`
	Leased       int    `json:"leased"`
	Done         int    `json:"done"`
	Buried       int    `json:"buried"`
	Total        int    `json:"total"`
	LeaseSeconds int    `json:"lease_seconds"`
	DB           string `json:"db"`
}

// config is the parsed command line.
type config struct {
	url     string
	project string
	worker  string
	icons   bool
	refresh time.Duration
}

// mode is the single source of truth for which keymap and overlay are
// active. Exactly one mode is active at a time.
type mode int

const (
	modeTable   mode = iota // cursor in the task table
	modeSearch              // typing into the / filter
	modeDetail              // Tab: keys scroll the detail pane
	modeZoom                // z: detail pane fills the screen
	modeForm                // n/e: create or edit form overlay
	modeConfirm             // D/x: yes/no overlay
	modeHelp                // ?: key reference overlay
)

// Messages. Every asynchronous result enters Update as one of these.
type (
	// tickMsg fires once per cfg.refresh; it advances m.now and starts a
	// poll when none is in flight.
	tickMsg time.Time

	// pollMsg is one refresh round trip: list, stats, projects. changed is
	// false when the daemon answered 304 for the list, in which case tasks
	// is nil and the model keeps its current slice.
	pollMsg struct {
		tasks    []task
		etag     string
		changed  bool
		stats    stats
		projects []string
		err      error
	}

	// actMsg is the result of a mutating request (claim, release, patch,
	// delete, done, create). msg is the success text for the footer.
	actMsg struct {
		msg string
		err error
	}

	// clearMsgMsg expires the footer message with matching id.
	clearMsgMsg struct{ id int }
)

// model is the whole application state. View is a pure function of it.
type model struct {
	cfg    config
	client *client
	theme  theme
	glyph  glyphs

	width, height int
	now           time.Time

	tasks    []task // last full list from the daemon, daemon order
	shown    []int  // indices into tasks after project, status, query
	cursor   int    // index into shown; 0 <= cursor < len(shown) or 0
	offset   int    // first index of shown drawn in the table
	filter   string // "", "pending", "leased", "done", "buried" (keys 0-4)
	project  string // "" means all projects
	query    string // / substring filter, case-insensitive
	mode     mode
	etag     string // tag of m.tasks, sent as If-None-Match
	polling  bool   // a pollCmd is in flight; cleared by pollMsg
	stats    stats
	hasStats bool
	projects []string

	connected bool
	lastErr   string
	msg       string
	msgID     int

	detail   viewport.Model // scrolls the detail pane body
	detailID string         // task the viewport content was built for

	form    formModel
	confirm confirmModel
}

// Layout constants shared by view and model (paging, offset clamping).
const (
	headerRows  = 1 // pill, url, connection, db, refresh
	tabRows     = 1 // status tabs and project selector
	colHeadRows = 1 // table column header
	footerRows  = 1 // key legend and position
	gapRows     = 2 // blank line above the table and above the detail rule
	minDetail   = 6 // detail pane never shrinks below this
	minTable    = 3 // table never shrinks below this
)
