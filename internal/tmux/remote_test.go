package tmux_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rknit/ssmux/internal/testutil"
	"github.com/rknit/ssmux/internal/tmux"
)

func TestRemoteQuoting(t *testing.T) {
	_, remote := testutil.FakeSSH(t)
	c := remote("box")
	const name = "it's $x; a b"
	id, err := c.NewSession(name, "")
	if err != nil {
		t.Fatal(err)
	}
	has := func(want string) bool {
		ss, err := c.ListSessions()
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range ss {
			if s.ID == id && s.Name == want {
				return true
			}
		}
		return false
	}
	if !has(name) {
		t.Errorf("session %q not listed", name)
	}
	if err := c.RenameSession(id, `q"z`); err != nil {
		t.Fatal(err)
	}
	if !has(`q"z`) {
		t.Error("renamed session not listed")
	}
}

func TestRemoteTimeout(t *testing.T) {
	ssh := filepath.Join(t.TempDir(), "slow")
	// A live master, so Timeout applies rather than the cold-connect limit.
	script := "#!/bin/sh\nfor a; do [ \"$a\" = -O ] && exit 0; done\nexec sleep 5\n"
	if err := os.WriteFile(ssh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := tmux.Client{Host: "h", SSH: ssh, Timeout: 100 * time.Millisecond}
	start := time.Now()
	_, err := c.ListSessions()
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %v, want timed out", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v", d)
	}
}

// A first ssh call that forks a ControlMaster leaves a background process
// holding the inherited stdout and stderr; the call must still return.
func TestRemoteForkedMaster(t *testing.T) {
	ssh := filepath.Join(t.TempDir(), "forking")
	script := "#!/bin/sh\necho out\necho warn >&2\n(sleep 5) &\n"
	if err := os.WriteFile(ssh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := tmux.Client{Host: "h", SSH: ssh, Timeout: 3 * time.Second}
	out, err := c.Exec("true")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out != "out" {
		t.Errorf("out = %q", out)
	}
}

// slowSSH writes a fake ssh whose calls sleep for delay and then leave a
// master behind, and which answers "-O check" by whether one exists.
func slowSSH(t *testing.T, delay string) string {
	t.Helper()
	dir := t.TempDir()
	ssh := filepath.Join(dir, "ssh")
	mark := filepath.Join(dir, "master")
	script := "#!/bin/sh\n" +
		"for a; do if [ \"$a\" = -O ]; then [ -e " + mark + " ] && exit 0; exit 255; fi; done\n" +
		"sleep " + delay + "\ntouch " + mark + "\necho ok\n"
	if err := os.WriteFile(ssh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return ssh
}

// A first call has to log in, which may take longer than Timeout; once the
// master is up, Timeout applies again.
func TestRemoteColdConnectTimeout(t *testing.T) {
	c := tmux.Client{Host: "h", SSH: slowSSH(t, "2"), Timeout: 1500 * time.Millisecond}
	out, err := c.Exec("true")
	if err != nil || out != "ok" {
		t.Fatalf("cold call: out = %q, err = %v", out, err)
	}
	if _, err := c.Exec("true"); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("warm call: err = %v, want timed out", err)
	}
}

// A hanging "ssh -O check" must not hang the call.
func TestRemoteMasterCheckHangs(t *testing.T) {
	ssh := filepath.Join(t.TempDir(), "ssh")
	script := "#!/bin/sh\nfor a; do [ \"$a\" = -O ] && exec sleep 30; done\necho ok\n"
	if err := os.WriteFile(ssh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := tmux.Client{Host: "h", SSH: ssh, Timeout: 1500 * time.Millisecond}
	start := time.Now()
	out, err := c.Exec("true")
	if err != nil || out != "ok" {
		t.Fatalf("out = %q, err = %v", out, err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("took %v", d)
	}
}
