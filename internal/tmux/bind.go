package tmux

import (
	"strconv"
	"strings"
)

// ShellQuote wraps s in single quotes for /bin/sh.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// BindKeys registers ssmux's root-table bindings on the server. tmux replaces
// an existing binding for the same key, so calling it again is harmless and
// refreshes the binary path.
//
// It also turns detach-on-destroy off, so a client whose session ends (its
// last shell exits) moves to another session and only leaves tmux when no
// session is left.
func (c Client) BindKeys(exe string) error {
	if err := c.SetGlobalOption("detach-on-destroy", "off"); err != nil {
		return err
	}
	bin := ShellQuote(exe)
	open := bin + " open --client '#{client_name}'"
	for _, key := range []string{"M-a", "M-Up"} {
		if _, err := c.Run("bind-key", "-n", key, "run-shell", "-b", open); err != nil {
			return err
		}
	}
	for n := 1; n <= 9; n++ {
		cmd := bin + " switch " + strconv.Itoa(n) + " --client '#{client_name}'"
		if _, err := c.Run("bind-key", "-n", "M-"+strconv.Itoa(n), "run-shell", "-b", cmd); err != nil {
			return err
		}
	}
	return nil
}

// OpenPopup shows the ssmux popup on client.
func (c Client) OpenPopup(client, exe string) error {
	args := []string{"display-popup", "-E", "-w", "80%", "-h", "80%", "-b", "heavy", "-S", "fg=magenta"}
	if client != "" {
		args = append(args, "-c", client)
	}
	// Separate arguments make tmux exec the command directly instead of
	// through default-shell, whose startup can take hundreds of ms.
	_, err := c.Run(append(args, exe, "popup", "--client", client)...)
	return err
}
