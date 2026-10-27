// Package ui is the session picker shown inside the tmux popup.
package ui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/rknit/ssmux/internal/slots"
	"github.com/rknit/ssmux/internal/tmux"
)

const (
	previewInterval = 250 * time.Millisecond
	listInterval    = time.Second
)

// Tmux is the subset of tmux.Client the picker uses.
type Tmux interface {
	ListSessions() ([]tmux.Session, error)
	SetSlot(id string, slot int) error
	NewSession(host, name string) (string, error)
	RenameSession(id, name string) error
	KillSession(id string) error
	SwitchClient(client, id string) error
	ClientSession(client string) (string, error)
	CapturePane(id string) (string, error)
}

type mode int

const (
	modeNormal mode = iota
	modePin
	modeNew
	modeRename
	modeConfirmKill
	modeFilter
)

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(previewInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

type Model struct {
	t      Tmux
	client string

	sessions []tmux.Session
	current  string
	rows     []Row
	cursor   int // index into rows; -1 when there are none
	filter   string

	mode    mode
	target  string // session ID chosen when kill or rename started
	input   textinput.Model
	err     string
	preview string

	width, height int
	lastList      time.Time
}

// New loads the session list for client and puts the cursor on the first
// RECENT row (the previous session), so Enter jumps back.
func New(t Tmux, client string) Model {
	m := Model{t: t, client: client, input: textinput.New()}
	m.reload()
	m.cursor = FirstSelectable(m.rows)
	return m
}

func (m Model) Init() tea.Cmd { return tick() }

// Selected returns the highlighted row, or a zero Row if the list is empty.
func (m Model) Selected() Row {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return Row{}
	}
	return m.rows[m.cursor]
}

// reload re-reads sessions and rebuilds rows, keeping the cursor on the same
// session: same section first, then any non-RECENT row, then anywhere.
func (m *Model) reload() {
	prev := m.Selected()
	m.lastList = time.Now()
	if cur, err := m.t.ClientSession(m.client); err == nil {
		m.current = cur
	}
	if ss, err := m.t.ListSessions(); err == nil {
		m.sessions = ss
	}
	m.rows = BuildRows(m.sessions, m.current, m.filter)
	m.cursor = m.find(prev.Session.ID, prev.Section)
}

func (m Model) find(id string, sec Section) int {
	if id == "" {
		return FirstSelectable(m.rows)
	}
	fallback := -1
	for i, r := range m.rows {
		if r.Header || r.Session.ID != id {
			continue
		}
		if r.Section == sec {
			return i
		}
		if fallback < 0 || (m.rows[fallback].Section == SectionRecent && r.Section != SectionRecent) {
			fallback = i
		}
	}
	if fallback < 0 {
		return FirstSelectable(m.rows)
	}
	return fallback
}

func (m *Model) move(delta int) {
	for i := m.cursor + delta; i >= 0 && i < len(m.rows); i += delta {
		if !m.rows[i].Header {
			m.cursor = i
			return
		}
	}
}

func (m *Model) apply(changes []slots.Change) {
	for _, c := range changes {
		if err := m.t.SetSlot(c.ID, c.Slot); err != nil {
			m.err = err.Error()
			break
		}
	}
	m.reload()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		if time.Since(m.lastList) >= listInterval {
			m.reload()
		}
		m.refreshPreview()
		return m, tick()
	case tea.KeyPressMsg:
		m.err = ""
		switch m.mode {
		case modePin:
			return m.updatePin(msg)
		case modeConfirmKill:
			return m.updateConfirmKill(msg)
		case modeNew, modeRename, modeFilter:
			return m.updateInput(msg)
		default:
			return m.updateNormal(msg)
		}
	}
	return m, nil
}

func (m *Model) refreshPreview() {
	sel := m.Selected()
	if sel.Session.ID == "" {
		m.preview = ""
		return
	}
	out, err := m.t.CapturePane(sel.Session.ID)
	if err != nil {
		m.preview = ""
		m.reload()
		return
	}
	m.preview = out
}

func (m Model) updateNormal(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	sel := m.Selected()
	switch msg.String() {
	case "up", "alt+up":
		m.move(-1)
	case "down", "alt+down":
		m.move(+1)
	case "shift+up", "K":
		m.apply(slots.Move(m.sessions, sel.Session.ID, -1))
	case "shift+down", "J":
		m.apply(slots.Move(m.sessions, sel.Session.ID, +1))
	case "enter":
		if sel.Session.ID == "" {
			return m, nil
		}
		if err := m.t.SwitchClient(m.client, sel.Session.ID); err != nil {
			m.err = err.Error()
			return m, nil
		}
		return m, tea.Quit
	case "p":
		if sel.Session.ID != "" {
			m.mode = modePin
		}
	case "x":
		if sel.Session.ID != "" {
			m.mode, m.target = modeConfirmKill, sel.Session.ID
		}
	case "n":
		return m, m.startInput(modeNew, "")
	case "r":
		if sel.Session.ID != "" {
			m.target = sel.Session.ID
			return m, m.startInput(modeRename, sel.Session.Name)
		}
	case "/":
		return m, m.startInput(modeFilter, m.filter)
	case "q", "esc":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updatePin(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	id := m.Selected().Session.ID
	k := msg.String()
	switch {
	case k == "esc":
	case k == "enter":
		m.apply(slots.PinFirstFree(m.sessions, id))
	case len(k) == 1 && k[0] >= '0' && k[0] <= '9':
		m.apply(slots.Pin(m.sessions, id, int(k[0]-'0')))
	default:
		return m, nil // stay in pin mode until a valid key
	}
	m.mode = modeNormal
	return m, nil
}

func (m *Model) startInput(md mode, value string) tea.Cmd {
	m.mode = md
	m.input.SetValue(value)
	m.input.CursorEnd()
	return m.input.Focus()
}

func (m *Model) endInput() {
	m.mode = modeNormal
	m.input.Blur()
	m.input.SetValue("")
}

func (m Model) updateInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.mode == modeFilter {
			m.filter = ""
			m.reload()
		}
		m.endInput()
		return m, nil
	case "up", "alt+up":
		if m.mode == modeFilter {
			m.move(-1)
		}
		return m, nil
	case "down", "alt+down":
		if m.mode == modeFilter {
			m.move(+1)
		}
		return m, nil
	case "enter":
		return m.submitInput()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.mode == modeFilter && m.input.Value() != m.filter {
		m.filter = m.input.Value()
		m.reload()
		m.cursor = FirstSelectable(m.rows)
	}
	return m, cmd
}

func (m Model) submitInput() (tea.Model, tea.Cmd) {
	value := m.input.Value()
	switch m.mode {
	case modeNew:
		id, err := m.t.NewSession("", value)
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		if err := m.t.SwitchClient(m.client, id); err != nil {
			m.err = err.Error()
			m.endInput()
			m.reload()
			return m, nil
		}
		return m, tea.Quit
	case modeRename:
		if !m.exists(m.target) {
			m.err = "session no longer exists"
			m.endInput()
			return m, nil
		}
		if strings.TrimSpace(value) == "" {
			m.err = "name can't be empty"
			return m, nil
		}
		if err := m.t.RenameSession(m.target, value); err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.endInput()
		m.reload()
		return m, nil
	case modeFilter:
		sel := m.Selected().Session.ID
		if sel == "" {
			return m, nil
		}
		if err := m.t.SwitchClient(m.client, sel); err != nil {
			m.err = err.Error()
			return m, nil
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) updateConfirmKill(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.mode = modeNormal
	if msg.String() != "y" {
		return m, nil
	}
	id := m.target
	if !m.exists(id) {
		m.err = "session no longer exists"
		return m, nil
	}
	if id == m.current {
		// Move the client away first so killing its session doesn't detach it.
		if next, ok := m.previousSession(); ok {
			if err := m.t.SwitchClient(m.client, next); err != nil {
				m.err = err.Error()
				return m, nil
			}
		}
	}
	if err := m.t.KillSession(id); err != nil {
		m.err = err.Error()
	}
	m.reload()
	return m, nil
}

// exists reports whether id is in the last loaded session list.
func (m Model) exists(id string) bool {
	for _, s := range m.sessions {
		if s.ID == id {
			return true
		}
	}
	return false
}

// previousSession is the most recently attached session other than the current one.
func (m Model) previousSession() (string, bool) {
	var best tmux.Session
	found := false
	for _, s := range m.sessions {
		if s.ID != m.current && (!found || s.LastAttached > best.LastAttached) {
			best, found = s, true
		}
	}
	return best.ID, found
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

// Run shows the picker for client until the user switches or closes it.
func Run(t Tmux, client string) error {
	_, err := tea.NewProgram(New(t, client)).Run()
	return err
}
