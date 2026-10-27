package ui

import (
	"reflect"
	"testing"

	"github.com/rknit/ssmux/internal/tmux"
)

type rowView struct {
	Header  bool
	Section Section
	Name    string
	Current bool
}

func view(rows []Row) []rowView {
	var out []rowView
	for _, r := range rows {
		v := rowView{Header: r.Header, Section: r.Section, Current: r.Current}
		if !r.Header {
			v.Name = r.Session.Name
		}
		out = append(out, v)
	}
	return out
}

func sampleSessions() []tmux.Session {
	return []tmux.Session{
		{ID: "$1", Name: "foo", LastAttached: 50, Slot: 1},
		{ID: "$2", Name: "api-server", LastAttached: 40, Slot: 9},
		{ID: "$3", Name: "api-client", LastAttached: 30},
		{ID: "$4", Name: "notes", LastAttached: 20},
		{ID: "$5", Name: "scratch", LastAttached: 10},
	}
}

func TestRowsSections(t *testing.T) {
	got := view(BuildRows(sampleSessions(), "$1", "", nil))
	want := []rowView{
		{Header: true, Section: SectionRecent},
		{Section: SectionRecent, Name: "api-server"},
		{Section: SectionRecent, Name: "api-client"},
		{Section: SectionRecent, Name: "notes"},
		{Header: true, Section: SectionPinned},
		{Section: SectionPinned, Name: "foo", Current: true},
		{Section: SectionPinned, Name: "api-server"},
		{Header: true, Section: SectionUnpinned},
		{Section: SectionUnpinned, Name: "api-client"},
		{Section: SectionUnpinned, Name: "notes"},
		{Section: SectionUnpinned, Name: "scratch"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%+v\nwant\n%+v", got, want)
	}
}

func TestRowsEmptySectionHasNoHeader(t *testing.T) {
	ss := []tmux.Session{{ID: "$1", Name: "a", LastAttached: 2}, {ID: "$2", Name: "b", LastAttached: 1}}
	for _, r := range BuildRows(ss, "$1", "", nil) {
		if r.Section == SectionPinned {
			t.Errorf("unexpected pinned row %+v", r)
		}
	}
}

func TestRowsFilter(t *testing.T) {
	for _, r := range BuildRows(sampleSessions(), "$1", "API", nil) {
		if !r.Header && r.Session.Name != "api-server" && r.Session.Name != "api-client" {
			t.Errorf("filter kept %q", r.Session.Name)
		}
	}
}

func TestRowsSingleSession(t *testing.T) {
	rows := BuildRows([]tmux.Session{{ID: "$1", Name: "only"}}, "$1", "", nil)
	for _, r := range rows {
		if r.Section == SectionRecent {
			t.Errorf("recent should be empty: %+v", r)
		}
	}
	i := FirstSelectable(rows)
	if i < 0 || rows[i].Session.Name != "only" {
		t.Errorf("FirstSelectable = %d", i)
	}
	if FirstSelectable(nil) != -1 {
		t.Errorf("FirstSelectable(nil) != -1")
	}
}

func TestUnpinnedOrder(t *testing.T) {
	ss := []tmux.Session{
		{ID: "ci/$1", Name: "bb", Host: "ci"},
		{ID: "box/$1", Name: "aa", Host: "box"},
		{ID: "$1", Name: "zz"},
	}
	names := func(hosts []string) []string {
		var out []string
		for _, r := range BuildRows(ss, "", "", hosts) {
			if !r.Header && r.Section == SectionUnpinned {
				out = append(out, r.Session.Name)
			}
		}
		return out
	}
	if got, want := names([]string{"box", "ci"}), []string{"zz", "aa", "bb"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := names([]string{"ci", "box"}), []string{"zz", "bb", "aa"}; !reflect.DeepEqual(got, want) {
		t.Errorf("config order: got %v, want %v", got, want)
	}
}

func TestFilterMatchesHost(t *testing.T) {
	ss := []tmux.Session{
		{ID: "$1", Name: "zz"},
		{ID: "box/$1", Name: "aa", Host: "box"},
		{ID: "ci/$1", Name: "bb", Host: "ci"},
	}
	kept := 0
	for _, r := range BuildRows(ss, "", "BOX", []string{"box", "ci"}) {
		if r.Header {
			continue
		}
		kept++
		if r.Session.Host != "box" {
			t.Errorf("filter kept %+v", r.Session)
		}
	}
	if kept == 0 {
		t.Errorf("filter dropped the box rows")
	}
}
