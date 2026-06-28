# Heavyweight parse-conformance: Rails & Puppet  (final, 2026-06-27)

Confronting **go-embedded-ruby**'s pure-Go front-end (`parser.Parse` →
`compiler.Compile`, no execution) with the two largest reference Ruby codebases —
**Ruby on Rails** and **Puppet** — as a conformance stress test.

**Headline (final):** rbgo **parses 99.96 %** of the Rails + Puppet corpus
(5577 / 5579 .rb files — the only 2 misses are intentional syntax-error fixtures
MRI itself rejects, i.e. **100 % of all valid Ruby**). End-to-end
(**parse + compile**): **Rails 99.82 %** (3417 / 3423), **Puppet 100.00 %**
(2154 / 2154). **0 over-permissive.** This is *front-end* acceptance, not
"rbgo runs Rails / Puppet" — see the honesty note under Results.

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

- rbgo built from this repo (`GOWORK=off go build ./cmd/rbgo`), against the
  final go-ruby-parser (5 parser rounds) with the matching compiler activation
  rounds in this repo (5 rbgo rounds).
- Oracle: MRI `ruby 4.0.5 (2026-05-20) +PRISM [arm64-darwin25]`.
- Repos: shallow clone of `rails/rails` and `puppetlabs/puppet` HEAD on 2026-06-25.

The front-end checker (`frontend/main.go`) runs `parser.Parse` then
`compiler.Compile` and recovers from panics, so a file that *parses + compiles*
but would fail at runtime (missing stdlib / missing method) still counts as a
front-end **success** — exactly what isolates parser/compiler gaps from runtime
gaps. It is `//go:build ignore` so it never enters the module's normal
`go build ./...` / coverage-gated test run.

## Results (final)

Two metrics, both measured against the MRI 4.0.5 oracle:

- **Parse** — `parser.Parse` accepts the file.
- **End-to-end (parse + compile)** — `parser.Parse` *and* `compiler.Compile`
  both accept the file.

### Parse acceptance

| Repo   | `.rb` files | MRI-valid | rbgo parses | **parse rate** |
|--------|------------:|----------:|------------:|---------------:|
| Rails  |       3 423 |     3 423 |       3 423 |     **100.00 %** |
| Puppet |       2 156 |     2 154 |       2 154 |     **100.00 %** |
| **Total** |    5 579 |     5 577 |       5 577 |      **99.96 %** |

The only 2 files in the corpus that rbgo does **not** parse are intentional
syntax-error test fixtures that **MRI itself rejects** (`both-reject`). So rbgo's
front-end parses **100 % of all valid Ruby** in the Rails + Puppet corpus
(5577 / 5577), and the headline 99.96 % (5577 / 5579) is against the raw file
count including those two invalid fixtures.

### End-to-end (parse + compile)

| Repo   | `.rb` files | MRI-valid | rbgo parses + compiles | **acceptance rate** | rbgo gaps |
|--------|------------:|----------:|-----------------------:|--------------------:|----------:|
| Rails  |       3 423 |     3 423 |                  3 417 |          **99.82 %** |         6 |
| Puppet |       2 154 |     2 154 |                  2 154 |         **100.00 %** |         0 |
| **Total** |    5 577 |     5 577 |                  5 571 |          **99.89 %** |         6 |

- `over-permissive` (rbgo accepts, MRI rejects): **0** across the campaign — rbgo
  never accepted Ruby that MRI rejected.
- Puppet is **fully accepted** (parse + compile): 2154 / 2154.
- The remaining 6 Rails gaps are in `compiler.Compile`, not the parser — the
  source parses but a small number of compile-time constructs are not yet lowered.

> **Honesty note.** These numbers are **front-end (parse + compile) acceptance**,
> *not* "rbgo runs Rails / Puppet." Running a full Rails or Puppet application
> additionally requires the runtime stdlib surface and C-extension equivalents,
> which is ongoing and unproven. What is established here is that **the Ruby
> language / front-end is essentially complete** on real-world code; whether any
> *given application boots* end-to-end is separate, future work.

## The journey

The campaign drove end-to-end (parse + compile) acceptance up through **10
rounds** — 5 go-ruby-parser rounds interleaved with 5 rbgo (compiler /
activation) rounds — each a find-gap → fix → measure loop against the MRI oracle.
Rails end-to-end acceptance over the campaign:

| Stage | Rails parse + compile |
| --- | ---: |
| start | 20.7 % |
| round 1 | 46.3 % |
| round 2 | 68.7 % |
| round 3 | 81.2 % |
| round 4 | 86.7 % |
| round 5 | 93.8 % |
| round 6 | 98.4 % |
| **final** | **99.82 %** |

A ~4.8× lift, with **0 over-permissive** at every stage (rbgo never accepted
Ruby that MRI rejects).

## What moved the needle

The constructs added during the campaign, roughly in order of files unblocked:

- **`::` scope-resolution in a constant *path* position** — qualified class /
  module names (`class Foo::Bar`), qualified superclass (`class E < Foo::Bar`)
  and leading top-level `::Const`. By far the largest single gap (~2 750 files,
  ~68 % of all gaps at the start); fixing it drove Rails 20.7 % → 46.3 %.
- **Paren-less command calls with arguments** — keyword / hash / multi-arg lists
  in command position (`delegate :a, to: :b`, `validates :x, presence: true`,
  `f 1, 2, 3`), with a trailing `do…end` binding to the command call as in MRI
  (~1 000 files across Rails/Puppet).
- **`class << self` / `class << obj`** (singleton class) — also fixed two latent
  VM bugs it surfaced (class-level instance variables; `attr_*` inside
  `class << self`).
- **Argument forwarding** — anonymous params (`def f(*, **, &)`) and `...`
  forwarding through calls and `super` (`def g(...); h(...); end`).
- **`%r` / `%x` / `%s` percent literals**, on top of the pre-existing
  `%w %W %i %I %q %Q`.
- **Keyword-named methods** (`def do`, `def then`, `def in`, …).
- **Compiler activations** the parser rounds exposed: block-pass after kwargs,
  anonymous `&`, block keyword params, `yield(*args)`, `rescue *classes`,
  `case … when *array`, and `*`/`**`/`...` forwarding through calls and `super`.
- **`alias` / `undef`**, masgn to any target (ivar/cvar/gvar/attr/index/constant
  + nested destructuring), special globals (`$$` etc.), shorthand hash `{x:}`,
  quoted / operator / char-literal symbols, rationals & imaginaries (`2r` / `3i`),
  unicode identifiers, `for…in…end` loops, begin-less `rescue`/`ensure`, `!~`.
- **Parser never-panics** — the front-end now returns a clean parse error rather
  than a Go panic on the handful of files that previously crashed it.

## Remaining end-to-end gaps

The 6 remaining Rails gaps are in `compiler.Compile`, not the parser (the source
parses): a small tail of compile-time constructs not yet lowered. Puppet has **0**
remaining gaps (parse + compile fully accepted).

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
| `puppet lib/puppet/util/character_encoding.rb`      | now **parses + compiles** (the `::` gap is fixed) |
| `puppet lib/puppet/coercion.rb`                     | now **parses + compiles** (the `::` gap is fixed) |
| `puppet acceptance/.../common_utils.rb`             | missing-method (`module_function`) |
| `puppet lib/puppet/concurrent/lock.rb`              | missing-constant (`RUBY_PLATFORM`) |

Takeaway: with the front-end now accepting essentially all valid Ruby in the
corpus, the next wall on *executing* a leaf file is a **runtime** concern — a
missing-stdlib gem, a missing-method on a builtin, or a missing predefined
constant (`RUBY_PLATFORM`) — separable from the parser/compiler work, and the bulk
of the remaining road to running whole applications.

## What is left

Front-end (parse + compile) is essentially complete on this corpus. The road from
here to **running** real applications is the **runtime** surface: the rest of the
stdlib, C-extension equivalents, and predefined constants. That is ongoing,
unproven work — distinct from the parse + compile acceptance reported above, which
establishes that *the language / front-end* is essentially complete, not that any
given application boots.

## Tier-0 language primitives: runtime boot progress (2026-06-27)

Following the front-end work above, a set of **Tier-0 language primitives** that
blocked `require "puppet"` at runtime were implemented (see the
`feat/tier0-language-primitives` PR): correct non-evaluating `defined?`,
`Module#module_function`, `private`/`public`/`protected` and the
`*_class_method` / `*_constant` directives, `Object#extend` on builtin-backed
receivers + `#singleton_class`, the `UnboundMethod` reflection API
(`instance_method`/`bind`/`bind_call`/`unbind`, `define_method` from a Method),
the `RUBY_*` version/platform constants, the `File::SEPARATOR` family, and
`Kernel#caller`/`#at_exit`/`#__method__` plus `Module#alias_method`.

Re-probing `require "puppet"` under a freshly built `rbgo`
(`$LOAD_PATH.unshift "<puppet>/lib"; require "puppet"`):

| | First blocker |
|---|---|
| **Baseline** (pre-Tier-0) | `NoMethodError: undefined method 'private_class_method'` — dies in the **first** required file (`puppet/version` → `puppet/concurrent/synchronized`) |
| **After Tier-0** | `NameError: uninitialized constant Gem` — clears `puppet/version`, `puppet/concurrent/synchronized` (including the `$LOAD_PATH.extend(...)` line), and `puppet/error`, reaching the `puppet/util` subsystem |

Per-subsystem `require` (baseline → Tier-0):

| Subsystem | Baseline | Tier-0 |
|-----------|----------|--------|
| `puppet/version`                | `private_class_method` missing | **OK** |
| `puppet/concurrent/synchronized`| `RUBY_PLATFORM` missing         | **OK** |
| `puppet/error`                  | OK                              | **OK** |
| `puppet/util`                   | `English` stdlib missing        | `English` stdlib missing |

The language-level blockers are cleared: the require graph advances from the
first subsystem to the second-plus, and the remaining first blockers are
**library/stdlib** concerns (`Gem`/RubyGems, the `English` stdlib) rather than
language primitives — exactly the boundary this work targeted.

## `puppet apply` runs end-to-end (2026-06-28)

Following the Tier-0 / Tier-2 runtime batches above, the remaining library/stdlib
walls were cleared and **the real `puppet apply` CLI now runs end-to-end** under a
pure-Go CGO=0 `rbgo`. `require "puppet"` fully boots the framework, and a manifest
travels the **complete** Puppet path — the genuine `Puppet::Util::CommandLine` →
`Puppet::Application::Apply` entry point (real `OptionParser`), all Puppet types +
providers loaded, the settings catalog applied (creating Puppet's config dirs on
disk), then the user catalog applied through the transaction / RAL — emitting real
Puppet output and exiting `0`.

```
$ rbgo run puppet_apply.rb   # ARGV = apply -e 'notify { "hello": message => "hi from rbgo cli" }'
Notice: Compiled catalog for  in environment production in 0.00 seconds
Notice: hi from rbgo cli
Notice: /Stage[main]/Main/Notify[hello]/message: defined 'message' as 'hi from rbgo cli'
```

This supersedes the earlier "boots, compiles, evaluates" milestone (PR #34): the
`notify` resource type now **applies** through the transaction and the Resource
Abstraction Layer, rather than a `notice(...)` evaluator print. The boot details
below remain the foundation it stands on.

### What now works

- **Boot.** With Puppet's pure-Ruby gem dependencies on the `$LOAD_PATH`
  (`semantic_puppet`, `concurrent-ruby`, `deep_merge`, `fast_gettext`, `facter`,
  `multi_json`, `racc`, `scanf`, `i18n`, `locale`), `require "puppet"` completes:

  ```
  $ rbgo run -e '%w[puppet semantic_puppet concurrent-ruby ...].each { |g|
                   $LOAD_PATH.unshift "<repos>/#{g}/lib" }
                 require "puppet"; puts "PUPPET BOOTED: #{Puppet.version}"'
  PUPPET BOOTED: version=8.11.0
  ```

- **Parse → compile → evaluate.** `Puppet::Parser::Compiler#compile` runs a
  trivial manifest through catalog setup and into the Pops evaluator, which emits
  real Puppet log output:

  ```
  notice("hi from puppet")
  # Notice: Scope(Class[main]): hi from puppet
  ```

  (Landed in PR #33 with a 100%-coverage primitive test; the evaluator path is
  also exercised via `evaluate_string`.)

### What it took

The boot and evaluation surfaced a long tail of runtime gaps, each reduced to a
minimal rbgo-vs-MRI snippet and asserted against MRI 4.0.5 before fixing
(PRs #20–#33):

- **VM / language:** `autoload`; frame-based `Exception#backtrace` /
  `set_backtrace` / `full_message` + an uncaught-exception printer; interpolated
  regexp literals; non-local block `return`; `Symbol#intern`; `NilClass`
  conversions; `Array` slice-assign; `Module.new` (anonymous module that runs its
  block as a body, includable, `class == Module`); `extend` transitivity
  (a module's transitively-included methods become class methods — this is how
  `desc` reaches a `Puppet::Type`); a setter expression returning its assigned RHS
  (a Ruby guarantee Puppet's `ThreadLocalVar#default` relies on); nested constant
  namespaces; method-visibility enforcement; `catch`/`throw` stack restore;
  Ruby Hash key semantics; `super`-in-block; `Proc.new`; `Module#constants`.
- **Pure-Go stdlib added on the boot + CLI path:** `ERB` (template engine),
  `openssl` (real crypto), `net/http`, `resolv`, `tmpdir`, `Process`,
  `StringScanner` (`strscan`), `Find`, `getoptlong`, `syslog`, `fileutils`,
  `ripper`, `objspace`, plus `Concurrent::ThreadLocalVar` honouring a lazy default
  block. Reaching the `apply` CLI added a **real `OptionParser`** (`optparse`,
  driving `Puppet::Application`'s argument handling) and **`File::Stat` /
  `FileTest` + on-disk filesystem operations** (so the settings catalog can create
  Puppet's config dirs).
- **Apply-path VM fixes (PRs #36–#37):** `class_eval` lexical scope, `return`
  inside `define_method`, class-method `super`, `String#chomp(sep)`, and more —
  each reduced to a minimal rbgo-vs-MRI repro before the fix.

### The frontier (honest)

What is **done**: the real `puppet apply` CLI **runs end-to-end** — boot → parse →
compile → transaction / RAL → output — applying a **`notify`** resource through
the full pipeline (genuine `OptionParser`, all types + providers loaded, settings
catalog applied to disk, then the user catalog applied).

What is **in progress**: **real resource *providers*** — `file` / `package` /
`service` convergence against an arbitrary host (ordering, idempotent
convergence) — and **run-report persistence**. `notify` exercises the whole
transaction path without needing either, so the pipeline is proven; converging
arbitrary system state and persisting the run report are the active next
milestone, not yet claimed.

This is the **C-extension → pure-Go shim** strategy validated end to end: a real
Ruby application ships as one static CGO=0 binary because its C-backed gem APIs
are backed by pure Go. Puppet's dependency tree is pure Ruby, so it loads as-is
and was the ideal first validation.
