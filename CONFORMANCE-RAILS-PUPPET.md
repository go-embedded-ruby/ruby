# Heavyweight parse-conformance: Rails & Puppet

Confronting **go-embedded-ruby**'s pure-Go front-end (`parser.Parse` →
`compiler.Compile`, no execution) with the two largest reference Ruby codebases —
**Ruby on Rails** and **Puppet** — as a conformance stress test.

Rails and Puppet cannot run end-to-end on a from-scratch Ruby subset (huge
dependency trees, C extensions), so the tractable, high-value metric is
**front-end (parser/compiler) acceptance**: for every `.rb` file, does rbgo's
front-end accept the same source that MRI considers valid? MRI `ruby -c` is the
oracle for "is this valid Ruby?".

All numbers below come from a real run of the harness in
`scripts/conformance/heavyweight/`; reproduce with:

```sh
scripts/conformance/heavyweight/sweep.sh           # clones repos, sweeps, summarizes
python3 scripts/conformance/heavyweight/categorize.py /tmp/ger-heavyweight-out rails
python3 scripts/conformance/heavyweight/categorize.py /tmp/ger-heavyweight-out puppet
```

## Environment

- rbgo built from this repo (`GOWORK=off go build ./cmd/rbgo`), go-ruby-parser
  **Round 5** (`v0.0.0-20260626192347-1bbe8b4672d0`) with the compiler's
  `ast.For` lowering activated.
- Oracle: MRI `ruby 4.0.5 (2026-05-20) +PRISM [arm64-darwin25]`.
- Repos: shallow clone of `rails/rails` and `puppetlabs/puppet` HEAD on 2026-06-25.

The front-end checker (`frontend/main.go`) runs `parser.Parse` then
`compiler.Compile` and recovers from panics, so a file that *parses + compiles*
but would fail at runtime (missing stdlib / missing method) still counts as a
front-end **success** — exactly what isolates parser/compiler gaps from runtime
gaps. It is `//go:build ignore` so it never enters the module's normal
`go build ./...` / coverage-gated test run.

## Results

| Repo   | `.rb` files | MRI-valid | rbgo front-end accepts | **acceptance rate** | rbgo gaps |
|--------|------------:|----------:|-----------------------:|--------------------:|----------:|
| Rails  |       3 423 |     3 423 |                  3 418 |          **99.85 %** |         5 |
| Puppet |       2 156 |     2 154 |                  2 150 |          **99.81 %** |         6 |
| **Total** |    5 579 |     5 577 |                  5 568 |          **99.84 %** |        11 |

(Round 5 parser + the compiler gaps it exposed now fixed: block-pass after
kwargs, anonymous `&`, block keyword params, `...`/`*`/`**` forwarding through
calls and `super`, `yield(*args)`, `rescue *classes`, and `case … when *array`.)

- `both-reject` (rbgo and MRI both reject): Rails 0, Puppet 2 — i.e. essentially
  every file rbgo rejects is *valid Ruby that MRI accepts*. These are genuine
  front-end gaps, not invalid input.
- `over-permissive` (rbgo accepts, MRI rejects): **0** in both repos — rbgo never
  accepted Ruby that MRI rejected.

The remaining 11 gaps are distinct features beyond this batch: rational-literal
compilation (`2r`), nested `MultiAssign` destructuring targets, named-regexp
capture locals (`/(?<x>…)/ =~ s` binding `x`), `begin…end until` post-loops with
assign-in-condition, and 2 parser-level gaps in Puppet.

The gap is dominated by a *small number of very common constructs*. The single
top construct (the `::` scope-resolution operator) blocks **2 723 files** — about
68 % of all gaps. Fixing the top ~6 categories below would, by file count, lift
acceptance dramatically.

## Top front-end gap categories (ranked by files blocked)

Ranked over both repos combined (R = Rails files, P = Puppet files). Each is
reduced to a **minimal repro** verified to parse on MRI (`Syntax OK`) and fail on
rbgo. These are recorded for a coordinated fix in `go-ruby-parser`; this PR does
**not** modify the parser/VM.

### 1. `::` scope-resolution in a constant *path* position — ~2 750 files (R≈1 980 / P≈770)

By far the largest gap. rbgo parses `::` only when the result is a value in
primary position (`x = Foo::Bar` parses, failing only at runtime). It does **not**
accept `::` where a *constant path* is expected:

```ruby
class E < Foo::Bar; end     # rbgo: unexpected token "::"      MRI: ok   (qualified superclass)
class Foo::Bar; end         # rbgo: unexpected token "::"      MRI: ok   (qualified class name)
module Foo::Bar; end        # rbgo: unexpected token "::"      MRI: ok   (qualified module name)
class E < ::Object; end      # rbgo: expected CONST, got "::"   MRI: ok   (leading ::, superclass)
x = ::Foo                    # rbgo: unexpected token "::"      MRI: ok   (leading ::, top-level)
::Kernel.puts 1              # rbgo: unexpected token "::"      MRI: ok   (leading ::, receiver)
```

Sub-cases and how they surface in the raw tallies:
- qualified superclass `< A::B` → `unexpected token "::"` (the bulk).
- leading `< ::Const` → `expected CONST, got "<<"` (the lexer reads `< ::` as the
  `<<` left-shift token) — 124 files, all this construct.
- leading `::Const` in expression/receiver → `expected CONST, got "::"` (26).

Rails uses `class X < Namespace::Base` and `::TopLevel` pervasively, which is why
this one construct gates most of the corpus.

### 2. Paren-less command-call keyword / hash arguments — ~680 files (R≈290 / P≈300)

rbgo accepts keyword args **with parentheses** (`f(to: 1)`, `merge!(a: 1)`) but
not in **paren-less command calls**, which Rails/Puppet use everywhere
(`delegate`, `validates`, `provide`, …):

```ruby
delegate :logger, to: :connection           # rbgo: unexpected token "to" (LABEL)   MRI: ok
f to: 1                                       # rbgo: unexpected "to" after statement  MRI: ok
f :x, parent: :dpkg                           # rbgo: unexpected token "parent" (LABEL) MRI: ok
provide :apt, :parent => :dpkg                # rbgo: (LABEL/SYMBOL)                   MRI: ok
```

Surfaces as `(LABEL)` (292), `unexpected … after statement` (388), `(:)` (32),
`expected IDENT, got "X" (SYMBOL)` (18).

### 3. Paren-less command call with multiple comma-separated args — part of the `after statement` bucket (388, R145/P243)

```ruby
args = "-t", "--server", master              # rbgo: unexpected "," after statement   MRI: ok
```

A bare call / multiple-RHS assignment whose arguments are not parenthesized and
the second token is not a label.

### 4. Argument forwarding `...` — 80 files (R80 / P0)

```ruby
def each_connection(...); end                 # rbgo: expected IDENT, got "..."        MRI: ok
def stream_for(broadcastables, ...); g(...); end
```

Surfaces as `expected IDENT, got "..."` (31) + `expected IDENT, got ")"` (49,
the trailing-`...` form `def f(a, ...)`).

### 5. `%r`, `%x`, `%s` percent literals — 48 files (R9 / P39)

rbgo lexes `%w %W %i %I %q %Q` and bare `%( %{ %|`, but **not** the regex /
command / symbol percent literals:

```ruby
LS_REGEX = %r[(.)(...)...]                     # rbgo: unexpected token "%"             MRI: ok   (%r regex)
gzip = %x{which gzip}                          # rbgo: unexpected token "%"             MRI: ok   (%x command)
x = %s[sym]                                    # rbgo: unexpected token "%"             MRI: ok   (%s symbol)
```

Verified matrix: `%w %W %i %I %q %Q %( %{ %|` → accepted; `%r %x %s` → rejected.

### 6. Methods named after keywords — 8 files (R5 / P3)

```ruby
def do(command, options); end                 # rbgo: expected method name after def   MRI: ok
def then(arg); end                            # rbgo: expected method name after def   MRI: ok
def in(container); end                        # rbgo: expected method name after def   MRI: ok
```

`def` followed by a keyword (`do`, `then`, `in`, …) — MRI allows keyword method
names; rbgo's `def` rule requires IDENT/CONST/operator.

### 7. Compiler (not parser) gaps — 23 files (R14 / P9)

These **parse** but fail in `compiler.Compile`:

- `cannot compile *ast.SplatArg` (11) — splat inside `super`:
  ```ruby
  class B < A; def m(*a, &b); super(*a, &b); end; end   # COMP: cannot compile *ast.SplatArg
  ```
- `cannot compile *ast.BlockPass` (12) — block-pass combined with keyword args in
  the same call:
  ```ruby
  render(self, partial: options, locals: locals, &block)   # COMP: cannot compile *ast.BlockPass
  ```
  (The pure parser also rejects the related paren form
  `f(1, x: 2, &b)` → `expected ), got ","` — kwargs + block-pass together.)

### 8. Front-end PANIC (robustness bug) — 4 files (R1 / P3)

The front-end **panicked** instead of returning a clean parse error on 4 files:

```
interface conversion: interface {} is *runtime.TypeAssertionError, not parser.parseError
```

Files: `rails/.../postgresql/quoting.rb`, `puppet/lib/puppet/pops.rb`,
`puppet/spec/unit/pops/evaluator/{arithmetic_ops,runtime3_converter}_spec.rb`.
The harness recovers, but the parser should turn any internal failure into a
`parseError` rather than a Go panic. `quoting.rb` contains a regex literal with
embedded `#{interpolation}` and `(?:::\w+)?`; this is the likely trigger and is
worth a dedicated minimization pass.

### Remaining tail

Smaller buckets (each < 20 files): `(rescue)` inline/begin-less rescue (18),
`(&)` block-pass in unsupported position (9), `(**)` double-splat (7),
`(ILLEGAL)` lexer (19 — non-ASCII / edge tokens), ternary edge cases (7), and a
long tail of single-file oddities. They are real but low-leverage compared with
categories 1–5.

## Constructs that already work (verified, not gaps)

To keep the fix list honest, these idiomatic constructs were confirmed to parse +
compile on rbgo:

- Heredocs incl. squiggly (`<<~SQL … SQL`), `<<-`.
- Block-pass of a symbol/expr (`map(&:to_s)`, `f(&m)`).
- Endless method defs (`def sq(x) = x*x`).
- Pattern matching `case … in [a, b]`.
- Numbered block params, safe-navigation `&.`, `**` double-splat of a method call
  (`f(**h.merge(b: 2))`).
- `A::B` constant *reference* in value position (`x = Foo::Bar`).
- Parenthesized keyword args (`f(to: 1)`, `merge!(a: 1)`).
- `%w %W %i %I %q %Q` and `%( %{ %|` literals.
- Operator/setter/`[]` method defs (`def +(o)`, `def name=(v)`, `def [](k)`).

## Load attempts (secondary)

Picked self-contained pure-Ruby leaf files (front-end already accepts them) and
tried `rbgo run -e "require '<file>'"`, recording the first failure:

| File | Result |
|------|--------|
| `rails activesupport/.../core_ext/array/wrap.rb`    | **LOAD OK** |
| `rails activesupport/.../core_ext/string/filters.rb`| **LOAD OK** |
| `puppet acceptance/.../agent_fqdn_utils.rb`         | **LOAD OK** |
| `rails activesupport/.../core_ext/object/blank.rb`  | missing-stdlib (`concurrent/map` gem) |
| `rails activesupport/.../inflector/methods.rb`      | missing-dependency (`active_support/inflections` sibling) |
| `puppet lib/puppet/util/character_encoding.rb`      | **parse gap** (`::`) |
| `puppet lib/puppet/coercion.rb`                     | **parse gap** (`::`) |
| `puppet acceptance/.../common_utils.rb`             | missing-method (`module_function`) |
| `puppet lib/puppet/concurrent/lock.rb`              | missing-constant (`RUBY_PLATFORM`) |

Takeaway: once the front-end accepts a leaf file, the next wall is usually a
missing-stdlib gem, a missing-method on a builtin, or a missing predefined
constant (`RUBY_PLATFORM`) — all *runtime* concerns, separable from the parser
work above. `module_function` and `RUBY_PLATFORM` are small, self-contained VM
additions worth picking up.

## Prioritized fix list for go-ruby-parser / compiler

1. **`::` constant-path parsing** in superclass, class/module name, and leading
   top-level positions (≈ 2 750 files — the single highest-leverage fix).
2. **Paren-less command-call keyword / hash / multi-arg lists** (≈ 1 000 files
   across categories 2–3).
3. **Argument forwarding `...`** in def + call (≈ 80 files).
4. **`%r` / `%x` / `%s` percent literals** (≈ 48 files).
5. **Keyword-named methods** `def do/then/in/…` (8 files).
6. **Compiler**: `SplatArg` in `super`, `BlockPass` + kwargs in one call (23).
7. **Robustness**: convert the 4 front-end panics into clean parse errors.
