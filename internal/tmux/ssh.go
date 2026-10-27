package tmux

import (
	"fmt"
	"os"
	"strings"
)

// sshDir holds the ssh control sockets shared by all remote calls.
func sshDir() string { return fmt.Sprintf("/tmp/ssmux-%d", os.Getuid()) }

// SSHArgs returns the ssh command line up to and including "--", ready for
// the remote command to be appended. tty adds -t.
func (c Client) SSHArgs(tty bool) []string {
	ssh := c.SSH
	if ssh == "" {
		ssh = "ssh"
	}
	args := []string{ssh,
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + sshDir() + "/cm-%C",
		"-o", "ControlPersist=10m",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=3",
	}
	if tty {
		args = append(args, "-t")
	}
	return append(args, c.Host, "--")
}

// remoteTmux builds the single command string run on the remote host.
func remoteTmux(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = ShellQuote(a)
	}
	return "tmux " + strings.Join(q, " ")
}
