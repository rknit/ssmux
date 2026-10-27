package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rknit/ssmux/internal/fleet"
)

// hostItem is one line of the host selector.
type hostItem struct {
	name   string // "" for local
	state  fleet.State
	local  bool
	manage bool // the "Manage hosts…" line
}

func (it hostItem) label() string {
	if it.manage {
		return "Manage hosts…"
	}
	return hostLabel(it.name)
}

// pickable reports whether ⏎ on it chooses it as the host.
func (it hostItem) pickable() bool {
	return !it.manage && (it.local || it.state != fleet.Unreachable)
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
	if match("local") {
		out = append(out, hostItem{local: true})
	}
	for _, h := range m.hosts {
		if match(h.Name) {
			out = append(out, hostItem{name: h.Name, state: h.State})
		}
	}
	return append(out, hostItem{manage: true})
}

// selected clamps the cursor to items and returns the item under it.
func (p *hostPickerDialog) selected(items []hostItem) hostItem {
	p.cursor = max(min(p.cursor, len(items)-1), 0)
	return items[p.cursor]
}

func (p *hostPickerDialog) update(m *Model, k tea.KeyPressMsg) tea.Cmd {
	items := p.items(*m)
	switch k.String() {
	case "up":
		p.cursor--
	case "down":
		p.cursor++
	case "enter":
		it := p.selected(items)
		switch {
		case it.manage:
			if p.onManage != nil {
				return p.onManage(m)
			}
		case it.pickable():
			m.pop()
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
		if !it.pickable() && !it.manage {
			label = dimStyle.Render(label)
		}
		if i == min(max(cur, 0), len(items)-1) {
			lines = append(lines, "▸ "+cursorStyle.Render(label))
		} else {
			lines = append(lines, "  "+label)
		}
	}
	// Scroll so the cursor stays visible when the screen is short.
	if fits := h - 3; fits > 0 && len(lines)-1 > fits {
		start := max(min(cur, len(items)-1)-fits+1, 0)
		lines = append(lines[:1], lines[1+start:1+start+fits]...)
	}
	return drawBox("Host", lines, hintKeys("↑↓", "move", "⏎", "pick", "Esc", "back"), 24, w, h)
}
