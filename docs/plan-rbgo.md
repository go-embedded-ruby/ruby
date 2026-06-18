# Implementation plan — Ruby in pure Go (`go-embedded-ruby`, CLI `rbgo`)

> Goal: an implementation of Ruby written in **pure Go (no cgo)** that produces a
> **single static binary** embedding compiled code while preserving Ruby's
> **full dynamism** (dynamic dispatch, monkey-patching, `eval`, runtime
> `require`, metaprogramming).

This document is the reference roadmap. It is amended at the end of every phase
(exit criteria met, decisions settled, risks updated). A standalone critique of
the original draft is folded into the relevant sections and summarised in
**§15 Risk register** and **§16 Decision journal**.

## 1. Vision & constraints

- **No cgo.** Everything is pure Go → trivial cross-compilation (`GOOS`/`GOARCH`),
  static binary by default.
- **Bytecode VM** (mruby model), not transpilation to Go. The front-end (lexer +
  parser + compiler) is **embedded** in the binary so dynamic `eval`/`require`
  work.
- **Dynamism = a live object model.** Dispatch goes through mutable method tables
  (our `objc_msgSend`).
- **Reuse the GC.** Ruby objects are Go heap objects; Go's GC collects them. We
  do not write a GC.
- **Build-time selection.** Embed only the subset of the stdlib that is needed
  (require-graph scan + config + build tags), not the whole distribution.

### Target & scope
- **Reference semantics: Ruby 4.0** (pattern matching, endless methods, `it`,
  numbered params, beginless/endless ranges, safe navigation).
- **In scope:** the language core, embedded pure-Ruby stdlib, extension leaves
  reimplemented in Go.
- **Out of scope (assumed):** third-party C extensions (gems with `.so`), 100%
  MRI compatibility. We target a well-defined subset that **grows** (mruby
  philosophy), validated by conformance tests.

## 2. Overall architecture

```
source.rb
   │  (lexer: stateful)
   ▼ tokens
   │  (parser: recursive descent + Pratt, locals table)
   ▼ AST
   │  (compiler: local resolution, catch tables)
   ▼ bytecode (ISeq)
   ▼
┌──────────────────────────────────────────────┐
│ VM (interpretation loop)                      │
│  • object model (RClass/RModule/RObject)      │
│  • mutable method tables → dispatch           │
│  • frames, env (closures), catch tables       │
│  • Fiber ↔ goroutine                          │
│  • Go core + embedded pure-Ruby stdlib        │
└──────────────────────────────────────────────┘
```

Two execution modes:
- `rbgo run app.rb` — compile in memory and interpret (development).
- `rbgo build app.rb -o app` — produce the single static binary (see §12).

## 3. Repository layout

```
ruby/
  cmd/rbgo/            CLI: run | build | compile | repl
  internal/
    token/             token kinds (rich: tUMINUS vs tMINUS, tLABEL, …)
    lexer/             stateful lexer: lexState, spaceSeen, heredoc queue, literal stack
    ast/               AST nodes
    parser/            recursive descent + Pratt; scope stack (locals)
    compiler/          AST → ISeq; local resolution; catch tables
    bytecode/          ISA, ISeq, (de)serialization
    vm/                interpretation loop, frames, exceptions, super
    object/            object model: Value, RClass, RModule, RObject, Method
      core/            Go core types: integer, float, string, array, hash, symbol, proc, range
    builtin/           Kernel/Object/Module/BasicObject + metaprogramming floor
    fiber/             Fiber ↔ goroutine bridge
    encoding/          String encodings (UTF-8, ASCII-8BIT, transcoding)
    regexp/            adapter onto the standalone go-onigmo/regexp module
    loader/            require resolution over embedded FS + bytecode cache
    stdlib/
      ruby/            upstream *.rb embedded (comparable.rb, enumerable.rb, set.rb, …) via //go:embed
      native/          Go leaves: json, digest, zlib, securerandom, base64, …
  oracle/              test harness: fixtures generated from MRI/Prism (dev only)
  testdata/
  spec/                imported subset of ruby/spec
```

## 4. Value representation

Starting decision: `Value` is an **interface** with concrete types. Go's GC
manages everything.

```go
type Value interface {
    RClass(*VM) *RClass   // dynamic class → used by dispatch
}
```

**Critique — this is the project's central tension, not a Phase 8 footnote.** An
`Integer` behind an interface is a heap allocation plus an interface method call
and a map lookup on every dispatch. The two ways MRI avoids this — **tagged /
NaN-boxed immediates** (fixnums, flonums, `nil`/`true`/`false`) — *fight Go's
GC*, which must statically know which words are pointers. So you largely cannot
do MRI-style immediates in safe Go. Consequences to accept up front:

- Position the project as **deployment/embedding convenience, not raw speed**
  (numeric loops will be several× MRI).
- Even so, **split `Fixnum` and `Bignum`** so the common integer does not carry a
  nil `*big.Int` word, and add a small-int cache (−128..255, like CRuby) early.

Principle: **immutable → value types**; **mutable → pointers**. `object_id` is
stable via a **weak-keyed** table (pointer → id); `equal?` is pointer identity
for reference types. Note `ObjectSpace._id2ref` / `each_object` are effectively
**incompatible** with reusing Go's GC (they need a registry that pins
everything) — stub them, do not promise them.

Phase 0 ships a minimal `Value` (Integer `int64`, Float, String, Bool, Nil,
Main) without `RClass`; the object model lands in Phase 1.

## 5. Object model (the heart of dynamism)

```go
type RClass struct {
    name      string
    super     *RClass
    methods   map[Symbol]*Method   // MUTABLE → monkey-patching, define_method
    consts    map[Symbol]Value
    ancestors []*RClass            // cached linearization (include/prepend)
    isSingleton bool
}
type Method struct {
    name    Symbol
    iseq    *ISeq      // Ruby method…
    native  NativeFn   // …or implemented in Go
    owner   *RClass
    visibility Visibility
}
```

Dispatch (our `objc_msgSend`) walks the ancestor chain, falling back to
`method_missing`. From this fall out **for free**: monkey-patching (mutate
`methods`), `define_method` (insert), `method_missing` (fallback), `send`
(computed selector), reflection (read the tables), singleton classes
(`RClass{isSingleton:true}` spliced into the chain).

## 6. Bytecode & VM

**Stack ISA (YARV-style).** Literal pushes, locals (`getlocal`/`setlocal` with
depth+index), ivars/consts/cvars, calls (`send{mid, argc, flags, blockiseq}`,
`invokesuper`, `invokeblock`), compound literals, control (`jump`, `branchif`,
`branchunless`, `branchnil`), definition (`definemethod`, `defineclass`), stack
ops, and non-local exit (`leave`, `throw{type}`).

```go
type ISeq struct {
    Insns  []Instr
    Consts []Value
    Locals []Symbol
    Catch  []CatchEntry  // rescue / ensure / break / next / redo / retry
    Arity  Arity
    Name   string
    Source SourceMap     // for backtraces
}
```

The **catch table** is the single mechanism for `rescue`/`ensure`/`retry` **and**
non-local `break`/`next`/`redo`/`return` from blocks — exactly like YARV.

**Critique — native-frame unwinding.** A clean "status return, never panic" for
control flow (see §8) has a hole: when a Go-implemented method (e.g. `Array#each`)
calls back a Ruby block that does `break` or `raise`, the **native Go frame must
unwind** — you cannot return a status *through* a Go `for` loop. Plan for a
**hybrid**: status returns for VM-internal flow, but `panic`/`recover` at the
native↔Ruby callback boundary. Design that boundary in Phase 1.

Phase 0 recurses on the Go stack (so `fib(20)` just works); explicit `Frame`/`Env`
objects, catch tables, and Fiber arrive in Phases 1–3.

## 7. Closures, blocks, Fiber

- **Blocks/Procs** compile to child ISeqs capturing `env` + `self`; `yield` is
  `invokeblock`. `lambda` vs `proc` is a flag (`return`/arity semantics).
- **Fiber ↔ goroutine**: each `Fiber.new` runs a goroutine on a VM loop,
  synchronised by two channels (resume/yield) in strict cooperative handoff.
- **Enumerator#next** (external iteration) is built on Fiber — load-bearing for
  `loop`, `lazy`, and `each` without a block.

**Critique — Fiber cost (raise to Medium-High).** Idiomatic Ruby spawns
enumerators constantly. Each is a goroutine (≥8 KB stack) with ~hundreds-of-ns
channel handoff per `#next`, exactly where Ruby leans hardest; and an unfinished
enumerator is a **leaked goroutine** blocked on its channel forever — needs
finalizer-based teardown.

## 8. Exceptions & control flow

- Exception hierarchy partly definable in Ruby; `raise`/`rescue`/`ensure`/
  `retry`/backtrace/`cause` are primitive.
- **Unwinding mechanism**: return a *status* (`ThrowState{type,value,target}`)
  from the dispatch loop rather than Go `panic`/`recover` for normal flow; reserve
  `panic` for **internal** VM bugs — modulo the native-frame boundary in §6.
- `break`/`next`/`redo`/`return` from a block = `throw` targeting the right frame
  via the catch table. `StopIteration` is load-bearing for `Enumerator`/`loop`.

Phase 0 has no `rescue` yet, so runtime errors are fatal and travel as a
`panic(RubyError)` recovered at the `Run` boundary; this converges to the
status-return design when exceptions land in Phase 3.

## 9. GC & memory semantics

- Reuse Go's GC (Ruby objects = Go heap objects). **No GC to write.**
- `object_id`: a **weak-keyed** pointer→id table (Go ≥1.24 `weak` package +
  finalizers) so it does not pin objects.
- Finalizers: `runtime.SetFinalizer` with care; `ObjectSpace` largely stubbed
  (`_id2ref`/`each_object` permanently out — see §4).
- `WeakRef`: via Go's `weak` package.

## 10. Front-end (the biggest piece)

> Prism itself is a hand-written recursive-descent parser. We reimplement the
> same family. The "no cgo" constraint applies to the *shipped binary*;
> **MRI/Ripper/Prism are an offline test oracle** (§13).

**Lexer** — where the difficulty lives: `lexState` (mirrors MRI `EXPR_*`),
`spaceSeen` (`foo -1` ≠ `foo - 1`), a literal stack for re-entrant `"#{…}"`
interpolation, and a heredoc queue (`<<~`/`<<-`/`<<"…"`, several per line).

**Parser** — recursive descent + **Pratt** for expressions; a **scope stack**
resolves variable-vs-method-call: each `ident = …` registers a local, each bare
identifier consults `scope.isLocal(name)`.

**Incremental grammar growth**: subset first, then interpolation → heredocs →
`%`-literals → multiple assignment/splat → keyword args → pattern matching
(`case/in`) → endless methods → beginless/endless ranges → safe navigation →
numbered params/`it`.

**Critique — this is risk #1, ahead of regexp, and there is an escape hatch.**
The grammar is the largest cumulative effort and is currently sequenced as a
single late phase (Phase 5); that is the schedule trap. Consider **Prism compiled
to WASM, run under a pure-Go WASM runtime ([wazero](https://github.com/tetratelabs/wazero))** —
a production-grade, upstream-tracking parser that stays inside the no-cgo
constraint, deleting most front-end risk (cost: a ~MB WASM blob; loses the
hand-written-parser learning goal). Tracked as an open decision in §16.

## 11. Go core vs embedded Ruby (the minimal floor)

**In Go (incompressible):** object model, dispatch, exceptions/unwinding, Fiber,
`eval`/`require`, the metaprogramming floor (`send`, `respond_to?`,
`define_method`, `instance_variable_*`, `const_*`, hooks), and a **small kernel
per type**: Integer/Float (arith + `math/big` promotion), String (`[]byte`+enc,
`pack`/`unpack`), Array (`each`+indexing), **ordered Hash keyed by `hash`/`eql?`**
(not a Go `map`), Symbol (interning + `to_proc`), Proc/Method (`call`), Range
(`each` via `succ`).

**In embedded Ruby (big win):** **Comparable** (from `<=>`) and **Enumerable**
(from `each`) — written once, inherited by every conforming type. Then upstream
pure-Ruby stdlib: `set`, `ostruct`, `forwardable`, `optparse`, `logger`,
`delegate`, `singleton`…

**Go leaves** (C extensions in MRI): `json` (`encoding/json`), `digest`
(`crypto/*`), `zlib` (`compress/zlib`), `securerandom`/`base64`, sockets/IO
(`net`/`os`), `time`, `math/big`.

Note (Symbol): a monotonically-growing global intern table is a memory-exhaustion
vector under `"x#{i}".to_sym`; Ruby 2.2+ GCs dynamic symbols — track as a known
limitation.

## 12. Build chain & single binary

`rbgo build app.rb -o app`:
1. **Scan the `require` graph** (literal strings) + explicit include config for
   dynamic requires.
2. **Stdlib selection**: only reached libs are kept (`.rb` embedded + Go leaves
   via **build tags**, so the Go linker drops the rest).
3. **Compile** app + selected stdlib to bytecode.
4. **Emit a Go file** embedding the bytecode (`//go:embed`) + registration of the
   chosen libs.
5. `go build` → **one static binary**.

The binary contains: VM + core + selected stdlib + app bytecode + the
**front-end** (for `eval`). **Closed-world mode** (opt-in, no
`eval`/dynamic-require/unknown `const_get`) drops the front-end and enables
aggressive DCE → smaller binary. Precedent: mruby's **mrbgems**, selected at
build time via `build_config.rb`.

## 13. Test & oracle strategy

- **Unit tests** per package.
- **Golden AST**: expected ASTs generated offline from MRI (Ripper/Prism), frozen
  as fixtures.
- **Behavioural conformance**: an `oracle/` harness runs snippets in MRI **and**
  `rbgo` and compares stdout/result. *(Phase 0 already does this against MRI.)*
- **Upstream suites**: embed a stdlib lib and run **its own test suite** → each
  red test points at a missing primitive.
- **ruby/spec**: import subsets progressively (the conformance gold standard).
- **Add differential fuzzing** of the parser against Prism (random valid-ish
  Ruby → compare ASTs); fixed goldens miss lexer-state bugs.
- **Performance is tested too, systematically** — see §13a.

## 13a. Performance, go-asmgen acceleration & multi-architecture validation

Performance is a first-class, continuous concern, not a Phase 8 afterthought.

- **Systematic benchmarks against the reference Ruby** (MRI/CRuby). The same
  corpus that validates behaviour in the `oracle/` harness is reused as a perf
  corpus: each snippet is timed in MRI and in `rbgo`, and the ratio is tracked
  over time. Microbenchmarks (`go test -bench`) cover hot primitives (dispatch,
  integer/float arithmetic, String/Array ops, Hash probing); macrobenchmarks
  cover representative programs. Notable regressions are gated in CI.
- **Use [go-asmgen](https://github.com/go-asmgen/asmgen) to accelerate the hot
  computations.** go-asmgen generates Plan 9 SIMD assembly in pure Go with
  `CGO_ENABLED=0`, so it stays inside the no-cgo constraint. Candidate fast
  paths: String/byte scanning, comparison and `pack`/`unpack`, UTF-8 validation
  and transcoding, the ordered Hash's hashing, bignum kernels, and bulk Array
  operations. Every go-asmgen-backed path sits behind the **same byte-identical
  tests** as its scalar fallback (prototype, then *measure* before/after).
- **Validate on all six supported 64-bit architectures**: **amd64, arm64,
  riscv64, loong64, ppc64le, s390x** — natively on amd64/arm64 and under qemu for
  the rest (note: amd64-AVX2 needs a full x86_64 VM, not Rosetta or
  docker-qemu-user). Both correctness and the perf matrix run across all six, the
  same target set go-asmgen itself covers.

## 14. Phasing

Principle: **vertical slice first** (de-risk the whole chain thin), then deepen.

### Phase 0 — Skeleton & vertical slice — ✅ DONE
Lexer+parser+compiler+VM for a tiny subset: integers, arithmetic, locals,
`if`/`while`, `def`/call, `puts`, basic String.
**Exit (met):** `puts 1 + 2` and a recursive `fib(n)` run end to end; the whole
chain exists (thin). Also shipped: floats, `unless`/`until`, statement modifiers,
floor division, `print`/`p`, ~92% coverage (object 100%), MRI differential tests.

### Phase 1 — Object model & dispatch — ✅ DONE (core)
Live object model: `RClass`/`RObject`/`Method` with **mutable per-class method
tables**, dynamic dispatch (`send`) walking the ancestor chain, **classes with
inheritance** (`class … < …`), `@ivars`, `new`/`initialize`, constants, and
**user-overridable `method_missing`** (default raises `NoMethodError`). Calls are
now receiver-aware `OpSend`; top-level `def` defines on `Object`. Base hierarchy:
BasicObject→Object→{Module→Class}, plus Integer/Float/String/True/False/Nil.
Kernel on Object: `puts`/`print`/`p`/`class`/`to_s`/`inspect`/`nil?`. 100%
coverage, CI green on 6 arches.
**Done since:** modules (`module … end`), `include` (mixins, via the ancestor
walk over each class's included modules), `super` (bare/forwarding, `super()`,
`super(args)`, `super arg`), and **blocks & `yield`** — real closures via an
`Env` parent chain with depth-addressed locals, `{ |params| … }` blocks, `yield`
(with args), `block_given?`, lenient block arity, and `Integer#times` as the
first block-driven iterator.
**Still to come in Phase 1:** `Proc`/`lambda` and `&block` (reifying a block as a
first-class value), singleton classes, `respond_to?`, and routing the
arithmetic fast paths through `send`. (`do…end` block syntax landed in Phase 2;
`method_missing` now receives a Symbol as of Phase 2.)
**Exit (met for core):** define classes, subclass, instantiate, call methods,
use ivars, override `method_missing`.

### Phase 2 — Go core types + Ruby mixins — 🚧 in progress
Integer (bignum), Float, **String (bytes+encoding)**, Symbol, Array, **ordered
Hash**, Range, `pack`/`unpack`. **Comparable** & **Enumerable** in Ruby.
**Started:** **Symbol** literals (`:name`, `:name?`/`:name!`), the `Symbol` class
(`to_s`/`to_sym`/`==`), and `method_missing` now receives a Symbol (matching MRI,
closing the Phase 1 String compromise). **Array** — literals `[…]`, indexing
`a[i]`/`a[i]=v` (negative indices), `length`/`size`/`push`/`first`/`last`/
`empty?`/`include?`/`each`/`map`, element-wise `==`, and `puts` array-flattening.
**ordered Hash** — literals `{k => v}` (insertion-ordered), `h[k]`/`h[k]=v`,
`size`/`length`/`empty?`/`key?`/`keys`/`values`/`each`/`select`/`reject`, `==`,
the **Ruby 4.0 `Hash#inspect`** form (`{a: 1, "b" => 2}` — label form for symbol
keys, spaced `=>` otherwise), plus `merge`/`fetch`/`dig`/`values_at`/
`transform_values`/`transform_keys`/`invert`/`to_h`/`store`/`delete`/`has_value?`
/`each_pair`, verified against a local Ruby 4.0.5 oracle.
**Range** — literals `1..5`/`1...5`, the `Range` class with
`begin`/`end`/`first`/`last`/`exclude_end?`, comparison-based
`include?`/`cover?`/`member?` (incomparable members return false rather than
raising, and `cover?` works on numeric and string ranges), `min`/`max`,
`size`/`count`, `to_a`/`each`/`map`, and `==`. Integer ranges iterate; Float
ranges raise `TypeError` ("can't iterate from Float") as in MRI.
**Comparable & Enumerable** — written *in embedded Ruby* via a go:embed'd
prelude that `VM.New` runs after the native bootstrap (the org's USP: each is
built on a single primitive — `<=>` and `each`). Enabling machinery: the `<=>`
and `<<` operators; operator method definitions (`def <=>`, `def ==`, `def []`
/`[]=`, …); comparison operators dispatch as methods when the receiver isn't a
built-in ordered type (numeric/string keep the inline fast path); native `<=>`
on Integer/Float/String/Object and a default identity `Object#==`; and blocks
made transparent to `yield` (a block reaches its enclosing method's block) so
`each { … yield … }` works. Comparable gives `<`/`<=`/`>`/`>=`/`==`/`between?`
/`clamp`; Enumerable gives map/select/reject/find/include?/to_a/count/sum/min
/max/reduce/any?/all?/none?/each_with_index/flat_map/partition/group_by/tally/zip.
**Short-circuit `&&` / `||`** with Ruby value semantics (yield the deciding
operand; the right side runs only when the left doesn't decide), compiled with
a conditional branch over a duplicated operand.
**`do…end` blocks** alongside `{ … }`, with the `do` after a while/until
condition correctly bound to the loop, not to a call in the condition.
**String methods**: length/size/bytesize
/empty?, upcase/downcase/capitalize/swapcase/reverse, strip/lstrip/rstrip/chomp
/chop, chars/bytes/split, include?/start_with?/end_with?/index, sub/gsub
(string patterns), to_i/to_f/to_s/to_str/to_sym, and `[]` slicing (index,
start+len, Range) — rune-aware where it matters. **String is mutable** (a
reference type): `<<`/concat/replace/prepend/insert/`[]=`/slice!/clear, the
bang transforms (`upcase!`…`gsub!`), and `freeze`/`FrozenError`.
**String interpolation** `"…#{expr}…"`: the lexer emits STRBEG/STRMID/STREND so
the embedded expression lexes in the outer scope (a `#{x}` reads the local `x`),
with per-interpolation brace tracking for nested `{}` and nesting; parts are
coerced with `to_s` and concatenated.
**Ternary** `cond ? a : b` (looser than ranges/binary, tighter than assignment,
right-associative) desugars to an If expression.
**`case`/`when`** (subject and condition forms) matching via `===`: Object#===
is `==`, Module/Class#=== is `is_a?`, Range#=== is membership — so `when
Integer`, `when 80..89`, `when 2` all dispatch correctly (subject evaluated
once; `===` lexed via a new EQQ token).
**Pattern matching `case`/`in`** (Ruby 4.0): the subject is deconstructed and
bound rather than tested with `===`. `in` is a contextual keyword (the clause
opener; no `for…in` in the grammar to conflict). Implemented forms: value
patterns (literals/ranges, matched with `===`), variable binding (`in x`, the
wildcard `_`), class/constant patterns (`in Integer`, via `is_a?`), the
`pat => name` binding suffix, and array patterns `[a, b]` / `[1, x]` / `[a,
*rest]` / `[*init, last]` / nested — using the **deconstruct** protocol
(`respond_to?(:deconstruct)`, Array#deconstruct returns self, Struct#deconstruct
its values; length + element checks). A leading constant gives a const array
pattern `Point[x, y]` (adds an `is_a?` guard). Guards (`in pat if cond` /
`unless cond`) gate a matched clause. With no matching clause and no `else`,
`OpRaiseNoMatch` raises **NoMatchingPatternError** (a StandardError subclass).
Compiled by `compileCaseIn`/`compilePattern`: the subject is evaluated once into
a hidden slot, each pattern leaves a boolean and performs its bindings as a side
effect, `OpTruthy` normalizes `===`/`is_a?` results.
**Hash patterns** use the **deconstruct_keys** protocol (`respond_to?`, then
`deconstruct_keys(nil)`; Hash and Struct both return their member hash): `in
{name:, age:}` (shorthand binding), `{status: "active"}` (value sub-pattern),
`{a: x}` (renaming), `{a:, **rest}` (`except`-captures the unnamed keys),
`{a:, **nil}` (forbids extras), and the empty `{}` (matches only an empty hash).
A key absent from the deconstructed hash fails the match. The brace-less
top-level form `in a:, b:` is accepted too. compileHashPattern emits the
per-key `key?` + `[]` checks. A leading constant gives a **const hash pattern**
`Point(x:, y:)` (the paren form; adds the `is_a?` guard, via deconstruct_keys).
**Pin** `^name` / `^(expr)` matches against an existing value with `===` instead
of binding; **alternative** `p1 | p2 | …` matches if any branch does
(short-circuiting). **One-line patterns**: `subject => pattern` (rightward
assignment — binds, or raises NoMatchingPatternError) and `subject in pattern`
(a boolean test), parsed at statement level or in parentheses (`r = (v in P)`),
with `=` binding tighter. The **find pattern** `[*pre, mid…, *post]` scans
for the first matching window (compileFindPattern's scanning loop). Pattern
matching is **complete**.
**More String methods**: `ljust`/`rjust`/`center` (rune-width padding) and
`tr`/`count`/`delete`/`squeeze` (with `a-z` range expansion).
**`**` exponentiation** (right-associative, tighter than `*`/`/`) + Integer
/Float `**`/`pow`.
**Beginless/endless ranges** (`..5`, `1..`, `arr[2..]`; nil endpoints, open in
===/cover?/include?), Array range + `start,len` slicing (shared `sliceRange`).
**Optional/default method parameters** (`def f(a, b = expr)`; default compiled
into the prologue via OpArgGiven, may reference earlier params; ISeq.NumRequired
+ range arity error). **Splat parameter** `def f(a, *rest)` (also `*all`, and
combined with optionals) — collects the remaining args into an array
(ISeq.SplatIndex; `expected N+` arity error). **Splat arguments** `f(*arr)` /
`[*a, *b]` — splice an array into a call's args or an array literal (runtime
arg-array build via OpSplatToArray/OpConcatArray + OpSendArray). **Keyword arguments** `def f(a:, b: 2)` and `f(a: 1)` — keyword
params (required and optional, defaults may reference earlier params) plus the
call-site last-hash sugar; the VM validates unknown/missing keywords
(ArgumentError, singular/plural). **Array** sum/each_slice/rotate + flatten(depth);
**Hash** slice/except/merge!. **Blocks/Procs/lambdas**: `&block` params reify
the method block as a first-class Proc (call/[]/yield/arity/lambda?); call-site
`&proc` block-pass + `Symbol#to_proc` (the `&:sym` shorthand, via native-bodied
Procs); `proc`/`lambda` constructors; and the stabby lambda `->(x){…}`. **`String#%`/`format`/`sprintf`** — Ruby's format
engine (d/i/s/f/e/g/x/X/o/b/c + flags/width/precision), Go-fmt backed. **Integer** `lcm`/`bit_length`/`digits(base)`. **String**
`lines`/`each_line`/`each_char`/`each_byte`. **Parser**: negative-numeric-literal
precedence (`-2.abs`==2, `-2**2`==-4). **Class methods** `def self.foo` (singleton-method
chain, inherited). **Constant assignment** `NAME = v` and **attribute assignment**
`recv.attr = v` + setter defs `def name=`. **Struct** `Struct.new(:a,:b)`
(accessors, to_a/to_h/members/[]/==/size).
**Integer/Float numeric methods**: Integer `abs`/`even?`/`odd?`/`zero?`/
`positive?`/`negative?`/`succ`/`next`/`pred`/`to_i`/`to_int`/`to_f`/`to_s(base)`
/`gcd`/`divmod`/`digits`/`chr`/`upto`/`downto`; Float `abs`/sign predicates/
`to_f`/`to_i`/`to_int`/`ceil`/`floor`/`round`/`nan?`/`finite?`/`infinite?`. The
prelude mixes **Comparable into Integer, Float, and String**, so `between?`
/`clamp` work on them, and **Enumerable into Array and Range**, so both inherit
`select`/`reject`/`find`/`reduce`/`sum`/`any?`/`all?`/`none?`/`each_with_index`
on top of their native `each` (native methods win where both exist).
**Block auto-splat**: a multi-parameter block called with a single Array
destructures it (`[[1,2]].each { |a,b| }`), which also makes **Hash
Enumerable** — `Hash#each` yields a `[k, v]` pair, so map/find/count/any?/all?
/none?/to_a operate on pairs (a one-param block sees the pair); `select`/`reject`
stay native since they return a Hash.
**Label hash literals**: `{ name: value }` lexes `name:` as a LABEL and is sugar
for `{ :name => value }`, mixable with the hashrocket form.
**`break` / `next`**: a compiler context stack resolves them to the innermost
block or loop. `next [v]` returns from a block frame or continues a loop;
`break [v]` exits a loop or terminates the iterator a block was passed to — and
unwinds to the correct call site even through a Ruby-level iterator (`map { break
}` returns from `map`), via an OpBreak panic tagged with the executing block's
identity.
**Compound assignment**: `+= -= *= /= %= <<= ||= &&=` desugar to `lhs = lhs OP
rhs` for locals (a fresh-slot-aware OpAssign node so `x ||= v` defines `x`),
ivars, and `recv[i] OP= v`.
**Top-level `@ivars`**: `main` is now a real object with its own ivar table,
shared with top-level method bodies (closing the Phase 1 no-op-set quirk).
**Array methods**: sort/sort_by/reverse/uniq/flatten/compact/join/index/take
/drop/min_by/max_by/each_with_object (on top of the inherited Enumerable set);
ordering goes through `<=>`.
**Exit:** most "ordinary" Ruby runs.

### Phase 3 — Control flow & exceptions — 🚧 in progress
Exception hierarchy, `raise`/`rescue`/`ensure`/`retry`, non-local
`break`/`next`/`redo`/`return` via catch tables, `StopIteration`, **Fiber +
Enumerator + `loop` + `lazy`**.
**Started:** the **exception hierarchy** (Exception → StandardError →
RuntimeError + the built-in error classes with correct superclasses), Exception
`#initialize`/`#message`/`#to_s`, `Object#is_a?`/`#kind_of?`/`#instance_of?`, and
`Kernel#raise` (string → RuntimeError, class → instantiated, instance →
re-raised, class + message). **`begin`/`rescue`/`ensure`/`else`** via a
re-entrant exec loop with a per-frame handler stack (`OpPushHandler`/
`OpPopHandler`, a deferred recover that resumes at the rescue dispatch and lets
non-exception panics through); rescue matches by class with `is_a?` (bare =
StandardError), class lists, `=> var` binding, and `OpReThrow` on no match;
`else` on the clean path, `ensure` on both normal and propagating paths;
internal raises (`1/0`, NoMethodError, …) are rescuable; bare `raise` re-raises;
**`retry`** re-enters the begin body from a rescue (ensure still runs once);
**method-level rescue/ensure** — a `def` body carries rescue/else/ensure
clauses without an explicit `begin` (shared `parseRescueTail`); and
**`Kernel#loop`** (block-driven infinite loop, terminated by `break`).

### Phase 4 — Full metaprogramming — 🚧 in progress
`define_method`, `instance_eval`/`instance_exec`, `class_eval`, constant
machinery, hooks (`included`/`inherited`/`method_added`/…),
`define_singleton_method`, **string `eval`**. (Refinements: deferred.)
**Started:** reflection/dispatch — `send`/`public_send` (forwarding the block),
`respond_to?`, `itself`, `tap`, `then`/`yield_self`; Kernel conversion methods
`Integer`/`Float`/`String`/`Array` (capitalized `Name(...)` now parses as a call).

### Phase 5 — Complete front-end — 🚧 in progress
Full grammar: heredocs, interpolation, `%`-literals, multiple assignment, keyword
args, pattern matching, endless methods, beginless/endless ranges, safe
navigation, numbered params/`it`. *(Or adopt Prism-on-wazero — see §16.)*
**Landed:** interpolation, keyword args, multiple assignment / destructuring,
**full pattern matching `case`/`in`** (value/bind/class/array/hash/find/pin/
alternative patterns, guards, one-line `=>`/`in`, `deconstruct`/
`deconstruct_keys`), endless methods, beginless/endless ranges, **safe
navigation `&.`**, **numbered params (`_1`/`_2`) + `it`**, **heredocs**
(`<<`/`<<-`/`<<~`, interpolating and literal), and **`%w`/`%i` arrays**. **Still
ahead:** `%q`/`%Q`/`%W`/`%I` literals.

### Phase 6 — Standard library
IO/File/Dir, Time, Random, Thread/Mutex/Queue, **Regexp** (§16), Marshal; embed
pure-Ruby libs; implement Go leaves (json, digest, zlib, securerandom, base64).
Run upstream suites.

**Regexp bridge — 🚧 landed (6a–6d):** the `go-onigmo/regexp` engine is wired in
(CGO=0).
- **6a** — `/re/imx` literals (lexer disambiguates `/` via the value/operand
  state), the `=~` operator, a vm-level `Regexp` value + `cRegexp`
  (`source`/`to_s`/`inspect`/`match`/`match?`/`=~`/`===`), a `MatchData` value +
  `cMatchData` (`[]` by index or name, `pre_match`/`post_match`/`to_a`/`captures`/
  `begin`/`end`/`named_captures`/`size`), and `String#=~`/`match`/`match?`. Ruby
  flags are translated to an inline `(?imx)` prefix; `RegexpError` for bad
  patterns.
- **6b** — `String#scan` (whole matches, or capture arrays when groups are
  present; block form yields each).
- **6c** — `String#sub`/`gsub` over a Regexp: replacement templates
  (`\0`/`\&`, `\1`..`\9`, `\k<name>`, `\``, `\'`) or a block.
- **6d** — `String#split` over a Regexp: interpolated captures, field `limit`,
  and the awk-style whitespace mode (no arg / `nil` / `" "`).

**Byte vs character offsets:** go-onigmo reports **byte** offsets, while Ruby's
`MatchData#begin`/`#end` and `=~` report **character** offsets; the bridge
converts (`byteToChar`). Matched substrings are representation-independent.
Differential-tested against MRI (Ruby 4.0.5): offset-exact on ASCII, substring
comparison for multibyte. Since go-onigmo's encoding-aware cursor (engine
`b6bfa2d`), a bare `.` and the byte-oriented classes advance by a whole UTF-8
**character** in the default UTF-8 mode, so `/./` matches `é` and
`"café".scan(/./)` yields four characters — matching MRI. (Literal multi-byte
*class members* like `[é]`/`[à-ï]` are the remaining engine-side item.)

The `$~`/`$1`..`$N`/`$&`/`` $` ``/`$'` match globals are now supported
(VM-global last match). **Still deferred:** `Regexp.new` from a string, the
`(?'name')` named-group spelling (unsupported by the engine), and the
`sub`/`gsub` enumerator and Hash-replacement forms.

### Phase 7 — Build toolchain
`rbgo build`, require-graph scan, selection (build tags), `//go:embed`, single
static binary, closed-world mode.

### Phase 8 — Conformance & performance
ruby/spec subset; optimisations: dispatch **inline caches** (key = class), fixnum
cache, reduced boxing. (JIT out of initial scope — Go does not help with runtime
native codegen.)

## 15. Risk register

| Risk | Severity | Mitigation |
|---|---|---|
| **Parser scope / front-end effort** | **Highest** | Subset-first + oracle; **evaluate Prism→WASM on wazero (§16)** to delete most risk; test-driven growth |
| **Regexp** (Onigmo vs RE2: no backrefs/lookbehind in Go `regexp`) | High | Pure-Go Onigmo reimpl., standalone module (`go-onigmo/regexp`); ReDoS via memoization + timeout |
| **Native-frame unwinding** at the Go↔Ruby callback boundary | High | Hybrid: status returns internally + `panic`/`recover` at native callbacks; design in Phase 1 |
| **String/encodings** (mutable, multi-encoding) | High | `[]byte`+encoding tag from Phase 2; incremental transcoding |
| **Value boxing vs Go GC** (no MRI-style immediates) | High | Position as embedding-not-speed; split Fixnum/Bignum + fixnum cache early; inline caches Phase 8 |
| **Fiber/Enumerator cost & goroutine leaks** | Medium-High | Finalizer teardown; consider pooling; document the perf cliff |
| **Thread/Ractor model** baked late | Medium | Decide early (§16); single-thread + Fiber to start; Ractor needs early design |
| **Marshal / ObjectSpace** | Low | Stub; `_id2ref`/`each_object` permanently out |
| **C-extension gems** | — | Out of scope (pure Ruby + Go leaves only) |

## 16. Decision journal / open questions

1. **Value representation** — interface + concrete types (start) → split
   Fixnum/Bignum + fixnum cache (early) → inline caches (Phase 8). *Settled:
   interface first; accept the perf positioning.*
2. **Regexp engine** — *settled* → **pure-Go reimplementation of Onigmo**
   (Ruby's engine), faithful backtracking VM, as a **standalone reusable Go
   module** in the sibling org `go-onigmo` (repo `regexp`; see
   `go-onigmo/regexp/docs/plan-regexp.md`). ReDoS handled by memoization +
   `Regexp.timeout` (as Ruby ≥3.2).
3. **Front-end strategy** — *OPEN* → hand-written lexer/parser (learning goal,
   full control) **vs Prism→WASM under pure-Go wazero** (production-grade,
   upstream-tracking, deletes most risk). Decide before investing heavily in
   Phase 5. This reshapes Phases 0/5/13.
4. **Native↔Ruby unwinding** — *settled* → hybrid (status internally +
   panic/recover at native callbacks); design the boundary in Phase 1.
5. **Thread model** — emulated GVL (MRI semantics) vs real parallelism vs
   **Ractor** (its shareable/non-shareable split already isolates state, and fits
   Go's goroutines). *Start single-thread + Fiber; decide by Phase 6, but note
   Ractor must be designed in early or it is permanently out.*
6. **Target version** — **Ruby 4.0** semantics.
7. **Oracle tool** — Ripper and/or Prism offline (dev only, not linked).
8. **Performance & multi-arch** — *settled* → benchmark **systematically against
   reference Ruby (MRI)** from the start (reuse the oracle corpus as a perf
   corpus, gate regressions in CI); use **go-asmgen** to accelerate hot
   computations (CGO=0 SIMD) behind byte-identical tests; **validate correctness
   and performance on all six 64-bit arches** — amd64, arm64, riscv64, loong64,
   ppc64le, s390x (see §13a).

## 17. First concrete steps (session 1) — done in Phase 0

`go mod init` · `internal/` tree · `token/` · `lexer/` (with `spaceSeen` and a
minimal `lexState` from the start) · `oracle`-style fixtures · `ast/` · `parser/`
(Pratt + scope stack) · `compiler/` · `vm/` · `cmd/rbgo run`.
**Exit criterion (met):** `puts 1 + 2` and `fib(20)` run.

## 18. Prior art & neighbours

- **`goruby/goruby`** (~600★, MIT) — the notable "Ruby in Go", but a
  **tree-walking** interpreter (Thorsten Ball lineage), partial, learning-oriented.
- **`towski/goruby`** — a small pedagogical **bytecode** interpreter; close to our
  approach, worth reading.
- **`goby-lang/goby`** — a Ruby-*inspired* OO language, not a compatible
  implementation.
- **cgo bindings** (out of scope: cgo) — `go-mruby`, `go-oniguruma`/`rubex`.

**Positioning of go-embedded-ruby:**

| Axis | `goruby/goruby` | `go-embedded-ruby` |
|---|---|---|
| Execution model | AST-walking | **Bytecode VM** + embedded front-end |
| Single binary / tree-shaking | No | **Yes** (build-time selection, build tags) |
| Dynamic `eval`/`require` | No | **Yes** (embedded front-end) |
| Regexp | Absent | **Pure-Go Onigmo** (`go-onigmo/regexp`) |
| Stdlib strategy | — | Go core + embedded Ruby |
| Compatibility target | Partial subset | **Ruby 4.0 semantics**, test-driven growth |

Reusable as reference (MIT): the lexer/parser/object model of `goruby/goruby`;
the bytecode approach of `towski/goruby`. To study, not to copy — our VM diverges
from the compiler onward.

### Naming decisions
- Org **`go-embedded-ruby`** (consistent with the `go-*` org convention;
  "embedded" names the zero-cgo embeddability USP better than a bare name). Main
  repo **`ruby`** (module `github.com/go-embedded-ruby/ruby`), CLI **`rbgo`**.
- Regexp engine in its own org **`go-onigmo`**, repo **`regexp`** — reusable
  beyond Ruby.
