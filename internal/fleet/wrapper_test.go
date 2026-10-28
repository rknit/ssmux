package fleet_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rknit/ssmux/internal/fleet"
	"github.com/rknit/ssmux/internal/tmux"
)

// output is a goroutine-safe buffer for a control-mode client's stdout.
type output struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.Write(p)
}

func (o *output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.String()
}

// attach starts a control-mode client on session and returns its name and
// output.
func attach(t *testing.T, c tmux.Client, session string) (string, *output) {
	t.Helper()
	before, _ := c.Run("list-clients", "-F", "#{client_name}")
	cmd := exec.Command("tmux", "-S", c.Socket, "-C", "attach", "-t", session)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out := &output{}
	cmd.Stdout = out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		cmd.Process.Kill()
		cmd.Wait()
	})
	var name string
	eventually(t, func() bool {
		all, _ := c.Run("list-clients", "-F", "#{client_name}")
		for n := range strings.SplitSeq(all, "\n") {
			if n != "" && !strings.Contains(before, n) {
				name = n
				return true
			}
		}
		return false
	})
	return name, out
}

func eventually(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("condition not met within 2s")
}

// switchEnv is a fleet with box refreshed, a remote session on box and a
// control-mode client attached to the local "base" session.
type switchEnv struct {
	*env
	f       *fleet.Fleet
	id, q   string
	created int64
	client  string
	out     *output
}

func newSwitchEnv(t *testing.T) *switchEnv {
	t.Helper()
	e := newEnv(t)
	id := must(t)(e.remote("box").NewSession("api", ""))
	f := e.fleet()
	f.Term = "xterm-256color"
	f.RefreshHost("box")
	s, ok := find(list(t, f), "box/"+id)
	if !ok {
		t.Fatal("remote session not listed")
	}
	client, out := attach(t, e.local, "base")
	return &switchEnv{env: e, f: f, id: id, q: "box/" + id, created: s.Created, client: client, out: out}
}

// wrappers returns local session ID -> @ssmux-remote value.
func wrappers(t *testing.T, c tmux.Client) map[string]string {
	t.Helper()
	tags, err := c.SessionTags("@ssmux-remote")
	if err != nil {
		t.Fatal(err)
	}
	return tags
}

func (s *switchEnv) tag() string {
	return "box," + s.id + "," + strconv.FormatInt(s.created, 10)
}

func (s *switchEnv) switchTo(t *testing.T) {
	t.Helper()
	if err := s.f.SwitchClient(s.client, s.q); err != nil {
		t.Fatal(err)
	}
}

// wrapperID returns the only wrapper's local ID.
func (s *switchEnv) wrapperID(t *testing.T) string {
	t.Helper()
	w := wrappers(t, s.local)
	if len(w) != 1 {
		t.Fatalf("wrappers = %v", w)
	}
	for id := range w {
		return id
	}
	return ""
}

func TestSwitchCreatesWrapperOnce(t *testing.T) {
	s := newSwitchEnv(t)
	s.switchTo(t)
	s.switchTo(t)
	w := wrappers(t, s.local)
	if len(w) != 1 {
		t.Fatalf("wrappers = %v", w)
	}
	id := s.wrapperID(t)
	if w[id] != s.tag() {
		t.Errorf("@ssmux-remote = %q, want %q", w[id], s.tag())
	}
	if name, _ := s.local.Run("display", "-p", "-t", id, "#{session_name}"); !strings.HasPrefix(name, "_ssmux_") {
		t.Errorf("wrapper name = %q", name)
	}
	for opt, want := range map[string]string{
		"status": "off", "prefix": "None", "prefix2": "None", "detach-on-destroy": "off",
	} {
		if v, _ := s.local.Run("show-options", "-v", "-t", id, opt); v != want {
			t.Errorf("%s = %q, want %q", opt, v, want)
		}
	}
	if got, _ := s.local.ClientSession(s.client); got != id {
		t.Errorf("client on %q, want wrapper %q", got, id)
	}
}

func TestSwitchRecordsRecent(t *testing.T) {
	s := newSwitchEnv(t)
	s.switchTo(t)
	es := fleet.ParseEntries(option(t, s.local, recentOpt))
	if len(es) != 1 || es[0].Key() != s.tag() {
		t.Fatalf("%s = %+v", recentOpt, es)
	}
	if d := time.Now().Unix() - es[0].Value; d < 0 || d > 5 {
		t.Errorf("recent time %d is %ds from now", es[0].Value, d)
	}
	if got, _ := find(list(t, s.f), s.q); got.LastAttached != es[0].Value {
		t.Errorf("LastAttached = %d, want %d", got.LastAttached, es[0].Value)
	}
}

func TestSwitchKeepsNewestRecent(t *testing.T) {
	s := newSwitchEnv(t)
	var old []fleet.Entry
	for i := range 60 {
		old = append(old, fleet.Entry{Host: "ci", ID: "$" + strconv.Itoa(i), Created: 1, Value: int64(1000 + i)})
	}
	if err := s.local.SetGlobalOption(recentOpt, fleet.FormatEntries(old)); err != nil {
		t.Fatal(err)
	}
	s.switchTo(t)
	es := fleet.ParseEntries(option(t, s.local, recentOpt))
	if len(es) != 50 || es[0].Key() != s.tag() {
		t.Fatalf("%d entries, first %+v", len(es), es[0])
	}
}

func TestSwitchUncachedSession(t *testing.T) {
	s := newSwitchEnv(t)
	q := must(t)(s.f.NewSession("box", "fresh"))
	if err := s.f.SwitchClient(s.client, q); err != nil {
		t.Fatal(err)
	}
	got, ok := find(list(t, s.f), q)
	if !ok || got.Name != "fresh" || got.LastAttached == 0 {
		t.Errorf("listed %+v, ok=%v", got, ok)
	}
}

// TestSwitchLiveWrapperSkipsSSH switches a fresh fleet, which only knows the
// session from its pin, to a session whose wrapper is live, with ssh broken.
func TestSwitchLiveWrapperSkipsSSH(t *testing.T) {
	s := newSwitchEnv(t)
	s.switchTo(t)
	w := s.wrapperID(t)
	if err := s.f.SetSlot(s.q, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.local.SwitchClient(s.client, "base"); err != nil {
		t.Fatal(err)
	}
	f := fleet.New(s.local, s.hostsPath, "", func(h string) tmux.Client {
		c := s.remote(h)
		c.SSH = "false"
		return c
	})
	if err := f.SwitchClient(s.client, s.q); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.local.ClientSession(s.client); got != w {
		t.Errorf("client on %q, want wrapper %q", got, w)
	}
}

// slowRefresh is a fleet whose next RefreshHost, once armed, lists the
// sessions and then waits for resume before returning the listing.
type slowRefresh struct {
	f      *fleet.Fleet
	client string
	dir    string
	slow   atomic.Value // "" or the directory of the armed refresh
}

func newSlowRefresh(t *testing.T, e *env) *slowRefresh {
	t.Helper()
	r := &slowRefresh{dir: t.TempDir()}
	r.slow.Store("")
	r.f = fleet.New(e.local, e.hostsPath, "", func(h string) tmux.Client {
		c := e.remote(h)
		if d := r.slow.Swap(""); d != "" {
			c.SSH = filepath.Join(d.(string), "ssh")
		}
		return c
	})
	r.f.Term = "xterm-256color"
	r.f.RefreshHost("box")
	r.client, _ = attach(t, e.local, "base")
	return r
}

// start begins a slow refresh of box in the background, waits until it has
// listed, and returns a function that lets it finish and waits for it.
func (r *slowRefresh) start(t *testing.T, e *env, name string) (finish func()) {
	t.Helper()
	d := filepath.Join(r.dir, name)
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	listed, resume := filepath.Join(d, "listed"), filepath.Join(d, "resume")
	script := "#!/bin/sh\nout=$(" + e.remote("box").SSH + " \"$@\")\ntouch " + listed +
		"\nwhile [ ! -f " + resume + " ]; do sleep 0.02; done\nprintf '%s\\n' \"$out\"\n"
	if err := os.WriteFile(filepath.Join(d, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r.slow.Store(d)
	done := make(chan struct{})
	go func() { r.f.RefreshHost("box"); close(done) }()
	eventually(t, func() bool { _, err := os.Stat(listed); return err == nil })
	return func() {
		t.Helper()
		if err := os.WriteFile(resume, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		<-done
	}
}

// switchFresh creates a box session through the fleet and switches to it.
func (r *slowRefresh) switchFresh(t *testing.T) string {
	t.Helper()
	q := must(t)(r.f.NewSession("box", "fresh"))
	if err := r.f.SwitchClient(r.client, q); err != nil {
		t.Fatal(err)
	}
	return q
}

func (r *slowRefresh) checkKept(t *testing.T, e *env, q string) {
	t.Helper()
	_, id := fleet.Split(q)
	if es := fleet.ParseEntries(option(t, e.local, recentOpt)); len(es) != 1 || es[0].ID != id {
		t.Errorf("%s = %+v", recentOpt, es)
	}
	if got, ok := find(list(t, r.f), q); !ok || got.LastAttached == 0 {
		t.Errorf("listed %+v, ok=%v", got, ok)
	}
}

// TestRefreshOlderThanSwitchKeepsRecent runs a refresh whose listing was
// taken before a switch to a new session, and checks the refresh doesn't
// drop the switch's recent entry.
func TestRefreshOlderThanSwitchKeepsRecent(t *testing.T) {
	e := newEnv(t)
	r := newSlowRefresh(t, e)
	finish := r.start(t, e, "a")
	q := r.switchFresh(t)
	finish()
	r.checkKept(t, e, q)

	// A refresh that starts after the switch is authoritative again.
	_, id := fleet.Split(q)
	if _, err := e.remote("box").Run("kill-session", "-t", id); err != nil {
		t.Fatal(err)
	}
	r.f.RefreshHost("box")
	if _, ok := find(list(t, r.f), q); ok {
		t.Error("killed session still listed")
	}
	if v := option(t, e.local, recentOpt); v != "" {
		t.Errorf("%s = %q", recentOpt, v)
	}
}

// TestOverlappingRefreshesKeepRecent finishes a refresh begun before a
// switch after a later refresh that already saw the switched session.
func TestOverlappingRefreshesKeepRecent(t *testing.T) {
	e := newEnv(t)
	r := newSlowRefresh(t, e)
	finishA := r.start(t, e, "a")
	q := r.switchFresh(t)
	r.f.RefreshHost("box")
	finishA()
	r.checkKept(t, e, q)
}

func TestClientSessionMapsWrapper(t *testing.T) {
	s := newSwitchEnv(t)
	s.switchTo(t)
	if got := must(t)(s.f.ClientSession(s.client)); got != s.q {
		t.Errorf("ClientSession = %q, want %q", got, s.q)
	}
	base := must(t)(s.local.Run("display", "-p", "-t", "base", "#{session_id}"))
	if err := s.f.SwitchClient(s.client, base); err != nil {
		t.Fatal(err)
	}
	if got := must(t)(s.f.ClientSession(s.client)); got != base {
		t.Errorf("ClientSession = %q, want %q", got, base)
	}
}

func startCommand(t *testing.T, c tmux.Client, id string) string {
	t.Helper()
	return must(t)(c.Run("display", "-p", "-t", id, "#{pane_start_command}"))
}

func TestTermFallback(t *testing.T) {
	s := newSwitchEnv(t)
	s.f.Term = "no-such-term-xyz"
	s.switchTo(t)
	if cmd := startCommand(t, s.local, s.wrapperID(t)); !strings.Contains(cmd, "TERM=screen-256color") {
		t.Errorf("start command = %q", cmd)
	}
	msg := "%message ssmux: box has no terminfo for no-such-term-xyz, using screen-256color\n"
	eventually(t, func() bool { return strings.Contains(s.out.String(), msg) })
	if h := s.f.Hosts(); !h[0].TermFallback || h[1].TermFallback {
		t.Errorf("Hosts = %+v", h)
	}
}

func TestTermSupported(t *testing.T) {
	s := newSwitchEnv(t)
	s.switchTo(t)
	if cmd := startCommand(t, s.local, s.wrapperID(t)); !strings.Contains(cmd, "TERM=xterm-256color") {
		t.Errorf("start command = %q", cmd)
	}
	if strings.Contains(s.out.String(), "%message") {
		t.Errorf("unexpected message in %q", s.out.String())
	}
	if h := s.f.Hosts(); h[0].TermFallback {
		t.Errorf("Hosts = %+v", h)
	}
}

func TestWrapperAttachesRemote(t *testing.T) {
	s := newSwitchEnv(t)
	s.switchTo(t)
	eventually(t, func() bool {
		cs, _ := s.remote("box").Run("list-clients", "-F", "#{session_id}")
		return cs == s.id
	})
}

func TestDeadWrapperReplaced(t *testing.T) {
	s := newSwitchEnv(t)
	if _, err := s.local.Run("set-option", "-g", "remain-on-exit", "on"); err != nil {
		t.Fatal(err)
	}
	dead := must(t)(s.local.Run("new-session", "-d", "-P", "-F", "#{session_id}", "-s", "_ssmux_1", "exit 1",
		";", "set-option", "@ssmux-remote", s.tag()))
	eventually(t, func() bool {
		v, _ := s.local.Run("display", "-p", "-t", dead, "#{pane_dead}")
		return v == "1"
	})
	s.switchTo(t)
	w := s.wrapperID(t)
	if w == dead {
		t.Fatal("dead wrapper reused")
	}
	if v, _ := s.local.Run("display", "-p", "-t", w, "#{pane_dead}"); v != "0" {
		t.Errorf("pane_dead = %q", v)
	}
	if got, _ := s.local.ClientSession(s.client); got != w {
		t.Errorf("client on %q, want %q", got, w)
	}
}

func TestWrapperClosesWhenRemoteKilled(t *testing.T) {
	s := newSwitchEnv(t)
	s.switchTo(t)
	s.wrapperID(t)
	if _, err := s.remote("box").Run("kill-session", "-t", s.id); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return len(wrappers(t, s.local)) == 0 })
}

func TestRemoteDetachDetachesClient(t *testing.T) {
	s := newSwitchEnv(t)
	s.switchTo(t)
	s.wrapperID(t)
	eventually(t, func() bool {
		cs, _ := s.remote("box").Run("list-clients", "-F", "#{session_id}")
		return cs == s.id
	})
	if _, err := s.remote("box").Run("detach-client", "-s", s.id); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		cs, _ := s.local.Run("list-clients", "-F", "#{client_name}")
		return !strings.Contains(cs, s.client)
	})
}

func TestLastRemote(t *testing.T) {
	e := newEnv(t)
	f := e.fleet()
	if _, ok := f.LastRemote(); ok {
		t.Fatal("LastRemote with no RECENT entries")
	}
	if _, err := e.local.Run("set-option", "-g", recentOpt, "box,$1,10,300 ci,$2,20,500 box,$3,30,400"); err != nil {
		t.Fatal(err)
	}
	if got, ok := f.LastRemote(); !ok || got != (fleet.Entry{Host: "ci", ID: "$2", Created: 20, Value: 500}) {
		t.Errorf("LastRemote = %+v, %v", got, ok)
	}
}

func TestWrap(t *testing.T) {
	s := newSwitchEnv(t)
	w, err := s.f.Wrap(fleet.Entry{Host: "box", ID: s.id, Created: s.created})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.wrapperID(t); got != w {
		t.Errorf("Wrap = %q, wrapper %q", w, got)
	}
	if e, ok := s.f.LastRemote(); !ok || e.ID != s.id || e.Value == 0 {
		t.Errorf("LastRemote = %+v, %v", e, ok)
	}
	if _, err := s.f.Wrap(fleet.Entry{Host: "box", ID: s.id, Created: s.created + 1}); err == nil {
		t.Error("Wrap of a reused ID succeeded")
	}
}

func TestKillWrappedKeepsClient(t *testing.T) {
	s := newSwitchEnv(t)
	s.switchTo(t)
	w := s.wrapperID(t)
	if err := s.f.KillSession(s.q); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return len(wrappers(t, s.local)) == 0 })
	got, err := s.local.ClientSession(s.client)
	if err != nil || got == "" || got == w {
		t.Errorf("client session = %q, %v", got, err)
	}
}

func TestRemoveHostKillsWrappers(t *testing.T) {
	s := newSwitchEnv(t)
	s.switchTo(t)
	ci := must(t)(s.remote("ci").NewSession("ci", ""))
	if err := s.f.SwitchClient(s.client, "ci/"+ci); err != nil {
		t.Fatal(err)
	}
	if err := s.f.RemoveHost("box"); err != nil {
		t.Fatal(err)
	}
	w := wrappers(t, s.local)
	if len(w) != 1 {
		t.Fatalf("wrappers = %v", w)
	}
	for _, v := range w {
		if !strings.HasPrefix(v, "ci,") {
			t.Errorf("remaining wrapper %q", v)
		}
	}
	if got, _ := s.local.ClientSession(s.client); got == "" {
		t.Error("client detached")
	}
}

func TestSwitchLocal(t *testing.T) {
	s := newSwitchEnv(t)
	id := must(t)(s.local.NewSession("other", ""))
	if err := s.f.SwitchClient(s.client, id); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.local.ClientSession(s.client); got != id {
		t.Errorf("client on %q, want %q", got, id)
	}
}

func TestResolve(t *testing.T) {
	s := newSwitchEnv(t)
	got, err := s.f.Resolve(s.q)
	if err != nil || got.ID != s.q || got.Created != s.created || got.Host != "box" || got.Name != "api" {
		t.Errorf("Resolve = %+v, %v", got, err)
	}
	if _, err := s.f.Resolve("box/$99"); err == nil {
		t.Error("Resolve of missing session succeeded")
	}
}

func TestDisplay(t *testing.T) {
	s := newSwitchEnv(t)
	if err := s.f.Display(s.client, "hi there"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return strings.Contains(s.out.String(), "%message hi there\n") })
}
