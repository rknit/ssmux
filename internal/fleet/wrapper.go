package fleet

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rknit/ssmux/internal/tmux"
)

const (
	wrapperPrefix = "_ssmux_"
	fallbackTerm  = "screen-256color"
	recentMax     = 50
)

// switched is a session recorded by a switch, with the host's gen at the
// time, so a refresh whose listing predates the switch keeps it.
type switched struct {
	gen     int
	session tmux.Session
}

// SwitchClient moves client to a session. A remote session is shown through
// its wrapper, created on first use, and its RECENT time is recorded.
func (f *Fleet) SwitchClient(client, qid string) error {
	name, id := Split(qid)
	if name == "" {
		return f.local.SwitchClient(client, id)
	}
	s, err := f.Resolve(qid)
	if err != nil {
		return err
	}
	w, fallback, err := f.wrapper(name, id, s.Created)
	if err != nil {
		return err
	}
	if err := f.local.SwitchClient(client, w); err != nil {
		return err
	}
	var derr error
	if fallback {
		derr = f.local.Display(client, fmt.Sprintf(
			"ssmux: %s has no terminfo for %s, using %s", name, f.Term, fallbackTerm))
	}
	return errors.Join(derr, f.touch(name, id, s))
}

// Resolve checks that a session still exists and returns it with its
// qualified ID. Slot and LastAttached are not filled in.
func (f *Fleet) Resolve(qid string) (tmux.Session, error) {
	name, id := Split(qid)
	out, err := f.on(name).Run("display-message", "-p", "-t", id,
		"#{session_created}:#{session_windows}:#{session_name}")
	if err != nil {
		return tmux.Session{}, err
	}
	// display-message prints empty fields, not an error, for a missing target.
	p := strings.SplitN(out, ":", 3)
	if len(p) != 3 || p[0] == "" {
		return tmux.Session{}, fmt.Errorf("can't find session: %s", qid)
	}
	s := tmux.Session{ID: qid, Name: p[2], Host: name}
	s.Created, _ = strconv.ParseInt(p[0], 10, 64)
	s.Windows, _ = strconv.Atoi(p[1])
	return s, nil
}

// wrapper returns the local wrapper session for a remote session, creating
// it if needed. fallback reports that a new wrapper uses fallbackTerm.
func (f *Fleet) wrapper(name, id string, created int64) (w string, fallback bool, err error) {
	f.wmu.Lock()
	defer f.wmu.Unlock()
	tag := Entry{Host: name, ID: id, Created: created}.Key()
	if w, err := f.liveWrapper(tag); err != nil || w != "" {
		return w, false, err
	}
	remote := f.remote(name)
	term := f.Term
	if _, err := remote.Exec("infocmp " + tmux.ShellQuote(term)); err != nil {
		term, fallback = fallbackTerm, true
		f.mu.Lock()
		if h := f.host(name); h != nil {
			h.TermFallback = true
		}
		f.mu.Unlock()
	}
	wname, err := f.wrapperName()
	if err != nil {
		return "", false, err
	}
	// The options are set in the same command list as new-session, which
	// makes them its target, so they apply before ssh can exit.
	// remain-on-exit off makes the wrapper close when ssh exits even if the
	// user enables it globally.
	args := []string{"new-session", "-d", "-P", "-F", "#{session_id}", "-s", wname,
		wrapperCommand(remote, term, id)}
	for _, o := range [][2]string{
		{remoteOption, tag}, {"status", "off"}, {"prefix", "None"}, {"prefix2", "None"},
		{"detach-on-destroy", "off"}, {"remain-on-exit", "off"},
	} {
		args = append(args, ";", "set-option", o[0], o[1])
	}
	w, err = f.local.Run(args...)
	if err != nil {
		f.local.KillSession("=" + wname)
		return "", false, err
	}
	return w, fallback, nil
}

// liveWrapper returns the wrapper tagged tag, or "" if there is none. A
// wrapper whose pane is dead (ssh exited under remain-on-exit) is killed and
// treated as absent.
func (f *Fleet) liveWrapper(tag string) (string, error) {
	out, err := f.local.Run("list-sessions", "-F", "#{session_id} #{pane_dead} #{"+remoteOption+"}")
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(out, "\n") {
		p := strings.SplitN(line, " ", 3)
		if len(p) != 3 || p[2] != tag {
			continue
		}
		if p[1] != "1" {
			return p[0], nil
		}
		if err := f.local.KillSession(p[0]); err != nil {
			return "", err
		}
	}
	return "", nil
}

// wrapperName returns the first unused _ssmux_<n> session name.
func (f *Fleet) wrapperName() (string, error) {
	out, err := f.local.Run("list-sessions", "-F", "#{session_name}")
	if err != nil {
		return "", err
	}
	names := strings.Split(out, "\n")
	for n := 1; ; n++ {
		if name := wrapperPrefix + strconv.Itoa(n); !slices.Contains(names, name) {
			return name, nil
		}
	}
}

// wrapperCommand is the shell command a wrapper runs: ssh with a tty
// attaching to session id on the host. The remote command is quoted twice:
// once for the local shell and once for the remote one.
func wrapperCommand(remote tmux.Client, term, id string) string {
	q := []string{"env", tmux.ShellQuote("TERM=" + term)}
	for _, a := range remote.SSHArgs(true) {
		q = append(q, tmux.ShellQuote(a))
	}
	q = append(q, tmux.ShellQuote("tmux attach -t "+tmux.ShellQuote(id)))
	return strings.Join(q, " ")
}

// touch records now as the RECENT time of a remote session and caches the
// session, so it lists before the next refresh.
func (f *Fleet) touch(name, id string, s tmux.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	recent, err := f.entries(recentOption)
	if err != nil {
		return err
	}
	recent = Newest(Upsert(recent, Entry{name, id, s.Created, time.Now().Unix()}), recentMax)
	if err := f.local.SetGlobalOption(recentOption, FormatEntries(recent)); err != nil {
		return err
	}
	h := f.host(name)
	if h == nil {
		return nil
	}
	s.ID = id
	h.gen++
	h.switched[id] = switched{h.gen, s}
	h.names[id] = s.Name
	if h.State == Connected {
		h.sessions = append(slices.DeleteFunc(h.sessions, func(x tmux.Session) bool { return x.ID == id }), s)
	}
	return nil
}

// startRefresh numbers a new refresh of the host and returns its sequence
// number and the host's switch generation; ok is false if the host is not
// configured.
func (f *Fleet) startRefresh(name string) (seq, gen int, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h := f.host(name)
	if h == nil {
		return 0, 0, false
	}
	h.started++
	return h.started, h.gen, true
}

// withSwitched returns ss plus the sessions switched to after gen, which a
// listing begun at gen may lack, and forgets the older switches. Callers
// hold mu.
func (h *host) withSwitched(ss []tmux.Session, gen int) []tmux.Session {
	for id, sw := range h.switched {
		if sw.gen <= gen {
			delete(h.switched, id)
			continue
		}
		ss = append(slices.DeleteFunc(ss, func(s tmux.Session) bool { return s.ID == id }), sw.session)
	}
	return ss
}

// parseTag parses an @ssmux-remote value "host,id,created".
func parseTag(v string) (Entry, bool) {
	p := strings.Split(v, ",")
	if len(p) != 3 || p[0] == "" || p[1] == "" {
		return Entry{}, false
	}
	created, err := strconv.ParseInt(p[2], 10, 64)
	if err != nil {
		return Entry{}, false
	}
	return Entry{Host: p[0], ID: p[1], Created: created}, true
}

// killWrappers kills the local wrappers of the host's sessions.
func (f *Fleet) killWrappers(name string) error {
	tags, err := f.local.SessionTags(remoteOption)
	if err != nil {
		return err
	}
	for w, v := range tags {
		if e, ok := parseTag(v); ok && e.Host == name {
			if err := f.local.KillSession(w); err != nil {
				return err
			}
		}
	}
	return nil
}
