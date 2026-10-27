package tmux

import (
	"os"
	"strings"
	"testing"
	"time"
)

func mustNew(t *testing.T, c Client, name string) string {
	t.Helper()
	id, err := c.NewSession(name, "/tmp")
	if err != nil {
		t.Fatalf("NewSession(%q): %v", name, err)
	}
	return id
}

func byID(t *testing.T, c Client, id string) Session {
	t.Helper()
	ss, err := c.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range ss {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("session %s not found in %+v", id, ss)
	return Session{}
}

func TestListSessionsAndSlots(t *testing.T) {
	c := newTestServer(t)
	foo := mustNew(t, c, "foo")
	bar := mustNew(t, c, "bar")
	if err := c.SetSlot(foo, 3); err != nil {
		t.Fatal(err)
	}
	ss, err := c.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 3 {
		t.Fatalf("got %d sessions, want 3", len(ss))
	}
	if s := byID(t, c, foo); s.Slot != 3 || s.Name != "foo" || s.Windows != 1 {
		t.Errorf("foo = %+v", s)
	}
	if s := byID(t, c, bar); s.Slot != 0 {
		t.Errorf("bar.Slot = %d, want 0", s.Slot)
	}
	if err := c.SetSlot(foo, 0); err != nil {
		t.Fatal(err)
	}
	if s := byID(t, c, foo); s.Slot != 0 {
		t.Errorf("after unpin foo.Slot = %d", s.Slot)
	}
}

func TestSlotSurvivesRename(t *testing.T) {
	c := newTestServer(t)
	foo := mustNew(t, c, "foo")
	c.SetSlot(foo, 2)
	if err := c.RenameSession(foo, "renamed"); err != nil {
		t.Fatal(err)
	}
	if s := byID(t, c, foo); s.Name != "renamed" || s.Slot != 2 {
		t.Errorf("got %+v", s)
	}
}

func TestNamesWithSpecialChars(t *testing.T) {
	c := newTestServer(t)
	id := mustNew(t, c, "my project")
	if s := byID(t, c, id); s.Name != "my project" {
		t.Errorf("name = %q", s.Name)
	}
	if err := c.SetSlot(id, 1); err != nil {
		t.Fatal(err)
	}
	if err := c.RenameSession(id, "v1 2"); err != nil {
		t.Fatal(err)
	}
	if s := byID(t, c, id); s.Name != "v1 2" || s.Slot != 1 {
		t.Errorf("got %+v", s)
	}
	if err := c.KillSession(id); err != nil {
		t.Fatal(err)
	}
	ss, _ := c.ListSessions()
	for _, s := range ss {
		if s.ID == id {
			t.Errorf("session still exists")
		}
	}
}

func TestKillDoesNotTouchOtherSlots(t *testing.T) {
	c := newTestServer(t)
	ids := []string{mustNew(t, c, "a"), mustNew(t, c, "b"), mustNew(t, c, "c")}
	for i, id := range ids {
		c.SetSlot(id, i+1)
	}
	if err := c.KillSession(ids[1]); err != nil {
		t.Fatal(err)
	}
	got := map[int]bool{}
	ss, _ := c.ListSessions()
	for _, s := range ss {
		if s.Slot != 0 {
			got[s.Slot] = true
		}
	}
	if len(got) != 2 || !got[1] || !got[3] {
		t.Errorf("slots = %v, want {1,3}", got)
	}
}

func TestNewSessionDuplicateName(t *testing.T) {
	c := newTestServer(t)
	mustNew(t, c, "foo")
	_, err := c.NewSession("foo", "/tmp")
	if err == nil || !strings.Contains(err.Error(), "duplicate session") {
		t.Errorf("err = %v", err)
	}
}

func TestCapturePane(t *testing.T) {
	c := newTestServer(t)
	id := mustNew(t, c, "cap")
	if _, err := c.Run("send-keys", "-t", id+":", "echo hi-ssmux", "Enter"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		out, err := c.CapturePane(id)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(out, "hi-ssmux") >= 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("capture never showed output:\n%s", out)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestListSessionsCLocale(t *testing.T) {
	t.Setenv("LANG", "C")
	t.Setenv("LC_ALL", "C")
	t.Setenv("LC_CTYPE", "C")
	t.Setenv("TMUX", "")
	os.Unsetenv("TMUX") // any non-empty TMUX makes tmux assume UTF-8
	c := newTestServer(t)
	id := mustNew(t, c, "my project")
	c.SetSlot(id, 4)
	if s := byID(t, c, id); s.Name != "my project" || s.Slot != 4 {
		t.Errorf("got %+v", s)
	}
}

func TestNameWithColon(t *testing.T) {
	c := newTestServer(t)
	id, err := c.NewSession("a:b", "/tmp")
	if err != nil {
		t.Skipf("tmux rejects ':' in names: %v", err)
	}
	c.SetSlot(id, 2)
	if s := byID(t, c, id); s.Slot != 2 || s.Windows != 1 {
		t.Errorf("got %+v", s)
	}
}
