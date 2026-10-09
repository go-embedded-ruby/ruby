<p align="center"><img src="https://raw.githubusercontent.com/go-embedded-ruby/brand/main/social/go-embedded-ruby.png" alt="go-embedded-ruby/ruby" width="720"></p>

# ruby — go-embedded-ruby

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-9B1C2E)](https://go-embedded-ruby.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.27.1%2B-00ADD8)](https://go.dev/dl/)
[![Release](https://img.shields.io/badge/release-v0.1.0-blue)](https://github.com/go-embedded-ruby/ruby/releases/tag/v0.1.0)
[![ruby/spec](https://img.shields.io/badge/ruby%2Fspec-23%2C483%20examples%20passing-1a7f37)](#runtime-conformance--rubyspec)

**A Ruby interpreter written in pure Go, with cgo disabled** — so you can embed it
in a Go program with `import "github.com/go-embedded-ruby/ruby"`, or ship it as a
single static binary that cross-compiles wherever Go does, with no C toolchain and
no libruby.

**How complete is it?** It runs **23,483** of ruby/spec's `language/` + `core/`
examples ([what that counts](#runtime-conformance--rubyspec)) — a large and growing
subset of the language, and **not** a drop-in replacement for CRuby. Read
[What does not work yet](#what-does-not-work-yet) before you depend on it; the
embedding API is currently **one function**, and the browser WebAssembly target
does not build today.

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

## Ecosystem — rbgo composes the `go-ruby-*` family

rbgo is increasingly assembled from a family of **standalone pure-Go (CGO=0),
MRI-compatible libraries** — each its own org with **100% coverage and 6-arch
CI** — that the interpreter composes and binds as native modules. The design
principle is a clean seam: a stdlib piece whose work is **pure compute and needs
no interpreter** — a regexp matcher, an ERB compiler, a YAML emitter, a Marshal
codec, an OptionParser argv engine — becomes a reusable standalone library, while
only the thin **interpreter-dependent glue** (the Ruby-object binding) stays in
rbgo. The same lever that produced the front-end ([go-ruby-parser][grp]) is now
applied across the stdlib. The benefit cuts both ways: rbgo still ships as a
**single CGO=0 static binary**, and every extracted piece is independently
reusable, tested and 6-arch by any Go program — no interpreter required.

The `go-ruby-*` family is a growing set of **standalone pure-Go modules — all
CI-green, 100% coverage, 6-arch** — each its own org. The `go.mod` currently
**binds 178 such modules across 176 orgs** into rbgo as native modules (count:
`grep -oE 'github\.com/go-ruby-[a-z0-9-]*/[a-z0-9-]*' go.mod | sort -u | wc -l`;
`go-ruby-widgets` contributes three):

| Library | Role | Org · landing |
| --- | --- | --- |
| **go-ruby-parser** | Ruby lexer / parser / AST front-end (embedded, so `eval`/`require` keep working) | [org][grp] · [site](https://go-ruby-parser.github.io/) |
| **go-ruby-regexp** | Onigmo-compatible regexp engine (incl. `\b`/`\B`, char-class literals) | [org][grr] · [site](https://go-ruby-regexp.github.io/) |
| **go-ruby-erb** | ERB template compiler | [org][gre] · [site](https://go-ruby-erb.github.io/) |
| **go-ruby-marshal** | Marshal (`dump`/`load`), byte-exact with MRI | [org][grm] · [site](https://go-ruby-marshal.github.io/) |
| **go-ruby-yaml** | Psych-compatible YAML emitter + loader | [org][gry] · [site](https://go-ruby-yaml.github.io/) |
| **go-ruby-format** | `sprintf` / `%` / `format` engine | [org][grf] · [site](https://go-ruby-format.github.io/) |
| **go-ruby-optparse** | `OptionParser` argv engine | [org][gro] · [site](https://go-ruby-optparse.github.io/) |
| **go-ruby-strscan** | `StringScanner` (`strscan`) | [org][grs] · [site](https://go-ruby-strscan.github.io/) |

The full bound family (alphabetical, all native modules in `go.mod`):
`aasm`, `abbrev`, `acme`, `actioncable`, `actionmailer`, `actionpack`,
`actionview`, `activejob`, `activemodel`, `activerecord`, `activestorage`,
`activesupport`, `addressable`, `age`, `arrow`, `async`, `augeas`, `base64`,
`bbolt`, `bcrypt`, `benchmark`, `bigdecimal`, `bleve`, `builder`, `bundler`,
`cancancan`, `capistrano`, `capybara`, `cgi`, `chronic`, `cmath`, `commonmark`,
`concurrent-ruby`, `confd`, `connection-pool`, `csv`, `date`, `deep-merge`,
`devise`, `did-you-mean`, `digest`, `dotenv`, `dry-struct`, `dry-types`,
`dry-validation`, `erb`, `erubi`, `etcd`, `excon`, `facter`, `factory-bot`,
`faker`, `faraday`, `fast-gettext`, `find`, `format`, `friendly-id`,
`getoptlong`, `grape`, `graphql`, `grpc`, `haml`, `hanami`, `hcl2`, `hiera`,
`hocon`, `http`, `httparty`, `i18n`, `images`, `ipaddr`, `irb`, `jbuilder`,
`jekyll`, `json`, `jwt`, `kafka`, `kaminari`, `kramdown`, `liquid`, `logger`,
`mail`, `marshal`, `matrix`, `mime-types`, `minitest`, `money`, `mongodb`,
`msgpack`, `multi-json`, `mustache`, `mysql`, `nats`, `net-ftp`, `net-http`,
`net-imap`, `net-pop`, `net-sftp`, `net-smtp`, `nokogiri`, `oauth2`, `observer`,
`oidc`, `omniauth`, `openbao`, `openstack`, `opentelemetry`, `opentype`,
`optparse`, `ostruct`, `pagy`, `paper-trail`, `parquet`, `parser`, `pathname`,
`pg`, `prawn`, `prettyprint`, `prime`, `protobuf`, `pstore`, `public-suffix`,
`puma`, `pundit`, `puppet`, `puppet-resource-api`, `racc`, `rack`, `rails`,
`railties`, `rake`, `ransack`, `rdoc`, `redis`, `regexp`, `reline`, `resolv`,
`resque`, `rexml`, `roda`, `rolify`, `rouge`, `rqrcode`, `rspec`, `rss`,
`rubocop`, `rubygems`, `saml`, `sass`, `scanf`, `securerandom`,
`semantic-puppet`, `sequel`, `shellwords`, `shrine`, `sidekiq`,
`simplecov`, `sinatra`, `slim`, `sodium`, `sqlite3`, `strscan`, `thor`,
`timecop`, `toml`, `tsort`, `typhoeus`, `tzinfo`, `unicode-normalize`, `uri`,
`vcr`, `warden`, `webauthn`, `webmock`, `webrick`, `widgets`, `yaml`,
`zeitwerk`, `zlib`, plus `activeldap`, `ldap` and `fast-gettext-locale` — each at
`github.com/go-ruby-<name>/<name>`.

Seven more are bound from **outside** the `go-ruby-*` naming convention, at
`github.com/go-<name>/<name>`: `commonmark`, `kramdown`, `liquid`, `mustache`,
`nokogiri`, `rouge` and `xslt`. (`set` is not bound at all — see *Set* under
*Supported today*: it is implemented in-tree.)

Beyond the `go-ruby-*` family, the scientific / container stack binds the
pure-Go [go-ndarray](https://github.com/go-ndarray/ndarray),
[go-fft](https://github.com/go-fft/fft), [go-images](https://github.com/go-images/images)
and [go-composites](https://github.com/go-composites) libraries the same way (see
*Supported today* below), and the pure-Go [go-widgets](https://github.com/go-widgets)
UI toolkit is bound through three adapters: `require "widgets"` (pixel-blitting
GUI toolkit), `require "tui"` (terminal-cell toolkit) and `require "mvvm"`
(data-binding layer) — each at `github.com/go-ruby-widgets/<name>`.

[grp]: https://github.com/go-ruby-parser
[grr]: https://github.com/go-ruby-regexp
[gre]: https://github.com/go-ruby-erb
[grm]: https://github.com/go-ruby-marshal
[gry]: https://github.com/go-ruby-yaml
[grf]: https://github.com/go-ruby-format
[gro]: https://github.com/go-ruby-optparse
[grs]: https://github.com/go-ruby-strscan

## Status

### Runtime conformance — ruby/spec

The honest summary: **rbgo runs a large and growing subset of Ruby, and is not a
drop-in replacement for CRuby.** The number below is the best evidence available,
and it is worth understanding exactly what it counts.

**What is measured.** The `language/` and `core/` suites of
[ruby/spec](https://github.com/ruby/spec) — the executable specification of the
Ruby language and its core library — run through rbgo by
`scripts/conformance/rubyspec/run.sh`. Each spec file runs in its own `rbgo`
process under a minimal MSpec-compatible shim that ships with this repo.

| | |
| --- | --- |
| **passing examples** | **23,471** |
| failing / erroring examples | 918 / 329 |
| skipped (a matcher our shim does not implement) | 491 |
| examples that actually ran (pass + fail + error) | 24,718 |
| share of those that passed | **95.0 %** |
| spec files | 2,202 of 2,206 produced a result; 4 produced none |
| **enforced by CI** | a **per-file** baseline (`BASELINE`), not a single number |

The badge and the summary above it count `BASELINE`, the per-file record CI
enforces, which moves with every merge. The table is one dated full sweep, so
the two differ by whatever has landed since — 10 examples at the time of
writing. Only the baseline is machine-checked against this file
(`TestReadmeConformanceCountMatchesTheBaseline`); the table's other rows come
from the sweep named below and are not derivable from the baseline, which
records passing counts and nothing else.

Measured 2026-09-28 on `862a9f3`, darwin/arm64, against the corpus pinned at
`SPEC_SHA=87b1631992bd00cf0c4934474766d54dad088191`, on an isolated snapshot of
it so no concurrent run could swap the shim underneath, and reached through a
**real path**: seven path-identity specs answer differently behind a symlink,
which is worth 20 examples and is itself a divergence from MRI (issue #741).
Three independent sweeps — two through `run.sh` and one tallying all four
counters — agreed on every one of the 2,206 per-file records except
`core/random/bytes_spec.rb`, which answers 8 or 9 on the same binary and is the
1 separating 23,471 from 23,472. Four files produce no result at all:
`core/kernel/exit_spec.rb`, `core/mutex/lock_spec.rb`,
`core/process/exit_spec.rb`, `core/string/unpack/carret_spec.rb`.

**What this number is *not*.** It is *not* "rbgo implements 93 % of Ruby", and
there is no honest way to turn it into a percentage of the language:

- **Only `language/` and `core/` are swept.** ruby/spec's `library/`, `optional/`
  and `security/` trees are **not run at all**. Most of what people mean by "the
  standard library" is outside this measurement. (rbgo does bind a large set of
  stdlib and gem replacements — see *Supported today* — but their conformance is
  not what this figure reports.)
- **The shim is not MSpec.** A few matchers (`be_computed_by`, `ruby_exe`, some
  `argf`/IO helpers) are stubbed, so the examples that need them are counted as
  **skipped**, not passed. That makes the total a conservative lower bound.
- **Skips are outside the ratio.** The 93.3 % divides passes by the examples that
  ran; the 477 skips and the 10 unreadable files are in neither column.
- **An "example" is not a feature.** Examples are not weighted, so a heavily
  specified method contributes far more than a rarely used one.

**What CI guarantees is per file, not a total.** `BASELINE` records the passing
count of each of the 2,206 spec files, and the run fails if any file passes
fewer than its record or stops loading. There is no margin and no single number
to quote: the gate and the measurement describe the same thing at the same
grain.

It used to be one frozen scalar, and that could not be calibrated. A spec file
that fails to load carries its whole count — the largest here is 384 examples
(`core/encoding/compatible_spec.rb`) — so the floor needed a margin wider than
the largest file, which would have made it blind to every regression under 384
out of 23,471. The last scalar floor sat 211 below the measured total, and a
drop of that size could not say whether one file had stopped loading or hundreds
of specs had regressed.

**Reproduce it yourself:**

```sh
scripts/conformance/rubyspec/run.sh     # clones the pinned corpus on first run
```
### Front-end conformance & performance (campaign final, 2026-06-27)

A major conformance + performance campaign measured rbgo's pure-Go **front-end
(parse + compile)** against the MRI 4.0.5 oracle on the two largest real-world
Ruby codebases and a suite of popular libraries. All figures are
**front-end acceptance**, not whole-application execution (see the honesty note
below). **These figures date from 2026-06-27 and have not been re-measured
since**; reproduce them with `scripts/conformance/heavyweight/sweep.sh`.

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
> and C-extension equivalents. That surface is now real enough that **the real
> `puppet apply` CLI runs end-to-end under rbgo** (see *Running Puppet* below),
> converging `notify`, `file` and `exec` resources; the broader resource
> *providers* (`package` / `service` host convergence) remain in progress. What is
> established is that **the Ruby language / front-end is essentially complete** on
> real-world code, and that the runtime is far enough along to boot a real
> application; whether any *given* application runs end-to-end remains
> application-specific work. Details:
> [CONFORMANCE-RAILS-PUPPET.md](CONFORMANCE-RAILS-PUPPET.md),
> [CONFORMANCE-LIBRARIES.md](CONFORMANCE-LIBRARIES.md),
> [CONFORMANCE-RSPEC.md](CONFORMANCE-RSPEC.md).

### Running Puppet — `puppet apply` runs end-to-end

Beyond parsing real-world Ruby, **rbgo now runs the real [Puppet](https://github.com/puppetlabs/puppet)
`puppet apply` CLI end-to-end**. `require "puppet"` **fully boots** the framework
(Puppet 8.11.0) on a pure-Go CGO=0 `rbgo` — its pure-Ruby gem dependencies
(`semantic_puppet`, `concurrent-ruby`, `deep_merge`, `fast_gettext`, `facter`,
`racc`, …) load on the `$LOAD_PATH` — and a manifest then travels the **complete**
Puppet path: the real `Puppet::Util::CommandLine` → `Puppet::Application::Apply`
entry point (a real `OptionParser`), every Puppet type + provider loaded, the
settings catalog applied (creating Puppet's config dirs on disk), and finally the
user catalog applied through the transaction / RAL. Driving the genuine CLI emits
real Puppet output and exits `0`:

```
$ rbgo run puppet_apply.rb   # ARGV = apply -e 'notify { "hello": message => "hi from rbgo cli" }'
Notice: Compiled catalog for  in environment production in 0.00 seconds
Notice: hi from rbgo cli
Notice: /Stage[main]/Main/Notify[hello]/message: defined 'message' as 'hi from rbgo cli'
```

That is the actual `notify` resource type applying through the transaction and the
Resource Abstraction Layer — not a `notice(...)` evaluator print. Reaching the CLI
added a real **`OptionParser`** (optparse), **`File::Stat` / `FileTest` + on-disk
filesystem operations**, and a batch of deep Ruby fixes (`class_eval` lexical
scope, `return` inside `define_method`, class-method `super`, `String#chomp(sep)`,
…).

Three resource types now converge end-to-end through the transaction / RAL:
**`notify`**, **`file`** (the catalog creates / manages a real file on disk), and
**`exec`** — which runs its command through pure-Go process execution, honouring
the `onlyif` / `unless` / `creates` / `path` guards (so an already-converged
`exec` is correctly skipped). `puppet apply` exits cleanly, the YAML run
report / state round-tripping through the pure-Go Psych emitter / loader.

Reaching this exercised a large slice of the runtime, each gap reduced to a
minimal rbgo-vs-MRI repro before fixing:

- **Language / VM conformance:** `autoload`, frame-based `Exception#backtrace` /
  `set_backtrace` / `full_message`, interpolated regexp literals, non-local block
  `return`, `Symbol#intern`, `NilClass` conversions, `Array` slice-assignment,
  `Module.new` (real anonymous module running its block as a body),
  `extend` transitivity (a module's transitively-included methods become class
  methods), setter expressions returning their RHS, nested constant namespaces,
  and method-visibility enforcement — among dozens more.
- **Pure-Go stdlib modules** added on the path to boot and run the CLI: **`ERB`**
  (template engine), **`openssl`** (real crypto, not a stub), **`net/http`**,
  **`resolv`**, **`tmpdir`**, **`Process`**, **`StringScanner`** (`strscan`),
  **`Find`**, **`getoptlong`**, **`syslog`**, **`fileutils`**, a real
  **`OptionParser`** (`optparse`), **`File::Stat` / `FileTest` + on-disk
  filesystem operations**, `objspace`, and more — each `require`-able and CGO=0.

This is the **C-extension → pure-Go shim** strategy in action: a real Ruby
application ships as a single static CGO=0 binary because the C-backed gem APIs
are backed by pure Go. Puppet validates the approach end to end — its
dependency tree is pure Ruby, so it loads as-is.

> **Frontier (honest):** what runs end-to-end is the `puppet apply` CLI converging
> **`notify`**, **`file`** and **`exec`** resources through the full pipeline
> (boot → parse → compile → transaction / RAL → output → clean exit). The next
> frontier is the **broader resource providers** — `user` / `group` are
> provider-ready, while `package` / `service` need a host package manager / systemd
> and root. So the language, front-end, boot, CLI/apply machinery and the first
> convergent providers are real today; the remaining system-state providers are
> the work in progress.

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
  `Fiber.new`/`resume`/`Fiber.yield`/`alive?`/`transfer`, plus **`Fiber#raise`**
  (raising into a suspended fiber), **`Fiber#kill`**, **fiber storage**
  (`Fiber[]`/`Fiber[]=`) and `Fiber#blocking?`).
- **Concurrency:** **`Thread`** (`new`/`start`/`join`/`value`/`status`/`current`/
  `main`/`list`/`pass`, thread-locals via `[]`/`[]=`, exception propagation on
  join), **`Mutex`** (`lock`/`unlock`/`try_lock`/`synchronize`/`owned?`) and
  **`Queue`** (blocking `push`/`pop`, `close`), on an **emulated GVL** — one Ruby
  thread runs at a time, matching MRI's memory model (race-free under the Go
  race detector). `Thread#backtrace` answers for the **current** thread;
  asking another thread for its backtrace raises `NotImplementedError`, because
  rbgo keeps one frame stack per VM rather than one per thread.
- **Processes:** **`Process`** — `spawn`/`exec` (argv and shell forms),
  the wait family (`wait`/`wait2`/`waitpid`/`waitpid2`, `Process::Status` through
  `$?`), `kill`, process groups (`getpgid`/`setpgid`/`getpgrp`), scheduling
  priorities (`getpriority`/`setpriority`) and resource limits
  (`getrlimit`/`setrlimit` with the `RLIMIT_*` constants), plus
  `pid`/`ppid`/`uid`/`gid`/`euid`/`egid`. There is **no `Process.fork`**
  (`Process.respond_to?(:fork)` is `false`) — Go's runtime cannot be forked
  safely; `spawn` is the supported way to start a child. **`Kernel#fork` is a
  different story**: `fork { ... }` *does* exist and runs the block in the same
  process — see the table under *Known divergences*, because code that forks to
  isolate or to drop privileges gets neither here.
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
  `instance_variable_get`/`set`/`defined?`, **`defined?`** over every form it
  takes — including **`defined?(super)`**, which answers `"super"` only when an
  ancestor actually defines the method — **`module_function`**, **string `eval`**
  (the embedded
  front-end compiling Ruby at runtime), **`Binding`** (`binding`,
  `Binding#eval`, `eval(str, binding)`, `local_variable_get`/`set`/`defined?`,
  `local_variables`, `receiver` — capturing a frame's locals so eval'd code
  reads and writes them), and the class/module **hooks**
  `inherited`/`included`/`prepended`/`method_added`/`extended`.
- **Runtime loading:** **`require`/`require_relative`** load, compile and run a
  `.rb` file once (relative + search-path resolution, `LoadError` on miss,
  `true`/`false` return) — the embedded front-end loading code at runtime — and
  **`autoload`**/`autoload?` on both `Object` and any `Module`, registering a
  constant that loads its file on first reference.
- **Predefined globals**, each with the setter MRI's variable table gives it
  rather than one untyped slot: the separators `$;` (`$-F`), `$,` and `$/`
  (`$-0`); `$0`/`$PROGRAM_NAME`; the match globals `$~`, `` $` ``, `$'`, `$&`,
  `$+` and `$1`–`$9`; `$!` and `$@`; `$stdout`/`$stderr`/`$stdin` and `$>`;
  `$:`/`$LOAD_PATH`/`$-I` and `$"`/`$LOADED_FEATURES`; `$?`; `$.` and `$_`;
  `$VERBOSE` (`$-v`/`$-w`) and `$DEBUG` (`$-d`) — both reading `false`, not
  `nil`, as MRI does. The read-only ones raise on assignment
  (`$LOAD_PATH = []` → `NameError`), the checked ones enforce their type
  (`$stdout = nil` → `TypeError: $stdout must have write method, NilClass
  given`), and **`trace_var`**/`untrace_var` hook assignment to a global.
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
  **CGO=0**. ReDoS is bounded and the bound is **reportable**: `Regexp.timeout=`
  (the default for every Regexp in this VM — MRI's is process-global, rbgo scopes
  it to the VM so embedding two is not a shared mutable setting) and
  `Regexp.new(src, timeout:)` (per-Regexp, overriding that default in either
  direction, as MRI does) are both enforced, and a match that exceeds its limit
  raises `Regexp::TimeoutError` rather than answering "no match" — so a Regexp
  used as a validator or a denylist fails closed.
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
- **Process-facing IO:** **`IO.popen`** (running a child and reading/writing its
  pipe, block form included), **`IO#fcntl`**, **`IO.select`**,
  **`IO#read_nonblock`**/`write_nonblock`, `IO#reopen` and
  `set_encoding`/`external_encoding`.
- **Filesystem predicates and canonical paths:** **`File.realpath`** and
  **`File.realdirpath`** (symlink and `..` resolution), and **`FileTest`** —
  **all 26** of MRI's predicates, name for name: `exist?`, `file?`,
  `directory?`, `empty?`, `zero?`, `size`, `size?`, `symlink?`, `blockdev?`,
  `chardev?`, `pipe?`, `socket?`, `identical?`, `owned?`, `grpowned?`,
  `setuid?`, `setgid?`, `sticky?`, `readable?`, `writable?`, `executable?`,
  the `*_real?` family and `world_readable?`/`world_writable?`. Differentially
  tested against MRI 4.0.5 over a temp tree of regular files, an empty file, a
  directory, a symlink, an executable, a FIFO, a missing path, `/dev/null` and
  `/dev/disk0`: **252 assertions, 0 disagreements**.
- **`Errno`:** MRI's **full 158-constant table**, with the same class-per-number
  identity — `Errno.constants.size` is 158 under both rbgo and MRI 4.0.5. See
  the limitations below for the 32 constants whose *numbers* differ on darwin.
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
  **`Enumerator::Lazy`** (`lazy`) — deferred `map`/`collect`, `select`/`filter`,
  `reject`, `filter_map`, `flat_map`/`collect_concat`, `grep`/`grep_v`, `zip`,
  `uniq`, `compact`, `with_index`, `take`/`take_while`, `drop`/`drop_while` over
  finite or **infinite** (`(1..Float::INFINITY).lazy`) sources, chained without
  materialising and pulled by `first(n)`/`each`/`to_a`/`force`.
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
  `union`(`|`)/`intersection`(`&`)/`difference`(`-`), `each`/`to_a`/`to_set`.
  Implemented **in-tree** (`internal/vm/set.go` + the prelude) as a shell over
  rbgo's own `object.Hash`, exactly as MRI's `set.rb` is Hash-backed, so members
  key by the same `#hash`/`#eql?` semantics Hash keys use.
- **Time:** Ruby's `Time` — `now`/`at`/`parse`, arithmetic (`+`/`-`/`<=>`),
  `strftime`/`strptime`, `year`/`month`/`mday`/`hour`/`min`/`sec`/`wday`, weekday
  predicates (`monday?`…`sunday?`), `utc`/`getutc`/`zone`, `to_i`/`to_f` —
  binding [go-composites/time](https://github.com/go-composites/time).
- **Date:** Ruby's `Date` — `new`/`parse`, `+`/`-`/`<<`/`>>` and
  `next_day`/`prev_day`/`next_month`/`prev_month`,
  `year`/`month`/`mday`/`wday`/`yday`/`cwday`, `leap?`, comparisons — binding
  [go-ruby-date](https://github.com/go-ruby-date/date).
- **BigDecimal:** arbitrary-precision decimal — `+ - * / **`,
  `sqrt`/`abs`/`ceil`/`floor`/`round`/`pow`, `to_f`/`to_i`, `zero?`, comparisons
  — binding
  [go-ruby-bigdecimal](https://github.com/go-ruby-bigdecimal/bigdecimal).
- **Bag:** a multiset / counter (element → multiplicity) — `add`(`<<`)/`delete`,
  `count`/`size`/`distinct`/`most_common`, `union`/`difference`/`intersection`,
  `include?`/`each`/`to_a` — binding
  [go-composites/bag](https://github.com/go-composites/bag).

Beyond the stdlib, rbgo binds a set of **native capability modules** — each
`require`-able and backed by a canonical pure-Go `go-ruby-*` library, so the
build stays **CGO=0**. These wrap real I/O / network / crypto engines, so their
throughput tracks the underlying driver rather than the interpreter:

- **Databases & key-value stores:** **`mysql2`** (`require "mysql2"` — `Mysql2::Client`,
  `Result` and prepared `Statement`, over
  [go-ruby-mysql](https://github.com/go-ruby-mysql/mysql), a pure-Go mysql2 over
  `go-sql-driver/mysql`); **`mongo`** + **`bson`** (`require "mongo"` — `Mongo::Client`/
  `Database`/`Collection`/`Cursor` with ordered `BSON` documents and `BSON::ObjectId`,
  over [go-ruby-mongodb](https://github.com/go-ruby-mongodb/mongodb)); **`etcd`**/
  **`etcdv3`** (`require "etcd"` — the etcd v3 KV client: get/put/txn/lease/watch/lock,
  over [go-ruby-etcd](https://github.com/go-ruby-etcd/etcd)); and **`bolt`**
  (`require "bolt"` — the embedded bbolt B+tree store: `DB`/`Tx`/`Bucket`/`Cursor`,
  files interoperable with reference bbolt tooling, over
  [go-ruby-bbolt](https://github.com/go-ruby-bbolt/bbolt) over `go.etcd.io/bbolt`).
- **Messaging:** **`nats`** (`require "nats"` — NATS publish / subscribe / request
  with headers, over [go-ruby-nats](https://github.com/go-ruby-nats/nats)); and
  **`kafka`** (`require "kafka"` — a ruby-kafka-faithful producer / consumer /
  admin client, over [go-ruby-kafka](https://github.com/go-ruby-kafka/kafka) over
  `twmb/franz-go`).
- **RPC, serialization & columnar data:** **`grpc`** (`require "grpc"` — a gRPC
  server and client stub sharing an in-process transport, so one program can be
  both peers, over [go-ruby-grpc](https://github.com/go-ruby-grpc/grpc));
  **`google/protobuf`** (`require "protobuf"` — Protocol Buffers with
  wire-compatible `encode`/`decode`/`encode_json`, well-known types and the
  descriptor pool, over
  [go-ruby-protobuf](https://github.com/go-ruby-protobuf/protobuf)); **`arrow`**
  (`require "arrow"` — Apache Arrow `DataType`/`Field`/`Schema`/`Array`/
  `RecordBatch`/`Table`, over [go-ruby-arrow](https://github.com/go-ruby-arrow/arrow));
  and **`parquet`** (`require "parquet"` — an Arrow-backed Parquet reader / writer,
  over [go-ruby-parquet](https://github.com/go-ruby-parquet/parquet)).
- **Web & security:** **`faraday`** (`require "faraday"` — the Faraday HTTP client:
  connection / request / response / middleware, over
  [go-ruby-faraday](https://github.com/go-ruby-faraday/faraday)); **`puma`**
  (`require "puma"` — a Rack HTTP server driving `app.call` under the emulated GVL,
  over [go-ruby-puma](https://github.com/go-ruby-puma/puma)); **`graphql`**
  (`require "graphql"` — a graphql-ruby-flavoured type system + query execution
  preserving the exact `data`/`errors` shape, over
  [go-ruby-graphql](https://github.com/go-ruby-graphql/graphql)); **`saml`**/
  **`ruby-saml`** (`require "saml"` — SAML SSO auth requests / responses / metadata /
  logout, exposed as `SAML` and `OneLogin::RubySaml`, over
  [go-ruby-saml](https://github.com/go-ruby-saml/saml)); **`webauthn`**
  (`require "webauthn"` — WebAuthn / FIDO2 relying-party registration and
  authentication, over [go-ruby-webauthn](https://github.com/go-ruby-webauthn/webauthn));
  **`acme`** (`require "acme"` — the ACME / Let's Encrypt `Acme::Client`
  account / order / authorization / challenge / certificate flow, over
  [go-ruby-acme](https://github.com/go-ruby-acme/acme)); **`rbnacl`**
  (`require "rbnacl"` — NaCl / libsodium `SecretBox`/`Box`, Ed25519 / Curve25519
  keys, AEAD, hashing and password hashing, over
  [go-ruby-sodium](https://github.com/go-ruby-sodium/sodium)); and **`age`**
  (`require "age"` — age file encryption with X25519 and scrypt recipients,
  interoperable with the reference `age` CLI, over
  [go-ruby-age](https://github.com/go-ruby-age/age) over `filippo.io/age`).
- **Documents & observability:** **`prawn`** (`require "prawn"` — the Prawn PDF
  document DSL, emitting well-formed PDF 1.3+, over
  [go-ruby-prawn](https://github.com/go-ruby-prawn/prawn) over `go-pdf/fpdf`);
  **`bleve`** (`require "bleve"` — full-text search: index / mapping / query /
  facets, over [go-ruby-bleve](https://github.com/go-ruby-bleve/bleve)); and
  **`opentelemetry`** (`require "opentelemetry"` — distributed tracing:
  tracer / span / W3C context and the SDK exporter seam over the OpenTelemetry Go
  SDK, over [go-ruby-opentelemetry](https://github.com/go-ruby-opentelemetry/opentelemetry)).

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

- **WebAssembly:** the **WASI** target (`GOOS=wasip1 GOARCH=wasm`) builds and runs,
  and is gated in CI. The **browser** target (`GOOS=js GOARCH=wasm`) does **not**
  currently build — the playground under `cmd/wasm`, the `JS` bridge and
  `rbgo build --closed --target wasm` are all blocked behind one compile error in
  `internal/vm`. See [WebAssembly](#webassembly) and *What does not work yet*.

### What does not work yet

Every entry below was re-checked on `fc8ca1a` (darwin/arm64, 2026-10-08) by running
it against **MRI 4.0.7 on the same host**. Entries that no longer reproduced have
been removed — two were, on that pass:

- The two paragraphs saying the **browser WebAssembly target** was broken and
  ungated. Both were fixed by #684, which closed the three missing `syscall`
  constants **and** added the gate, so this file was describing a gap that a
  green lane in the same repository had been contradicting on every pull
  request. `readme_wasm_lane_test.go` now fails if the two disagree again —
  including, as it happens, if this very note quotes the old wording back.
- *"`$stderr` is not a separate stream"*. The VM takes two writers
  (`vm.NewWithStderr`), and `rbgo -e '$stdout.print "O"; $stderr.print "E"'` now
  sends one byte to each; `warn` goes to stderr. Covered by
  `internal/vm/diagnostic_stream_test.go`.

**`Timeout.timeout` never times out.** The module and `Timeout::Error` exist and
`rescue Timeout::Error` resolves, but the block runs to completion and its value
is returned: enforcing a deadline needs pre-emptive interruption of a running
block, which the scheduler does not do yet.

```console
$ rbgo -e 'require "timeout"; Timeout.timeout(0.3) { sleep 2 }; puts "returned"'
returned                 # MRI 4.0.7: Timeout::Error after 0.3s
```

This one **fails open**, which is why it is stated here rather than left in the
table below. `Timeout.timeout` is the ordinary Ruby way to bound work that might
hang, so code relying on it for that is unprotected under rbgo — and unprotected
*silently*, because the call returns successfully. The block is also yielded
`nil` where MRI yields the limit.

**A backtick command holds the interpreter lock.** While `` `cmd` `` runs, every
other Ruby `Thread` is stopped; under MRI they keep running. `Kernel#sleep` does
yield, so this is specific to the subprocess path:

```console
$ rbgo -e 't = Thread.new { loop { $n = ($n||0)+1; sleep 0.01 } }; sleep 0.05
          b = $n; `sleep 1`; puts($n > b ? "ran" : "frozen")'
frozen                   # MRI 4.0.7: ran
```

Otherwise:

| | how it differs from MRI 4.0.5 |
| --- | --- |
| `RUBY_VERSION` | rbgo reports `"3.4.1"`; the differential oracle it is developed against is MRI 4.0.5 |
| `Numeric#to_int` | not defined — `Complex(2.9, 0).to_int` raises `NoMethodError`; MRI returns `2` |
| `File::Stat#==` | `a == b` is `false` for two stats of the same file (MRI: `true`); `a.==(b)` answers `true` |
| `File#stat` on a file unlinked while open | raises `Errno::ENOENT`; MRI answers from the open descriptor. rbgo's streams are buffered by path and hold no descriptor |
| `Errno` | all **158** of MRI's constant names are present, but **32** report errno `0` where MRI has a real number (`EAUTH`, `EBADRPC`, `EDEVERR`, …). Measured by set difference on darwin: 81 names are zero-valued under rbgo, 49 under MRI, and every MRI zero is also zero under rbgo. Go's portable `syscall` package does not expose the platform-only names |
| magic encoding comments | `# encoding: ascii-8bit` is not honoured — a literal still reports `UTF-8`, where MRI reports `ASCII-8BIT` |
| `pp` | neither `Kernel#pp` nor `require "pp"` exists (`require "prettyprint"` does work) |
| `Process.fork` | does not exist — Go's runtime cannot be forked safely |
| `Kernel#fork` | **exists, and does not fork.** `fork { ... }` runs the block **in the same process**, so the “child” mutates the parent's globals, `ENV` and working directory, and returns a synthetic pid (`100001`, counting up) while `Process.pid` is unchanged. Under MRI the block runs in a real child and the parent is untouched. A `SystemExit` raised inside the block unwinds the whole program |
| `BEGIN { }` / `END { }` | do not parse (`parse error: unexpected "{" after statement`). Everything else the front-end was known to refuse now parses — see [go-ruby-parser](https://github.com/go-ruby-parser/parser) |
| `Thread#backtrace` for another thread | raises `NotImplementedError`; rbgo keeps one frame stack per VM, not per thread |

## Platforms

`.github/workflows/ci.yml` is the authority; this is what it actually runs.

**On every pull request:**

| Lane | What runs |
| --- | --- |
| **linux, macOS, windows** | the full suite under `-race`. The **100 % coverage gate** is enforced on linux and macOS only — a few POSIX-only paths (opening `/dev/null` as a character device) are unreachable on windows, so its coverage is structurally below 100 % |
| **amd64, arm64** | `go test ./...` on native runners |
| **wasip1/wasm** | built with `CGO_ENABLED=0 GOOS=wasip1 GOARCH=wasm` and **run** under [wazero](https://wazero.io), a pure-Go runtime — verified here: `wazero run rbgo.wasm -e 'puts (1..10).sum'` prints `55` |
| **js/wasm (browser)** | `CGO_ENABLED=0 GOOS=js GOARCH=wasm go build ./...` plus `go vet` over `./cmd/wasm ./cmd/rbgo ./internal/vm` (`wasm (js, browser)` in `ci.yml`). Added with #684, which fixed the three `syscall` constants the `js` port does not define |

**Not on the pull-request path** — validated on a nightly schedule instead:
`riscv64`, `loong64`, `ppc64le` and `s390x`, under QEMU
(`arch-qemu-nightly.yml`) and natively on real hardware (`native-arch.yml`: the
GCC Compile Farm, plus an IBM LinuxONE s390x host).

Every target named above is built by a lane on the pull-request path, including
the browser one: an earlier version of this file said `GOOS=js GOARCH=wasm` had
no lane and did not build, which stopped being true at #684 and stayed in the
README afterwards.

## Try it in one command

No checkout, no toolchain beyond Go itself:

```console
$ go run github.com/go-embedded-ruby/ruby/cmd/rbgo@latest -e 'puts "hello from rbgo"; puts (1..10).sum'
hello from rbgo
55
```

(The first run compiles the interpreter, which takes a while and produces a large
binary. `@latest` now resolves to the newest **release tag** rather than the tip
of `main` — `v0.1.0` as of this writing — so pass `@main` explicitly if you want
unreleased work.)

## Quick start

Requires **Go 1.27.1+**. From a checkout:

```console
$ go build -o rbgo ./cmd/rbgo          # CGO_ENABLED=0 is the default here
$ ./rbgo -e 'puts 1 + 2'
3
```

Run a file:

```console
$ cat > fib.rb <<'RB'
def fib(n)
  n < 2 ? n : fib(n - 1) + fib(n - 2)
end
puts fib(20)
RB
$ ./rbgo fib.rb
6765
```

`rbgo build` compiles a program's lowerable methods to native Go and links a
specialised binary. **It must be run from a checkout of this repository**, because
it shells out to `go build` against this module — run elsewhere it fails with
`stat .../cmd/rbgo: directory not found`:

```console
$ ./rbgo build -o fib fib.rb
rbgo build: fib (1 method(s) AOT-compiled: Object#fib)
$ ./fib fib.rb
6765
```

Add `--closed` to bake the program in as bytecode and drop the front-end entirely,
giving a binary that needs no source file:

```console
$ ./rbgo build --closed -o fib fib.rb
rbgo build: fib (1 method(s) AOT-compiled: Object#fib)
rbgo build: closed-world — front-end dropped (no lexer/parser/compiler linked)
rbgo build: binary size 172.5 MiB
$ ./fib
6765
```

## Embedding in a Go program

The public API is deliberately tiny — **one function**:

```go
package ruby // github.com/go-embedded-ruby/ruby

func Run(src string, out io.Writer) error
```

`Run` parses, compiles and executes `src` on a **fresh VM**, writing the program's
output to `out`. Everything else in this repository is under `internal/`, so this
is the whole embedding surface today.

```console
$ go get github.com/go-embedded-ruby/ruby@v0.1.0
```

`v0.1.0` is the first tagged release; before it the module was consumable only by
pseudo-version. It is a `v0.x`, so the API carries no compatibility promise yet —
pin the version rather than tracking `@latest`, and read the release notes, which
list the open security issues rather than leaving them to be discovered.

```go
package main

import (
	"bytes"
	"fmt"

	"github.com/go-embedded-ruby/ruby"
)

func main() {
	var out bytes.Buffer
	if err := ruby.Run(`puts "hello from Ruby"`, &out); err != nil {
		panic(err)
	}
	fmt.Printf("%q\n", out.String()) // "hello from Ruby\n"
}
```

The contract, as measured rather than intended:

- **One writer, both streams.** There is no separate stderr. `$stdout`, `$stderr`
  and `warn` all land in `out`: running
  `$stdout.print "O"; $stderr.print "E"; warn "W"` fills the buffer with `"OEW\n"`.
  If you need the two apart, you cannot get it through this API.
- **A Ruby exception becomes a Go `error`.** Running `raise ArgumentError, "bad"`
  returns an error whose message is `ArgumentError: bad`, and writes nothing to
  `out`. A syntax error comes back the same way, with a line number:
  `parse error at line 1: ...`.
- **Every call is a fresh VM — but the same process.** The Ruby heap is not
  shared: a global, a constant and a `String` monkey-patch set in one `Run` are
  all gone in the next. **Process-global state is shared**, with the host and
  with every other `Run`: `ENV`, the working directory, open files, signal
  handlers and the filesystem. A script that runs `ENV["PATH"] = "/attacker/bin"`
  and `Dir.chdir("/tmp")` changes them *for the host Go program*, and they stay
  changed after `Run` returns — measured: a host whose `PATH` was 860 bytes and
  whose cwd was its build directory came back with a 13-byte `PATH` and a cwd of
  `/private/tmp`. Because `require_relative` resolves against that same working
  directory (next bullet), one `Run` can also write a file that a later `Run`
  loads. There is no exported way to hold a VM open, pre-load code into it, call
  a Ruby method from Go, or pass Go values in.
- **A program that exits is reported as success.** `exit`, `exit!` and `abort`
  stop the program and `Run` returns **`nil`**, so an embedder cannot tell
  “finished” from “stopped”: `abort "fatal"` writes `fatal` to `out` and reports
  no error, and `exit 3` loses the 3. Only a Ruby *exception* becomes a Go error.
  The `rbgo` CLI recovers the status through `internal/vm`, which is unreachable
  from outside, so today there is no way for an embedder to obtain it.
- **`require_relative` resolves against the process working directory**, not
  against a script, because `Run` has no file to anchor to. From a different
  working directory the same program fails with
  `LoadError: cannot load such file -- /tmp/lib`.

The VM stays alive after `Run` returns as long as something still references it —
which is what lets an event-driven embedded program keep running.

### `Run` is not a sandbox

Conformance is the goal, and MRI is not sandboxed either — so neither is this.
That is the intended behaviour and not a defect, but it is worth stating plainly,
because MRI is normally a CLI a person invokes on their own code, whereas `Run`
is a function someone links into a server. **Do not pass untrusted Ruby to it.**
Measured, through the one public function and nothing else:

- **A shell.** `system`, backticks, `%x{}`, `IO.popen` and `Process.spawn` all
  spawn real processes as the host's uid, and anything with a shell
  metacharacter goes to `/bin/sh -c`, so pipes, `;`, `$(...)`, globs and
  redirection all work.
- **The whole filesystem**, read and write, wherever the host's uid can reach:
  `File.read("/etc/passwd")` returns it, `Dir.glob("~/.ssh/*")` lists 34 entries
  here, and reads and writes outside the working directory are unrestricted.
  `File.symlink` works, so a readable path can be pointed anywhere.
- **The network.** A script opened a listening `TCPServer`, connected to it with
  `TCPSocket` and exchanged bytes, all inside one `Run`.
- **Any Ruby file on disk**: `require` and `load` take absolute paths.
- **The host's environment and working directory**, as described above.

Two things it does *not* reach, deliberately: there is **no path from Ruby to the
host's Go state** (no `RubyVM`, `ObjectSpace` is a stub, `fiddle` does not load,
a `.so` is refused, and CGO is 0), and the host's standard descriptors are **not**
exposed — `IO.for_fd(1)` raises `Errno::EBADF` where MRI writes to the real
descriptor, so a script cannot write outside the `out` you passed.

There is also no call-depth limit yet: runaway recursion overflows the **Go**
stack, which is a fatal runtime error that `recover()` cannot contain, so it
takes the host process with it. Tracked in
[#768](https://github.com/go-embedded-ruby/ruby/issues/768).

## WebAssembly

**WASI (`GOOS=wasip1 GOARCH=wasm`) works and is gated in CI.** The interpreter
builds with cgo disabled and runs under any WASI runtime:

```console
$ CGO_ENABLED=0 GOOS=wasip1 GOARCH=wasm go build -o rbgo.wasm ./cmd/rbgo
$ wazero run rbgo.wasm -e 'puts (1..10).sum'
55
```

**The browser target (`GOOS=js GOARCH=wasm`) does not currently build** — see
*What does not work yet*. The playground under `./cmd/wasm`, the `JS` bridge and
`rbgo build --closed --target wasm` are all blocked behind that compile error, so
treat the browser story as unavailable until it is fixed.

On any wasm target the gem backends that need real sockets or OS facilities are
compiled out (`grpc`, `nats`, `kafka`, `mysql2`, `mongo`/`bson`, `arrow`,
`parquet`, `openstack`, `sidekiq`, `resque`, …); `require` of one raises a clean
Ruby `LoadError` rather than failing to link.
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

### ruby/spec ratchet

Behavioural conformance against Ruby's executable spec suite is tracked as a
**shrink-only ratchet**. [`scripts/conformance/rubyspec/run.sh`](scripts/conformance/rubyspec/run.sh)
runs the ruby/spec **language + core** suites through rbgo under a minimal
MSpec-compatible shim, then judges the sweep **file by file** against
[`BASELINE`](scripts/conformance/rubyspec/BASELINE): a per-PR CI lane fails if
any spec file passes fewer examples than its recorded count, or stops loading.
So language + core conformance is measured on every change and can only go up.
It complements the differential oracle below: the ratchet is the absolute floor,
the oracle catches divergences the specs don't cover.

**Judged per file, so the gate and the measurement are the same number.** Each
of the 2,206 files is compared with its own record, which needs no margin and
names whatever moved. A single frozen total could not do that: the largest spec
file here carries 384 examples, so a scalar floor safe against one file failing
to load would have been blind to every regression under 384 out of 23,471 — and
a drop could not say whether one file had stopped loading or hundreds of specs
had regressed.

```bash
scripts/conformance/rubyspec/run.sh          # judge against the baseline
UPDATE_BASELINE=1 scripts/conformance/rubyspec/run.sh   # re-record after a win
# point it at an existing corpus instead of cloning:
SPECDIR=/path/to/ruby-spec CACHE=/path/to/ruby-spec scripts/conformance/rubyspec/run.sh
```

### CI layering

Per-PR CI is a **fast native gate**: the `-race` + 100 %-coverage suite on
ubuntu/macos/windows plus native `amd64` and `arm64` lanes. Because rbgo is
pure-Go (CGO=0, no assembly), the four exotic 64-bit targets
(`riscv64`/`loong64`/`ppc64le`/`s390x`) are validated **off the per-PR critical
path** — nightly under QEMU, and on **real hardware via the GCC Compile Farm**
(ppc64le/riscv64/loong64) plus a LinuxONE `s390x` host. The real-hardware
workflow is scheduled + secret-gated (inert until credentials are added) to
respect the cfarm acceptable-use policy. See **[docs/CI.md](docs/CI.md)**.

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

On top of the front-end sweeps, a **ruby/spec ratchet** runs the `language/` and
`core/` suites of [ruby/spec](https://github.com/ruby/spec) — the executable
specification of the language — through `rbgo` under a minimal MSpec-compatible
shim, and gates CI on a **per-file** baseline
([`scripts/conformance/rubyspec/`](scripts/conformance/rubyspec/), in
`BASELINE`). No file may pass fewer examples than its record or stop loading, so
measured language conformance moves in one direction; the total, **23,483**, is
a derived summary rather than the thing gated. Run it with
`scripts/conformance/rubyspec/run.sh`, and see *Runtime conformance* under
*Status* for the full breakdown.

## Design & roadmap

See **[docs/plan-rbgo.md](docs/plan-rbgo.md)** for the full architecture, the
9-phase plan (Phase 0 vertical slice → Phase 8 conformance & performance), the
risk register, and the decision journal. The regexp engine is developed
separately as a pure-Go reimplementation of Onigmo in
[go-ruby-regexp/regexp](https://github.com/go-ruby-regexp/regexp).

## License

BSD-3-Clause. See [LICENSE](LICENSE).
