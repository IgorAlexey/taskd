package main

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
		pages:   1,
		now:     time.Now(),
		detail:  vp,
	}
	vw := m.width - 2
	if vw < 1 {
		vw = 1
	}
	vh := m.detailViewportHeight()
	m.detail.SetWidth(vw)
	m.detail.SetHeight(vh)
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
	return listFilter{project: m.project, status: m.filter, query: m.query}
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

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.mode == modeForm {
			m.form.fit(msg.Width, msg.Height, m.theme)
		}
		if m.mode == modeHelp {
			yOffset := m.help.vp.YOffset()
			m.help = newHelpModel(msg.Width, msg.Height, m.help.prev, m.theme)
			m.help.vp.SetYOffset(yOffset)
		}
		vw := m.width - 2
		if vw < 1 {
			vw = 1
		}
		vh := m.detailViewportHeight()
		m.detail.SetWidth(vw)
		m.detail.SetHeight(vh)
		m.clamp()
		if t, ok := m.selected(); ok {
			m.detail.SetContent(m.renderBody(t))
		}
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		now := m.now.Unix()
		for i := range m.tasks {
			m.tasks[i].normalize(now)
		}
		m.rebuild()
		// A walk the operator asked for is theirs to repeat: re-walking
		// it every tick would cost the daemon a page per second for as
		// long as they read the tail. The counters still refresh, so
		// only the task list is held still, and the footer says so.
		var poll tea.Cmd
		switch {
		case m.pages > 1:
			poll = statsCmd(m.client, m.project)
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
			now := m.now.Unix()
			for i := range m.tasks {
				m.tasks[i].normalize(now)
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
		if m.mode == modeHelp {
			var cmd tea.Cmd
			m.help, cmd = m.help.Update(msg)
			return m, cmd
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.mode == modeDetail || m.mode == modeZoom {
				m.detail.ScrollUp(3)
			} else {
				m.move(-3)
			}
		case tea.MouseWheelDown:
			if m.mode == modeDetail || m.mode == modeZoom {
				m.detail.ScrollDown(3)
			} else {
				m.move(3)
			}
		}
		return m, nil

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && (m.mode == modeTable || m.mode == modeDetail) {
			bandTop := headerRows + tabRows + 1 + colHeadRows
			tr, dr := m.layout()
			if msg.Y >= bandTop && msg.Y < bandTop+tr {
				idx := m.offset + (msg.Y - bandTop)
				if idx >= 0 && idx < len(m.shown) {
					m.cursor = idx
					m.lastRow = idx
					m.clamp()
					m.syncDetail()
				}
				if m.mode == modeDetail {
					m.mode = modeTable
				}
			} else {
				detailTop := bandTop + tr + 1
				if msg.Y >= detailTop && msg.Y < detailTop+dr {
					if m.mode == modeTable {
						m.mode = modeDetail
					}
				}
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		if msg.Mod&tea.ModCtrl != 0 && msg.Code == 'c' {
			if m.mode == modeForm {
				if m.form.dirty() && !m.form.discarding {
					m.form.discarding = true
					return m, nil
				}
				if !m.form.dirty() {
					m.mode = modeTable
					return m, nil
				}
			}
			return m, tea.Quit
		}
		switch m.mode {
		case modeForm:
			var cmd tea.Cmd
			m.form, cmd = m.form.Update(msg)
			if m.form.cancelled {
				m.mode = modeTable
				return m, cmd
			}
			if m.form.done {
				method, path, body, success, errText := m.form.submit()
				if errText == "" {
					m.mode = modeTable
					return m, tea.Batch(cmd, actCmd(m.client, method, path, body, success))
				}
			}
			return m, cmd

		case modeConfirm:
			switch {
			case msg.Text == "y" || msg.Text == "Y":
				m.mode = modeTable
				return m, actCmd(m.client, m.confirm.method, m.confirm.path, m.confirm.body, m.confirm.success)
			case msg.Code == tea.KeyEnter || msg.Code == tea.KeyEscape ||
				msg.Text == "n" || msg.Text == "N" || msg.Text == "q":
				m.mode = modeTable
				return m, nil
			}
			return m, nil

		case modeHelp:
			if msg.Code == tea.KeyEscape || msg.Text == "?" || msg.Text == "q" || msg.Code == tea.KeyEnter {
				m.mode = m.help.prev
				if m.mode != modeTable && m.mode != modeDetail && m.mode != modeZoom {
					m.mode = modeTable
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.help, cmd = m.help.Update(msg)
			return m, cmd

		case modeSearch:
			switch {
			case msg.Code == tea.KeyEscape:
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
				m.mode = modeTable
				return m, nil
			case msg.Text == "z":
				if m.mode == modeZoom {
					m.mode = modeTable
				} else {
					m.mode = modeZoom
				}
				m.clamp()
				m.detail.SetHeight(m.detailViewportHeight())
				return m, nil
			case msg.Text == "q":
				return m, tea.Quit
			case msg.Text == "y":
				if t, ok := m.selected(); ok {
					return m, copyToClipboard(t.ID)
				}
				return m, nil
			case msg.Text == "Y":
				if t, ok := m.selected(); ok {
					return m, copyToClipboard(t.Body)
				}
				return m, nil
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
				m.help = newHelpModel(m.width, m.height, m.mode, m.theme)
				m.mode = modeHelp
				return m, nil
			default:
				var cmd tea.Cmd
				m.detail, cmd = m.detail.Update(msg)
				return m, cmd
			}

		case modeTable:
			switch {
			case msg.Text == "q":
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
				if len(m.projects) == 0 {
					m.project = ""
				} else if m.project == "" {
					m.project = m.projects[0]
				} else {
					next := ""
					for i, p := range m.projects {
						if p == m.project {
							if i+1 < len(m.projects) {
								next = m.projects[i+1]
							} else {
								next = ""
							}
							break
						}
					}
					m.project = next
				}
				// The tag, the counts and the walk depth describe the
				// previous project's list; the new one starts live.
				m.stats, m.hasStats = stats{}, false
				return m, m.rescope()
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
				return m, nil
			case msg.Code == tea.KeyTab || msg.Code == tea.KeyEnter:
				m.mode = modeDetail
				return m, nil
			case msg.Text == "z":
				m.mode = modeZoom
				m.clamp()
				m.detail.SetHeight(m.detailViewportHeight())
				return m, nil
			case msg.Text == "n":
				var cmd tea.Cmd
				m.form, cmd = newCreateForm(m.project)
				m.form.fit(m.width, m.height, m.theme)
				m.mode = modeForm
				return m, cmd
			case msg.Text == "e":
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
				return m, cmd
			case msg.Text == "+" || msg.Text == "=" || msg.Code == '+' || msg.Code == '=':
				t, ok := m.selected()
				if !ok || t.Priority == 0 {
					return m, nil
				}
				pri := t.Priority - 1
				if pri < 1 {
					pri = 1
				}
				if pri != t.Priority {
					return m, actCmd(m.client, "PATCH", "/tasks/"+t.ID, map[string]any{"priority": pri}, fmt.Sprintf("priority set to %d", pri))
				}
				return m, nil
			case msg.Text == "-" || msg.Code == '-':
				t, ok := m.selected()
				if !ok {
					return m, nil
				}
				pri := t.Priority + 1
				if pri != t.Priority {
					return m, actCmd(m.client, "PATCH", "/tasks/"+t.ID, map[string]any{"priority": pri}, fmt.Sprintf("priority set to %d", pri))
				}
				return m, nil
			case msg.Text == "c":
				t, ok := m.selected()
				if !ok {
					return m, nil
				}
				if t.Status != "pending" {
					cmd := m.setMsg("task is not pending")
					return m, cmd
				}
				id7 := shortID(t.ID)
				m.msg = ""
				return m, actCmd(m.client, "POST", "/tasks/"+t.ID+"/claim", map[string]any{"worker": m.cfg.worker}, "claimed task "+id7)
			case msg.Text == "u":
				t, ok := m.selected()
				if !ok {
					return m, nil
				}
				if t.Status != "leased" {
					cmd := m.setMsg("task is not leased")
					return m, cmd
				}
				id7 := shortID(t.ID)
				m.msg = ""
				return m, actCmd(m.client, "POST", "/tasks/"+t.ID+"/release", map[string]any{"worker": t.Worker}, "released task "+id7)
			case msg.Text == "t":
				t, ok := m.selected()
				if !ok {
					return m, nil
				}
				if t.Status != "leased" {
					cmd := m.setMsg("task is not leased")
					return m, cmd
				}
				id7 := shortID(t.ID)
				m.msg = ""
				return m, actCmd(m.client, "POST", "/tasks/"+t.ID+"/touch", map[string]any{"worker": m.cfg.worker}, "touched task "+id7)
			case msg.Text == "b":
				t, ok := m.selected()
				if !ok {
					return m, nil
				}
				if t.Status != "leased" {
					cmd := m.setMsg("task is not leased")
					return m, cmd
				}
				if t.Worker != m.cfg.worker {
					cmd := m.setMsg("task leased by another worker")
					return m, cmd
				}
				m.confirmTask("Bury", "buried", "POST", "/tasks/"+t.ID+"/bury", t, map[string]any{"worker": m.cfg.worker})
				return m, nil
			case msg.Text == "K":
				t, ok := m.selected()
				if !ok {
					return m, nil
				}
				if t.Status != "buried" {
					cmd := m.setMsg("task is not buried")
					return m, cmd
				}
				m.confirmTask("Kick", "kicked", "POST", "/tasks/"+t.ID+"/kick", t, nil)
				return m, nil
			case msg.Text == "D":
				t, ok := m.selected()
				if !ok {
					return m, nil
				}
				if t.Status == "leased" && t.LeaseExpires >= m.now.Unix() {
					cmd := m.setMsg("cannot delete actively leased task")
					return m, cmd
				}
				m.confirmTask("Delete", "deleted", "DELETE", "/tasks/"+t.ID, t, nil)
				return m, nil
			case msg.Text == "x":
				t, ok := m.selected()
				if !ok {
					return m, nil
				}
				if t.Status == "done" {
					cmd := m.setMsg("task is already done")
					return m, cmd
				}
				if t.Status == "leased" && t.LeaseExpires >= m.now.Unix() {
					cmd := m.setMsg("cannot complete actively leased task")
					return m, cmd
				}
				m.confirmTask("Complete", "completed", "POST", "/tasks/"+t.ID+"/close", t, nil)
				return m, nil
			case msg.Text == "y":
				if t, ok := m.selected(); ok {
					return m, copyToClipboard(t.ID)
				}
				return m, nil
			case msg.Text == "Y":
				if t, ok := m.selected(); ok {
					return m, copyToClipboard(t.Body)
				}
				return m, nil
			case msg.Text == "r":
				poll := m.startPoll()
				return m, poll
			case msg.Text == "?":
				m.help = newHelpModel(m.width, m.height, m.mode, m.theme)
				m.mode = modeHelp
				return m, nil
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

func (m model) setFilter(f string) (model, tea.Cmd) {
	if m.filter == f {
		return m, nil
	}
	m.filter = f
	return m, m.rescope()
}

func (m *model) rebuildShown() {
	var leased, pending, buried, done []int
	for i, t := range m.tasks {
		if m.project != "" && t.Project != m.project {
			continue
		}
		if m.filter != "" && t.Status != m.filter {
			continue
		}
		switch t.Status {
		case "leased":
			leased = append(leased, i)
		case "pending":
			pending = append(pending, i)
		case "buried":
			buried = append(buried, i)
		case "done":
			done = append(done, i)
		default:
			pending = append(pending, i)
		}
	}
	shown := make([]int, 0, len(leased)+len(pending)+len(buried)+len(done))
	shown = append(shown, leased...)
	shown = append(shown, pending...)
	shown = append(shown, buried...)
	shown = append(shown, done...)
	m.shown = shown
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

func titleOf(t task) (scope, title string) {
	scope = t.Project
	first := t.Body
	if idx := strings.IndexByte(first, '\n'); idx >= 0 {
		first = first[:idx]
	}
	first = strings.TrimRight(first, "\r")
	if t.Project != "" && strings.HasPrefix(first, t.Project+":") {
		first = strings.TrimPrefix(first, t.Project+":")
	}
	title = strings.TrimSpace(first)
	return scope, title
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
	body := t.Body
	var rest string
	if idx := strings.IndexByte(body, '\n'); idx >= 0 {
		rest = strings.TrimLeft(body[idx+1:], "\n")
	}
	prim := strings.TrimSpace(string(t.Primitives))
	if prim != "" && prim != "null" {
		if rest != "" {
			rest += "\n\nresult: " + prim
		} else {
			rest = "result: " + prim
		}
	}
	rest = highlightCode(rest, m.theme)
	w := m.detail.Width()
	if w <= 0 {
		w = m.width - 2
	}
	if w <= 0 {
		w = 78
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
	vh := m.detailRows() - 3
	if vh < 1 {
		return 1
	}
	return vh
}
