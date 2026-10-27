package ui

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rknit/ssmux/internal/fleet"
	"github.com/rknit/ssmux/internal/tmux"
)

func TestInitialCursorOnFirstRecent(t *testing.T) {
	m := New(newFake(), "c")
	r := m.Selected()
	if r.Section != SectionRecent || r.Session.ID != "$2" {
		t.Errorf("selected %+v, want recent $2", r)
	}
}

func TestEnterSwitches(t *testing.T) {
	f := newFake()
	_, cmd := press(t, New(f, "c"), "enter")
	if !reflect.DeepEqual(f.calls, []string{"SwitchClient c $2"}) {
		t.Errorf("calls = %v", f.calls)
	}
	if !isQuit(cmd) {
		t.Errorf("enter did not quit")
	}
}

func TestArrowsSkipHeaders(t *testing.T) {
	m := New(newFake(), "c")
	// Rows: RECENT $2 $3 $4 | PINNED $1 $2 | UNPINNED $3 $4
	m, _ = press(t, m, "down", "down", "down")
	if r := m.Selected(); r.Section != SectionPinned || r.Session.ID != "$1" {
		t.Errorf("after 3 downs: %+v", r)
	}
	m, _ = press(t, m, "alt+up")
	if r := m.Selected(); r.Section != SectionRecent || r.Session.ID != "$4" {
		t.Errorf("after alt+up: %+v", r)
	}
	m, _ = press(t, m, "up", "up", "up", "up")
	if r := m.Selected(); r.Session.ID != "$2" || r.Section != SectionRecent {
		t.Errorf("top should stick: %+v", r)
	}
	m, _ = press(t, m, "alt+down", "down", "down", "down", "down", "down", "down", "down", "down")
	if r := m.Selected(); r.Section != SectionUnpinned || r.Session.ID != "$4" {
		t.Errorf("bottom should stick: %+v", r)
	}
}

func TestPinDigit(t *testing.T) {
	f := newFake()
	m := New(f, "c")
	m, _ = press(t, m, "down", "p", "4") // recent $3 -> slot 4
	m, _ = press(t, m, "p", "0")         // unpin it again
	press(t, m, "p", "enter")            // first free = 3
	want := []string{"SetSlot $3 4", "SetSlot $3 0", "SetSlot $3 3"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
}

func TestPinEscCancels(t *testing.T) {
	f := newFake()
	m, _ := press(t, New(f, "c"), "p", "esc", "4")
	if len(f.calls) != 0 {
		t.Errorf("calls = %v", f.calls)
	}
	if m.mode != modeNormal {
		t.Errorf("mode = %v", m.mode)
	}
}

func TestShiftArrowsReorder(t *testing.T) {
	f := newFake()
	m := New(f, "c")
	m, _ = press(t, m, "down", "down", "down", "down") // pinned $2 (slot 2)
	m, _ = press(t, m, "shift+up")
	want := []string{"SetSlot $1 2", "SetSlot $2 1"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
	if r := m.Selected(); r.Session.ID != "$2" || r.Section != SectionPinned {
		t.Errorf("cursor left moved session: %+v", r)
	}
	f.calls = nil
	m, _ = press(t, m, "J") // same as shift+down
	if !reflect.DeepEqual(f.calls, []string{"SetSlot $1 1", "SetSlot $2 2"}) {
		t.Errorf("J calls = %v", f.calls)
	}
	f.calls = nil
	m, _ = press(t, m, "K", "K") // second K at slot 1 is a no-op
	if !reflect.DeepEqual(f.calls, []string{"SetSlot $1 2", "SetSlot $2 1"}) {
		t.Errorf("K calls = %v", f.calls)
	}
	f.calls = nil
	m, _ = press(t, m, "down", "down", "shift+up") // unpinned $3
	if len(f.calls) != 0 {
		t.Errorf("unpinned reorder calls = %v", f.calls)
	}
}

func TestPreviewErrorClearsPreview(t *testing.T) {
	f := newFake()
	m := tickPreview(t, New(f, "c"))
	if m.preview != "preview text" {
		t.Fatalf("preview = %q", m.preview)
	}
	f.errs["CapturePane"] = errors.New("can't find session")
	f.sessions = f.sessions[:1] // highlighted session vanished
	m = tickPreview(t, m)
	if m.preview != "" {
		t.Errorf("preview = %q, want empty", m.preview)
	}
	if len(m.sessions) != 1 {
		t.Errorf("list not refreshed: %d sessions", len(m.sessions))
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "esc"} {
		if _, cmd := press(t, New(newFake(), "c"), k); !isQuit(cmd) {
			t.Errorf("%s did not quit", k)
		}
	}
}

func TestNewSession(t *testing.T) {
	f := newFake()
	m, _ := press(t, New(f, "c"), "n")
	m = typeText(t, m, "proj")
	_, cmd := press(t, m, "enter")
	want := []string{"NewSession  proj", "SwitchClient c $10"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
	if !isQuit(cmd) {
		t.Errorf("did not quit")
	}
}

func TestNewSessionEscCancels(t *testing.T) {
	f := newFake()
	m, _ := press(t, New(f, "c"), "n")
	m = typeText(t, m, "x")
	m, _ = press(t, m, "esc")
	if len(f.calls) != 0 || len(m.dialogs) != 0 {
		t.Errorf("calls = %v dialogs = %d", f.calls, len(m.dialogs))
	}
}

func TestNewDuplicateShowsError(t *testing.T) {
	f := newFake()
	f.errs["NewSession"] = errors.New("duplicate session: proj")
	m, _ := press(t, New(f, "c"), "n")
	m = typeText(t, m, "proj")
	m, cmd := press(t, m, "enter")
	d, ok := m.top().(*newSessionDialog)
	if isQuit(cmd) || !ok || d.name.Value() != "proj" {
		t.Fatalf("dialogs = %d top = %#v", len(m.dialogs), m.top())
	}
	if !strings.Contains(m.render(), "duplicate session: proj") {
		t.Errorf("error not rendered:\n%s", m.render())
	}
}

func TestRename(t *testing.T) {
	f := newFake()
	m, _ := press(t, New(f, "c"), "r") // highlighted: bar ($2)
	if m.input.Value() != "bar" {
		t.Errorf("prefill = %q", m.input.Value())
	}
	m, _ = press(t, m, "backspace", "backspace", "backspace")
	m = typeText(t, m, "new name")
	m, _ = press(t, m, "enter")
	if !reflect.DeepEqual(f.calls, []string{"RenameSession $2 new name"}) {
		t.Errorf("calls = %v", f.calls)
	}
	if m.mode != modeNormal || m.Selected().Session.ID != "$2" {
		t.Errorf("mode = %v selected = %+v", m.mode, m.Selected())
	}
}

func TestRenameDuplicateShowsError(t *testing.T) {
	f := newFake()
	f.errs["RenameSession"] = errors.New("duplicate session: foo")
	m, _ := press(t, New(f, "c"), "r", "backspace", "backspace", "backspace")
	m = typeText(t, m, "foo")
	m, _ = press(t, m, "enter")
	if m.mode != modeRename || m.input.Value() != "foo" {
		t.Errorf("mode = %v input = %q", m.mode, m.input.Value())
	}
	if !strings.Contains(m.render(), "duplicate session: foo") {
		t.Errorf("error not rendered")
	}
}

func TestKillConfirm(t *testing.T) {
	f := newFake()
	m, _ := press(t, New(f, "c"), "x", "n")
	m, _ = press(t, m, "x", "esc")
	if len(f.calls) != 0 {
		t.Errorf("cancelled kill made calls: %v", f.calls)
	}
	press(t, m, "x", "y")
	if !reflect.DeepEqual(f.calls, []string{"KillSession $2"}) {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestKillCurrentSwitchesFirst(t *testing.T) {
	f := newFake()
	m := New(f, "c")
	m, _ = press(t, m, "down", "down", "down") // pinned foo ($1) = current
	press(t, m, "x", "y")
	want := []string{"SwitchClient c $2", "KillSession $1"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}

	solo := newFake()
	solo.sessions = solo.sessions[:1]
	press(t, New(solo, "c"), "x", "y")
	if !reflect.DeepEqual(solo.calls, []string{"KillSession $1"}) {
		t.Errorf("solo calls = %v", solo.calls)
	}
}

func TestFilter(t *testing.T) {
	f := newFake()
	m, _ := press(t, New(f, "c"), "/")
	m = typeText(t, m, "BA")
	for _, r := range m.rows {
		if !r.Header && r.Session.Name != "bar" && r.Session.Name != "baz" {
			t.Errorf("filter kept %q", r.Session.Name)
		}
	}
	if m.cursor != FirstSelectable(m.rows) {
		t.Errorf("cursor = %d", m.cursor)
	}
	m2, _ := press(t, m, "esc")
	if m2.filter != "" || m2.mode != modeNormal || len(m2.rows) != len(New(newFake(), "c").rows) {
		t.Errorf("esc did not clear filter")
	}
	m, _ = press(t, m, "down")
	sel := m.Selected().Session.ID
	_, cmd := press(t, m, "enter")
	if !isQuit(cmd) || !reflect.DeepEqual(f.calls, []string{"SwitchClient c " + sel}) {
		t.Errorf("calls = %v", f.calls)
	}
}

// vanish removes id from the fake and forces the next tick to reload the list.
func vanish(t *testing.T, f *fakeTmux, m Model, id string) Model {
	t.Helper()
	f.sessions = append(f.sessions[:f.find(id)], f.sessions[f.find(id)+1:]...)
	m.lastList = time.Time{}
	next, _ := m.Update(tickMsg{})
	return next.(Model)
}

func TestKillTargetVanishedDoesNotKillOther(t *testing.T) {
	f := newFake()
	m, _ := press(t, New(f, "c"), "x") // target bar ($2)
	m = vanish(t, f, m, "$2")
	m, _ = press(t, m, "y")
	if f.called("KillSession") {
		t.Errorf("killed another session: %v", f.calls)
	}
	if m.mode != modeNormal {
		t.Errorf("mode = %v", m.mode)
	}
}

func TestRenameTargetVanishedDoesNotRenameOther(t *testing.T) {
	f := newFake()
	m, _ := press(t, New(f, "c"), "r") // target bar ($2)
	m = vanish(t, f, m, "$2")
	m = typeText(t, m, "x")
	press(t, m, "enter")
	if f.called("RenameSession") {
		t.Errorf("renamed another session: %v", f.calls)
	}
}

func TestRenameEmptyRejected(t *testing.T) {
	f := newFake()
	m, _ := press(t, New(f, "c"), "r", "backspace", "backspace", "backspace", " ")
	m, _ = press(t, m, "enter")
	if f.called("RenameSession") {
		t.Errorf("empty rename sent: %v", f.calls)
	}
	if m.mode != modeRename || !strings.Contains(m.render(), "name can't be empty") {
		t.Errorf("mode = %v, render:\n%s", m.mode, plain(m))
	}
}

func TestInitRefreshesHostsAsync(t *testing.T) {
	f := newFake()
	f.hosts = []fleet.HostStatus{{Name: "box"}, {Name: "ci"}}
	f.after = map[string]fleet.HostStatus{"ci": {State: fleet.Unreachable, Err: "ssh: timeout"}}
	f.sessions = append(f.sessions,
		tmux.Session{ID: "box/$1", Name: "api", Host: "box", Slot: 5},
		tmux.Session{ID: "box/$2", Name: "web", Host: "box"},
		tmux.Session{ID: "ci/$1", Name: "build", Host: "ci", Slot: 6},
	)
	m := sized(New(f, "c"), 100, 30)
	if f.called("RefreshHost") {
		t.Fatalf("New waited on hosts: %v", f.calls)
	}
	out := plain(m)
	if !strings.Contains(lineWith(out, "api"), "(connecting)") {
		t.Errorf("pinned remote row not connecting:\n%s", out)
	}
	if strings.Contains(out, "web") {
		t.Errorf("unpinned row of a loading host shown:\n%s", out)
	}

	var hostMsgs []hostMsg
	for _, msg := range msgs(m.Init()) {
		if h, ok := msg.(hostMsg); ok {
			hostMsgs = append(hostMsgs, h)
		}
	}
	if len(hostMsgs) != 2 {
		t.Fatalf("hostMsgs = %v, want one per host", hostMsgs)
	}
	for _, h := range hostMsgs {
		var cmd tea.Cmd
		m, cmd = update(m, h)
		if cmd == nil {
			t.Errorf("no next refresh scheduled for %s", h.host)
		}
	}
	out = plain(m)
	if l := lineWith(out, "api"); strings.Contains(l, "(") {
		t.Errorf("connected row still has a status: %q", l)
	}
	if !strings.Contains(out, "web") {
		t.Errorf("connected host rows missing:\n%s", out)
	}
	if !strings.Contains(lineWith(out, "build"), "(unreachable)") {
		t.Errorf("unreachable row not labelled:\n%s", out)
	}
}

func TestHostMsgForRemovedHostStops(t *testing.T) {
	f := newFake()
	_, cmd := update(New(f, "c"), hostMsg{host: "gone"})
	if cmd != nil {
		t.Errorf("refresh scheduled for a host that is no longer configured")
	}
}

// remoteFake has a local current session and an unreachable pinned box row
// that the cursor starts on.
func remoteFake() *fakeTmux {
	f := newFake()
	f.sessions = []tmux.Session{
		{ID: "$1", Name: "loc", LastAttached: 50},
		{ID: "box/$1", Name: "api", Host: "box", Slot: 1, LastAttached: 10},
	}
	f.hosts = []fleet.HostStatus{{Name: "box", State: fleet.Unreachable, Err: "ssh: connect to host box: timeout"}}
	return f
}

func TestEnterOnUnreachableRow(t *testing.T) {
	f := remoteFake()
	m := New(f, "c")
	if r := m.Selected(); r.Session.Status != "unreachable" {
		t.Fatalf("selected %+v", r)
	}
	m, cmd := press(t, m, "enter")
	if m.err != "box unreachable" {
		t.Errorf("err = %q", m.err)
	}
	if f.called("SwitchClient") || isQuit(cmd) {
		t.Errorf("calls = %v quit = %v", f.calls, isQuit(cmd))
	}

	m, _ = press(t, New(f, "c"), "/")
	m = typeText(t, m, "api")
	m, cmd = press(t, m, "enter")
	if m.err != "box unreachable" || f.called("SwitchClient") || isQuit(cmd) {
		t.Errorf("search enter: err = %q calls = %v", m.err, f.calls)
	}
}

func TestPreviewAsync(t *testing.T) {
	f := newFake()
	m := New(f, "c")
	sel := m.Selected().Session.ID
	m, cmd := update(m, tickMsg{})
	if m.preview != "" || f.called("CapturePane") {
		t.Fatalf("tick captured synchronously: preview = %q calls = %v", m.preview, f.calls)
	}
	var got []previewMsg
	for _, msg := range msgs(cmd) {
		if p, ok := msg.(previewMsg); ok {
			got = append(got, p)
		}
	}
	if len(got) != 1 || got[0].id != sel || got[0].text != "preview text" {
		t.Fatalf("previewMsgs = %+v", got)
	}
	m, _ = update(m, previewMsg{id: "$4", text: "stale"})
	if m.preview != "" {
		t.Errorf("stale preview applied: %q", m.preview)
	}
	m, _ = update(m, got[0])
	if m.preview != "preview text" {
		t.Errorf("preview = %q", m.preview)
	}

	r := remoteFake()
	m = tickPreview(t, New(r, "c"))
	if r.called("CapturePane") {
		t.Errorf("captured an unreachable row: %v", r.calls)
	}
	if m.preview != "ssh: connect to host box: timeout" {
		t.Errorf("unreachable preview = %q", m.preview)
	}
}

func TestKillCurrentRemoteSwitchesAway(t *testing.T) {
	f := newFake()
	f.sessions = []tmux.Session{
		{ID: "$1", Name: "foo", LastAttached: 50},
		{ID: "box/$3", Name: "api", Host: "box", LastAttached: 60},
	}
	f.current = "box/$3"
	m := New(f, "c")
	m, _ = press(t, m, "down", "down") // UNPINNED: foo, api
	if id := m.Selected().Session.ID; id != "box/$3" {
		t.Fatalf("selected %q", id)
	}
	press(t, m, "x", "y")
	want := []string{"SwitchClient c $1", "KillSession box/$3"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
}
