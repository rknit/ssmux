#!/bin/sh
# Fake ssh: ignores options, runs the remote command locally against a
# per-host tmux server. Env: FAKESSH_BIN (tmux shim dir). "-O check" reports
# a master for hosts that have had a call.
ctl=
while [ $# -gt 0 ]; do
	case "$1" in
	-o) shift 2 ;;
	-O) ctl=$2; shift 2 ;;
	-t | -T) shift ;;
	--) shift; break ;;
	-*) shift ;;
	*) break ;;
	esac
done
dest=$1
shift
master=$FAKESSH_BIN/../$dest.master
if [ -n "$ctl" ]; then
	[ "$ctl" = check ] && [ -e "$master" ] && exit 0
	exit 255
fi
: >"$master"
if [ "$1" = "--" ]; then shift; fi
unset TMUX
export FAKESSH_DEST=$dest
PATH=$FAKESSH_BIN:$PATH exec sh -c "$*"
