// Package app implements ssmux's command-line entry points.
package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/rknit/ssmux/internal/tmux"
)

const usage = `usage:
  ssmux                         attach to the last session, or open the popup inside tmux
  ssmux open   [--client C]     open the popup on client C
  ssmux popup  [--client C]     run the session picker (used inside the popup)
  ssmux switch N [--client C]   switch client C to the session pinned on slot N`

// RunPopup runs the picker UI. It is set by main to avoid an import cycle in tests.
var RunPopup func(c tmux.Client, client string) error

type switcher interface {
	ListSessions() ([]tmux.Session, error)
	SwitchClient(client, id string) error
}

// Switch moves client to the session on slot. An empty slot is a silent no-op.
func Switch(c switcher, client string, slot int) error {
	ss, err := c.ListSessions()
	if err != nil {
		return err
	}
	for _, s := range ss {
		if s.Slot == slot {
			return c.SwitchClient(client, s.ID)
		}
	}
	return nil
}

// MostRecent returns the session attached most recently.
func MostRecent(ss []tmux.Session) (tmux.Session, bool) {
	if len(ss) == 0 {
		return tmux.Session{}, false
	}
	best := ss[0]
	for _, s := range ss[1:] {
		if s.LastAttached > best.LastAttached {
			best = s
		}
	}
	return best, true
}

func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// parse splits "[positional] [--client C]".
func parse(args []string) (pos []string, client string, ok bool) {
	for i := 0; i < len(args); i++ {
		if args[i] == "--client" {
			if i+1 >= len(args) {
				return nil, "", false
			}
			client = args[i+1]
			i++
			continue
		}
		pos = append(pos, args[i])
	}
	return pos, client, true
}

// Main dispatches args (without the program name) and returns the exit code.
func Main(args []string) int {
	if err := run(args); err != nil {
		if err == errUsage {
			fmt.Fprintln(os.Stderr, usage)
			return 2
		}
		fmt.Fprintln(os.Stderr, "ssmux:", err)
		return 1
	}
	return 0
}

var errUsage = fmt.Errorf("usage")

func run(args []string) error {
	c := tmux.Client{}
	if len(args) == 0 {
		return attachOrOpen(c)
	}
	pos, client, ok := parse(args[1:])
	if !ok {
		return errUsage
	}
	switch args[0] {
	case "open":
		if len(pos) != 0 {
			return errUsage
		}
		exe, err := executable()
		if err != nil {
			return err
		}
		return c.OpenPopup(client, exe)
	case "popup":
		if len(pos) != 0 || RunPopup == nil {
			return errUsage
		}
		return RunPopup(c, client)
	case "switch":
		if len(pos) != 1 {
			return errUsage
		}
		n, err := strconv.Atoi(pos[0])
		if err != nil || n < 1 || n > 9 {
			return errUsage
		}
		return Switch(c, client, n)
	}
	return errUsage
}

func attachOrOpen(c tmux.Client) error {
	exe, err := executable()
	if err != nil {
		return err
	}
	if os.Getenv("TMUX") != "" {
		if err := c.BindKeys(exe); err != nil {
			return err
		}
		client, err := c.Run("display-message", "-p", "#{client_name}")
		if err != nil {
			return err
		}
		return c.OpenPopup(client, exe)
	}

	var target string
	if !c.HasServer() {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		if target, err = c.NewSession("", home); err != nil {
			return err
		}
	}
	if err := c.BindKeys(exe); err != nil {
		return err
	}
	if target == "" {
		ss, err := c.ListSessions()
		if err != nil {
			return err
		}
		s, _ := MostRecent(ss)
		target = s.ID
	}
	bin, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	return syscall.Exec(bin, []string{"tmux", "attach-session", "-t", target}, os.Environ())
}
