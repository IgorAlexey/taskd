package main

import (
	"bytes"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"cmp"
	"encoding/json"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
)

func newModel(cfg config, c *client) model {
	glyph := nerdGlyphs
	if !cfg.icons {
		glyph = asciiGlyphs
	}
	vp := viewport.New()
	m := model{
		cfg:     cfg,
		client:  c,
		theme:   newTheme(true),
		glyph:   glyph,
		width:   80,
		height:  24,
		project: cfg.project,
		mode:    modeTable,
		sortCol: cfg.sortCol,
		pages:   1,
		now:     time.Now(),
		detail:  vp,
	}
	m.detail.SetWidth(m.detailViewportWidth())
	m.detail.SetHeight(m.detailViewportHeight())
	return m
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Init fires an immediate tick; the tick handler owns starting polls.
func (m model) Init() tea.Cmd {
	return func() tea.Msg { return tickMsg(time.Now()) }
}

func (m model) listFilter() listFilter {
	return listFilter{project: m.project, worker: m.worker, status: m.filter, query: m.query}
}

const searchDebounce = 150 * time.Millisecond

// typeQuery arms the debounce: the next keystroke would throw an answer
// away, so the term only goes down the wire once the typing pauses.
func (m *model) typeQuery() tea.Cmd {
	m.searchSeq++
	seq := m.searchSeq
	return tea.Tick(searchDebounce, func(time.Time) tea.Msg {
		return searchMsg{seq: seq}
	})
}

// commitQuery is the other half: Escape and Enter are decisions, not
// keystrokes, so they drop the pending tick and ask at once. A term the
// daemon has already answered is not asked again.
func (m *model) commitQuery() tea.Cmd {
	m.searchSeq++
	if m.listFilter() == m.asked {
		return nil
	}
	return m.rescope()
}

func (m model) listScope() listScope {
	return listScope{filter: m.listFilter(), pages: m.pages}
}

// startPoll asks the current question. It always asks: the generation makes
// the newest answer the only one that counts, so a keypress never has to
// pretend no poll is in flight to be heard.
func (m *model) startPoll() tea.Cmd {
	m.polling = true
	m.asked = m.listFilter()
	m.seq++
	return pollCmd(m.client, m.listScope(), m.etag, m.seq)
}

// rescope drops what described the old question and asks the new one: the
// tag and the walk depth belong to the list being replaced.
func (m *model) rescope() tea.Cmd {
	m.etag, m.pages = "", 1
	m.rebuild()
	return m.startPoll()
}

func (m model) isModal() bool {
	return m.mode == modeForm || m.mode == modeConfirm || m.mode == modeHelp
}

func (m model) updateModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeForm:
		switch msg := msg.(type) {
		case tea.KeyPressMsg:
			if isCtrlC(msg) {
				m.formSeq = 0
				if m.form.dirty() && !m.form.discarding {
					m.form.discarding = true
					return m, nil
				}
				if !m.form.dirty() {
					m.mode = modeTable
					return m, nil
				}
				return m, tea.Quit
			}
			if m.formSeq != 0 && msg.Code != tea.KeyEscape && !m.form.discarding {
				return m, nil
			}
			var cmd tea.Cmd
			m.form, cmd = m.form.Update(msg)
			return m.handleFormResult(cmd)
		case tea.MouseWheelMsg, tea.MouseClickMsg:
			if m.formSeq != 0 {
				return m, nil
			}
			var cmd tea.Cmd
			m.form, cmd = m.form.Update(msg)
			return m.handleFormResult(cmd)
		}
		return m, nil

	case modeConfirm:
		switch msg := msg.(type) {
		case tea.KeyPressMsg:
			switch {
			case msg.Text == "y" || msg.Text == "Y":
				m.mode = modeTable
				return m, actCmd(m.client, m.confirm.method, m.confirm.path, m.confirm.body, m.confirm.success)
			case msg.Code == tea.KeyEnter || msg.Code == tea.KeyEscape ||
				msg.Text == "n" || msg.Text == "N" || msg.Text == "q" || isCtrlC(msg):
				m.mode = modeTable
				return m, nil
			}
		case tea.MouseClickMsg:
			if msg.Button == tea.MouseLeft {
				return m.handleConfirmClick(msg.X, msg.Y)
			}
		}
		return m, nil

	case modeHelp:
		switch msg := msg.(type) {
		case tea.KeyPressMsg:
			if msg.Code == tea.KeyEscape || msg.Text == "?" || msg.Text == "q" || msg.Code == tea.KeyEnter || isCtrlC(msg) {
				return m.closeHelp(), nil
			}
			var cmd tea.Cmd
			m.help, cmd = m.help.Update(msg)
			return m, cmd
		case tea.MouseWheelMsg:
			var cmd tea.Cmd
			m.help, cmd = m.help.Update(msg)
			return m, cmd
		case tea.MouseClickMsg:
			if msg.Button == tea.MouseLeft {
				return m.closeHelp(), nil
			}
		}
		return m, nil
	}
	return m, nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.isModal() {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.MouseWheelMsg, tea.MouseClickMsg:
			return m.updateModal(msg)
		}
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateCols()
		if m.mode == modeForm {
			m.form.fit(msg.Width, msg.Height, m.theme)
		}
		if m.mode == modeHelp {
			yOffset := m.help.vp.YOffset()
			m.help = newHelpModel(msg.Width, msg.Height, m.help.prev, m.theme)
			m.help.vp.SetYOffset(yOffset)
		}
		m.detail.SetWidth(m.detailViewportWidth())
		m.detail.SetHeight(m.detailViewportHeight())
		m.clamp()
		if t, ok := m.selected(); ok {
			m.detail.SetContent(m.renderBody(t))
		}
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		m.rebuild()
		// A walk the operator asked for is theirs to repeat: re-walking
		// it every tick would cost the daemon a page per second for as
		// long as they read the tail. The counters still refresh, so
		// only the task list is held still, and the footer says so.
		var poll tea.Cmd
		switch {
		case m.pages > 1:
			poll = statsCmd(m.client, m.project, m.worker)
		case !m.polling:
			poll = m.startPoll()
		}
		return m, tea.Batch(tickCmd(m.cfg.refresh), poll)

	case pollMsg:
		if msg.seq != m.seq {
			// Answer to a superseded poll; the newest one is still on
			// the wire and owns the in-flight flag.
			return m, nil
		}
		m.polling = false
		if msg.scope.filter != m.listFilter() {
			// The newest poll, but for a question the operator has
			// since changed; its rows would answer nothing on screen.
			return m, nil
		}
		var cmd tea.Cmd
		if msg.changed {
			m.etag = msg.etag
			m.total, m.more = msg.total, msg.more
			selID := ""
			curRow := m.cursor
			if sel, ok := m.selected(); ok {
				selID = sel.ID
			}
			m.tasks = msg.tasks
			m.rebuildShown()
			if selID != "" {
				found := false
				for i, idx := range m.shown {
					if m.tasks[idx].ID == selID {
						m.cursor = i
						m.lastRow = i
						found = true
						break
					}
				}
				if !found {
					m.lastRow = curRow
					m.cursor = -1
					cmd = m.setMsg("selected task " + selID + " left the view")
				}
			}
			if m.endPages > 0 && msg.scope.pages >= m.endPages {
				m.endPages = 0
				if len(m.shown) > 0 {
					m.cursor = len(m.shown) - 1
					m.lastRow = m.cursor
				}
			}
			m.clamp()
			m.syncDetail()
		}
		if msg.err != nil {
			m.connected = false
			m.lastErr = msg.err.Error()
			return m, cmd
		}
		m.connected = true
		m.lastErr = ""
		m.stats = msg.stats
		m.hasStats = true
		if msg.projects != nil {
			m.projects = msg.projects
		}
		if msg.workers != nil {
			m.workers = msg.workers
		}
		return m, cmd

	case actMsg:
		var cmd tea.Cmd
		if msg.err != nil {
			cmd = m.setError(msg.err.Error())
		} else if msg.msg != "" {
			cmd = m.setMsg(msg.msg)
		}
		poll := m.startPoll()
		return m, tea.Batch(cmd, poll)

	case formActMsg:
		if msg.seq != m.formSeq || m.formSeq == 0 {
			return m, nil
		}
		m.formSeq = 0
		if m.mode == modeForm {
			if msg.err != nil {
				m.form.errText = msg.err.Error()
				m.form = m.form.refit()
				return m, nil
			}
			m.mode = modeTable
		}
		var cmd tea.Cmd
		if msg.err != nil {
			cmd = m.setError(msg.err.Error())
		} else if msg.msg != "" {
			cmd = m.setMsg(msg.msg)
		}
		poll := m.startPoll()
		return m, tea.Batch(cmd, poll)

	case statsMsg:
		if msg.err != nil {
			m.connected = false
			m.lastErr = msg.err.Error()
			return m, nil
		}
		m.connected = true
		m.lastErr = ""
		m.stats = msg.stats
		m.hasStats = true
		return m, nil

	case searchMsg:
		if msg.seq != m.searchSeq {
			return m, nil
		}
		return m, m.rescope()

	case clearMsgMsg:
		if msg.id == m.msgID {
			m.msg = ""
		}
		return m, nil

	case tea.MouseWheelMsg:
		panes := m.panes()
		targetDetail := false
		if m.mode == modeZoom || panes.inDetail(msg.Y) {
			targetDetail = true
		} else if panes.inTable(msg.Y) {
			targetDetail = false
		} else {
			targetDetail = (m.mode == modeDetail)
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			if targetDetail {
				m.detail.ScrollUp(3)
			} else {
				m.move(-3)
			}
		case tea.MouseWheelDown:
			if targetDetail {
				m.detail.ScrollDown(3)
			} else {
				m.move(3)
			}
		}
		return m, nil

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && (m.mode == modeTable || m.mode == modeDetail || m.mode == modeZoom) {
			if msg.Y == headerRows {
				bounds := m.row1Bounds()
				for _, tab := range bounds.tabs {
					if msg.X >= tab.start && msg.X < tab.end {
						m.mode = modeTable
						return m.setFilter(tab.filter)
					}
				}
				if msg.X >= bounds.proj[0] && msg.X < bounds.proj[1] {
					m.mode = modeTable
					return m.cycleProject(1)
				}
				if msg.X >= bounds.worker[0] && msg.X < bounds.worker[1] {
					m.mode = modeTable
					return m.cycleWorker(1)
				}
				return m, nil
			}
			if msg.Y == headerRows+tabRows+1 && (m.mode == modeTable || m.mode == modeDetail) {
				m.handleColHeadClick(msg.X)
				return m, nil
			}
			if m.height > 0 && msg.Y == m.height-1 {
				return m.handleFooterClick(msg.X)
			}

			panes := m.panes()
			if panes.inTable(msg.Y) {
				sb := calcScrollbar(len(m.shown), m.offset, panes.tableRows)
				if msg.X == m.width-1 && sb.hasScrollbar {
					clickRow := msg.Y - panes.tableTop
					step := panes.tableRows / 2
					if step < 1 {
						step = 1
					}
					if clickRow < sb.thumbStart {
						m.offset -= step
						if m.cursor >= 0 {
							m.cursor -= step
						}
					} else if clickRow >= sb.thumbStart+sb.thumbSize {
						m.offset += step
						if m.cursor >= 0 {
							m.cursor += step
						}
					}
					m.clamp()
					if m.cursor >= 0 {
						m.lastRow = m.cursor
					}
					m.syncDetail()
					if m.mode == modeDetail {
						m.mode = modeTable
					}
					return m, nil
				}

				idx := m.offset + (msg.Y - panes.tableTop)
				if idx >= 0 && idx < len(m.shown) {
					m.cursor = idx
					m.lastRow = idx
					m.clamp()
					m.syncDetail()
				}
				if m.mode == modeDetail {
					m.mode = modeTable
				}
			} else if panes.inDetail(msg.Y) {
				if m.mode == modeTable {
					m.mode = modeDetail
				}
				if msg.X == m.width-1 {
					vpMax := panes.detailViewportRows()
					clickRow := msg.Y - panes.detailViewportTop()
					if clickRow >= 0 && clickRow < vpMax {
						sb := calcScrollbar(m.detail.TotalLineCount(), m.detail.YOffset(), vpMax)
						if sb.hasScrollbar {
							step := vpMax / 2
							if step < 1 {
								step = 1
							}
							if clickRow < sb.thumbStart {
								m.detail.ScrollUp(step)
							} else if clickRow >= sb.thumbStart+sb.thumbSize {
								m.detail.ScrollDown(step)
							}
							return m, nil
						}
					}
				}
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		switch m.mode {
		case modeSearch:
			switch {
			case msg.Code == tea.KeyEscape || isCtrlC(msg):
				m.query = ""
				m.mode = modeTable
				return m, m.commitQuery()
			case msg.Code == tea.KeyEnter:
				m.mode = modeTable
				return m, m.commitQuery()
			case msg.Mod&tea.ModCtrl != 0 && msg.Code == 'u':
				m.query = ""
				return m, m.commitQuery()
			case msg.Mod&tea.ModCtrl != 0 && msg.Code == 'w':
				m.query = deleteWord(m.query)
				return m, m.typeQuery()
			case msg.Code == tea.KeyBackspace:
				if len(m.query) > 0 {
					r := []rune(m.query)
					m.query = string(r[:len(r)-1])
					return m, m.typeQuery()
				}
			case msg.Code == tea.KeyDown || (msg.Mod&tea.ModCtrl != 0 && msg.Code == 'n'):
				m.move(1)
				return m, nil
			case msg.Code == tea.KeyUp || (msg.Mod&tea.ModCtrl != 0 && msg.Code == 'p'):
				m.move(-1)
				return m, nil
			default:
				if msg.Text != "" && msg.Mod&^tea.ModShift == 0 {
					m.query += msg.Text
					return m, m.typeQuery()
				}
			}
			return m, nil

		case modeDetail, modeZoom:
			switch {
			case msg.Code == tea.KeyTab || msg.Code == tea.KeyEscape:
				return m.actionBack()
			case msg.Text == "z":
				return m.actionToggleZoom()
			case msg.Text == "q" || isCtrlC(msg):
				return m.actionQuit()
			case msg.Text == "j":
				m.detail.ScrollDown(1)
				return m, nil
			case msg.Text == "k":
				m.detail.ScrollUp(1)
				return m, nil
			case msg.Text == "g" || msg.Code == tea.KeyHome:
				m.detail.GotoTop()
				return m, nil
			case msg.Text == "G" || msg.Code == tea.KeyEnd:
				m.detail.GotoBottom()
				return m, nil
			case msg.Mod&tea.ModCtrl != 0 && msg.Code == 'd':
				m.detail.HalfPageDown()
				return m, nil
			case msg.Mod&tea.ModCtrl != 0 && msg.Code == 'u':
				m.detail.HalfPageUp()
				return m, nil
			case msg.Text == "?":
				return m.actionHelp()
			default:
				if m, cmd, ok := m.handleAction(msg); ok {
					return m, cmd
				}
				var cmd tea.Cmd
				m.detail, cmd = m.detail.Update(msg)
				return m, cmd
			}

		case modeTable:
			if m, cmd, ok := m.handleAction(msg); ok {
				return m, cmd
			}
			switch {
			case msg.Text == "q" || isCtrlC(msg):
				return m, tea.Quit
			case msg.Text == "j" || msg.Code == tea.KeyDown:
				m.move(1)
				return m, nil
			case msg.Text == "k" || msg.Code == tea.KeyUp:
				m.move(-1)
				return m, nil
			case msg.Text == "g" || msg.Code == tea.KeyHome:
				m.cursor = 0
				m.lastRow = 0
				m.clamp()
				m.syncDetail()
				// The jump to the head is the key the footer names for
				// leaving a paged snapshot; nothing else throws away
				// pages the operator paid for.
				if m.pages > 1 {
					return m, m.rescope()
				}
				return m, nil
			case msg.Text == "G" || msg.Code == tea.KeyEnd:
				if len(m.shown) > 0 {
					m.cursor = len(m.shown) - 1
					m.lastRow = m.cursor
					m.clamp()
					m.syncDetail()
				}
				// Each press buys another walk, up to a ceiling a single
				// answer can carry; the depth it asked for rides along,
				// and a walk already on the wire owns the next one.
				if depth := min(m.pages+tasksMaxPages, maxPageDepth); m.more && depth > m.pages && m.endPages == 0 {
					m.pages, m.endPages = depth, depth
					m.etag = ""
					return m, m.startPoll()
				}
				return m, nil
			case (msg.Mod&tea.ModCtrl != 0 && msg.Code == 'd') || msg.Code == tea.KeyPgDown:
				step := m.tableRows() / 2
				if step < 1 {
					step = 1
				}
				m.move(step)
				return m, nil
			case (msg.Mod&tea.ModCtrl != 0 && msg.Code == 'u') || msg.Code == tea.KeyPgUp:
				step := m.tableRows() / 2
				if step < 1 {
					step = 1
				}
				m.move(-step)
				return m, nil
			case msg.Text == "0":
				return m.setFilter("")
			case msg.Text == "1":
				return m.setFilter("pending")
			case msg.Text == "2":
				return m.setFilter("leased")
			case msg.Text == "3":
				return m.setFilter("done")
			case msg.Text == "4":
				return m.setFilter("buried")
			case msg.Text == "p":
				return m.cycleProject(1)
			case msg.Text == "P":
				return m.cycleProject(-1)
			case msg.Text == "w":
				return m.cycleWorker(1)
			case msg.Text == "W":
				return m.cycleWorker(-1)
			case msg.Text == "/":
				m.mode = modeSearch
				return m, nil
			case msg.Code == tea.KeyEscape:
				if m.query != "" {
					m.query = ""
					return m, m.commitQuery()
				}
				if m.msg != "" {
					m.msg = ""
					return m, nil
				}
				if m.filter != "" {
					return m.setFilter("")
				}
				if m.project != "" {
					return m.setProject("")
				}
				if m.worker != "" {
					return m.setWorker("")
				}
				return m, nil
			case msg.Code == tea.KeyTab || msg.Code == tea.KeyEnter:
				m.mode = modeDetail
				return m, nil
			case msg.Text == "z":
				return m.actionToggleZoom()
			case msg.Text == "r":
				poll := m.startPoll()
				return m, poll
			case msg.Text == "?":
				return m.actionHelp()
			}
		}
	default:
		// Cursor blink and other widget messages reach the form only
		// while it is open.
		if m.mode == modeForm {
			var cmd tea.Cmd
			m.form, cmd = m.form.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m model) handleAction(msg tea.KeyPressMsg) (model, tea.Cmd, bool) {
	key := msg.Text
	if key == "" {
		switch msg.Code {
		case '+':
			key = "+"
		case '-':
			key = "-"
		case '=':
			key = "="
		}
	}

	switch key {
	case "s":
		m.sortCol = (m.sortCol + 1) % sortColCount
		m.rebuild()
		return m, nil, true
	case "n":
		m, cmd := m.actionCreate()
		return m, cmd, true
	case "e":
		m, cmd := m.actionEdit()
		return m, cmd, true
	case "+", "=":
		m, cmd := m.actionPriRaise()
		return m, cmd, true
	case "-":
		m, cmd := m.actionPriLower()
		return m, cmd, true
	case "D":
		m, cmd := m.actionDelete()
		return m, cmd, true
	case "x":
		m, cmd := m.actionComplete()
		return m, cmd, true
	case "y":
		m, cmd := m.actionCopyID()
		return m, cmd, true
	case "Y":
		m, cmd := m.actionCopyBody()
		return m, cmd, true
	case "c", "u", "t", "b", "K":
		t, ok := m.selected()
		if !ok {
			return m, nil, true
		}
		switch key {

		case "c":
			if cmd, ok := m.requireWorker(); !ok {
				return m, cmd, true
			}
			if t.Status != "pending" {
				cmd := m.setMsg("task is not pending")
				return m, cmd, true
			}
			id7 := shortID(t.ID)
			m.msg = ""
			return m, actCmd(m.client, "POST", "/tasks/"+t.ID+"/claim", map[string]any{"worker": m.cfg.worker}, "claimed task "+id7), true
		case "u":
			if t.Status != "leased" {
				cmd := m.setMsg("task is not leased")
				return m, cmd, true
			}
			if t.Worker != m.cfg.worker {
				cmd := m.setMsg("cannot release lease held by another worker")
				return m, cmd, true
			}
			id7 := shortID(t.ID)
			m.msg = ""
			return m, actCmd(m.client, "POST", "/tasks/"+t.ID+"/release", map[string]any{"worker": m.cfg.worker}, "released task "+id7), true
		case "t":
			if cmd, ok := m.requireWorker(); !ok {
				return m, cmd, true
			}
			if t.Status != "leased" {
				cmd := m.setMsg("task is not leased")
				return m, cmd, true
			}
			if t.Worker != m.cfg.worker {
				cmd := m.setMsg("cannot touch lease held by another worker")
				return m, cmd, true
			}
			now := m.now
			if now.IsZero() {
				now = time.Now()
			}
			if t.LeaseExpires <= now.Unix() {
				cmd := m.setMsg("lease has expired")
				return m, cmd, true
			}
			id7 := shortID(t.ID)
			m.msg = ""
			return m, actCmd(m.client, "POST", "/tasks/"+t.ID+"/touch", map[string]any{"worker": m.cfg.worker}, "touched task "+id7), true
		case "b":
			if t.Status != "leased" {
				cmd := m.setMsg("task is not leased")
				return m, cmd, true
			}
			if t.Worker != m.cfg.worker {
				cmd := m.setMsg("task leased by another worker")
				return m, cmd, true
			}
			m.confirmTask("Bury", "buried", "POST", "/tasks/"+t.ID+"/bury", t, map[string]any{"worker": m.cfg.worker})
			return m, nil, true
		case "K":
			if t.Status != "buried" {
				cmd := m.setMsg("task is not buried")
				return m, cmd, true
			}
			m.confirmTask("Kick", "kicked", "POST", "/tasks/"+t.ID+"/kick", t, nil)
			return m, nil, true

		}
	}
	return m, nil, false
}

func (m model) setFilter(f string) (model, tea.Cmd) {
	if m.filter == f {
		return m, nil
	}
	m.filter = f
	return m, m.rescope()
}

func (m model) setProject(p string) (model, tea.Cmd) {
	if m.project == p {
		return m, nil
	}
	m.project = p
	m.stats, m.hasStats = stats{}, false
	return m, m.rescope()
}

func (m model) setWorker(w string) (model, tea.Cmd) {
	if m.worker == w {
		return m, nil
	}
	m.worker = w
	return m, m.rescope()
}

func (m model) cycleProject(delta int) (model, tea.Cmd) {
	return m.setProject(cycleScope(m.project, m.projects, delta))
}

func (m model) cycleWorker(delta int) (model, tea.Cmd) {
	return m.setWorker(cycleScope(m.worker, m.workers, delta))
}

func (m model) panes() paneLayout {
	tr, dr := m.layout()
	tableTop := headerRows + tabRows + 1 + colHeadRows
	detailTop := tableTop + tr + 1
	if m.mode == modeZoom {
		detailTop = headerRows + tabRows + 1
	}
	return paneLayout{
		tableTop:   tableTop,
		tableRows:  tr,
		detailTop:  detailTop,
		detailRows: dr,
	}
}

func (m model) row1Bounds() row1Bounds {
	tabDefs := m.tabDefs()
	var b row1Bounds
	x := 0
	for _, tab := range tabDefs {
		w := ansi.StringWidth(m.renderTab(tab))
		b.tabs = append(b.tabs, tabHitTarget{
			filter: tab.filter,
			start:  x,
			end:    x + w,
		})
		x += w + 2
	}
	tlw := max(0, x-2)

	pw := ansi.StringWidth(m.renderProject())
	ww := ansi.StringWidth(m.renderWorker())
	trw := pw + 2 + ww

	w := m.width
	if w <= 0 {
		w = 80
	}
	rightStart := w - trw
	if tlw+trw+1 > w {
		rightStart = tlw + 1
	}

	b.proj = [2]int{min(w, rightStart), min(w, rightStart+pw)}
	b.worker = [2]int{min(w, rightStart+pw+2), min(w, rightStart+trw)}
	return b
}

func (m *model) rebuildShown() {
	var indices []int
	for i, t := range m.tasks {
		if m.project != "" && t.Project != m.project {
			continue
		}
		if m.worker != "" && t.Worker != m.worker {
			continue
		}
		if m.filter != "" && t.Status != m.filter {
			continue
		}
		indices = append(indices, i)
	}

	slices.SortStableFunc(indices, func(a, b int) int {
		ti, tj := m.tasks[a], m.tasks[b]
		switch m.sortCol {
		case sortStatus:
			si := statusRank(ti.Status)
			sj := statusRank(tj.Status)
			if si != sj {
				return cmp.Compare(si, sj)
			}
			return cmp.Compare(ti.Priority, tj.Priority)
		case sortProject:
			if c := compareFold(ti.Project, tj.Project); c != 0 {
				return c
			}
			return cmp.Compare(ti.Priority, tj.Priority)
		case sortWorker:
			if (ti.Worker != "") != (tj.Worker != "") {
				if ti.Worker != "" {
					return -1
				}
				return 1
			}
			if c := compareFold(ti.Worker, tj.Worker); c != 0 {
				return c
			}
			return cmp.Compare(ti.Priority, tj.Priority)
		case sortLease:
			if (ti.LeaseExpires > 0) != (tj.LeaseExpires > 0) {
				if ti.LeaseExpires > 0 {
					return -1
				}
				return 1
			}
			if ti.LeaseExpires != tj.LeaseExpires {
				return cmp.Compare(ti.LeaseExpires, tj.LeaseExpires)
			}
			return cmp.Compare(ti.Priority, tj.Priority)
		default:
			if ti.Priority != tj.Priority {
				return cmp.Compare(ti.Priority, tj.Priority)
			}
			return cmp.Compare(statusRank(ti.Status), statusRank(tj.Status))
		}
	})

	m.shown = indices
	m.updateCols()
}

func compareFold(a, b string) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			if ca < cb {
				return -1
			}
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	} else if len(a) > len(b) {
		return 1
	}
	return 0
}

func statusRank(s string) int {
	switch s {
	case "leased":
		return 0
	case "pending":
		return 1
	case "buried":
		return 2
	case "done":
		return 3
	default:
		return 4
	}
}

func (m *model) updateCols() {
	maxScope, maxWorker, maxClaims := 0, 0, 0
	for _, idx := range m.shown {
		if idx >= 0 && idx < len(m.tasks) {
			t := m.tasks[idx]
			sc, _ := m.displayScope(t)
			if sc != "" {
				if sw := ansi.StringWidth(sc); sw > maxScope {
					maxScope = sw
				}
			}
			_, co := workerParts(t.Worker)
			if co != "" {
				if ww := ansi.StringWidth(m.glyph.branch + path.Base(co)); ww > maxWorker {
					maxWorker = ww
				}
			}
			if t.ClaimCount > 1 {
				if cw := ansi.StringWidth(m.glyph.refresh + " " + strconv.Itoa(t.ClaimCount)); cw > maxClaims {
					maxClaims = cw
				}
			}
		}
	}
	tRows, _ := m.layout()
	sb := calcScrollbar(len(m.shown), m.offset, tRows)
	m.cols = budgetColumns(m.width, maxScope, maxWorker, maxClaims, sb.hasScrollbar)
}

func (m *model) handleColHeadClick(x int) {
	if x < 2 {
		m.sortCol = sortStatus
		m.rebuild()
		return
	}
	if x < 4 {
		m.sortCol = sortPriority
		m.rebuild()
		return
	}

	cols := m.cols
	currX := 4
	if cols.scope > 0 {
		if x >= currX && x < currX+cols.scope {
			m.sortCol = sortProject
			m.rebuild()
			return
		}
		currX += cols.scope + 1
	}

	currX += cols.title
	if cols.claims > 0 {
		currX += 1 + cols.claims
	}

	if cols.worker > 0 {
		currX += 1
		if x >= currX && x < currX+cols.worker {
			m.sortCol = sortWorker
			m.rebuild()
			return
		}
		currX += cols.worker
	}

	if cols.lease > 0 {
		currX += 1
		leaseWidth := cols.lease
		if cols.left > 0 {
			leaseWidth += 1 + cols.left
		}
		if x >= currX && x < currX+leaseWidth {
			m.sortCol = sortLease
			m.rebuild()
			return
		}
	}
}

func (m *model) rebuild() {
	selID := ""
	curRow := m.cursor
	if sel, ok := m.selected(); ok {
		selID = sel.ID
	}
	m.rebuildShown()
	if selID != "" {
		found := false
		for i, idx := range m.shown {
			if m.tasks[idx].ID == selID {
				m.cursor = i
				m.lastRow = i
				found = true
				break
			}
		}
		if !found {
			m.lastRow = curRow
			m.cursor = -1
		}
	}
	m.clamp()
	m.syncDetail()
}

func (m *model) move(delta int) {
	if len(m.shown) == 0 {
		return
	}
	if m.cursor == -1 {
		if m.lastRow < len(m.shown) {
			if delta > 0 {
				m.cursor = m.lastRow + delta - 1
			} else {
				m.cursor = m.lastRow + delta
			}
		} else {
			lastIdx := len(m.shown) - 1
			if delta > 0 {
				m.cursor = lastIdx + delta - 1
			} else {
				m.cursor = lastIdx + delta + 1
			}
		}
	} else {
		m.cursor += delta
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.shown) {
		m.cursor = len(m.shown) - 1
	}
	m.lastRow = m.cursor
	m.clamp()
	m.syncDetail()
}

func (m *model) clamp() {
	if len(m.shown) == 0 {
		if m.cursor > 0 {
			m.cursor = 0
		}
		m.offset = 0
		return
	}
	if m.cursor >= len(m.shown) {
		m.cursor = len(m.shown) - 1
	}
	if m.cursor < -1 {
		m.cursor = 0
	}
	tr := m.tableRows()
	if tr > 0 {
		if m.cursor >= 0 {
			if m.cursor < m.offset {
				m.offset = m.cursor
			}
			if m.cursor >= m.offset+tr {
				m.offset = m.cursor - tr + 1
			}
		}
		maxOffset := len(m.shown) - tr
		if maxOffset < 0 {
			maxOffset = 0
		}
		if m.offset > maxOffset {
			m.offset = maxOffset
		}
		if m.offset < 0 {
			m.offset = 0
		}
		if m.cursor >= 0 && m.cursor < m.offset {
			m.offset = m.cursor
		}
	} else {
		m.offset = 0
	}
}

func (m model) selected() (task, bool) {
	if m.cursor < 0 || m.cursor >= len(m.shown) {
		return task{}, false
	}
	idx := m.shown[m.cursor]
	if idx < 0 || idx >= len(m.tasks) {
		return task{}, false
	}
	return m.tasks[idx], true
}

func (m *model) setMsg(s string) tea.Cmd {
	m.msg = s
	m.msgID++
	id := m.msgID
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return clearMsgMsg{id: id}
	})
}

func (m *model) requireWorker() (tea.Cmd, bool) {
	if m.cfg.worker == "" {
		return m.setMsg("worker required; set via -worker flag or TASKD_WORKER"), false
	}
	return nil, true
}

func (m *model) setError(s string) tea.Cmd {
	m.msg = "error: " + s
	m.msgID++
	return nil
}

func (m *model) confirmTask(action, past, method, path string, t task, body any) {
	id7 := shortID(t.ID)
	_, title := titleOf(t)
	title40 := truncateRunes(title, 40)
	m.confirm = confirmModel{
		text:    fmt.Sprintf("%s task %s %q?", action, id7, title40),
		button:  strings.ToLower(action),
		method:  method,
		path:    path,
		body:    body,
		success: fmt.Sprintf("%s task %s", past, id7),
	}
	m.mode = modeConfirm
}

func isScope(candidate, project string) bool {
	if candidate == "" || project == "" {
		return false
	}
	if strings.EqualFold(candidate, project) {
		return true
	}
	p := strings.ToLower(project)
	c := strings.ToLower(candidate)
	return strings.HasPrefix(c, p+"-") || strings.HasPrefix(c, p+"/")
}

func titleOf(t task) (scope, title string) {
	first := t.Body
	if idx := strings.IndexByte(first, '\n'); idx >= 0 {
		first = first[:idx]
	}
	first = strings.TrimRight(first, "\r")
	if strings.HasPrefix(first, "[") {
		if idx := strings.IndexByte(first, ']'); idx > 1 {
			candidate := first[1:idx]
			if !strings.ContainsAny(candidate, " \t") {
				return candidate, strings.TrimSpace(first[idx+1:])
			}
		}
	}
	if idx := strings.Index(first, ": "); idx > 0 {
		candidate := first[:idx]
		if isScope(candidate, t.Project) {
			return candidate, strings.TrimSpace(first[idx+2:])
		}
	}
	return "", strings.TrimSpace(first)
}

func workerParts(w string) (host, checkout string) {
	if idx := strings.IndexByte(w, ':'); idx >= 0 {
		return w[:idx], w[idx+1:]
	}
	return "", w
}

func (m *model) syncDetail() {
	m.detail.SetHeight(m.detailViewportHeight())
	t, ok := m.selected()
	if !ok {
		m.detailID = ""
		m.detail.SetContent("")
		return
	}
	newContent := m.renderBody(t)
	if t.ID != m.detailID || newContent != m.detail.GetContent() {
		m.detailID = t.ID
		m.detail.SetContent(newContent)
		m.detail.GotoTop()
	}
}

func (m model) renderBody(t task) string {
	rest := strings.TrimSpace(t.Body)
	prim := strings.TrimSpace(string(t.Primitives))
	if prim != "" && prim != "null" {
		var buf bytes.Buffer
		if err := json.Indent(&buf, t.Primitives, "", "  "); err == nil {
			prim = buf.String()
		}
		if rest != "" {
			rest += "\n\nresult: " + prim
		} else {
			rest = "result: " + prim
		}
	}
	rest = highlightCode(rest, m.theme)
	w := m.detail.Width()
	if w <= 0 {
		w = m.detailViewportWidth()
	}
	return lipgloss.NewStyle().Width(w).Render(rest)
}

func shortID(id string) string {
	if len(id) > 7 {
		return id[:7]
	}
	return id
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func deleteWord(s string) string {
	s = strings.TrimRight(s, " ")
	if idx := strings.LastIndexByte(s, ' '); idx >= 0 {
		return s[:idx+1]
	}
	return ""
}
func (m model) detailViewportHeight() int {
	vh := m.panes().detailViewportRows()
	if vh < 1 {
		return 1
	}
	return vh
}
func (m model) detailViewportWidth() int {
	w := m.width
	if w <= 0 {
		w = 80
	}
	vw := w - detailIndent - scrollbarWidth
	if vw < 1 {
		return 1
	}
	return vw
}
func cycleScope(current string, items []string, delta int) string {
	if len(items) == 0 {
		return ""
	}
	idx := slices.Index(items, current) + 1
	n := len(items) + 1
	idx = (idx + delta) % n
	if idx < 0 {
		idx += n
	}
	if idx == 0 {
		return ""
	}
	return items[idx-1]
}
func (m model) handleFooterClick(x int) (tea.Model, tea.Cmd) {
	frw := lipgloss.Width(m.footRight())
	footLeft, targets := m.footLeft(frw)
	w := m.width
	footLeftW := ansi.StringWidth(footLeft)
	maxLeft := w
	if footLeftW+frw+1 > w {
		maxLeft = max(0, w-frw-1)
	}
	if x >= maxLeft {
		return m, nil
	}
	for _, target := range targets {
		if x >= target.start && x < target.end {
			switch target.action {
			case "clear_search":
				m.query = ""
				return m, m.commitQuery()
			case "create":
				return m.actionCreate()
			case "edit":
				return m.actionEdit()
			case "pri_raise":
				return m.actionPriRaise()
			case "pri_lower":
				return m.actionPriLower()
			case "delete":
				return m.actionDelete()
			case "complete":
				return m.actionComplete()
			case "copy_id":
				return m.actionCopyID()
			case "copy_body":
				return m.actionCopyBody()
			case "zoom":
				return m.actionToggleZoom()
			case "quit":
				return m.actionQuit()
			case "help":
				return m.actionHelp()
			case "back":
				return m.actionBack()
			case "project":
				return m.cycleProject(1)
			case "worker":
				return m.cycleWorker(1)
			case "sort":
				m.sortCol = (m.sortCol + 1) % sortColCount
				m.rebuild()
				return m, nil
			}
		}
	}
	return m, nil
}

func (m model) actionCreate() (model, tea.Cmd) {
	var cmd tea.Cmd
	m.form, cmd = newCreateForm(m.project)
	m.form.fit(m.width, m.height, m.theme)
	m.mode = modeForm
	m.formSeq = 0
	return m, cmd
}

func (m model) actionEdit() (model, tea.Cmd) {
	t, ok := m.selected()
	if !ok {
		return m, nil
	}
	if t.Status == "done" {
		cmd := m.setMsg("cannot edit done task")
		return m, cmd
	}
	if t.Status == "leased" && t.LeaseExpires >= m.now.Unix() {
		cmd := m.setMsg("cannot edit actively leased task")
		return m, cmd
	}
	var cmd tea.Cmd
	m.form, cmd = newEditForm(t)
	m.form.fit(m.width, m.height, m.theme)
	m.mode = modeForm
	m.formSeq = 0
	return m, cmd
}

func (m model) actionPriAdjust(delta int) (model, tea.Cmd) {
	t, ok := m.selected()
	if !ok {
		return m, nil
	}
	if t.Status == "done" {
		cmd := m.setMsg("cannot adjust priority on done task")
		return m, cmd
	}
	if t.Status == "leased" && t.LeaseExpires >= m.now.Unix() {
		cmd := m.setMsg("cannot adjust priority on actively leased task")
		return m, cmd
	}
	if delta < 0 && t.Priority <= 0 {
		cmd := m.setMsg("already at highest priority")
		return m, cmd
	}
	pri := t.Priority + delta
	if pri < 0 {
		pri = 0
	}
	return m, actCmd(m.client, "PATCH", "/tasks/"+t.ID, map[string]any{"priority": pri}, fmt.Sprintf("priority set to %d", pri))
}

func (m model) actionPriRaise() (model, tea.Cmd) {
	return m.actionPriAdjust(-1)
}

func (m model) actionPriLower() (model, tea.Cmd) {
	return m.actionPriAdjust(1)
}

func (m model) actionDelete() (model, tea.Cmd) {
	t, ok := m.selected()
	if !ok {
		return m, nil
	}
	if t.Status == "leased" && t.LeaseExpires >= m.now.Unix() {
		cmd := m.setMsg("cannot delete actively leased task")
		return m, cmd
	}
	path := "/tasks/" + t.ID
	if t.Status == "done" {
		path += "?force=1"
	}
	m.confirmTask("Delete", "deleted", "DELETE", path, t, nil)
	return m, nil
}

func (m model) actionComplete() (model, tea.Cmd) {
	t, ok := m.selected()
	if !ok {
		return m, nil
	}
	if t.Status == "done" {
		cmd := m.setMsg("task is already done")
		return m, cmd
	}
	if t.Status == "leased" && t.LeaseExpires >= m.now.Unix() {
		if m.cfg.worker == "" {
			cmd := m.setMsg("worker not configured")
			return m, cmd
		}
		if t.Worker != m.cfg.worker {
			cmd := m.setMsg("task leased by another worker")
			return m, cmd
		}
		m.confirmTask("Complete", "completed", "POST", "/tasks/"+t.ID+"/done", t, map[string]any{"worker": m.cfg.worker})
		return m, nil
	}
	m.confirmTask("Complete", "completed", "POST", "/tasks/"+t.ID+"/close", t, nil)
	return m, nil
}

func (m model) actionCopyID() (model, tea.Cmd) {
	if t, ok := m.selected(); ok {
		return m, copyToClipboard(t.ID)
	}
	return m, nil
}

func (m model) actionCopyBody() (model, tea.Cmd) {
	if t, ok := m.selected(); ok {
		return m, copyToClipboard(t.Body)
	}
	return m, nil
}

func (m model) actionToggleZoom() (model, tea.Cmd) {
	if m.mode == modeZoom {
		m.mode = modeTable
	} else {
		m.mode = modeZoom
	}
	m.clamp()
	m.detail.SetHeight(m.detailViewportHeight())
	return m, nil
}

func (m model) actionHelp() (model, tea.Cmd) {
	m.help = newHelpModel(m.width, m.height, m.mode, m.theme)
	m.mode = modeHelp
	return m, nil
}

func (m model) closeHelp() model {
	m.mode = m.help.prev
	if m.mode != modeTable && m.mode != modeDetail && m.mode != modeZoom {
		m.mode = modeTable
	}
	return m
}
func (m model) actionQuit() (model, tea.Cmd) {
	return m, tea.Quit
}

func (m model) actionBack() (model, tea.Cmd) {
	m.mode = modeTable
	return m, nil
}

func isCtrlC(msg tea.KeyPressMsg) bool {
	return msg.Mod&tea.ModCtrl != 0 && msg.Code == 'c'
}

func (m model) handleFormResult(cmd tea.Cmd) (model, tea.Cmd) {
	if m.form.cancelled {
		m.mode = modeTable
		m.formSeq = 0
		return m, cmd
	}
	if m.form.done {
		if m.formSeq != 0 {
			return m, cmd
		}
		method, path, body, success, errText := m.form.submit()
		if errText != "" {
			m.form.errText = errText
			m.form = m.form.refit()
			return m, cmd
		}
		if m.form.editing && len(body) == 0 {
			m.mode = modeTable
			return m, cmd
		}
		m.formSeq++
		fcmd := formActCmd(m.client, m.formSeq, method, path, body, success)
		if cmd != nil {
			return m, tea.Batch(cmd, fcmd)
		}
		return m, fcmd
	}
	return m, cmd
}

func (m model) handleConfirmClick(x, y int) (tea.Model, tea.Cmd) {
	for _, target := range m.confirmTargets() {
		if y == target.y && x >= target.start && x < target.end {
			switch target.action {
			case confirmActionYes:
				m.mode = modeTable
				return m, actCmd(m.client, m.confirm.method, m.confirm.path, m.confirm.body, m.confirm.success)
			case confirmActionNo:
				m.mode = modeTable
				return m, nil
			}
		}
	}
	return m, nil
}
