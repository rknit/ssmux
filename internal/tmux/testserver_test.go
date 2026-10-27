package tmux

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// newTestServer starts a private tmux server with one session named "base".
// The socket lives under /tmp because macOS limits unix socket paths to 104 bytes.
func newTestServer(t *testing.T) Client {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	dir, err := os.MkdirTemp("/tmp", "ssmux")
	if err != nil {
		t.Fatal(err)
	}
	c := Client{Socket: filepath.Join(dir, "sock")}
	conf := filepath.Join(dir, "tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g default-shell /bin/sh\nset -g remain-on-exit on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("tmux", "-S", c.Socket, "-f", conf,
		"new-session", "-d", "-s", "base", "-c", dir).CombinedOutput(); err != nil {
		t.Fatalf("start tmux: %v: %s", err, out)
	}
	t.Cleanup(func() {
		exec.Command("tmux", "-S", c.Socket, "kill-server").Run()
		os.RemoveAll(dir)
	})
	return c
}
