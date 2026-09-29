#!/usr/bin/env bash
# Copyright (c) the go-embedded-ruby/ruby authors
#
# SPDX-License-Identifier: BSD-3-Clause
#
# ruby/spec conformance RATCHET for go-embedded-ruby (rbgo).
#
# Runs the ruby/spec language + core suites through rbgo under a minimal
# MSpec-compatible shim (spec_helper.rb, vendored beside this script) and
# judges the result against a PER-FILE baseline (the BASELINE file beside this
# script). It is a shrink-only ratchet: if any file passes fewer examples than
# its recorded count, or stops loading, the run FAILS -- so conformance is
# tracked and can only go up. When rbgo improves, regenerate the baseline with
# UPDATE_BASELINE=1 to lock the win in.
#
# The judging is a Go program (cmd/ratchet beside this script), not more shell.
# Reading a sweep and deciding is where a silent failure reads as a clean
# result, and the tool refuses input it cannot read -- an empty sweep, an
# unrecognised record, a pass count that is not a number -- with exit 2, which
# is distinct from exit 1 for a real regression.
#
# It replaced a single frozen scalar, which could not be calibrated. A spec file
# that fails to load carries tens of examples -- one flip was measured moving the
# total by 43 on a documentation-only commit -- so the floor needed a margin
# wider than the largest file, and a gate with a 43-example margin is blind to
# every regression smaller than 43. The total was also unattributable: -43 is one
# file not loading, or 43 specs regressing, and those need opposite responses.
# Judged per file, no margin is needed and every move names its file.
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
#   UPDATE_BASELINE=1  rewrite BASELINE from this sweep and exit 0
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../../.." && pwd)"
RBGO="${RBGO:-/tmp/rbgo-rubyspec}"
CACHE="${CACHE:-/tmp/rubyspec-corpus}"
SPECDIR="${SPECDIR:-$CACHE}"
TIMEOUT="${TIMEOUT:-25}"
JOBS="${JOBS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)}"

# The ruby/spec commit the baseline was measured against. Bump together with
# BASELINE: a corpus bump changes which files exist, and the judge reports files
# that appear or vanish rather than silently absorbing them.
SPEC_SHA="87b1631992bd00cf0c4934474766d54dad088191"
SPEC_URL="https://github.com/ruby/spec"

BASELINE="$HERE/BASELINE"

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

# Hand the sweep to the judge. Everything above this line runs processes;
# everything below it decides, and that half is Go.
RATCHET="$(mktemp -d)/ratchet"
trap 'rm -f "$RESULTS" "$RATCHET"' EXIT
(cd "$REPO_ROOT" && GOWORK=off CGO_ENABLED=0 go build -o "$RATCHET" ./scripts/conformance/rubyspec/cmd/ratchet) || exit 2

echo
if [ "${UPDATE_BASELINE:-0}" = "1" ]; then
  exec "$RATCHET" -baseline "$BASELINE" -results "$RESULTS" -update
fi
exec "$RATCHET" -baseline "$BASELINE" -results "$RESULTS"
