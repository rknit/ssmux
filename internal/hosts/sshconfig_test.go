package hosts

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestSuggestions(t *testing.T) {
	d := t.TempDir()
	write(t, filepath.Join(d, "config"), "Host a b\n  HostName x\nhost *.x c?\nHost !d e\nMatch host z\n  User q\n  Host y\nInclude conf.d/*\nHost=g\n")
	write(t, filepath.Join(d, "conf.d", "1"), "Host f\n")
	got := Suggestions(d, []string{"b", "g"})
	want := []string{"a", "e", "f", "y"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSuggestionsNoConfig(t *testing.T) {
	if got := Suggestions(t.TempDir(), nil); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}
