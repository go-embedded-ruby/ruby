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

Measured on `main` after #849: **59 attributable expectations over 2206 spec
files, 0 candidates** — so **no pass anywhere in the corpus is bought by a
missing method**, and any future non-zero is the signal.

It was 1 before #849 (`Array#rfind`), and that one is instructive: the example
read *"raises a NoMethodError if the ifnone argument does not respond to
#call"*, and on a build without `rfind` it passed because `rfind` itself was
undefined. `BASELINE` recorded that file as `0`, so the counted total never held
it — the pass only appeared with the shim at 4.0, which is where this tool will
matter when the version moves.

The 2206 is the same population `BASELINE` sums, which is the check that this
tool and the gate are looking at the same corpus.

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
## Which examples the version string selects — `cmd/guards`

`cmd/guards` and `cmd/phantom` answer different questions about the same
number and neither replaces the other: guards says **which examples the
version string lets into the population**, phantom says **whether a pass in
that population is behaviour or an absence**. Both are analysis tools rather
than gates, and both print their own control first.

`spec_helper.rb` sets `CUR_VERSION`, and the corpus guards whole blocks and
individual values on it. Moving that string moves the population **in both
directions**, so `cmd/guards` censuses the corpus before anything is
re-recorded — a static read, needing no sweep, and so runnable on a loaded
machine where a sweep is not:

```bash
go run ./scripts/conformance/rubyspec/cmd/guards -root /tmp/rubyspec-corpus -list
```

It separates the two shapes, because they do opposite things:

| shape | at a version below the threshold | raising `CUR_VERSION` |
|---|---|---|
| `ruby_version_is "4.0" do … end` | the examples inside are **skipped** | **adds** them to the population |
| `ruby_version_is("4.0") ? a : b` | the example **runs**, asserting the pre-4.0 branch | **changes what it asserts** |

⛔ **It reports its own control first.** The guard identification is checked
against the figure in [#799](https://github.com/go-embedded-ruby/ruby/issues/799)
(56 guards at `"4.0"` under a `do$` anchor); the example counts are a **lower
bound**, since a static reader cannot expand `it_behaves_like`. Read the guard
columns as the measurement and the example columns as a floor — and if the
control ever stops reproducing, nothing below it is worth reading.

It is an analysis tool, not a gate: nothing fails when it is not run. What it
exists to prevent is a `UPDATE_BASELINE=1` that absorbs a loss silently.

## Asking the reference interpreter — `cmd/oracle`

Every parity fix starts with *"what does MRI actually do here?"*, and the obvious
way to ask it loses the answer. `ruby probe.rb > out 2>&1` discards the **exit
status**; a pipe into `grep` or `tail` discards whichever part of the answer did
not match the pattern the asker already had in mind; and running the reference
and rbgo as two separate commands invites comparing a fresh measurement against
a remembered one.

```sh
# the reference, the control, and the treatment — in one invocation
go run ./scripts/conformance/rubyspec/cmd/oracle \
  -script probe.rb /usr/bin/ruby ./rbgo-main ./rbgo-branch
```

It **parses nothing**. Both streams are copied through verbatim with their byte
counts, the status is printed as a number (or `KILLED`, for a probe that never
returns — one of the defects found this way was an endless range that hung), and
the only judgement made is byte equality between engines:

```text
=== the engines DISAGREED ===
  /usr/bin/ruby vs ./rbgo-main:   stderr 1008 vs 407 bytes
  /usr/bin/ruby vs ./rbgo-branch: stderr 1008 vs 1008 bytes
```

That second row is the point. **Same length, different content** — the branch
had gained the warning but still differed from MRI in the line number and the
quote style. A comparison by length would have called it parity, which is why
`-selftest` plants exactly that case (`'$x'` vs `'$y'`, equal length, one byte
apart) and refuses to pass unless the comparator sees it:

```console
$ go run ./scripts/conformance/rubyspec/cmd/oracle -selftest
selftest: the comparison is right on all 5 cases
```

Exit status is `0` when the engines agreed, `1` when they did not, and `2` for a
refusal — no script, an unreadable script, no engine named, or an engine that
would not start. The last distinction is deliberate and tested: *"could not
ask"* and *"answered differently"* call for opposite responses, so a missing
interpreter must never arrive as a disagreement.

Three engines is the shape the measurement rules want: reference, control
(`main`), treatment (the branch). A difference between the last two that the
reference does not share is the defect, or the fix.

## Caveats

The shim is not full MSpec: a few matchers (`complain`, `output`,
`be_computed_by`, `ruby_exe`, some `argf`/IO helpers) are stubbed, so those
examples count as skipped rather than passing. The measured totals are therefore
a conservative lower bound on rbgo's true conformance — which is exactly what a
ratchet should be.

`cmd/phantom` is the other half of that honesty: the stubbed matchers make the
total too LOW, and a pass bought by an absence would make it too high. Neither is
visible in the number itself.
