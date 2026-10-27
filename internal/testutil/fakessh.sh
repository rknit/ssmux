#!/bin/sh
# Fake ssh: ignores options, runs the remote command locally against a
# per-host tmux server. Env: FAKESSH_BIN (tmux shim dir).
while [ $# -gt 0 ]; do
	case "$1" in
	-o) shift 2 ;;
	-t | -T) shift ;;
	--) shift; break ;;
	-*) shift ;;
	*) break ;;
	esac
done
dest=$1
shift
if [ "$1" = "--" ]; then shift; fi
unset TMUX
export FAKESSH_DEST=$dest
PATH=$FAKESSH_BIN:$PATH exec sh -c "$*"
