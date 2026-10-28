// Package app implements ssmux's command-line entry points.
package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/rknit/ssmux/internal/fleet"
	"github.com/rknit/ssmux/internal/hosts"
	"github.com/rknit/ssmux/internal/tmux"
)

const usage = `usage:
  ssmux                         attach to the last session, or open the popup inside tmux
  ssmux open   [--client C]     open the popup on client C
  ssmux popup  [--client C]     run the session picker (used inside the popup)
  ssmux switch N [--client C]   switch client C to the session pinned on slot N
  ssmux version                 print the version`

// RunPopup runs the picker UI. It is set by main to avoid an import cycle in tests.
var RunPopup func(f *fleet.Fleet, client string) error

type slotSwitcher interface {
	ListSessions() ([]tmux.Session, error)
	SwitchClient(client, qid string) error
	Display(client, msg string) error
}

// Switch moves client to the session on slot. An empty slot is a silent no-op.
// If a remote pin's host is unreachable or the session is gone, the reason
// is shown on the client and Switch returns nil.
func Switch(f slotSwitcher, client string, slot int) error {
	ss, err := f.ListSessions()
	if err != nil {
		return err
	}
	for _, s := range ss {
		if s.Slot != slot {
			continue
		}
		err := f.SwitchClient(client, s.ID)
		if host, _ := fleet.Split(s.ID); host != "" && err != nil {
			return f.Display(client, "ssmux: "+err.Error())
		}
		return err
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

// remoteAttacher reattaches a remote session through its wrapper.
type remoteAttacher interface {
	LastRemote() (fleet.Entry, bool)
	Wrap(e fleet.Entry) (string, error)
}

// attachTarget returns the session plain ssmux attaches to: the most recent
// one that is not a hidden remote wrapper, or the wrapper of the remote
// session used last if that is more recent and still reachable, or a new one
// in the home directory when there is none.
func attachTarget(c tmux.Client, r remoteAttacher) (string, error) {
	if c.HasServer() {
		ss, err := c.ListSessions()
		if err != nil {
			return "", err
		}
		wrappers, err := c.SessionTags(fleet.RemoteOption)
		if err != nil {
			return "", err
		}
		ss = slices.DeleteFunc(ss, func(s tmux.Session) bool { return wrappers[s.ID] != "" })
		s, ok := MostRecent(ss)
		if e, rok := r.LastRemote(); rok && (!ok || e.Value > s.LastAttached) {
			if w, err := r.Wrap(e); err == nil {
				return w, nil
			}
		}
		if ok {
			return s.ID, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return c.NewSession("", home)
}

// paneTerm returns the TERM of a pane on c's server, which is what a
// wrapper's ssh writes to, or fallback if the server has none to report.
// Outside tmux, TERM names the outer terminal instead.
func paneTerm(c tmux.Client, fallback string) string {
	if t, err := c.GlobalOption("default-terminal"); err == nil && t != "" {
		return t
	}
	return fallback
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

func newFleet(c tmux.Client) (*fleet.Fleet, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	remote := func(h string) tmux.Client {
		return tmux.Client{Host: h, Timeout: 1500 * time.Millisecond}
	}
	return fleet.New(c, hosts.Path(), filepath.Join(home, ".ssh"), remote), nil
}

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
		f, err := newFleet(c)
		if err != nil {
			return err
		}
		return RunPopup(f, client)
	case "switch":
		if len(pos) != 1 {
			return errUsage
		}
		n, err := strconv.Atoi(pos[0])
		if err != nil || n < 1 || n > 9 {
			return errUsage
		}
		f, err := newFleet(c)
		if err != nil {
			return err
		}
		return Switch(f, client, n)
	case "version":
		if len(args) != 1 {
			return errUsage
		}
		fmt.Println(versionString(debug.ReadBuildInfo()))
		return nil
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

	f, err := newFleet(c)
	if err != nil {
		return err
	}
	f.Term = paneTerm(c, f.Term)
	target, err := attachTarget(c, f)
	if err != nil {
		return err
	}
	if err := c.BindKeys(exe); err != nil {
		return err
	}
	bin, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	return syscall.Exec(bin, []string{"tmux", "attach-session", "-t", target}, os.Environ())
}
