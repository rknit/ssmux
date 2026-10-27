package tmux

import (
	"strconv"
	"strings"
)

// shellQuote wraps s in single quotes for /bin/sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// BindKeys registers ssmux's root-table bindings on the server. tmux replaces
// an existing binding for the same key, so calling it again is harmless and
// refreshes the binary path.
func (c Client) BindKeys(exe string) error {
	bin := shellQuote(exe)
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
	args := []string{"display-popup", "-E", "-w", "80%", "-h", "80%"}
	if client != "" {
		args = append(args, "-c", client)
	}
	cmd := shellQuote(exe) + " popup --client " + shellQuote(client)
	_, err := c.Run(append(args, cmd)...)
	return err
}
