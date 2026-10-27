package tmux

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureSSHDirCreates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cm")
	if err := ensureSSHDir(dir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
}

func TestEnsureSSHDirTightensMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cm")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureSSHDir(dir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("mode = %v, want 0700", fi.Mode().Perm())
	}
}

func TestEnsureSSHDirRefusesSymlink(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "elsewhere")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(tmp, "cm")
	if err := os.Symlink(target, dir); err != nil {
		t.Fatal(err)
	}
	if err := ensureSSHDir(dir); err == nil {
		t.Error("symlink accepted")
	}
}
