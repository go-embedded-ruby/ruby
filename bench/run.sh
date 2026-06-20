#!/usr/bin/env bash
# Differential performance harness: runs each bench/*.rb under rbgo (interpreter),
# rbgo+AOT (a native binary from `rbgo build`), MRI, and MRI+YJIT, takes the best
# of N runs (least noise), and prints a Markdown table of wall-clock times and
# ratios. Correctness (same stdout) is checked first; a program is skipped if any
# runtime disagrees with MRI.
#
# Usage: bench/run.sh [runs]   (default 5)
#
# The AOT column needs the Go toolchain and a module checkout (so `rbgo build`
# can compile + link). Set AOT=0 to skip it (e.g. with only an installed rbgo).
set -u
RUNS="${1:-5}"
RBGO="${RBGO:-./rbgo}"
RUBY="${RUBY:-ruby}"
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

printf '| Benchmark | rbgo | rbgo+AOT | MRI | MRI+YJIT | AOT/MRI | AOT/YJIT |\n'
printf '| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n'
for f in "$HERE"/*.rb; do
  name=$(basename "$f" .rb)
  ro=$("$RBGO" "$f" 2>/dev/null); mo=$("$RUBY" "$f" 2>/dev/null)
  [ "$ro" != "$mo" ] && { printf '| %s | (output differs, skipped) ||||||\n' "$name"; continue; }

  rb=$(best "$RBGO" "$f")
  mr=$(best "$RUBY" "$f")
  yj=$(best "$RUBY" --yjit "$f")

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

  printf '| %s | %ss | %s | %ss | %ss | %s | %s |\n' "$name" "$rb" "$at" "$mr" "$yj" "$am" "$ay"
done
