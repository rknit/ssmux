package app

import (
	"errors"
	"reflect"
	"runtime/debug"
	"testing"

	"github.com/rknit/ssmux/internal/fleet"
	"github.com/rknit/ssmux/internal/testutil"
	"github.com/rknit/ssmux/internal/tmux"
)

type fakeSwitcher struct {
	sessions  []tmux.Session
	switchErr error
	calls     []string
}

func (f *fakeSwitcher) ListSessions() ([]tmux.Session, error) { return f.sessions, nil }

func (f *fakeSwitcher) SwitchClient(client, qid string) error {
	f.calls = append(f.calls, "SwitchClient "+client+" "+qid)
	return f.switchErr
}

func (f *fakeSwitcher) Display(client, msg string) error {
	f.calls = append(f.calls, "Display "+client+" "+msg)
	return nil
}

func TestSwitchEmptySlot(t *testing.T) {
	f := &fakeSwitcher{sessions: []tmux.Session{{ID: "$1", Slot: 2}}}
	if err := Switch(f, "c", 7); err != nil {
		t.Errorf("empty slot: %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("empty slot made calls: %v", f.calls)
	}
}

func TestSwitchFindsSlot(t *testing.T) {
	f := &fakeSwitcher{sessions: []tmux.Session{{ID: "$1"}, {ID: "$2", Slot: 3}}}
	if err := Switch(f, "c", 3); err != nil {
		t.Fatal(err)
	}
	if want := []string{"SwitchClient c $2"}; !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
}

func TestSwitchRemotePin(t *testing.T) {
	f := &fakeSwitcher{sessions: []tmux.Session{{ID: "box/$3", Slot: 4, Status: "connecting"}}}
	if err := Switch(f, "c", 4); err != nil {
		t.Fatal(err)
	}
	want := []string{"SwitchClient c box/$3"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
}

func TestSwitchUnreachableDisplays(t *testing.T) {
	msg := "ssh: connect to host box port 22: Operation timed out"
	f := &fakeSwitcher{
		sessions:  []tmux.Session{{ID: "box/$3", Slot: 4}},
		switchErr: errors.New(msg),
	}
	if err := Switch(f, "c", 4); err != nil {
		t.Fatal(err)
	}
	want := []string{"SwitchClient c box/$3", "Display c ssmux: " + msg}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
}

func TestMostRecent(t *testing.T) {
	ss := []tmux.Session{{ID: "a", LastAttached: 5}, {ID: "b", LastAttached: 9}, {ID: "c"}}
	if s, ok := MostRecent(ss); !ok || s.ID != "b" {
		t.Errorf("got %v %v", s, ok)
	}
	if _, ok := MostRecent(nil); ok {
		t.Errorf("empty input returned ok")
	}
}

func TestMainUnknownArgs(t *testing.T) {
	if code := Main([]string{"bogus"}); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if code := Main([]string{"switch", "x"}); code != 2 {
		t.Errorf("bad slot exit = %d, want 2", code)
	}
	if code := Main([]string{"version", "x"}); code != 2 {
		t.Errorf("version with args exit = %d, want 2", code)
	}
}

type fakeAttacher struct {
	last    fleet.Entry
	ok      bool
	wrapErr error
}

func (f *fakeAttacher) LastRemote() (fleet.Entry, bool) { return f.last, f.ok }

func (f *fakeAttacher) Wrap(e fleet.Entry) (string, error) {
	if f.wrapErr != nil {
		return "", f.wrapErr
	}
	return "wrap:" + e.ID, nil
}

func TestAttachTargetRemote(t *testing.T) {
	c := testutil.NewServer(t)
	ss, err := c.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	base := ss[0]
	tests := []struct {
		name string
		r    *fakeAttacher
		want string
	}{
		{"remote newer", &fakeAttacher{last: fleet.Entry{Host: "box", ID: "$1", Value: base.LastAttached + 1}, ok: true}, "wrap:$1"},
		{"local newer", &fakeAttacher{last: fleet.Entry{Host: "box", ID: "$1", Value: base.LastAttached - 1}, ok: true}, base.ID},
		{"remote gone", &fakeAttacher{last: fleet.Entry{Host: "box", ID: "$1", Value: base.LastAttached + 1}, ok: true, wrapErr: errors.New("gone")}, base.ID},
		{"no remote", &fakeAttacher{}, base.ID},
	}
	for _, tt := range tests {
		got, err := attachTarget(c, tt.r)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("%s: attachTarget = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestAttachTargetSkipsWrappers(t *testing.T) {
	c := testutil.NewServer(t)
	// "a-wrap" sorts before "base", so it would be picked on a tie.
	w, err := c.NewSession("a-wrap", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetSessionOption(w, fleet.RemoteOption, "box,$1"); err != nil {
		t.Fatal(err)
	}
	got, err := attachTarget(c, &fakeAttacher{})
	if err != nil {
		t.Fatal(err)
	}
	if got == w {
		t.Fatalf("attachTarget picked wrapper %s", w)
	}

	// With only wrappers left, a new session is created.
	ss, err := c.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range ss {
		if s.ID != w {
			if err := c.KillSession(s.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	got, err = attachTarget(c, &fakeAttacher{})
	if err != nil {
		t.Fatal(err)
	}
	if got == w || got == "" {
		t.Errorf("attachTarget = %q with only wrapper %s", got, w)
	}
}

func TestPaneTerm(t *testing.T) {
	c := testutil.NewServer(t)
	if _, err := c.Run("set-option", "-g", "default-terminal", "tmux-256color"); err != nil {
		t.Fatal(err)
	}
	if got := paneTerm(c, "xterm-ghostty"); got != "tmux-256color" {
		t.Errorf("paneTerm = %q, want tmux-256color", got)
	}
	none := tmux.Client{Socket: c.Socket + "-none"}
	if got := paneTerm(none, "xterm-ghostty"); got != "xterm-ghostty" {
		t.Errorf("paneTerm without server = %q, want xterm-ghostty", got)
	}
}

func TestVersionString(t *testing.T) {
	// the nix build sets Version through ldflags for tests too
	origVersion := Version
	t.Cleanup(func() { Version = origVersion })

	tests := []struct {
		name    string
		version string
		info    *debug.BuildInfo
		want    string
	}{
		{"build info", "", &debug.BuildInfo{Main: debug.Module{Version: "v0.9.0"}}, "ssmux v0.9.0"},
		{"devel with vcs", "", &debug.BuildInfo{
			Main:     debug.Module{Version: "(devel)"},
			Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc1234def5678"}},
		}, "ssmux dev-abc1234"},
		{"devel with dirty vcs", "", &debug.BuildInfo{
			Main: debug.Module{Version: "(devel)"},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abc1234def5678"},
				{Key: "vcs.modified", Value: "true"},
			},
		}, "ssmux dev-abc1234-dirty"},
		{"devel without vcs", "", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "ssmux dev"},
		{"pseudo-version", "", &debug.BuildInfo{Main: debug.Module{Version: "v1.1.1-0.20261028000000-abcdef123456"}}, "ssmux dev-abcdef1"},
		{"no build info", "", nil, "ssmux dev"},
		{"ldflags without v", "1.2.3", &debug.BuildInfo{Main: debug.Module{Version: "v0.9.0"}}, "ssmux v1.2.3"},
		{"ldflags with v", "v1.2.3", nil, "ssmux v1.2.3"},
		{"ldflags malformed", "1.2", nil, "ssmux dev"},
	}
	for _, tt := range tests {
		Version = tt.version
		if got := versionString(tt.info, tt.info != nil); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}
