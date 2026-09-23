<p align="center"><img src="https://raw.githubusercontent.com/go-embedded-ruby/brand/main/social/go-embedded-ruby.png" alt="go-embedded-ruby/ruby" width="720"></p>

# ruby — go-embedded-ruby

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-9B1C2E)](https://go-embedded-ruby.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Phase](https://img.shields.io/badge/phases-6%20%2B%208%20in%20progress-1a7f37)](https://go-embedded-ruby.github.io/docs/roadmap/)

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

### Runtime conformance — ruby/spec (measured 2026-09-23)

Since July the work has been **runtime** conformance: making the VM behave as
Ruby does, measured against [ruby/spec](https://github.com/ruby/spec)'s
`language/` and `core/` suites. Measured with
`scripts/conformance/rubyspec/run.sh` on darwin/arm64 against the pinned corpus
at `SPEC_SHA=87b1631992bd00cf0c4934474766d54dad088191`, on `d498ddc` — the tip
of `main` at the time, including
[#629](https://github.com/go-embedded-ruby/ruby/pull/629). `main` has since
advanced to `2200e17`, whose only change is the `FLOOR` file
([#638](https://github.com/go-embedded-ruby/ruby/pull/638)); the interpreter
built from it is byte-identical, so these figures stand unchanged.

| | |
| --- | --- |
| **passing examples** | **21,982** — four consecutive runs, all four identical |
| fail / error | 1,321 / 740 |
| skipped | 473 |
| pass rate of examples that ran | **91.4 %** (21,983 of 24,044) |
| spec files | 2,191 of 2,206 produce a result; 15 produce none |
| **CI floor** (`FLOOR`) | **21,970** |

(The ratchet reports 21,982; a separate sweep capturing `fail`/`error`/`skip` as
well read 21,983. The one-example difference is the `#615` noise below.)

**The floor is not the score, and the two are close on purpose.** `FLOOR` says
*"no run may come in below this"* — a shrink-only ratchet, raised in its own PR
after a wave lands. The measured total sits **above** it, but only just: 21,982
against 21,970 is a margin of **12**. That is deliberate. The floor is set a
handful of examples below the **lowest observed run** — enough to absorb the
run-to-run jitter of [#615](https://github.com/go-embedded-ruby/ruby/issues/615),
and no more, because a floor slack enough to hide the loss of a whole spec file
would defeat the thing the ratchet exists to catch. So a small gap is the ratchet
working; a large one would mean it had gone slack.

They remain different claims. The floor is the **guarantee CI enforces**; the
measured total is **what rbgo does** on a given run. Quoting the floor as the
conformance figure understates rbgo by the margin; quoting the measured total as
a guarantee overstates it by the same amount.

**Run-to-run spread is possible and is a known bug, not noise in the method.**
`core/module/autoload_spec.rb` crashes intermittently while popping a frame
([#615](https://github.com/go-embedded-ruby/ruby/issues/615)), and the whole
file's examples are lost when it does — an earlier set of four runs on `68cb53a`
spread 21,845–21,903 for exactly that reason. The four runs above happened not to
hit it; the issue is open, so run it more than once and take the *low* run as the
guaranteed figure.

> **Not every gain is VM conformance.** Two of the recent jumps came from fixing
> the **measurement**, not the interpreter, and it is worth keeping them apart:
>
> - **The mspec shim** ([#624](https://github.com/go-embedded-ruby/ruby/pull/624),
>   refs [#621](https://github.com/go-embedded-ruby/ruby/issues/621)) ran each
>   example under `instance_eval`, so a helper defined in an outer `describe` was
>   unreachable from a nested one. `core/string/valid_encoding/utf_8_spec.rb`
>   scored **0 of 28 — and 0 of 28 under MRI 4.0.5 too**, through the same shim.
>   Fixing the shim moved the corpus 20,905 → 20,936, of which **28 are
>   attributable**.
> - **The parser upgrade to v0.2.0**
>   ([#625](https://github.com/go-embedded-ruby/ruby/pull/625)) made **13
>   `language/*_spec.rb` files parseable that had never parsed at all**. They had
>   been contributing zero to *both* columns — not failing, invisible. `language/`
>   went 1,443 → 1,776 passing (+333), while `fail+error` rose 283 → 436, because
>   those files brought their own failures with them.
>
> Both are gains in what the measurement can **see**, not in what the VM can
> **do**. The rest of the climb — the floor went from **6,000** when the ratchet
> landed on 2026-08-03 ([#263](https://github.com/go-embedded-ruby/ruby/pull/263))
> to **21,970** today, across 27 conformance waves — is the VM.

There is no honest denominator for "percent of Ruby". 91.1 % is the share of the
examples *this shim actually ran*; the shim is not mspec, it stubs some matchers
(so their examples are **skipped**, not passed), and 473 skips and 15 unreadable
files sit outside the ratio entirely.

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
  safely; `spawn` is the supported way to start a child.
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

- **WebAssembly (`GOOS=js GOARCH=wasm`) is a supported target.** rbgo runs in the
  browser two ways: the **playground** ships the full interpreter (front-end +
  VM + numeric/image stack) as a wasm module that evaluates arbitrary Ruby in the
  page (see [Run in the browser](#run-in-the-browser--webassembly)); and
  **`rbgo build --closed --target wasm app.rb`** cross-compiles a closed-world
  wasm module that runs *that one program* (no front-end linked) and can drive
  the page's DOM/Canvas through the built-in `JS` module
  (`JS.document`/`JS.log`/`JS::Ref#call`/`JS.raf`).

### Not implemented — the limitations worth naming

Measured on `main` (`d498ddc`), darwin/arm64, against MRI 4.0.5 on the same host.

**rbgo carries no line map, so `__LINE__` is always `0`.** This is the single
widest gap, because three separate features are built on source positions and all
three are lost:

```ruby
# probe.rb, line 1 is the puts
puts __LINE__                      # rbgo: 0            MRI: 1
def f; warn "msg", uplevel: 0; end
f                                  # rbgo: "warning: msg"
                                   # MRI:  "probe.rb:2: warning: msg"
begin; raise "boom"; rescue => e; puts e.backtrace.first; end
                                   # rbgo: "probe.rb:0:in '<main>'"
                                   # MRI:  "probe.rb:4:in '<main>'"
p Object.const_source_location(:Comparable)   # rbgo: nil   MRI: []
```

Every backtrace frame reports line `0`; `warn(uplevel:)` emits the message with
no `file:line:` prefix; `const_source_location` answers `nil`. Nothing here is a
partial implementation to be tuned — the information is not recorded.

Otherwise, and each with an open issue that states it precisely:

| | |
| --- | --- |
| `Errno` carries all **158** of MRI's constants, but **32** of them report errno `0` on darwin where MRI has a real number (`EAUTH` 0 vs 80, `EBADRPC` 0 vs 72, `EDEVERR` 0 vs 83, …). Go's portable `syscall` package does not expose the platform-only names. Nothing else about the table disagrees: all 158 names are present under both, and the other 126 numbers match exactly | [#633](https://github.com/go-embedded-ruby/ruby/issues/633) |
| `Numeric#to_int` is **not defined**, so `Complex(2.9, 0).to_int` raises `NoMethodError` where MRI returns `2` | [#631](https://github.com/go-embedded-ruby/ruby/issues/631) |
| `File::Stat#==` answers identity: `a.==(b)` is `true` (Comparable) while `a == b` is `false` | [#632](https://github.com/go-embedded-ruby/ruby/issues/632) |
| `File#stat` cannot answer for a file unlinked while open — rbgo's streams are buffered by path and hold no descriptor | [#635](https://github.com/go-embedded-ruby/ruby/issues/635) |
| **Windows:** no text-mode newline translation — CRLF reaches the reader as written | [#610](https://github.com/go-embedded-ruby/ruby/issues/610) |
| **Windows:** `File::Stat#atime`/`#ctime` fall back to mtime; `#birthtime` raises | [#635](https://github.com/go-embedded-ruby/ruby/issues/635) |
| **Windows:** `File.realpath` does not expand 8.3 short names (`RUNNER~1`) | [#636](https://github.com/go-embedded-ruby/ruby/issues/636) |
| `core/module/autoload_spec.rb` crashes intermittently popping a frame, costing its whole file from the measurement | [#615](https://github.com/go-embedded-ruby/ruby/issues/615) |
| **`Process.fork` does not exist** — Go's runtime cannot be forked safely | — |
| `BEGIN { }` / `END { }` blocks do not parse (the one construct of 41 probed where MRI accepts and the front-end refuses) | front-end |

Two further entries are **harness**, not interpreter, and are listed here only so
that nobody reads them as VM gaps: the conformance shim's `SPEC_TMP_BASE` is
`/tmp`, a symlink on darwin, which fails 11 `realpath`/`realdirpath` examples on a
path that is correct ([#630](https://github.com/go-embedded-ruby/ruby/issues/630));
**MRI 4.0.5 fails them the same way through the same shim**. And the shim's
`instance_eval` rebinding ([#621](https://github.com/go-embedded-ruby/ruby/issues/621))
is largely fixed but not closed.

**100% coverage** is enforced in CI on the two **POSIX** lanes — ubuntu and
macOS. The full `-race` suite also runs on **windows**, but the coverage gate is
not applied there: a few POSIX-only paths (opening `/dev/null` as a character
device) are unreachable, so its measured coverage is structurally below 100 %.
Per-PR architecture cover is the two **native** lanes, `amd64` and `arm64`, which
run `go test ./...` (not the coverage gate); the four exotic 64-bit targets
(`riscv64`/`loong64`/`ppc64le`/`s390x`) are validated **off** the per-PR path —
nightly under QEMU and on scheduled real hardware. A `wasip1/wasm` lane builds and
runs the interpreter under wazero. `.github/workflows/ci.yml` is the authority.
Phase 8 (conformance and
representation/perf tuning) is well advanced. The 2026-06 campaign brought the
**front-end** to ~100 % parse / 99.82 % parse+compile on real-world Ruby; since
July the work has been **runtime** conformance, and the ruby/spec ratchet has
gone from a floor of 6,000 to 21,970 across 27 waves, measuring **21,982**
passing examples today. On the performance side small-integer interning and capture-tracked
frame-environment recycling have cut call-path allocations (a small-int loop from
~245k allocations to 1; recursion's call allocations halved, ~14% faster), with
the 6-runtime benchmark suite ([BENCHMARKS.md](BENCHMARKS.md)) tracking rbgo vs
MRI / YJIT / JRuby / TruffleRuby. The road from "parses + compiles" to "runs whole
applications" — the runtime stdlib + C-extension surface — is now well underway:
**the real `puppet apply` CLI runs end-to-end** (see *Running Puppet* above),
converging `notify`, `file` and `exec` resources and exiting cleanly, with the
broader resource providers (`package` / `service`) the active next milestone.
See the
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

> **js/wasm excludes the network/OS gem backends.** A browser has no TCP sockets
> and no cgo, so the gems whose backends open real sockets or use OS facilities
> are compiled out of the `GOOS=js GOARCH=wasm` build (they carry a
> `//go:build !(js && wasm)` tag, which also keeps ~90 MB of driver code out of
> the module). On wasm, `require` of any of them raises a clean Ruby `LoadError`
> (`cannot load such file -- kafka (not available in the wasm build)`):
> **`grpc`**, **`nats`**, **`kafka`**, **`mysql2`/`mysql`**, **`mongo`/`bson`**,
> **`arrow`**, **`parquet`**, **`openstack`**, **`sidekiq`**, **`resque`** (the
> last two are the `redis`-backed job queues). A second tier —
> **`sqlite3`**, **`sequel`**, **`activerecord`**, **`bolt`**, **`bleve`**,
> **`etcd`** — still `require`s successfully but raises when a connection/handle
> is constructed (e.g. `SQLite3::Database.new`), because their drivers do not
> build for wasm. The **native build is unchanged**: every gem is present, loads
> and conforms exactly as before. Plain Ruby, the numeric/image stack and the
> `JS` DOM/Canvas bridge are fully available in the browser.

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

### ruby/spec ratchet

Behavioural conformance against Ruby's executable spec suite is tracked as a
**shrink-only ratchet**. [`scripts/conformance/rubyspec/run.sh`](scripts/conformance/rubyspec/run.sh)
runs the ruby/spec **language + core** suites through rbgo under a minimal
MSpec-compatible shim and sums the passing examples; a per-PR CI lane fails if
that total drops below the frozen floor in
[`FLOOR`](scripts/conformance/rubyspec/FLOOR), so language + core conformance is
measured on every change and can only go up. It complements the differential
oracle below: the ratchet is the absolute floor, the oracle catches divergences
the specs don't cover.

**The floor and the measurement are two different numbers.** `FLOOR` is
**21,970** — the level CI refuses to fall below, raised there in
[#638](https://github.com/go-embedded-ruby/ruby/pull/638) after wave 27. A run
measures **21,982** passing, four consecutive runs all identical: a margin of 12,
which is the size it is meant to be (see *Runtime conformance* for why). See
*Runtime conformance* above for the full breakdown, the run-to-run spread
([#615](https://github.com/go-embedded-ruby/ruby/issues/615)) and what the ratio
does and does not mean.

```bash
scripts/conformance/rubyspec/run.sh          # measure vs the floor
UPDATE_FLOOR=1 scripts/conformance/rubyspec/run.sh   # print the new floor after a win
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
shim, and gates CI on a **shrink-only floor** of passing examples
([`scripts/conformance/rubyspec/`](scripts/conformance/rubyspec/), floor in
`FLOOR`, currently **21,970**). The floor can only be raised, so measured
language conformance moves in one direction — but the floor is a *gate*, not the
result: the measured total is **21,982**. Run it with
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
