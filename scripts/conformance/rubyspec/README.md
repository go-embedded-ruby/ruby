# ruby/spec conformance ratchet

A shrink-only gate on rbgo's language conformance, measured against
[ruby/spec](https://github.com/ruby/spec) (the executable specification of the
Ruby language and core library).

## What it does

`run.sh` runs the `language/` and `core/` suites of ruby/spec — pinned to the
commit in `SPEC_SHA` — through rbgo, under a minimal MSpec-compatible shim
(`spec_helper.rb`, installed in place of ruby/spec's real `spec_helper.rb`).
Each spec file runs in its own `rbgo` process; the shim prints a
`RBGO_RESULT pass=.. fail=.. error=.. skip=..` line at exit.

The sweep is then handed to `cmd/ratchet`, which judges it **file by file**
against `BASELINE`:

| verdict | meaning | gated |
|---|---|---|
| `REGRESSED` | the file loaded and passed fewer examples than its baseline | **yes** |
| `FAILED TO LOAD` | the baseline says it loads; this run says it did not | **yes** |
| `MISSING FROM THE SWEEP` | in the baseline, absent from the run entirely | **yes** |
| `improved` / `now loading` | more than the baseline | no — reported |
| `new files` | in the run, absent from the baseline (a corpus bump) | no — reported |

The passing total is still printed, but as a derived summary rather than as the
thing being gated.

### Why per file, and not one number

The gate used to be a single frozen scalar in a `FLOOR` file, and it could not
be calibrated. A spec file that fails to load carries **tens** of examples —
one flip was measured moving the total by **43** on a documentation-only commit
— so the floor needed a margin wider than the largest file. A gate with a
43-example margin is blind to every regression smaller than 43, which is to say
all the real ones.

The total was also **unattributable**: −43 is *one file not loading* or *43
specs regressing*, and those need opposite responses — rerun versus bisect.

Judged per file, no margin is needed and every move names its file. And the
compensation a single total allows is caught: one file losing an example while
another gains nineteen **raises** the total, and the ratchet still fails.

### Why the judging is Go

Reading a sweep and deciding is where a silent failure reads as a clean result.
`cmd/ratchet` refuses input it cannot read — an empty sweep, an unrecognised
record, an `OK` line with no count, a count that is not a number, the same file
twice — and exits **2**, distinct from **1** for a real regression, so a broken
ratchet cannot be mistaken for a failing one.

## Usage

```sh
# Run the ratchet (clones the pinned corpus to /tmp on first run):
scripts/conformance/rubyspec/run.sh

# Re-record the baseline after a genuine improvement:
UPDATE_BASELINE=1 scripts/conformance/rubyspec/run.sh

# Point at an existing checkout instead of cloning:
SPECDIR=~/src/ruby-spec scripts/conformance/rubyspec/run.sh
```

Environment overrides: `RBGO`, `SPECDIR`, `CACHE`, `JOBS`, `TIMEOUT`.

## Re-recording the baseline

When a change improves conformance, regenerate `BASELINE` in the same PR
(`UPDATE_BASELINE=1`) and, if you moved to a newer ruby/spec snapshot, update
`SPEC_SHA` alongside it — a corpus bump changes which files exist, and the judge
reports files that appear or vanish rather than absorbing them. The baseline is
written sorted and in the same format it reads, so a regenerated one diffs
cleanly against its predecessor and the review sees exactly which files moved.

**Record it on an unloaded machine.** A pass/fail count is corrupted under load
through the per-file timeout: a heavy file that would pass can exceed `TIMEOUT`
and be recorded as not loading, which bakes the load into the gate. The CI job
(`.github/workflows/rubyspec-ratchet.yml`) enforces the baseline on every push
and pull request, and raises `TIMEOUT` to 60s for exactly this reason.

## Caveats

The shim is not full MSpec: a few matchers (`complain`, `output`,
`be_computed_by`, `ruby_exe`, some `argf`/IO helpers) are stubbed, so those
examples count as skipped rather than passing. The measured totals are therefore
a conservative lower bound on rbgo's true conformance — which is exactly what a
ratchet should be.
