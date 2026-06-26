#!/usr/bin/env bash
# Differential performance harness: runs each bench/*.rb under rbgo (interpreter),
# rbgo+AOT (a native binary from `rbgo build`), MRI, MRI+YJIT, JRuby and
# TruffleRuby, takes the best of N runs (least noise), and prints a Markdown table
# of wall-clock times and ratios. Correctness (same stdout as MRI) is checked
# first; a program is skipped if rbgo disagrees with MRI. JRuby and TruffleRuby
# are extra reference runtimes, shown as "n/a" when not installed and "diff" when
# their output diverges from MRI.
#
# Usage: bench/run.sh [runs]   (default 5)
#
# Env: RBGO (./rbgo), RUBY (ruby), JRUBY (jruby), TRUFFLE (truffleruby), AOT (1).
# The AOT column needs the Go toolchain + a module checkout; set AOT=0 to skip it.
set -u
RUNS="${1:-5}"
RBGO="${RBGO:-./rbgo}"
RUBY="${RUBY:-ruby}"
JRUBY="${JRUBY:-jruby}"
TRUFFLE="${TRUFFLE:-truffleruby}"
AOT="${AOT:-1}"
HERE="$(cd "$(dirname "$0")" && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

best() { # best() prog args... → minimal real seconds over $RUNS
  local b=99999 t
  for _ in $(seq "$RUNS"); do
    t=$( { /usr/bin/time -p "$@" >/dev/null; } 2>&1 | awk '/real/{print $2}' )
    awk "BEGIN{exit !($t < $b)}" && b=$t
  done
  echo "$b"
}

# best_ref times an optional reference runtime: "Ns" on success, "n/a" when the
# binary is absent, "diff" when its output diverges from MRI's.
best_ref() { # bin, prog, expected_output
  command -v "$1" >/dev/null 2>&1 || { echo "n/a"; return; }
  [ "$("$1" "$2" 2>/dev/null)" = "$3" ] || { echo "diff"; return; }
  echo "$(best "$1" "$2")s"
}

printf '| Benchmark | rbgo | rbgo+AOT | MRI | MRI+YJIT | JRuby | TruffleRuby | AOT/MRI | AOT/YJIT |\n'
printf '| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n'
for f in "$HERE"/*.rb; do
  name=$(basename "$f" .rb)
  ro=$("$RBGO" "$f" 2>/dev/null); mo=$("$RUBY" "$f" 2>/dev/null)
  [ "$ro" != "$mo" ] && { printf '| %s | (output differs, skipped) | | | | | | | |\n' "$name"; continue; }

  rb=$(best "$RBGO" "$f")
  mr=$(best "$RUBY" "$f")
  yj=$(best "$RUBY" --yjit "$f")
  jr=$(best_ref "$JRUBY" "$f" "$mo")
  tr=$(best_ref "$TRUFFLE" "$f" "$mo")

  # AOT: build a specialised native binary for this program, then time it.
  at="n/a"; am="—"; ay="—"
  if [ "$AOT" = "1" ]; then
    abin="$TMP/$name"
    if "$RBGO" build -o "$abin" "$f" >/dev/null 2>&1; then
      ao=$("$abin" "$f" 2>/dev/null)
      if [ "$ao" = "$mo" ]; then
        a=$(best "$abin" "$f"); at="${a}s"
        am="$(awk "BEGIN{printf \"%.2f\", $a/$mr}")×"
        ay="$(awk "BEGIN{printf \"%.2f\", $a/$yj}")×"
      else
        at="(differs)"
      fi
    else
      at="(build failed)"
    fi
  fi

  printf '| %s | %ss | %s | %ss | %ss | %s | %s | %s | %s |\n' "$name" "$rb" "$at" "$mr" "$yj" "$jr" "$tr" "$am" "$ay"
done
