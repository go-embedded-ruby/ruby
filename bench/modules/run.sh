#!/usr/bin/env bash
# Per-module comparative performance harness for the stdlib modules that rbgo
# binds to standalone pure-Go libraries (go-ruby-<mod>). Each bench/modules/*.rb
# exercises the hot path of one module and prints a deterministic checksum. The
# SAME .rb is run under rbgo, MRI, MRI+YJIT, JRuby and TruffleRuby — apples to
# apples on the Ruby-visible operation — its output is checked byte-identical to
# MRI first, then wall-clock timed (best-of-N to suppress scheduler noise). A
# runtime not installed shows "n/a"; a runtime whose output diverges from MRI
# shows "diff" (and is not timed).
#
# Usage:  bench/modules/run.sh [runs]      (default 5)
# Env:    RBGO, RUBY, JRUBY, TRUFFLE, N (override every script's iteration count).
set -u
RUNS="${1:-5}"
RBGO="${RBGO:-/tmp/rbgo}"
RUBY="${RUBY:-ruby}"
JRUBY="${JRUBY:-jruby}"
TRUFFLE="${TRUFFLE:-truffleruby}"
HERE="$(cd "$(dirname "$0")" && pwd)"

# Module order = the order they were bound into rbgo main.
# Wave-1/2 modules first, then the wave-3 standalone go-ruby-<mod> bindings.
MODULES="regexp erb yaml format strscan optparse json bigdecimal date uri digest \
set prime matrix complex rational cmath tsort abbrev did-you-mean prettyprint \
scanf unicode-normalize cgi zlib ipaddr pathname rexml"

# best PROG ARGS... → minimal real milliseconds over $RUNS runs.
best() {
  local b=99999999 t
  for _ in $(seq "$RUNS"); do
    t=$( { /usr/bin/time -p "$@" >/dev/null; } 2>&1 | awk '/real/{print $2}' )
    awk "BEGIN{exit !($t < $b)}" && b=$t
  done
  awk "BEGIN{printf \"%d\", $b*1000}"
}

# best_ref BIN PROG EXPECTED → "Nms" | "n/a" (absent) | "diff" (output diverges).
best_ref() {
  command -v "$1" >/dev/null 2>&1 || { echo "n/a"; return; }
  [ "$("$1" "$2" 2>/dev/null)" = "$3" ] || { echo "diff"; return; }
  echo "$(best "$1" "$2")"
}

printf '| Module | rbgo | MRI | MRI+YJIT | JRuby | TruffleRuby | rbgo/MRI | rbgo/YJIT |\n'
printf '| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n'

for m in $MODULES; do
  f="$HERE/$m.rb"
  [ -f "$f" ] || { printf '| %s | (no script) | | | | | | |\n' "$m"; continue; }

  mo=$("$RUBY" "$f" 2>/dev/null)
  ro=$("$RBGO" run "$f" 2>/dev/null)
  if [ "$ro" != "$mo" ]; then
    printf '| %s | (output differs vs MRI, skipped) | | | | | | |\n' "$m"
    continue
  fi

  rb=$(best "$RBGO" run "$f")
  mr=$(best "$RUBY" "$f")
  yj=$(best "$RUBY" --yjit "$f")
  jr=$(best_ref "$JRUBY" "$f" "$mo")
  tr=$(best_ref "$TRUFFLE" "$f" "$mo")

  rm_ratio=$(awk "BEGIN{printf \"%.2f\", $rb/$mr}")
  ry_ratio=$(awk "BEGIN{printf \"%.2f\", $rb/$yj}")
  jr_s=$([ "$jr" = "n/a" ] || [ "$jr" = "diff" ] && echo "$jr" || echo "${jr}ms")
  tr_s=$([ "$tr" = "n/a" ] || [ "$tr" = "diff" ] && echo "$tr" || echo "${tr}ms")

  printf '| %s | %sms | %sms | %sms | %s | %s | %s× | %s× |\n' \
    "$m" "$rb" "$mr" "$yj" "$jr_s" "$tr_s" "$rm_ratio" "$ry_ratio"
done
