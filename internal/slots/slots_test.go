package slots

import (
	"reflect"
	"testing"

	"github.com/rknit/ssmux/internal/tmux"
)

// sessions builds A, B, C with the given slots (0 = unpinned).
func sessions(a, b, c int) []tmux.Session {
	return []tmux.Session{{ID: "A", Slot: a}, {ID: "B", Slot: b}, {ID: "C", Slot: c}}
}

func full() []tmux.Session {
	var ss []tmux.Session
	for i := 1; i <= 9; i++ {
		ss = append(ss, tmux.Session{ID: string(rune('0' + i)), Slot: i})
	}
	return append(ss, tmux.Session{ID: "A"})
}

func TestPin(t *testing.T) {
	tests := []struct {
		name string
		ss   []tmux.Session
		slot int
		want []Change
	}{
		{"free slot", sessions(0, 0, 0), 4, []Change{{"A", 4}}},
		{"unpinned into taken unpins occupant", sessions(0, 2, 0), 2, []Change{{"B", 0}, {"A", 2}}},
		{"pinned into taken swaps", sessions(1, 2, 0), 2, []Change{{"B", 1}, {"A", 2}}},
		{"same slot", sessions(1, 0, 0), 1, nil},
		{"unpin", sessions(1, 0, 0), 0, []Change{{"A", 0}}},
		{"unpin unpinned", sessions(0, 0, 0), 0, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Pin(tt.ss, "A", tt.slot); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPinFirstFree(t *testing.T) {
	ss := append(sessions(0, 1, 2), tmux.Session{ID: "D", Slot: 4})
	if got, want := PinFirstFree(ss, "A"), []Change{{"A", 3}}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := PinFirstFree(full(), "A"); got != nil {
		t.Errorf("all taken: got %v", got)
	}
	if got := PinFirstFree(sessions(5, 0, 0), "A"); got != nil {
		t.Errorf("already pinned: got %v", got)
	}
}

func TestMove(t *testing.T) {
	tests := []struct {
		name  string
		ss    []tmux.Session
		delta int
		want  []Change
	}{
		{"into empty", sessions(3, 0, 0), -1, []Change{{"A", 2}}},
		{"swap with neighbour", sessions(3, 2, 0), -1, []Change{{"B", 3}, {"A", 2}}},
		{"down", sessions(3, 4, 0), +1, []Change{{"B", 3}, {"A", 4}}},
		{"top edge", sessions(1, 0, 0), -1, nil},
		{"bottom edge", sessions(9, 0, 0), +1, nil},
		{"unpinned", sessions(0, 0, 0), -1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Move(tt.ss, "A", tt.delta); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
