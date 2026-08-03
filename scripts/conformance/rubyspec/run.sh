#!/usr/bin/env bash
# Copyright (c) the go-embedded-ruby/ruby authors
#
# SPDX-License-Identifier: BSD-3-Clause
#
# ruby/spec conformance RATCHET for go-embedded-ruby (rbgo).
#
# Runs the ruby/spec language + core suites through rbgo under a minimal
# MSpec-compatible shim (spec_helper.rb, vendored beside this script) and
# reports the total number of passing examples. It is a shrink-only ratchet:
# if the pass total drops below the frozen floor (the FLOOR file beside this
# script) the run FAILS, so language conformance is tracked and can only go up.
# When rbgo improves, bump FLOOR to the newly measured total to lock in the win.
#
# Each spec file runs in its own rbgo process (the shim prints a
# `RBGO_RESULT pass=.. fail=.. error=.. skip=..` line at exit); a file that
# crashes, times out or fails to load contributes 0 and is listed. The corpus is
# pinned to SPEC_SHA for reproducibility.
#
# Env:
#   RBGO      path to the rbgo binary   (default: built from ./cmd/rbgo)
#   SPECDIR   ruby/spec checkout        (default: cloned to CACHE at SPEC_SHA)
#   CACHE     clone cache dir           (default: /tmp/rubyspec-corpus)
#   JOBS      parallelism               (default: number of CPUs)
#   TIMEOUT   per-file timeout seconds  (default: 25)
#   UPDATE_FLOOR=1  print the measured total as the new floor and exit 0
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../../.." && pwd)"
RBGO="${RBGO:-/tmp/rbgo-rubyspec}"
CACHE="${CACHE:-/tmp/rubyspec-corpus}"
SPECDIR="${SPECDIR:-$CACHE}"
TIMEOUT="${TIMEOUT:-25}"
JOBS="${JOBS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)}"

# The ruby/spec commit the floor was measured against. Bump together with FLOOR.
SPEC_SHA="87b1631992bd00cf0c4934474766d54dad088191"
SPEC_URL="https://github.com/ruby/spec"

FLOOR="$(tr -dc '0-9' < "$HERE/FLOOR")"

echo "== build rbgo =="
if [ ! -x "$RBGO" ]; then
  (cd "$REPO_ROOT" && GOWORK=off CGO_ENABLED=0 go build -o "$RBGO" ./cmd/rbgo) || exit 1
fi
echo "rbgo: $RBGO"

echo
echo "== ruby/spec corpus =="
if [ ! -d "$SPECDIR/language" ]; then
  if ! command -v git >/dev/null 2>&1; then
    echo "git missing and no corpus at $SPECDIR — cannot run ratchet" >&2
    exit 2
  fi
  rm -rf "$CACHE"
  git clone --filter=blob:none "$SPEC_URL" "$CACHE" >/dev/null 2>&1 || {
    echo "clone failed (offline?) — cannot run ratchet" >&2; exit 2; }
  (cd "$CACHE" && git checkout -q "$SPEC_SHA") || {
    echo "checkout of $SPEC_SHA failed" >&2; exit 2; }
  SPECDIR="$CACHE"
fi
echo "corpus: $SPECDIR @ $SPEC_SHA"

# Install the MSpec shim in place of ruby/spec's real spec_helper.
cp "$HERE/spec_helper.rb" "$SPECDIR/spec_helper.rb"

RESULTS="$(mktemp)"
trap 'rm -f "$RESULTS"' EXIT

run_one() {
  f="$1"
  # Strip NULs: some specs print binary data, which bash command substitution
  # would warn about ("ignored null byte in input").
  out="$(cd "$SPECDIR" && timeout "$TIMEOUT" "$RBGO" "$f" 2>&1 | tr -d '\000')"
  res="$(printf '%s\n' "$out" | grep '^RBGO_RESULT' | head -1)"
  if [ -z "$res" ]; then
    printf 'FILEFAIL\t%s\n' "${f#"$SPECDIR"/}"
  else
    p="$(printf '%s' "$res" | sed -E 's/.*pass=([0-9]+).*/\1/')"
    printf 'OK\t%s\t%s\n' "${f#"$SPECDIR"/}" "$p"
  fi
}
export -f run_one
export SPECDIR RBGO TIMEOUT

echo
echo "== run language + core (${JOBS}-way) =="
find "$SPECDIR/language" "$SPECDIR/core" -name '*_spec.rb' | sort \
  | xargs -P "$JOBS" -I{} bash -c 'run_one "$@"' _ {} > "$RESULTS"

TOTAL_PASS="$(awk -F'\t' '$1=="OK"{s+=$3} END{print s+0}' "$RESULTS")"
FILES_OK="$(awk -F'\t' '$1=="OK"' "$RESULTS" | wc -l | tr -d ' ')"
FILES_FAIL="$(awk -F'\t' '$1=="FILEFAIL"' "$RESULTS" | wc -l | tr -d ' ')"

echo
echo "passing examples: $TOTAL_PASS   (files loaded: $FILES_OK, failed to load: $FILES_FAIL)"
echo "frozen floor:     $FLOOR"

if [ "${UPDATE_FLOOR:-0}" = "1" ]; then
  echo "$TOTAL_PASS" > "$HERE/FLOOR"
  echo "FLOOR updated to $TOTAL_PASS"
  exit 0
fi

if [ "$TOTAL_PASS" -lt "$FLOOR" ]; then
  echo "::error::ruby/spec ratchet REGRESSION: $TOTAL_PASS passing < floor $FLOOR"
  echo "-- files that failed to load --"
  awk -F'\t' '$1=="FILEFAIL"{print "   "$2}' "$RESULTS" | head -40
  exit 1
fi

if [ "$TOTAL_PASS" -gt "$FLOOR" ]; then
  echo "note: $((TOTAL_PASS - FLOOR)) more passing than the floor — bump FLOOR to lock this in (UPDATE_FLOOR=1)."
fi
echo "ruby/spec ratchet OK"
