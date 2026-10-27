// Package tmux runs the tmux CLI. Sessions are always targeted by ID ("$3")
// so names with spaces, dots or colons never need escaping.
package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const slotOption = "@ssmux-slot"

type Session struct {
	ID           string // "$3"
	Name         string
	Windows      int
	LastAttached int64  // unix seconds; 0 if never attached
	Slot         int    // 0 = unpinned, 1..9
	Host         string // "" = local
	Created      int64  // unix seconds
	Status       string // "", "connecting" or "unreachable"
}

// Client talks to one tmux server, locally or over ssh when Host is set.
// An empty Socket means the default server. An empty SSH means "ssh".
// A zero Timeout means no limit.
type Client struct {
	Socket, Host, SSH string
	Timeout           time.Duration
}

// Run executes tmux with args and returns trimmed stdout.
func (c Client) Run(args ...string) (string, error) {
	name := args[0]
	if c.Host == "" && c.Socket != "" {
		args = append([]string{"-S", c.Socket}, args...)
	}
	var argv []string
	if c.Host == "" {
		argv = append([]string{"tmux"}, args...)
	} else {
		argv = append(c.SSHArgs(false), remoteTmux(args))
	}
	out, err := c.run(argv)
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w", name, err)
	}
	return out, nil
}

// Exec runs a raw shell command on the host (locally when Host is empty).
func (c Client) Exec(cmd string) (string, error) {
	argv := []string{"sh", "-c", cmd}
	if c.Host != "" {
		argv = append(c.SSHArgs(false), cmd)
	}
	out, err := c.run(argv)
	if err != nil {
		return "", fmt.Errorf("exec: %w", err)
	}
	return out, nil
}

// run executes argv and returns trimmed stdout; errors carry stderr.
func (c Client) run(argv []string) (string, error) {
	timeout := c.Timeout
	if c.Host != "" {
		if err := ensureSSHDir(sshDir()); err != nil {
			return "", err
		}
		// Without a master this call has to log in first, which Timeout
		// isn't meant to cover.
		if timeout > 0 && timeout < coldTimeout && !c.masterAlive() {
			timeout = coldTimeout
		}
	}
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if c.Host != "" {
		// ssh may fork a ControlMaster that keeps the inherited stdout and
		// stderr open. With pipes, Wait would wait for it until WaitDelay;
		// files let Wait return as soon as ssh exits.
		outF, err := tempOutput()
		if err != nil {
			return "", err
		}
		defer outF.Close()
		errF, err := tempOutput()
		if err != nil {
			return "", err
		}
		defer errF.Close()
		cmd.Stdout, cmd.Stderr = outF, errF
	}
	err := cmd.Run()
	if f, ok := cmd.Stdout.(*os.File); ok {
		readOutput(f, &stdout)
	}
	if f, ok := cmd.Stderr.(*os.File); ok {
		readOutput(f, &stderr)
	}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			if c.Host != "" {
				return "", fmt.Errorf("ssh %s: timed out", c.Host)
			}
			return "", errors.New("timed out")
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

func (c Client) HasServer() bool {
	_, err := c.Run("list-sessions")
	return err == nil
}

func (c Client) ListSessions() ([]Session, error) {
	// tmux replaces non-printable separators with "_" unless the client
	// locale is UTF-8, and names may contain any printable character, so the
	// name goes last and only the fields before it are split on ":".
	format := strings.Join([]string{
		"#{session_id}", "#{session_windows}", "#{session_last_attached}",
		"#{" + slotOption + "}", "#{session_created}", "#{session_name}",
	}, ":")
	out, err := c.Run("list-sessions", "-F", format)
	if err != nil {
		// A reachable host without a running tmux server has no sessions;
		// creating one starts the server.
		if c.Host != "" && noServer(err) {
			return nil, nil
		}
		return nil, err
	}
	var ss []Session
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.SplitN(line, ":", 6)
		if len(f) != 6 {
			continue
		}
		s := Session{ID: f[0], Name: f[5], Host: c.Host}
		s.Windows, _ = strconv.Atoi(f[1])
		s.LastAttached, _ = strconv.ParseInt(f[2], 10, 64)
		s.Created, _ = strconv.ParseInt(f[4], 10, 64)
		if n, err := strconv.Atoi(f[3]); err == nil && n >= 1 && n <= 9 {
			s.Slot = n
		}
		ss = append(ss, s)
	}
	return ss, nil
}

// noServer reports whether err is tmux finding no server to talk to: none
// running, a missing socket or a stale one. Other socket errors, such as
// permission denied, are real failures.
func noServer(err error) bool {
	msg := err.Error()
	if strings.Contains(msg, "no server running on") {
		return true
	}
	return strings.Contains(msg, "error connecting to") &&
		(strings.Contains(msg, "(No such file or directory)") ||
			strings.Contains(msg, "(Connection refused)"))
}

// SetSlot stores slot on the session; slot 0 removes it.
func (c Client) SetSlot(id string, slot int) error {
	var err error
	if slot == 0 {
		_, err = c.Run("set-option", "-u", "-t", id, slotOption)
	} else {
		_, err = c.Run("set-option", "-t", id, slotOption, strconv.Itoa(slot))
	}
	return err
}

// NewSession creates a detached session in dir and returns its ID.
// An empty name lets tmux pick one.
func (c Client) NewSession(name, dir string) (string, error) {
	args := []string{"new-session", "-d", "-P", "-F", "#{session_id}"}
	if dir != "" {
		args = append(args, "-c", dir)
	}
	if name != "" {
		args = append(args, "-s", name)
	}
	return c.Run(args...)
}

func (c Client) RenameSession(id, name string) error {
	_, err := c.Run("rename-session", "-t", id, name)
	return err
}

func (c Client) KillSession(id string) error {
	_, err := c.Run("kill-session", "-t", id)
	return err
}

// SwitchClient moves client (or the current client if empty) to session id.
func (c Client) SwitchClient(client, id string) error {
	args := []string{"switch-client", "-t", id}
	if client != "" {
		args = append(args, "-c", client)
	}
	_, err := c.Run(args...)
	return err
}

// ClientSession returns the ID of the session client is showing.
func (c Client) ClientSession(client string) (string, error) {
	args := []string{"display-message", "-p"}
	if client != "" {
		args = append(args, "-c", client)
	}
	return c.Run(append(args, "#{session_id}")...)
}

// CapturePane returns the visible contents of the session's active pane,
// including color escapes.
func (c Client) CapturePane(id string) (string, error) {
	return c.Run("capture-pane", "-p", "-e", "-t", id+":")
}

// SessionTags returns session ID -> value of a user option, for sessions
// where it is set. Values must not contain spaces.
func (c Client) SessionTags(option string) (map[string]string, error) {
	out, err := c.Run("list-sessions", "-F", "#{session_id} #{"+option+"}")
	if err != nil {
		return nil, err
	}
	tags := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if id, v, ok := strings.Cut(line, " "); ok && v != "" {
			tags[id] = v
		}
	}
	return tags, nil
}

// GlobalOption returns a global option's value, "" if unset.
func (c Client) GlobalOption(name string) (string, error) {
	return c.Run("show-options", "-gqv", name)
}

// SetGlobalOption sets a global option; an empty value unsets it.
func (c Client) SetGlobalOption(name, value string) error {
	var err error
	if value == "" {
		_, err = c.Run("set-option", "-gu", name)
	} else {
		_, err = c.Run("set-option", "-g", name, value)
	}
	return err
}

func (c Client) SetSessionOption(id, name, value string) error {
	_, err := c.Run("set-option", "-t", id, name, value)
	return err
}

// NewCommandSession creates a detached session running cmd and returns its ID.
func (c Client) NewCommandSession(name, cmd string) (string, error) {
	return c.Run("new-session", "-d", "-P", "-F", "#{session_id}", "-s", name, cmd)
}

// Display shows msg on client's status line.
func (c Client) Display(client, msg string) error {
	_, err := c.Run("display-message", "-c", client, msg)
	return err
}
