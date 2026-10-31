// Package hosts manages the list of remote ssh destinations.
package hosts

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

// Path returns the hosts file location.
func Path() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "ssmux", "hosts")
}

// Host is a named remote machine reachable through one or more ssh
// destinations, tried in order.
type Host struct {
	Name  string
	Dests []string
}

// line formats h as a hosts file line: "name dest...", or just the name when
// it is the only destination.
func (h Host) line() string {
	if len(h.Dests) == 1 && h.Dests[0] == h.Name {
		return h.Name
	}
	return strings.Join(append([]string{h.Name}, h.Dests...), " ")
}

// parseLine parses "name [dest...]"; a name alone is its own destination.
func parseLine(line string) (Host, bool) {
	f := strings.Fields(line)
	if len(f) == 0 || strings.HasPrefix(f[0], "#") {
		return Host{}, false
	}
	h := Host{Name: f[0], Dests: f[1:]}
	if len(h.Dests) == 0 {
		h.Dests = []string{h.Name}
	}
	return h, h.validate() == nil
}

// Load returns the hosts in path, skipping comments and invalid lines. A
// missing file yields nil.
func Load(path string) ([]Host, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Host
	for line := range strings.SplitSeq(string(b), "\n") {
		if h, ok := parseLine(line); ok {
			out = append(out, h)
		}
	}
	return out, nil
}

func (h Host) validate() error {
	if err := validate("name", h.Name); err != nil {
		return err
	}
	if len(h.Dests) == 0 {
		return errors.New("host has no destination")
	}
	for i, d := range h.Dests {
		if err := validate("destination", d); err != nil {
			return err
		}
		if slices.Contains(h.Dests[:i], d) {
			return errors.New("duplicate destination " + d)
		}
	}
	return nil
}

func validate(what, s string) error {
	switch {
	case s == "":
		return errors.New(what + " is empty")
	case strings.HasPrefix(s, "-"):
		return errors.New(what + " must not start with '-'")
	case strings.HasPrefix(s, "#"):
		return errors.New(what + " must not start with '#'")
	case strings.Contains(s, "/"):
		return errors.New(what + " must not contain '/'")
	case strings.Contains(s, ","):
		return errors.New(what + " must not contain ','")
	case strings.IndexFunc(s, unicode.IsSpace) >= 0:
		return errors.New(what + " must not contain whitespace")
	}
	return nil
}

// Parse parses user input "name [dest...]" into a host.
func Parse(s string) (Host, error) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return Host{}, errors.New("name is empty")
	}
	h := Host{Name: f[0], Dests: f[1:]}
	if len(h.Dests) == 0 {
		h.Dests = []string{h.Name}
	}
	return h, h.validate()
}

// Add appends h to path, creating the file and its directories.
func Add(path string, h Host) error {
	if err := h.validate(); err != nil {
		return err
	}
	cur, err := Load(path)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(cur, func(c Host) bool { return c.Name == h.Name }) {
		return errors.New("host already added")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	line := h.line() + "\n"
	if len(b) > 0 && b[len(b)-1] != '\n' {
		line = "\n" + line
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(line); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Remove drops the lines of the host named name and keeps the rest of the
// file as is.
func Remove(path, name string) error {
	return rewrite(path, name, "")
}

// SetDests replaces the destinations of the host named name.
func SetDests(path, name string, dests []string) error {
	h := Host{Name: name, Dests: dests}
	if err := h.validate(); err != nil {
		return err
	}
	return rewrite(path, name, h.line()+"\n")
}

// rewrite replaces the lines of the host named name with repl, keeping the
// rest of the file as is.
func rewrite(path, name, repl string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(b), "\n")
	var kept strings.Builder
	for _, l := range lines {
		if h, ok := parseLine(l); ok && h.Name == name {
			kept.WriteString(repl)
			repl = ""
			continue
		}
		kept.WriteString(l)
	}
	return os.WriteFile(path, []byte(kept.String()), 0o644)
}
