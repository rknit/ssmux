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

// Load returns the destinations in path, skipping comments and invalid
// lines. A missing file yields nil.
func Load(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for line := range strings.SplitSeq(string(b), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") && validate(line) == nil {
			out = append(out, line)
		}
	}
	return out, nil
}

func validate(dest string) error {
	switch {
	case dest == "":
		return errors.New("host is empty")
	case strings.HasPrefix(dest, "-"):
		return errors.New("host must not start with '-'")
	case strings.HasPrefix(dest, "#"):
		return errors.New("host must not start with '#'")
	case strings.Contains(dest, "/"):
		return errors.New("host must not contain '/'")
	case strings.Contains(dest, ","):
		return errors.New("host must not contain ','")
	case strings.IndexFunc(dest, unicode.IsSpace) >= 0:
		return errors.New("host must not contain whitespace")
	}
	return nil
}

// Add appends dest to path, creating the file and its directories.
func Add(path, dest string) error {
	if err := validate(dest); err != nil {
		return err
	}
	cur, err := Load(path)
	if err != nil {
		return err
	}
	if slices.Contains(cur, dest) {
		return errors.New("host already added")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	line := dest + "\n"
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

// Remove drops lines equal to dest and keeps the rest of the file as is.
func Remove(path, dest string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(b), "\n")
	var kept strings.Builder
	for _, l := range lines {
		if strings.TrimSpace(l) != dest {
			kept.WriteString(l)
		}
	}
	return os.WriteFile(path, []byte(kept.String()), 0o644)
}
