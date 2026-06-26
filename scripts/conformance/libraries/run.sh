#!/bin/sh
# run.sh — library parse-conformance sweep for go-embedded-ruby.
#
# Confronts rbgo's front-end (parser.Parse + compiler.Compile, NO execution)
# with the lib/ trees of widely-used real-world Ruby libraries and measures how
# much each one rbgo accepts. Released gems are valid Ruby, so a file rbgo
# rejects is a genuine front-end gap; acceptance = OK / total .rb files.
#
# This complements the existing sweeps:
#   - scripts/conformance/heavyweight/  (Rails, Puppet — the largest codebases)
#   - scripts/conformance/apps/         (load + usage of small libraries)
#   - scripts/conformance/rspec/        (RSpec DSL usage)
# and the stdlib (run with FRONTEND over RbConfig's rubylibdir).
#
# Usage:
#   scripts/conformance/libraries/run.sh [CLONES_DIR]
#
# CLONES_DIR (default /tmp/conf-libs) holds the shallow clones; missing repos
# are cloned (240s timeout each). Re-runnable and offline-graceful: an already
# cloned repo is reused; a clone that fails (offline) is reported and skipped.
#
# Env: FRONTEND (path to the built front-end checker; default: build from the
# heavyweight checker source). REPO_ROOT (module root; default: derived).
set -u

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=${REPO_ROOT:-$(CDPATH= cd -- "$here/../../.." && pwd)}
CLONES=${1:-/tmp/conf-libs}
FRONTEND=${FRONTEND:-}
mkdir -p "$CLONES"

if [ -z "$FRONTEND" ]; then
	FRONTEND="$CLONES/frontend"
	echo ">> building front-end checker"
	(cd "$REPO_ROOT" && GOWORK=off go build -o "$FRONTEND" scripts/conformance/heavyweight/frontend/main.go)
fi

# sweep NAME REPO_URL [SUBDIR]
sweep() {
	name=$1
	url=$2
	sub=${3:-lib}
	dir="$CLONES/$name"
	if [ ! -d "$dir/.git" ]; then
		timeout 240 git clone --depth 1 "$url" "$dir" >/dev/null 2>&1 || {
			printf '%-16s CLONE FAILED (offline?) — skipped\n' "$name"
			return
		}
	fi
	find "$dir/$sub" -name '*.rb' >"$CLONES/$name.files" 2>/dev/null
	total=$(grep -c . "$CLONES/$name.files" 2>/dev/null || echo 0)
	if [ "$total" -eq 0 ]; then
		printf '%-16s no .rb files under %s\n' "$name" "$sub"
		return
	fi
	"$FRONTEND" -list "$CLONES/$name.files" >"$CLONES/$name.tsv" 2>/dev/null
	ok=$(grep -c '^OK' "$CLONES/$name.tsv" 2>/dev/null || echo 0)
	printf '%-16s %5d/%-5d accepted  %5.1f%%\n' "$name" "$ok" "$total" "$(awk "BEGIN{print $ok*100/$total}")"
}

echo "==== library parse-conformance (rbgo front-end) ===="
sweep rubocop     https://github.com/rubocop/rubocop
sweep sinatra     https://github.com/sinatra/sinatra
sweep asciidoctor https://github.com/asciidoctor/asciidoctor
sweep kramdown    https://github.com/gettalong/kramdown
sweep thor        https://github.com/rails/thor
sweep concurrent  https://github.com/ruby-concurrency/concurrent-ruby lib/concurrent-ruby
sweep chef        https://github.com/chef/chef
sweep jekyll      https://github.com/jekyll/jekyll
sweep homebrew    https://github.com/Homebrew/brew Library/Homebrew
sweep dry-struct  https://github.com/dry-rb/dry-struct

echo
echo "==== top front-end gap categories (all libraries) ===="
cat "$CLONES"/*.tsv 2>/dev/null | awk -F'\t' '$1=="PARSE"||$1=="COMPILE"{e=$3;gsub(/[0-9]+/,"N",e);gsub(/"[^"]*"/,"Q",e);gsub(/'\''[^'\'']*'\''/,"Q",e);print e}' | sort | uniq -c | sort -rn | head -15
