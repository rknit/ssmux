package ui

import (
	"slices"
	"sort"
	"strings"

	"github.com/rknit/ssmux/internal/tmux"
)

type Section int

const (
	SectionRecent Section = iota
	SectionPinned
	SectionUnpinned
)

// Row is one line of the session list: a section header or a session.
type Row struct {
	Header  bool
	Section Section
	Session tmux.Session
	Current bool
}

// BuildRows lays out RECENT (every session ever attached, newest first,
// excluding the current session), PINNED (occupied slots only, by slot) and
// UNPINNED (local sessions, then each host's in hosts order, by name).
// Sections with no sessions get no header. filter matches names or hosts
// case-insensitively; while filtering, each match is listed once, pinned
// then unpinned, with no headers or RECENT.
func BuildRows(ss []tmux.Session, currentID, filter string, hosts []string) []Row {
	filter = strings.ToLower(filter)
	var kept []tmux.Session
	for _, s := range ss {
		if strings.Contains(strings.ToLower(s.Name), filter) || strings.Contains(strings.ToLower(s.Host), filter) {
			kept = append(kept, s)
		}
	}
	// rank orders hosts: local first, then config order, then unknown hosts.
	rank := func(host string) int {
		if host == "" {
			return 0
		}
		if i := slices.Index(hosts, host); i >= 0 {
			return i + 1
		}
		return len(hosts) + 1
	}

	var recent, pinned, unpinned []tmux.Session
	for _, s := range kept {
		if s.ID != currentID && s.LastAttached > 0 {
			recent = append(recent, s)
		}
		if s.Slot != 0 {
			pinned = append(pinned, s)
		} else {
			unpinned = append(unpinned, s)
		}
	}
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].LastAttached > recent[j].LastAttached })
	sort.SliceStable(pinned, func(i, j int) bool { return pinned[i].Slot < pinned[j].Slot })
	sort.SliceStable(unpinned, func(i, j int) bool {
		a, b := unpinned[i], unpinned[j]
		if ra, rb := rank(a.Host), rank(b.Host); ra != rb {
			return ra < rb
		}
		if a.Host != b.Host {
			return a.Host < b.Host
		}
		return a.Name < b.Name
	})

	var rows []Row
	add := func(sec Section, list []tmux.Session) {
		if len(list) == 0 {
			return
		}
		if filter != "" {
			if sec == SectionRecent {
				return
			}
		} else {
			rows = append(rows, Row{Header: true, Section: sec})
		}
		for _, s := range list {
			rows = append(rows, Row{Section: sec, Session: s, Current: s.ID == currentID})
		}
	}
	add(SectionRecent, recent)
	add(SectionPinned, pinned)
	add(SectionUnpinned, unpinned)
	return rows
}

// FirstSelectable returns the index of the first session row, or -1.
func FirstSelectable(rows []Row) int {
	for i, r := range rows {
		if !r.Header {
			return i
		}
	}
	return -1
}
