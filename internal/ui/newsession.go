package ui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

const (
	focusName = iota
	focusHost
)

// newSessionDialog asks for a session name and the host to create it on.
type newSessionDialog struct {
	name  textinput.Model
	host  string // "" is local
	focus int
	err   string
}

// openNewSession pushes a New session dialog whose host is the selected
// row's, or local when that row is local or its host is unreachable.
func (m *Model) openNewSession() tea.Cmd {
	d := &newSessionDialog{name: textinput.New()}
	d.name.Prompt = ""
	if s := m.Selected().Session; s.Status != "unreachable" {
		d.host = s.Host
	}
	m.push(d)
	d.fitName(*m)
	return d.name.Focus()
}

// hostLabel is how a host is shown: the local host is "local".
func hostLabel(host string) string {
	if host == "" {
		return "local"
	}
	return host
}

func (d *newSessionDialog) update(m *Model, k tea.KeyPressMsg) tea.Cmd {
	d.err = ""
	switch k.String() {
	case "tab", "shift+tab", "up", "down":
		if d.focus == focusName {
			d.focus = focusHost
			d.name.Blur()
			return nil
		}
		d.focus = focusName
		return d.name.Focus()
	}
	if d.focus == focusHost {
		switch k.String() {
		case "enter", "space":
			m.push(newHostPicker(*m, d.host, func(host string) { d.host = host }))
		case "r":
			d.host = ""
		}
		return nil
	}
	if k.String() == "enter" {
		return d.create(m)
	}
	d.fitName(*m)
	var cmd tea.Cmd
	d.name, cmd = d.name.Update(k)
	return cmd
}

// create makes the session on the chosen host and switches to it. If the
// switch fails the session exists, so the dialog closes and the list shows it.
// On a remote host both run as a command reporting a createMsg.
func (d *newSessionDialog) create(m *Model) tea.Cmd {
	if d.host != "" {
		t, client, host, name := m.t, m.client, d.host, d.name.Value()
		m.pending = true
		return func() tea.Msg {
			id, err := t.NewSession(host, name)
			if err != nil {
				return createMsg{newErr: err}
			}
			return createMsg{switchErr: t.SwitchClient(client, id)}
		}
	}
	id, err := m.t.NewSession(d.host, d.name.Value())
	if err != nil {
		d.err = err.Error()
		return nil
	}
	if err := m.t.SwitchClient(m.client, id); err != nil {
		m.pop()
		m.err = err.Error()
		m.reload()
		return nil
	}
	return tea.Quit
}

var (
	newNameHints = hintKeys("⏎", "create", "Tab", "host", "Esc", "cancel")
	newHostHints = hintKeys("⏎", "choose", "r", "local", "Tab", "name") + "  " + keyStyle.Render("Esc")
)

const namePrefix = "name  "

// status is the line under the fields: progress or the last error.
func (d *newSessionDialog) status(m Model) []string {
	switch {
	case m.pending:
		return []string{dimStyle.Render("working…")}
	case d.err != "":
		return []string{errStyle.Render(d.err)}
	}
	return nil
}

// inner is the box's inner width. It doesn't depend on the name, which
// scrolls inside it.
func (d *newSessionDialog) inner(m Model) int {
	w, _ := m.size()
	lines := d.status(m)
	// Size for both hint lines so the box doesn't jump when focus moves.
	inner := boxInner("New session", lines, newHostHints, 33, w)
	return boxInner("New session", lines, newNameHints, inner, w)
}

// fitName sizes the name input to the box, leaving a cell for the cursor
// at the end, so long names scroll instead of being cut off.
func (d *newSessionDialog) fitName(m Model) {
	d.name.SetWidth(max(d.inner(m)-len(namePrefix)-1, 1))
}

func (d *newSessionDialog) view(m Model) string {
	w, h := m.size()
	d.fitName(m) // the screen or the status line may have changed
	lines := append([]string{namePrefix + d.name.View()}, d.status(m)...)
	inner := d.inner(m)
	host := hostLabel(d.host)
	hints := newNameHints
	if d.focus == focusHost {
		host = cursorStyle.Render(host)
		hints = newHostHints
	}
	host = fit("host  "+host, inner-1) + "▸"
	lines = append(lines[:1], append([]string{host}, lines[1:]...)...)
	return drawBox("New session", lines, hints, inner, w, h)
}
