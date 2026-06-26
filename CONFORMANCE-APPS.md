# Real-World Library Conformance Confrontation

**Date: 2026-06-25**

This report measures how far the pure-Go Ruby implementation (`rbgo`, this repo)
can load and run the **source and representative usage of well-known reference
Ruby libraries**, compared against MRI/CRuby as the oracle. It is a measured,
evidence-based gap map — **not** a claim that these frameworks run. `rbgo` is a
from-scratch Ruby 4.0 subset with no C extensions and a partial stdlib, so big
libraries are not expected to load end-to-end. The actionable output is the
**prioritized gap list** at the end: the missing language features, methods and
stdlib modules, ranked by how many reference libraries each one blocks.

## How to reproduce

```sh
GOWORK=off go build -o /tmp/rbgo ./cmd/rbgo
scripts/conformance/apps/run.sh            # uses cached clones; skips missing libs
scripts/conformance/apps/run.sh --clone    # clones the libraries (needs network)
scripts/conformance/apps/run.sh --only rake
```

Every number below comes from an actual run of `/tmp/rbgo` vs `ruby` (MRI 4.0.5,
arm64-darwin). For each library two confrontations run, each rbgo-vs-MRI:

- **LOAD** — `require` the library entrypoint. Does rbgo parse+load like MRI?
- **USAGE** — representative API snippets from the README/docs (in
  `scripts/conformance/apps/snippets/<lib>.txt`), comparing stdout.

`rbgo` parses and compiles an entire file at `require` time, so a parse error
anywhere in the entrypoint's transitive `require` graph blocks the whole load
before any of the library's code runs. Consequently a single front-end gap
cascades into a 0/N usage score — which is exactly why the parser gaps dominate
the priority list.

## Libraries confronted (ladder, easiest first)

| Library | Repo | Why chosen |
|---|---|---|
| mustache | mustache/mustache | small, pure-Ruby templating, few deps |
| minitest | seattlerb/minitest | pure-Ruby test framework, stdlib-only |
| rake | ruby/rake | pure-Ruby, but pulls several stdlib modules |
| Liquid | Shopify/liquid | pure-Ruby templating, leans on `strscan` |
| Rack | rack/rack | web server interface, `autoload`-heavy |
| i18n | ruby-i18n/i18n | needs the `concurrent-ruby` gem |
| ActiveSupport | rails/rails (`core_ext`) | stretch: per-file parse sweep of 117 files |

`concurrent-ruby` was `gem install`ed so MRI can load i18n; rbgo has no gem
mechanism, so i18n is blocked on rbgo independently of any parser issue.

## Results — load & usage

| Library | LOAD (rbgo) | First failure | Category | USAGE rbgo/MRI |
|---|---|---|---|---|
| mustache | FAIL | `mustache.rb:192` `... if not partialpath and raise_on_context_miss?` | parse-error (`and`/`not`) | 0/5 |
| minitest | FAIL | `minitest.rb:20` `(class << self; self; end)` | parse-error (singleton-class expr) | 0/4 |
| rake | FAIL | `rake/version.rb:6` `MAJOR, MINOR, BUILD, *OTHER = …` | parse-error (constant multiple-assignment) | 0/4 |
| Liquid | FAIL | `require "strscan"` | unsupported-stdlib-require | 0/5 |
| Rack | FAIL | `rack.rb` `autoload …` | missing-method (`autoload`) | 0/4 |
| i18n | FAIL | `i18n.rb:59` `current = Fiber[…] || self.config = …` | parse-error (assignment inside expression) + needs `concurrent-ruby` | 0/3 |
| ActiveSupport core_ext | 27/117 files parse+run standalone (~23%) | mixed | parse-error 45, missing-method 8, intra-AS/stdlib-load 37 | n/a (file sweep) |

**Aggregate (last full run):** `pass=27 fail=84`. Histogram of rbgo divergences
where MRI succeeds:

| Category | Count |
|---|---|
| parse-error | 49 |
| missing-method | 9 |
| missing-class/module | 0 |
| unsupported-stdlib-require | 1 |
| wrong-behavior | 0 |
| external-gem (MRI-blocked, not an rbgo fault) | 0* |

\* i18n's external-gem dependency is real but is reported as a parse-error
because rbgo hits the line-59 parse failure before it ever reaches the line-3
`require 'concurrent/map'`. Both blockers are listed below.

The single most important takeaway: **front-end (parser) gaps, not missing
runtime methods or stdlib, are what block these libraries today.** Five of the
six top-level libraries are blocked by a parser construct, and parse-error is
the dominant category in the ActiveSupport sweep too.

## Prioritized gap list

Ranked by how many of the confronted libraries each gap blocks. Every entry has
a minimal repro verified against MRI 4.0.5. None of these were fixed here (per
scope: no edits to `internal/vm/*`); they are recorded for follow-up.

### P0 — parser gaps (each blocks a top library and recurs across ActiveSupport)

1. **`and` / `or` / `not` low-precedence keyword operators** — blocks **mustache**, recurs in AS.
   - Repro: `x = true and false` → rbgo `parse error: unexpected "and"`; MRI `true`.
   - Repro: `p(1) if not a and b` → rbgo parse error; MRI runs.
   - `not` currently parses as a method call: `not true` → `undefined method 'not'`.
   - Front-end owner: go-ruby-parser (precedence-1 `and`/`or`, unary `not`).

2. **Multiple assignment to constant targets** — blocks **rake**.
   - Repro: `A, B = 1, 2` → rbgo `parse error: unexpected ","`; MRI `1`.
   - Lowercase masgn (`a, b = 1, 2`) works; only CONST LHS targets fail.
   - Full failing line: `MAJOR, MINOR, BUILD, *OTHER = Rake::VERSION.split "."`.

3. **Singleton-class expression `(class << obj; … end)` as a value** — blocks **minitest**.
   - Repro: `k = (class << Object.new; self; end); p k.class` → rbgo `parse error: unexpected "class"`; MRI `Class`.
   - The statement form is presumably handled; the expression/value form is not.

4. **Assignment embedded in an expression** — blocks **i18n** (and AS `array/grouping.rb`).
   - As RHS of `||`: `y = nil || self.config = 3` → rbgo `parse error: unexpected "="`; MRI `3`.
   - As an operand: `a << z = 5` → rbgo `parse error: unexpected "="`; MRI `[5]`.

5. **Leading `::` (top-level scope resolution)** — blocks **Rack** (`rack/utils.rb`), recurs in AS `date_and_time/*`.
   - Repro (parse): `X = ::Array` and `defined?(::URI::X) ? 1 : 2` → rbgo `parse error: unexpected "::"`; MRI works.
   - Repro (wrong-behavior): `p ::Array` parses but yields `TypeError: nil is not a class/module`; MRI `Array`.

6. **`alias` with symbol-literal operands** — recurs in AS (`array/access.rb` etc.).
   - Repro: `alias :bar :foo` → rbgo `parse error: unexpected "foo"`; MRI works.

### P1 — runtime: missing Module/Class metaprogramming methods (block AS broadly)

7. **`alias_method`** — entirely missing; blocks AS `hash/keys.rb`, `range.rb`, etc.
   - Repro: `class C; def f;1;end; alias_method :g, :f; end; C.new.g` → `undefined method 'alias_method'`.

8. **`alias` bare-word form does not actually create the alias** (wrong-behavior).
   - Repro: `def foo;1;end; alias bar foo; p bar` → parses, then `undefined method 'bar'`; MRI `1`.
   - At class scope it errors earlier: `alias bar foo` → `undefined method 'foo' for Class`.

9. **`private` (with or without arguments)** — entirely missing; blocks AS `name_error.rb`, `hash/deep_transform_values.rb`.
   - Repro: `class C; private; def x;1;end; end` → `undefined method 'private'`. (`private :x` form also missing.)

10. **`instance_method`** — missing; blocks AS `object/duplicable.rb`.
    - Repro: `C.instance_method(:x)` → `undefined method 'instance_method'`.

11. **`undef_method`** — missing; blocks AS `object/with.rb`.
    - Repro: `class C; def x;1;end; undef_method :x; end` → `undefined method 'undef_method'`.

12. **`autoload`** — missing; blocks **Rack** at the entrypoint.
    - Repro: `class C; autoload :X, "x"; end` → `undefined method 'autoload' for Class`.
    - A correct lazy `autoload` is hard; even a no-op-then-`require`-on-miss stub
      would let many `autoload`-organized libraries load.

### P2 — missing stdlib modules (`require` fails)

These pure-Ruby/stdlib feature names are unknown to rbgo's loader
(`internal/vm/require.go` `providedFeatures`), so `require` raises `LoadError`:

| Feature | Blocks | Notes |
|---|---|---|
| `strscan` | **Liquid** (hard blocker) | StringScanner; pure-Ruby lexers depend on it |
| `optparse` | minitest, rake | option parser |
| `fileutils` | rake | filesystem ops |
| `rbconfig` | rake | build config constants |
| `singleton` | rake | Singleton mixin |
| `monitor` | rake | Monitor / MonitorMixin |
| `forwardable` | (common) | def_delegator |
| `concurrent/map`, `concurrent/hash` | **i18n** | external gem `concurrent-ruby`, not stdlib |

Already provided by rbgo (no gap): `set`, `date`, `time`, `bigdecimal`,
`base64`, `digest`, `json`, `zlib`, `stringio`, `securerandom`.

## Suggested fix order (maximizes libraries unblocked per unit of work)

1. P0-1 `and`/`or`/`not` operators → unblocks mustache's load; pervasive idiom.
2. P0-2 constant multiple-assignment → unblocks rake's first file.
3. P1-7..9 `alias_method` / `private` / fix bare `alias` → unblocks the largest
   slice of ActiveSupport core_ext (the dominant missing-method cluster).
4. P0-5 leading `::` (parse + resolve) → unblocks Rack utils and AS date code.
5. P2 `strscan` (StringScanner) → unblocks Liquid (its only blocker).
6. P0-3 singleton-class expression → unblocks minitest.
7. P1-12 `autoload` stub → unblocks Rack's entrypoint.
8. P0-4 assignment-in-expression + P2 `optparse`/`fileutils`/etc. → chips away
   at rake and i18n (i18n additionally needs a gem path for concurrent-ruby).

## Scope notes

Per the task's coordination constraint, **no `internal/vm/*` or other core Go
source was edited** — concurrent work on the regexp bridge / synthetic
conformance pass lives there. This change adds only the harness
(`scripts/conformance/apps/`) and this report. Each gap above is recorded with a
minimal repro so it can be fixed without re-deriving it.
