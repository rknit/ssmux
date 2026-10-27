// Package tmux runs the tmux CLI. Sessions are always targeted by ID ("$3")
// so names with spaces, dots or colons never need escaping.
package tmux

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const slotOption = "@ssmux-slot"

type Session struct {
	ID           string // "$3"
	Name         string
	Windows      int
	LastAttached int64 // unix seconds; 0 if never attached
	Slot         int   // 0 = unpinned, 1..9
}

// Client talks to one tmux server. An empty Socket means the default server.
type Client struct{ Socket string }

// Run executes tmux with args and returns trimmed stdout.
func (c Client) Run(args ...string) (string, error) {
	name := args[0]
	if c.Socket != "" {
		args = append([]string{"-S", c.Socket}, args...)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("tmux", args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("tmux %s: %s", name, msg)
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
		"#{" + slotOption + "}", "#{session_name}",
	}, ":")
	out, err := c.Run("list-sessions", "-F", format)
	if err != nil {
		return nil, err
	}
	var ss []Session
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.SplitN(line, ":", 5)
		if len(f) != 5 {
			continue
		}
		s := Session{ID: f[0], Name: f[4]}
		s.Windows, _ = strconv.Atoi(f[1])
		s.LastAttached, _ = strconv.ParseInt(f[2], 10, 64)
		if n, err := strconv.Atoi(f[3]); err == nil && n >= 1 && n <= 9 {
			s.Slot = n
		}
		ss = append(ss, s)
	}
	return ss, nil
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
	args := []string{"new-session", "-d", "-P", "-F", "#{session_id}", "-c", dir}
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
