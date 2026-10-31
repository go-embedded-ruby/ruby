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

## `cmd/phantom` — is a pass the behaviour, or the method's absence?

A spec that asserts an exception class cannot tell why it got one.
`core/array/rfind_spec.rb` has

```ruby
it "raises a NoMethodError if the ifnone argument does not respond to #call …" do
  -> { [1, 2, 3].rfind(42) { |x| false } }.should.raise(NoMethodError)
end
```

which means *"42 is not callable"*. On a build where `Array#rfind` does not exist,
`[1,2,3].rfind(42)` raises NoMethodError for **that** reason and the example
passes — a pass for a method the interpreter does not have.

`cmd/phantom` names those. For every `describe "X#y"` or `describe "X.y"` holding
a NoMethodError expectation it asks a real rbgo whether the method exists at all
(`method_defined?`/`private_method_defined?`, or `respond_to?` for the `.` form —
no instance is constructed), and reports the ones that do not.

```bash
go run ./scripts/conformance/rubyspec/cmd/phantom -root /tmp/rubyspec-corpus -rbgo /tmp/rbgo-rubyspec
```

Measured on `main` at 3a3fb818: **59 attributable expectations over 2206 spec
files, 1 candidate** (`Array#rfind`, implemented in #849), and `BASELINE` records
that file as `0` — so **no counted pass in the total is bought by an absence**.
The value of running it again is the comparison with that line.

It withdraws any describe block that **also asserts the absence**, because such
an example is asserting exactly what it got: `core/complex/negative_spec.rb`
("Complex#negative? is undefined") is conformance, not a phantom, and MRI really
does not define it. 23 of the 82 attributable expectations are of that kind.

Its limits are printed on every run rather than left in a comment: it reads the
describe string, skips operator methods, and stops attribution at a sibling
`describe` judged by indentation — so it can MISS a candidate but will not invent
one, and its count is a lower bound. Like `cmd/ratchet` it refuses rather than
guesses: exit 2 if the corpus cannot be read, if the interpreter cannot be run,
or if its own control fails (a method that certainly exists must come back
present, and one that cannot exist must come back absent).

## Caveats

The shim is not full MSpec: a few matchers (`complain`, `output`,
`be_computed_by`, `ruby_exe`, some `argf`/IO helpers) are stubbed, so those
examples count as skipped rather than passing. The measured totals are therefore
a conservative lower bound on rbgo's true conformance — which is exactly what a
ratchet should be.

`cmd/phantom` is the other half of that honesty: the stubbed matchers make the
total too LOW, and a pass bought by an absence would make it too high. Neither is
visible in the number itself.
