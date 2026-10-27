// Package testutil provides tmux servers and a fake ssh for tests.
package testutil

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rknit/ssmux/internal/tmux"
)

//go:embed fakessh.sh
var fakeSSHScript string

const testConf = "set -g default-shell /bin/sh\nset -g remain-on-exit on\n"

// tempDir returns a directory under /tmp because macOS limits unix socket
// paths to 104 bytes.
func tempDir(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	dir, err := os.MkdirTemp("/tmp", "ssmux")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// start launches a tmux server on sock with one session named "base".
func start(t *testing.T, sock, conf, dir string) {
	t.Helper()
	if out, err := exec.Command("tmux", "-S", sock, "-f", conf,
		"new-session", "-d", "-s", "base", "-c", dir).CombinedOutput(); err != nil {
		t.Fatalf("start tmux: %v: %s", err, out)
	}
	t.Cleanup(func() { exec.Command("tmux", "-S", sock, "kill-server").Run() })
}

// NewServer starts a private tmux server with one session named "base".
func NewServer(t *testing.T) tmux.Client {
	t.Helper()
	dir := tempDir(t)
	conf := filepath.Join(dir, "tmux.conf")
	writeFile(t, conf, testConf, 0o644)
	c := tmux.Client{Socket: filepath.Join(dir, "sock")}
	start(t, c.Socket, conf, dir)
	return c
}

// FakeSSH returns a fake ssh binary and a constructor for clients routed
// through it. remote(h) lazily starts one tmux server per host h.
func FakeSSH(t *testing.T) (ssh string, remote func(host string) tmux.Client) {
	t.Helper()
	dir := tempDir(t)
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(dir, "tmux.conf")
	writeFile(t, conf, testConf, 0o644)
	real, err := exec.LookPath("tmux")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bin, "tmux"),
		"#!/bin/sh\nexec "+real+" -S \""+dir+"/$FAKESSH_DEST.sock\" -f "+conf+" \"$@\"\n", 0o755)
	ssh = filepath.Join(dir, "ssh")
	writeFile(t, ssh, fakeSSHScript, 0o755)
	t.Setenv("FAKESSH_BIN", bin)
	started := map[string]bool{}
	return ssh, func(host string) tmux.Client {
		if !started[host] {
			started[host] = true
			start(t, filepath.Join(dir, host+".sock"), conf, dir)
		}
		return tmux.Client{Host: host, SSH: ssh}
	}
}
