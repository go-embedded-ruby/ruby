#!/bin/sh
# oracle.sh — differential oracle for go-embedded-ruby.
#
# Runs Ruby through rbgo, MRI (CRuby), JRuby and TruffleRuby and reports whether
# they agree. MRI, JRuby and TruffleRuby are independent reference implementations
# of Ruby; rbgo is the pure-Go implementation under test, so a divergence from any
# reference is a conformance signal. Exits non-zero on any divergence.
#
# Usage:
#   scripts/oracle.sh -e 'p [1, 2, 3].sum'      # one snippet
#   scripts/oracle.sh path/to/program.rb        # a whole program file
#   scripts/oracle.sh -b scripts/conformance/core_ext.txt   # batch: one snippet
#                                                           # per line (# = comment)
#
# Environment overrides: RBGO (default: /tmp/rbgo, else built from ./cmd/rbgo),
# MRI (default: ruby), JRUBY (default: jruby), TRUFFLE (default: truffleruby). A
# reference that is not installed is skipped with a note rather than failing the
# run, so the oracle still works with whichever references are present.
set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
RBGO=${RBGO:-}
MRI=${MRI:-ruby}
JRUBY=${JRUBY:-jruby}
TRUFFLE=${TRUFFLE:-truffleruby}

if [ -z "$RBGO" ]; then
	if [ -x /tmp/rbgo ]; then
		RBGO=/tmp/rbgo
	else
		RBGO=/tmp/rbgo-oracle
		(cd "$here" && GOWORK=off go build -o "$RBGO" ./cmd/rbgo)
	fi
fi

usage() {
	echo "usage: $0 (-e '<ruby>' | -b <corpus-file> | <program.rb>)" >&2
	exit 2
}

# run executes a reference binary, or notes that it is absent (so a missing
# reference is skipped, not counted as a divergence).
run() { # binary, args...
	bin=$1
	shift
	if ! command -v "$bin" >/dev/null 2>&1 && [ ! -x "$bin" ]; then
		printf '(%s not installed — skipped)' "$bin"
		return
	fi
	"$bin" "$@" 2>&1 | tail -1
}

# eval_all / file_all fill rbgo_out, mri_out, jruby_out, truffle_out for a
# snippet / file.
eval_all() {
	rbgo_out=$("$RBGO" run -e "$1" 2>&1 | tail -1)
	mri_out=$(run "$MRI" -e "$1")
	jruby_out=$(run "$JRUBY" -e "$1")
	truffle_out=$(run "$TRUFFLE" -e "$1")
}
file_all() {
	rbgo_out=$("$RBGO" run "$1" 2>&1 | tail -1)
	mri_out=$(run "$MRI" "$1")
	jruby_out=$(run "$JRUBY" "$1")
	truffle_out=$(run "$TRUFFLE" "$1")
}

# diff_one compares rbgo against one reference, skipping it when it is absent.
# Appends the reference label to $st on a divergence. Args: ref-output, label.
diff_one() { # ref_out, label
	case "$1" in
	*'not installed'*) return ;;
	esac
	if [ "$rbgo_out" != "$1" ]; then
		if [ "$st" = agree ]; then st="DIVERGE-from-$2"; else st="$st,$2"; fi
	fi
}

# status_of echoes "agree" or a DIVERGE-* label, judged against whichever
# references actually ran.
status_of() {
	st=agree
	diff_one "$mri_out" mri
	diff_one "$jruby_out" jruby
	diff_one "$truffle_out" truffle
	echo "$st"
}

report_one() { # heading already printed by caller
	printf 'rbgo   : %s\n' "$rbgo_out"
	printf 'mri    : %s\n' "$mri_out"
	printf 'jruby  : %s\n' "$jruby_out"
	printf 'truffle: %s\n' "$truffle_out"
	printf '=> %s\n' "$(status_of)"
	[ "$(status_of)" = agree ]
}

case "${1:-}" in
-e)
	[ $# -ge 2 ] || usage
	eval_all "$2"
	report_one
	;;
-b | --batch)
	[ $# -ge 2 ] || usage
	corpus=$2
	[ -f "$corpus" ] || {
		echo "no such corpus file: $corpus" >&2
		exit 2
	}
	total=0
	diverged=0
	while IFS= read -r line || [ -n "$line" ]; do
		case "$line" in '' | \#*) continue ;; esac # skip blank lines and comments
		total=$((total + 1))
		eval_all "$line"
		st=$(status_of)
		if [ "$st" = agree ]; then
			printf 'ok   %s\n' "$line"
		else
			diverged=$((diverged + 1))
			printf 'DIFF %s\n' "$line"
			printf '       rbgo   : %s\n' "$rbgo_out"
			printf '       mri    : %s\n' "$mri_out"
			printf '       jruby  : %s\n' "$jruby_out"
			printf '       truffle: %s\n' "$truffle_out"
		fi
	done <"$corpus"
	printf '\n%d/%d agree (%d diverge)\n' "$((total - diverged))" "$total" "$diverged"
	[ "$diverged" -eq 0 ]
	;;
'')
	usage
	;;
*)
	file_all "$1"
	report_one
	;;
esac
