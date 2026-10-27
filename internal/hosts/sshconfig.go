package hosts

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const maxIncludeDepth = 16

// Suggestions lists concrete Host names from sshDir/config and its Includes,
// sorted, unique, and without anything in exclude.
func Suggestions(sshDir string, exclude []string) []string {
	set := map[string]bool{}
	parseConfig(sshDir, filepath.Join(sshDir, "config"), 0, set)
	var out []string
	for h := range set {
		if !slices.Contains(exclude, h) {
			out = append(out, h)
		}
	}
	slices.Sort(out)
	return out
}

func parseConfig(sshDir, file string, depth int, set map[string]bool) {
	if depth > maxIncludeDepth {
		return
	}
	f, err := os.Open(file)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val := splitDirective(sc.Text())
		switch strings.ToLower(key) {
		case "host":
			for _, p := range tokenize(val) {
				if !strings.ContainsAny(p, "*?!") && !strings.ContainsAny(p, " \t") {
					set[p] = true
				}
			}
		case "include":
			for _, p := range tokenize(val) {
				for _, m := range globInclude(sshDir, p) {
					parseConfig(sshDir, m, depth+1, set)
				}
			}
		}
	}
}

// splitDirective splits "Key value" or "Key=value"; comments give "".
func splitDirective(line string) (key, val string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", ""
	}
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return line, ""
	}
	key = line[:i]
	val = strings.TrimLeft(line[i:], " \t")
	val = strings.TrimSpace(strings.TrimPrefix(val, "="))
	return key, val
}

func globInclude(sshDir, pat string) []string {
	switch {
	case strings.HasPrefix(pat, "~/"):
		home, _ := os.UserHomeDir()
		pat = filepath.Join(home, pat[2:])
	case !filepath.IsAbs(pat):
		pat = filepath.Join(sshDir, pat)
	}
	m, _ := filepath.Glob(pat)
	return m
}

// tokenize splits val on whitespace, honoring double quotes and stopping at
// an unquoted token that starts with '#'.
func tokenize(val string) []string {
	var out []string
	var cur strings.Builder
	inTok, quoted, startQuote := false, false, false
	flush := func() bool {
		if !inTok {
			return true
		}
		t := cur.String()
		cur.Reset()
		inTok = false
		if !startQuote && strings.HasPrefix(t, "#") {
			return false
		}
		out = append(out, t)
		return true
	}
	for _, r := range val {
		switch {
		case r == '"':
			if !inTok {
				startQuote = true
			}
			inTok = true
			quoted = !quoted
		case (r == ' ' || r == '\t') && !quoted:
			if !flush() {
				return out
			}
		default:
			if !inTok {
				startQuote = false
			}
			inTok = true
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}
