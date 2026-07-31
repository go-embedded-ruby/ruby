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

## Update — 2026-07-31: gap G2 closed (command call + `do…end` block)

**G2 is closed** and executes with MRI-4.0.5 semantics. A paren-less command
call whose trailing argument is followed by a `do…end` block —
`describe "x" do … end`, `foo bar do … end`,
`config.expect_with :rspec do |x| … end` — now parses *and runs*. The block
binds to the **outermost** command call (not to the last argument), matching
Ruby's classic do-vs-brace precedence rule.

The parse gap closed in `go-ruby-parser` via commit `0fe341b` ("Attach do…end
block to a receiver command call", 2026-06-26); it is already in the parser
version rbgo pins (`v0.0.0-20260703103305`). The compiler + VM already lower and
execute the resulting AST, so no rbgo production change was required — this
update adds the **execution** regression guard and re-measures.

**Measured impact — pure G2** (single parser commit `0fe341b^` → `0fe341b`, run
over an *identical* current RSpec file set, `parser.Parse` only, so the delta is
attributable to G2 alone):

| file set | before (G2 open) | after (G2 closed) | `do`-rejects before → after |
|----------|------------------|-------------------|------------------------------|
| **spec files** (the G2 hot path) | 34 / 276 = **12.3 %** | 236 / 276 = **85.5 %** | **237 → 1** |
| lib files | 130 / 197 = 66.0 % | 132 / 197 = 67.0 % | 5 → 0 |
| all (lib+spec) | 164 / 473 = 34.7 % | 368 / 473 = **77.8 %** | 242 → 1 |

G2 alone lifts **spec-file** parse acceptance **12.3 % → 85.5 % (+73.2 pts)**,
eliminating 236 of the 237 `unexpected "do" after statement` rejects — by far
the single biggest parse-acceptance lever, exactly as this doc predicted. Before
the fix, `unexpected "do" after statement` was **97.9 %** of all spec-file
rejects (237 / 242).

With the full current parser + VM that rbgo ships (G2 plus every later fix),
re-measured today with `scripts/conformance/rspec/run.sh` + a spec-file sweep:

| metric | now (2026-07-31) |
|--------|-------------------|
| `lib/` parse acceptance | 196 / 197 = **99.5 %** |
| **spec-file** parse acceptance | 274 / 276 = **99.3 %** |
| DSL snippets vs MRI | **10 / 10 PASS** |

MRI-parity proofs (rbgo output byte-identical to `ruby` 4.0.5):

```ruby
def foo(x); yield x*2; end; foo 3 do |n| p n end          # => 6   (yield through a command-call block)
def m1(x); "m1:#{block_given?}"; end                       # do binds to the OUTER command → m1 sees the block
def m2; "m2:#{block_given?}"; end
puts(m1 m2 do end)                                         # => m1:true
puts(m1 m2 { })                                            # => m1:false  ({…} binds to the NEAREST call, m2)
def describe(n); "D:#{n}(#{yield})"; end                   # nested RSpec DSL shape
def it(n); "I:#{n}=#{yield}"; end
puts(describe "o" do; it "i" do 42 end; end)               # => D:o(I:i=42)
```

Regression guard: `internal/vm/command_call_block_test.go`
(`TestCommandCallBlock`) — 20 cases covering yield-through, do-vs-brace
precedence, nested describe/it, splat/keyword args, `&block` capture, closure
over caller locals, the `LocalJumpError` branch, and paren-form regression.

---

## Update — 2026-07-31: gaps G4 + G5 closed (singleton classes)

Both singleton-class parse gaps are **closed** and execute with MRI-4.0.5
semantics. `class << <expr>` (of which `class << self` is one case) is one
grammar production: the parser emits `ast.SingletonClassDef{Target, Body}`, the
compiler lowers it via `compileSingletonClass` → `OpOpenSingletonClass`, and the
VM runs the body with the target's singleton (meta) class as the definee.

Re-measured with `scripts/conformance/rspec/run.sh` (parser dep
`go-ruby-parser 2026-07-03`, which post-dates the 2026-06-25 snapshot the body of
this doc was measured against — several other gaps have also closed since):

| metric | this doc (2026-06-25) | now (2026-07-31) |
|--------|-----------------------|-------------------|
| **G4** `class << self` | open (11 `expected CONST, got "<<"`) | **CLOSED** |
| **G5** `class << expr` | open (5 `unexpected token "class"`) | **CLOSED** |
| `lib/` parse acceptance | 79 / 197 = 40.1 % | **196 / 197 = 99.5 %** |
| DSL snippets vs MRI | 5 / 10 PASS | **10 / 10 PASS** |
| `rspec/support.rb` (line 21 `(class << self; self; end).__send__`) | parse FAIL (G5) | **parses in full** (load then hits a runtime gap, not the parser) |

Per-repo `lib/` acceptance now: rspec-support **33/33**, rspec-core **73/74**,
rspec-expectations **49/49**, rspec-mocks **41/41**. The single remaining reject
is unrelated to G4/G5 — `rspec-core/lib/rspec/core/formatters.rb:241` uses the
`retry` keyword (a distinct, newly-surfaced parse gap).

**`require "rspec/support"`**: the file now parses in full — the line-21
`class << expr` blocker is gone. With the `$LOAD_PATH` cwd workaround (gap
G-LOAD, still open) the require advances *past* parsing and reaches a *runtime*
gap instead — `undefined method 'method' for class 'Kernel'` (`Kernel#method`
reflection is not yet defined). So the parse gap G5 no longer blocks the load;
the first hard failure has moved from the parser to that runtime method.

MRI-parity proofs (rbgo output byte-identical to `ruby` 4.0.5):

```ruby
class Foo; class << self; def bar; 42; end; end; end; Foo.bar   # => 42  (class method via class << self)
o = Object.new; o.singleton_class.equal?(class << o; self; end) # => true (singleton_class identity)
module M; class << self; def reg; "R"; end; end; end; M.reg     # => "R" (module class-level accessor)
(class << Object.new; 40; 2; end)                               # => 2   (body value = last expression)
```

Regression guard: `internal/vm/singleton_test.go`
(`TestSingletonClass`, `TestSingletonClassOfExpression`,
`TestSingletonClassErrors`) and `go-ruby-parser/singleton_class_test.go`.

The rest of this document is the original 2026-06-25 gap map, retained for
history; the table below has been annotated where G4/G5 are referenced.

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
| 11    | `expected CONST, got "<<"`                           | G4 `class << self` — **CLOSED**|
|  6    | `expected IDENT, got ")"`                            | G6 bare anon splat|
|  5    | `unexpected token "class"`                           | G5 `class << expr` — **CLOSED**|
|  5    | `unexpected "do" after statement`                   | G2 cmd-call + block|
|  ~30  | `unexpected "..." after statement` (long literals)  | G7 line-cont + interp|
|  1    | `compile error: cannot compile *ast.SplatArg`       | G8 `super(*args)`|

---

## 2. Load attempts — `require` of RSpec entrypoints

| entrypoint            | result |
|-----------------------|--------|
| `require "rspec/support"`      | parse gap **G5 closed** — `rspec/support.rb` parses in full; load now advances to a *runtime* gap (`Kernel#method` undefined), not the parser (see 2026-07-31 update) |
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

**Result (2026-06-25 snapshot): 5 PASS / 5 FAIL.** As of 2026-07-31 the harness
measures **10 PASS / 0 FAIL** — G4 flipped 01 and 09; the three runtime gaps
(G-RT1/2/3) have since closed too. Rows below keep the original per-snippet
diagnosis.

| # | snippet | pattern exercised | result |
|---|---------|-------------------|--------|
| 01 | describe/it DSL | `define_method` + `instance_exec` + class-level block recording, `class << self` accessor | **PASS** (G4 closed) |
| 02 | `let` memoization | `define_method` caching in ivar, `instance_exec` | PASS |
| 03 | `method_missing` matcher | `be_<predicate>` dispatch via `method_missing` + `respond_to_missing?` | **FAIL** (semantic G-RT1) |
| 04 | Comparable / `===` | `Comparable` mixin, `<=>`, `Range#===` | PASS |
| 05 | matcher-builder DSL | `define_singleton_method` capturing a block param `{ |&blk| }` | **FAIL** (semantic G-RT3) |
| 06 | nested context inheritance | `Class.new(parent)` + `class_eval(&block)` + `superclass` | PASS |
| 07 | yield / block args | explicit block params, `block.call`, lambdas | PASS |
| 08 | stub / send | `define_singleton_method`, `__send__`, `public_send` | **FAIL** (missing core G-RT2) |
| 09 | shared examples registry | module `class << self` + `instance_exec(*args, &block)` | **PASS** (G4 closed) |
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

### G4 — `class << self` (singleton class, statement position) — 20 files — **CLOSED 2026-07-31**
```ruby
class Foo
  class << self
    def bar; 1; end
  end
end
```
- MRI: `Syntax OK`
- rbgo: **CLOSED** — parses + executes; `Foo.bar` returns `1`. DSL snippets 01
  and 09 now PASS. `ast.SingletonClassDef` → `compileSingletonClass` →
  `OpOpenSingletonClass`.
- Layer: parser + compiler + vm.

### G5 — `class << expr` (singleton class of an expression) — RSpec bootstrap — **CLOSED 2026-07-31**

```ruby
(class << self; self; end).__send__(:define_method, name) { }
```

- MRI: `Syntax OK`
- rbgo: **CLOSED** — parses + executes. The body is a real scope whose value is
  its last expression, so `(class << obj; self; end)` returns `obj`'s singleton
  class (`obj.singleton_class.equal?(...)` is `true`). `rspec/support.rb:21` now
  parses in full; the load advances past the parser to a runtime gap
  (`Kernel#method`, see §2) rather than failing here.
- Layer: parser + compiler + vm (same production as G4).

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
