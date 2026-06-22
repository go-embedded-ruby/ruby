<p align="center"><img src="https://raw.githubusercontent.com/go-embedded-ruby/brand/main/social/go-embedded-ruby.png" alt="go-embedded-ruby/ruby" width="720"></p>

# ruby — go-embedded-ruby

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-9B1C2E)](https://go-embedded-ruby.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Phase](https://img.shields.io/badge/phases-1--8%20active-1a7f37)](https://go-embedded-ruby.github.io/docs/roadmap/)

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

Supported today (every feature **differential-tested against MRI Ruby 4.0.5**):

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
  `block_given?`, `&block` capture, **numbered params (`_1`/`_2`) and `it`**,
  `Proc`/`lambda`/**stabby `->(){}`**, `&proc` block-pass and `Symbol#to_proc`
  (the `&:sym` shorthand).
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
  `%`/`format`/`sprintf`, case/strip/`split`/`each_char`/`lines` and friends.
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
- **Collections:** Array / Hash / Range with `Enumerable` (map/select/reduce/…)
  and `Comparable`, both written once in embedded Ruby; Array **bang methods**
  (`map!`/`sort!`/`select!`/`reject!`/`compact!`/`uniq!`/`reverse!`);
  **`Range#step`/`Integer#step`** (integer and float walks, both directions).
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
  `instance_variable_get`/`set`.
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

**100% coverage** is enforced in CI across all six 64-bit targets (amd64, arm64,
riscv64, loong64, ppc64le, s390x) and three OSes. Phase 8 (conformance and
representation/perf tuning) is now under way: small-integer interning and
capture-tracked frame-environment recycling have cut call-path allocations (a
small-int loop from ~245k allocations to 1; recursion's call allocations halved,
~14% faster). See the [roadmap](https://go-embedded-ruby.github.io/docs/roadmap/).

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
```

### Browser playground (WebAssembly)

The interpreter, the numeric stack and the cgo-free image pipeline also compile
to `GOOS=js GOARCH=wasm` and run **entirely in the browser** — no server-side
code. A self-contained playground (Ruby REPL + a load→`gaussian_blur`/`sobel`/
`canny`→render image demo) lives in [`web/`](web):

```bash
./web/build.sh serve        # build web/rbgo.wasm and serve http://localhost:8080
```

See [web/README.md](web/README.md) for the JS bridge (`rbgoEval`, `rbgoImage`).

## Layout

```
cmd/rbgo/            CLI: run, build (+ build --closed; repl arrives later)
cmd/wasm/            GOOS=js GOARCH=wasm front-end (see web/) + native build stub
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

## Testing

```bash
go test ./...
go test -coverpkg=./internal/... -coverprofile=cov.out ./internal/...
go tool cover -func=cov.out | tail -1
```

If a parent `go.work` is present, prefix commands with `GOWORK=off`.

## Design & roadmap

See **[docs/plan-rbgo.md](docs/plan-rbgo.md)** for the full architecture, the
9-phase plan (Phase 0 vertical slice → Phase 8 conformance & performance), the
risk register, and the decision journal. The regexp engine is developed
separately as a pure-Go reimplementation of Onigmo in
[go-ruby-regexp/regexp](https://github.com/go-ruby-regexp/regexp).

## License

BSD-3-Clause. See [LICENSE](LICENSE).
