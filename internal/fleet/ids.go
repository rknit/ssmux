// Package fleet combines the local tmux server with remote hosts reached over
// ssh. Remote sessions are addressed by qualified IDs ("host/$3").
package fleet

import "strings"

// Qualify returns "host/id", or id unchanged for the local host ("").
func Qualify(host, id string) string {
	if host == "" {
		return id
	}
	return host + "/" + id
}

// Split is the inverse of Qualify. It splits at the last "/"; tmux IDs never
// contain one.
func Split(qid string) (host, id string) {
	i := strings.LastIndex(qid, "/")
	if i < 0 {
		return "", qid
	}
	return qid[:i], qid[i+1:]
}
