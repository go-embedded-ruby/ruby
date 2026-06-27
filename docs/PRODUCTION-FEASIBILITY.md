# Production feasibility: can `rbgo` *run* Puppet and a Rack/Rails demo?

**Date: 2026-06-27**

This is an empirical **runtime probe**, not an implementation effort. The
question is not "can `rbgo` parse + compile Puppet/Rails source" (it already does
~100%), but "what happens when you actually try to **boot and run** them?"

Every claim below is backed by an actual run of the `rbgo` binary with its raw
output. Where a blocker was tractable, it was stubbed/worked around to expose the
*next* blocker, building a ranked chain that measures the distance to "it boots".

## Environment

| Component | Version / commit |
|-----------|------------------|
| `rbgo`    | `GOWORK=off go build -o /tmp/rbgo-probe ./cmd/rbgo` (HEAD, this repo) |
| MRI (reference) | `ruby 4.0.5 (2026-05-20) +PRISM [arm64-darwin25]` |
| Puppet    | `puppetlabs/puppet` @ `e227c27` (PUPPETVERSION `8.11.0`) |
| Rack      | `rack/rack` @ `1e62232` |

Invocation is `rbgo run <file.rb>` or `rbgo run -e '<code>'` (note the `run`
sub-command; bare `rbgo -e` is not the CLI form).

---

## TL;DR verdicts

### Puppet distribution: **NOT today; medium-large but tractable effort. Pure-Ruby, no C-extension wall.**

Puppet's top-level `require "puppet"` pulls in ~20 subsystems before it even
reaches `puppet/defaults`; the full tree has **908 distinct require targets**.
Today `rbgo` stops on the **very first file** (`puppet/version`) and, once that
is worked around, marches through a chain of missing core methods, missing
constants and—decisively—**missing pure-Ruby stdlib modules** (`delegate`,
`uri`, `pathname`, `forwardable`, `logger`, `openssl`, `yaml`, …). None of the
early blockers is a C extension. Puppet itself is overwhelmingly pure Ruby, so
the wall is **breadth of stdlib + a handful of reflection primitives**, not a
structural CGO=0 impossibility.

### Rack / Rails demo: **NOT today. One structural blocker dominates: there is no networking in the runtime.**

`rbgo` has **no socket layer at all** — `TCPServer`, `Socket`, `Net::HTTP`,
`webrick` are all absent, and there is no `net.Listen`/`"net"` import anywhere in
the Go runtime. A hand-rolled `TCPServer` "hello" server therefore cannot even
bind. This is the gating item for *any* HTTP serving. The good news: it is
**implementable in pure Go (Go's `net` is CGO=0-friendly)** — it is a
missing-stdlib, *not* a C-extension blocker. Rack additionally needs the
`autoload` language feature (its whole public surface is lazy `autoload`s).
**Full Rails is not realistic in the near term** — beyond the socket gap,
ActiveRecord's real database adapters (`pg`, `mysql2`, `sqlite3`) are C
extensions, which CGO=0 `rbgo` structurally cannot load.

---

## Probe A — Puppet

### Driver

```ruby
$LOAD_PATH.unshift "/tmp/conf-repos/puppet/lib"
require "puppet"
puts "PUPPET LOADED ok"
```

### Reference behaviour (MRI 4.0.5)

For calibration: even MRI 4.0.5 cannot load this checkout of Puppet 8.11.0 —
it dies in `puppet/util/monkey_patches.rb:56` with a `FrozenError` (the checkout
predates Ruby 4.0's frozen-by-default `OpenSSL::SSL::SSLContext::DEFAULT_PARAMS`).
That is an *upstream/Puppet-vs-Ruby-4.0* incompatibility, unrelated to `rbgo`.
It does not change the `rbgo` analysis: `rbgo` never gets remotely close to that
file.

### The blocker chain (ordered, as encountered)

Each row is the **first** error `rbgo` produced; the next row is what appeared
after stubbing/working around the previous one. Walking was stopped once the
nature of the wall was unambiguous (deep inside `puppet/util`, on stdlib).

| # | Where | Error (`rbgo`) | Category | Notes |
|---|-------|----------------|----------|-------|
| 1 | `puppet/version.rb:92` | `NoMethodError: undefined method 'private_class_method'` | missing-core-method | `Module#private_class_method` / `public_class_method` |
| 2 | guards everywhere | `defined?(X)` raises `NameError` for an undefined constant instead of returning `nil` | unimplemented-language-feature | breaks every `X = … unless defined?(X)` idiom and `RUBY_VERSION` guards |
| 3 | `puppet.rb:7` | `NameError: uninitialized constant RUBY_VERSION` (also `RUBY_PLATFORM`, `RUBY_ENGINE`) | missing-constant | top-level version/platform predefined constants |
| 4 | `puppet.rb:7` | `NameError: uninitialized constant Gem` | missing-stdlib (`rubygems`) | needs at least `Gem::Version` (14 uses), `Gem::Requirement` (3), `Gem::Specification` (5) |
| 5 | `puppet.rb:11` | `TypeError: can't extend a Array` — `$LOAD_PATH.extend(Module)` | unimplemented-language-feature | `Object#extend` works on plain objects but **not on builtin-backed types** (Array, …); singleton-class machinery missing for builtins |
| 6 | `puppet/util.rb:5` | `LoadError: cannot load such file -- English` | missing-stdlib (`English`) | empty alias module; cheap |
| 7 | `puppet/util.rb:8` | `LoadError: cannot load such file -- uri` | missing-stdlib (`uri`) | real functionality (URI parse/format) |
| 8 | `puppet/util.rb` | `NoMethodError: undefined method 'module_function'` | missing-core-method | both bare and `module_function :name` forms; **load-bearing** (Puppet exposes module methods this way) |
| 9 | (consequence of 8) | `NoMethodError: undefined method 'instance_method'` | missing-core-method (reflection) | `Module#instance_method` → `UnboundMethod`; no `UnboundMethod`/`#bind`/`#unbind` |
| 10 | `puppet/util/platform.rb` | `NameError: uninitialized constant File::ALT_SEPARATOR` | missing-constant | `File::ALT_SEPARATOR`, `File::PATH_SEPARATOR` |
| 11 | `puppet/file_system/uniquefile.rb:13` | `TypeError: File is not a module` — `class … < DelegateClass(File)` | missing-stdlib (`delegate`) | `DelegateClass`/`SimpleDelegator`/`Delegator`; pure Ruby, implementable |

After working around 1–11, `rbgo` is still inside `puppet/util` (a circular
`require_relative` re-entry), with the remaining early requires all resolving to
**empty stubs** that will fail the moment their real classes are used:
`pathname`, `ostruct`, `openssl`, `logger`, `forwardable`, `tempfile`, `tmpdir`,
`yaml`, `socket`, `etc`, `ipaddr`, `strscan`, `timeout`, `monitor`, `singleton`,
`observer`, `cgi`, `erb`, `optparse`, `benchmark`, `net/http`.

### stdlib availability snapshot

Tested `require "<name>"` directly under `rbgo`:

```
PROVIDED:  json  base64  securerandom  stringio  zlib  set  time  date
           (+ digest, bigdecimal, bag, marshal per the VM's providedFeatures)
MISSING:   uri  pathname  fileutils  logger  tempfile  tmpdir  openssl  net/http
           yaml  socket  etc  ipaddr  strscan  timeout  monitor  singleton
           delegate  observer  ostruct  cgi  erb  optparse  benchmark  forwardable
           digest/md5  digest/sha1
```

### `puppet apply` entrypoint

Not reached. `puppet apply -e 'notify { "hello": }'` requires the full
`require "puppet"` boot (which fails at blocker #1) **plus** the entire
application/configurer/catalog/resource stack on top — `lib/puppet/application/apply.rb`
itself pulls `puppet/application`, `puppet/configurer`, and the profiler. There
is no point at which a manifest is evaluated today.

### Puppet C-extension check (confirmed pure Ruby)

The early boot path hits **zero** C extensions. Puppet ships essentially no
native code of its own; its native surface is borrowed transitively via stdlib
(`openssl`, `zlib`, `digest`, `etc`, `socket`) and a couple of optional gems
(`ffi`, `racc`-generated parsers are pure Ruby once generated). For the *core*
`puppet apply` path the realistic native dependencies are `openssl` (TLS for
remote, **not** needed for local `apply`), `zlib`/`digest` (already provided),
and `etc` (uid/gid lookups — a thin syscall shim). **Conclusion: a
single-static-binary `puppet apply` is not blocked by any C-extension wall** —
it is blocked by stdlib breadth + the reflection primitives in the chain above.

---

## Probe B — minimal Rack / WEBrick web app

### B1 — `require "webrick"`

```
$ rbgo run -e 'require "webrick"'
LoadError: cannot load such file -- webrick
```

Absent. (`webrick` is pure Ruby in MRI but is built on `socket` — see below.)

### B2 — hand-rolled `TCPServer` "hello" server

```
$ rbgo run -e 'require "socket"'
LoadError: cannot load such file -- socket
$ rbgo run -e 's = TCPServer.new("127.0.0.1", 0)'
NameError: uninitialized constant TCPServer
$ rbgo run -e 'p defined?(TCPServer); p defined?(Socket)'
NameError: uninitialized constant TCPServer   # defined? itself also raises (blocker A#2)
```

**There is no networking primitive in the runtime at all.** A grep of the Go
sources confirms it is a structural absence, not a stub:

```
$ grep -rln 'TCPServer\|net.Listen\|TCPSocket\|"net"\|Socket' internal/ cmd/
(no matches)
```

So the background-server + self-`curl` test could not even be attempted: nothing
can bind a socket. This is **the** gating blocker for any HTTP serving.

What *does* work (so the rest of a server would have substance once a socket
exists): real filesystem `File.read`/`File.write`, `Thread`/`Thread#value`, and
backtick subprocess `` `echo hi` ``. (`Kernel#system` is missing.)

### B3 — the `rack` gem

```
$ rbgo run -e '$LOAD_PATH.unshift "…/rack/lib"; require "rack"'
NoMethodError: undefined method 'autoload' for Class
```

Rack's entire public API is exposed through `Module#autoload`. With a no-op
`autoload` stub, `require "rack"` "succeeds" but every constant is hollow:

```
$ rbgo run …rackdrv.rb
RACK LOADED
USE FAIL: NameError: uninitialized constant Rack::Response
```

`autoload` is a genuine **language-runtime feature** (lazy constant resolution
that loads a file on first reference); it cannot be faithfully faked from Ruby —
confirmed:

```
$ rbgo run -e 'autoload :Foo, "…/foo_def.rb"; puts Foo.new.hi'
NoMethodError: undefined method 'autoload' for Object        # Kernel#autoload
$ rbgo run -e 'module M; end; M.autoload(:X, "…")'
NoMethodError: undefined method 'autoload' for Class         # Module#autoload
```

Even with `autoload`, Rack still needs a **server handler**, and every handler
(`Rackup::Handler::WEBrick`, `Puma`, …) needs sockets — back to B2.

### Rack/WEBrick C-extension check

`webrick`, `rack`, and Rackup's pure-Ruby handlers are **pure Ruby** — they need
`socket` (and, for HTTPS, `openssl`). `socket`/`openssl` are *stdlib* backed by
Go's `net`/`crypto/tls`, which are **pure Go (CGO=0)**. So a minimal HTTP
"hello" server is **implementable** — it is blocked by missing stdlib +
`autoload`, not by any C extension.

### Full Rails reality check (ActiveRecord / DB drivers)

Full Rails is **not realistic in the near term**, and one part of it is a *hard*
CGO=0 wall: ActiveRecord's production database adapters — `pg` (libpq),
`mysql2` (libmysqlclient), `sqlite3` (libsqlite3) — are **C extensions**. A
CGO=0 binary structurally cannot load them. A Rails demo would have to either
ship a **pure-Go DB driver re-exposed as a Ruby `socket`-speaking adapter**
(e.g. talk the Postgres wire protocol over the future pure-Go `socket`), or run
DB-less. Everything else in Rails (Action Pack, routing, ERB views) is pure Ruby
and would ride on the same `socket`/`autoload`/stdlib work as Rack.

---

## Ranked roadmap — highest leverage first

Ordering is by **how many downstream blockers each item unblocks**.

### Tier 0 — language-runtime features (small count, unblock *everything*)

1. **`defined?(Const)` → `nil` instead of `NameError`.** Pervasive: every
   `X = … unless defined?(X)` guard and version/feature probe. Tiny fix, huge
   reach. *(Puppet, Rack, almost all gems.)*
2. **`autoload` (Kernel + Module), lazy constant resolution.** Gates the entire
   Rack/Rails public surface and most large gems. *(Rack/Rails.)*
3. **`Object#extend` on builtin-backed values (Array, Hash, String, …)** +
   **`singleton_class`.** Needed at Puppet line 11 and broadly. *(Puppet.)*
4. **Reflection: `Module#instance_method` → `UnboundMethod` (`#bind`/`#unbind`),
   `alias_method`, faithful `module_function` (named form).** `module_function`
   is load-bearing in Puppet (modules expose methods this way). *(Puppet + most
   metaprogramming-heavy gems.)*

### Tier 1 — the one structural networking gap (gates ALL web serving)

5. **`socket` stdlib: `TCPServer`/`TCPSocket`/`Socket`** on Go's `net`
   (CGO=0). Without this, no HTTP app of any kind can run. Then **`webrick`**
   (pure Ruby) falls out almost for free. *(Rack, Rails, any server.)*

### Tier 2 — stdlib breadth (each unblocks a slice of the 908-require Puppet tree)

Highest-fan-out first (count = how many real downstream classes depend on it in
the early/most-used Puppet paths):

6. **`delegate`** (`DelegateClass`/`SimpleDelegator`) — blocks `puppet/util`
   itself (the 2nd subsystem). Pure Ruby, cheap.
7. **`uri`** — heavy use across HTTP/indirector/server.
8. **`pathname`** — filesystem-path object, used everywhere.
9. **`forwardable`** (`def_delegator`/`def_delegators`) — common mixin.
10. **`logger`** — Puppet logging.
11. **`openssl`** (on Go `crypto/*`/`crypto/tls`, CGO=0) — needed for HTTPS and
    cert handling; **not** needed for local `puppet apply`, so it can come after
    a DB-less / local-only milestone.
12. **`ostruct`, `tempfile`/`tmpdir`, `fileutils`, `strscan`, `timeout`,
    `monitor`, `singleton`, `observer`, `cgi`, `erb`, `optparse`, `benchmark`,
    `ipaddr`, `etc`, `net/http`, `digest/md5`, `digest/sha1`, `yaml`** — the
    long tail. Several are tiny (alias modules, thin wrappers); `yaml`,
    `net/http`, `erb` are larger.

### Tier 3 — predefined constants & misc core

13. **`RUBY_VERSION`/`RUBY_PLATFORM`/`RUBY_ENGINE`/`RUBY_PATCHLEVEL`** and
    **`File::ALT_SEPARATOR`/`File::PATH_SEPARATOR`** — trivial, but needed early.
14. **`rubygems` shim: `Gem::Version`/`Requirement`/`Specification`** — at least
    `Gem::Version` (version comparison) which Puppet uses on the first page.
15. **`freeze` actually freezing** (today `{...}.freeze` then mutating
    *succeeds*). Correctness, not a hard blocker, but Ruby 4.0 + many gems assume
    real frozen semantics.
16. **`Kernel#at_exit`, `caller`/backtraces, `ObjectSpace`, `Kernel#system`** —
    smaller correctness/diagnostic gaps surfaced during probing.

---

## Honest effort estimate

- **Minimal pure-Ruby Rack/WEBrick "hello" server, today:** *no.* Needs Tier-0
  item 2 (`autoload`) — or a non-`autoload` hand-written server — **and** Tier-1
  item 5 (`socket`). With `socket` alone + a hand-rolled `TCPServer` loop
  (skipping Rack/WEBrick), a "hello" responder is the **smallest viable web
  demo**: it depends on exactly **one** new subsystem (`socket` on Go `net`).
  This is the recommended first web milestone and is a **bounded, pure-Go**
  piece of work.
- **Full Rails:** *not realistic near-term.* Gated by `socket` + `autoload` +
  large stdlib + the **C-extension DB-adapter wall** (would require a pure-Go
  wire-protocol DB adapter exposed through Ruby).
- **`puppet apply` single static binary:** *not today, medium-large but
  tractable.* No C-extension wall for the local-apply path. The work is Tier-0
  reflection primitives + Tier-2 stdlib breadth (starting with `delegate`,
  `uri`, `pathname`, `forwardable`, `logger`). Because Puppet has 908 require
  targets, expect the long tail to dominate; a phased target ("`require "puppet"`
  completes" → "`Puppet::Pal` evaluates a trivial manifest" → "`puppet apply -e`")
  is the right decomposition.

## Structural vs. additive — the bottom line

| Blocker | Kind | CGO=0 verdict |
|---------|------|---------------|
| `socket`/`webrick` networking | missing pure-Ruby stdlib on Go `net` | **additive** — implementable |
| `openssl` (HTTPS) | stdlib on Go `crypto/tls` | **additive** — implementable |
| `autoload`, `defined?`, `extend`-on-builtins, reflection | language-runtime features | **additive** — implementable in the VM |
| stdlib breadth (`delegate`, `uri`, `pathname`, …) | missing pure-Ruby stdlib | **additive** — implementable |
| ActiveRecord DB drivers (`pg`/`mysql2`/`sqlite3`) | **C extensions** | **structural** — cannot load under CGO=0; needs a pure-Go wire-protocol adapter |

Nothing on the **Puppet** local-apply path or the **minimal Rack/WEBrick** path
is a structural C-extension blocker. The only hard CGO=0 wall encountered is
**Rails' ActiveRecord database adapters**.

---

### Reproduction

All commands above were run against `/tmp/rbgo-probe`
(`GOWORK=off go build -o /tmp/rbgo-probe ./cmd/rbgo`) with shallow clones of
Puppet and Rack under `/tmp/conf-repos`. Probe drivers and the incremental
work-around prelude live under `/tmp/conf-probe/` (not committed — scratch).
