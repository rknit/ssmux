package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rknit/ssmux/internal/tmux"
)

// fakeTmux is an in-memory tmux server that records calls.
type fakeTmux struct {
	sessions []tmux.Session
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
	return append([]tmux.Session(nil), f.sessions...), nil
}

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
	f.nextID++
	f.sessions = append(f.sessions, tmux.Session{ID: id, Name: name})
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
