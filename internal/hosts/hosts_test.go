package hosts

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSkipsCommentsAndBlanks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts")
	write(t, p, "# c\n\n a \nb\n")
	got, err := Load(p)
	if err != nil || !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestLoadMissing(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "nope"))
	if got != nil || err != nil {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestAddCreatesDir(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x", "y", "hosts")
	if err := Add(p, "box"); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || !reflect.DeepEqual(got, []string{"box"}) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestAddRejects(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts")
	if err := Add(p, "box"); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"ssh://h", "a b", "a,b", "", "box"} {
		if err := Add(p, d); err == nil {
			t.Errorf("Add(%q) succeeded", d)
		}
	}
}

func TestRemoveKeepsComments(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts")
	write(t, p, "# c\nbox\nci\n")
	if err := Remove(p, "box"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "# c\nci\n" {
		t.Fatalf("got %q", b)
	}
}
