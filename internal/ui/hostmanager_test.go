package ui

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rknit/ssmux/internal/fleet"
)

// managerFake has one host in each state and two ~/.ssh/config suggestions.
func managerFake() *fakeTmux {
	f := newFake()
	f.hosts = []fleet.HostStatus{
		{Name: "devbox", State: fleet.Connected},
		{Name: "ci", State: fleet.Unreachable, Err: "ssh: timeout"},
		{Name: "old", State: fleet.Connected, TermFallback: true},
		{Name: "new", State: fleet.Connecting},
	}
	f.suggest = []string{"staging", "prod"}
	return f
}

func hostManager(t *testing.T, m Model) *hostManagerDialog {
	t.Helper()
	d, ok := m.top().(*hostManagerDialog)
	if !ok {
		t.Fatalf("top dialog = %#v, want host manager", m.top())
	}
	return d
}

// hostMsgs runs cmd and returns the hostMsgs it produces.
func hostMsgs(cmd tea.Cmd) []hostMsg {
	var out []hostMsg
	for _, msg := range msgs(cmd) {
		if h, ok := msg.(hostMsg); ok {
			out = append(out, h)
		}
	}
	return out
}

func TestHostManagerRows(t *testing.T) {
	m, _ := press(t, sized(New(managerFake(), "c"), 100, 30), "h")
	hostManager(t, m)
	box := strings.Join(topBox(t, m, "Hosts"), "\n")
	for _, s := range []string{
		"│ HOSTS",
		"devbox   connected",
		"ci       unreachable: ssh: timeout",
		"old      connected, TERM fallback",
		"new      connecting",
		"│ FROM ~/.ssh/config",
		"  staging",
		"  prod",
		"a address  d remove  n new  Esc",
	} {
		if !strings.Contains(box, s) {
			t.Errorf("host manager missing %q:\n%s", s, box)
		}
	}
	if strings.Index(box, "devbox") > strings.Index(box, "FROM") || strings.Index(box, "FROM") > strings.Index(box, "staging") {
		t.Errorf("sections out of order:\n%s", box)
	}
}

func TestAddSuggestion(t *testing.T) {
	f := managerFake()
	m, _ := press(t, sized(New(f, "c"), 100, 30), "h")
	m, _ = press(t, m, "enter") // on devbox: a configured host, nothing to add
	if f.called("AddHost") {
		t.Fatalf("⏎ on a host added it: %v", f.calls)
	}
	m, cmd := press(t, m, "down", "down", "down", "down", "enter") // staging
	if !slices.Contains(f.calls, "AddHost staging staging") {
		t.Fatalf("calls = %v, want AddHost staging", f.calls)
	}
	box := strings.Join(topBox(t, m, "Hosts"), "\n")
	if !regexp.MustCompile(`staging +connecting`).MatchString(box) {
		t.Errorf("staging not listed as connecting:\n%s", box)
	}
	if strings.Contains(box[strings.Index(box, "FROM"):], "staging") {
		t.Errorf("staging still suggested:\n%s", box)
	}
	if got := hostMsgs(cmd); len(got) != 1 || got[0].host != "staging" || !slices.Contains(f.calls, "RefreshHost staging") {
		t.Errorf("refresh msgs = %+v calls = %v, want a staging refresh", got, f.calls)
	}
}

func TestAddTyped(t *testing.T) {
	f := managerFake()
	f.errs["AddHost"] = errFake("bad destination")
	m, _ := press(t, sized(New(f, "c"), 100, 30), "h", "n")
	if _, ok := m.top().(*inputDialog); !ok {
		t.Fatalf("top = %#v, want input dialog", m.top())
	}
	m = typeText(t, m, "u@h")
	m, cmd := press(t, m, "enter")
	if !slices.Contains(f.calls, "AddHost u@h u@h") {
		t.Fatalf("calls = %v", f.calls)
	}
	if _, ok := m.top().(*inputDialog); !ok || len(m.dialogs) != 2 || cmd != nil {
		t.Fatalf("error closed the input: dialogs = %d top = %#v", len(m.dialogs), m.top())
	}
	if box := strings.Join(topBox(t, m, "Add host"), "\n"); !strings.Contains(box, "bad destination") {
		t.Errorf("error not in the input dialog:\n%s", box)
	}
	delete(f.errs, "AddHost")
	m, cmd = press(t, m, "enter")
	hostManager(t, m)
	if len(m.dialogs) != 1 {
		t.Fatalf("dialogs = %d after add", len(m.dialogs))
	}
	if !regexp.MustCompile(`u@h +connecting`).MatchString(plain(m)) {
		t.Errorf("u@h not listed:\n%s", plain(m))
	}
	if got := hostMsgs(cmd); len(got) != 1 || got[0].host != "u@h" {
		t.Errorf("refresh msgs = %+v, want u@h", got)
	}
}

func TestAddTypedWithDests(t *testing.T) {
	f := managerFake()
	m, _ := press(t, sized(New(f, "c"), 100, 30), "h", "n")
	m = typeText(t, m, "home 10.0.0.5 home.example.com")
	m, _ = press(t, m, "enter")
	if !slices.Contains(f.calls, "AddHost home 10.0.0.5 home.example.com") {
		t.Fatalf("calls = %v", f.calls)
	}
	hostManager(t, m)
}

// destFake has home with two addresses, connected through the second.
func destFake() *fakeTmux {
	f := newFake()
	f.hosts = []fleet.HostStatus{
		{Name: "home", Dests: []string{"home", "home.vpn"}, Dest: "home.vpn", State: fleet.Connected},
		{Name: "box", Dests: []string{"10.0.0.5"}, Dest: "10.0.0.5", State: fleet.Unreachable},
	}
	f.suggest = []string{"staging"}
	return f
}

func TestAddressRows(t *testing.T) {
	m, _ := press(t, sized(New(destFake(), "c"), 100, 30), "h", "down")
	box := strings.Join(topBox(t, m, "Hosts"), "\n")
	for _, re := range []string{
		`home +connected`, `▸ ○ home\b`, `  ● home\.vpn`, `box +unreachable`, `  ○ 10\.0\.0\.5`,
		`a address  d remove  J/K order  n new  Esc`,
	} {
		if !regexp.MustCompile(re).MatchString(box) {
			t.Errorf("missing %q:\n%s", re, box)
		}
	}
	m, _ = press(t, m, "down", "down", "down", "down") // staging
	if box := strings.Join(topBox(t, m, "Hosts"), "\n"); !strings.Contains(box, "⏎ add  m merge  n new  Esc") {
		t.Errorf("suggestion hints:\n%s", box)
	}
}

func TestAddAddress(t *testing.T) {
	f := destFake()
	m, _ := press(t, sized(New(f, "c"), 100, 30), "h", "down", "down", "down", "a") // on box
	if box := strings.Join(topBox(t, m, "Add address to box"), "\n"); !strings.Contains(box, "⏎ add") {
		t.Errorf("add box:\n%s", box)
	}
	m = typeText(t, m, "box.vpn")
	m, cmd := press(t, m, "enter")
	if !slices.Contains(f.calls, "SetDests box 10.0.0.5 box.vpn") {
		t.Fatalf("calls = %v", f.calls)
	}
	d := hostManager(t, m)
	if got := hostMsgs(cmd); len(got) != 1 || got[0].host != "box" {
		t.Errorf("refresh msgs = %+v, want box", got)
	}
	if it, _ := d.selected(d.items(m)); it.host.Name != "box" || it.dest != 1 {
		t.Errorf("cursor on %+v, want box.vpn", it)
	}
}

func TestRemoveAndOrderAddress(t *testing.T) {
	f := destFake()
	m, _ := press(t, sized(New(f, "c"), 100, 30), "h", "down", "J")
	if !slices.Contains(f.calls, "SetDests home home.vpn home") {
		t.Fatalf("calls = %v", f.calls)
	}
	d := hostManager(t, m)
	if it, _ := d.selected(d.items(m)); it.dest != 1 {
		t.Errorf("cursor did not follow the moved address: %+v", it)
	}
	m, _ = press(t, m, "J") // already last
	if f.called("SetDests home home home.vpn") {
		t.Errorf("J past the end moved: %v", f.calls)
	}
	m, _ = press(t, m, "d")
	if !slices.Contains(f.calls, "SetDests home home.vpn") {
		t.Fatalf("calls = %v", f.calls)
	}
	m, _ = press(t, m, "d") // last address
	if box := strings.Join(topBox(t, m, "Hosts"), "\n"); !strings.Contains(box, "a host needs an address") {
		t.Errorf("last address removed:\n%s", box)
	}
	if _, ok := m.top().(*confirmDialog); ok {
		t.Errorf("d on an address asked to remove the host")
	}
}

func TestMergeSuggestion(t *testing.T) {
	f := destFake()
	m, _ := press(t, sized(New(f, "c"), 100, 30), "h", "down", "down", "down", "down", "down", "m")
	if _, ok := m.top().(*hostPickerDialog); !ok {
		t.Fatalf("top = %#v, want host picker", m.top())
	}
	box := strings.Join(topBox(t, m, "Merge staging into"), "\n")
	if strings.Contains(box, "local") || strings.Contains(box, "Manage") {
		t.Errorf("merge picker lists more than hosts:\n%s", box)
	}
	m, cmd := press(t, m, "down", "enter") // box, though unreachable
	if !slices.Contains(f.calls, "SetDests box 10.0.0.5 staging") {
		t.Fatalf("calls = %v", f.calls)
	}
	d := hostManager(t, m)
	if got := hostMsgs(cmd); len(got) != 1 || got[0].host != "box" {
		t.Errorf("refresh msgs = %+v, want box", got)
	}
	if it, _ := d.selected(d.items(m)); it.host.Name != "box" || it.dest != 1 {
		t.Errorf("cursor on %+v, want box's staging", it)
	}
}

func TestRemoveConfirm(t *testing.T) {
	f := managerFake()
	m, _ := press(t, sized(New(f, "c"), 100, 30), "h", "down", "d")
	if _, ok := m.top().(*confirmDialog); !ok {
		t.Fatalf("top = %#v, want confirm", m.top())
	}
	if box := strings.Join(topBox(t, m, "Remove host"), "\n"); !strings.Contains(box, "ci") || !strings.Contains(box, "y yes") {
		t.Errorf("confirm box:\n%s", box)
	}
	m, _ = press(t, m, "n")
	hostManager(t, m)
	if f.called("RemoveHost") {
		t.Fatalf("n removed: %v", f.calls)
	}
	m, _ = press(t, m, "d", "y")
	hostManager(t, m)
	if !slices.Contains(f.calls, "RemoveHost ci") {
		t.Fatalf("calls = %v", f.calls)
	}
	if box := strings.Join(topBox(t, m, "Hosts"), "\n"); strings.Contains(box, "ci ") {
		t.Errorf("ci still listed:\n%s", box)
	}
	m, _ = press(t, m, "down", "down", "down", "d") // on a suggestion: nothing to remove
	hostManager(t, m)

	f.errs["RemoveHost"] = errFake("kill failed")
	m, _ = press(t, m, "up", "up", "d", "y") // prod → staging → new
	if box := strings.Join(topBox(t, m, "Hosts"), "\n"); !strings.Contains(box, "kill failed") {
		t.Errorf("remove error not in the manager:\n%s", box)
	}
}

func TestManageFromPicker(t *testing.T) {
	f := hostsFake()
	f.suggest = []string{"staging"}
	m, _ := press(t, sized(New(f, "c"), 100, 30), "n", "tab", "enter")
	m, _ = press(t, m, "down", "down", "down", "enter") // local, box, ci, Manage hosts…
	hostManager(t, m)
	if len(m.dialogs) != 3 {
		t.Fatalf("dialogs = %d, want 3", len(m.dialogs))
	}
	m, _ = press(t, m, "down", "down", "enter", "esc") // box, ci, staging
	if _, ok := m.top().(*hostPickerDialog); !ok {
		t.Fatalf("top = %#v, want host picker", m.top())
	}
	if box := strings.Join(topBox(t, m, "Host"), "\n"); !strings.Contains(box, "staging") {
		t.Errorf("picker misses the added host:\n%s", box)
	}
}

func TestFooterHasHosts(t *testing.T) {
	if out := plain(sized(New(newFake(), "c"), 100, 30)); !strings.Contains(out, "h hosts") {
		t.Errorf("footer missing h hosts:\n%s", out)
	}
}

// A host removed and added again while its old refresh is pending must end
// up with one refresh loop, not two.
func TestReaddedHostHasOneLoop(t *testing.T) {
	f := hostsFake()
	m := sized(New(f, "c"), 100, 30)
	var old hostMsg
	for _, h := range hostMsgs(m.Init()) {
		if h.host == "ci" {
			old = h
		}
	}
	if old.host != "ci" {
		t.Fatalf("no ci refresh from Init")
	}
	m, _ = press(t, m, "h", "down", "d", "y")
	m, _ = press(t, m, "n")
	m = typeText(t, m, "ci")
	m, cmd := press(t, m, "enter")
	fresh := hostMsgs(cmd)
	if len(fresh) != 1 || fresh[0].host != "ci" {
		t.Fatalf("re-add refresh = %+v", fresh)
	}
	if _, next := update(m, old); next != nil {
		t.Errorf("stale loop kept running")
	}
	if _, next := update(m, fresh[0]); next == nil {
		t.Errorf("new loop stopped")
	}
}
