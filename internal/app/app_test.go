package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rknit/ssmux/internal/tmux"
)

func newServer(t *testing.T) tmux.Client {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	dir, err := os.MkdirTemp("/tmp", "ssmux")
	if err != nil {
		t.Fatal(err)
	}
	c := tmux.Client{Socket: filepath.Join(dir, "sock")}
	conf := filepath.Join(dir, "tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g default-shell /bin/sh\nset -g remain-on-exit on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("tmux", "-S", c.Socket, "-f", conf,
		"new-session", "-d", "-s", "base").CombinedOutput(); err != nil {
		t.Fatalf("start tmux: %v: %s", err, out)
	}
	t.Cleanup(func() {
		exec.Command("tmux", "-S", c.Socket, "kill-server").Run()
		os.RemoveAll(dir)
	})
	return c
}

func TestSwitchEmptySlot(t *testing.T) {
	c := newServer(t)
	if err := Switch(c, "", 7); err != nil {
		t.Errorf("empty slot: %v", err)
	}
}

type recorder struct {
	tmux.Client
	switched []string
}

func (r *recorder) SwitchClient(client, id string) error {
	r.switched = append(r.switched, client+" "+id)
	return nil
}

func TestSwitchFindsSlot(t *testing.T) {
	r := &recorder{Client: newServer(t)}
	id, err := r.NewSession("target", "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetSlot(id, 3); err != nil {
		t.Fatal(err)
	}
	if err := Switch(r, "c", 3); err != nil {
		t.Fatal(err)
	}
	if want := []string{"c " + id}; !reflect.DeepEqual(r.switched, want) {
		t.Errorf("switched = %v, want %v", r.switched, want)
	}
	r.switched = nil
	Switch(r, "c", 4)
	if len(r.switched) != 0 {
		t.Errorf("empty slot switched: %v", r.switched)
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
