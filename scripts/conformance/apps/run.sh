#!/bin/sh
# run.sh — real-world conformance confrontation for go-embedded-ruby.
#
# Runs the SOURCE and representative USAGE of well-known reference Ruby
# libraries through rbgo (pure-Go Ruby, the implementation under test) and
# compares against MRI/CRuby (the oracle). The goal is a measured gap map, not
# green suites: big libraries are not expected to load end-to-end.
#
# For each library two confrontations run, each rbgo-vs-MRI:
#   (a) LOAD  — require the library entrypoint; does rbgo parse+load like MRI?
#   (b) USAGE — representative API snippets from the README/docs; compare stdout.
#
# Every failure is categorized: parse-error | missing-method |
# missing-class/module | unsupported-stdlib-require | wrong-behavior |
# external-gem | C-extension.
#
# Usage:
#   scripts/conformance/apps/run.sh              # use cached clones, skip if absent
#   scripts/conformance/apps/run.sh --clone      # clone missing libs (needs network)
#   scripts/conformance/apps/run.sh --only rake  # one library
#
# Environment overrides: RBGO (default /tmp/rbgo, else built from ./cmd/rbgo),
# MRI (default: ruby), CACHE (default /tmp/ger-apps-cache).
set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo=$(CDPATH= cd -- "$here/../../.." && pwd)
RBGO=${RBGO:-}
MRI=${MRI:-ruby}
CACHE=${CACHE:-/tmp/ger-apps-cache}
CLONE=0
ONLY=""

while [ $# -gt 0 ]; do
	case "$1" in
	--clone) CLONE=1 ;;
	--only) shift; ONLY=$1 ;;
	-h | --help) sed -n '2,28p' "$0"; exit 0 ;;
	*) echo "unknown arg: $1" >&2; exit 2 ;;
	esac
	shift
done

if [ -z "$RBGO" ]; then
	if [ -x /tmp/rbgo ]; then
		RBGO=/tmp/rbgo
	else
		RBGO=/tmp/rbgo-apps
		echo "building rbgo -> $RBGO"
		(cd "$repo" && GOWORK=off go build -o "$RBGO" ./cmd/rbgo)
	fi
fi

if ! command -v "$MRI" >/dev/null 2>&1; then
	echo "MRI ($MRI) not installed — the oracle is required; aborting." >&2
	exit 1
fi

mkdir -p "$CACHE"

# Library ladder: name | github repo | lib dir (relative to clone) | entrypoint feature
# Ordered most-tractable first.
LIBS='
mustache|mustache/mustache|lib|mustache
minitest|seattlerb/minitest|lib|minitest
rake|ruby/rake|lib|rake
liquid|Shopify/liquid|lib|liquid
rack|rack/rack|lib|rack
i18n|ruby-i18n/i18n|lib|i18n
'

clone_lib() { # repo destdir
	repo_path=$1; dest=$2
	if [ -d "$dest" ]; then return 0; fi
	if [ "$CLONE" -ne 1 ]; then
		echo "  (not cached and --clone not given — skipped)"
		return 1
	fi
	git clone --depth 1 -q "https://github.com/$repo_path.git" "$dest" 2>&1 | tail -1 || return 1
}

# diff_run: run one snippet through rbgo and MRI inside libdir (so plain require
# resolves against lib/), and classify the result. Prints "PASS" or
# "FAIL:<category>" plus a one-line detail.
classify_rbgo() { # rbgo_output -> category on stdout
	out=$1
	case "$out" in
	*"SyntaxError"* | *"parse error"*) echo "parse-error" ;;
	*"undefined method"*) echo "missing-method" ;;
	*"uninitialized constant"* | *"NameError"*) echo "missing-class/module" ;;
	*"cannot load such file"* | *"LoadError"*) echo "unsupported-stdlib-require" ;;
	*) echo "wrong-behavior" ;;
	esac
}

PASS=0; FAIL=0
HIST_parse=0; HIST_method=0; HIST_class=0; HIST_stdlib=0; HIST_wrong=0; HIST_gem=0

run_load() { # name libdir entry
	name=$1; libdir=$2; entry=$3
	rbgo_out=$(cd "$libdir" && "$RBGO" run -e "require '$entry'; puts 'LOADED-OK'" 2>&1 | tail -1)
	mri_out=$("$MRI" -I "$libdir" -e "require '$entry'; puts 'LOADED-OK'" 2>&1 | tail -1)
	printf '  LOAD  rbgo: %s\n' "$rbgo_out"
	printf '  LOAD  mri : %s\n' "$mri_out"
	if [ "$rbgo_out" = "LOADED-OK" ] && [ "$mri_out" = "LOADED-OK" ]; then
		echo "  LOAD  => PASS"
		PASS=$((PASS + 1))
	elif [ "$mri_out" != "LOADED-OK" ]; then
		echo "  LOAD  => SKIP (MRI cannot load either — external dependency)"
		HIST_gem=$((HIST_gem + 1))
	else
		cat=$(classify_rbgo "$rbgo_out")
		echo "  LOAD  => FAIL:$cat"
		FAIL=$((FAIL + 1))
		case "$cat" in
		parse-error) HIST_parse=$((HIST_parse + 1)) ;;
		missing-method) HIST_method=$((HIST_method + 1)) ;;
		missing-class/module) HIST_class=$((HIST_class + 1)) ;;
		unsupported-stdlib-require) HIST_stdlib=$((HIST_stdlib + 1)) ;;
		*) HIST_wrong=$((HIST_wrong + 1)) ;;
		esac
	fi
}

run_usage() { # name libdir snippetfile
	name=$1; libdir=$2; snipf=$3
	[ -f "$snipf" ] || return 0
	up=0; uf=0; n=0
	while IFS= read -r line; do
		case "$line" in '' | '#'*) continue ;; esac
		n=$((n + 1))
		r=$(cd "$libdir" && "$RBGO" run -e "$line" 2>&1 | tail -1)
		m=$("$MRI" -I "$libdir" -e "$line" 2>&1 | tail -1)
		if [ "$r" = "$m" ]; then
			up=$((up + 1))
		else
			uf=$((uf + 1))
			cat=$(classify_rbgo "$r")
			printf '  USAGE #%d => FAIL:%s\n        rbgo=[%s]\n        mri =[%s]\n' "$n" "$cat" "$r" "$m"
		fi
	done <"$snipf"
	printf '  USAGE => %d/%d match\n' "$up" "$n"
	PASS=$((PASS + up)); FAIL=$((FAIL + uf))
}

echo "rbgo: $RBGO"
echo "mri : $($MRI --version)"
echo "cache: $CACHE"
echo "======================================================================"

# Iterate without a pipe so counters accumulate in this shell.
OLDIFS=$IFS
printf '%s\n' "$LIBS" | grep -v '^[[:space:]]*$' >"$CACHE/.libs.tmp"
while IFS='|' read -r name repo_path libsub entry; do
	[ -z "$name" ] && continue
	[ -n "$ONLY" ] && [ "$ONLY" != "$name" ] && continue
	echo
	echo "### $name ($repo_path)"
	dest="$CACHE/$(basename "$repo_path")"
	if ! clone_lib "$repo_path" "$dest"; then continue; fi
	libdir="$dest/$libsub"
	if [ ! -d "$libdir" ]; then echo "  (lib dir $libdir missing — skipped)"; continue; fi
	run_load "$name" "$libdir" "$entry"
	run_usage "$name" "$libdir" "$here/snippets/$name.txt"
done <"$CACHE/.libs.tmp"
IFS=$OLDIFS
rm -f "$CACHE/.libs.tmp"

# ActiveSupport core_ext: too large to load as a unit and depends on
# concurrent-ruby; instead measure how many individual pure-Ruby files rbgo can
# PARSE+RUN standalone (the front-end signal), categorizing each failure.
as_dir="$CACHE/rails/activesupport/lib/active_support/core_ext"
if [ -z "$ONLY" ] || [ "$ONLY" = "activesupport" ]; then
	echo
	echo "### activesupport (rails/rails — core_ext file-by-file parse sweep)"
	if [ "$CLONE" -eq 1 ] && [ ! -d "$as_dir" ]; then
		git clone --depth 1 -q --filter=blob:none --sparse \
			https://github.com/rails/rails.git "$CACHE/rails" 2>&1 | tail -1 || true
		(cd "$CACHE/rails" && git sparse-checkout set \
			activesupport/lib/active_support/core_ext 2>&1 | tail -1) || true
	fi
	if [ -d "$as_dir" ]; then
		as_total=0; as_ok=0; as_parse=0; as_load=0; as_method=0
		for f in $(find "$as_dir" -name '*.rb' | sort); do
			as_total=$((as_total + 1))
			out=$("$RBGO" run "$f" 2>&1 | tail -1)
			case "$out" in
			'') as_ok=$((as_ok + 1)) ;;
			*"parse error"* | *"SyntaxError"*) as_parse=$((as_parse + 1)) ;;
			*"cannot load such file"* | *"LoadError"*) as_load=$((as_load + 1)) ;;
			*"undefined method"*) as_method=$((as_method + 1)) ;;
			*) ;;
			esac
		done
		echo "  parses+runs standalone: $as_ok / $as_total"
		echo "  parse-error: $as_parse  intra-AS/stdlib-load: $as_load  missing-method: $as_method"
		PASS=$((PASS + as_ok))
		FAIL=$((FAIL + as_parse + as_method))
		HIST_parse=$((HIST_parse + as_parse))
		HIST_method=$((HIST_method + as_method))
	else
		echo "  (rails core_ext not cached and --clone not given — skipped)"
	fi
fi

echo
echo "======================================================================"
echo "AGGREGATE  pass=$PASS  fail=$FAIL"
echo "Failure histogram (rbgo divergences where MRI succeeds):"
echo "  parse-error                : $HIST_parse"
echo "  missing-method             : $HIST_method"
echo "  missing-class/module       : $HIST_class"
echo "  unsupported-stdlib-require : $HIST_stdlib"
echo "  wrong-behavior             : $HIST_wrong"
echo "  external-gem (MRI-blocked) : $HIST_gem  (not an rbgo fault)"
echo
echo "See CONFORMANCE-APPS.md for the curated, prioritized gap list."
