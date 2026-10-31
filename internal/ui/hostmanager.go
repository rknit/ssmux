package ui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rknit/ssmux/internal/fleet"
	"github.com/rknit/ssmux/internal/hosts"
)

// managerItem is one selectable line of the host manager: a configured
// host, one of its addresses, or a ~/.ssh/config suggestion.
type managerItem struct {
	host       fleet.HostStatus
	dest       int // index into host.Dests; -1 on the host line
	suggestion bool
}

func (it managerItem) isDest() bool { return !it.suggestion && it.dest >= 0 }

// hostManagerDialog lists the configured hosts with their status and
// addresses, and the ~/.ssh/config hosts that could be added.
type hostManagerDialog struct {
	suggestions []string // read on open and after each change
	cursor      int
	err         string
}

// openHostManager pushes the host manager.
func (m *Model) openHostManager() tea.Cmd {
	m.push(&hostManagerDialog{suggestions: m.t.Suggestions()})
	return nil
}

// addHost configures h, reloads, and starts its refresh loop.
func (m *Model) addHost(h hosts.Host) (tea.Cmd, error) {
	if err := m.t.AddHost(h); err != nil {
		return nil, err
	}
	m.reload()
	return m.startRefresh(h.Name), nil
}

// setDests replaces a host's addresses, reloads, and restarts its refresh
// loop so the new list is tried at once.
func (m *Model) setDests(name string, dests []string) (tea.Cmd, error) {
	if err := m.t.SetDests(name, dests); err != nil {
		return nil, err
	}
	m.reload()
	return m.startRefresh(name), nil
}

func (d *hostManagerDialog) items(m Model) []managerItem {
	var out []managerItem
	for _, h := range m.hosts {
		out = append(out, managerItem{host: h, dest: -1})
		for i := range h.Dests {
			out = append(out, managerItem{host: h, dest: i})
		}
	}
	for _, s := range d.suggestions {
		out = append(out, managerItem{host: fleet.HostStatus{Name: s}, dest: -1, suggestion: true})
	}
	return out
}

// selected clamps the cursor to items and returns the item under it.
func (d *hostManagerDialog) selected(items []managerItem) (managerItem, bool) {
	d.cursor = max(min(d.cursor, len(items)-1), 0)
	if len(items) == 0 {
		return managerItem{}, false
	}
	return items[d.cursor], true
}

// moveTo puts the cursor on the given host's address i, or on the host line
// when i is -1.
func (d *hostManagerDialog) moveTo(m Model, host string, i int) {
	for n, it := range d.items(m) {
		if !it.suggestion && it.host.Name == host && it.dest == i {
			d.cursor = n
		}
	}
}

// changed takes the outcome of a host edit: it keeps the error in the dialog
// and rereads the suggestions.
func (d *hostManagerDialog) changed(m *Model) func(tea.Cmd, error) tea.Cmd {
	return func(cmd tea.Cmd, err error) tea.Cmd {
		if err != nil {
			d.err = err.Error()
		}
		d.suggestions = m.t.Suggestions()
		return cmd
	}
}

func (d *hostManagerDialog) update(m *Model, k tea.KeyPressMsg) tea.Cmd {
	d.err = ""
	it, ok := d.selected(d.items(*m))
	name, dests := it.host.Name, it.host.Dests
	switch k.String() {
	case "up":
		d.cursor--
	case "down":
		d.cursor++
	case "enter":
		if ok && it.suggestion {
			return d.changed(m)(m.addHost(hosts.Host{Name: name, Dests: []string{name}}))
		}
	case "m":
		if ok && it.suggestion && len(m.hosts) > 0 {
			p := &hostPickerDialog{hostsOnly: true, title: "Merge " + name + " into",
				onPickCmd: func(m *Model, host string) tea.Cmd {
					h, _ := m.host(host)
					cmd := d.changed(m)(m.setDests(host, append(slices.Clone(h.Dests), name)))
					d.moveTo(*m, host, len(h.Dests))
					return cmd
				}}
			m.push(p)
		}
	case "n":
		return m.openInput("Add host", "name [address…]", "add", func(m *Model, in string) (tea.Cmd, error) {
			h, err := hosts.Parse(in)
			if err != nil {
				return nil, err
			}
			cmd, err := m.addHost(h)
			if err == nil {
				d.suggestions = m.t.Suggestions()
				d.moveTo(*m, h.Name, -1)
			}
			return cmd, err
		})
	case "a":
		if ok && !it.suggestion {
			return m.openInput("Add address to "+name, "address", "add", func(m *Model, in string) (tea.Cmd, error) {
				cmd, err := m.setDests(name, append(slices.Clone(dests), strings.TrimSpace(in)))
				if err == nil {
					d.suggestions = m.t.Suggestions()
					d.moveTo(*m, name, len(dests))
				}
				return cmd, err
			})
		}
	case "d":
		switch {
		case ok && it.isDest() && len(dests) == 1:
			d.err = "a host needs an address; d on the host removes it"
		case ok && it.isDest():
			cmd := d.changed(m)(m.setDests(name, slices.Delete(slices.Clone(dests), it.dest, it.dest+1)))
			d.moveTo(*m, name, min(it.dest, len(dests)-2))
			return cmd
		case ok && !it.suggestion:
			m.push(&confirmDialog{
				title:  "Remove host",
				prompt: fmt.Sprintf("remove %q?", name),
				onYes: func(m *Model) tea.Cmd {
					if err := m.t.RemoveHost(name); err != nil {
						d.err = err.Error()
					}
					m.reload()
					d.suggestions = m.t.Suggestions()
					return nil
				},
			})
		}
	case "J", "K":
		to := it.dest + 1
		if k.String() == "K" {
			to = it.dest - 1
		}
		if ok && it.isDest() && to >= 0 && to < len(dests) {
			ds := slices.Clone(dests)
			ds[it.dest], ds[to] = ds[to], ds[it.dest]
			cmd := d.changed(m)(m.setDests(name, ds))
			d.moveTo(*m, name, to)
			return cmd
		}
	}
	d.selected(d.items(*m))
	return nil
}

// hints lists the keys that act on it.
func (d *hostManagerDialog) hints(it managerItem, ok bool, m Model) string {
	var pairs []string
	switch {
	case ok && it.suggestion:
		pairs = append(pairs, "⏎", "add")
		if len(m.hosts) > 0 {
			pairs = append(pairs, "m", "merge")
		}
	case ok && it.isDest():
		pairs = append(pairs, "a", "address", "d", "remove", "J/K", "order")
	case ok:
		pairs = append(pairs, "a", "address", "d", "remove")
	}
	pairs = append(pairs, "n", "new")
	return hintKeys(pairs...) + "  " + keyStyle.Render("Esc")
}

// hostState describes a configured host's status.
func hostState(h fleet.HostStatus) string {
	switch {
	case h.State == fleet.Unreachable:
		reason := strings.Join(strings.Fields(h.Err), " ")
		if reason == "" {
			return "unreachable"
		}
		return "unreachable: " + reason
	case h.State == fleet.Connected && h.TermFallback:
		return "connected, TERM fallback"
	}
	return stateText[h.State]
}

func (d *hostManagerDialog) view(m Model) string {
	w, h := m.size()
	items := d.items(m)
	cur := min(max(d.cursor, 0), len(items)-1)
	nameW := 0
	for _, it := range items {
		if !it.suggestion && !it.isDest() {
			nameW = max(nameW, ansi.StringWidth(it.host.Name))
		}
	}
	lines := []string{headerStyle.Render("HOSTS")}
	curLine := 0
	for i, it := range items {
		if it.suggestion && (i == 0 || !items[i-1].suggestion) {
			lines = append(lines, headerStyle.Render("FROM ~/.ssh/config"))
		}
		indent, label, style := "", it.host.Name, lipgloss.NewStyle()
		switch {
		case it.isDest():
			mark := "○ "
			if it.host.State == fleet.Connected && it.host.Dests[it.dest] == it.host.Dest {
				mark = "● "
			} else {
				style = dimStyle
			}
			indent, label = "  ", mark+it.host.Dests[it.dest]
		case !it.suggestion:
			label = fit(label, nameW) + "   " + hostState(it.host)
		}
		if i == cur {
			curLine = len(lines)
			lines = append(lines, indent+"▸ "+cursorStyle.Render(label))
		} else {
			lines = append(lines, indent+"  "+style.Render(label))
		}
	}
	// Scroll so the cursor stays visible when the screen is short.
	fits := h - 2
	if d.err != "" {
		fits--
	}
	if fits > 0 && len(lines) > fits {
		start := max(min(curLine-fits+1, len(lines)-fits), 0)
		lines = lines[start : start+fits]
	}
	if d.err != "" {
		lines = append(lines, errStyle.Render(d.err))
	}
	it, ok := d.selected(items)
	return drawBox("Hosts", lines, d.hints(it, ok, m), 34, w, h)
}

// inputDialog asks for one line of text. onSubmit's error keeps it open and
// shows inside it; on success the dialog closes.
type inputDialog struct {
	title    string
	prompt   string
	action   string
	input    textinput.Model
	onSubmit func(m *Model, value string) (tea.Cmd, error)
	err      string
}

// openInput pushes an input dialog and focuses it. action names what ⏎
// does.
func (m *Model) openInput(title, prompt, action string, onSubmit func(m *Model, value string) (tea.Cmd, error)) tea.Cmd {
	d := &inputDialog{title: title, prompt: prompt, action: action, input: textinput.New(), onSubmit: onSubmit}
	d.input.Prompt = ""
	m.push(d)
	return d.input.Focus()
}

func (d *inputDialog) update(m *Model, k tea.KeyPressMsg) tea.Cmd {
	d.err = ""
	if k.String() == "enter" {
		cmd, err := d.onSubmit(m, d.input.Value())
		if err != nil {
			d.err = err.Error()
			return nil
		}
		m.pop()
		return cmd
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(k)
	return cmd
}

func (d *inputDialog) view(m Model) string {
	w, h := m.size()
	lines := []string{d.prompt + "  " + d.input.View()}
	if d.err != "" {
		lines = append(lines, errStyle.Render(d.err))
	}
	return drawBox(d.title, lines, hintKeys("⏎", d.action, "Esc", "cancel"), 30, w, h)
}

// confirmDialog asks a y/n question. y closes it and runs onYes; any other
// key closes it.
type confirmDialog struct {
	title  string
	prompt string
	onYes  func(m *Model) tea.Cmd
}

func (d *confirmDialog) update(m *Model, k tea.KeyPressMsg) tea.Cmd {
	m.pop()
	if k.String() != "y" {
		return nil
	}
	return d.onYes(m)
}

func (d *confirmDialog) view(m Model) string {
	w, h := m.size()
	return drawBox(d.title, []string{d.prompt}, hintKeys("y", "yes", "n", "no"), 20, w, h)
}
