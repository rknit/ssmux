// Package ui is the session picker shown inside the tmux popup.
package ui

import (
	"maps"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/rknit/ssmux/internal/fleet"
	"github.com/rknit/ssmux/internal/slots"
	"github.com/rknit/ssmux/internal/tmux"
)

const (
	previewInterval = 250 * time.Millisecond
	listInterval    = time.Second
)

// Tmux is the subset of fleet.Fleet the picker uses. RefreshHost and
// CapturePane touch the network and the picker runs them as commands.
// SwitchClient, NewSession, RenameSession and KillSession run ssh when given
// a remote host or ID; the picker then runs them as commands too (see
// startAction) and calls them synchronously only for local targets.
type Tmux interface {
	Hosts() []fleet.HostStatus
	RefreshHost(host string)
	AddHost(dest string) error
	RemoveHost(host string) error
	Suggestions() []string
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
	modeRename
	modeConfirmKill
	modeFilter
)

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(previewInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// hostMsg reports that a refresh of host by its loop number gen finished.
type hostMsg struct {
	host string
	gen  int
}

// previewMsg carries a capture of session id.
type previewMsg struct {
	id, text string
	err      error
}

// refreshHost refreshes host after delay and reports it with a hostMsg
// from loop gen.
func refreshHost(t Tmux, host string, gen int, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		t.RefreshHost(host)
		return hostMsg{host, gen}
	})
}

// doneMsg reports a remote action started by startAction.
type doneMsg struct {
	act    action
	err    error
	reload bool // reload the list even though err is set
}

type action int

const (
	actSwitch action = iota
	actRename
	actKill
)

// createMsg reports a remote create started by the New session dialog.
// newErr is set when the session wasn't created, switchErr when it was but
// the switch failed.
type createMsg struct {
	newErr, switchErr error
}

// remote reports whether any of ids lives on a remote host.
func remote(ids ...string) bool {
	for _, id := range ids {
		if h, _ := fleet.Split(id); h != "" {
			return true
		}
	}
	return false
}

type Model struct {
	t      Tmux
	client string

	sessions []tmux.Session
	hosts    []fleet.HostStatus
	current  string
	rows     []Row
	cursor   int // index into rows; -1 when there are none
	filter   string

	mode    mode
	dialogs []dialog // open dialogs, top last; they take all keys
	target  string   // session ID chosen when kill or rename started
	input   textinput.Model
	err     string
	preview string

	// loops holds each host's current refresh loop number. Loops started by
	// Init are 0; adding a host starts a new one, so a loop left over from
	// before the host was removed stops at its next hostMsg.
	loops map[string]int

	width, height int
	lastList      time.Time
	capturing     bool // a capture command is in flight
	// pending is set while a remote action runs as a command. Action keys
	// are ignored until its result arrives.
	pending bool
}

// New loads the session list for client and puts the cursor on the first
// RECENT row (the previous session), so Enter jumps back.
func New(t Tmux, client string) Model {
	m := Model{t: t, client: client, input: textinput.New()}
	m.reload()
	m.cursor = FirstSelectable(m.rows)
	return m
}

// Init starts the preview tick and one refresh per host, so remote rows
// appear as each host answers.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tick()}
	for _, h := range m.hosts {
		cmds = append(cmds, refreshHost(m.t, h.Name, 0, 0))
	}
	return tea.Batch(cmds...)
}

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
	m.hosts = m.t.Hosts()
	names := make([]string, len(m.hosts))
	for i, h := range m.hosts {
		names[i] = h.Name
	}
	m.rows = BuildRows(m.sessions, m.current, m.filter, names)
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
		cmd := m.refreshPreview()
		return m, tea.Batch(tick(), cmd)
	case hostMsg:
		if msg.gen != m.loops[msg.host] {
			return m, nil // superseded by a newer loop for a re-added host
		}
		m.reload()
		if _, ok := m.host(msg.host); !ok {
			return m, nil // host was removed
		}
		return m, refreshHost(m.t, msg.host, msg.gen, listInterval)
	case previewMsg:
		m.capturing = false
		if msg.id != m.Selected().Session.ID {
			return m, nil
		}
		if msg.err != nil {
			m.preview = ""
			m.reload()
			return m, nil
		}
		m.preview = msg.text
		return m, nil
	case doneMsg:
		return m.finishAction(msg)
	case createMsg:
		return m.finishCreate(msg)
	case tea.KeyPressMsg:
		if m.pending {
			return m.updatePending(msg)
		}
		m.err = ""
		if len(m.dialogs) > 0 {
			return m.updateDialog(msg)
		}
		switch m.mode {
		case modePin:
			return m.updatePin(msg)
		case modeConfirmKill:
			return m.updateConfirmKill(msg)
		case modeRename, modeFilter:
			return m.updateInput(msg)
		default:
			return m.updateNormal(msg)
		}
	}
	return m, nil
}

// refreshPreview returns a command capturing the selected session. An
// unreachable row shows its host's error instead.
func (m *Model) refreshPreview() tea.Cmd {
	s := m.Selected().Session
	switch {
	case s.ID == "":
		m.preview = ""
		return nil
	case s.Status == "unreachable":
		h, _ := m.host(s.Host)
		m.preview = h.Err
		return nil
	case m.capturing:
		return nil
	}
	m.capturing = true
	t := m.t
	return func() tea.Msg {
		out, err := t.CapturePane(s.ID)
		return previewMsg{id: s.ID, text: out, err: err}
	}
}

// startRefresh starts a new refresh loop for host, superseding any older one.
func (m *Model) startRefresh(host string) tea.Cmd {
	m.loops = maps.Clone(m.loops) // copies of m share the map
	if m.loops == nil {
		m.loops = map[string]int{}
	}
	m.loops[host]++
	return refreshHost(m.t, host, m.loops[host], 0)
}

// host returns the status of the configured host named name.
func (m Model) host(name string) (fleet.HostStatus, bool) {
	for _, h := range m.hosts {
		if h.Name == name {
			return h, true
		}
	}
	return fleet.HostStatus{}, false
}

// startAction runs fn as a command for a remote action and marks the model
// pending until its doneMsg arrives.
func (m *Model) startAction(act action, fn func() error) tea.Cmd {
	m.pending = true
	return func() tea.Msg { return doneMsg{act: act, err: fn()} }
}

// finishAction applies the result of a remote action.
func (m Model) finishAction(msg doneMsg) (tea.Model, tea.Cmd) {
	m.pending = false
	if msg.err != nil {
		m.err = msg.err.Error()
		if msg.reload {
			m.reload()
		}
		return m, nil
	}
	switch msg.act {
	case actSwitch:
		return m, tea.Quit
	case actRename:
		m.endInput()
	}
	m.reload()
	return m, nil
}

// finishCreate applies the result of a remote create from the New session
// dialog, which is still on top.
func (m Model) finishCreate(msg createMsg) (tea.Model, tea.Cmd) {
	m.pending = false
	switch {
	case msg.newErr != nil:
		if d, ok := m.top().(*newSessionDialog); ok {
			d.err = msg.newErr.Error()
		}
		return m, nil
	case msg.switchErr != nil:
		m.pop()
		m.err = msg.switchErr.Error()
		m.reload()
		return m, nil
	}
	return m, tea.Quit
}

// updatePending handles keys while a remote action runs: Esc (and q in the
// list) closes the popup, arrows move in the list, the rest is ignored.
func (m Model) updatePending(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "esc" {
		return m, tea.Quit
	}
	if len(m.dialogs) > 0 {
		return m, nil
	}
	switch k {
	case "q":
		if m.mode == modeNormal {
			return m, tea.Quit
		}
	case "up", "alt+up":
		if m.mode == modeNormal || m.mode == modeFilter {
			m.move(-1)
		}
	case "down", "alt+down":
		if m.mode == modeNormal || m.mode == modeFilter {
			m.move(+1)
		}
	}
	return m, nil
}

// switchTo moves the client to s and quits, or shows why it can't.
func (m Model) switchTo(s tmux.Session) (tea.Model, tea.Cmd) {
	if s.ID == "" {
		return m, nil
	}
	if s.Status == "unreachable" {
		m.err = s.Host + " unreachable"
		return m, nil
	}
	if remote(s.ID) {
		t, client := m.t, m.client
		cmd := m.startAction(actSwitch, func() error { return t.SwitchClient(client, s.ID) })
		return m, cmd
	}
	if err := m.t.SwitchClient(m.client, s.ID); err != nil {
		m.err = err.Error()
		return m, nil
	}
	return m, tea.Quit
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
		return m.switchTo(sel.Session)
	case "p":
		if sel.Session.ID != "" {
			m.mode = modePin
		}
	case "x":
		if sel.Session.ID != "" {
			m.mode, m.target = modeConfirmKill, sel.Session.ID
		}
	case "n":
		cmd := m.openNewSession()
		return m, cmd
	case "h":
		cmd := m.openHostManager()
		return m, cmd
	case "r":
		if sel.Session.ID != "" {
			m.target = sel.Session.ID
			cmd := m.startInput(modeRename, sel.Session.Name)
			return m, cmd
		}
	case "/":
		cmd := m.startInput(modeFilter, m.filter)
		return m, cmd
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
		if remote(m.target) {
			t, id := m.t, m.target
			cmd := m.startAction(actRename, func() error { return t.RenameSession(id, value) })
			return m, cmd
		}
		if err := m.t.RenameSession(m.target, value); err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.endInput()
		m.reload()
		return m, nil
	case modeFilter:
		return m.switchTo(m.Selected().Session)
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
	// Move the client away first so killing its session doesn't detach it.
	next := ""
	if id == m.current {
		next, _ = m.previousSession()
	}
	if remote(id, next) {
		t, client := m.t, m.client
		m.pending = true
		cmd := func() tea.Msg {
			if next != "" {
				if err := t.SwitchClient(client, next); err != nil {
					return doneMsg{act: actKill, err: err}
				}
			}
			return doneMsg{act: actKill, err: t.KillSession(id), reload: true}
		}
		return m, cmd
	}
	if next != "" {
		if err := m.t.SwitchClient(m.client, next); err != nil {
			m.err = err.Error()
			return m, nil
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
