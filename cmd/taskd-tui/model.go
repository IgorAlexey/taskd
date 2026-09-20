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
		now:     time.Now(),
		detail:  vp,
	}
	vw := m.width - 2
	if vw < 1 {
		vw = 1
	}
	vh := m.detailRows() - 4
	if vh < 1 {
		vh = 1
	}
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

// startPoll starts a poll unless one is in flight.
func (m *model) startPoll() tea.Cmd {
	if m.polling {
		return nil
	}
	m.polling = true
	m.seq++
	return pollCmd(m.client, m.project, m.etag, m.seq)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vw := m.width - 2
		if vw < 1 {
			vw = 1
		}
		vh := m.detailRows() - 4
		if vh < 1 {
			vh = 1
		}
		m.detail.SetWidth(vw)
		m.detail.SetHeight(vh)
		m.clamp()
		if t, ok := m.selected(); ok {
			m.detail.SetContent(m.renderBody(t))
		}
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		poll := m.startPoll()
		return m, tea.Batch(tickCmd(m.cfg.refresh), poll)

	case pollMsg:
		if msg.seq != m.seq {
			// Answer to a superseded poll; the newest one is still on
			// the wire and owns the in-flight flag.
			return m, nil
		}
		m.polling = false
		if msg.changed {
			m.etag = msg.etag
			selID := ""
			if sel, ok := m.selected(); ok {
				selID = sel.ID
			}
			m.tasks = msg.tasks
			m.rebuild()
			if selID != "" {
				for i, idx := range m.shown {
					if m.tasks[idx].ID == selID {
						m.cursor = i
						break
					}
				}
			}
			m.clamp()
			m.syncDetail()
		}
		if msg.err != nil {
			m.connected = false
			m.lastErr = msg.err.Error()
			return m, nil
		}
		m.connected = true
		m.lastErr = ""
		m.stats = msg.stats
		m.hasStats = true
		if msg.projects != nil {
			m.projects = msg.projects
		}
		return m, nil

	case actMsg:
		var cmd tea.Cmd
		if msg.err != nil {
			cmd = m.setMsg("error: " + msg.err.Error())
		} else if msg.msg != "" {
			cmd = m.setMsg(msg.msg)
		}
		poll := m.startPoll()
		return m, tea.Batch(cmd, poll)

	case clearMsgMsg:
		if msg.id == m.msgID {
			m.msg = ""
		}
		return m, nil

	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.mode == modeDetail || m.mode == modeZoom {
				m.detail.ScrollUp(3)
			} else {
				m.cursor -= 3
				m.clamp()
				m.syncDetail()
			}
		case tea.MouseWheelDown:
			if m.mode == modeDetail || m.mode == modeZoom {
				m.detail.ScrollDown(3)
			} else {
				m.cursor += 3
				m.clamp()
				m.syncDetail()
			}
		}
		return m, nil

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && m.mode == modeTable {
			bandTop := headerRows + tabRows + 1 + colHeadRows
			tr := m.tableRows()
			if msg.Y >= bandTop && msg.Y < bandTop+tr {
				idx := m.offset + (msg.Y - bandTop)
				if idx >= 0 && idx < len(m.shown) {
					m.cursor = idx
					m.clamp()
					m.syncDetail()
				}
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		if msg.Mod&tea.ModCtrl != 0 && msg.Code == 'c' {
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
				if errText != "" {
					return m, cmd
				}
				m.mode = modeTable
				act := actCmd(m.client, method, path, body, success)
				if cmd != nil {
					return m, tea.Batch(cmd, act)
				}
				return m, act
			}
			return m, cmd

		case modeConfirm:
			switch {
			case msg.Text == "y" || msg.Text == "Y":
				m.mode = modeTable
				return m, actCmd(m.client, m.confirm.method, m.confirm.path, m.confirm.body, m.confirm.success)
			case msg.Code == tea.KeyEnter || msg.Code == tea.KeyEscape ||
				msg.Text == "n" || msg.Text == "N" || msg.Text == "q":
				// Bare Enter cancels: a destructive action needs an
				// explicit y.
				m.mode = modeTable
				return m, nil
			}
			return m, nil

		case modeHelp:
			m.mode = modeTable
			return m, nil

		case modeSearch:
			switch {
			case msg.Code == tea.KeyEscape:
				m.query = ""
				m.mode = modeTable
				m.rebuild()
			case msg.Code == tea.KeyEnter:
				m.mode = modeTable
			case msg.Mod&tea.ModCtrl != 0 && msg.Code == 'u':
				m.query = ""
				m.rebuild()
			case msg.Mod&tea.ModCtrl != 0 && msg.Code == 'w':
				m.query = deleteWord(m.query)
				m.rebuild()
			case msg.Code == tea.KeyBackspace:
				if len(m.query) > 0 {
					r := []rune(m.query)
					m.query = string(r[:len(r)-1])
					m.rebuild()
				}
			default:
				if msg.Text != "" && msg.Mod&^tea.ModShift == 0 {
					m.query += msg.Text
					m.rebuild()
				}
			}
			return m, nil

		case modeDetail, modeZoom:
			switch {
			case msg.Code == tea.KeyTab || msg.Code == tea.KeyEscape:
				m.mode = modeTable
				vh := m.detailRows() - 4
				if vh < 1 {
					vh = 1
				}
				m.detail.SetHeight(vh)
				return m, nil
			case msg.Text == "z":
				if m.mode == modeZoom {
					m.mode = modeTable
				} else {
					m.mode = modeZoom
				}
				vh := m.detailRows() - 4
				if vh < 1 {
					vh = 1
				}
				m.detail.SetHeight(vh)
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
				if len(m.shown) > 0 && m.cursor < len(m.shown)-1 {
					m.cursor++
					m.clamp()
					m.syncDetail()
				}
				return m, nil
			case msg.Text == "k" || msg.Code == tea.KeyUp:
				if m.cursor > 0 {
					m.cursor--
					m.clamp()
					m.syncDetail()
				}
				return m, nil
			case msg.Text == "g" || msg.Code == tea.KeyHome:
				m.cursor = 0
				m.clamp()
				m.syncDetail()
				return m, nil
			case msg.Text == "G" || msg.Code == tea.KeyEnd:
				if len(m.shown) > 0 {
					m.cursor = len(m.shown) - 1
					m.clamp()
					m.syncDetail()
				}
				return m, nil
			case (msg.Mod&tea.ModCtrl != 0 && msg.Code == 'd') || msg.Code == tea.KeyPgDown:
				step := m.tableRows() / 2
				if step < 1 {
					step = 1
				}
				m.cursor += step
				m.clamp()
				m.syncDetail()
				return m, nil
			case (msg.Mod&tea.ModCtrl != 0 && msg.Code == 'u') || msg.Code == tea.KeyPgUp:
				step := m.tableRows() / 2
				if step < 1 {
					step = 1
				}
				m.cursor -= step
				m.clamp()
				m.syncDetail()
				return m, nil
			case msg.Text == "0":
				m.filter = ""
				m.rebuild()
				return m, nil
			case msg.Text == "1":
				m.filter = "pending"
				m.rebuild()
				return m, nil
			case msg.Text == "2":
				m.filter = "leased"
				m.rebuild()
				return m, nil
			case msg.Text == "3":
				m.filter = "done"
				m.rebuild()
				return m, nil
			case msg.Text == "4":
				m.filter = "buried"
				m.rebuild()
				return m, nil
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
				// The tag and the counts describe the previous project's
				// list; a poll for the new one is a different query, not
				// duplicate work, so the in-flight guard yields to it.
				m.etag, m.stats, m.hasStats, m.polling = "", stats{}, false, false
				m.rebuild()
				poll := m.startPoll()
				return m, poll
			case msg.Text == "/":
				m.mode = modeSearch
				return m, nil
			case msg.Code == tea.KeyEscape:
				if m.query != "" {
					m.query = ""
					m.rebuild()
				}
				return m, nil
			case msg.Code == tea.KeyTab:
				m.mode = modeDetail
				return m, nil
			case msg.Text == "z":
				m.mode = modeZoom
				vh := m.detailRows() - 4
				if vh < 1 {
					vh = 1
				}
				m.detail.SetHeight(vh)
				return m, nil
			case msg.Text == "n":
				m.form = newCreateForm(m.project, m.width)
				m.mode = modeForm
				return m, nil
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
				m.form = newEditForm(t, m.width)
				m.mode = modeForm
				return m, nil
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
				return m, actCmd(m.client, "POST", "/tasks/"+t.ID+"/release", map[string]any{"worker": t.Worker}, "released task "+id7)
			case msg.Text == "D":
				t, ok := m.selected()
				if !ok {
					return m, nil
				}
				if t.Status == "leased" && t.LeaseExpires >= m.now.Unix() {
					cmd := m.setMsg("cannot delete actively leased task")
					return m, cmd
				}
				id7 := shortID(t.ID)
				_, title := titleOf(t)
				title40 := truncateRunes(title, 40)
				m.confirm = confirmModel{
					text:    fmt.Sprintf("Delete task %s %q?", id7, title40),
					button:  "delete",
					method:  "DELETE",
					path:    "/tasks/" + t.ID,
					success: "deleted task " + id7,
				}
				m.mode = modeConfirm
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
				id7 := shortID(t.ID)
				_, title := titleOf(t)
				title40 := truncateRunes(title, 40)
				m.confirm = confirmModel{
					text:    fmt.Sprintf("Complete task %s %q?", id7, title40),
					button:  "complete",
					method:  "POST",
					path:    "/tasks/" + t.ID + "/close",
					success: "completed task " + id7,
				}
				m.mode = modeConfirm
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
				m.mode = modeHelp
				return m, nil
			}
		}
	}
	return m, nil
}

func (m *model) rebuild() {
	shown := make([]int, 0, len(m.tasks))
	q := strings.ToLower(m.query)
	for i, t := range m.tasks {
		if m.project != "" && t.Project != m.project {
			continue
		}
		if m.filter != "" && t.Status != m.filter {
			continue
		}
		if q != "" {
			if !strings.Contains(strings.ToLower(t.Body), q) &&
				!strings.Contains(strings.ToLower(t.AssetPath), q) &&
				!strings.Contains(strings.ToLower(t.Worker), q) &&
				!strings.Contains(strings.ToLower(t.ID), q) {
				continue
			}
		}
		shown = append(shown, i)
	}
	m.shown = shown
	m.clamp()
	m.syncDetail()
}

func (m *model) clamp() {
	if len(m.shown) == 0 {
		m.cursor = 0
		m.offset = 0
		return
	}
	if m.cursor >= len(m.shown) {
		m.cursor = len(m.shown) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	tr := m.tableRows()
	if tr > 0 {
		if m.cursor < m.offset {
			m.offset = m.cursor
		}
		if m.cursor >= m.offset+tr {
			m.offset = m.cursor - tr + 1
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
		if m.cursor < m.offset {
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
