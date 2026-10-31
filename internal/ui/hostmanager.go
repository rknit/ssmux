package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rknit/ssmux/internal/fleet"
	"github.com/rknit/ssmux/internal/hosts"
)

// managerItem is one selectable line of the host manager: a configured host
// or a ~/.ssh/config suggestion.
type managerItem struct {
	host       fleet.HostStatus
	suggestion bool
}

// hostManagerDialog lists the configured hosts with their status and the
// ~/.ssh/config hosts that could be added.
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

// setDests replaces a host's destinations from space-separated input,
// reloads, and restarts its refresh loop so the new list is tried at once.
func (m *Model) setDests(name, input string) (tea.Cmd, error) {
	dests := strings.Fields(input)
	if len(dests) == 0 {
		return nil, fmt.Errorf("host has no destination")
	}
	if err := m.t.SetDests(name, dests); err != nil {
		return nil, err
	}
	m.reload()
	return m.startRefresh(name), nil
}

func (d *hostManagerDialog) items(m Model) []managerItem {
	var out []managerItem
	for _, h := range m.hosts {
		out = append(out, managerItem{host: h})
	}
	for _, s := range d.suggestions {
		out = append(out, managerItem{host: fleet.HostStatus{Name: s}, suggestion: true})
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

func (d *hostManagerDialog) update(m *Model, k tea.KeyPressMsg) tea.Cmd {
	d.err = ""
	it, ok := d.selected(d.items(*m))
	switch k.String() {
	case "up":
		d.cursor--
	case "down":
		d.cursor++
	case "enter":
		if ok && it.suggestion {
			cmd, err := m.addHost(hosts.Host{Name: it.host.Name, Dests: []string{it.host.Name}})
			if err != nil {
				d.err = err.Error()
			}
			d.suggestions = m.t.Suggestions()
			return cmd
		}
	case "a":
		return m.openInput("Add host", "name [destination…]", "", "add", func(m *Model, in string) (tea.Cmd, error) {
			h, err := hosts.Parse(in)
			if err != nil {
				return nil, err
			}
			cmd, err := m.addHost(h)
			if err == nil {
				d.suggestions = m.t.Suggestions()
			}
			return cmd, err
		})
	case "e":
		if ok && !it.suggestion {
			name := it.host.Name
			return m.openInput("Destinations of "+name, "destinations", strings.Join(it.host.Dests, " "), "save",
				func(m *Model, in string) (tea.Cmd, error) {
					cmd, err := m.setDests(name, in)
					if err == nil {
						d.suggestions = m.t.Suggestions()
					}
					return cmd, err
				})
		}
	case "d":
		if ok && !it.suggestion {
			name := it.host.Name
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
	}
	d.selected(d.items(*m))
	return nil
}

var managerHints = hintKeys("⏎", "add", "a", "type", "e", "edit", "d", "remove") + "  " + keyStyle.Render("Esc")

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
		return "connected" + via(h) + ", TERM fallback"
	case h.State == fleet.Connected:
		return "connected" + via(h)
	}
	return stateText[h.State]
}

// via names the destination a host with several is reached through.
func via(h fleet.HostStatus) string {
	if len(h.Dests) < 2 {
		return ""
	}
	return " via " + h.Dest
}

func (d *hostManagerDialog) view(m Model) string {
	w, h := m.size()
	items := d.items(m)
	cur := min(max(d.cursor, 0), len(items)-1)
	nameW := 0
	for _, it := range items {
		if !it.suggestion {
			nameW = max(nameW, ansi.StringWidth(it.host.Name))
		}
	}
	lines := []string{headerStyle.Render("HOSTS")}
	curLine := 0
	for i, it := range items {
		if it.suggestion && (i == 0 || !items[i-1].suggestion) {
			lines = append(lines, headerStyle.Render("FROM ~/.ssh/config"))
		}
		label := it.host.Name
		if !it.suggestion {
			label = fit(label, nameW) + "   " + hostState(it.host)
		}
		if i == cur {
			curLine = len(lines)
			lines = append(lines, "▸ "+cursorStyle.Render(label))
		} else {
			lines = append(lines, "  "+label)
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
	return drawBox("Hosts", lines, managerHints, 34, w, h)
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

// openInput pushes an input dialog holding value and focuses it.
// action names what ⏎ does.
func (m *Model) openInput(title, prompt, value, action string, onSubmit func(m *Model, value string) (tea.Cmd, error)) tea.Cmd {
	d := &inputDialog{title: title, prompt: prompt, action: action, input: textinput.New(), onSubmit: onSubmit}
	d.input.Prompt = ""
	d.input.SetValue(value)
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
