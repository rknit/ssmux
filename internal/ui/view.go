package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	headerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)
	cursorStyle = lipgloss.NewStyle().Reverse(true)
	currentDot  = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render("●")
	keyStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

var sectionTitle = map[Section]string{
	SectionRecent:   "RECENT",
	SectionPinned:   "PINNED",
	SectionUnpinned: "UNPINNED",
}

// keys renders "k label" pairs as a footer line.
func keys(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, keyStyle.Render(pairs[i])+" "+pairs[i+1])
	}
	return " " + strings.Join(parts, "  ")
}

func (m Model) footer() []string {
	switch m.mode {
	case modePin:
		return []string{keys("1-9", "slot", "⏎", "first free", "0", "unpin", "Esc", "cancel")}
	case modeConfirmKill:
		return []string{fmt.Sprintf(" kill %q?  ", m.Selected().Session.Name) + keys("y", "yes", "n", "no")}
	case modeNew, modeRename:
		return []string{keys("⏎", "confirm", "Esc", "cancel")}
	case modeFilter:
		return []string{keys("↑↓", "move", "⏎", "switch", "Esc", "clear")}
	}
	return []string{
		keys("↑↓", "move", "⏎", "switch", "⇧↑↓", "reorder", "p", "pin", "n", "new"),
		keys("r", "rename", "x", "kill", "/", "search", "q", "close"),
	}
}

// promptLine is the line above the footer: the text input or an error.
func (m Model) promptLine() (string, bool) {
	var s string
	switch m.mode {
	case modeNew:
		s = " new session: " + m.input.View()
	case modeRename:
		s = " rename: " + m.input.View()
	case modeFilter:
		s = " search: " + m.input.View()
	}
	if m.err != "" {
		if s != "" {
			s += "  "
		}
		s += errStyle.Render(m.err)
	}
	return s, s != ""
}

func (m Model) listLine(r Row, selected bool) string {
	if r.Header {
		return " " + headerStyle.Render(sectionTitle[r.Section])
	}
	slot := "   "
	if r.Section == SectionPinned {
		slot = fmt.Sprintf("%d  ", r.Session.Slot)
	}
	line := slot + r.Session.Name
	if r.Session.Host != "" {
		line += " " + dimStyle.Render("@"+r.Session.Host)
	}
	if r.Session.Status != "" {
		line += " " + dimStyle.Render("("+r.Session.Status+")")
	}
	if r.Current {
		line += " " + currentDot
	}
	if selected {
		return "▸ " + cursorStyle.Render(line)
	}
	return "  " + line
}

// fit truncates s to w cells and pads it to exactly w.
func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

func (m Model) previewLines(h int) []string {
	lines := strings.Split(strings.TrimRight(m.preview, "\n "), "\n")
	if len(lines) > h {
		lines = lines[len(lines)-h:]
	}
	return lines
}

func (m Model) render() string {
	w, h := m.width, m.height
	if w <= 0 || h <= 0 {
		w, h = 80, 24
	}
	footer := m.footer()
	prompt, hasPrompt := m.promptLine()
	bodyH := h - len(footer)
	if hasPrompt {
		bodyH--
	}
	bodyH = max(bodyH, 1)

	listW := min(max(w*30/100, 24), w)
	previewW := max(w-listW-3, 0) // " │ "

	// Scroll the list so the cursor stays visible.
	start := 0
	if m.cursor >= bodyH {
		start = m.cursor - bodyH + 1
	}
	preview := m.previewLines(bodyH)

	var b strings.Builder
	for i := range bodyH {
		var left string
		if idx := start + i; idx < len(m.rows) {
			left = m.listLine(m.rows[idx], idx == m.cursor)
		}
		line := fit(left, listW)
		if previewW > 0 {
			right := ""
			if i < len(preview) {
				right = ansi.Truncate(preview[i], previewW, "") + "\x1b[0m"
			}
			line += dimStyle.Render(" │ ") + right
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if hasPrompt {
		b.WriteString(ansi.Truncate(prompt, w, "…"))
		b.WriteByte('\n')
	}
	for i, f := range footer {
		b.WriteString(ansi.Truncate(f, w, "…"))
		if i < len(footer)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
