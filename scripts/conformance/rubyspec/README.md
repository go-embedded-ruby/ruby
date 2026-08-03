# ruby/spec conformance ratchet

A shrink-only gate on rbgo's language conformance, measured against
[ruby/spec](https://github.com/ruby/spec) (the executable specification of the
Ruby language and core library).

## What it does

`run.sh` runs the `language/` and `core/` suites of ruby/spec — pinned to the
commit in `SPEC_SHA` — through rbgo, under a minimal MSpec-compatible shim
(`spec_helper.rb`, installed in place of ruby/spec's real `spec_helper.rb`).
Each spec file runs in its own `rbgo` process; the shim prints a
`RBGO_RESULT pass=.. fail=.. error=.. skip=..` line at exit. The runner sums the
passing examples across every file and compares the total to the **frozen floor**
in `FLOOR`.

- Total **below** the floor → the run fails (a conformance regression).
- Total **above** the floor → the run passes and prints how far ahead it is.

Because the floor can only be raised, rbgo's language conformance is tracked and
can only go up. The shim reports fixed platform/version guards, so the pass total
is stable across host OSes (it does not drift between the CI Linux runner and a
developer's machine).

## Usage

```sh
# Run the ratchet (clones the pinned corpus to /tmp on first run):
scripts/conformance/rubyspec/run.sh

# Re-freeze the floor after a genuine improvement:
UPDATE_FLOOR=1 scripts/conformance/rubyspec/run.sh

# Point at an existing checkout instead of cloning:
SPECDIR=~/src/ruby-spec scripts/conformance/rubyspec/run.sh
```

Environment overrides: `RBGO`, `SPECDIR`, `CACHE`, `JOBS`, `TIMEOUT`.

## Raising the floor

When a change improves conformance, bump `FLOOR` to the newly measured total in
the same PR (run with `UPDATE_FLOOR=1`) and, if you moved to a newer ruby/spec
snapshot, update `SPEC_SHA` alongside it. The CI job
(`.github/workflows/rubyspec-ratchet.yml`) enforces the floor on every push and
pull request.

## Caveats

The shim is not full MSpec: a few matchers (`complain`, `output`,
`be_computed_by`, `ruby_exe`, some `argf`/IO helpers) are stubbed, so those
examples count as skipped rather than passing. The measured total is therefore a
conservative lower bound on rbgo's true conformance — which is exactly what a
ratchet floor should be.
