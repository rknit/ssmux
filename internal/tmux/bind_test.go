package tmux

import (
	"strings"
	"testing"
)

func TestBindKeys(t *testing.T) {
	c := newTestServer(t)
	if err := c.BindKeys("/x y/ssmux"); err != nil {
		t.Fatal(err)
	}
	first, err := c.Run("list-keys", "-T", "root")
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{"M-a", "M-Up"}
	for n := '1'; n <= '9'; n++ {
		keys = append(keys, "M-"+string(n))
	}
	for _, k := range keys {
		found := false
		for line := range strings.SplitSeq(first, "\n") {
			f := strings.Fields(line)
			if len(f) > 3 && f[3] == k && strings.Contains(line, "/x y/ssmux") {
				found = true
			}
		}
		if !found {
			t.Errorf("no binding for %s referencing the binary", k)
		}
	}
	if err := c.BindKeys("/x y/ssmux"); err != nil {
		t.Fatal(err)
	}
	if second, _ := c.Run("list-keys", "-T", "root"); second != first {
		t.Errorf("BindKeys not idempotent")
	}
	if v, _ := c.GlobalOption("detach-on-destroy"); v != "off" {
		t.Errorf("detach-on-destroy = %q, want off", v)
	}
}
