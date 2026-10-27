// Package slots computes slot assignments. It never talks to tmux; callers
// apply the returned changes in order with tmux.Client.SetSlot.
package slots

import "github.com/rknit/ssmux/internal/tmux"

const maxSlot = 9

// Change is one SetSlot call. Slot 0 means unpin. The displaced session is
// always listed first, so the moved session's change is applied last.
type Change struct {
	ID   string
	Slot int
}

func slotOf(ss []tmux.Session, id string) int {
	for _, s := range ss {
		if s.ID == id {
			return s.Slot
		}
	}
	return 0
}

func holder(ss []tmux.Session, slot int) (string, bool) {
	for _, s := range ss {
		if s.Slot == slot {
			return s.ID, true
		}
	}
	return "", false
}

// Pin puts id on slot (1..9), or unpins it when slot is 0. A session already
// on that slot takes id's old slot, which is "unpinned" if id had none.
func Pin(ss []tmux.Session, id string, slot int) []Change {
	cur := slotOf(ss, id)
	if cur == slot {
		return nil
	}
	if slot == 0 {
		return []Change{{id, 0}}
	}
	if other, ok := holder(ss, slot); ok {
		return []Change{{other, cur}, {id, slot}}
	}
	return []Change{{id, slot}}
}

// PinFirstFree pins an unpinned id to the lowest free slot.
func PinFirstFree(ss []tmux.Session, id string) []Change {
	if slotOf(ss, id) != 0 {
		return nil
	}
	for n := 1; n <= maxSlot; n++ {
		if _, ok := holder(ss, n); !ok {
			return []Change{{id, n}}
		}
	}
	return nil
}

// Move shifts a pinned id by delta slots, swapping with any neighbour there.
func Move(ss []tmux.Session, id string, delta int) []Change {
	cur := slotOf(ss, id)
	target := cur + delta
	if cur == 0 || target < 1 || target > maxSlot {
		return nil
	}
	return Pin(ss, id, target)
}
