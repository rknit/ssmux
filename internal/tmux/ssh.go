package tmux

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const (
	// coldTimeout bounds a remote call that has to log in first.
	coldTimeout = 10 * time.Second
	// checkTimeout bounds "ssh -O check", a local socket check.
	checkTimeout = time.Second
)

// sshDir holds the ssh control sockets shared by all remote calls.
func sshDir() string { return fmt.Sprintf("/tmp/ssmux-%d", os.Getuid()) }

// controlPath is the ControlPath option value for every ssh call.
func controlPath() string { return "ControlPath=" + sshDir() + "/cm-%C" }

// masterAlive reports whether a ControlMaster for c.Host is running.
func (c Client) masterAlive() bool {
	ssh := c.SSH
	if ssh == "" {
		ssh = "ssh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ssh, "-o", controlPath(), "-O", "check", "--", c.Host)
	cmd.WaitDelay = 100 * time.Millisecond
	return cmd.Run() == nil
}

// ensureSSHDir creates dir if needed and makes sure it is a real directory
// owned by the current user and private to it, since its path is
// predictable and anyone could have created it first.
func ensureSSHDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s: not a directory", dir)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s: not owned by the current user", dir)
	}
	if fi.Mode().Perm() != 0o700 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// tempOutput returns an unlinked temporary file for a child's output.
func tempOutput() (*os.File, error) {
	f, err := os.CreateTemp("", "ssmux-out")
	if err != nil {
		return nil, err
	}
	if err := os.Remove(f.Name()); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// readOutput appends everything written to f to buf.
func readOutput(f *os.File, buf *bytes.Buffer) {
	if _, err := f.Seek(0, io.SeekStart); err == nil {
		io.Copy(buf, f)
	}
}

// SSHArgs returns the ssh command line up to and including "--", ready for
// the remote command to be appended. tty adds -t.
func (c Client) SSHArgs(tty bool) []string {
	ssh := c.SSH
	if ssh == "" {
		ssh = "ssh"
	}
	args := []string{ssh,
		"-o", "ControlMaster=auto",
		"-o", controlPath(),
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
