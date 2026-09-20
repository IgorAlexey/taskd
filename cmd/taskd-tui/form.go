package main

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type formModel struct {
	title     string
	project   textinput.Model
	priority  textinput.Model
	asset     textinput.Model
	body      textarea.Model
	focus     int // 0 project, 1 priority, 2 asset, 3 body, 4 save button
	editing   bool
	id        string
	errText   string
	done      bool
	cancelled bool

	origProject     string
	origPriority    int
	origHasPriority bool
	origAsset       string
	origBody        string
	level           int // compaction fit chose; see lines
	width, height   int // terminal fit laid out for; zero before the first fit
	th              theme
}

func newCreateForm(project string) (formModel, tea.Cmd) {
	f := formModel{
		title:   "New Task",
		editing: false,
	}

	f.project = textinput.New()
	f.project.Prompt = ""
	f.project.SetValue(project)

	f.priority = textinput.New()
	f.priority.Prompt = ""
	f.priority.Placeholder = "3 (1 is top, blank for default)"

	f.asset = textinput.New()
	f.asset.Prompt = ""
	f.asset.Placeholder = "optional"

	f.body = textarea.New()
	f.body.Prompt = ""
	f.body.Placeholder = "first line is the title"
	f.body.ShowLineNumbers = false

	if project == "" {
		return f, f.setFocus(0)
	}
	return f, f.setFocus(3)
}

func newEditForm(t task) (formModel, tea.Cmd) {
	f := formModel{
		title:           "Edit Task",
		editing:         true,
		id:              t.ID,
		origProject:     t.Project,
		origPriority:    t.Priority,
		origHasPriority: true,
		origAsset:       t.AssetPath,
		origBody:        t.Body,
	}

	f.project = textinput.New()
	f.project.Prompt = ""
	f.project.SetValue(t.Project)

	f.priority = textinput.New()
	f.priority.Prompt = ""
	f.priority.Placeholder = "3 (1 is top, blank for default)"
	f.priority.SetValue(strconv.Itoa(t.Priority))

	f.asset = textinput.New()
	f.asset.Prompt = ""
	f.asset.Placeholder = "optional"
	f.asset.SetValue(t.AssetPath)

	f.body = textarea.New()
	f.body.Prompt = ""
	f.body.Placeholder = "first line is the title"
	f.body.ShowLineNumbers = false
	f.body.SetValue(t.Body)

	cmd := f.setFocus(3)
	return f, cmd
}

// boxSize is an overlay's outer width for a terminal, between lo and hi
// with a two-column margin, and the text width inside its border and
// padding. Under five columns there is no text width; box draws nothing.
func boxSize(width, lo, hi int) (outer, inner int) {
	outer = min(max(lo, min(width-4, hi)), width)
	return outer, outer - 4
}

func (f *formModel) resize(inner int) {
	f.project.SetWidth(max(1, inner-11))
	f.priority.SetWidth(max(1, inner-11))
	f.asset.SetWidth(max(1, inner-11))
	f.body.SetWidth(inner)
}

// inRing reports whether a field (0 project, 1 priority, 2 asset,
// 3 body) can take focus at a compaction level. From level 2 the asset
// field leaves the ring, and is not drawn, unless it holds a value or
// the cursor: a value the user cannot see is never sent. The button (4)
// is always in the ring.
func (f formModel) inRing(level, field int) bool {
	return field != 2 || level < 2 || f.asset.Value() != "" || f.focus == 2
}

// drawn reports whether a field has a row at a level: 0 and 1 draw
// every field, 2 drops the asset field, 3 draws only the focused one
// (the body when the button has focus). Level 3 never hides the focused
// field, so it cannot veto a focus move; that is inRing's job.
func (f formModel) drawn(level, field int) bool {
	if !f.inRing(level, field) {
		return false
	}
	return level < 3 || f.focus == field || (field == 3 && f.focus == 4)
}

// lines is the form's rows at a given level of compaction: 0 keeps the
// separators and hint, 1 drops them, 2 also drops the asset field and
// body label, 3 shows only the focused field, the button and the error.
// slot is the index the textarea goes at (-1 when it is not drawn);
// rows[keepAt:] up to keepN are the button and the error, which a short
// frame must never cut; anything after them is the hint.
func (f formModel) lines(level int, th theme) (rows []string, slot, keepAt, keepN int) {
	if level < 3 {
		rows = append(rows, th.accent.Render(f.title))
	}
	if level == 0 {
		rows = append(rows, "")
	}
	if f.drawn(level, 0) {
		rows = append(rows, th.dim.Render("project:  ")+f.project.View())
	}
	if f.drawn(level, 1) {
		rows = append(rows, th.dim.Render("priority: ")+f.priority.View())
	}
	if f.drawn(level, 2) {
		rows = append(rows, th.dim.Render("asset:    ")+f.asset.View())
	}
	if level < 2 {
		rows = append(rows, th.dim.Render("body:"))
	}
	slot = -1
	if f.drawn(level, 3) {
		slot = len(rows)
		rows = append(rows, "")
	}
	if level == 0 {
		rows = append(rows, "")
	}
	keepAt = len(rows)
	if f.focus == 4 {
		rows = append(rows, th.accentPill.Render("[ save ]"))
	} else {
		rows = append(rows, th.dim.Render("[ save ]"))
	}
	if f.errText != "" {
		rows = append(rows, th.err.Render(f.errText))
	}
	keepN = len(rows) - keepAt
	if level == 0 {
		rows = append(rows, "", th.dim.Render("Tab next  ctrl-s save  Esc cancel"))
	}
	return rows, slot, keepAt, keepN
}

// wrapRows renders rows at width so each element is one terminal line;
// a row that soft-wraps becomes several. Counting happens after this.
func wrapRows(rows []string, width int) []string {
	if width < 1 {
		return nil
	}
	var out []string
	for _, r := range rows {
		soft := lipgloss.NewStyle().Width(width).Render(r)
		for _, l := range strings.Split(ansi.Hardwrap(soft, width, true), "\n") {
			out = append(out, strings.TrimRight(l, " "))
		}
	}
	return out
}

// box draws an overlay. head and tail are lines that may be cut; keep
// are rows, each already wrapped to boxWidth-4, drawn between them and
// meant to survive: when the frame is short, tail goes first, then head,
// then the middle keep rows from the front, then the last keep row, and
// a first row that still does not fit shows its first lines. The form
// hands over field, button, error in that order, so the field being
// typed into outlives the error, which outlives the button.
func box(head []string, keep [][]string, tail []string, boxWidth, height int, align lipgloss.Position, th theme) string {
	if boxWidth < 5 {
		return ""
	}
	room := max(1, height-2)
	need := 0
	for _, r := range keep {
		need += len(r)
	}
	if len(head)+need+len(tail) > room {
		tail = tail[:max(0, room-len(head)-need)]
		head = head[:max(0, room-need)]
		for need > room && len(keep) > 2 {
			need -= len(keep[1])
			keep = append(keep[:1:1], keep[2:]...)
		}
		if need > room && len(keep) > 1 {
			keep = keep[:1]
		}
	}
	lines := append([]string{}, head...)
	for _, r := range keep {
		lines = append(lines, r...)
	}
	lines = append(lines, tail...)
	if len(lines) > room {
		lines = lines[:room]
	}
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(th.accent.GetForeground()).
		Padding(0, 1).
		Width(boxWidth).
		Align(align).
		Render(lipgloss.JoinVertical(align, lines...))
}

func (f *formModel) setFocus(target int) tea.Cmd {
	step := 1
	if target < f.focus {
		step = -1
	}
	// Fields outside the ring at this level are skipped in the
	// direction of travel.
	for !f.inRing(f.level, (target%5+5)%5) {
		target += step
	}
	f.focus = (target%5 + 5) % 5
	f.project.Blur()
	f.priority.Blur()
	f.asset.Blur()
	f.body.Blur()
	switch f.focus {
	case 0:
		return f.project.Focus()
	case 1:
		return f.priority.Focus()
	case 2:
		return f.asset.Focus()
	case 3:
		return f.body.Focus()
	case 4:
		return nil
	}
	return nil
}

func (f formModel) validate() string {
	if strings.TrimSpace(f.body.Value()) == "" {
		return "body cannot be blank"
	}

	pri := strings.TrimSpace(f.priority.Value())
	if pri != "" {
		p, err := strconv.Atoi(pri)
		if err != nil || p < 0 {
			return "priority must be an integer >= 0"
		}
	}

	proj := strings.TrimSpace(f.project.Value())
	if !f.editing && proj == "" {
		return "project cannot be blank"
	}
	if proj != "" {
		for i := 0; i < len(proj); i++ {
			c := proj[i]
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-') {
				return "project may only contain [A-Za-z0-9._-]"
			}
		}
	}

	return ""
}

func (f formModel) Update(msg tea.Msg) (formModel, tea.Cmd) {
	f.done = false
	f.cancelled = false

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.Code == tea.KeyEscape {
			f.cancelled = true
			return f, nil
		}

		if msg.Code == 's' && msg.Mod&tea.ModCtrl != 0 {
			if err := f.validate(); err != "" {
				f.errText = err
				return f.refit(), nil
			}
			f.errText = ""
			f.done = true
			return f, nil
		}

		if msg.Code == tea.KeyTab {
			if msg.Mod&tea.ModShift != 0 {
				cmd := f.setFocus(f.focus - 1)
				return f.refit(), cmd
			}
			cmd := f.setFocus(f.focus + 1)
			return f.refit(), cmd
		}

		if msg.Code == tea.KeyEnter {
			if f.focus >= 0 && f.focus <= 2 {
				cmd := f.setFocus(f.focus + 1)
				return f.refit(), cmd
			}
			if f.focus == 4 {
				if err := f.validate(); err != "" {
					f.errText = err
					return f.refit(), nil
				}
				f.errText = ""
				f.done = true
				return f, nil
			}
		}

		var cmd tea.Cmd
		switch f.focus {
		case 0:
			f.project, cmd = f.project.Update(msg)
		case 1:
			f.priority, cmd = f.priority.Update(msg)
		case 2:
			f.asset, cmd = f.asset.Update(msg)
		case 3:
			f.body, cmd = f.body.Update(msg)
		case 4:
		}
		return f.refit(), cmd

	default:
		var cmds []tea.Cmd
		var cmd tea.Cmd
		f.project, cmd = f.project.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		f.priority, cmd = f.priority.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		f.asset, cmd = f.asset.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		f.body, cmd = f.body.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return f, tea.Batch(cmds...)
	}
}

func (f formModel) submit() (method, path string, body map[string]any, success string, errText string) {
	if err := f.validate(); err != "" {
		return "", "", nil, "", err
	}

	body = make(map[string]any)

	if !f.editing {
		method = "POST"
		path = "/tasks"
		success = "created task"

		body["project"] = strings.TrimSpace(f.project.Value())
		body["body"] = f.body.Value()

		asset := strings.TrimSpace(f.asset.Value())
		if asset != "" {
			body["asset_path"] = asset
		}

		priStr := strings.TrimSpace(f.priority.Value())
		if priStr != "" {
			if p, err := strconv.Atoi(priStr); err == nil {
				body["priority"] = p
			}
		}
		return method, path, body, success, ""
	}

	method = "PATCH"
	path = "/tasks/" + f.id
	id7 := f.id
	if len(id7) > 7 {
		id7 = id7[:7]
	}
	success = "updated task " + id7

	if f.body.Value() != f.origBody {
		body["body"] = f.body.Value()
	}

	proj := strings.TrimSpace(f.project.Value())
	if proj != f.origProject && proj != "" {
		body["project"] = proj
	}

	asset := strings.TrimSpace(f.asset.Value())
	if asset != f.origAsset {
		body["asset_path"] = asset
	}

	priStr := strings.TrimSpace(f.priority.Value())
	if priStr != "" {
		if p, err := strconv.Atoi(priStr); err == nil {
			if !f.origHasPriority || p != f.origPriority {
				body["priority"] = p
			}
		}
	}

	return method, path, body, success, ""
}

// fit lays the form out for a terminal: rows are built, wrapped to the
// box and counted; the textarea gets what is left, compacting rows when
// even one is short. It sets the stored textarea size, so Update and
// View agree on the layout.
func (f *formModel) fit(width, height int, th theme) (head []string, keep [][]string, tail []string, boxWidth int) {
	f.width, f.height, f.th = width, height, th
	boxWidth, inner := boxSize(width, 20, 90)
	f.resize(max(1, inner))
	var rows []string
	var slot, keepAt, keepN, bodyH int
	for f.level = 0; f.level <= 3; f.level++ {
		rows, slot, keepAt, keepN = f.lines(f.level, th)
		bodyH = height - 2 - len(wrapRows(rows, inner))
		if slot >= 0 {
			bodyH++ // the slot row stands in for the textarea
		}
		if bodyH >= 1 || f.level == 3 {
			break
		}
	}
	if f.level == 3 {
		bodyH = 1 // the box must not jump as focus moves between fields
	}
	bodyH = max(1, min(12, bodyH))
	f.body.SetHeight(bodyH)
	head = wrapRows(rows[:keepAt], inner)
	if slot >= 0 {
		// The textarea can render its placeholder taller than its
		// height; hold it to the rows that were budgeted.
		body := wrapRows([]string{f.body.View()}, inner)
		if len(body) > bodyH {
			body = body[:bodyH]
		}
		head = append(append(wrapRows(rows[:slot], inner), body...), wrapRows(rows[slot+1:keepAt], inner)...)
	}
	if f.level == 3 {
		// Only the focused field is drawn; it outranks button and error.
		keep = append(keep, head)
		head = nil
	}
	for _, r := range rows[keepAt : keepAt+keepN] {
		keep = append(keep, wrapRows([]string{r}, inner))
	}
	return head, keep, wrapRows(rows[keepAt+keepN:], inner), boxWidth
}

// refit lays the form out again for the terminal it was last fitted
// to, so a keypress that changes the rows (an error, a focus move at
// level 3) leaves the stored layout current. Before any fit it is a
// no-op.
func (f formModel) refit() formModel {
	if f.width > 0 {
		f.fit(f.width, f.height, f.th)
	}
	return f
}

// View renders the form for the terminal it was last fitted to; the
// copy is refitted so the drawing and the stored layout are the same.
func (f formModel) View() string {
	head, keep, tail, boxWidth := f.fit(f.width, f.height, f.th)
	return box(head, keep, tail, boxWidth, f.height, lipgloss.Left, f.th)
}

type confirmModel struct {
	text, button, method, path, success string
	body                                any
}

func (c confirmModel) View(width, height int, th theme) string {
	btn := c.button
	if btn == "" {
		btn = "confirm"
	}
	actions := th.accent.Render("[y] "+btn) + "   " + th.dim.Render("[n] cancel")
	boxWidth, inner := boxSize(width, 20, 54)
	head := wrapRows([]string{c.text, ""}, inner)
	return box(head, [][]string{wrapRows([]string{actions}, inner)}, nil, boxWidth, height, lipgloss.Center, th)
}

func padRightVisual(s string, w int) string {
	sw := lipgloss.Width(s)
	if sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	return s
}

func helpView(width, height int, th theme) string {
	type keyRef struct {
		key  string
		desc string
	}

	col1 := []keyRef{
		{"j/k, ↑/↓", "move"},
		{"g/G", "first/last"},
		{"ctrl-d/ctrl-u", "half page"},
		{"PgUp/PgDn", "page"},
		{"p", "project"},
		{"/", "search"},
		{"Tab", "detail"},
		{"z", "zoom"},
		{"n", "new"},
		{"e", "edit"},
	}

	col2 := []keyRef{
		{"0-4", "filter (all/pending/leased/done/buried)"},
		{"+/-", "priority"},
		{"c", "claim"},
		{"u", "release"},
		{"D", "delete"},
		{"x", "complete"},
		{"y/Y", "copy id/body"},
		{"r", "refresh"},
		{"?", "help"},
		{"q", "quit"},
	}

	var rows []string
	rows = append(rows, th.accent.Render("Keyboard Shortcuts"), "")
	for i := 0; i < len(col1) && i < len(col2); i++ {
		k1 := th.accent.Render(padRightVisual(col1[i].key, 13))
		d1 := th.dim.Render(padRightVisual(col1[i].desc, 10))
		k2 := th.accent.Render(padRightVisual(col2[i].key, 4))
		d2 := th.dim.Render(col2[i].desc)
		rows = append(rows, k1+" "+d1+"  "+k2+" "+d2)
	}
	rows = append(rows, "", th.dim.Render("Press ? or Esc to close"))

	// Wrap to the box first, then fit the lines to the terminal before
	// drawing the border; the closing hint keeps all its lines.
	natural := lipgloss.Width(lipgloss.JoinVertical(lipgloss.Left, rows...)) + 4
	boxWidth, inner := boxSize(width, 20, natural)
	head := wrapRows(rows[:len(rows)-1], inner)
	return box(head, [][]string{wrapRows(rows[len(rows)-1:], inner)}, nil, boxWidth, height, lipgloss.Left, th)
}
