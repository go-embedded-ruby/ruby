#!/bin/sh
# sweep.sh — heavyweight parse-conformance sweep for go-embedded-ruby.
#
# Confronts rbgo's front-end (parser.Parse + compiler.Compile, NO execution)
# with two of the largest real-world Ruby codebases — Ruby on Rails and Puppet —
# and measures how much idiomatic, large-scale Ruby the front-end accepts.
#
# MRI `ruby -c` is the oracle for "is this valid Ruby?". For every .rb file we
# record the rbgo front-end verdict and the MRI verdict, then classify:
#
#   both-accept                 rbgo OK,    MRI valid     (conformant)
#   rbgo-gap                    rbgo FAIL,  MRI valid     (REAL front-end gap)
#   both-reject                 rbgo FAIL,  MRI invalid   (not our problem)
#   rbgo-accepts-mri-rejects    rbgo OK,    MRI invalid   (over-permissive; rare)
#
# Acceptance rate is computed over MRI-valid files only (the files a correct
# Ruby front-end is expected to accept).
#
# Usage:
#   scripts/conformance/heavyweight/sweep.sh [REPOS_DIR] [OUT_DIR]
#
# REPOS_DIR (default /tmp/conf-repos) must contain shallow clones `rails/` and
# `puppet/`. If missing, the script clones them. OUT_DIR (default
# /tmp/ger-heavyweight-out) receives the raw TSVs and the summary.
#
# Environment overrides:
#   FRONTEND  path to the built front-end checker (default: build from source)
#   MRI       MRI ruby binary (default: ruby); if absent, MRI columns are skipped
#   REPO_ROOT go-embedded-ruby module root (default: derived from this script)
#
# Re-runnable and offline-graceful: an absent MRI degrades to rbgo-only counts;
# an absent network with repos already cloned still runs.
set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=${REPO_ROOT:-$(CDPATH= cd -- "$here/../../.." && pwd)}
REPOS_DIR=${1:-/tmp/conf-repos}
OUT_DIR=${2:-/tmp/ger-heavyweight-out}
MRI=${MRI:-ruby}
FRONTEND=${FRONTEND:-}

mkdir -p "$REPOS_DIR" "$OUT_DIR"

# --- build (or locate) the front-end checker ---------------------------------
if [ -z "$FRONTEND" ]; then
	FRONTEND="$OUT_DIR/frontend"
	echo ">> building front-end checker" >&2
	(cd "$REPO_ROOT" && GOWORK=off go build -o "$FRONTEND" \
		./scripts/conformance/heavyweight/frontend/main.go)
fi

# --- ensure repos present -----------------------------------------------------
clone_if_missing() { # name, url
	if [ ! -d "$REPOS_DIR/$1" ]; then
		echo ">> cloning $1" >&2
		git clone --depth 1 "$2" "$REPOS_DIR/$1" || {
			echo "!! could not clone $1 (offline?) — skipping" >&2
			return 1
		}
	fi
	return 0
}
clone_if_missing rails https://github.com/rails/rails || true
clone_if_missing puppet https://github.com/puppetlabs/puppet || true

have_mri=no
if command -v "$MRI" >/dev/null 2>&1; then have_mri=yes; else
	echo ">> MRI ($MRI) not installed — acceptance rate falls back to all files" >&2
fi

# --- per-repo sweep -----------------------------------------------------------
sweep_repo() { # repo-name
	repo=$1
	dir="$REPOS_DIR/$repo"
	[ -d "$dir" ] || { echo ">> $repo absent — skipped" >&2; return; }

	list="$OUT_DIR/$repo.files"
	find "$dir" -name '*.rb' -type f | sort >"$list"
	n=$(wc -l <"$list" | tr -d ' ')
	echo ">> $repo: $n .rb files — running rbgo front-end" >&2

	# rbgo front-end verdicts (one process, all files via -list).
	"$FRONTEND" -list "$list" >"$OUT_DIR/$repo.rbgo.tsv"

	# MRI oracle verdicts.
	mri="$OUT_DIR/$repo.mri.tsv"
	: >"$mri"
	if [ "$have_mri" = yes ]; then
		echo ">> $repo: running MRI -c oracle" >&2
		while IFS= read -r f; do
			if "$MRI" -c "$f" >/dev/null 2>&1; then
				printf 'VALID\t%s\n' "$f" >>"$mri"
			else
				printf 'INVALID\t%s\n' "$f" >>"$mri"
			fi
		done <"$list"
	fi
}

for repo in rails puppet; do sweep_repo "$repo"; done

# --- join + classify (done in awk for speed) ---------------------------------
classify() { # repo
	repo=$1
	rbgo="$OUT_DIR/$repo.rbgo.tsv"
	mri="$OUT_DIR/$repo.mri.tsv"
	[ -f "$rbgo" ] || return
	awk -v repo="$repo" -v have_mri="$have_mri" -F'\t' '
		FNR==NR { kind[$2]=$1; next }              # rbgo pass: kind by path
		{ mriv[$2]=$1 }                            # mri  pass: VALID/INVALID
		END {
			for (p in kind) {
				k=kind[p]
				ok = (k=="OK")
				v  = (have_mri=="yes") ? (mriv[p]=="VALID") : 1
				if (have_mri!="yes") {
					if (ok) bothA++; else rbgoGap++
					continue
				}
				if (ok && v) bothA++
				else if (!ok && v) rbgoGap++
				else if (!ok && !v) bothR++
				else overperm++
			}
			tot = bothA + rbgoGap + bothR + overperm
			validTot = bothA + rbgoGap            # MRI-valid files
			rate = (validTot>0) ? (100.0*bothA/validTot) : 0
			printf "%s\ttotal=%d\tboth_accept=%d\trbgo_gap=%d\tboth_reject=%d\toverperm=%d\tmri_valid=%d\taccept_rate=%.2f%%\n",
				repo, tot, bothA, rbgoGap, bothR, overperm, validTot, rate
		}
	' "$rbgo" "$mri" 2>/dev/null || true
}

summary="$OUT_DIR/summary.tsv"
: >"$summary"
for repo in rails puppet; do classify "$repo" >>"$summary"; done

echo "" >&2
echo "==== SUMMARY ====" >&2
cat "$summary" >&2
echo "" >&2
echo "raw TSVs + summary in: $OUT_DIR" >&2
