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
	if err != nil || !reflect.DeepEqual(got, []Host{{"a", []string{"a"}}, {"b", []string{"b"}}}) {
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
	if err := Add(p, Host{"box", []string{"box"}}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || !reflect.DeepEqual(got, []Host{{"box", []string{"box"}}}) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestAddRejects(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts")
	if err := Add(p, Host{"box", []string{"box"}}); err != nil {
		t.Fatal(err)
	}
	for _, h := range []Host{
		{"ssh://h", []string{"h"}}, {"a b", []string{"a"}}, {"a,b", []string{"a"}}, {"", []string{"a"}},
		{"box", []string{"x"}}, {"-oProxyCommand=x", []string{"a"}}, {"#box", []string{"a"}},
		{"n", nil}, {"n", []string{"-oProxyCommand=x"}}, {"n", []string{"a,b"}}, {"n", []string{"a", "a"}},
	} {
		if err := Add(p, h); err == nil {
			t.Errorf("Add(%v) succeeded", h)
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

func TestLoadSkipsInvalid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts")
	write(t, p, "a\nb/c\nd,e\nf -oX=y\n-oX=y\ng\n")
	got, err := Load(p)
	if err != nil || !reflect.DeepEqual(got, []Host{{"a", []string{"a"}}, {"g", []string{"g"}}}) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestLoadDests(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts")
	write(t, p, "home  192.168.1.5\tu@example.com\n")
	got, err := Load(p)
	want := []Host{{"home", []string{"192.168.1.5", "u@example.com"}}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestSetDests(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts")
	write(t, p, "# c\nbox\nci a b\n")
	if err := SetDests(p, "box", []string{"lan", "wan"}); err != nil {
		t.Fatal(err)
	}
	if err := SetDests(p, "ci", []string{"ci"}); err != nil {
		t.Fatal(err)
	}
	if err := SetDests(p, "ci", nil); err == nil {
		t.Error("SetDests accepted no destination")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "# c\nbox lan wan\nci\n" {
		t.Fatalf("got %q", b)
	}
}

func TestParse(t *testing.T) {
	h, err := Parse("  home lan  wan ")
	if err != nil || !reflect.DeepEqual(h, Host{"home", []string{"lan", "wan"}}) {
		t.Fatalf("got %v, %v", h, err)
	}
	h, err = Parse("u@h")
	if err != nil || !reflect.DeepEqual(h, Host{"u@h", []string{"u@h"}}) {
		t.Fatalf("got %v, %v", h, err)
	}
	if _, err := Parse("  "); err == nil {
		t.Error("Parse accepted blank input")
	}
}
