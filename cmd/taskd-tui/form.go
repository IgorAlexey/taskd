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

// formRow is one row of the form with its drop rank: when the terminal
// is short, rows go one at a time from the lowest rank up. Rank 0 is
// never dropped; the focused field holds it, so nothing is typed into a
// row nobody can see. What is drawn is not what is sent: submit sends
// every field.
type formRow struct {
	text  string
	rank  int
	field int // 0 project, 1 priority, 2 asset, 3 body (textarea), -1 other
}

// Drop ranks, lowest goes first. A field row with text in it adds
// rankFilled, so filled fields (at most rankProject+rankFilled) outlive
// empty ones and the title, and still go before the button; the button
// goes before the error because a button without its error invites a
// silent failure. The focused row, field or button, is rank 0.
const (
	rankHint      = 1
	rankSeparator = 2
	rankBodyLabel = 3
	rankAsset     = 4 // empty and unfocused: below the title
	rankTitle     = 5
	rankBody      = 6
	rankPriority  = 7
	rankProject   = 8
	rankFilled    = 5 // a filled asset outranks the title and an empty body
	rankButton    = 14
	rankError     = 15
)

// rows is the whole form in reading order. Field rows take their rank
// unless focused, which pins them; a field with text in it outranks
// every empty one, so what the user typed stays on screen longest.
func (f formModel) rows(th theme) []formRow {
	field := func(n, rank int, value, text string) formRow {
		switch {
		case f.focus == n:
			rank = 0
		case value != "":
			rank += rankFilled
		}
		return formRow{text: text, rank: rank, field: n}
	}
	save, saveRank := th.dim.Render("[ save ]"), rankButton
	if f.focus == 4 {
		save, saveRank = th.accentPill.Render("[ save ]"), 0
	}
	rows := []formRow{
		{text: th.accent.Render(f.title), rank: rankTitle, field: -1},
		{rank: rankSeparator, field: -1},
		field(0, rankProject, f.project.Value(), th.dim.Render("project:  ")+f.project.View()),
		field(1, rankPriority, f.priority.Value(), th.dim.Render("priority: ")+f.priority.View()),
		field(2, rankAsset, f.asset.Value(), th.dim.Render("asset:    ")+f.asset.View()),
		{text: th.dim.Render("body:"), rank: rankBodyLabel, field: -1},
		field(3, rankBody, f.body.Value(), ""),
		{rank: rankSeparator, field: -1},
	}
	if f.errText != "" {
		rows = append(rows, formRow{text: th.err.Render(f.errText), rank: rankError, field: -1})
	}
	rows = append(rows,
		formRow{text: save, rank: saveRank, field: -1},
		formRow{rank: rankHint, field: -1},
		formRow{text: th.dim.Render("Tab next  ctrl-s save  Esc cancel"), rank: rankHint, field: -1},
	)
	return rows
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

// box draws an overlay. head are lines that may be cut; keep are the
// lines of one row, already wrapped to boxWidth-4, drawn after them and
// meant to survive: when the frame is short, head goes first and keep is
// trimmed to its opening lines rather than removed.
func box(head, keep []string, boxWidth, height int, align lipgloss.Position, th theme) string {
	if boxWidth < 5 {
		return ""
	}
	room := max(1, height-2)
	if len(head)+len(keep) > room {
		head = head[:max(0, room-len(keep))]
	}
	lines := append(append([]string{}, head...), keep...)
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
// box and counted, then dropped one at a time by rank until they fit;
// the textarea, when drawn, takes what is left. It sets the stored
// textarea size, so Update and View agree on the layout.
func (f *formModel) fit(width, height int, th theme) (head []string, boxWidth int) {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	f.width, f.height, f.th = width, height, th
	boxWidth, inner := boxSize(width, 20, 90)
	f.resize(inner)

	rows := f.rows(th)
	wrapped := make([][]string, len(rows))
	total := 2 // border
	for i, r := range rows {
		if r.field == 3 {
			wrapped[i] = []string{""} // one row stands in for the textarea
		} else {
			wrapped[i] = wrapRows([]string{r.text}, inner)
		}
		total += len(wrapped[i])
	}
	for total > height {
		drop := -1
		for i, r := range rows {
			if r.rank > 0 && (drop < 0 || r.rank < rows[drop].rank) {
				drop = i
			}
		}
		if drop < 0 {
			break
		}
		total -= len(wrapped[drop])
		rows = append(rows[:drop], rows[drop+1:]...)
		wrapped = append(wrapped[:drop], wrapped[drop+1:]...)
	}

	body := -1
	for i, r := range rows {
		if r.field == 3 {
			body = i
		}
	}
	if body >= 0 {
		bodyH := max(1, min(12, height-total+1))
		f.body.SetHeight(bodyH)
		// The textarea can render its placeholder taller than its
		// height; hold it to the rows that were budgeted.
		lines := wrapRows([]string{f.body.View()}, inner)
		if len(lines) > bodyH {
			lines = lines[:bodyH]
		}
		wrapped[body] = lines
	}

	// Only the rank-0 row can be left over height: box trims it to its
	// opening lines.
	for _, w := range wrapped {
		head = append(head, w...)
	}
	return head, boxWidth
}

// refit lays the form out again for the terminal it was last fitted
// to, so a keypress that changes the rows (an error, a focus move at
// the focused-only level) leaves the stored textarea size current.
// Before any fit it is a no-op.
func (f formModel) refit() formModel {
	if f.width > 0 {
		f.fit(f.width, f.height, f.th)
	}
	return f
}

// View renders the form for the terminal it was last fitted to; the
// copy is refitted so the drawing and the stored layout are the same.
func (f formModel) View() string {
	head, boxWidth := f.fit(f.width, f.height, f.th)
	return box(head, nil, boxWidth, f.height, lipgloss.Left, f.th)
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
	return box(head, wrapRows([]string{actions}, inner), boxWidth, height, lipgloss.Center, th)
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
	const col1KeyW = 13
	const col1DescW = 10
	const col2KeyW = 4

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
		{"ctrl-s", "save form"},
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
	n := max(len(col1), len(col2))
	for i := range n {
		var left, right string
		if i < len(col1) {
			left = th.accent.Render(padRightVisual(col1[i].key, col1KeyW)) + " " + th.dim.Render(padRightVisual(col1[i].desc, col1DescW))
		} else {
			left = strings.Repeat(" ", col1KeyW+1+col1DescW)
		}
		if i < len(col2) {
			right = th.accent.Render(padRightVisual(col2[i].key, col2KeyW)) + " " + th.dim.Render(col2[i].desc)
		}
		rows = append(rows, left+"  "+right)
	}
	rows = append(rows, "", th.dim.Render("Press ? or Esc to close"))

	// Wrap to the box first, then fit the lines to the terminal before
	// drawing the border; the closing hint keeps all its lines.
	natural := lipgloss.Width(lipgloss.JoinVertical(lipgloss.Left, rows...)) + 4
	boxWidth, inner := boxSize(width, 20, natural)
	head := wrapRows(rows[:len(rows)-1], inner)
	return box(head, wrapRows(rows[len(rows)-1:], inner), boxWidth, height, lipgloss.Left, th)
}
