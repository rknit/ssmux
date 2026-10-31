package fleet

import (
	"errors"
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
	// RemoteOption tags the hidden local wrapper sessions.
	RemoteOption = "@ssmux-remote"
)

// State is a host's connection state.
type State int

const (
	Connecting State = iota
	Connected
	Unreachable
)

type HostStatus struct {
	Name string
	// Dests are the host's ssh destinations, tried in order.
	Dests []string
	// Dest is the destination in use: the last one that answered.
	Dest         string
	State        State
	Err          string
	TermFallback bool
}

// host is the cached view of one configured host.
type host struct {
	HostStatus
	sessions []tmux.Session    // last listing; nil unless Connected
	names    map[string]string // id -> last known name
	gen      int               // bumped by each switch to one of its sessions
	switched map[string]switched
	started  int // refreshes started
	applied  int // sequence number of the newest refresh applied
}

// Fleet merges the local tmux server with every configured ssh host and
// routes calls by qualified ID. It is safe for concurrent use.
type Fleet struct {
	local     tmux.Client
	hostsPath string
	sshDir    string
	dial      func(dest string) tmux.Client

	// Term is the local TERM that wrappers pass to ssh.
	Term string

	wmu   sync.Mutex // serializes wrapper lookup and creation
	mu    sync.Mutex
	hosts []*host
}

// New loads the hosts file; every host starts out Connecting. dial builds
// the client for an ssh destination, and its Timeout bounds every remote
// call.
func New(local tmux.Client, hostsPath, sshDir string, dial func(dest string) tmux.Client) *Fleet {
	f := &Fleet{local: local, hostsPath: hostsPath, sshDir: sshDir, dial: dial, Term: os.Getenv("TERM")}
	hs, _ := hosts.Load(hostsPath)
	for _, h := range hs {
		f.hosts = append(f.hosts, newHost(h))
	}
	return f
}

func newHost(h hosts.Host) *host {
	return &host{
		HostStatus: HostStatus{Name: h.Name, Dests: h.Dests, Dest: h.Dests[0]},
		names:      map[string]string{}, switched: map[string]switched{},
	}
}

// remote returns the client for the host named name through its current
// destination. An unknown name is used as the destination.
func (f *Fleet) remote(name string) tmux.Client {
	f.mu.Lock()
	defer f.mu.Unlock()
	if h := f.host(name); h != nil {
		return f.dial(h.Dest)
	}
	return f.dial(name)
}

// dests returns the host's destinations, the current one first.
func (f *Fleet) dests(name string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	h := f.host(name)
	if h == nil {
		return nil
	}
	out := []string{h.Dest}
	for _, d := range h.Dests {
		if d != h.Dest {
			out = append(out, d)
		}
	}
	return out
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
// It tries the current destination first, then the others in order, and
// makes the first that answers current. It blocks for up to the remote
// client's Timeout per destination tried.
func (f *Fleet) RefreshHost(name string) {
	seq, gen, ok := f.startRefresh(name)
	if !ok {
		return
	}
	var (
		ss   []tmux.Session
		err  error
		dest string
		errs []error
	)
	for _, dest = range f.dests(name) {
		if ss, err = f.dial(dest).ListSessions(); err == nil {
			break
		}
		errs = append(errs, fmt.Errorf("%s: %w", dest, err))
	}
	if err != nil && len(errs) > 1 {
		err = errors.Join(errs...)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	h := f.host(name)
	if h == nil || seq < h.applied {
		return // a refresh that started later already applied
	}
	h.applied = seq
	if err != nil {
		h.State, h.Err, h.sessions = Unreachable, err.Error(), nil
		return
	}
	for i := range ss {
		ss[i].Host = name // the client sets the destination
	}
	ss = h.withSwitched(ss, gen)
	if slices.Contains(h.Dests, dest) {
		h.Dest = dest
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
	wrappers, err := f.local.SessionTags(RemoteOption)
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
		delete(h.switched, id)
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

// ClientSession returns the ID of the session client is showing: the remote
// session's qualified ID when it is in a wrapper.
func (f *Fleet) ClientSession(client string) (string, error) {
	id, err := f.local.ClientSession(client)
	if err != nil {
		return "", err
	}
	tags, err := f.local.SessionTags(RemoteOption)
	if err != nil {
		return "", err
	}
	if e, ok := parseTag(tags[id]); ok {
		return Qualify(e.Host, e.ID), nil
	}
	return id, nil
}

// Display shows msg on client's status line.
func (f *Fleet) Display(client, msg string) error {
	return f.local.Display(client, msg)
}

// AddHost appends h to the hosts file and starts tracking it.
func (f *Fleet) AddHost(h hosts.Host) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := hosts.Add(f.hostsPath, h); err != nil {
		return err
	}
	f.hosts = append(f.hosts, newHost(h))
	return nil
}

// SetDests replaces the host's destinations. Its sessions, pins and wrappers
// stay, since they belong to the host, not to a destination. The current
// destination is kept if it is still listed.
func (f *Fleet) SetDests(name string, dests []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	h := f.host(name)
	if h == nil {
		return fmt.Errorf("unknown host %s", name)
	}
	if err := hosts.SetDests(f.hostsPath, name, dests); err != nil {
		return err
	}
	h.Dests = slices.Clone(dests)
	if !slices.Contains(dests, h.Dest) {
		h.Dest = dests[0]
	}
	return nil
}

// Suggestions lists ~/.ssh/config hosts that are not configured yet, as a
// name or as a destination.
func (f *Fleet) Suggestions() []string {
	f.mu.Lock()
	var used []string
	for _, h := range f.hosts {
		used = append(append(used, h.Name), h.Dests...)
	}
	f.mu.Unlock()
	return hosts.Suggestions(f.sshDir, used)
}

// RemoveHost kills the host's wrappers and drops the host from the hosts
// file and its entries from the local options.
func (f *Fleet) RemoveHost(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.killWrappers(name); err != nil {
		return err
	}
	if err := hosts.Remove(f.hostsPath, name); err != nil {
		return err
	}
	f.hosts = slices.DeleteFunc(f.hosts, func(h *host) bool { return h.Name == name })
	return f.dropEntries(func(e Entry) bool { return e.Host != name })
}
