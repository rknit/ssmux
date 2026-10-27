package tmux

import (
	"errors"
	"testing"
)

func TestNoServer(t *testing.T) {
	for _, tc := range []struct {
		msg  string
		want bool
	}{
		{"tmux list-sessions: no server running on /tmp/tmux-1000/default", true},
		{"tmux list-sessions: error connecting to /tmp/tmux-1000/default (No such file or directory)", true},
		{"tmux list-sessions: error connecting to /tmp/tmux-1000/default (Connection refused)", true},
		{"tmux list-sessions: error connecting to /tmp/tmux-1000/default (Permission denied)", false},
		{"tmux list-sessions: ssh: connect to host box port 22: Connection refused", false},
		{"tmux list-sessions: sh: tmux: command not found", false},
	} {
		if got := noServer(errors.New(tc.msg)); got != tc.want {
			t.Errorf("noServer(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}
