package fleet_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rknit/ssmux/internal/fleet"
	"github.com/rknit/ssmux/internal/hosts"
	"github.com/rknit/ssmux/internal/testutil"
	"github.com/rknit/ssmux/internal/tmux"
)

const (
	pinsOpt   = "@ssmux-remote-pins"
	recentOpt = "@ssmux-remote-recent"
)

type env struct {
	local     tmux.Client
	remote    func(string) tmux.Client
	hostsPath string
}

// newEnv starts a local server and fake ssh hosts, with "box" and "ci"
// configured.
func newEnv(t *testing.T) *env {
	t.Helper()
	_, remote := testutil.FakeSSH(t)
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte("box\nci\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &env{local: testutil.NewServer(t), remote: remote, hostsPath: path}
}

func (e *env) fleet() *fleet.Fleet {
	return fleet.New(e.local, e.hostsPath, "", e.remote)
}

func must(t *testing.T) func(string, error) string {
	return func(id string, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
}

func list(t *testing.T, f *fleet.Fleet) []tmux.Session {
	t.Helper()
	ss, err := f.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	return ss
}

func find(ss []tmux.Session, id string) (tmux.Session, bool) {
	for _, s := range ss {
		if s.ID == id {
			return s, true
		}
	}
	return tmux.Session{}, false
}

func option(t *testing.T, c tmux.Client, name string) string {
	t.Helper()
	v, err := c.GlobalOption(name)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestListBeforeRefresh(t *testing.T) {
	e := newEnv(t)
	id := must(t)(e.remote("box").NewSession("api", ""))
	old := e.fleet()
	old.RefreshHost("box")
	if err := old.SetSlot("box/"+id, 2); err != nil {
		t.Fatal(err)
	}

	ss := list(t, e.fleet())
	if _, ok := find(ss, "$0"); !ok {
		t.Errorf("local base session missing: %+v", ss)
	}
	var remote []tmux.Session
	for _, s := range ss {
		if s.Host == "box" {
			remote = append(remote, s)
		}
	}
	if len(remote) != 1 {
		t.Fatalf("box sessions = %+v, want only the pinned one", remote)
	}
	s := remote[0]
	if s.ID != "box/"+id || s.Slot != 2 || s.Status != "connecting" || s.Name != id {
		t.Errorf("placeholder = %+v", s)
	}
}

func TestListAfterRefresh(t *testing.T) {
	e := newEnv(t)
	id := must(t)(e.remote("box").NewSession("api", ""))
	f := e.fleet()
	f.RefreshHost("box")
	s, ok := find(list(t, f), "box/"+id)
	if !ok {
		t.Fatal("api not listed")
	}
	if s.Name != "api" || s.Host != "box" || s.Status != "" || s.LastAttached != 0 {
		t.Errorf("api = %+v", s)
	}
	if h := f.Hosts(); len(h) != 2 || h[0].Name != "box" || h[0].State != fleet.Connected {
		t.Errorf("Hosts = %+v", h)
	}
}

func TestUnreachableKeepsPins(t *testing.T) {
	e := newEnv(t)
	bad := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(bad, []byte("#!/bin/sh\nexit 255\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ci := e.remote("ci")
	pinned := must(t)(ci.NewSession("pinned", ""))
	loose := must(t)(ci.NewSession("loose", ""))
	failing := false
	f := fleet.New(e.local, e.hostsPath, "", func(h string) tmux.Client {
		if h == "ci" && failing {
			return tmux.Client{Host: h, SSH: bad}
		}
		return e.remote(h)
	})
	f.RefreshHost("ci")
	if err := f.SetSlot("ci/"+pinned, 5); err != nil {
		t.Fatal(err)
	}
	failing = true
	f.RefreshHost("ci")
	ss := list(t, f)
	s, ok := find(ss, "ci/"+pinned)
	if !ok || s.Status != "unreachable" || s.Slot != 5 || s.Name != "pinned" {
		t.Errorf("pinned ci session = %+v, %v", s, ok)
	}
	if s, ok := find(ss, "ci/"+loose); ok {
		t.Errorf("unpinned ci session listed: %+v", s)
	}
	if v := option(t, e.local, pinsOpt); !strings.HasPrefix(v, "ci,"+pinned+",") {
		t.Errorf("failed refresh changed %s: %q", pinsOpt, v)
	}
	h := f.Hosts()[1]
	if h.Name != "ci" || h.State != fleet.Unreachable || h.Err == "" {
		t.Errorf("ci status = %+v", h)
	}
}

func TestMissingSessionEntriesDropped(t *testing.T) {
	e := newEnv(t)
	e.remote("box")
	if err := e.local.SetGlobalOption(pinsOpt, "box,$99,1,4 ci,$99,1,6"); err != nil {
		t.Fatal(err)
	}
	if err := e.local.SetGlobalOption(recentOpt, "box,$99,1,50"); err != nil {
		t.Fatal(err)
	}
	f := e.fleet()
	f.RefreshHost("box")
	if v := option(t, e.local, pinsOpt); v != "ci,$99,1,6" {
		t.Errorf("%s = %q", pinsOpt, v)
	}
	if v := option(t, e.local, recentOpt); v != "" {
		t.Errorf("%s = %q", recentOpt, v)
	}
}

func TestKillDropsEntries(t *testing.T) {
	e := newEnv(t)
	box := e.remote("box")
	id := must(t)(box.NewSession("api", ""))
	created, err := box.Run("display", "-p", "-t", id, "#{session_created}")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.local.SetGlobalOption(recentOpt, "box,"+id+","+created+",77"); err != nil {
		t.Fatal(err)
	}
	f := e.fleet()
	f.RefreshHost("box")
	if err := f.SetSlot("box/"+id, 2); err != nil {
		t.Fatal(err)
	}
	if err := f.KillSession("box/" + id); err != nil {
		t.Fatal(err)
	}
	for _, opt := range []string{pinsOpt, recentOpt} {
		if v := option(t, e.local, opt); v != "" {
			t.Errorf("%s = %q", opt, v)
		}
	}
	if _, ok := find(list(t, f), "box/"+id); ok {
		t.Error("killed session listed")
	}
}

func TestRemoteSlotStoredLocally(t *testing.T) {
	e := newEnv(t)
	box := e.remote("box")
	id := must(t)(box.NewSession("api", ""))
	f := e.fleet()
	f.RefreshHost("box")
	if err := f.SetSlot("box/"+id, 3); err != nil {
		t.Fatal(err)
	}
	if v := option(t, e.local, pinsOpt); !strings.HasPrefix(v, "box,"+id+",") || !strings.HasSuffix(v, ",3") {
		t.Errorf("%s = %q", pinsOpt, v)
	}
	if v, err := box.Run("display", "-p", "-t", id, "#{@ssmux-slot}"); err != nil || v != "" {
		t.Errorf("box @ssmux-slot = %q, %v", v, err)
	}
	if s, _ := find(list(t, f), "box/"+id); s.Slot != 3 {
		t.Errorf("slot = %d, want 3", s.Slot)
	}
	if err := f.SetSlot("box/"+id, 0); err != nil {
		t.Fatal(err)
	}
	if v := option(t, e.local, pinsOpt); v != "" {
		t.Errorf("after unpin %s = %q", pinsOpt, v)
	}
}

func TestStalePinDropped(t *testing.T) {
	e := newEnv(t)
	id := must(t)(e.remote("box").NewSession("api", ""))
	if err := e.local.SetGlobalOption(pinsOpt, "box,"+id+",1,4 ci,$9,1,6"); err != nil {
		t.Fatal(err)
	}
	if err := e.local.SetGlobalOption(recentOpt, "box,"+id+",1,99"); err != nil {
		t.Fatal(err)
	}
	f := e.fleet()
	f.RefreshHost("box")
	s, _ := find(list(t, f), "box/"+id)
	if s.Slot != 0 || s.LastAttached != 0 {
		t.Errorf("stale entries applied: %+v", s)
	}
	if v := option(t, e.local, pinsOpt); strings.Contains(v, "box,"+id+",") || !strings.Contains(v, "ci,$9,1,6") {
		t.Errorf("%s = %q", pinsOpt, v)
	}
	if v := option(t, e.local, recentOpt); strings.Contains(v, "box,"+id+",") {
		t.Errorf("%s = %q", recentOpt, v)
	}
}

func TestRecentFromLocalOption(t *testing.T) {
	e := newEnv(t)
	box := e.remote("box")
	id := must(t)(box.NewSession("api", ""))
	created, err := box.Run("display", "-p", "-t", id, "#{session_created}")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.local.SetGlobalOption(recentOpt, "box,"+id+","+created+",77"); err != nil {
		t.Fatal(err)
	}
	f := e.fleet()
	f.RefreshHost("box")
	if s, _ := find(list(t, f), "box/"+id); s.LastAttached != 77 {
		t.Errorf("LastAttached = %d, want 77", s.LastAttached)
	}
}

func TestLocalWrappersHidden(t *testing.T) {
	e := newEnv(t)
	id := must(t)(e.local.NewSession("_ssmux_1", ""))
	if err := e.local.SetSessionOption(id, "@ssmux-remote", "box,$1,100"); err != nil {
		t.Fatal(err)
	}
	if _, ok := find(list(t, e.fleet()), id); ok {
		t.Error("wrapper listed")
	}
}

func TestRoutingRemote(t *testing.T) {
	e := newEnv(t)
	box := e.remote("box")
	id := must(t)(box.NewSession("api", ""))
	f := e.fleet()
	f.RefreshHost("box")
	q := "box/" + id
	if err := f.RenameSession(q, "x y"); err != nil {
		t.Fatal(err)
	}
	if v, _ := box.Run("display", "-p", "-t", id, "#{session_name}"); v != "x y" {
		t.Errorf("remote name = %q", v)
	}
	if s, _ := find(list(t, f), q); s.Name != "x y" {
		t.Errorf("listed name = %q", s.Name)
	}
	if _, err := f.CapturePane(q); err != nil {
		t.Error(err)
	}
	if err := f.KillSession(q); err != nil {
		t.Fatal(err)
	}
	ss, err := box.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := find(ss, id); ok {
		t.Error("session still on box")
	}
	if _, ok := find(list(t, f), q); ok {
		t.Error("killed session still listed")
	}
}

func TestNewSessionRemote(t *testing.T) {
	e := newEnv(t)
	f := e.fleet()
	q := must(t)(f.NewSession("box", "n"))
	host, id := fleet.Split(q)
	if host != "box" || !strings.HasPrefix(id, "$") {
		t.Fatalf("NewSession = %q", q)
	}
	ss, err := e.remote("box").ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := find(ss, id); !ok || s.Name != "n" {
		t.Errorf("box sessions = %+v", ss)
	}
	local := must(t)(f.NewSession("", "l"))
	if strings.Contains(local, "/") {
		t.Errorf("local NewSession = %q", local)
	}
}

func TestRemoveHostClearsState(t *testing.T) {
	e := newEnv(t)
	if err := e.local.SetGlobalOption(pinsOpt, "box,$1,1,2 ci,$1,1,3"); err != nil {
		t.Fatal(err)
	}
	if err := e.local.SetGlobalOption(recentOpt, "box,$1,1,50"); err != nil {
		t.Fatal(err)
	}
	f := e.fleet()
	if err := f.RemoveHost("box"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(e.hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "box") {
		t.Errorf("hosts file = %q", b)
	}
	if v := option(t, e.local, pinsOpt); v != "ci,$1,1,3" {
		t.Errorf("%s = %q", pinsOpt, v)
	}
	if v := option(t, e.local, recentOpt); v != "" {
		t.Errorf("%s = %q", recentOpt, v)
	}
	if h := f.Hosts(); len(h) != 1 || h[0].Name != "ci" {
		t.Errorf("Hosts = %+v", h)
	}
}

func TestAddHost(t *testing.T) {
	e := newEnv(t)
	f := e.fleet()
	if err := f.AddHost(hosts.Host{Name: "dev", Dests: []string{"dev"}}); err != nil {
		t.Fatal(err)
	}
	h := f.Hosts()
	if len(h) != 3 || h[2].Name != "dev" || h[2].State != fleet.Connecting {
		t.Errorf("Hosts = %+v", h)
	}
	if err := f.AddHost(hosts.Host{Name: "a/b", Dests: []string{"a"}}); err == nil {
		t.Error("AddHost accepted '/'")
	}
}

// A host that runs sshd and tmux but no tmux server is Connected with no
// sessions, so the first remote session can be created there.
func TestRefreshHostWithoutServer(t *testing.T) {
	ssh, remote := testutil.FakeSSH(t)
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte("fresh\nbox\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh := tmux.Client{Host: "fresh", SSH: ssh}
	t.Cleanup(func() { fresh.Run("kill-server") })
	box := remote("box")
	f := fleet.New(testutil.NewServer(t), path, "", func(h string) tmux.Client {
		if h == "fresh" {
			return fresh
		}
		return remote(h)
	})

	f.RefreshHost("fresh")
	if h := f.Hosts()[0]; h.State != fleet.Connected || h.Err != "" {
		t.Fatalf("fresh = %+v", h)
	}
	for _, s := range list(t, f) {
		if s.Host == "fresh" {
			t.Errorf("unexpected session %+v", s)
		}
	}
	q := must(t)(f.NewSession("fresh", "x"))
	f.RefreshHost("fresh")
	if s, ok := find(list(t, f), q); !ok || s.Name != "x" {
		t.Errorf("x not listed after NewSession: %q", q)
	}

	// Killing the last session stops the server; the host stays usable.
	ss, err := box.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range ss {
		if err := box.KillSession(s.ID); err != nil {
			t.Fatal(err)
		}
	}
	f.RefreshHost("box")
	if h := f.Hosts()[1]; h.State != fleet.Connected || h.Err != "" {
		t.Errorf("box after killing all = %+v", h)
	}
}

// A host tries its destinations in order and sticks with the one that
// answers; sessions and pins are keyed by the host name, not the destination.
func TestHostDestFallback(t *testing.T) {
	_, remote := testutil.FakeSSH(t)
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte("home lan wan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wan := remote("wan")
	id := must(t)(wan.NewSession("api", ""))
	var dialed []string
	f := fleet.New(testutil.NewServer(t), path, "", func(d string) tmux.Client {
		dialed = append(dialed, d)
		if d == "lan" { // unreachable: every call fails
			return tmux.Client{Host: "lan", SSH: "/nonexistent/ssh"}
		}
		return remote(d)
	})
	if h := f.Hosts()[0]; h.Name != "home" || h.Dest != "lan" || len(h.Dests) != 2 {
		t.Fatalf("Hosts = %+v", h)
	}
	f.RefreshHost("home")
	h := f.Hosts()[0]
	if h.State != fleet.Connected || h.Dest != "wan" {
		t.Fatalf("home = %+v", h)
	}
	q := fleet.Qualify("home", id)
	if s, ok := find(list(t, f), q); !ok || s.Name != "api" || s.Host != "home" {
		t.Fatalf("%s not listed: %+v", q, list(t, f))
	}
	if err := f.SetSlot(q, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Resolve(q); err != nil {
		t.Errorf("Resolve through wan: %v", err)
	}

	// The current destination is tried first on the next refresh.
	dialed = nil
	f.RefreshHost("home")
	if len(dialed) != 1 || dialed[0] != "wan" {
		t.Errorf("dialed %v, want only wan", dialed)
	}

	if err := f.SetDests("home", []string{"lan"}); err != nil {
		t.Fatal(err)
	}
	f.RefreshHost("home")
	if h := f.Hosts()[0]; h.State != fleet.Unreachable || h.Dest != "lan" {
		t.Errorf("home after SetDests = %+v", h)
	}
	if s, ok := find(list(t, f), q); !ok || s.Slot != 3 {
		t.Errorf("pin lost: %+v", list(t, f))
	}
	b, _ := os.ReadFile(path)
	if string(b) != "home lan\n" {
		t.Errorf("hosts file = %q", b)
	}
}
