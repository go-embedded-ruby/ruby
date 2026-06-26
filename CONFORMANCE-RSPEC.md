# RSpec conformance stress test for rbgo (go-embedded-ruby)

Confronting **rbgo** (pure-Go, CGO=0 Ruby; CLI `rbgo`) with **RSpec** — the
dominant Ruby testing framework. RSpec is mostly pure Ruby and extremely
metaprogramming-heavy (`describe`/`context`/`it` DSL, `instance_exec`,
`method_missing`, `define_method`, `let`, matchers), so it exercises exactly the
dynamic-dispatch / blocks / metaprogramming features rbgo claims.

This is **not** an attempt to run RSpec's suite green. It is a **measured gap
map**: how much of RSpec rbgo can parse/load, and how rbgo's DSL behaves on
small hand-written specs, versus MRI.

## How to reproduce

```sh
scripts/conformance/rspec/run.sh
```

The harness builds `rbgo` + a `parsesweep` helper, shallow-clones the four core
RSpec repos, runs a parse sweep over their `lib/` trees, and diffs 10 hand-
written RSpec-style DSL snippets against MRI. It is re-runnable and skips
gracefully when offline (no clones) or when MRI `ruby` is absent (rbgo-only).

- Oracle: MRI **ruby 4.0.5** (`ruby -c` for syntax acceptance, `ruby` for DSL
  output). `parsesweep` calls rbgo's real front-end (`parser.Parse` +
  `compiler.Compile`, no execution) — a file that "compiles" here is one `rbgo`
  can load.
- RSpec repos pinned at shallow HEAD of `rspec/{rspec-support,rspec-core,
  rspec-expectations,rspec-mocks}` as of 2026-06-25.

All numbers below are from real runs, not static reasoning.

---

## 1. Parse sweep — `lib/` trees (the framework code itself)

MRI baseline: **all 197** `lib/` files pass `ruby -c`. So every rbgo reject
below is a genuine rbgo gap (MRI-accepts / rbgo-rejects).

| Repo               | rbgo accepts |
|--------------------|--------------|
| rspec-support      | 15 / 33      |
| rspec-core         | 38 / 74      |
| rspec-expectations | 16 / 49      |
| rspec-mocks        | 10 / 41      |
| **Total (lib/)**   | **79 / 197 = 40.1 %** |

Including spec files (which use the DSL heavily) across all four repos:
**127 / 559 = 22.7 %** — the DSL surface (paren-less command call + block, see
gap G2) is the main drag on spec-file acceptance.

Breakdown of the 117 parse rejects + 1 compile reject, clustered by message:

| count | rbgo error cluster                                  | root cause (gap) |
|------:|-----------------------------------------------------|------------------|
| 33    | `unexpected token "::"`                              | G1 leading `::`  |
| 11    | `unexpected "," after statement`                    | G3 masgn ivar lhs|
| 11    | `expected CONST, got "<<"`                           | G4 `class << self`|
|  6    | `expected IDENT, got ")"`                            | G6 bare anon splat|
|  5    | `unexpected token "class"`                           | G5 `class << expr`|
|  5    | `unexpected "do" after statement`                   | G2 cmd-call + block|
|  ~30  | `unexpected "..." after statement` (long literals)  | G7 line-cont + interp|
|  1    | `compile error: cannot compile *ast.SplatArg`       | G8 `super(*args)`|

---

## 2. Load attempts — `require` of RSpec entrypoints

| entrypoint            | result |
|-----------------------|--------|
| `require "rspec/support"`      | **fails** — `rspec/support.rb:21` `unexpected token "class"` (gap G5) |
| `require "rspec/core"`         | fails (depends on rspec/support) |
| `require "rspec/expectations"` | fails (depends on rspec/support) |
| `require "rspec/mocks"`        | fails (depends on rspec/support) |

No RSpec entrypoint loads under rbgo. Two distinct blockers surface:

1. **`$LOAD_PATH` / `$:` is `nil`** in rbgo, and `require` does not consult a
   load path — it searches only the requiring file's directory and the process
   CWD (`internal/vm/require.go: requireCandidates`). RSpec's own bootstrap does
   `$LOAD_PATH.unshift lib` and then `require "rspec/support"`, which cannot
   work. (Workaround for this harness: `cd` into the `lib/` dir so CWD-relative
   resolution applies.)
2. With the path worked around, the **first hard failure** is a parse gap:
   `rspec/support.rb:21` uses `(class << self; self; end).__send__(...)` — the
   singleton-class-of-expression form (gap G5), which also hits `__send__`
   (gap G-RT2) once it parses.

---

## 3. DSL usage — the headline (rbgo metaprogramming vs MRI)

Ten hand-written snippets reproducing RSpec's real DSL patterns *without*
loading RSpec. Run through rbgo and MRI; stdout + exit code compared.

**Result: 5 PASS / 5 FAIL.**

| # | snippet | pattern exercised | result |
|---|---------|-------------------|--------|
| 01 | describe/it DSL | `define_method` + `instance_exec` + class-level block recording, `class << self` accessor | **FAIL** (parse G4) |
| 02 | `let` memoization | `define_method` caching in ivar, `instance_exec` | PASS |
| 03 | `method_missing` matcher | `be_<predicate>` dispatch via `method_missing` + `respond_to_missing?` | **FAIL** (semantic G-RT1) |
| 04 | Comparable / `===` | `Comparable` mixin, `<=>`, `Range#===` | PASS |
| 05 | matcher-builder DSL | `define_singleton_method` capturing a block param `{ |&blk| }` | **FAIL** (semantic G-RT3) |
| 06 | nested context inheritance | `Class.new(parent)` + `class_eval(&block)` + `superclass` | PASS |
| 07 | yield / block args | explicit block params, `block.call`, lambdas | PASS |
| 08 | stub / send | `define_singleton_method`, `__send__`, `public_send` | **FAIL** (missing core G-RT2) |
| 09 | shared examples registry | module `class << self` + `instance_exec(*args, &block)` | **FAIL** (parse G4) |
| 10 | `expect(x).to(matcher)` chain | fluent objects, raise/rescue | PASS |

**Verdict.** rbgo's *core* dynamism holds up well: `define_method`,
`instance_exec`, `class_eval`, `Class.new` subclassing, `superclass`,
`Comparable`, block/lambda passing, `send`/`public_send`, and the
`expect().to()` fluent spine all work (snippets 02, 04, 06, 07, 10 are
byte-identical to MRI). The failures are **not** failures of the metaprogramming
*model* — they are a small set of concrete, isolatable gaps (two parse-level,
three runtime-level) that happen to sit on RSpec's hot path. Fix the eight gaps
below and a large fraction of both the parse sweep and the DSL snippets flip
green.

---

## 4. Ranked gap map (minimal repros, MRI-expected vs rbgo-actual)

Ranked by impact (lib/ files containing the construct, out of 197). For the
coordinated `internal/vm` / go-ruby-parser fix pass — **not fixed here.**

### G1 — leading `::` (top-level constant reference) — 64 files
```ruby
if defined?(::Foo)
  x = ::Foo::Bar.new
end
```
- MRI: `Syntax OK`
- rbgo: `parse error at line 1: unexpected token "::" (::)`
- Layer: go-ruby-parser (lexer/parser). `::Const` in expression position.

### G2 — paren-less command call with arg **and** `do…end` block — spec hot path
```ruby
config.expect_with :rspec do |x|
  x
end
```
- MRI: `Syntax OK`
- rbgo: `parse error at line 1: unexpected "do" after statement`
- Layer: parser. `recv.meth arg do … end`. The paren form
  `recv.meth(arg) do … end` parses fine; only the command (paren-less) form with
  both an argument and a `do` block fails. This is the shape of `RSpec.configure
  do`, `config.expect_with :rspec do`, etc. — the single biggest blocker on
  actual `*_spec.rb` files (drives spec-file acceptance down to 22.7 %).

### G3 — multiple assignment with instance-variable targets — 12 files
```ruby
@read_io, @write_io = IO.pipe
```
- MRI: `Syntax OK`
- rbgo: `parse error at line 1: unexpected "," after statement`
- Layer: parser. Note: masgn to *local* vars works (`a, b = 1, 2`); masgn to
  *ivars* on the LHS does not.

### G4 — `class << self` (singleton class, statement position) — 20 files
```ruby
class Foo
  class << self
    def bar; 1; end
  end
end
```
- MRI: `Syntax OK`
- rbgo: `parse error at line 2: expected CONST, got "<<" (<<)`
- Layer: parser. Blocks DSL snippets 01 and 09.

### G5 — `class << expr` (singleton class of an expression) — RSpec bootstrap
```ruby
(class << self; self; end).__send__(:define_method, name) { }
```
- MRI: `Syntax OK`
- rbgo: `parse error: unexpected token "class"`
- Layer: parser. This is `rspec/support.rb:21` — the first hard load blocker.

### G6 — bare anonymous splat parameter `(a, *)` — 3 files
```ruby
def self.extract(file, line, *)
  [file, line]
end
```
- MRI: `Syntax OK`
- rbgo: `parse error: expected IDENT, got ")" ())`
- Layer: parser. Trailing nameless `*` (and likely nameless `**`/`&`).

### G7 — backslash line-continuation + interpolation in the continued string
```ruby
x = "a" \
    "b#{1}"
```
- MRI: `Syntax OK`
- rbgo: `parse error at line 2: unexpected "b" after statement`
- Layer: lexer. Adjacent string-literal concatenation across a `\` line
  continuation works when both fragments are plain, but breaks when the second
  fragment contains `#{…}`. Accounts for the ~30 long "after statement" rejects
  (e.g. `raise ArgumentError, "..." \` / `"...`#{verb}`..."`).

### G8 — `super(*args)` (explicit super with a splat argument) — compile stage
```ruby
class B < A
  def m(*a); super(*a); end
end
```
- MRI: `[1, 2]`
- rbgo: `compile error: cannot compile *ast.SplatArg`
- Layer: **compiler** (parses, fails to lower). The only compile-stage reject in
  the lib sweep (`verifying_message_expectation.rb`). Plain `f(*args)` lowers
  fine; only inside an explicit `super(...)` does the splat arg fail.

### Runtime / semantic gaps (surface only after parse succeeds)

### G-RT1 — `respond_to?` ignores `respond_to_missing?`
```ruby
class C
  def respond_to_missing?(n, p=false); n == :foo; end
  def method_missing(n,*a); n == :foo ? 1 : super; end
end
C.new.respond_to?(:foo)   # MRI: true   rbgo: false
```
- Layer: `internal/vm`. `respond_to?` does not fall back to
  `respond_to_missing?`. Breaks DSL snippet 03 (predicate matchers).

### G-RT2 — `__send__` not defined as a core method
```ruby
P.new.__send__(:g)   # MRI: ok   rbgo: NoMethodError: undefined method '__send__'
```
- Layer: `internal/vm`. `send` and `public_send` **both work**; only the
  `__send__` alias is missing. RSpec uses `__send__` pervasively. Trivial fix
  (alias). Breaks DSL snippet 08.

### G-RT3 — block param `{ |&blk| … }` captures `nil` in `define_singleton_method`
```ruby
o = Object.new
captured = nil
o.define_singleton_method(:take) { |&blk| captured = blk }
o.take { 99 }
captured.call   # MRI: 99   rbgo: NoMethodError: undefined method 'call' for NilClass
```
- Layer: `internal/vm`. A block passed to a singleton method whose body declares
  a `&blk` block parameter is not bound. Breaks DSL snippet 05 (the matcher
  builder DSL — `RSpec::Matchers.define`).

### G-LOAD — `$LOAD_PATH` is `nil`; `require` has no load-path search
- `$LOAD_PATH` / `$:` evaluate to `nil`; `require` searches only the requiring
  file's dir + CWD. RSpec (and most gems) bootstrap via
  `$LOAD_PATH.unshift "lib"` then `require "rspec/support"`, which cannot
  resolve. Layer: `internal/vm` (`require.go`). Needed for any real gem loading.

---

## 5. Bottom line

- **Parse acceptance**: 40.1 % of RSpec `lib/` (79/197), 22.7 % including specs.
  Every reject is a real rbgo gap (MRI accepts 100 % of `lib/`).
- **Load**: no RSpec entrypoint loads — blocked first by `$LOAD_PATH` being
  unusable, then by the `class << expr` parse gap at `rspec/support.rb:21`.
- **DSL dynamism (headline)**: the metaprogramming model is sound — 5/10
  hand-written RSpec-pattern snippets are byte-identical to MRI, including
  `define_method`/`instance_exec`/`let`, anonymous-subclass nesting,
  `Comparable`, blocks/lambdas, and the `expect().to()` fluent chain. The 5
  failures reduce to a short, isolatable gap list (G1–G8 parse/compile +
  G-RT1/2/3 runtime + G-LOAD). None is a deep architectural problem; G1
  (leading `::`, 64 files) and G2 (command call + block, spec hot path) give the
  most leverage, and G-RT2 (`__send__` alias) is a near-free win.
