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
	scope  int
	title  int
	claims int
	worker int
	lease  int
	left   int
	id     int
}

// budgetColumns pays the title first. Worker, lease bar, remaining and id
// are all reprinted by the detail pane one keypress away, so each is shed,
// widest and most redundant first, until the title clears minTitle; the
// scope degrades to its header before the id, the last identifier on the
// row, is given up.
func budgetColumns(w, maxScope, maxWorker, maxClaims int, hasScrollbar bool) tableCols {
	const minTitle = 24
	c := tableCols{
		scope:  min(max(maxScope, 5), 12),
		claims: maxClaims,
		worker: min(maxWorker, 14),
		lease:  8,
		left:   7,
		id:     7,
	}

	fixed := 5
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

func highlightCode(s string, th theme) string {
	var b strings.Builder
	for {
		start := strings.IndexByte(s, '`')
		if start == -1 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:start])
		s = s[start+1:]
		end := strings.IndexByte(s, '`')
		if end == -1 {
			b.WriteByte('`')
			b.WriteString(s)
			break
		}
		for i, line := range strings.Split(s[:end], "\n") {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(th.code.Render(line))
		}
		s = s[end+1:]
	}
	return b.String()
}
func (m model) tabDefs() []tabDef {
	st := m.stats
	defs := []tabDef{
		{"0", "all", st.Total, ""},
		{"1", "pending", st.Pending, "pending"},
		{"2", "leased", st.Leased, "leased"},
		{"3", "done", st.Done, "done"},
	}
	if st.Buried > 0 || m.filter == "buried" {
		defs = append(defs, tabDef{"4", "buried", st.Buried, "buried"})
	}
	return defs
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
	headLeft := pill + urlPart + connPart

	var dbPart string
	if m.stats.DB != "" {
		dbPart = m.glyph.db + " " + m.stats.DB + "   "
	}
	refreshDur := m.cfg.refresh
	if refreshDur == 0 {
		refreshDur = 2 * time.Second
	}
	refreshPart := m.glyph.refresh + " every " + refreshDur.String()
	headRight := m.theme.dim.Render(dbPart + refreshPart)

	hlw := lipgloss.Width(headLeft)
	hrw := lipgloss.Width(headRight)
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

		// 4. Column header (1 row, dim, lowercase)
		var colH strings.Builder
		ind := m.glyph.sort
		if ind == "" {
			ind = "▼"
		}

		if m.sortCol == sortStatus {
			colH.WriteString(" " + ind + "p ")
		} else if m.sortCol == sortPriority {
			colH.WriteString("  p" + ind)
		} else {
			colH.WriteString("  p ")
		}

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
			colH.WriteString(padRight("", wClaims))
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
			colH.WriteString(padRight("id", wID))
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
				if len(priStr) > 1 {
					priStr = priStr[:1]
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
				if t.ClaimCount > 1 {
					cStr := padRight(fmt.Sprintf("%s %d", m.glyph.refresh, t.ClaimCount), wClaims)
					if isDone {
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

		if dRows >= 2 {
			scope, title := titleOf(curTask)
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
			if curTask.AssetPath != "" {
				chips = append(chips, curTask.AssetPath)
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
	default:
		return m.stats.Total
	}
}

func (m model) emptyState() string {
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
func (m model) footerItems() [][2]string {
	if m.mode == modeDetail || m.mode == modeZoom {
		return [][2]string{
			{"j/k", "scroll"},
			{"Tab", "back"},
			{"z", "zoom"},
			{"y/Y", "copy"},
		}
	}
	return [][2]string{
		{"j/k", "move"},
		{"0-4", "filter"},
		{"s", "sort"},
		{"p", "project"},
		{"w", "worker"},
		{"n", "new"},
		{"e", "edit"},
		{"y/Y", "copy"},
		{"+/-", "pri"},
		{"D", "delete"},
		{"x", "complete"},
		{"z", "zoom"},
	}
}

func (m model) footerPinned() [][2]string {
	return [][2]string{
		{"q", "quit"},
		{"?", "help"},
	}
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

func appendFooterTarget(targets []footerTarget, key string, start, width int) []footerTarget {
	switch key {
	case "s":
		return append(targets, footerTarget{action: "sort", start: start, end: start + width})
	case "p":
		return append(targets, footerTarget{action: "project", start: start, end: start + width})
	case "w":
		return append(targets, footerTarget{action: "worker", start: start, end: start + width})
	case "n":
		return append(targets, footerTarget{action: "create", start: start, end: start + width})
	case "e":
		return append(targets, footerTarget{action: "edit", start: start, end: start + width})
	case "+/-":
		return append(targets,
			footerTarget{action: "pri_raise", start: start, end: start + 2},
			footerTarget{action: "pri_lower", start: start + 2, end: start + width},
		)
	case "D":
		return append(targets, footerTarget{action: "delete", start: start, end: start + width})
	case "x":
		return append(targets, footerTarget{action: "complete", start: start, end: start + width})
	case "y/Y":
		return append(targets,
			footerTarget{action: "copy_id", start: start, end: start + 2},
			footerTarget{action: "copy_body", start: start + 2, end: start + width},
		)
	case "z":
		return append(targets, footerTarget{action: "zoom", start: start, end: start + width})
	case "q":
		return append(targets, footerTarget{action: "quit", start: start, end: start + width})
	case "?":
		return append(targets, footerTarget{action: "help", start: start, end: start + width})
	case "Tab":
		return append(targets, footerTarget{action: "back", start: start, end: start + width})
	}
	return targets
}

func (m model) footLeft(frw int) (string, []footerTarget) {
	w := m.width
	if m.msg != "" {
		if strings.HasPrefix(m.msg, "error: ") {
			return m.theme.err.Render(strings.TrimPrefix(m.msg, "error: ")), nil
		}
		return m.theme.accent.Render(m.msg), nil
	}
	if m.mode == modeSearch {
		return m.theme.accent.Render("/") + m.query + m.theme.accent.Render(m.glyph.caret), nil
	}
	if m.mode != modeTable && m.mode != modeDetail && m.mode != modeZoom {
		return "", nil
	}
	if m.mode == modeTable && m.query != "" {
		tag := m.theme.dim.Render("filter")
		hint := m.theme.accent.Render("[Esc clear]")
		q := trunc(m.query, max(0, w-frw-22), m.glyph.ellipsis)
		footLeft := fmt.Sprintf("%s \"%s\" %s", tag, m.theme.bold.Render(q), hint)
		start := 10 + ansi.StringWidth(q)
		end := start + 11
		return footLeft, []footerTarget{{action: "clear_search", start: start, end: end}}
	}

	availW := max(0, w-frw-1)
	items := m.footerItems()
	pinned := m.footerPinned()
	allCount := len(items) + len(pinned)
	if allCount == 0 || availW <= 0 {
		return "", nil
	}

	itemWidth := func(it [2]string) int {
		return ansi.StringWidth(it[0]) + 1 + ansi.StringWidth(it[1])
	}

	tailW := 0
	for i, it := range pinned {
		if i > 0 {
			tailW += 2
		}
		tailW += itemWidth(it)
	}

	itemsW := 0
	for i, it := range items {
		if i > 0 {
			itemsW += 2
		}
		itemsW += itemWidth(it)
	}

	totalW := itemsW
	if len(items) > 0 && len(pinned) > 0 {
		totalW += 2
	}
	totalW += tailW

	var sb strings.Builder
	var targets []footerTarget
	x := 0

	emitItem := func(it [2]string) {
		if sb.Len() > 0 {
			sb.WriteString("  ")
		}
		sb.WriteString(m.theme.accent.Render(it[0]))
		sb.WriteString(" ")
		sb.WriteString(m.theme.dim.Render(it[1]))
		wTok := itemWidth(it)
		targets = appendFooterTarget(targets, it[0], x, wTok)
		x += wTok + 2
	}

	if totalW <= availW {
		for _, it := range items {
			emitItem(it)
		}
		for _, it := range pinned {
			emitItem(it)
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
	for _, it := range items {
		wTok := itemWidth(it)
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
			for _, it := range pinned {
				emitItem(it)
			}
			return sb.String(), targets
		}
		return "", nil
	}

	for _, it := range items[:frontCount] {
		emitItem(it)
	}

	sb.WriteString("  ")
	sb.WriteString(m.theme.dim.Render(ell))
	x += ellW + 2

	for _, it := range pinned {
		emitItem(it)
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
