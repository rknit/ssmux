package fleet

import (
	"sort"
	"strconv"
	"strings"
)

// Entry is a pin or RECENT record for a remote session. Created
// disambiguates reused tmux IDs; Value is the pinned slot or last-used time.
type Entry struct {
	Host, ID       string
	Created, Value int64
}

// Key identifies the session an entry refers to.
func (e Entry) Key() string {
	return e.Host + "," + e.ID + "," + strconv.FormatInt(e.Created, 10)
}

// ParseEntries parses space-separated "host,id,created,value" tokens,
// skipping malformed ones.
func ParseEntries(s string) []Entry {
	var es []Entry
	for _, tok := range strings.Fields(s) {
		p := strings.Split(tok, ",")
		if len(p) != 4 || p[0] == "" || p[1] == "" {
			continue
		}
		created, err1 := strconv.ParseInt(p[2], 10, 64)
		value, err2 := strconv.ParseInt(p[3], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		es = append(es, Entry{p[0], p[1], created, value})
	}
	return es
}

// FormatEntries is the inverse of ParseEntries.
func FormatEntries(es []Entry) string {
	toks := make([]string, len(es))
	for i, e := range es {
		toks[i] = e.Key() + "," + strconv.FormatInt(e.Value, 10)
	}
	return strings.Join(toks, " ")
}

// Upsert returns es with e replacing any entry for the same Host and ID.
func Upsert(es []Entry, e Entry) []Entry {
	out := Without(es, func(x Entry) bool { return x.Host != e.Host || x.ID != e.ID })
	return append(out, e)
}

// Without returns the entries for which keep returns true.
func Without(es []Entry, keep func(Entry) bool) []Entry {
	var out []Entry
	for _, e := range es {
		if keep(e) {
			out = append(out, e)
		}
	}
	return out
}

// Newest returns the n entries with the largest Value, largest first.
func Newest(es []Entry, n int) []Entry {
	out := append([]Entry(nil), es...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Value > out[j].Value })
	if len(out) > n {
		out = out[:n]
	}
	return out
}
