#!/usr/bin/env bash
# RSpec conformance harness for go-embedded-ruby (rbgo).
#
# Three stages:
#   1. build rbgo + the parsesweep helper
#   2. parse-sweep the cloned RSpec lib/ trees (rbgo front-end vs `ruby -c`)
#   3. DSL-usage diff: run hand-written RSpec-style snippets through rbgo and
#      MRI and compare stdout.
#
# Re-runnable. Skips gracefully if the RSpec repos are not cloned (offline) or
# if MRI `ruby` is unavailable (rbgo-only run, no oracle diff).
#
# Env:
#   RBGO      path to rbgo binary       (default: built from ./cmd/rbgo EVERY run)
#   RUBY      path to MRI ruby          (default: first `ruby` on PATH)
#   CLONES    dir holding rspec-* repos (default: /tmp/rspec-clones)
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/../../.." && pwd)"
RBGO="${RBGO:-}"
RUBY="${RUBY:-$(command -v ruby || true)}"
CLONES="${CLONES:-/tmp/rspec-clones}"
PARSESWEEP="${PARSESWEEP:-/tmp/parsesweep}"

echo "== build rbgo =="
# #964: built EVERY run, into a directory this run owns and deletes. The old
# default reused any /tmp/rbgo already there, built by whichever checkout ran
# first; scripts/oracle.sh and apps/run.sh then picked that same file up.
if [ -z "$RBGO" ]; then
  RBGO_DIR="$(mktemp -d)"
  trap 'rm -rf "$RBGO_DIR"' EXIT
  RBGO="$RBGO_DIR/rbgo"
  (cd "$REPO_ROOT" && GOWORK=off go build -o "$RBGO" ./cmd/rbgo) || exit 1
fi
(cd "$REPO_ROOT" && GOWORK=off go build -o "$PARSESWEEP" ./scripts/conformance/rspec/parsesweep) || exit 1
echo "rbgo: $RBGO"
go version -m "$RBGO" | grep -E 'vcs\.(revision|modified)' ||
  echo "  (no vcs stamp in $RBGO: built outside a git tree, or with -buildvcs=false)"
echo "ruby: ${RUBY:-<none>}"

REPOS="rspec-support rspec-core rspec-expectations rspec-mocks"

echo
echo "== clone rspec repos (shallow) =="
mkdir -p "$CLONES"
for r in $REPOS; do
  if [ -d "$CLONES/$r/.git" ]; then
    echo "  $r: present"
  elif command -v git >/dev/null 2>&1; then
    (cd "$CLONES" && git clone --depth 1 "https://github.com/rspec/$r" >/dev/null 2>&1) \
      && echo "  $r: cloned" || echo "  $r: clone FAILED (offline?) - skipping"
  else
    echo "  $r: git missing - skipping"
  fi
done

LIBDIRS=""
for r in $REPOS; do
  [ -d "$CLONES/$r/lib" ] && LIBDIRS="$LIBDIRS $CLONES/$r/lib"
done

if [ -n "$LIBDIRS" ]; then
  echo
  echo "== parse sweep (rbgo front-end over lib/ trees) =="
  # shellcheck disable=SC2086
  "$PARSESWEEP" $LIBDIRS
else
  echo "== parse sweep skipped (no repos cloned) =="
fi

echo
echo "== DSL usage diff (rbgo vs MRI) =="
pass=0; fail=0; nooracle=0
for f in "$HERE"/dsl/*.rb; do
  name="$(basename "$f")"
  rb_out="$("$RBGO" run "$f" 2>&1)"; rb_rc=$?
  if [ -n "$RUBY" ]; then
    mri_out="$("$RUBY" "$f" 2>&1)"; mri_rc=$?
    if [ "$rb_out" = "$mri_out" ] && [ "$rb_rc" = "$mri_rc" ]; then
      echo "  PASS  $name"
      pass=$((pass+1))
    else
      echo "  FAIL  $name (rbgo rc=$rb_rc mri rc=$mri_rc)"
      echo "    --- rbgo ---"; echo "$rb_out" | sed 's/^/    /'
      echo "    --- mri  ---"; echo "$mri_out" | sed 's/^/    /'
      fail=$((fail+1))
    fi
  else
    echo "  RBGO-only  $name rc=$rb_rc"
    echo "$rb_out" | sed 's/^/    /'
    nooracle=$((nooracle+1))
  fi
done

echo
echo "== DSL summary: pass=$pass fail=$fail no-oracle=$nooracle =="
