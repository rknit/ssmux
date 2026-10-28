package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rknit/ssmux/internal/fleet"
)

// settle runs the command of a pending remote action, which must not have
// touched tmux yet, and feeds its result back.
func settle(t *testing.T, f *fakeTmux, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatalf("no command; calls = %v", f.calls)
	}
	if len(f.calls) != 0 {
		t.Fatalf("ran synchronously: calls = %v", f.calls)
	}
	if !strings.Contains(plain(m), "working…") {
		t.Errorf("no working indicator:\n%s", plain(m))
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); ok {
		t.Fatal("quit before the action finished")
	}
	return update(m, msg)
}

// remoteOn returns a picker over hostsFake with the cursor on box/$1.
func remoteOn(t *testing.T) (*fakeTmux, Model) {
	f := hostsFake()
	m := cursorOn(t, sized(New(f, "c"), 100, 30), "box/$1")
	f.calls = nil
	return f, m
}

func TestRemoteEnterAsync(t *testing.T) {
	f, m := remoteOn(t)
	m, cmd := press(t, m, "enter")
	// Keys while pending start nothing.
	m2, again := press(t, m, "enter", "x", "y", "r", "n", "p", "/")
	if again != nil || m2.mode != modeNormal || len(m2.dialogs) != 0 {
		t.Errorf("key while pending: cmd = %v mode = %v dialogs = %d", again != nil, m2.mode, len(m2.dialogs))
	}
	m2, cmd2 := settle(t, f, m2, cmd)
	if !reflect.DeepEqual(f.calls, []string{"SwitchClient c box/$1"}) {
		t.Errorf("calls = %v", f.calls)
	}
	if !isQuit(cmd2) {
		t.Errorf("did not quit after switching")
	}
	_ = m2
}

func TestRemoteSearchEnterAsync(t *testing.T) {
	f, m := remoteOn(t)
	m, _ = press(t, m, "/")
	m = typeText(t, m, "api")
	m, cmd := press(t, m, "enter")
	_, cmd = settle(t, f, m, cmd)
	if !isQuit(cmd) || !f.called("SwitchClient c box/$1") {
		t.Errorf("calls = %v quit = %v", f.calls, isQuit(cmd))
	}
}

func TestRemoteSwitchError(t *testing.T) {
	f, m := remoteOn(t)
	f.errs["SwitchClient"] = errFake("ssh box: timed out")
	m, cmd := press(t, m, "enter")
	m, cmd = settle(t, f, m, cmd)
	if isQuit(cmd) || m.err != "ssh box: timed out" {
		t.Errorf("err = %q quit = %v", m.err, isQuit(cmd))
	}
	if strings.Contains(plain(m), "working…") {
		t.Errorf("still working after the result")
	}
	if _, cmd = press(t, m, "enter"); cmd == nil {
		t.Errorf("enter ignored after the action finished")
	}
}

func TestEscWhilePendingQuits(t *testing.T) {
	_, m := remoteOn(t)
	m, _ = press(t, m, "enter")
	if _, cmd := press(t, m, "esc"); !isQuit(cmd) {
		t.Errorf("esc did not close the popup")
	}
}

func TestRemoteRenameAsync(t *testing.T) {
	f, m := remoteOn(t)
	m, _ = press(t, m, "r")
	m = typeText(t, m, "2")
	m, cmd := press(t, m, "enter")
	m, _ = settle(t, f, m, cmd)
	if !reflect.DeepEqual(f.calls, []string{"RenameSession box/$1 api2"}) {
		t.Errorf("calls = %v", f.calls)
	}
	if m.mode != modeNormal || m.Selected().Session.Name != "api2" {
		t.Errorf("mode = %v selected = %+v", m.mode, m.Selected())
	}

	f, m = remoteOn(t)
	f.errs["RenameSession"] = errFake("duplicate session: x")
	m, _ = press(t, m, "r")
	m, cmd = press(t, m, "enter")
	m, _ = settle(t, f, m, cmd)
	if m.mode != modeRename || m.err != "duplicate session: x" {
		t.Errorf("mode = %v err = %q", m.mode, m.err)
	}
}

func TestRemoteKillAsync(t *testing.T) {
	f, m := remoteOn(t)
	m, cmd := press(t, m, "x", "y")
	m, _ = settle(t, f, m, cmd)
	if !reflect.DeepEqual(f.calls, []string{"KillSession box/$1"}) {
		t.Errorf("calls = %v", f.calls)
	}
	if m.exists("box/$1") {
		t.Errorf("killed session still listed")
	}

	f, m = remoteOn(t)
	f.errs["KillSession"] = errFake("boom")
	m, cmd = press(t, m, "x", "y")
	m, _ = settle(t, f, m, cmd)
	if m.err != "boom" {
		t.Errorf("err = %q", m.err)
	}
}

func TestRemoteCreateAsync(t *testing.T) {
	f, m := remoteOn(t)
	m, _ = press(t, m, "n", "x")
	m, cmd := press(t, m, "enter")
	if top := topBox(t, m, "New session"); !strings.Contains(strings.Join(top, "\n"), "working…") {
		t.Errorf("working not inside the dialog:\n%s", plain(m))
	}
	m, cmd = settle(t, f, m, cmd)
	if !reflect.DeepEqual(f.calls, []string{"NewSession box x", "SwitchClient c box/$10"}) {
		t.Errorf("calls = %v", f.calls)
	}
	if !isQuit(cmd) {
		t.Errorf("did not quit")
	}

	f, m = remoteOn(t)
	f.errs["NewSession"] = errFake("duplicate session: x")
	m, _ = press(t, m, "n", "x")
	m, cmd = press(t, m, "enter")
	m, cmd = settle(t, f, m, cmd)
	if isQuit(cmd) || len(m.dialogs) != 1 || newDialog(t, m).err != "duplicate session: x" {
		t.Errorf("dialogs = %d quit = %v", len(m.dialogs), isQuit(cmd))
	}

	f, m = remoteOn(t)
	f.errs["SwitchClient"] = errFake("boom")
	m, _ = press(t, m, "n", "x")
	m, cmd = press(t, m, "enter")
	m, cmd = settle(t, f, m, cmd)
	if isQuit(cmd) || len(m.dialogs) != 0 || m.err != "boom" {
		t.Errorf("dialogs = %d quit = %v err = %q", len(m.dialogs), isQuit(cmd), m.err)
	}
}

// A remote session that arrives newer than the top RECENT row takes the
// cursor's place as long as the user hasn't moved it; after that the cursor
// stays on its session.
func TestLateRemoteKeepsCursorOnTop(t *testing.T) {
	for _, moved := range []bool{false, true} {
		f := hostsFake()
		f.current = ""
		f.hosts[0].State = fleet.Connecting
		f.sessions[1].LastAttached = 60
		m := sized(New(f, "c"), 100, 30)
		if id := m.Selected().Session.ID; id != "$1" {
			t.Fatalf("selected %s before refresh", id)
		}
		if moved {
			m, _ = press(t, m, "down", "up")
		}
		m, _ = update(m, msgs(refreshHost(f, "box", 0, 0))[0])
		want := "box/$1"
		if moved {
			want = "$1"
		}
		if id := m.Selected().Session.ID; id != want {
			t.Errorf("moved=%v: selected %s after refresh, want %s", moved, id, want)
		}
	}
}
