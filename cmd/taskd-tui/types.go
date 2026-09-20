package main

import (
	"encoding/json"
	"time"

	"charm.land/bubbles/v2/viewport"
)

// task mirrors the daemon's JSON representation of a queue entry.
type taskNote struct {
	ID        int64  `json:"id"`
	CreatedAt int64  `json:"created_at"`
	Author    string `json:"author"`
	Text      string `json:"text"`
}

type task struct {
	ID           string          `json:"id"`
	Project      string          `json:"project"`
	AssetPath    string          `json:"asset_path"`
	Status       string          `json:"status"`
	Worker       string          `json:"worker"`
	LeaseExpires int64           `json:"lease_expires"`
	Priority     int             `json:"priority"`
	ClaimCount   int             `json:"claim_count"`
	CreatedAt    int64           `json:"created_at"`
	Version      int             `json:"version"`
	Body         string          `json:"body"`
	Primitives   json.RawMessage `json:"primitives"`
	Notes        []taskNote      `json:"notes"`
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
	query   string
	status  string
	icons   bool
	refresh time.Duration
	version bool
	sortCol sortColumn
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
	modeNote
)

type sortColumn int

const (
	sortPriority sortColumn = iota
	sortStatus
	sortProject
	sortWorker
	sortLease
	sortColCount
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
		seq      uint64    // the startPoll generation this reply answers
		scope    listScope // the question this reply answers
		tasks    []task
		etag     string
		changed  bool
		total    int  // the daemon's count for the question
		more     bool // rows the daemon held back
		stats    stats
		projects []string
		workers  []string
		err      error
	}

	// statsMsg is a counters-only refresh, used while a paged snapshot
	// holds the task list still.
	statsMsg struct {
		stats stats
		err   error
	}

	// actMsg is the result of a mutating request (claim, release, patch,
	// delete, done, create). msg is the success text for the footer.
	actMsg struct {
		msg string
		err error
	}

	formActMsg struct {
		seq uint64
		msg string
		err error
	}

	// searchMsg fires once typing pauses; a stale generation is dropped,
	// so a term only reaches the daemon when the operator stops typing.
	searchMsg struct{ seq uint64 }

	noteFetchMsg struct {
		id  string
		seq uint64
	}
	taskNotesMsg struct {
		id    string
		notes []taskNote
		err   error
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

	tasks     []task // last full list from the daemon, daemon order
	shown     []int  // indices into tasks after project, status, query
	cursor    int    // index into shown; 0 <= cursor < len(shown) or 0
	lastRow   int
	offset    int    // first index of shown drawn in the table
	filter    string // "", "pending", "leased", "done", "buried", "live" (keys 0-5)
	project   string // "" means all projects
	worker    string
	query     string // / substring filter, case-insensitive
	mode      mode
	sortCol   sortColumn
	cols      tableCols
	etag      string     // ETag of m.tasks for m.project
	pages     int        // pages of the daemon cursor to walk; 1 is a live poll
	total     int        // the daemon's count for the current question
	more      bool       // the daemon holds rows this list does not
	endPages  int        // depth a pending G waits for; 0 when none
	searchSeq uint64     // generation of the newest query keystroke
	asked     listFilter // the question the newest poll carried
	polling   bool       // a pollCmd is in flight; cleared by its pollMsg
	seq       uint64     // generation of the newest poll; older replies are dropped
	formSeq   uint64
	stats     stats
	hasStats  bool // a poll has delivered stats at least once
	projects  []string
	workers   []string

	connected bool
	lastErr   string
	msg       string
	msgID     int

	detail   viewport.Model // scrolls the detail pane body
	detailID string         // task the viewport content was built for

	form       formModel
	confirm    confirmModel
	help       helpModel
	note       noteModel
	notesCache map[string][]taskNote
	noteSeq    uint64
}
type tabDef struct {
	key    string
	name   string
	count  int
	filter string
}
type tabHitTarget struct {
	filter string
	start  int
	end    int
}

type row1Bounds struct {
	tabs   []tabHitTarget
	proj   [2]int
	worker [2]int
}
type footerTarget struct {
	action string
	start  int
	end    int
}
type confirmAction int

const (
	confirmActionNone confirmAction = iota
	confirmActionYes
	confirmActionNo
)

type confirmTarget struct {
	action confirmAction
	y      int
	start  int
	end    int
}
type paneLayout struct {
	tableTop   int
	tableRows  int
	detailTop  int
	detailRows int
}

func (p paneLayout) inTable(y int) bool {
	return p.tableRows > 0 && y >= p.tableTop && y < p.tableTop+p.tableRows
}

func (p paneLayout) inDetail(y int) bool {
	return p.detailRows > 0 && y >= p.detailTop && y < p.detailTop+p.detailRows
}

func (p paneLayout) detailViewportTop() int {
	return p.detailTop + detailHeaderRows
}

func (p paneLayout) detailViewportRows() int {
	if p.detailRows <= detailHeaderRows {
		return 0
	}
	return p.detailRows - detailHeaderRows
}

type scrollbarLayout struct {
	hasScrollbar bool
	thumbStart   int
	thumbSize    int
}

func calcScrollbar(total, offset, visible int) scrollbarLayout {
	if total <= visible || visible <= 0 {
		return scrollbarLayout{}
	}
	thumbSize := visible * visible / total
	if thumbSize < 1 {
		thumbSize = 1
	}
	thumbStart := offset * visible / total
	if thumbStart+thumbSize > visible {
		thumbStart = visible - thumbSize
	}
	if thumbStart < 0 {
		thumbStart = 0
	}
	return scrollbarLayout{
		hasScrollbar: true,
		thumbStart:   thumbStart,
		thumbSize:    thumbSize,
	}
}

// Layout constants shared by view and model (paging, offset clamping).
const (
	headerRows         = 1 // pill, url, connection, db, refresh
	tabRows            = 1 // status tabs and project selector
	colHeadRows        = 1 // table column header
	footerRows         = 1 // key legend and position
	gapRows            = 2 // blank line above the table and above the detail rule
	minDetail          = 8
	minTable           = 3
	detailHeaderRows   = 3
	detailIndentSpaces = " "
	detailIndent       = len(detailIndentSpaces)
	scrollbarWidth     = 1
)
