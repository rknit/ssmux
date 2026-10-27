package ui

import (
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

const recentCount = 3

// Row is one line of the session list: a section header or a session.
type Row struct {
	Header  bool
	Section Section
	Session tmux.Session
	Current bool
}

// BuildRows lays out RECENT (newest 3, excluding the current session),
// PINNED (occupied slots only, by slot) and UNPINNED (by name). Sections
// with no sessions get no header. filter matches names case-insensitively.
func BuildRows(ss []tmux.Session, currentID, filter string) []Row {
	filter = strings.ToLower(filter)
	var kept []tmux.Session
	for _, s := range ss {
		if strings.Contains(strings.ToLower(s.Name), filter) {
			kept = append(kept, s)
		}
	}

	var recent, pinned, unpinned []tmux.Session
	for _, s := range kept {
		if s.ID != currentID {
			recent = append(recent, s)
		}
		if s.Slot != 0 {
			pinned = append(pinned, s)
		} else {
			unpinned = append(unpinned, s)
		}
	}
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].LastAttached > recent[j].LastAttached })
	recent = recent[:min(len(recent), recentCount)]
	sort.SliceStable(pinned, func(i, j int) bool { return pinned[i].Slot < pinned[j].Slot })
	sort.SliceStable(unpinned, func(i, j int) bool { return unpinned[i].Name < unpinned[j].Name })

	var rows []Row
	add := func(sec Section, list []tmux.Session) {
		if len(list) == 0 {
			return
		}
		rows = append(rows, Row{Header: true, Section: sec})
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
