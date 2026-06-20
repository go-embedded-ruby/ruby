#!/usr/bin/env bash
# Differential performance harness: runs each bench/*.rb under rbgo, MRI, and
# MRI+YJIT, takes the best of N runs (least noise), and prints a Markdown table
# of wall-clock times and rbgo/MRI ratios. Correctness (same stdout) is checked
# first; a program is skipped if rbgo and MRI disagree.
#
# Usage: bench/run.sh [runs]   (default 5)
set -u
RUNS="${1:-5}"
RBGO="${RBGO:-./rbgo}"
RUBY="${RUBY:-ruby}"
HERE="$(cd "$(dirname "$0")" && pwd)"

best() { # best() prog args... → minimal real seconds over $RUNS
  local b=99999 t
  for _ in $(seq "$RUNS"); do
    t=$( { /usr/bin/time -p "$@" >/dev/null; } 2>&1 | awk '/real/{print $2}' )
    awk "BEGIN{exit !($t < $b)}" && b=$t
  done
  echo "$b"
}

printf '| Benchmark | rbgo | MRI | MRI+YJIT | rbgo/MRI | rbgo/YJIT |\n'
printf '| --- | ---: | ---: | ---: | ---: | ---: |\n'
for f in "$HERE"/*.rb; do
  name=$(basename "$f" .rb)
  ro=$("$RBGO" "$f" 2>/dev/null); mo=$("$RUBY" "$f" 2>/dev/null)
  [ "$ro" != "$mo" ] && { printf '| %s | (output differs, skipped) |||||\n' "$name"; continue; }
  rb=$(best "$RBGO" "$f")
  mr=$(best "$RUBY" "$f")
  yj=$(best "$RUBY" --yjit "$f")
  rm=$(awk "BEGIN{printf \"%.2f\", $rb/$mr}")
  ry=$(awk "BEGIN{printf \"%.2f\", $rb/$yj}")
  printf '| %s | %ss | %ss | %ss | %s× | %s× |\n' "$name" "$rb" "$mr" "$yj" "$rm" "$ry"
done
