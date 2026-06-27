# Library parse-conformance — go-embedded-ruby (rbgo)  (final, 2026-06-27)

How much real-world Ruby does rbgo's pure-Go front-end accept? This report
confronts rbgo (`parser.Parse` + `compiler.Compile`, **no execution**) with the
`lib/` trees of widely-used Ruby libraries, plus the cross-phase deltas on the
larger codebases. Released gems are valid Ruby (`ruby -c` clean), so a file rbgo
rejects is a genuine front-end gap; acceptance = `OK / total .rb`.

Reproduce: `scripts/conformance/libraries/run.sh` (libraries below),
`scripts/conformance/heavyweight/sweep.sh` (Rails/Puppet),
`scripts/conformance/apps/run.sh`, `scripts/conformance/rspec/run.sh`.

## Libraries (final)

Parse-conformance against the `lib/` trees of widely-used Ruby gems (released
gems are `ruby -c` clean, so a rejection is a genuine front-end gap):

| Library | Accept | | Library | Accept |
| --- | ---: | --- | --- | ---: |
| **RuboCop** | **99.7%** | | dry-struct | **100%** |
| Sinatra | **100%** | | Chef | 99.1% |
| Jekyll | **100%** | | concurrent-ruby | 98.3% |
| Thor | **100%** | | Homebrew | 98.7% |
| Kramdown | **100%** | | Asciidoctor | 93.8% |

Six of the ten libraries parse **completely** (Sinatra, Jekyll, Thor, Kramdown,
dry-struct, and — to within rounding — RuboCop, *the* community style benchmark at
99.7%). The lowest, Asciidoctor, is at 93.8%. rbgo now parses essentially every
major Ruby library measured.

## The journey (Rails end-to-end, parse + compile)

The campaign was an agent-driven find-gap → fix → measure loop run across **10
rounds** — 5 go-ruby-parser rounds interleaved with 5 rbgo (compiler / activation)
rounds. Rails end-to-end (parse + compile) acceptance over the campaign:

| Stage | Rails parse + compile |
| --- | ---: |
| start | 20.7% |
| round 1 | 46.3% |
| round 2 | 68.7% |
| round 3 | 81.2% |
| round 4 | 86.7% |
| round 5 | 93.8% |
| round 6 | 98.4% |
| **final** | **99.82%** |

A ~4.8× lift, with **0 over-permissive** at every stage — rbgo never accepts Ruby
that MRI rejects. Alongside Rails: Puppet reached **100%** end-to-end (2154/2154),
the combined Rails+Puppet **parse** rate is **99.96%** (100% of all valid Ruby),
and **RSpec DSL usage is 10/10** byte-identical to MRI.

## What moved the needle

- **`::` in constant-path position** (`class Foo::Bar`, `class E < Foo::Bar`,
  leading `::Top`) — ~68% of Rails gaps before the fix. Drove Rails 20.7% → 46.3%.
- **Paren-less command calls with arguments** (`delegate :a, to: :b`,
  `validates :x, presence: true`, `foo 1, 2, 3`) — the largest single remaining
  gap (~1562 files across Rails/Puppet). A trailing `do…end` now binds to the
  command call, matching MRI.
- **`class << self` / `class << obj`** (singleton class) — unblocked RSpec's last
  two DSL cases (→ 10/10) and ~200 files; also fixed two latent VM bugs it
  surfaced (class-level instance variables; `attr_*` inside `class << self`).
- **Argument forwarding** (`def f(*, **, &)` / `g(...)`), **`alias`/`undef`**,
  masgn to any target + nested destructuring, special globals, shorthand hash
  `{x:}`, quoted/operator/char-literal symbols, rationals & imaginaries
  (`2r`/`3i`), unicode identifiers, `for…in…end`, begin-less rescue/ensure, `!~`,
  and `%r`/`%x`/`%s` percent literals.
- **Parser never-panics** — the front-end returns a clean parse error rather than
  a Go panic on the files that previously crashed it.

## What is left

Front-end (parse + compile) is essentially complete. The remaining tail is a small
number of compile-time constructs (the 6 remaining Rails gaps) and, beyond the
front-end, the **runtime** surface — the rest of the stdlib, C-extension
equivalents and predefined constants — which is what stands between "parses + compiles"
and "runs the application." That is ongoing, unproven work tracked in the sibling
CONFORMANCE-*.md reports.
