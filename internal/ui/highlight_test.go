package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/rknit/ssmux/internal/fleet"
	"github.com/rknit/ssmux/internal/tmux"
)

var sgrRE = regexp.MustCompile("\x1b\\[([0-9;:]*)m")

// unreversed returns the visible non-space text between the cursor marker
// "▸ " and the next "│" (the preview separator or a box edge) that is drawn
// without reverse video.
func unreversed(line string) string {
	_, line, ok := strings.Cut(line, "▸ ")
	if !ok {
		return "<no cursor>"
	}
	var out strings.Builder
	reverse := false
	for line != "" {
		if loc := sgrRE.FindStringSubmatchIndex(line); loc != nil && loc[0] == 0 {
			params := strings.Split(line[loc[2]:loc[3]], ";")
			for i := 0; i < len(params); i++ {
				switch params[i] {
				case "", "0":
					reverse = false
				case "7":
					reverse = true
				case "27":
					reverse = false
				case "38", "48", "58": // extended color: skip its arguments
					if i+1 < len(params) && params[i+1] == "5" {
						i += 2
					} else if i+1 < len(params) && params[i+1] == "2" {
						i += 4
					}
				}
			}
			line = line[loc[1]:]
			continue
		}
		if strings.HasPrefix(line, "\x1b") { // other escapes
			line = line[1:]
			continue
		}
		r := []rune(line)[0]
		if r == '│' {
			break
		}
		if !reverse && r != ' ' {
			out.WriteRune(r)
		}
		line = line[len(string(r)):]
	}
	return out.String()
}

// styledLine returns the rendered (unstripped) line containing s.
func styledLine(t *testing.T, out, s string) string {
	t.Helper()
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "▸") && strings.Contains(line, s) {
			return line
		}
	}
	t.Fatalf("no selected line with %q:\n%s", s, out)
	return ""
}

func TestSelectedRowStaysReversed(t *testing.T) {
	f := newFake()
	f.sessions = []tmux.Session{
		{ID: "$1", Name: "loc", LastAttached: 50},
		{ID: "box/$2", Name: "api", Host: "box", Slot: 1, LastAttached: 40},
	}
	f.hosts = []fleet.HostStatus{{Name: "box", State: fleet.Connecting}}
	f.current = "box/$2"
	m := sized(New(f, "c"), 100, 30)
	m = cursorOn(t, m, "box/$2")
	// On the pinned row: name, @box, (connecting) and the current dot.
	line := styledLine(t, m.render(), "api")
	if !strings.Contains(ansi.Strip(line), "(connecting)") || !strings.Contains(line, "●") {
		t.Fatalf("row lacks labels: %q", ansi.Strip(line))
	}
	if u := unreversed(line); u != "" {
		t.Errorf("not reversed: %q in %q", u, line)
	}
}

func TestHostPickerDimRowStaysReversed(t *testing.T) {
	f := hostsFake()
	m := cursorOn(t, sized(New(f, "c"), 100, 30), "$1")
	m, _ = press(t, m, "n", "tab", "enter")
	m = typeText(t, m, "ci") // cursor on the unreachable host ci
	line := styledLine(t, m.render(), "unreachable")
	if u := unreversed(line); u != "" {
		t.Errorf("not reversed: %q in %q", u, line)
	}
}
