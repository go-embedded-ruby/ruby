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

### ⛔ Record it where it is enforced — `workflow_dispatch: rubyspec-baseline`

An unloaded machine is not enough, because it is still the **wrong** machine.
The gate is pinned to `ubuntu-24.04` precisely because the environment changes
the answer — `setrlimit` read 28 on one Ubuntu major and 29 on the next, same
commit (#838) — so a baseline recorded anywhere else is recorded somewhere the
gate will never run. Measured on one developer's laptop: **+9 examples** against
the CI-recorded baseline, for no change in the code at all.

`UPDATE_BASELINE=1` was a local command and nothing else, so *every* baseline
was recorded on the wrong machine, and the difference is invisible because both
machines print a number. Run the **rubyspec-baseline** lane instead
(Actions → rubyspec-baseline → Run workflow). It downloads as an artefact
called `BASELINE`, with the diffstat printed beside it; commit that file.

It and the gate are **one definition with two callers**
(`.github/workflows/rubyspec-sweep.yml`): the pinned image, the Go version, the
raised `TIMEOUT` and the memory watcher are written once. Two lanes agreeing
about `runs-on` would be an agreement somebody has to maintain, and the whole
point of pinning is that nobody notices when it lapses.

Nothing is committed from the lane. A lane that pushed to a branch would be a
lane that can change the gate it just ran, and the person who asked for the
re-record is exactly the person who should read the delta before it becomes the
record.

### ⛔ A flaky file must be recorded at its FLOOR, not at the draw you got

`core/random/bytes_spec.rb` answers **8 or 9 on the same binary** — documented
in four places in this repository before it bit, including `cmd/specdelta`'s own
comment, which exists *because* that file once reported `-1` between two engines
that differed only in a version string.

The 4.0.9 re-record drew **9** and banked it. The gate then failed on the very
next pull request, naming a regression that was a coin flip:

```text
passing examples: 23672 (baseline 23673, -1)
-- REGRESSED (1) --
   core/random/bytes_spec.rb: 8 passing, baseline 9 (-1)
```

That is not a conformance failure, it is a **~50 % flaky gate**, self-inflicted
by recording a maximum as if it were a guarantee. A baseline row means *"no run
may come in below this"*, so for an unstable file the only honest row is its
**lowest** observed value: at 8 the gate is deterministic, a 9 shows up as an
improvement, and a genuine drop to 7 is still caught.

Its row is therefore pinned to `8` by hand, and a re-record that raises it again
should be corrected rather than accepted — the number to trust is the floor, not
the luckiest sweep. `cmd/specdelta` already refuses to build a verdict on a
single run (`-repeat`, and the `UNSTABLE` marking); the ratchet has no such
notion, which is the real gap.

### ⛔ A re-record reads the old baseline first, and refuses to bank a loss

`-update` used to write straight over `BASELINE` **without reading it**, so the
one thing it could never report was whether the sweep replacing the record was
better or worse than it. The comparison simply never happened.

The hazard was written down twice in this repository and gated in neither —
`cmd/guards`' own header (*"`UPDATE_BASELINE=1` records whatever it sees and
would absorb a loss"*) and the ratchet lane's comment about an Ubuntu image
roll: *"the same run also showed `kernel/require` +1, equally environmental, and
`UPDATE_BASELINE=1` would bank it as progress."* Two notes, no refusal.

You re-record to lock in a **win**. A file that went backwards is either a real
regression or an environment that moved, and both need a person — not an
overwrite that erases the evidence of itself:

```console
$ UPDATE_BASELINE=1 scripts/conformance/rubyspec/run.sh
re-record: 2206 files measured, 2206 in the baseline being replaced

-- REGRESSED (1) --
   core/a_spec.rb: 9 passing, baseline 10 (-1)

::error::REFUSING to re-record: 1 file(s) went backwards -- named above
A re-record locks in a win. Fix the regression, or pass -absorb to record
it on purpose -- the baseline then carries a line naming every file absorbed.
```

It exits **1**, distinct in the source from the plain regression verdict
although they share the status: *"this branch broke something"* and *"I declined
to erase the evidence that something is broken"* send a reader to different
places.

`-absorb` is the named derogation and it **stamps the artefact**, not just the
log — a console warning scrolls away, and the baseline outlives the run that
wrote it by months:

```text
# ⛔ ABSORBED 1 file(s) that went backwards, recorded with -absorb on 2026-11-03:
#     core/a_spec.rb: 9 passing, baseline 10 (-1)
```

A refused re-record leaves `BASELINE` **byte for byte as it was**, which is
asserted: a refusal that truncated the file would destroy the record it declined
to replace. A first recording, with no baseline to be worse than, is not a
refusal at all.

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

### ⛔ It used to be an analysis tool, and that was the defect

This section read *"it is an analysis tool, not a gate: nothing fails when it
is not run"*. On 2026-11-01 it was not run, and the 4.0 version move was
measured by comparing **passing** counts between two engines and published as
**ten examples of work**. The real figure was about **seventy-five**.

Three `core/binding/implicit_parameter*_spec.rb` files are
`pass=0 fail=0 error=0` below 4.0 — every example behind a `ruby_version_is
"4.0"` guard — and **37 failures** above it, because Ruby 4.0 adds three
`Binding` methods rbgo does not have. A pass delta over those files is zero in
both directions, and `-dir` had never been pointed at `core/binding` at all.

The lesson *"ask what the target version **adds** before measuring what moves"*
was already written down and was not consulted. So the reading moved into
`internal/guardcensus`, and `cmd/specdelta` now **cannot produce a table** until
the caller declares which comparison it is making. The note was not the missing
part; a refusal was.

```bash
# what the move does to the CORPUS, before any engine runs
go run ./scripts/conformance/rubyspec/cmd/guards \
    -root ~/ruby-spec-corpus -move 3.4.1:4.0.9
```

Against the pinned corpus that move **opens 67 block guards in 50 files**
(≥145 examples), **closes 45** in 37 files, and **flips 5 inline sites** in 3.
Those 50 files are the surface any claim about the move has to cover.

It remains the right instrument for the other half too: a `UPDATE_BASELINE=1`
that absorbs a loss silently.

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

## Holding the corpus still — `cmd/specdelta`

`cmd/ratchet` judges one sweep against `BASELINE`. That is the right gate for
*"may this land"* and the wrong instrument for *"what does this change do"*: a
baseline comparison folds in every difference between the recording machine and
this one. `cmd/specdelta` asks the other question — hold the corpus and the
machine fixed, vary only the interpreter, and name every file that moved.

```console
$ specdelta -root ~/ruby-spec-corpus -move none -dir core/set -quiet -repeat 3 ./rbgo-main ./rbgo-409
MOVE none declared: both engines read the same corpus, so every delta below is the ENGINE
file                                        control   treat  delta
core/set/flatten_merge_spec.rb                    3       0     -3
core/set/to_s_spec.rb                             2       4     +2
core/set/divide_spec.rb                           9       8     -1
...
57 file(s): control 175 passing, treatment 167 passing, -8
9 file(s) moved, 8 regressed
```

That table took the `Set` half of the 4.0 version move from "eight files to work
through" to **two defects** — the other six are examples 4.0 stopped asking for,
which the deltas cannot tell you and the `ruby_version_is` guards can.

**⛔ `-move` is required, and has no default.** The two runs above read the same
corpus; a run across a version move reads two *different* corpora, and the
tables look identical. Only one of them is a reading of the engines.

| declaration | what it asserts |
|---|---|
| `-move none` | both engines read the corpus at the same `RUBY_VERSION` |
| `-move 3.4.1:4.0.9` | they do not: the guards move too |

With a move declared, specdelta censuses the corpus through
`internal/guardcensus` — the same reading `cmd/guards` prints — and **refuses
before starting either engine** if the move touches a file `-dir` does not
measure:

```console
$ specdelta -root ~/ruby-spec-corpus -move 3.4.1:4.0.9 -dir core/set ./a ./b
specdelta: the move 3.4.1 → 4.0.9 touches 49 file(s) that -dir core/set does not measure:
    core/array/detect_spec.rb
    core/binding/implicit_parameter_defined_spec.rb
    core/binding/implicit_parameter_get_spec.rb
    core/binding/implicit_parameters_spec.rb
    …
  A number from this run would not be a number about the move. Either
    -dir core,language
  which measures the whole surface, or -partial to say so on purpose —
  the report is then stamped PARTIAL.
```

The refusal comes **first** because a `core/` sweep is an hour, and an hour
spent on a figure already known to be incomplete is the cost the gate exists to
avoid. A measured file the move opens is marked `⛔ THE MOVE OPENS ≥N examples
here` and is **never dropped by `-quiet`** — a delta of zero on such a file is
the signature of the defect, not of calm — and the summary reports the move's
share apart from the engines'.

### ⛔ `-dir` names a set, because the refusal's advice has to be followable

That refusal first read *"Widen `-dir`"*, and `-dir` took **one** directory.
The surface of the move this repository is actually making spans **two** suites
— `core/` and `language/` — so from `-dir core` the only widening available was
the whole corpus: `library/`, `optional/`, `command_line/`, `security/`, and
hours of sweep over files the move never touches.

That left `-partial` as the only reachable exit, which would have stamped the
headline figure for #799 `PARTIAL` not because the surface was incomplete but
because the flag could not express a complete one. **A derogation that is the
normal road is not a derogation**, and a gate whose honest exit nobody can
reach is not a gate.

So `-dir` is comma-separated, and the refusal computes the `-dir` that *would*
cover the surface from the census rather than telling the reader to widen:

```sh
specdelta -root ~/ruby-spec-corpus -move 3.4.1:4.0.9 -dir core,language \
    -repeat 3 -quiet ./rbgo-341 ./rbgo-409
```

Top-level suites and no deeper, on purpose: the 50 exact directories would be
unreadable and would go stale the moment the corpus moves, while
`core,language` is both the honest surface and something a reader can retype.
It reaches `core/` and `language/` and **not** `library/` — a suggestion that
over-reached would re-create the problem it fixes.

A file reached through two named directories (`-dir core,core/set`) is measured
**once**. Counted twice it would be counted on both sides, which makes the
totals move with how the caller spelled `-dir` rather than with the engines.
And `-dir core,` does not quietly become the whole corpus.

**⛔ Provenance, and it is a repeat offence.** On 2026-11-01 `pass=118 fail=1`
was published for [#858](https://github.com/go-embedded-ruby/ruby/pull/858) from
a binary built **before that branch existed**; it carried the previous branch's
code, and the true figure was 117/2. The notes already held the same shape from
an earlier arc — a control built from the *working tree* rather than from the
commit, attributing `+3` to the wrong fix. Written down twice, and it happened
again.

`go build` stamps `vcs.revision` and `vcs.modified` into every binary it
produces from a repository, so a binary can be **asked what it is**. specdelta
reads both engines before running either, prints what it found above the table,
and refuses the three ways a comparison is void before it starts:

| what it finds | why the comparison is void |
|---|---|
| the same revision twice | one of them was not rebuilt |
| `vcs.modified=true` | the revision does not name the code in the binary |
| the work tree has moved past the binary's revision | the binary is not the code being measured — **#858** |

An engine with no Go build information (MRI, a shell fake) is **not** refused —
it is legitimately not a Go binary — but the report is stamped
`⛔ UNPROVENANCED`, because a table nobody can attribute must not look like one
that can. `-unprovenanced` is the named derogation and it stamps rather than
silences.

**`-repeat N`.** One run per engine cannot tell a FLAKE from a difference. The
tool's first real use reported `core/random/bytes_spec.rb` as `-1` between two
binaries that differ by one file and three constants; three runs of each gave
control 9, 9, 8 and treatment 8, 9, 9. Above 1, each file runs N times per
engine, the **lowest** count is kept, and an engine that disagrees with itself
marks the file `UNSTABLE` and has its delta excluded from the verdict.

**⛔ An example that went red inside a file that improved.** On 2026-11-02 a
control and a treatment built from the same commit minus one change read:

```text
core/thread/raise_spec.rb   51 → 68   +17
53 file(s): control 328 passing, treatment 345 passing, +17
1 file(s) moved, 0 regressed
```

True, and incomplete. **Nineteen examples turned green and two turned red** —
and no count of passes, of failures, or of both can show a swap. The two were
found by hand, by comparing the two runs' failure *descriptions*; the example
that broke had been passing **because of** a different defect, so nothing about
it looked wrong.

specdelta now compares those descriptions as a multiset (the same shared block
included twice reports twice), names every one the treatment reports more often,
and **exits 1** for it — a per-file verdict of "0 regressed" must not carry an
example out:

```console
core/thread/raise_spec.rb                 51      68    +17
      ⛔ WENT RED: fail  shared kernel_raise | raises a TypeError when passed a message and an extra argument (×2)

1 file(s) moved, 0 regressed, 2 example(s) WENT RED
```

It keys on the **description** and not the message, because a message carries
object addresses that differ between two runs of the *same* binary.

**Two channels.** `RBGO_RESULT pass=…` comes out of the subject's own stdout, so
a subject that miscounts can claim anything. specdelta also counts the
`RBGO_DETAIL` lines and flags a file that printed **fewer** than its own
`fail + error` tally. It is an inequality and not an equality, because the shim
emits a detail with no counter for a `before(:all)` failure and a counter with
no detail for a guard-driven skip.

Exit **2**, never 1, for a refusal: an engine that would not start, an
unreadable result line, two result lines in one run, a missing or malformed
`-move`, a declared move whose surface this run does not cover, or an **empty
population** — a mistyped `-dir` would otherwise print `0 files, no regressions`
and exit 0.

### ⛔ A hang is not an absence, and the report says which

A row whose engine was stopped by `-timeout` reads

```console
library/net-ftp/put_spec.rb         39       0    -39  KILLED on timeout, the treatment
```

and the kill is reported **before** `DID NOT RUN`. The order is the whole point:
a killed engine prints no `RBGO_RESULT` line either, so `ran` is false and the
`DID NOT RUN` arms would otherwise claim every killed row — publishing a file
the **clock** stopped as a file the engine could not **load**. Those call for
opposite work: *the timeout is too short, or it hangs* against *this is
unimplemented*.

It did exactly that until 2026-11-10 (#915): `run()` returned a freshly parsed
result, so `killed` was false on every row ever printed and this note was dead
code. The documented way to judge a whole report — *grep it for `KILLED`; trust
the table when the count is 0* — could only ever answer 0. The first sweep after
the fix turned **75 files** from "unimplemented" into "hangs" (`Net::FTP` 39,
`Net::HTTP` 27, `Socket` 8, `Timeout` 1 — issue #916).

⛔ **Reports written before that commit cannot tell the two apart.**

**Pick the timeout for the question.** For *did this regress*, give a file
roughly 60× its usual work. For *does it hang or is it missing*, a SHORT budget
is the better instrument: 20 s is already ~100× a normal spec file, anything
past it is a hang by any definition, and the answer arrives in minutes. The
first re-measurement of `net-ftp` at 300 s was still running after 77 minutes
with nothing printed; at 20 s it answered in 13.

### Control bytes never reach the report

ruby/spec prints raw binary on purpose — zlib round-trips, `Net::HTTP` bodies,
`String#unpack` — and the shim quotes an example's description verbatim into
`RBGO_DETAIL`. Those bytes used to land in the report, and `file` then called it
*"RAGE Package Format (RPF)"*: **grep skips a file it decides is binary**, so
every search over it returned no output and exit 1, which reads exactly like
"no matches". Three checks of one `library/` report were passed on that silence.
`printable()` strips control bytes before parsing, keeping tab and newline
because the format is built on them — the same guard `run.sh` has always had one
layer out (`tr -d '\000'`).

## Rolling a report up to the module — `cmd/specroll`

specdelta answers per **file**. *"What is left to reach parity with MRI?"* is a
question about **modules**, and one run of `library/` is 497 rows with 189
moved — a shape no one reads by eye, and summarising it by eye is a count done
by eye.

```console
$ go run ./scripts/conformance/rubyspec/cmd/specroll -depth 2 -top 5 parity-*.txt
module                              files      MRI    rbgo     gap  unloadable (examples)
library/net-ftp                        47      448       6     442  39 (388)
library/net-http                      124      354      14     340  30 (75)
library/matrix                         80      334     101     233
library/bigdecimal                     41      298     118     180  2 (8)
library/stringscanner                  33      215      61     154  3 (27)
TOTAL                                 970     3676    1335    2341  152 (620)
```

It **checks its arithmetic against each report's own footer** and refuses when
they disagree — a rollup is exactly where a dropped row is invisible, since the
table still looks tidy and the only symptom is a total nobody re-derives. The
invariant differs by report shape: a full report must match on all three
numbers, a `-quiet` one only on the **delta**, since a file it omits contributes
0 to it. It also keeps *behind* and *absent* in separate columns, and says so
when a `-quiet` input makes the two count columns cover only the moved files.

## Caveats

The shim is not full MSpec. What is genuinely stubbed raises `SpecSkip`, so
those examples count as **skipped** rather than passing, and the measured totals
are a conservative lower bound on rbgo's true conformance — which is exactly
what a ratchet should be:

| stubbed (raises `SpecSkip`) | where |
|---|---|
| `ruby_exe` — no subprocess | `spec_helper.rb:882` |
| `ruby_cmd` — no subprocess command line | `spec_helper.rb:883` |
| `output_to_fd` — no real fd redirection | `spec_helper.rb:280` |

⛔ **That table is the whole list, and it is now derived rather than retyped.**
It had a fourth row, "some `argf`/IO helpers", with no `where` — and nothing in
the shim raises `SpecSkip` for them; the empty cell was the tell. It also
omitted `ruby_cmd`, one line below the `ruby_exe` it names. A census that is
maintained by hand is a census that drifts in both directions at once, so
`TestReadmeStubbedHelpersMatchTheShim` (repository root) reads every
`SpecSkip` raise out of `spec_helper.rb` and fails unless this table and the
top-level README name exactly that set.

⛔ **`complain`, `output` and `be_computed_by` are NOT stubbed**, and this
paragraph said they were until 2026-11-08. They are implemented:
`ComplainMatcher` at `spec_helper.rb:204-249` with a real `$stderr` capture
(`complain` wired at `:249`), `OutputMatcher` at `:254-279`, and
`be_computed_by` at `:199`.

**The error ran in the direction that matters.** An example using a stubbed
matcher skips and makes the total too LOW; one using `complain` **runs**, and
when the behaviour is missing it **fails** and counts against the ratchet. A
reader trusting the old sentence would have looked for such examples among the
skips, where they are not. Found through
`core/enumerable/to_set_spec.rb`, which asserts a 4.0 deprecation warning rbgo
does not emit (issue #909) and reports

```text
Enumerable#to_set | instantiates an object of provided as the first argument set class
    expected a warning matching /Enumerable#to_set/, got ""
```

A skipped example cannot report that, which is how the claim was caught: the
output contradicted it rather than anyone re-reading the shim.

`cmd/phantom` is the other half of that honesty: the stubbed matchers make the
total too LOW, and a pass bought by an absence would make it too high. Neither
is visible in the number itself — and note that its rationale quotes the
sentence corrected above, so "too LOW" holds for `ruby_exe` and `output_to_fd`
and not for the three matchers that do run.
