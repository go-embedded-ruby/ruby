#!/bin/sh
# oracle.sh — three-way differential oracle for go-embedded-ruby.
#
# Runs a Ruby snippet (or file) through rbgo, MRI (CRuby) and JRuby and reports
# whether they agree. MRI and JRuby are independent reference implementations of
# Ruby 4.0; rbgo is the pure-Go implementation under test. A divergence from
# either reference is a conformance signal.
#
# Usage:
#   scripts/oracle.sh -e 'p [1, 2, 3].sum'
#   scripts/oracle.sh path/to/snippet.rb
#
# Environment overrides: RBGO (default: the freshly built ./cmd/rbgo, falling back
# to /tmp/rbgo), MRI (default: ruby), JRUBY (default: jruby). A reference that is
# not installed is skipped with a note rather than failing the run.
set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
RBGO=${RBGO:-}
MRI=${MRI:-ruby}
JRUBY=${JRUBY:-jruby}

if [ -z "$RBGO" ]; then
	if [ -x /tmp/rbgo ]; then
		RBGO=/tmp/rbgo
	else
		RBGO=/tmp/rbgo-oracle
		( cd "$here" && GOWORK=off go build -o "$RBGO" ./cmd/rbgo )
	fi
fi

run() { # impl-binary, args...
	bin=$1
	shift
	if ! command -v "$bin" >/dev/null 2>&1 && [ ! -x "$bin" ]; then
		printf '(%s not installed — skipped)' "$bin"
		return
	fi
	"$bin" "$@" 2>&1 | tail -1
}

if [ "${1:-}" = "-e" ]; then
	[ $# -ge 2 ] || { echo "usage: $0 -e '<ruby>'" >&2; exit 2; }
	code=$2
	rbgo_out=$("$RBGO" run -e "$code" 2>&1 | tail -1)
	mri_out=$(run "$MRI" -e "$code")
	jruby_out=$(run "$JRUBY" -e "$code")
else
	[ $# -ge 1 ] || { echo "usage: $0 (-e '<ruby>' | file.rb)" >&2; exit 2; }
	file=$1
	rbgo_out=$("$RBGO" run "$file" 2>&1 | tail -1)
	mri_out=$(run "$MRI" "$file")
	jruby_out=$(run "$JRUBY" "$file")
fi

printf 'rbgo : %s\n'  "$rbgo_out"
printf 'mri  : %s\n'  "$mri_out"
printf 'jruby: %s\n'  "$jruby_out"

# Agreement is judged against whichever references actually ran.
status=agree
case "$mri_out" in *'not installed'*) ;; *) [ "$rbgo_out" = "$mri_out" ] || status=DIVERGE-from-mri ;; esac
case "$jruby_out" in *'not installed'*) ;; *) [ "$rbgo_out" = "$jruby_out" ] || { [ "$status" = agree ] && status=DIVERGE-from-jruby || status="$status,jruby"; } ;; esac
printf '=> %s\n' "$status"
[ "$status" = agree ]
