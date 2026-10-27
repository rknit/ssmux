package ui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// dialog is a box drawn over the list. Only the top dialog of Model.dialogs
// gets keys; Esc pops it before it sees them. Dialogs are pointers, so copies
// of a Model share them: callers must keep using the Model Update returns.
type dialog interface {
	// update handles a key. It may change m, push further dialogs or pop
	// itself, and returns a command to run.
	update(m *Model, k tea.KeyPressMsg) tea.Cmd
	// view renders the whole box, border included.
	view(m Model) string
}

// push opens d on top of the stack.
func (m *Model) push(d dialog) {
	m.dialogs = append(slices.Clip(m.dialogs), d) // never write into a shared array
}

// pop closes the top dialog.
func (m *Model) pop() {
	if len(m.dialogs) > 0 {
		m.dialogs = m.dialogs[:len(m.dialogs)-1]
	}
}

// top returns the top dialog, or nil.
func (m Model) top() dialog {
	if len(m.dialogs) == 0 {
		return nil
	}
	return m.dialogs[len(m.dialogs)-1]
}

// updateDialog sends a key to the top dialog. The list underneath never moves.
func (m Model) updateDialog(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if k.String() == "esc" {
		m.pop()
		return m, nil
	}
	cmd := m.top().update(&m, k)
	return m, cmd
}

// dimmed strips s of its styles and draws it in the dim color.
func dimmed(s string) string {
	lines := strings.Split(ansi.Strip(s), "\n")
	for i, l := range lines {
		lines[i] = dimStyle.Render(l)
	}
	return strings.Join(lines, "\n")
}

// compose draws the dialogs centered over base, everything below the top
// dialog dimmed. A dialog starts no higher than one line below the top of the
// dialog under it, so that dialog's title stays readable.
func (m Model) compose(base string, w, h int) string {
	layers := []*lipgloss.Layer{lipgloss.NewLayer(dimmed(base))}
	prevY := -1
	for i, d := range m.dialogs {
		s := d.view(m)
		if i < len(m.dialogs)-1 {
			s = dimmed(s)
		}
		x := max((w-lipgloss.Width(s))/2, 0)
		y := max((h-lipgloss.Height(s))/2, prevY+1)
		y = max(min(y, h-lipgloss.Height(s)), 0)
		prevY = y
		layers = append(layers, lipgloss.NewLayer(s).X(x).Y(y).Z(i+1))
	}
	return lipgloss.NewCanvas(w, h).Compose(lipgloss.NewCompositor(layers...)).Render()
}

// boxInner is the inner width of a box: at least minInner, wide enough for
// its lines and for title and hints with some border after them, but never
// wider than a screen w cells wide allows.
func boxInner(title string, lines []string, hints string, minInner, w int) int {
	inner := max(minInner, ansi.StringWidth(title)+3, ansi.StringWidth(hints)+3)
	for _, l := range lines {
		inner = max(inner, ansi.StringWidth(l))
	}
	return max(min(inner, w-4), 1)
}

// drawBox frames lines in a normal border on a w x h screen, with title in the
// top edge and hints in the bottom edge. Lines that don't fit are dropped.
func drawBox(title string, lines []string, hints string, minInner, w, h int) string {
	inner := boxInner(title, lines, hints, minInner, w)
	b := lipgloss.NormalBorder()
	edge := func(left, label, right string) string {
		room := inner + 3 - ansi.StringWidth(left) // box is inner+4 wide
		label = ansi.Truncate(label, room, "…")
		return left + label + strings.Repeat(b.Top, room-ansi.StringWidth(label)) + right
	}
	if n := h - 2; n >= 0 && len(lines) > n {
		lines = lines[:n]
	}
	out := []string{edge(b.TopLeft+" ", headerStyle.Render(title)+" ", b.TopRight)}
	for _, l := range lines {
		out = append(out, b.Left+" "+fit(l, inner)+" "+b.Right)
	}
	out = append(out, edge(b.BottomLeft+b.Bottom+" ", hints+" ", b.BottomRight))
	return strings.Join(out, "\n")
}

// hintKeys renders "k label" pairs for a box's bottom edge.
func hintKeys(pairs ...string) string {
	return strings.TrimPrefix(keys(pairs...), " ")
}
