package ui

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
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
	m := New(f, "c")
	next, _ := m.Update(tickMsg{})
	m = next.(Model)
	if m.preview != "preview text" {
		t.Fatalf("preview = %q", m.preview)
	}
	f.errs["CapturePane"] = errors.New("can't find session")
	f.sessions = f.sessions[:1] // highlighted session vanished
	next, _ = m.Update(tickMsg{})
	m = next.(Model)
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

func homeDir(t *testing.T) string {
	t.Helper()
	h, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestNewSession(t *testing.T) {
	f := newFake()
	m, _ := press(t, New(f, "c"), "n")
	m = typeText(t, m, "proj")
	_, cmd := press(t, m, "enter")
	want := []string{"NewSession proj " + homeDir(t), "SwitchClient c $10"}
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
	if len(f.calls) != 0 || m.mode != modeNormal {
		t.Errorf("calls = %v mode = %v", f.calls, m.mode)
	}
}

func TestNewDuplicateShowsError(t *testing.T) {
	f := newFake()
	f.errs["NewSession"] = errors.New("duplicate session: proj")
	m, _ := press(t, New(f, "c"), "n")
	m = typeText(t, m, "proj")
	m, cmd := press(t, m, "enter")
	if isQuit(cmd) || m.mode != modeNew || m.input.Value() != "proj" {
		t.Errorf("mode = %v input = %q", m.mode, m.input.Value())
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
