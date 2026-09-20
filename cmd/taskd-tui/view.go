package main

import (
	"fmt"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

func (m model) layout() (tableRows, detailRows int) {
	h := m.height
	if h <= 0 {
		h = 24
	}
	if m.mode == modeZoom {
		d := h - headerRows - tabRows - footerRows - 1
		if d < 0 {
			d = 0
		}
		return 0, d
	}
	fixed := headerRows + tabRows + gapRows + colHeadRows + footerRows
	avail := h - fixed
	if avail < 0 {
		return 0, 0
	}

	neededTable := len(m.shown)
	if neededTable < 1 {
		neededTable = 1
	}

	maxTable := avail - minDetail
	if maxTable < minTable {
		maxTable = minTable
	}
	half := avail / 2
	if neededTable > half && maxTable > half {
		maxTable = half
	}
	if maxTable > avail {
		maxTable = avail
	}

	t := neededTable
	if t > maxTable {
		t = maxTable
	}
	d := avail - t
	return t, d
}

func (m model) tableRows() int {
	t, _ := m.layout()
	return t
}

func (m model) detailRows() int {
	_, d := m.layout()
	return d
}

func trunc(s string, w int, ellipsis string) string {
	if w <= 0 {
		return ""
	}
	if uniseg.StringWidth(s) <= w {
		return s
	}
	ew := ansi.StringWidth(ellipsis)
	if ew >= w {
		return ansi.Truncate(s, w, "")
	}
	return ansi.Truncate(s, w, ellipsis)
}

type tableCols struct {
	priority int
	scope    int
	title    int
	claims   int
	worker   int
	lease    int
	left     int
	id       int
}

// budgetColumns pays the title first. Worker, lease bar, remaining and id
// are all reprinted by the detail pane one keypress away, so each is shed,
// widest and most redundant first, until the title clears minTitle; the
// scope degrades to its header before the id, the last identifier on the
// row, is given up.
func budgetColumns(w, maxPri, maxScope, maxWorker, maxClaims int, hasScrollbar bool) tableCols {
	const minTitle = 24
	c := tableCols{
		priority: max(maxPri, 1),
		scope:    min(max(maxScope, 5), 12),
		claims:   maxClaims,
		worker:   min(maxWorker, 14),
		lease:    8,
		left:     7,
		id:       7,
	}

	fixed := 4 + c.priority
	for _, cw := range []int{c.scope, c.claims, c.worker, c.lease, c.left, c.id} {
		if cw > 0 {
			fixed += 1 + cw
		}
	}
	if hasScrollbar {
		fixed++
	}

	for _, shed := range []*int{&c.claims, &c.worker, &c.lease, &c.left} {
		if w-fixed >= minTitle {
			break
		}
		if *shed > 0 {
			fixed -= 1 + *shed
			*shed = 0
		}
	}
	if w-fixed < minTitle && c.scope > 5 {
		fixed -= c.scope - 5
		c.scope = 5
	}
	if w-fixed < minTitle && c.id > 0 {
		fixed -= 1 + c.id
		c.id = 0
	}

	c.title = max(w-fixed, 5)
	return c
}

func padRight(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	return s
}

func padLine(s string, w int) string {
	sw := lipgloss.Width(s)
	if sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	if sw > w {
		return ansi.Truncate(s, w, "")
	}
	return s
}
func padLineIndent(s string, w int) string {
	if w <= detailIndent {
		return padLine(s, w)
	}
	return detailIndentSpaces + padLine(s, w-detailIndent)
}

func leaseLeft(t task, now time.Time) string {
	if t.Status != "leased" || t.LeaseExpires <= 0 {
		return ""
	}
	rem := t.LeaseExpires - now.Unix()
	if rem <= 0 {
		return "expired"
	}
	if rem >= 3600 {
		hours := rem / 3600
		mins := (rem % 3600) / 60
		return fmt.Sprintf("%dh%02dm", hours, mins)
	}
	mins := rem / 60
	secs := rem % 60
	return fmt.Sprintf("%dm%02ds", mins, secs)
}

func createdAge(createdAt int64, now time.Time) string {
	if createdAt <= 0 || now.IsZero() {
		return ""
	}
	diff := now.Unix() - createdAt
	if diff < 0 {
		diff = 0
	}
	switch {
	case diff < 60:
		return fmt.Sprintf("%ds", diff)
	case diff < 3600:
		return fmt.Sprintf("%dm", diff/60)
	case diff < 86400:
		return fmt.Sprintf("%dh", diff/3600)
	default:
		return fmt.Sprintf("%dd", diff/86400)
	}
}

func isOpeningFence(line string) (int, bool) {
	spaces := 0
	for spaces < len(line) && line[spaces] == ' ' {
		spaces++
	}
	if spaces > 3 {
		return 0, false
	}
	rest := line[spaces:]
	n := 0
	for n < len(rest) && rest[n] == '`' {
		n++
	}
	if n < 3 || strings.ContainsRune(rest[n:], '`') {
		return 0, false
	}
	return n, true
}

func isClosingFence(line string, fenceLen int) bool {
	spaces := 0
	for spaces < len(line) && line[spaces] == ' ' {
		spaces++
	}
	if spaces > 3 {
		return false
	}
	rest := strings.TrimRight(line[spaces:], " \t")
	if len(rest) < fenceLen {
		return false
	}
	for i := 0; i < len(rest); i++ {
		if rest[i] != '`' {
			return false
		}
	}
	return true
}

func highlightInline(s string, th theme) string {
	var b strings.Builder
	for {
		start := strings.IndexByte(s, '`')
		if start == -1 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:start])
		s = s[start:]

		n := 0
		for n < len(s) && s[n] == '`' {
			n++
		}
		delim := s[:n]
		search := s[n:]

		closingIdx := -1
		offset := 0
		for {
			idx := strings.Index(search[offset:], delim)
			if idx == -1 {
				break
			}
			pos := offset + idx
			after := pos + n
			for after < len(search) && search[after] == '`' {
				after++
			}
			if after > pos+n {
				offset = after
				continue
			}
			closingIdx = pos
			break
		}

		if closingIdx == -1 {
			b.WriteString(delim)
			s = search
			continue
		}

		b.WriteString(th.code.Render(search[:closingIdx]))
		s = search[closingIdx+n:]
	}
	return b.String()
}

func highlightCode(s string, th theme) string {
	lines := strings.Split(s, "\n")
	var b strings.Builder
	inFence := false
	fenceLen := 0

	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		if inFence {
			b.WriteString(th.code.Render(line))
			if isClosingFence(line, fenceLen) {
				inFence = false
			}
			continue
		}
		if n, ok := isOpeningFence(line); ok {
			inFence = true
			fenceLen = n
			b.WriteString(th.code.Render(line))
			continue
		}
		b.WriteString(highlightInline(line, th))
	}
	return b.String()
}
func (m model) tabDefs() []tabDef {
	st := m.stats
	return []tabDef{
		{"0", "all", st.Total, ""},
		{"1", "pending", st.Pending, "pending"},
		{"2", "leased", st.Leased, "leased"},
		{"3", "done", st.Done, "done"},
		{"4", "buried", st.Buried, "buried"},
		{"5", "live", st.Pending + st.Leased, "live"},
	}
}

func (m model) renderTab(tab tabDef) string {
	countStr := "-"
	if m.hasStats {
		countStr = strconv.Itoa(tab.count)
	}
	active := m.filter == tab.filter
	pill := func(body string) string {
		return m.theme.accent.Render(m.glyph.pillL) + m.theme.tabActive.Render(body) + m.theme.accent.Render(m.glyph.pillR)
	}
	switch {
	case active && m.connected:
		return pill(tab.key + " " + tab.name + " " + countStr)
	case active:
		return pill(tab.key+" "+tab.name) + " " + m.theme.dim.Render(countStr)
	default:
		return m.theme.tabKey.Render(tab.key) + " " + tab.name + " " + m.theme.dim.Render(countStr)
	}
}
func (m model) renderProject() string {
	name := m.project
	if name == "" {
		name = "all"
	}
	return m.glyph.folder + " " + m.theme.tabKey.Render("p") + " " + m.theme.dim.Render("project ") + name
}

func (m model) renderWorker() string {
	name := m.worker
	if name == "" {
		name = "all"
	}
	return m.glyph.host + " " + m.theme.tabKey.Render("w") + " " + m.theme.dim.Render("worker ") + name
}

func (m model) displayScope(t task) (scope, title string) {
	scope, title = titleOf(t)
	if scope == "" && m.project == "" {
		scope = t.Project
	}
	return scope, title
}
func (m model) View() tea.View {
	w := m.width
	if w <= 0 {
		w = 80
	}
	h := m.height
	if h <= 0 {
		h = 24
	}

	center := func(box string) tea.View {
		return frame(lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box), h)
	}
	switch m.mode {
	case modeForm:
		return center(m.form.View())
	case modeConfirm:
		return center(m.confirm.View(w, h, m.theme))
	case modeHelp:
		return center(m.help.View(h, m.theme))
	case modeNote:
		return center(m.note.View(w, h, m.theme))
	}

	tRows, dRows := m.layout()

	// 1. Header (1 row)
	pill := m.theme.accent.Render(m.glyph.pillL) + m.theme.accentPill.Render(m.glyph.server+" taskd") + m.theme.accent.Render(m.glyph.pillR)
	urlPart := " " + m.cfg.url + " "
	var connPart string
	if m.connected {
		connPart = m.theme.ok.Render(m.glyph.dot + " connected")
	} else {
		connPart = m.theme.err.Render(m.glyph.dot + " disconnected")
		if m.lastErr != "" {
			connPart += " " + m.theme.dim.Render(m.lastErr)
		}
	}
	fixedLeft := pill + urlPart + connPart
	fixW := lipgloss.Width(fixedLeft)

	var dbPart string
	if m.stats.DB != "" {
		dbPart = m.glyph.db + " " + m.stats.DB + "  "
	}
	refreshDur := m.cfg.refresh
	if refreshDur == 0 {
		refreshDur = 2 * time.Second
	}
	refreshPart := m.glyph.refresh + " every " + refreshDur.String()
	headRight := m.theme.dim.Render(dbPart + refreshPart)
	hrw := lipgloss.Width(headRight)

	headLeft := fixedLeft
	if m.cfg.worker != "" {
		avail := w - fixW - hrw - 2
		prefix := "as: "
		pw := ansi.StringWidth(prefix)
		if avail >= pw+3 {
			wName := trunc(m.cfg.worker, avail-pw, m.glyph.ellipsis)
			headLeft += " " + m.theme.dim.Render(prefix+wName)
		}
	}

	hlw := lipgloss.Width(headLeft)
	var headerLine string
	if hlw+hrw+1 <= w {
		spaces := w - hlw - hrw
		headerLine = headLeft + strings.Repeat(" ", spaces) + headRight
	} else {
		headerLine = padLine(headLeft+" "+headRight, w)
	}

	tabDefs := m.tabDefs()
	var tabParts []string
	for _, tab := range tabDefs {
		tabParts = append(tabParts, m.renderTab(tab))
	}
	tabsLeft := strings.Join(tabParts, "  ")

	tabsRight := m.renderProject() + "  " + m.renderWorker()
	tlw := lipgloss.Width(tabsLeft)
	trw := lipgloss.Width(tabsRight)
	var tabLine string
	if tlw+trw+1 <= w {
		spaces := w - tlw - trw
		tabLine = tabsLeft + strings.Repeat(" ", spaces) + tabsRight
	} else {
		tabLine = padLine(tabsLeft+" "+tabsRight, w)
	}

	// Table calculation (only if not zoomed)
	var colHeadLine string
	var tableLines []string

	if m.mode != modeZoom && tRows > 0 {
		end := m.offset + tRows
		if end > len(m.shown) {
			end = len(m.shown)
		}
		cols := m.cols
		sb := calcScrollbar(len(m.shown), m.offset, tRows)
		hasScrollbar := sb.hasScrollbar
		wScope, wClaims, wWorker := cols.scope, cols.claims, cols.worker
		wLease, wLeft, wID, wTitle := cols.lease, cols.left, cols.id, cols.title
		priHead := "p"
		ind := m.sortIndicator()
		if m.sortCol == sortPriority {
			priHead += ind
		}
		wPri := max(cols.priority, ansi.StringWidth(priHead))

		// 4. Column header (1 row, dim, lowercase)
		var colH strings.Builder
		colH.WriteString(" ")
		if m.sortCol == sortStatus {
			colH.WriteString(ind)
		} else {
			colH.WriteString(" ")
		}
		colH.WriteString(" ")
		colH.WriteString(padRight(priHead, wPri))
		colH.WriteString(" ")

		if wScope > 0 {
			scopeHead := "scope"
			if m.sortCol == sortProject {
				scopeHead += ind
			}
			if ansi.StringWidth(scopeHead) > wScope {
				scopeHead = ansi.Truncate(scopeHead, wScope, "")
			}
			colH.WriteString(padRight(scopeHead, wScope))
			colH.WriteString(" ")
		}
		colH.WriteString(padRight("title", wTitle))
		if wClaims > 0 {
			colH.WriteString(" ")
			claimsHead := "c"
			if m.sortCol == sortClaims {
				claimsHead += ind
			}
			if ansi.StringWidth(claimsHead) > wClaims {
				claimsHead = ansi.Truncate(claimsHead, wClaims, "")
			}
			colH.WriteString(padRight(claimsHead, wClaims))
		}
		if wWorker > 0 {
			colH.WriteString(" ")
			workerHead := "worker"
			if m.sortCol == sortWorker {
				workerHead += ind
			}
			if ansi.StringWidth(workerHead) > wWorker {
				workerHead = ansi.Truncate(workerHead, wWorker, "")
			}
			colH.WriteString(padRight(workerHead, wWorker))
		}
		if wLease > 0 {
			colH.WriteString(" ")
			leaseHead := "lease"
			if m.sortCol == sortLease {
				leaseHead += ind
			}
			if ansi.StringWidth(leaseHead) > wLease {
				leaseHead = ansi.Truncate(leaseHead, wLease, "")
			}
			colH.WriteString(padRight(leaseHead, wLease))
		}
		if wLeft > 0 {
			colH.WriteString(" ")
			colH.WriteString(padRight("left", wLeft))
		}
		if wID > 0 {
			colH.WriteString(" ")
			idHead := "id"
			if m.sortCol == sortID {
				idHead += ind
			}
			if ansi.StringWidth(idHead) > wID {
				idHead = ansi.Truncate(idHead, wID, "")
			}
			colH.WriteString(padRight(idHead, wID))
		}
		if hasScrollbar {
			colH.WriteString(" ")
		}
		colHeadLine = padLine(m.theme.heading.Render(colH.String()), w)

		// 5. Table rows
		tableLines = make([]string, tRows)
		if len(m.shown) == 0 {
			emptyMsg := m.emptyState()
			mid := tRows / 2
			for r := 0; r < tRows; r++ {
				if r == mid {
					tableLines[r] = padLine(lipgloss.PlaceHorizontal(w, lipgloss.Center, m.theme.dim.Render(emptyMsg)), w)
				} else {
					tableLines[r] = strings.Repeat(" ", w)
				}
			}
		} else {
			ellipsis := m.glyph.ellipsis

			for i := 0; i < tRows; i++ {
				shownIdx := m.offset + i
				var scrollCell string
				if hasScrollbar {
					if i >= sb.thumbStart && i < sb.thumbStart+sb.thumbSize {
						scrollCell = m.theme.accent.Render(m.glyph.thumb)
					} else {
						scrollCell = m.theme.dim.Render(m.glyph.track)
					}
				}

				if shownIdx >= end {
					rowW := w
					if hasScrollbar {
						rowW = w - 1
					}
					tableLines[i] = padLine(strings.Repeat(" ", rowW)+scrollCell, w)
					continue
				}

				taskIdx := m.shown[shownIdx]
				t := m.tasks[taskIdx]
				isSelected := (shownIdx == m.cursor)
				isDone := (t.Status == "done")

				var cursorCell string
				if isSelected {
					cursorCell = m.theme.accent.Render(m.glyph.cursor)
				} else {
					cursorCell = " "
				}

				var statusGlyph string
				switch t.Status {
				case "pending":
					statusGlyph = m.glyph.pending
				case "leased":
					statusGlyph = m.glyph.leased
				case "done":
					statusGlyph = m.glyph.done
				case "buried":
					statusGlyph = m.glyph.buried
				default:
					statusGlyph = " "
				}
				var statusStyled string
				if isDone {
					statusStyled = m.theme.dim.Render(statusGlyph)
				} else if t.Status == "leased" {
					statusStyled = m.theme.warn.Render(statusGlyph)
				} else if t.Status == "pending" {
					statusStyled = m.theme.ok.Render(statusGlyph)
				} else {
					statusStyled = m.theme.dim.Render(statusGlyph)
				}

				priStr := strconv.Itoa(t.Priority)
				if len(priStr) < wPri {
					priStr = strings.Repeat(" ", wPri-len(priStr)) + priStr
				}
				priStyled := m.theme.dim.Render(priStr)

				sc, rawTitle := m.displayScope(t)
				var scopeStyled string
				if wScope > 0 {
					sText := padRight(trunc(sc, wScope, ""), wScope)
					if isDone {
						scopeStyled = m.theme.dim.Render(sText)
					} else {
						scopeStyled = m.theme.scope.Render(sText)
					}
				}

				titleText := padRight(trunc(rawTitle, wTitle, ellipsis), wTitle)
				var titleStyled string
				if isDone {
					titleStyled = m.theme.dim.Render(titleText)
				} else {
					titleStyled = titleText
				}

				var claimsStyled string
				if t.ClaimCount > 1 || m.sortCol == sortClaims {
					cStr := padRight(fmt.Sprintf("%s %d", m.glyph.refresh, t.ClaimCount), wClaims)
					if isDone || t.ClaimCount <= 1 {
						claimsStyled = m.theme.dim.Render(cStr)
					} else {
						claimsStyled = m.theme.err.Render(cStr)
					}
				} else {
					claimsStyled = strings.Repeat(" ", wClaims)
				}

				var workerStyled string
				if wWorker > 0 {
					_, co := workerParts(t.Worker)
					if co != "" {
						wStr := padRight(trunc(m.glyph.branch+path.Base(co), wWorker, ""), wWorker)
						workerStyled = m.theme.dim.Render(wStr)
					} else {
						workerStyled = strings.Repeat(" ", wWorker)
					}
				}

				var barStyled string
				if t.Status == "leased" && m.stats.LeaseSeconds > 0 {
					rem := t.LeaseExpires - m.now.Unix()
					var filled int
					if rem > 0 {
						ratio := float64(rem) / float64(m.stats.LeaseSeconds)
						filled = int(math.Round(8.0 * ratio))
						if filled < 0 {
							filled = 0
						}
						if filled > 8 {
							filled = 8
						}
					}
					if isDone {
						barStyled = m.theme.dim.Render(strings.Repeat(m.glyph.barOff, 8))
					} else {
						barStyled = m.theme.accent.Render(strings.Repeat(m.glyph.barOn, filled)) + m.theme.dim.Render(strings.Repeat(m.glyph.barOff, 8-filled))
					}
				} else {
					barStyled = strings.Repeat(" ", wLease)
				}

				leftStr := leaseLeft(t, m.now)
				var leftStyled string
				if leftStr != "" {
					leftStyled = padRight(trunc(leftStr, wLeft, ""), wLeft)
					if isDone {
						leftStyled = m.theme.dim.Render(leftStyled)
					}
				} else {
					leftStyled = strings.Repeat(" ", wLeft)
				}

				id7 := t.ID
				if len(id7) > 7 {
					id7 = id7[:7]
				}
				idStyled := m.theme.dim.Render(padRight(id7, wID))

				var rowBody strings.Builder
				rowBody.WriteString(statusStyled)
				rowBody.WriteString(" ")
				rowBody.WriteString(priStyled)
				rowBody.WriteString(" ")
				if wScope > 0 {
					rowBody.WriteString(scopeStyled)
					rowBody.WriteString(" ")
				}
				rowBody.WriteString(titleStyled)
				if wClaims > 0 {
					rowBody.WriteString(" ")
					rowBody.WriteString(claimsStyled)
				}
				if wWorker > 0 {
					rowBody.WriteString(" ")
					rowBody.WriteString(workerStyled)
				}
				if wLease > 0 {
					rowBody.WriteString(" ")
					rowBody.WriteString(barStyled)
				}
				if wLeft > 0 {
					rowBody.WriteString(" ")
					rowBody.WriteString(leftStyled)
				}
				if wID > 0 {
					rowBody.WriteString(" ")
					rowBody.WriteString(idStyled)
				}

				rowW := w - 1
				if hasScrollbar {
					rowW = w - 2
				}
				bodyStr := padLine(rowBody.String(), rowW)
				if isSelected {
					bodyStr = m.theme.selected.Render(bodyStr)
				}

				tableLines[i] = padLine(cursorCell+bodyStr+scrollCell, w)
			}
		}
	}

	// 7. Detail pane (detailRows() rows)
	ruleChar := m.glyph.rule

	detailLines := make([]string, 0, dRows)
	curTask, hasTask := m.selected()

	if !hasTask {
		detailLines = append(detailLines, padLine(m.theme.rule.Render(strings.Repeat(ruleChar, w)), w))
		for r := 1; r < dRows; r++ {
			detailLines = append(detailLines, strings.Repeat(" ", w))
		}
	} else {
		vpMax := dRows - detailHeaderRows
		if vpMax < 0 {
			vpMax = 0
		}
		totalLines := m.detail.TotalLineCount()
		sb := calcScrollbar(totalLines, m.detail.YOffset(), vpMax)
		hasDetailScroll := sb.hasScrollbar

		id7 := curTask.ID
		idRest := ""
		if len(curTask.ID) > 7 {
			id7 = curTask.ID[:7]
			idRest = curTask.ID[7:]
		}
		leftPart := m.theme.rule.Render(ruleChar+ruleChar+" ") + m.theme.dim.Render(m.glyph.hash+" ") + m.theme.bold.Render(id7) + m.theme.dim.Render(idRest) + m.theme.rule.Render(" ")

		var stGlyph string
		switch curTask.Status {
		case "pending":
			stGlyph = m.glyph.pending
		case "leased":
			stGlyph = m.glyph.leased
		case "done":
			stGlyph = m.glyph.done
		case "buried":
			stGlyph = m.glyph.buried
		default:
			stGlyph = ""
		}
		stText := stGlyph + " " + curTask.Status
		var rightContent string
		if curTask.Status == "leased" {
			lStr := leaseLeft(curTask, m.now)
			if lStr != "" {
				rightContent = "[" + stText + "] [" + lStr + "] left"
			} else {
				rightContent = "[" + stText + "]"
			}
		} else {
			rightContent = "[" + stText + "]"
		}
		if hasDetailScroll && vpMax > 0 {
			curLine := m.detail.YOffset() + 1
			pct := int(math.Round(m.detail.ScrollPercent() * 100))
			rightContent += fmt.Sprintf(" [line %d/%d %d%%]", curLine, totalLines, pct)
		}
		rightPart := m.theme.rule.Render(" ") + m.theme.dim.Render(rightContent) + m.theme.rule.Render(" "+ruleChar+ruleChar)

		lpw := lipgloss.Width(leftPart)
		rpw := lipgloss.Width(rightPart)
		var ruleLine string
		if lpw+rpw < w {
			fillCount := w - lpw - rpw
			ruleLine = leftPart + m.theme.rule.Render(strings.Repeat(ruleChar, fillCount)) + rightPart
		} else {
			ruleLine = padLine(leftPart+" "+rightPart, w)
		}
		detailLines = append(detailLines, padLine(ruleLine, w))

		var title string
		if dRows >= 2 {
			var scope string
			scope, title = titleOf(curTask)
			var l1 strings.Builder
			if scope != "" {
				l1.WriteString(m.theme.scope.Render(scope) + " ")
			}
			l1.WriteString(m.theme.bold.Render(title))
			detailLines = append(detailLines, padLineIndent(l1.String(), w))
		}

		if dRows >= 3 {
			var chips []string
			if curTask.Project != "" {
				chips = append(chips, m.glyph.folder+" "+curTask.Project)
			}
			chips = append(chips, "p "+strconv.Itoa(curTask.Priority))
			h, co := workerParts(curTask.Worker)
			if h != "" {
				chips = append(chips, m.glyph.host+" "+h)
			}
			if co != "" {
				chips = append(chips, m.glyph.branch+" "+co)
			}
			if curTask.ClaimCount > 0 {
				cw := "claim"
				if curTask.ClaimCount != 1 {
					cw = "claims"
				}
				chips = append(chips, m.glyph.refresh+" "+strconv.Itoa(curTask.ClaimCount)+" "+cw)
			}
			notes := curTask.Notes
			if cached, ok := m.notesCache[curTask.ID]; ok {
				notes = cached
			}
			if len(notes) > 0 {
				nw := "note"
				if len(notes) != 1 {
					nw = "notes"
				}
				chips = append(chips, strconv.Itoa(len(notes))+" "+nw)
			}
			if curTask.AssetPath != "" && curTask.AssetPath != title {
				chips = append(chips, curTask.AssetPath)
			}
			if age := createdAge(curTask.CreatedAt, m.now); age != "" {
				chips = append(chips, age)
			}
			chipsLine := m.theme.dim.Render(strings.Join(chips, "  "))
			detailLines = append(detailLines, padLineIndent(chipsLine, w))
		}

		vpContent := m.detail.View()
		var vpLines []string
		if vpContent != "" {
			vpLines = strings.Split(vpContent, "\n")
		}

		for r := range vpMax {
			var line string
			if r < len(vpLines) {
				line = vpLines[r]
			}
			if hasDetailScroll {
				var scrollCell string
				if r >= sb.thumbStart && r < sb.thumbStart+sb.thumbSize {
					scrollCell = m.theme.accent.Render(m.glyph.thumb)
				} else {
					scrollCell = m.theme.dim.Render(m.glyph.track)
				}
				detailLines = append(detailLines, padLineIndent(line, w-scrollbarWidth)+scrollCell)
			} else {
				detailLines = append(detailLines, padLineIndent(line, w))
			}
		}
	}

	footRight := m.footRight()
	frw := lipgloss.Width(footRight)
	footLeft, _ := m.footLeft(frw)

	flw := lipgloss.Width(footLeft)
	var footerLine string
	if flw+frw+1 <= w {
		spaces := w - flw - frw
		footerLine = footLeft + strings.Repeat(" ", spaces) + footRight
	} else {
		// The key legend is the part the operator can afford to lose:
		// the position, and what it says about rows not loaded, is why
		// the line is there.
		footerLine = padLine(ansi.Truncate(footLeft, max(0, w-frw-1), "")+" "+footRight, w)
	}

	// Frame assembly
	var allLines []string
	allLines = append(allLines, headerLine)
	allLines = append(allLines, tabLine)
	allLines = append(allLines, strings.Repeat(" ", w))

	if m.mode == modeZoom {
		allLines = append(allLines, detailLines...)
	} else {
		allLines = append(allLines, colHeadLine)
		allLines = append(allLines, tableLines...)
		allLines = append(allLines, strings.Repeat(" ", w))
		allLines = append(allLines, detailLines...)
	}
	// Drop lines above the footer first so it survives until there is
	// no room for it at all.
	if len(allLines) >= h {
		allLines = allLines[:h-1]
	}
	allLines = append(allLines, footerLine)
	return frame(strings.Join(allLines, "\n"), h)
}

// frame wraps rendered content in the program's view, cut to the
// terminal height so no mode can scroll the alt screen.
func frame(content string, h int) tea.View {
	if lines := strings.Split(content, "\n"); len(lines) > h {
		content = strings.Join(lines[:h], "\n")
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m model) corpusCount() int {
	if !m.hasStats {
		return 0
	}
	switch m.filter {
	case "pending":
		return m.stats.Pending
	case "leased":
		return m.stats.Leased
	case "done":
		return m.stats.Done
	case "buried":
		return m.stats.Buried
	case "live":
		return m.stats.Pending + m.stats.Leased
	default:
		return m.stats.Total
	}
}

func (m model) emptyState() string {
	if !m.connected {
		url := m.cfg.url
		if url == "" {
			url = "(no URL configured)"
		}
		if m.lastErr != "" {
			return fmt.Sprintf("Disconnected from %s: %s", url, m.lastErr)
		}
		return fmt.Sprintf("Disconnected from %s", url)
	}
	if m.project != "" && !slices.Contains(m.projects, m.project) {
		return fmt.Sprintf("No tasks in project %s. Press 'p' to cycle project.", m.project)
	}
	corpus := m.corpusCount()
	if corpus == 0 {
		if m.filter != "" {
			return fmt.Sprintf("No %s tasks. Press '0' to show all.", m.filter)
		}
		return "No tasks yet. Press 'n' to create a task."
	}
	if m.query != "" {
		if m.project != "" {
			return fmt.Sprintf("No task matches %q. Press 'Esc' to clear.", m.query)
		}
		return fmt.Sprintf("No task matches %q (0 of %d). Press 'Esc' to clear.", m.query, corpus)
	}
	return "No tasks match filter."
}

type footerAction struct {
	key    string
	label  string
	action string
}

func (m model) footerActions() []footerAction {
	var actions []footerAction
	t, hasTask := m.selected()

	if m.mode == modeDetail || m.mode == modeZoom {
		actions = append(actions,
			footerAction{"j/k", "scroll", ""},
			footerAction{"Tab", "back", "back"},
			footerAction{"z", "zoom", "zoom"},
		)
	}

	if hasTask {
		switch t.Status {
		case "pending":
			actions = append(actions, footerAction{"c", "claim", "claim"})
		case "leased":
			actions = append(actions,
				footerAction{"t", "touch", "touch"},
				footerAction{"u", "release", "release"},
				footerAction{"b", "bury", "bury"},
			)
		case "buried":
			actions = append(actions, footerAction{"K", "kick", "kick"})
		}
	}

	if m.mode == modeDetail || m.mode == modeZoom {
		if hasTask {
			actions = append(actions,
				footerAction{"e", "edit", "edit"},
				footerAction{"a", "note", "note"},
				footerAction{"x", "complete", "complete"},
				footerAction{"D", "delete", "delete"},
			)
		}
		actions = append(actions, footerAction{"y/Y", "copy", "copy"})
		return actions
	}
	actions = append(actions, footerAction{"s", "sort", "sort"})
	if len(m.projects) > 0 {
		actions = append(actions, footerAction{"p", "project", "project"})
	}
	if len(m.workers) > 0 {
		actions = append(actions, footerAction{"w", "worker", "worker"})
	}
	actions = append(actions,
		footerAction{"n", "new", "create"},
		footerAction{"0-5", "filter", ""},
		footerAction{"j/k", "move", ""},
		footerAction{"e", "edit", "edit"},
		footerAction{"a", "note", "note"},
		footerAction{"D", "delete", "delete"},
		footerAction{"y/Y", "copy", "copy"},
		footerAction{"/", "search", "search"},
		footerAction{"+/-", "pri", "pri"},
		footerAction{"x", "complete", "complete"},
		footerAction{"z", "zoom", "zoom"},
		footerAction{"r", "refresh", "refresh"},
	)
	return actions
}

func (m model) pinnedActions() []footerAction {
	return []footerAction{
		{"q", "quit", "quit"},
		{"?", "help", "help"},
	}
}

func (m model) footerItems() [][2]string {
	actions := m.footerActions()
	items := make([][2]string, len(actions))
	for i, a := range actions {
		items[i] = [2]string{a.key, a.label}
	}
	return items
}

func (m model) footerPinned() [][2]string {
	actions := m.pinnedActions()
	items := make([][2]string, len(actions))
	for i, a := range actions {
		items[i] = [2]string{a.key, a.label}
	}
	return items
}

func (m model) footRight() string {
	pos := 0
	if len(m.shown) > 0 && m.cursor >= 0 {
		pos = m.cursor + 1
	}
	posStr := fmt.Sprintf("%d/%d", pos, len(m.shown))
	if m.more && m.total > len(m.tasks) {
		posStr += fmt.Sprintf("  %d of %d", len(m.tasks), m.total)
	}
	if m.pages > 1 {
		posStr += "  paged (g live)"
	}
	return m.theme.dim.Render(posStr)
}

func appendFooterTarget(targets []footerTarget, a footerAction, start, width int) []footerTarget {
	switch a.action {
	case "":
		return targets
	case "pri":
		return append(targets,
			footerTarget{action: "pri_raise", start: start, end: start + 2},
			footerTarget{action: "pri_lower", start: start + 2, end: start + width},
		)
	case "copy":
		return append(targets,
			footerTarget{action: "copy_id", start: start, end: start + 2},
			footerTarget{action: "copy_body", start: start + 2, end: start + width},
		)
	default:
		return append(targets, footerTarget{action: a.action, start: start, end: start + width})
	}
}

func (m model) footLeft(frw int) (string, []footerTarget) {
	w := m.width
	if m.msg != "" {
		if m.msgErr {
			errText := m.msg
			hint := "[Esc dismiss]"
			availW := max(0, w-frw-1)
			if availW <= 0 {
				return "", nil
			}
			hintW := ansi.StringWidth(hint)
			var rendered string
			if ansi.StringWidth(errText)+1+hintW <= availW {
				rendered = m.theme.err.Render(errText) + " " + m.theme.dim.Render(hint)
			} else if availW > hintW+2 {
				rendered = m.theme.err.Render(trunc(errText, availW-hintW-1, m.glyph.ellipsis)) + " " + m.theme.dim.Render(hint)
			} else {
				rendered = m.theme.err.Render(trunc(errText, availW, m.glyph.ellipsis))
			}
			targets := []footerTarget{{action: "dismiss", start: 0, end: ansi.StringWidth(rendered)}}
			return rendered, targets
		}
		return m.theme.accent.Render(m.msg), nil
	}
	if m.mode == modeSearch {
		return m.theme.accent.Render("/") + m.query + m.theme.accent.Render(m.glyph.caret), nil
	}
	if m.mode != modeTable && m.mode != modeDetail && m.mode != modeZoom {
		return "", nil
	}
	availW := max(0, w-frw-1)
	if availW <= 0 {
		return "", nil
	}

	var sb strings.Builder
	var targets []footerTarget
	x := 0

	if m.mode == modeTable && m.query != "" {
		tag := m.theme.dim.Render("filter")
		hint := m.theme.accent.Render("[Esc clear]")
		hintW := ansi.StringWidth(hint)
		overhead := ansi.StringWidth("filter \"\" ") + hintW
		q := trunc(m.query, max(0, availW-overhead), m.glyph.ellipsis)
		filterPill := fmt.Sprintf("%s \"%s\" %s", tag, m.theme.bold.Render(q), hint)
		filterW := ansi.StringWidth(filterPill)
		targets = append(targets, footerTarget{action: "clear_search", start: filterW - hintW, end: filterW})

		sb.WriteString(filterPill)
		x = filterW + 2
		availW -= (filterW + 2)
	}

	if availW <= 0 {
		return sb.String(), targets
	}

	actions := m.footerActions()
	pinned := m.pinnedActions()
	allCount := len(actions) + len(pinned)
	if allCount == 0 {
		return sb.String(), targets
	}

	actionWidth := func(a footerAction) int {
		return ansi.StringWidth(a.key) + 1 + ansi.StringWidth(a.label)
	}

	tailW := 0
	for i, a := range pinned {
		if i > 0 {
			tailW += 2
		}
		tailW += actionWidth(a)
	}

	sumTokens := 0
	for _, a := range actions {
		sumTokens += actionWidth(a)
	}
	for _, a := range pinned {
		sumTokens += actionWidth(a)
	}

	totalW2 := sumTokens + 2*(allCount-1)
	totalW1 := sumTokens + 1*(allCount-1)

	sep := "  "
	sepW := 2
	allFit := totalW2 <= availW || totalW1 <= availW
	if allFit && totalW2 > availW {
		sep = " "
		sepW = 1
	}

	emitItem := func(a footerAction) {
		if sb.Len() > 0 {
			sb.WriteString(sep)
		}
		sb.WriteString(m.theme.accent.Render(a.key))
		sb.WriteString(" ")
		sb.WriteString(m.theme.dim.Render(a.label))
		wTok := actionWidth(a)
		targets = appendFooterTarget(targets, a, x, wTok)
		x += wTok + sepW
	}

	if allFit {
		for _, a := range actions {
			emitItem(a)
		}
		for _, a := range pinned {
			emitItem(a)
		}
		return sb.String(), targets
	}

	ell := m.glyph.ellipsis
	ellW := ansi.StringWidth(ell)
	overhead := 2 + ellW
	if len(pinned) > 0 {
		overhead += 2 + tailW
	}

	used := 0
	frontCount := 0
	for _, a := range actions {
		wTok := actionWidth(a)
		add := wTok
		if frontCount > 0 {
			add += 2
		}
		if used+add+overhead <= availW {
			used += add
			frontCount++
		} else {
			break
		}
	}

	if frontCount == 0 {
		if len(pinned) > 0 && tailW <= availW {
			for _, a := range pinned {
				emitItem(a)
			}
		}
		return sb.String(), targets
	}

	for _, a := range actions[:frontCount] {
		emitItem(a)
	}

	sb.WriteString("  ")
	sb.WriteString(m.theme.dim.Render(ell))
	x += ellW + 2

	for _, a := range pinned {
		emitItem(a)
	}

	return sb.String(), targets
}

func (m model) footerTargets() []footerTarget {
	frw := lipgloss.Width(m.footRight())
	_, targets := m.footLeft(frw)
	return targets
}

func (m model) confirmTargets() []confirmTarget {
	if m.mode != modeConfirm {
		return nil
	}
	w := m.width
	if w <= 0 {
		w = 80
	}
	h := m.height
	if h <= 0 {
		h = 24
	}
	return m.confirm.buttonBounds(w, h)
}
func (m model) sortIndicator() string {
	if m.sortDesc {
		return m.glyph.sortRev
	}
	return m.glyph.sort
}
