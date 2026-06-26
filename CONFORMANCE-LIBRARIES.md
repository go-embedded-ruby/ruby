# Library parse-conformance — go-embedded-ruby (rbgo)  (2026-06-26)

How much real-world Ruby does rbgo's pure-Go front-end accept? This report
confronts rbgo (`parser.Parse` + `compiler.Compile`, **no execution**) with the
`lib/` trees of widely-used Ruby libraries, plus the cross-phase deltas on the
larger codebases. Released gems are valid Ruby (`ruby -c` clean), so a file rbgo
rejects is a genuine front-end gap; acceptance = `OK / total .rb`.

Reproduce: `scripts/conformance/libraries/run.sh` (libraries below),
`scripts/conformance/heavyweight/sweep.sh` (Rails/Puppet),
`scripts/conformance/apps/run.sh`, `scripts/conformance/rspec/run.sh`.

## Libraries (post-Phase-C `main`)

| Library | Accept | | Library | Accept |
| --- | ---: | --- | --- | ---: |
| **RuboCop** | **87.7%** (806/919) | | Chef | 77.8% (621/798) |
| concurrent-ruby | 84.7% (100/118) | | Jekyll | 73.0% (65/89) |
| Thor | 69.4% (25/36) | | dry-struct | 66.7% (10/15) |
| Kramdown | 65.5% (36/55) | | Homebrew | 42.8% (889/2077) |
| Asciidoctor | 35.4% (17/48) | | Sinatra | 28.6% (2/7) |

RuboCop — a large, deliberately idiomatic Ruby codebase and *the* community style
benchmark — is at **87.7%**. rbgo now parses the majority of every major library
measured.

## The journey (front-end acceptance across phases)

The agent-driven find-gap → fix → measure loop, in order: the `::` constant-path
fix (Phase B), then paren-less command-call arguments + `class << self` (Phase C).

| Target | Start | Post-`::` | **Post-Phase-C** | Total Δ |
| --- | ---: | ---: | ---: | ---: |
| Rails (3423 files) | 20.7% | 46.3% | **68.7%** | **+48.0 pts (3.3×)** |
| Puppet (2154 files) | 41.2% | 70.8% | **78.1%** | +36.9 pts |
| MRI stdlib (728 files) | ~49% | 52.3% | **57.8%** | +8.8 pts |
| RSpec DSL usage | 5/10 | 8/10 | **10/10** | +5 |

0 over-permissive across all corpora — rbgo never accepts Ruby that MRI rejects.

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
- **Parser never-panics** — a negative Bignum-literal crash accounted for every
  panic observed on the Rails corpus.

## Top remaining front-end gaps (next tier)

Ranked across all libraries above (normalized):

```
892  unexpected <tok> after statement   (further command-call / chaining edges)
294  unexpected token )                 (call/grouping edges)
 56  unexpected token ,
 55  unexpected token *
 49  unexpected token rescue            (rescue in expression position)
 38  ILLEGAL token                      (lexer: tokens it mis-handles)
 36  unexpected token NEWLINE
 31  unexpected token =                 (scoped-constant assignment Foo::BAR=)
  9  malformed string interpolation
```

These define the next round of go-ruby-parser work. The remaining VM/stdlib gaps
(missing methods, unimplemented stdlib modules) are tracked in the sibling
CONFORMANCE-*.md reports.
