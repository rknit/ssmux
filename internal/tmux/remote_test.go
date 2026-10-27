package tmux_test

import (
	"os"
	"path/filepath"
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
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\nsleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := tmux.Client{Host: "h", SSH: ssh, Timeout: 100 * time.Millisecond}
	start := time.Now()
	if _, err := c.ListSessions(); err == nil {
		t.Error("expected error")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v", d)
	}
}
