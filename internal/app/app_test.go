package app

import (
	"errors"
	"reflect"
	"testing"

	"github.com/rknit/ssmux/internal/tmux"
)

type fakeSwitcher struct {
	sessions   []tmux.Session
	resolveErr error
	calls      []string
}

func (f *fakeSwitcher) ListSessions() ([]tmux.Session, error) { return f.sessions, nil }

func (f *fakeSwitcher) Resolve(qid string) (tmux.Session, error) {
	f.calls = append(f.calls, "Resolve "+qid)
	return tmux.Session{ID: qid}, f.resolveErr
}

func (f *fakeSwitcher) SwitchClient(client, qid string) error {
	f.calls = append(f.calls, "SwitchClient "+client+" "+qid)
	return nil
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
	want := []string{"Resolve box/$3", "SwitchClient c box/$3"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
}

func TestSwitchUnreachableDisplays(t *testing.T) {
	msg := "ssh: connect to host box port 22: Operation timed out"
	f := &fakeSwitcher{
		sessions:   []tmux.Session{{ID: "box/$3", Slot: 4}},
		resolveErr: errors.New(msg),
	}
	if err := Switch(f, "c", 4); err != nil {
		t.Fatal(err)
	}
	want := []string{"Resolve box/$3", "Display c ssmux: " + msg}
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
}
