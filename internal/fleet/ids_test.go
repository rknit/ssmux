package fleet

import "testing"

func TestQualifySplit(t *testing.T) {
	cases := []struct{ host, id, q string }{
		{"box", "$3", "box/$3"},
		{"", "$3", "$3"},
		{"u@h", "$1", "u@h/$1"},
	}
	for _, c := range cases {
		if got := Qualify(c.host, c.id); got != c.q {
			t.Errorf("Qualify(%q,%q) = %q, want %q", c.host, c.id, got, c.q)
		}
		h, id := Split(c.q)
		if h != c.host || id != c.id {
			t.Errorf("Split(%q) = %q,%q, want %q,%q", c.q, h, id, c.host, c.id)
		}
	}
}
