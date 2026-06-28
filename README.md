<p align="center"><img src="https://raw.githubusercontent.com/go-embedded-ruby/brand/main/social/go-embedded-ruby.png" alt="go-embedded-ruby/ruby" width="720"></p>

# ruby — go-embedded-ruby

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-9B1C2E)](https://go-embedded-ruby.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Phase](https://img.shields.io/badge/phases-0--7%20done%20%C2%B7%208%20active-1a7f37)](https://go-embedded-ruby.github.io/docs/roadmap/)

**A pure-Go implementation of Ruby — one static binary, full dynamism, zero cgo.**

This repository is the interpreter: a compiler that lowers Ruby to bytecode, and
a stack VM (mruby/YARV lineage) that runs it. The Ruby **front-end** (lexer,
parser, AST) is the standalone pure-Go
[go-ruby-parser](https://github.com/go-ruby-parser/parser) module, which this
interpreter imports. The front-end is **embedded in the binary**, so `eval` and
runtime `require` keep working. Ruby
objects are Go heap objects, so **Go's garbage collector is reused**. Dispatch
goes through **mutable per-class method tables**, which is what makes
monkey-patching, `define_method` and `method_missing` free.

> 🌐 [Website](https://go-embedded-ruby.github.io) · 📚 [Documentation](https://go-embedded-ruby.github.io/docs/) · 🧭 [Roadmap](docs/plan-rbgo.md)

## Status

### Conformance & performance (campaign final, 2026-06-27)

A major conformance + performance campaign measured rbgo's pure-Go **front-end
(parse + compile)** against the MRI 4.0.5 oracle on the two largest real-world
Ruby codebases and a suite of popular libraries. All figures are
**front-end acceptance**, not whole-application execution (see the honesty note
below).

- **Parse: 99.96 % of Rails + Puppet** (5577 / 5579 `.rb` files). The only 2
  misses are intentional syntax-error fixtures **MRI itself rejects** — i.e.
  **100 % of all valid Ruby** in the corpus parses.
- **End-to-end (parse + compile): Rails 99.82 %** (3417 / 3423),
  **Puppet 100.00 %** (2154 / 2154).
- **0 over-permissive** — rbgo never accepts Ruby that MRI rejects.
- **Library parse-conformance:** RuboCop 99.7 %, Sinatra / Jekyll / Thor /
  Kramdown / dry-struct 100 %, Homebrew 98.7 %, Chef 99.1 %, concurrent-ruby
  98.3 %, Asciidoctor 93.8 %.
- **RSpec DSL usage: 10/10** byte-identical to MRI.
- The journey (Rails end-to-end, across 5 parser rounds + 5 rbgo activation
  rounds): 20.7 % → 46.3 % → 68.7 % → 81.2 % → 86.7 % → 93.8 % → 98.4 % →
  **99.82 %**.

Features added in the campaign include `::` constant-paths (qualified class /
module names, superclass, leading `::`), paren-less command calls with
args/kwargs, `class << self` (singleton class), masgn to any target
(ivar/cvar/gvar/attr/index/constant + nested destructuring), `alias`/`undef`,
anonymous params + forwarding (`def f(*, **, &)` / `g(...)`), special globals
(`$$` etc.), shorthand hash `{x:}`, quoted / operator / char-literal symbols,
rationals & imaginaries (`2r` / `3i`), unicode identifiers, `for…in…end` loops,
block keyword params, begin-less rescue/ensure, and `!~` — on top of the
pre-existing core (full object model, metaprogramming, Fiber/Thread, Marshal,
regexp, the scientific bindings, and the js/wasm target) detailed below.

**Performance:** a 6-runtime comparative suite ([BENCHMARKS.md](BENCHMARKS.md))
pits rbgo and rbgo+AOT against MRI, MRI+YJIT, JRuby and TruffleRuby. The rbgo
interpreter runs ~3–6× MRI on compute and at parity on I/O-bound work;
**rbgo+AOT beats MRI+YJIT 18–24× on loop/fib** (the only runtime here that beats
YJIT); TruffleRuby is the compute ceiling.

> **Honesty note.** "99.82 % of Rails" means rbgo's front-end **parses and
> compiles** that fraction of Rails's `.rb` files — *not* that rbgo **runs**
> Rails. Running a full application additionally needs the runtime stdlib surface
> and C-extension equivalents. That surface is now real enough that **Puppet
> boots and evaluates manifests under rbgo** (see *Running Puppet* below); full
> `puppet apply` (the transaction / provider layer) is still in progress. What is
> established is that **the Ruby language / front-end is essentially complete** on
> real-world code, and that the runtime is far enough along to boot a real
> application; whether any *given* application runs end-to-end remains
> application-specific work. Details:
> [CONFORMANCE-RAILS-PUPPET.md](CONFORMANCE-RAILS-PUPPET.md),
> [CONFORMANCE-LIBRARIES.md](CONFORMANCE-LIBRARIES.md),
> [CONFORMANCE-RSPEC.md](CONFORMANCE-RSPEC.md).

### Running Puppet — boots, parses, compiles, and evaluates manifests

Beyond parsing real-world Ruby, **rbgo now runs [Puppet](https://github.com/puppetlabs/puppet)**.
`require "puppet"` **fully boots** the framework (Puppet 8.11.0) on a pure-Go
CGO=0 `rbgo` — its pure-Ruby gem dependencies (`semantic_puppet`,
`concurrent-ruby`, `deep_merge`, `fast_gettext`, `facter`, `racc`, …) load on the
`$LOAD_PATH` — and a manifest then travels the real Puppet path: it **parses to
the Pops AST, compiles to a catalog, and evaluates**. A trivial manifest emits
real Puppet output:

```ruby
notice("hi from puppet")
# => Notice: Scope(Class[main]): hi from puppet
```

Reaching this exercised a large slice of the runtime, each gap reduced to a
minimal rbgo-vs-MRI repro before fixing:

- **Language / VM conformance:** `autoload`, frame-based `Exception#backtrace` /
  `set_backtrace` / `full_message`, interpolated regexp literals, non-local block
  `return`, `Symbol#intern`, `NilClass` conversions, `Array` slice-assignment,
  `Module.new` (real anonymous module running its block as a body),
  `extend` transitivity (a module's transitively-included methods become class
  methods), setter expressions returning their RHS, nested constant namespaces,
  and method-visibility enforcement — among dozens more.
- **Pure-Go stdlib modules** added on the path to boot: **`ERB`** (template
  engine), **`openssl`** (real crypto, not a stub), **`net/http`**, **`resolv`**,
  **`tmpdir`**, **`Process`**, **`StringScanner`** (`strscan`), **`Find`**,
  **`getoptlong`**, **`syslog`**, **`fileutils`**, `optparse`, `objspace`, and
  more — each `require`-able and CGO=0.

This is the **C-extension → pure-Go shim** strategy in action: a real Ruby
application ships as a single static CGO=0 binary because the C-backed gem APIs
are backed by pure Go. Puppet validates the approach end to end — its
dependency tree is pure Ruby, so it loads as-is.

> **Frontier (honest):** what works today is **boot → parse → compile →
> evaluate** a manifest (the Pops evaluator emits `Notice:`/`warning`/… output).
> Full **`puppet apply`** — the transaction / RAL / resource-provider layer that
> mutates real system state — is the **active next milestone**, not done. The
> example above is a real evaluation; an `apply` that converges resources against
> a host is in progress.

### Supported today

Supported today (every feature **differential-tested against MRI Ruby 4.0.5 and
JRuby**):

- **Values:** integers (`int64`, with automatic **Bignum** promotion on int64
  overflow and arbitrary-precision integer literals, **radix literals** `0x`/`0o`/
  bare-`0`-octal/`0b`/`0d` with underscores), floats, strings, symbols (incl.
  **operator-method symbols** `:+`/`:<<`/`:[]=`/`:<=>`, usable with
  `reduce(:+)`/`inject`/`send(:+, x)`), arrays, hashes, ranges (incl.
  beginless/endless), **`Complex`** and **`Rational`** numbers, `true`/`false`/
  `nil`, `self`, `Proc`/lambda, `Regexp`/`MatchData`, `Struct`.
- **Operators:** arithmetic (`+ - * / %`, **Ruby floor division**, `**`),
  comparison/`<=>`, `==`/`===`, bitwise/shift (`<< >> & | ^ ~`, arbitrary
  precision), `&&`/`||`, ternary, ranges, **`::` constant scope** (`Math::PI`,
  `Foo::BAR`); correct negative-literal precedence (`-2.abs == 2`, `-2**2 == -4`).
- **Control flow:** `if`/`elsif`/`else`, `unless`, `while`/`until`,
  `case`/`when`, statement modifiers (incl. modifier `rescue`,
  `expr rescue fallback`), `begin`/`rescue`/`else`/`ensure`/`retry`,
  `break`/`next`, `Kernel#loop`, and **`Fiber`** (cooperative coroutines —
  `Fiber.new`/`resume`/`Fiber.yield`/`alive?`).
- **Concurrency:** **`Thread`** (`new`/`start`/`join`/`value`/`status`/`current`/
  `main`/`list`/`pass`, thread-locals via `[]`/`[]=`, exception propagation on
  join), **`Mutex`** (`lock`/`unlock`/`try_lock`/`synchronize`/`owned?`) and
  **`Queue`** (blocking `push`/`pop`, `close`), on an **emulated GVL** — one Ruby
  thread runs at a time, matching MRI's memory model (race-free under the Go
  race detector).
- **Pattern matching (`case`/`in`):** value, variable-binding, class/constant,
  array (incl. splat and nested), hash (`deconstruct_keys`, `**rest`/`**nil`),
  find (`[*pre, x, *post]`), pin (`^x`) and alternative (`a | b`) patterns;
  the `=> name` binding suffix, guards (`if`/`unless`), the one-line forms
  `expr => pattern` and `expr in pattern`, and `NoMatchingPatternError` — via
  the `deconstruct`/`deconstruct_keys` protocols.
- **Assignment:** multiple assignment / destructuring (`a, b = 1, 2`, swap,
  `x, *rest = …`, `*init, last = …`), compound assignment
  (`+= -= *= /= %= <<= ||= &&=`), **global variables** (`$g`, plain and compound).
- **Methods:** required / optional / `*splat` / **keyword** (`a:`, `b: 2`) /
  `**rest` / `&block` parameters, setter defs (`def name=`), **endless methods**
  (`def foo = expr`), **singleton method defs on any object** (`def obj.foo` /
  `def Const.foo`), recursion, `return`, `super`.
- **Blocks / Procs / lambdas:** `{ }` / `do…end` closures, `yield`,
  `block_given?`, `&block` capture, **block params** with destructuring
  (`|(a, b)|`) and **rest** (`|*rest|`, `|head, *rest|`), **numbered params
  (`_1`/`_2`) and `it`**, `Proc`/`lambda`/**stabby `->(){}`**, `&proc` block-pass
  and `Symbol#to_proc` (the `&:sym` shorthand).
- **Classes & modules:** inheritance, `@ivars`, **`@@class variables`** (shared
  down the superclass hierarchy), `new`/`initialize`, constants and constant
  assignment, **class methods** (`def self.foo`), modules + **`include`/`prepend`**
  (mixins, with full **ancestor-chain `super`** through included/prepended
  modules and the singleton chain), `Module#ancestors`/`include?`,
  **`attr_accessor`/`reader`/`writer`**, **`Struct.new`**.
- **Metaprogramming:** dynamic dispatch via mutable method tables,
  `method_missing`, `send`/`public_send`, `respond_to?`, **`define_method`**,
  **`instance_eval`/`instance_exec`**, **`class_eval`/`module_eval`/`class_exec`**,
  `instance_variable_get`/`set`/`defined?`, **string `eval`** (the embedded
  front-end compiling Ruby at runtime), **`Binding`** (`binding`,
  `Binding#eval`, `eval(str, binding)`, `local_variable_get`/`set`/`defined?`,
  `local_variables`, `receiver` — capturing a frame's locals so eval'd code
  reads and writes them), and the class/module **hooks**
  `inherited`/`included`/`prepended`/`method_added`/`extended`.
- **Runtime loading:** **`require`/`require_relative`** load, compile and run a
  `.rb` file once (relative + search-path resolution, `LoadError` on miss,
  `true`/`false` return) — the embedded front-end loading code at runtime.
- **Strings:** mutable (reference semantics) with `<<`/concat/replace/prepend/
  insert/`[]=`/slice!/the bang methods and `freeze`/`FrozenError`;
  interpolation, heredocs (`<<`/`<<-`/`<<~`), `%w`/`%i` and `%q`/`%Q`/`%W`/`%I`
  literals, the `\a`/`\b`/`\v`/`\f`/`\s`/`\n`/`\t`/`\r`/`\e`/`\0` escapes,
  `%`/`format`/`sprintf`, case/strip/`split`/`each_char`/`lines`/`succ`(`next`)
  and friends.
- **Regular expressions:** `/re/imx` literals, `Regexp`/`MatchData`, `=~` /
  `match` / `match?` / `scan` / `gsub` / `sub` / `split`, and the match globals
  `$~` / `$1`..`$N` / `$&` / `` $` `` / `$'` — running on the standalone pure-Go
  [go-ruby-regexp](https://github.com/go-ruby-regexp/regexp) engine, so the build stays
  **CGO=0**.
- **Standard library leaves:** **`JSON`** (`generate`/`dump`/`pretty_generate`/
  `parse` + `Object#to_json`, with object key order preserved and MRI-matching
  number/escape formatting), **`Digest`** (`MD5`/`SHA1`/`SHA256`/`SHA512` —
  `hexdigest`/`digest`/`base64digest`), **`Base64`**, and **`Zlib`** (`crc32`/
  `adler32` + `Deflate`/`Inflate`) — each `require`-able and pure-Go.
- **`Marshal`** (`dump`/`load`/`restore` + `MAJOR_VERSION`/`MINOR_VERSION`) —
  Ruby's binary serialization, **byte-for-byte identical to MRI** across
  Integer/Bignum, Float, Symbol, String, Array, Hash (incl. defaults), and the
  symbol/object-link tables (shared objects and cycles round-trip). Runs on the
  standalone pure-Go
  [go-ruby-marshal](https://github.com/go-ruby-marshal/marshal) engine, so the
  build stays **CGO=0**.
- **File & Random:** **`File`** — path helpers (`basename`/`dirname`/`extname`/
  `join`/`split`/`expand_path`) and filesystem ops (`read`/`write`/`exist?`/
  `file?`/`directory?`/`size`/`delete`), raising `Errno::ENOENT` for missing
  paths; **`Random`** — a bit-exact reimplementation of MRI's seeded MT19937, so
  `Random.new(seed)` / `srand`+`rand` reproduce MRI's sequence.
- **IO:** **`IO`** with `$stdout`/`$stderr`/`STDOUT`/`STDERR`/`$stdin` as real
  objects (`write`/`<<`/`print`/`puts`/`printf`/`putc`/`sync`/`flush`/`close`),
  **`StringIO`** (`require "stringio"`) — an in-memory IO with the full read side
  (`read`/`gets`/`getc`/`readline`/`readlines`/`each_line`/`each_char`,
  `pos`/`seek`/`rewind`/`truncate`/`eof?`/`string`), and `Kernel#warn`.
  `Kernel#puts`/`print`/`p` write through the current `$stdout`, so reassigning
  it to a `StringIO` captures output, as in MRI. **`File.open`** (modes `r`/`w`/
  `a`/`r+`, block-scoped auto-close) returns a file-backed IO (`File` **< `IO`**)
  with the same read/write protocol, plus `File.readlines`/`File.foreach`.
- **`Dir`:** `entries`/`children`/`glob`/`[]`, `exist?`/`empty?`, `pwd`/`home`,
  `mkdir`/`rmdir`/`chdir` (block-scoped), `each_child`/`foreach`, raising
  `Errno::ENOENT`/`Errno::EEXIST` as MRI does.
- **Collections:** Array / Hash / Range with `Enumerable` (map/select/reduce/
  `minmax`/…) and `Comparable`, both written once in embedded Ruby; Array **bang
  methods** (`map!`/`sort!`/`select!`/`reject!`/`compact!`/`uniq!`/`reverse!`),
  **structural/combinatorial ops** (`transpose`/`product`/`combination`/`to_h`),
  the **`Hash[…]`** constructor, **String ranges** (`("a".."e")` iterating via
  `String#succ`), and **`Range#step`/`Integer#step`** (integer and float walks,
  both directions).
- **Enumerator:** every blockless iterator (`each`/`map`/`select`/`reject`/
  `each_slice`/`each_cons`/`each_with_index`/`times`/`upto`/`each_char`/…) returns
  an `Enumerator` (MRI semantics) with `next`/`peek`/`rewind`/`size`/`to_a`,
  `with_index`/`each_with_index`, `Kernel#enum_for`/`to_enum`, and full
  `Enumerable` chaining (`[1,2,3].map.with_index { |x, i| … }`); plus
  **`Enumerator::Lazy`** (`lazy`) — deferred `map`/`select`/`reject`/`filter_map`/
  `take`/`take_while`/`drop`/`drop_while` over finite or **infinite**
  (`(1..Float::INFINITY).lazy`) sources, materialised by `first`/`to_a`/`force`.
- **Numeric tower:** `Integer`/`Float`/`Rational`/`Complex` under a shared
  `Numeric` (carrying `Comparable`); `Module#ancestors`/`include?`,
  `Class#superclass`.
- **Objects:** `dup`/`clone`/`freeze`/`frozen?`, `equal?`,
  `object_id`/`__id__` (MRI's deterministic immediate-value ids, stable per
  reference object), `instance_variable_get`/`set`.
- **Math:** the `Math` module (`sqrt`/`cbrt`/`exp`/`log`/`log2`/`log10`, the
  trig and hyperbolic functions, `atan2`/`hypot`/`pow`) with `Math::PI`/`Math::E`.
- **NDArray:** a NumPy-style n-dimensional array — `zeros`/`ones`/`full`/`arange`/
  `from`, element-wise `+ - * /` with scalar broadcasting, ufuncs
  (`sqrt`/`exp`/`log`/`sin`/`cos`/`abs`), reductions (`sum`/`mean`/`max`/`min`/
  `prod`/`argmax`/`argmin`), `matmul`/`dot`, `transpose`/`reshape`/`flatten`,
  `shape`/`to_a`/`[]` — binding the pure-Go
  [go-ndarray](https://github.com/go-ndarray/ndarray) library, **no cgo / no
  NumPy**.
- **Image:** a scikit-image-style image processor — `Image.new`/`load`/`save`,
  pixel `get`/`set`, filters (`gaussian_blur`/`box_blur`/`median`/`sharpen`),
  edges (`sobel`/`prewitt`/`scharr`/`laplacian`/`canny`), morphology
  (`erode`/`dilate`), geometry (`resize`/`rotate90`/`crop`/`flip_*`), colour
  (`grayscale`/`invert`/`rgb_to_hsv`/`otsu`) — binding the pure-Go
  [go-images](https://github.com/go-images/images) library, **no cgo**.
- **FFT:** an `FFT` module — 1-D (`fft`/`ifft`/`rfft`/`irfft`), N-D and 2-D
  (`fftn`/`ifftn`/`fft2`/`ifft2`), bin-frequency helpers
  (`fftfreq`/`rfftfreq`), window functions
  (`hann`/`hamming`/`blackman`/`blackman_harris`/`bartlett`), and spectral
  helpers (`psd`/`spectrogram`) — binding the pure-Go
  [go-fft](https://github.com/go-fft/fft) library, a `numpy.fft`-style transform
  with **no cgo / no FFTW**, returning `Complex` spectra.
- **Set:** Ruby's `Set` — `new`/`[]`, `add`(`<<`)/`add?`/`delete`/`merge`/`clear`,
  `include?`/`member?`/`size`/`length`/`count`/`empty?`, `subset?`/`superset?`,
  `union`(`|`)/`intersection`(`&`)/`difference`(`-`), `each`/`to_a`/`to_set` —
  binding the pure-Go
  [go-composites/set](https://github.com/go-composites/set) library.
- **Time:** Ruby's `Time` — `now`/`at`/`parse`, arithmetic (`+`/`-`/`<=>`),
  `strftime`/`strptime`, `year`/`month`/`mday`/`hour`/`min`/`sec`/`wday`, weekday
  predicates (`monday?`…`sunday?`), `utc`/`getutc`/`zone`, `to_i`/`to_f` —
  binding [go-composites/time](https://github.com/go-composites/time).
- **Date:** Ruby's `Date` — `new`/`parse`, `+`/`-`/`<<`/`>>` and
  `next_day`/`prev_day`/`next_month`/`prev_month`,
  `year`/`month`/`mday`/`wday`/`yday`/`cwday`, `leap?`, comparisons — binding
  [go-composites/date](https://github.com/go-composites/date).
- **BigDecimal:** arbitrary-precision decimal — `+ - * / **`,
  `sqrt`/`abs`/`ceil`/`floor`/`round`/`pow`, `to_f`/`to_i`, `zero?`, comparisons
  — binding [go-composites/bigfloat](https://github.com/go-composites/bigfloat).
- **Bag:** a multiset / counter (element → multiplicity) — `add`(`<<`)/`delete`,
  `count`/`size`/`distinct`/`most_common`, `union`/`difference`/`intersection`,
  `include?`/`each`/`to_a` — binding
  [go-composites/bag](https://github.com/go-composites/bag).

- **AOT compiler (`rbgo build`):** lowers a program's methods to native Go and
  links a specialised binary. Pure integer methods become unboxed `int64`
  kernels with an overflow/`÷0` deopt back to the interpreter — the generated
  `fib(30)` runs **~4× faster than MRI+YJIT** while staying correct for every
  input. See [docs/aot-compiler.md](docs/aot-compiler.md).

- **Closed-world binary (`rbgo build --closed`):** bakes the whole program in as
  bytecode (and loads the prelude from frozen bytecode), then **drops the
  lexer/parser/compiler** from the link. The result runs with no source file and
  no front-end — a smaller, self-contained binary. `rbgo build` reports which
  `eval`/`require` calls (if any) would raise in the closed binary, since there
  is no front-end left to compile source at runtime.

- **WebAssembly (`GOOS=js GOARCH=wasm`) is a supported target.** rbgo runs in the
  browser two ways: the **playground** ships the full interpreter (front-end +
  VM + numeric/image stack) as a wasm module that evaluates arbitrary Ruby in the
  page (see [Run in the browser](#run-in-the-browser--webassembly)); and
  **`rbgo build --closed --target wasm app.rb`** cross-compiles a closed-world
  wasm module that runs *that one program* (no front-end linked) and can drive
  the page's DOM/Canvas through the built-in `JS` module
  (`JS.document`/`JS.log`/`JS::Ref#call`/`JS.raf`).

**100% coverage** is enforced in CI across all six 64-bit targets (amd64, arm64,
riscv64, loong64, ppc64le, s390x) and three OSes. Phase 8 (conformance and
representation/perf tuning) is well advanced: the conformance campaign above
brought the front-end to ~100 % parse / 99.82 % parse+compile on real-world Ruby,
and on the performance side small-integer interning and capture-tracked
frame-environment recycling have cut call-path allocations (a small-int loop from
~245k allocations to 1; recursion's call allocations halved, ~14% faster), with
the 6-runtime benchmark suite ([BENCHMARKS.md](BENCHMARKS.md)) tracking rbgo vs
MRI / YJIT / JRuby / TruffleRuby. The road from "parses + compiles" to "runs whole
applications" — the runtime stdlib + C-extension surface — is now well underway:
**Puppet boots and evaluates manifests** (see *Running Puppet* above), with full
`puppet apply` the active next milestone. See the
[roadmap](https://go-embedded-ruby.github.io/docs/roadmap/).

## Quick start

Requires **Go 1.26.4+**.

```bash
# run a one-liner
go run ./cmd/rbgo run -e 'puts 1 + 2'        # => 3

# run a file
cat > fib.rb <<'RB'
def fib(n)
  if n < 2
    n
  else
    fib(n - 1) + fib(n - 2)
  end
end
puts fib(20)
RB
go run ./cmd/rbgo run fib.rb                  # => 6765

# build the CLI
go build -o rbgo ./cmd/rbgo
./rbgo fib.rb

# AOT-compile a program's methods to native code and link a specialised binary
./rbgo build -o fib fib.rb                    # fib (the method) becomes native int64
./fib fib.rb                                  # => 6765

# closed-world: bake the program in as bytecode and drop the front-end
./rbgo build --closed -o fib fib.rb           # no lexer/parser/compiler linked
./fib                                          # => 6765  (runs with no source file)

# WebAssembly: cross-compile a closed-world program to a browser wasm module
./rbgo build --closed --target wasm -o app.wasm app.rb   # GOOS=js GOARCH=wasm
```

## Run in the browser — WebAssembly

**`GOOS=js GOARCH=wasm` is a first-class target.** Everything is pure Go with cgo
disabled, so the interpreter, the numeric stack and the cgo-free image pipeline
compile to a single wasm module and run **entirely in the browser** — there is no
server-side code. There are two ways to ship Ruby to the browser:

**1. The playground — full interpreter in wasm.** A self-contained page (Ruby
REPL + a load→`gaussian_blur`/`sobel`/`canny`→render image demo) lives in
[`web/`](web). It builds `cmd/wasm`, which links the whole front-end (lexer,
parser, compiler) and VM, so the page can evaluate *arbitrary* Ruby typed by the
user:

```bash
./web/build.sh serve        # build web/rbgo.wasm and serve http://localhost:8080
```

The module publishes `rbgoEval(src)` and `rbgoImage(src, bytes)` on the JS global
object; see [web/README.md](web/README.md) for the bridge.

**2. `rbgo build --target wasm` — a closed-world wasm app.** To ship *one* Ruby
program (not a REPL), AOT-bake it into a closed-world wasm module that drops the
front-end:

```bash
./rbgo build --closed --target wasm -o app.wasm app.rb
```

`--target wasm` requires `--closed` (the wasm entry runs the embedded program,
then parks the Go runtime with `select{}` so JS callbacks keep firing). The
program can reach the page through the built-in **`JS` module** — `JS.global`,
`JS.window`, `JS.document`, `JS.log`, `JS::Ref#get`/`set`/`call`/`[]`/`on` for
DOM and Canvas, and `JS.raf { |t| … }` for an animation loop — so a closed-world
wasm app can render and handle events with no JavaScript of its own. Serve the
emitted `app.wasm` next to Go's `wasm_exec.js` loader.

## Layout

```
cmd/rbgo/            CLI: run, build (+ build --closed [--target wasm]; repl later)
cmd/wasm/            GOOS=js GOARCH=wasm playground front-end (see web/) + native stub
cmd/aotgen/          regenerates the AOT differential suite (go:generate)
cmd/freeze-prelude/  regenerates the frozen prelude bytecode (go:generate)
web/                 browser playground: index.html, build.sh (rbgoEval/rbgoImage)
internal/
  compiler/          AST → bytecode (ISeq), local-slot resolution
  bytecode/          instruction set + ISeq
  vm/                stack-machine interpreter, arithmetic, builtins
                     (front-end isolated behind the rbgo_closed build tag)
  aot/               AOT compiler: bytecode → Go (level-1/3 kernels, FreezeISeq)
  object/            Value interface + concrete value types
docs/                plan-rbgo.md (the roadmap), aot-compiler.md
```

## Testing & conformance

```bash
go test ./...
go test -coverpkg=./internal/... -coverprofile=cov.out ./internal/...
go tool cover -func=cov.out | tail -1
```

If a parent `go.work` is present, prefix commands with `GOWORK=off`.

Correctness is judged against **independent reference implementations** of
Ruby 4.0 — **MRI (CRuby) 4.0.5** and **JRuby**, with **TruffleRuby** being added
as a third reference (conformance + performance). The differential oracle
[`scripts/oracle.sh`](scripts/oracle.sh) runs a snippet through rbgo, MRI and
JRuby and flags any divergence:

```bash
scripts/oracle.sh -e 'p (1..10).select(&:even?).map { |x| x**2 }'
```

Beyond synthetic tests, the bar is **real-world Ruby**: idioms and test suites
from reference applications — **Ruby on Rails** (ActiveSupport's pure-Ruby
`core_ext`) and **OpenVox**/Puppet (Ruby-heavy manifest evaluation) — drive the
remaining work by demand and double as conformance corpora and performance
baselines (pure-Go CGO=0 vs CRuby's C, JRuby's JVM JIT and TruffleRuby's Graal).
The heavyweight front-end (parse + compile) conformance results — Rails 99.82 %,
Puppet 100 %, ~100 % of all valid Ruby parsed, plus the per-library table — are
in [CONFORMANCE-RAILS-PUPPET.md](CONFORMANCE-RAILS-PUPPET.md),
[CONFORMANCE-LIBRARIES.md](CONFORMANCE-LIBRARIES.md) and
[CONFORMANCE-RSPEC.md](CONFORMANCE-RSPEC.md); reproduce with
`scripts/conformance/heavyweight/sweep.sh`.

## Design & roadmap

See **[docs/plan-rbgo.md](docs/plan-rbgo.md)** for the full architecture, the
9-phase plan (Phase 0 vertical slice → Phase 8 conformance & performance), the
risk register, and the decision journal. The regexp engine is developed
separately as a pure-Go reimplementation of Onigmo in
[go-ruby-regexp/regexp](https://github.com/go-ruby-regexp/regexp).

## License

BSD-3-Clause. See [LICENSE](LICENSE).
