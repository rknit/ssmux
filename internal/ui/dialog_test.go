package ui

import (
	"image/color"
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rknit/ssmux/internal/fleet"
	"github.com/rknit/ssmux/internal/tmux"
)

// hostsFake has a local session, a session on the connected host box and a
// pinned session on the unreachable host ci.
func hostsFake() *fakeTmux {
	f := newFake()
	f.sessions = []tmux.Session{
		{ID: "$1", Name: "loc", LastAttached: 50},
		{ID: "box/$1", Name: "api", Host: "box", LastAttached: 40},
		{ID: "ci/$1", Name: "build", Host: "ci", Slot: 1, LastAttached: 30},
	}
	f.hosts = []fleet.HostStatus{
		{Name: "box", State: fleet.Connected},
		{Name: "ci", State: fleet.Unreachable, Err: "ssh: timeout"},
	}
	return f
}

// cursorOn moves the cursor to the first row of session id.
func cursorOn(t *testing.T, m Model, id string) Model {
	t.Helper()
	for i, r := range m.rows {
		if !r.Header && r.Session.ID == id {
			m.cursor, m.moved = i, true
			return m
		}
	}
	t.Fatalf("no row for %s", id)
	return m
}

func newDialog(t *testing.T, m Model) *newSessionDialog {
	t.Helper()
	d, ok := m.top().(*newSessionDialog)
	if !ok {
		t.Fatalf("top dialog = %#v, want new session", m.top())
	}
	return d
}

func TestNewDefaultsHostFromCursor(t *testing.T) {
	for _, tc := range []struct{ id, want string }{
		{"box/$1", "box"},
		{"$1", ""},
		{"ci/$1", ""},
	} {
		m := cursorOn(t, New(hostsFake(), "c"), tc.id)
		m, _ = press(t, m, "n")
		if d := newDialog(t, m); d.host != tc.want || d.focus != 0 {
			t.Errorf("on %s: host = %q focus = %d, want %q focus 0", tc.id, d.host, d.focus, tc.want)
		}
	}
	f := hostsFake()
	f.sessions = nil
	m, _ := press(t, New(f, "c"), "n")
	if d := newDialog(t, m); d.host != "" {
		t.Errorf("empty list: host = %q", d.host)
	}
	if !strings.Contains(plain(sized(m, 100, 30)), "host  local") {
		t.Errorf("local host not shown as local:\n%s", plain(m))
	}
}

func TestNewFocusKeys(t *testing.T) {
	f := hostsFake()
	m := cursorOn(t, sized(New(f, "c"), 100, 30), "box/$1")
	m, _ = press(t, m, "n", "r")
	d := newDialog(t, m)
	if d.name.Value() != "r" {
		t.Errorf("name = %q", d.name.Value())
	}
	if out := plain(m); !strings.Contains(out, "⏎ create  Tab host  Esc cancel") {
		t.Errorf("name hints missing:\n%s", out)
	}
	m, _ = press(t, m, "tab")
	if out := plain(m); !strings.Contains(out, "⏎ choose  r local  Tab name  Esc") {
		t.Errorf("host hints missing:\n%s", out)
	}
	m, _ = press(t, m, "x", "r")
	d = newDialog(t, m)
	if d.host != "" || d.name.Value() != "r" {
		t.Errorf("host = %q name = %q", d.host, d.name.Value())
	}
	m, _ = press(t, m, "down", "up", "down") // arrows move focus too
	if d.focus != 0 {
		t.Errorf("focus = %d after down/up/down", d.focus)
	}
	if m.Selected().Session.ID != "box/$1" {
		t.Errorf("list moved under the dialog: %+v", m.Selected())
	}
	m, _ = press(t, m, "tab", "tab")
	_, cmd := press(t, m, "enter")
	want := []string{"NewSession  r", "SwitchClient c $10"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
	if !isQuit(cmd) {
		t.Errorf("did not quit")
	}
}

func TestNewEnterCreatesRemote(t *testing.T) {
	f := hostsFake()
	m := cursorOn(t, New(f, "c"), "box/$1")
	m, _ = press(t, m, "n", "x")
	f.calls = nil
	m, cmd := press(t, m, "enter")
	_, cmd = settle(t, f, m, cmd)
	want := []string{"NewSession box x", "SwitchClient c box/$10"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
	if !isQuit(cmd) {
		t.Errorf("did not quit")
	}
}

func TestHostPickerNested(t *testing.T) {
	f := hostsFake()
	m := cursorOn(t, sized(New(f, "c"), 100, 30), "$1")
	m, _ = press(t, m, "n", "tab", "enter")
	if len(m.dialogs) != 2 {
		t.Fatalf("dialogs = %d, want 2", len(m.dialogs))
	}
	out := plain(m)
	for _, s := range []string{"local", "box", "connected", "ci", "unreachable", "Manage hosts…"} {
		if !strings.Contains(out, s) {
			t.Errorf("picker missing %q:\n%s", s, out)
		}
	}
	m = typeText(t, m, "ci")
	p, ok := m.top().(*hostPickerDialog)
	if !ok {
		t.Fatalf("top = %#v", m.top())
	}
	if got := p.items(m); len(got) != 2 || got[0].name != "ci" || !got[1].manage {
		t.Errorf("filtered items = %+v, want ci and Manage hosts…", got)
	}
	m, _ = press(t, m, "enter") // ci is unreachable
	if len(m.dialogs) != 2 {
		t.Fatalf("picked an unreachable host: dialogs = %d", len(m.dialogs))
	}
	m, _ = press(t, m, "esc")
	if len(m.dialogs) != 1 || newDialog(t, m).host != "" {
		t.Fatalf("esc: dialogs = %d host = %q", len(m.dialogs), newDialog(t, m).host)
	}
	m, _ = press(t, m, "space") // reopen; items: local, box, ci, Manage
	m, _ = press(t, m, "down", "enter")
	if len(m.dialogs) != 1 || newDialog(t, m).host != "box" {
		t.Errorf("pick box: dialogs = %d host = %q", len(m.dialogs), newDialog(t, m).host)
	}
	m, _ = press(t, m, "space", "down", "down", "down", "enter")
	if _, ok := m.top().(*hostManagerDialog); !ok || len(m.dialogs) != 3 {
		t.Errorf("manage: dialogs = %d top = %#v", len(m.dialogs), m.top())
	}
}

func TestEscPopsOnlyTop(t *testing.T) {
	f := hostsFake()
	m, _ := press(t, New(f, "c"), "n", "tab", "enter")
	m, cmd := press(t, m, "esc")
	if len(m.dialogs) != 1 || isQuit(cmd) {
		t.Fatalf("dialogs = %d quit = %v", len(m.dialogs), isQuit(cmd))
	}
	m, cmd = press(t, m, "esc")
	if len(m.dialogs) != 0 || isQuit(cmd) {
		t.Fatalf("dialogs = %d quit = %v", len(m.dialogs), isQuit(cmd))
	}
	if _, cmd = press(t, m, "esc"); !isQuit(cmd) {
		t.Errorf("esc with no dialog should quit")
	}
	if len(f.calls) != 0 {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestNewErrorStaysOpen(t *testing.T) {
	f := hostsFake()
	f.errs["NewSession"] = errFake("duplicate session: x")
	m, _ := press(t, sized(New(f, "c"), 100, 30), "n", "x")
	f.calls = nil
	m, cmd := press(t, m, "enter") // on box: runs as a command
	m, cmd = settle(t, f, m, cmd)
	if isQuit(cmd) || len(m.dialogs) != 1 || newDialog(t, m).name.Value() != "x" {
		t.Fatalf("dialogs = %d quit = %v", len(m.dialogs), isQuit(cmd))
	}
	if top := topBox(t, m, "New session"); !strings.Contains(strings.Join(top, "\n"), "duplicate session: x") {
		t.Errorf("error not inside the dialog:\n%s", plain(m))
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }

// box is the cell rectangle of a dialog on screen.
type box struct{ x, y, w, h int }

// findBox locates the box whose top edge starts with "┌ title".
func findBox(t *testing.T, m Model, title string) box {
	t.Helper()
	lines := strings.Split(plain(m), "\n")
	for y, line := range lines {
		i := strings.Index(line, "┌ "+title)
		if i < 0 {
			continue
		}
		x := ansi.StringWidth(line[:i])
		rest := line[i:]
		w := ansi.StringWidth(rest[:strings.Index(rest, "┐")]) + 1
		for y2 := y + 1; y2 < len(lines); y2++ {
			if strings.HasPrefix(ansi.Cut(lines[y2], x, x+1), "└") {
				return box{x, y, w, y2 - y + 1}
			}
		}
	}
	t.Fatalf("no %q box:\n%s", title, plain(m))
	return box{}
}

// topBox returns the plain lines inside the dialog titled title.
func topBox(t *testing.T, m Model, title string) []string {
	t.Helper()
	b := findBox(t, m, title)
	lines := strings.Split(plain(m), "\n")
	var out []string
	for y := b.y; y < b.y+b.h; y++ {
		out = append(out, ansi.Cut(lines[y], b.x, b.x+b.w))
	}
	return out
}

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}

func TestDimUnderTopDialog(t *testing.T) {
	f := hostsFake()
	m := tickPreview(t, sized(New(f, "c"), 100, 30))
	m, _ = press(t, m, "n")
	check := func(top string, under ...string) {
		t.Helper()
		b := findBox(t, m, top)
		c := lipgloss.NewCanvas(m.width, m.height)
		c.Compose(lipgloss.NewLayer(m.render()))
		dim := lipgloss.Color("8")
		bright := false
		for y := range m.height {
			for x := range m.width {
				cell := c.CellAt(x, y)
				if cell == nil {
					continue
				}
				in := x >= b.x && x < b.x+b.w && y >= b.y && y < b.y+b.h
				st := cell.Style
				if in {
					if st.Fg != nil && !sameColor(st.Fg, dim) {
						bright = true
					}
					continue
				}
				if strings.TrimSpace(cell.Content) == "" {
					continue
				}
				if !sameColor(st.Fg, dim) || st.Bg != nil || st.Attrs != 0 {
					t.Fatalf("cell (%d,%d) %q outside %q has style %+v, want dim only", x, y, cell.Content, top, st)
				}
			}
		}
		if !bright {
			t.Errorf("top dialog %q has no full-brightness color", top)
		}
		out := plain(m)
		for _, s := range under {
			if !strings.Contains(out, s) {
				t.Errorf("%q not visible under the dialog:\n%s", s, out)
			}
		}
	}
	check("New session", "RECENT", "preview text", "loc")
	m, _ = press(t, m, "tab", "enter")
	check("Host", "┌ New session", "RECENT")
}

func TestDialogFitsSmallScreen(t *testing.T) {
	m, _ := press(t, sized(New(hostsFake(), "c"), 30, 6), "n", "tab", "enter")
	out := plain(m)
	for line := range strings.SplitSeq(out, "\n") {
		if w := ansi.StringWidth(line); w > 30 {
			t.Errorf("line width %d > 30: %q", w, line)
		}
	}
	if n := strings.Count(out, "\n") + 1; n > 6 {
		t.Errorf("%d lines > 6", n)
	}
}

func TestNewLongNameScrolls(t *testing.T) {
	m, _ := press(t, sized(New(newFake(), "c"), 60, 20), "n")
	m = typeText(t, m, strings.Repeat("a", 90)+"tail")
	top := strings.Join(topBox(t, m, "New session"), "\n")
	if !strings.Contains(top, "tail") {
		t.Errorf("end of the name not visible:\n%s", top)
	}
}
