package fleet

import (
	"fmt"
	"os"
	"slices"
	"sync"

	"github.com/rknit/ssmux/internal/hosts"
	"github.com/rknit/ssmux/internal/tmux"
)

const (
	pinsOption   = "@ssmux-remote-pins"
	recentOption = "@ssmux-remote-recent"
	remoteOption = "@ssmux-remote"
)

// State is a host's connection state.
type State int

const (
	Connecting State = iota
	Connected
	Unreachable
)

type HostStatus struct {
	Name         string
	State        State
	Err          string
	TermFallback bool
}

// host is the cached view of one configured host.
type host struct {
	HostStatus
	sessions []tmux.Session    // last listing; nil unless Connected
	names    map[string]string // id -> last known name
}

// Fleet merges the local tmux server with every configured ssh host and
// routes calls by qualified ID. It is safe for concurrent use.
type Fleet struct {
	local     tmux.Client
	hostsPath string
	sshDir    string
	remote    func(host string) tmux.Client

	mu    sync.Mutex
	hosts []*host
}

// New loads the hosts file; every host starts out Connecting. remote builds
// the client for a host, and its Timeout bounds every remote call.
func New(local tmux.Client, hostsPath, sshDir string, remote func(host string) tmux.Client) *Fleet {
	f := &Fleet{local: local, hostsPath: hostsPath, sshDir: sshDir, remote: remote}
	names, _ := hosts.Load(hostsPath)
	for _, n := range names {
		f.hosts = append(f.hosts, newHost(n))
	}
	return f
}

func newHost(name string) *host {
	return &host{HostStatus: HostStatus{Name: name}, names: map[string]string{}}
}

// host returns the cached host named name, or nil. Callers hold mu.
func (f *Fleet) host(name string) *host {
	for _, h := range f.hosts {
		if h.Name == name {
			return h
		}
	}
	return nil
}

// Hosts returns the status of each configured host in config order.
func (f *Fleet) Hosts() []HostStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]HostStatus, len(f.hosts))
	for i, h := range f.hosts {
		out[i] = h.HostStatus
	}
	return out
}

// RefreshHost lists the host's sessions over ssh and caches the result.
// It blocks for up to the remote client's Timeout.
func (f *Fleet) RefreshHost(name string) {
	ss, err := f.remote(name).ListSessions()
	f.mu.Lock()
	defer f.mu.Unlock()
	h := f.host(name)
	if h == nil {
		return
	}
	if err != nil {
		h.State, h.Err, h.sessions = Unreachable, err.Error(), nil
		return
	}
	h.State, h.Err, h.sessions = Connected, "", ss
	for _, s := range ss {
		h.names[s.ID] = s.Name
	}
	f.dropStale(name, ss)
}

// dropStale removes the host's entries for sessions that ss lacks or lists
// with a different Created (the remote server restarted and reused the ID).
// Callers hold mu.
func (f *Fleet) dropStale(name string, ss []tmux.Session) {
	created := map[string]int64{}
	for _, s := range ss {
		created[s.ID] = s.Created
	}
	// Best effort: a failed write leaves the entries for the next refresh,
	// and ListSessions never matches them meanwhile.
	f.dropEntries(func(e Entry) bool {
		c, ok := created[e.ID]
		return e.Host != name || ok && c == e.Created
	})
}

// dropEntries rewrites both local options without the entries keep rejects,
// writing only options that change. Callers hold mu.
func (f *Fleet) dropEntries(keep func(Entry) bool) error {
	for _, opt := range []string{pinsOption, recentOption} {
		es, err := f.entries(opt)
		if err != nil {
			return err
		}
		if kept := Without(es, keep); len(kept) != len(es) {
			if err := f.local.SetGlobalOption(opt, FormatEntries(kept)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *Fleet) entries(opt string) ([]Entry, error) {
	v, err := f.local.GlobalOption(opt)
	if err != nil {
		return nil, err
	}
	return ParseEntries(v), nil
}

// ListSessions returns local sessions, minus wrappers, plus the cached
// sessions of every host. It never touches the network.
func (f *Fleet) ListSessions() ([]tmux.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out, err := f.localSessions()
	if err != nil {
		return nil, err
	}
	pins, err := f.entries(pinsOption)
	if err != nil {
		return nil, err
	}
	recent, err := f.entries(recentOption)
	if err != nil {
		return nil, err
	}
	for _, h := range f.hosts {
		out = append(out, h.list(pins, recent)...)
	}
	return out, nil
}

func (f *Fleet) localSessions() ([]tmux.Session, error) {
	ss, err := f.local.ListSessions()
	if err != nil {
		return nil, err
	}
	wrappers, err := f.local.SessionTags(remoteOption)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(ss, func(s tmux.Session) bool {
		_, ok := wrappers[s.ID]
		return ok
	}), nil
}

// list returns h's sessions with qualified IDs and Slot and LastAttached
// taken from the local entries. A host without a listing yields its pins as
// placeholders carrying the host's status.
func (h *host) list(pins, recent []Entry) []tmux.Session {
	if h.State != Connected {
		return h.placeholders(pins, recent)
	}
	out := make([]tmux.Session, len(h.sessions))
	for i, s := range h.sessions {
		s.Slot = int(value(pins, h.Name, s.ID, s.Created))
		s.LastAttached = value(recent, h.Name, s.ID, s.Created)
		s.ID = Qualify(h.Name, s.ID)
		out[i] = s
	}
	return out
}

func (h *host) placeholders(pins, recent []Entry) []tmux.Session {
	status := "connecting"
	if h.State == Unreachable {
		status = "unreachable"
	}
	var out []tmux.Session
	for _, e := range pins {
		if e.Host != h.Name {
			continue
		}
		name := h.names[e.ID]
		if name == "" {
			name = e.ID
		}
		out = append(out, tmux.Session{
			ID: Qualify(h.Name, e.ID), Name: name, Host: h.Name,
			Created: e.Created, Slot: int(e.Value), Status: status,
			LastAttached: value(recent, h.Name, e.ID, e.Created),
		})
	}
	return out
}

// value returns the Value of the entry matching Host, ID and Created; 0 if
// none.
func value(es []Entry, hostName, id string, created int64) int64 {
	for _, e := range es {
		if e.Host == hostName && e.ID == id && e.Created == created {
			return e.Value
		}
	}
	return 0
}

// SetSlot pins a session; slot 0 unpins it. Remote pins live in the local
// @ssmux-remote-pins option, never on the remote server.
func (f *Fleet) SetSlot(qid string, slot int) error {
	name, id := Split(qid)
	if name == "" {
		return f.local.SetSlot(id, slot)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	pins, err := f.entries(pinsOption)
	if err != nil {
		return err
	}
	if slot == 0 {
		pins = Without(pins, func(e Entry) bool { return e.Host != name || e.ID != id })
	} else {
		created, ok := f.created(pins, name, id)
		if !ok {
			return fmt.Errorf("unknown session %s", qid)
		}
		pins = Upsert(pins, Entry{name, id, created, int64(slot)})
	}
	return f.local.SetGlobalOption(pinsOption, FormatEntries(pins))
}

// created finds a remote session's Created in the cached listing, else in
// its pin entry. Callers hold mu.
func (f *Fleet) created(pins []Entry, name, id string) (int64, bool) {
	if h := f.host(name); h != nil {
		for _, s := range h.sessions {
			if s.ID == id {
				return s.Created, true
			}
		}
	}
	for _, e := range pins {
		if e.Host == name && e.ID == id {
			return e.Created, true
		}
	}
	return 0, false
}

// on returns the client for host ("" for local).
func (f *Fleet) on(hostName string) tmux.Client {
	if hostName == "" {
		return f.local
	}
	return f.remote(hostName)
}

// client returns the client for a qualified ID and the unqualified ID.
func (f *Fleet) client(qid string) (tmux.Client, string) {
	name, id := Split(qid)
	return f.on(name), id
}

// NewSession creates a session on host ("" for local) and returns its
// qualified ID. Local sessions start in the home directory, remote ones in
// the remote login directory.
func (f *Fleet) NewSession(hostName, name string) (string, error) {
	dir := ""
	if hostName == "" {
		dir, _ = os.UserHomeDir()
	}
	id, err := f.on(hostName).NewSession(name, dir)
	if err != nil {
		return "", err
	}
	return Qualify(hostName, id), nil
}

func (f *Fleet) RenameSession(qid, name string) error {
	c, id := f.client(qid)
	if err := c.RenameSession(id, name); err != nil {
		return err
	}
	f.cached(qid, func(h *host, id string) {
		for i := range h.sessions {
			if h.sessions[i].ID == id {
				h.sessions[i].Name = name
			}
		}
		h.names[id] = name
	})
	return nil
}

func (f *Fleet) KillSession(qid string) error {
	c, id := f.client(qid)
	if err := c.KillSession(id); err != nil {
		return err
	}
	name, _ := Split(qid)
	if name == "" {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if h := f.host(name); h != nil {
		h.sessions = slices.DeleteFunc(h.sessions, func(s tmux.Session) bool { return s.ID == id })
	}
	return f.dropEntries(func(e Entry) bool { return e.Host != name || e.ID != id })
}

// cached runs fn under mu on the cached host of a remote qualified ID, so
// renames show up before the next refresh.
func (f *Fleet) cached(qid string, fn func(h *host, id string)) {
	name, id := Split(qid)
	f.mu.Lock()
	defer f.mu.Unlock()
	if h := f.host(name); name != "" && h != nil {
		fn(h, id)
	}
}

func (f *Fleet) CapturePane(qid string) (string, error) {
	c, id := f.client(qid)
	return c.CapturePane(id)
}

// ClientSession returns the ID of the local session client is showing.
func (f *Fleet) ClientSession(client string) (string, error) {
	return f.local.ClientSession(client)
}

// AddHost appends dest to the hosts file and starts tracking it.
func (f *Fleet) AddHost(dest string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := hosts.Add(f.hostsPath, dest); err != nil {
		return err
	}
	f.hosts = append(f.hosts, newHost(dest))
	return nil
}

// Suggestions lists ~/.ssh/config hosts that are not configured yet.
func (f *Fleet) Suggestions() []string {
	f.mu.Lock()
	names := make([]string, len(f.hosts))
	for i, h := range f.hosts {
		names[i] = h.Name
	}
	f.mu.Unlock()
	return hosts.Suggestions(f.sshDir, names)
}

// RemoveHost drops the host from the hosts file and its entries from the
// local options.
func (f *Fleet) RemoveHost(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := hosts.Remove(f.hostsPath, name); err != nil {
		return err
	}
	f.hosts = slices.DeleteFunc(f.hosts, func(h *host) bool { return h.Name == name })
	return f.dropEntries(func(e Entry) bool { return e.Host != name })
}
