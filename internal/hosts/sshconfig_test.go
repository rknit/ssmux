package hosts

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestSuggestions(t *testing.T) {
	d := t.TempDir()
	write(t, filepath.Join(d, "config"), "Host a b\n  HostName x\nhost *.x c?\nHost !d e\nMatch host z\n  User q\n  Host y\nInclude conf.d/*\nHost=g\nhost h\ninclude conf.d/2\nHost i # work box\nHost \"my box\" j\nInclude conf.d/3 # x\n")
	write(t, filepath.Join(d, "conf.d", "1"), "Host f\n")
	write(t, filepath.Join(d, "conf.d", "2"), "Host k\n")
	write(t, filepath.Join(d, "conf.d", "3"), "Host l\n")
	got := Suggestions(d, []string{"b"})
	want := []string{"a", "e", "f", "g", "h", "i", "j", "k", "l", "y"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSuggestionsNoConfig(t *testing.T) {
	if got := Suggestions(t.TempDir(), nil); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}
