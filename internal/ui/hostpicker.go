package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rknit/ssmux/internal/fleet"
)

// hostItem is one line of the host selector.
type hostItem struct {
	name   string // "" for local
	state  fleet.State
	local  bool
	manage bool // the "Manage hosts…" line
	any    bool // pickable whatever its state
}

func (it hostItem) label() string {
	if it.manage {
		return "Manage hosts…"
	}
	return hostLabel(it.name)
}

// pickable reports whether ⏎ on it chooses it as the host.
func (it hostItem) pickable() bool {
	return !it.manage && (it.local || it.any || it.state != fleet.Unreachable)
}

var stateText = map[fleet.State]string{
	fleet.Connecting:  "connecting",
	fleet.Connected:   "connected",
	fleet.Unreachable: "unreachable",
}

// hostPickerDialog chooses a host for the dialog under it.
type hostPickerDialog struct {
	filter string
	cursor int
	onPick func(host string)
	// onPickCmd, if set, runs instead of onPick and returns a command.
	onPickCmd func(m *Model, host string) tea.Cmd
	// hostsOnly lists only the configured hosts, all pickable, without
	// local or "Manage hosts…".
	hostsOnly bool
	title     string // "" means "Host"
	// onManage runs on ⏎ over "Manage hosts…". Nil does nothing.
	onManage func(m *Model) tea.Cmd
}

// newHostPicker opens with the cursor on current.
func newHostPicker(m Model, current string, onPick func(host string)) *hostPickerDialog {
	p := &hostPickerDialog{onPick: onPick, onManage: (*Model).openHostManager}
	for i, it := range p.items(m) {
		if !it.manage && it.name == current {
			p.cursor = i
		}
	}
	return p
}

// items lists local and the configured hosts matching the filter, then
// "Manage hosts…". Hosts are read from m on every call, so status changes
// and host edits show up live.
func (p *hostPickerDialog) items(m Model) []hostItem {
	var out []hostItem
	match := func(label string) bool {
		return strings.Contains(strings.ToLower(label), strings.ToLower(p.filter))
	}
	if !p.hostsOnly && match("local") {
		out = append(out, hostItem{local: true})
	}
	for _, h := range m.hosts {
		if match(h.Name) {
			out = append(out, hostItem{name: h.Name, state: h.State, any: p.hostsOnly})
		}
	}
	if p.hostsOnly {
		return out
	}
	return append(out, hostItem{manage: true})
}

// selected clamps the cursor to items and returns the item under it.
func (p *hostPickerDialog) selected(items []hostItem) (hostItem, bool) {
	p.cursor = max(min(p.cursor, len(items)-1), 0)
	if len(items) == 0 {
		return hostItem{}, false
	}
	return items[p.cursor], true
}

func (p *hostPickerDialog) update(m *Model, k tea.KeyPressMsg) tea.Cmd {
	items := p.items(*m)
	switch k.String() {
	case "up":
		p.cursor--
	case "down":
		p.cursor++
	case "enter":
		it, ok := p.selected(items)
		switch {
		case !ok:
		case it.manage:
			if p.onManage != nil {
				return p.onManage(m)
			}
		case it.pickable():
			m.pop()
			if p.onPickCmd != nil {
				return p.onPickCmd(m, it.name)
			}
			p.onPick(it.name)
		}
		return nil
	case "backspace":
		if r := []rune(p.filter); len(r) > 0 {
			p.filter = string(r[:len(r)-1])
			p.cursor = 0
		}
	default:
		if k.Text != "" {
			p.filter += k.Text
			p.cursor = 0
		}
	}
	p.selected(p.items(*m))
	return nil
}

func (p *hostPickerDialog) view(m Model) string {
	w, h := m.size()
	items := p.items(m)
	cur := p.cursor
	nameW := 0
	for _, it := range items {
		if !it.manage {
			nameW = max(nameW, ansi.StringWidth(it.label()))
		}
	}
	filter := dimStyle.Render("type to filter")
	if p.filter != "" {
		filter = p.filter
	}
	lines := []string{"› " + filter}
	for i, it := range items {
		label := it.label()
		if !it.manage {
			label = fit(label, nameW)
			if s := stateText[it.state]; !it.local {
				label += "  " + s
			}
		}
		style := lipgloss.NewStyle()
		if !it.pickable() && !it.manage {
			style = dimStyle
		}
		if i == min(max(cur, 0), len(items)-1) {
			lines = append(lines, "▸ "+style.Reverse(true).Render(label))
		} else {
			lines = append(lines, "  "+style.Render(label))
		}
	}
	// Scroll so the cursor stays visible when the screen is short.
	if fits := h - 3; fits > 0 && len(lines)-1 > fits {
		start := max(min(cur, len(items)-1)-fits+1, 0)
		lines = append(lines[:1], lines[1+start:1+start+fits]...)
	}
	title := p.title
	if title == "" {
		title = "Host"
	}
	return drawBox(title, lines, hintKeys("↑↓", "move", "⏎", "pick", "Esc", "back"), 24, w, h)
}
