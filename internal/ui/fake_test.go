package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rknit/ssmux/internal/fleet"
	"github.com/rknit/ssmux/internal/tmux"
)

// fakeTmux is an in-memory tmux server that records calls. Sessions with a
// Host belong to that host: while it is not Connected only its pinned
// sessions are listed, as placeholders carrying its status. Hosts missing
// from hosts count as connected.
type fakeTmux struct {
	sessions []tmux.Session
	hosts    []fleet.HostStatus
	after    map[string]fleet.HostStatus // host -> status RefreshHost sets
	current  string
	calls    []string
	errs     map[string]error // method name -> error to return
	capture  string
	nextID   int
}

func newFake() *fakeTmux {
	return &fakeTmux{
		sessions: []tmux.Session{
			{ID: "$1", Name: "foo", LastAttached: 50, Slot: 1},
			{ID: "$2", Name: "bar", LastAttached: 40, Slot: 2},
			{ID: "$3", Name: "baz", LastAttached: 30},
			{ID: "$4", Name: "qux", LastAttached: 20},
		},
		current: "$1",
		errs:    map[string]error{},
		capture: "preview text",
		nextID:  10,
	}
}

func (f *fakeTmux) record(format string, a ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, a...))
}

func (f *fakeTmux) find(id string) int {
	for i, s := range f.sessions {
		if s.ID == id {
			return i
		}
	}
	return -1
}

func (f *fakeTmux) ListSessions() ([]tmux.Session, error) {
	if err := f.errs["ListSessions"]; err != nil {
		return nil, err
	}
	var out []tmux.Session
	for _, s := range f.sessions {
		if h, ok := f.hostStatus(s.Host); ok && h.State != fleet.Connected {
			if s.Slot == 0 {
				continue
			}
			s.Status = "connecting"
			if h.State == fleet.Unreachable {
				s.Status = "unreachable"
			}
		}
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeTmux) hostStatus(name string) (fleet.HostStatus, bool) {
	for _, h := range f.hosts {
		if h.Name == name {
			return h, true
		}
	}
	return fleet.HostStatus{}, false
}

func (f *fakeTmux) Hosts() []fleet.HostStatus {
	return append([]fleet.HostStatus(nil), f.hosts...)
}

func (f *fakeTmux) RefreshHost(name string) {
	f.record("RefreshHost %s", name)
	for i, h := range f.hosts {
		if h.Name != name {
			continue
		}
		next, ok := f.after[name]
		if !ok {
			next = fleet.HostStatus{State: fleet.Connected}
		}
		next.Name = name
		f.hosts[i] = next
	}
}

func (f *fakeTmux) AddHost(dest string) error {
	f.record("AddHost %s", dest)
	f.hosts = append(f.hosts, fleet.HostStatus{Name: dest})
	return nil
}

func (f *fakeTmux) RemoveHost(name string) error {
	f.record("RemoveHost %s", name)
	for i, h := range f.hosts {
		if h.Name == name {
			f.hosts = append(f.hosts[:i], f.hosts[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeTmux) Suggestions() []string { return nil }

func (f *fakeTmux) SetSlot(id string, slot int) error {
	f.record("SetSlot %s %d", id, slot)
	if i := f.find(id); i >= 0 {
		f.sessions[i].Slot = slot
	}
	return nil
}

func (f *fakeTmux) NewSession(host, name string) (string, error) {
	f.record("NewSession %s %s", host, name)
	if err := f.errs["NewSession"]; err != nil {
		return "", err
	}
	id := fmt.Sprintf("$%d", f.nextID)
	if host != "" {
		id = host + "/" + id
	}
	f.nextID++
	f.sessions = append(f.sessions, tmux.Session{ID: id, Name: name, Host: host})
	return id, nil
}

func (f *fakeTmux) RenameSession(id, name string) error {
	f.record("RenameSession %s %s", id, name)
	if err := f.errs["RenameSession"]; err != nil {
		return err
	}
	f.sessions[f.find(id)].Name = name
	return nil
}

func (f *fakeTmux) KillSession(id string) error {
	f.record("KillSession %s", id)
	i := f.find(id)
	f.sessions = append(f.sessions[:i], f.sessions[i+1:]...)
	return nil
}

func (f *fakeTmux) SwitchClient(client, id string) error {
	f.record("SwitchClient %s %s", client, id)
	f.current = id
	return nil
}

func (f *fakeTmux) ClientSession(client string) (string, error) { return f.current, nil }

func (f *fakeTmux) CapturePane(id string) (string, error) {
	f.record("CapturePane %s", id)
	if err := f.errs["CapturePane"]; err != nil {
		return "", err
	}
	return f.capture, nil
}

// key builds a key press from names like "up", "shift+up", "enter", "p".
func key(s string) tea.KeyPressMsg {
	var k tea.KeyPressMsg
	parts := strings.Split(s, "+")
	for _, mod := range parts[:len(parts)-1] {
		switch mod {
		case "shift":
			k.Mod |= tea.ModShift
		case "alt":
			k.Mod |= tea.ModAlt
		}
	}
	switch name := parts[len(parts)-1]; name {
	case "up":
		k.Code = tea.KeyUp
	case "down":
		k.Code = tea.KeyDown
	case "enter":
		k.Code = tea.KeyEnter
	case "esc":
		k.Code = tea.KeyEscape
	case "backspace":
		k.Code = tea.KeyBackspace
	case "tab":
		k.Code = tea.KeyTab
	case "space":
		k.Code, k.Text = tea.KeySpace, " "
	default:
		r := []rune(name)[0]
		k.Code, k.Text = r, name
	}
	return k
}

// press sends keys in order and returns the model and the last command.
func press(t *testing.T, m Model, keys ...string) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		var next tea.Model
		next, cmd = m.Update(key(k))
		m = next.(Model)
	}
	return m, cmd
}

// typeText sends each rune of s as a key press.
func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = press(t, m, string(r))
	}
	return m
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func (f *fakeTmux) called(prefix string) bool {
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

// msgs runs cmd, and every command batched into it, and returns the messages
// that arrive within a short wait. Timers (ticks, scheduled refreshes) are
// dropped.
func msgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if b, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range b {
				out = append(out, msgs(c)...)
			}
			return out
		}
		return []tea.Msg{msg}
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}

// update sends msg to m and returns the new model and command.
func update(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

// tickPreview sends a tick and feeds back the preview it captures.
func tickPreview(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := update(m, tickMsg{})
	for _, msg := range msgs(cmd) {
		if p, ok := msg.(previewMsg); ok {
			m, _ = update(m, p)
		}
	}
	return m
}
