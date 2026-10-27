package fleet

import "testing"

func TestEntriesRoundTrip(t *testing.T) {
	s := "box,$3,100,2 ci,$1,7,9"
	es := ParseEntries(s)
	if len(es) != 2 {
		t.Fatalf("got %d entries, want 2", len(es))
	}
	if got := FormatEntries(es); got != s {
		t.Errorf("FormatEntries = %q, want %q", got, s)
	}
	if es := ParseEntries("junk a,b"); len(es) != 0 {
		t.Errorf("malformed parsed to %d entries, want 0", len(es))
	}
}

func TestUpsertReplacesSameSession(t *testing.T) {
	es := []Entry{{"box", "$3", 100, 2}}
	for _, e := range []Entry{{"box", "$3", 100, 5}, {"box", "$3", 200, 5}} {
		got := Upsert(es, e)
		if len(got) != 1 || got[0] != e {
			t.Errorf("Upsert(%v) = %v", e, got)
		}
	}
}

func TestWithout(t *testing.T) {
	es := []Entry{{"a", "$1", 1, 1}, {"b", "$2", 2, 2}}
	got := Without(es, func(e Entry) bool { return e.Host == "b" })
	if len(got) != 1 || got[0].Host != "b" {
		t.Errorf("Without = %v", got)
	}
}

func TestNewest(t *testing.T) {
	var es []Entry
	for i := 0; i < 60; i++ {
		es = append(es, Entry{"h", "$" + string(rune('a'+i)), int64(i), int64(i)})
	}
	got := Newest(es, 50)
	if len(got) != 50 {
		t.Fatalf("len = %d, want 50", len(got))
	}
	for _, e := range got {
		if e.Value < 10 {
			t.Errorf("kept old entry %v", e)
		}
	}
}
