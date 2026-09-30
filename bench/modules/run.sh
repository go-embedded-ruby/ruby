#!/usr/bin/env bash
# Per-module comparative performance harness for the stdlib modules that rbgo
# binds to standalone pure-Go libraries (go-ruby-<mod>). Each bench/modules/*.rb
# exercises the hot path of one module and prints a deterministic checksum. The
# SAME .rb is run under rbgo, MRI, MRI+YJIT, JRuby and TruffleRuby — apples to
# apples on the Ruby-visible operation — its output is checked byte-identical to
# MRI first, then wall-clock timed. A runtime not installed shows "n/a"; a
# runtime whose output diverges from MRI shows "diff" (and is not timed).
#
# Usage:  bench/modules/run.sh [runs]      (default 5)
# Env:    RBGO, RUBY, JRUBY, TRUFFLE, N (override every script's iteration count).
#
# Three things this harness gets right, each because it once did not:
#
#   The clock is Time::HiRes, not `/usr/bin/time -p`. That prints two decimals of
#   a second, so its resolution is 10 ms and anything faster than one tick reads
#   zero. It left 116 of 116 published figures sitting on a multiple of 10 ms,
#   a third of them ten ticks or fewer, and ratios like 0.40x resting on a
#   two-tick measurement.
#
#   A run that exits non-zero is an error, not a fast run. Keeping the smallest
#   time over N runs without looking at the exit status means a command that dies
#   immediately wins the series and gets published.
#
#   The runtimes are interleaved, one run each in rotation, rather than each one
#   taking its own block of wall-clock time. A machine whose load moves during a
#   block charges the movement to whichever runtime was running, and nothing in
#   the output says so. Rotating the order each round means none of them always
#   goes first. See go-crdt/crdt#131 for what that is worth: the same defect put
#   a published ratio 14% out, and contention does not fall on every arm alike —
#   it falls hardest on the shortest-running one.
set -u
RUNS="${1:-5}"
RBGO="${RBGO:-/tmp/rbgo}"
RUBY="${RUBY:-ruby}"
JRUBY="${JRUBY:-jruby}"
TRUFFLE="${TRUFFLE:-truffleruby}"
HERE="$(cd "$(dirname "$0")" && pwd)"

# hires PROG ARGS... → "MILLISECONDS EXITSTATUS", with the child's own output
# discarded inside the fork so that neither stream can reach this one.
hires() {
  perl -MTime::HiRes=time -e '
    my $t = time;
    my $pid = fork();
    defined $pid or exit 97;
    if ($pid == 0) {
      open(STDOUT, ">", "/dev/null") or exit 126;
      open(STDERR, ">", "/dev/null") or exit 126;
      exec(@ARGV);
      exit 127;
    }
    waitpid($pid, 0);
    printf "%.3f %d\n", (time - $t) * 1000, $? >> 8;
  ' -- "$@"
}

# The arms, as parallel arrays: name, then the command as a single string that
# word-splits into program and arguments. A runtime that is absent, or whose
# output diverges from MRI, is dropped before any timing.
arm_name=(); arm_cmd=(); arm_best=()

# add_arm NAME PROG [ARGS...] — the command is stored shell-quoted, so a
# repository path with a space in it stays one argument.
add_arm() {
  local name="$1"; shift
  arm_name[${#arm_name[@]}]="$name"
  arm_cmd[${#arm_cmd[@]}]="$(printf '%q ' "$@")"
  arm_best[${#arm_best[@]}]=""
}

printf '| Module | rbgo | MRI | MRI+YJIT | JRuby | TruffleRuby | rbgo/MRI | rbgo/YJIT |\n'
printf '| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n'

MODULES="${MODULES:-regexp erb yaml format strscan optparse json bigdecimal date uri digest \
set prime matrix complex rational cmath tsort abbrev did-you-mean prettyprint \
scanf unicode-normalize cgi zlib ipaddr pathname rexml \
base64 securerandom ostruct observer logger find benchmark}"

for m in $MODULES; do
  f="$HERE/$m.rb"
  [ -f "$f" ] || { printf '| %s | (no script) | | | | | | |\n' "$m"; continue; }

  mo=$("$RUBY" "$f" 2>/dev/null)
  ro=$("$RBGO" run "$f" 2>/dev/null)
  if [ "$ro" != "$mo" ]; then
    printf '| %s | (output differs vs MRI, skipped) | | | | | | |\n' "$m"
    continue
  fi

  arm_name=(); arm_cmd=(); arm_best=()
  add_arm rbgo "$RBGO" run "$f"
  add_arm mri "$RUBY" "$f"
  add_arm yjit "$RUBY" --yjit "$f"
  for pair in "jruby $JRUBY" "truffle $TRUFFLE"; do
    set -- $pair
    label="$1"; bin="$2"
    command -v "$bin" >/dev/null 2>&1 || { eval "${label}_state=n/a"; continue; }
    [ "$("$bin" "$f" 2>/dev/null)" = "$mo" ] || { eval "${label}_state=diff"; continue; }
    eval "${label}_state=timed"
    add_arm "$label" "$bin" "$f"
  done

  # One round asks every arm for one run, rotating who goes first, and the best
  # of those runs is kept — the minimum, because the environment can only add
  # delay to a program, never take any away (Chen & Revels, Robust benchmarking
  # in noisy environments, IEEE HPEC 2016).
  n=${#arm_name[@]}
  r=0
  while [ "$r" -lt "$RUNS" ]; do
    i=0
    while [ "$i" -lt "$n" ]; do
      k=$(( (i + r) % n ))
      if [ "${arm_best[$k]}" = "err" ]; then i=$(( i + 1 )); continue; fi
      out=$(eval hires "${arm_cmd[$k]}")
      ms=${out% *}; rc=${out#* }
      if [ "$rc" != "0" ]; then
        # A run that failed is not a fast run. The old harness kept the smallest
        # time of N without looking at the exit status, so a command that died
        # immediately won its own series and got published.
        echo "harness: ${arm_name[$k]} exited $rc on $m — not timing it" >&2
        arm_best[$k]="err"
        i=$(( i + 1 ))
        continue
      fi
      b=${arm_best[$k]}
      if [ -z "$b" ] || awk "BEGIN{exit !($ms < $b)}"; then arm_best[$k]=$ms; fi
      i=$(( i + 1 ))
    done
    r=$(( r + 1 ))
  done

  get() { # get NAME → best milliseconds, or empty
    local j=0
    while [ "$j" -lt "${#arm_name[@]}" ]; do
      [ "${arm_name[$j]}" = "$1" ] && { echo "${arm_best[$j]}"; return; }
      j=$(( j + 1 ))
    done
  }
  rb=$(get rbgo); mr=$(get mri); yj=$(get yjit)
  jr=$(get jruby); tr=$(get truffle)

  fmt() {
    case "$1" in
      "") echo "$2" ;;
      err) echo "err" ;;
      *) awk "BEGIN{printf \"%.1fms\", $1}" ;;
    esac
  }
  ratio() { # ratio NUMERATOR DENOMINATOR → "n.nn" or "—"
    case "$1:$2" in
      err:*|*:err|:*|*:) echo "—"; return ;;
    esac
    awk "BEGIN{printf \"%.2f\", $1/$2}"
  }
  rm_ratio=$(ratio "$rb" "$mr")
  ry_ratio=$(ratio "$rb" "$yj")

  printf '| %s | %s | %s | %s | %s | %s | %s× | %s× |\n' \
    "$m" "$(fmt "$rb" '')" "$(fmt "$mr" '')" "$(fmt "$yj" '')" \
    "$(fmt "$jr" "${jruby_state:-n/a}")" "$(fmt "$tr" "${truffle_state:-n/a}")" \
    "$rm_ratio" "$ry_ratio"
done
