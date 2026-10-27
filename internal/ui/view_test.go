package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rknit/ssmux/internal/tmux"
)

func sized(m Model, w, h int) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(Model)
}

func plain(m Model) string { return ansi.Strip(m.render()) }

func lineWith(out, s string) string {
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, s) {
			return line
		}
	}
	return ""
}

func TestViewSections(t *testing.T) {
	f := newFake()
	f.sessions[0].Slot = 3 // foo
	f.sessions[1].Slot = 5 // bar
	out := plain(sized(New(f, "c"), 100, 30))
	for _, h := range []string{"RECENT", "PINNED", "UNPINNED"} {
		if !strings.Contains(out, h) {
			t.Errorf("missing %s header", h)
		}
	}
	if !strings.Contains(out, "3  foo") || !strings.Contains(out, "5  bar") {
		t.Errorf("pinned rows missing slot numbers:\n%s", out)
	}
	if strings.Contains(out, " 4  ") {
		t.Errorf("empty slot 4 rendered:\n%s", out)
	}
}

func TestViewMarkers(t *testing.T) {
	out := plain(sized(New(newFake(), "c"), 100, 30))
	if !strings.Contains(lineWith(out, "foo"), "●") {
		t.Errorf("current session not marked:\n%s", out)
	}
	if !strings.Contains(lineWith(out, "▸"), "bar") {
		t.Errorf("cursor row not marked:\n%s", out)
	}
}

func TestViewFooter(t *testing.T) {
	m := sized(New(newFake(), "c"), 100, 30)
	out := plain(m)
	for _, k := range []string{"↑↓", "⏎", "⇧↑↓", "p", "n", "r", "x", "/", "q"} {
		if !strings.Contains(out, k) {
			t.Errorf("footer missing %q", k)
		}
	}
	m, _ = press(t, m, "p")
	out = plain(m)
	for _, k := range []string{"1-9", "⏎ first free", "0 unpin", "Esc"} {
		if !strings.Contains(out, k) {
			t.Errorf("pin footer missing %q:\n%s", k, out)
		}
	}
}

func TestViewPreviewAndWidth(t *testing.T) {
	f := newFake()
	f.capture = strings.Repeat("x", 300) + "\nline two"
	m := tickPreview(t, sized(New(f, "c"), 60, 20))
	out := plain(m)
	if !strings.Contains(out, "line two") {
		t.Errorf("preview not shown:\n%s", out)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if w := ansi.StringWidth(line); w > 60 {
			t.Errorf("line width %d > 60: %q", w, line)
		}
	}
	if n := strings.Count(out, "\n") + 1; n > 20 {
		t.Errorf("%d lines > height 20", n)
	}
}

func TestViewEmpty(t *testing.T) {
	f := newFake()
	f.sessions = []tmux.Session{}
	_ = plain(sized(New(f, "c"), 40, 10)) // must not panic
	_ = plain(New(f, "c"))                // no size yet
}

func TestRemoteRowLabel(t *testing.T) {
	f := newFake()
	f.sessions = []tmux.Session{
		{ID: "$1", Name: "loc", LastAttached: 50},
		{ID: "box/$2", Name: "api", Host: "box", LastAttached: 40},
	}
	out := plain(sized(New(f, "c"), 100, 30))
	if l := lineWith(out, "api"); !strings.Contains(l, "@box") {
		t.Errorf("remote row = %q, want @box", l)
	}
	if l := lineWith(out, "loc"); strings.Contains(l, "@") {
		t.Errorf("local row = %q has a host", l)
	}
}
