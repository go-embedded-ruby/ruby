# go-ruby-* ecosystem map

*Last reconciled: 2026-07-02.*

The `go-ruby-*` family is **53 standalone pure-Go modules — all CI-green, 100%
coverage, 6-arch** — each its own org at `github.com/go-ruby-<name>/<name>`
(the 54th org, `go-ruby-stdlib`, is the empty meta/aggregator). **40 of the 53
are currently bound into rbgo** as native modules (binding the rest is in
progress).

This map classifies every green module by **how far it stands on its own**. The
single criterion is:

> **Does the module need a Ruby runtime to be useful, or does it only
> produce/consume a Ruby-only artifact?**

- **Tier 1 — fully general Go library.** Solves a general problem with no Ruby
  runtime required; a Go program can import it and get value with zero Ruby in
  the loop. The Ruby API is just the shape rbgo happens to bind.
- **Tier 2 — general, but the value is Ruby-exact semantics.** Also useful with
  no interpreter, but the reason it exists as its own library (rather than
  Go stdlib) is that it reproduces Ruby's *exact* semantics — precision rules,
  ordering, formatting, edge cases — bit-for-bit with MRI.
- **Tier 3 — Ruby-interop-only.** Needs a Ruby evaluator to do its job, **or**
  it exists only to produce/consume a Ruby-only artifact (Ruby source, the
  Marshal wire format, a Ruby web/DSL framework, a Ruby-toolchain component).

| Tier | Count |
| --- | --- |
| Tier 1 — fully general Go library | 20 |
| Tier 2 — general, Ruby-exact semantics is the value | 12 |
| Tier 3 — Ruby-interop-only | 21 |
| **Total** | **53** |

---

## Tier 1 — fully general Go library (no Ruby runtime needed)

| Module | Rationale |
| --- | --- |
| **base64** | Base64 encode/decode — a general codec, no Ruby needed. |
| **cgi** | CGI/URL/HTML escaping + query parsing — general web plumbing. |
| **csv** | CSV reader/writer — a general data-interchange format. |
| **digest** | MD5/SHA1/SHA256/SHA512 message digests — general cryptographic hashing. |
| **find** | Recursive directory walk — a general filesystem traversal. |
| **getoptlong** | GNU-style argv option parser — general CLI plumbing. |
| **ipaddr** | IPv4/IPv6 address + CIDR arithmetic — general networking. |
| **json** | JSON generate/parse — a general, language-neutral wire format. |
| **logger** | Leveled logging with formatters/rotation — general application logging. |
| **net-http** | HTTP/HTTPS client — a general network protocol client. |
| **optparse** | `OptionParser` argv engine — general CLI parsing. |
| **pathname** | Path manipulation (join/split/relative/absolute) — general filesystem paths. |
| **resolv** | DNS resolver / record parsing — a general network protocol. |
| **securerandom** | CSPRNG bytes/hex/uuid — general secure randomness. |
| **shellwords** | POSIX shell word split/escape — general shell-command handling. |
| **syslog** | RFC syslog client — a general logging transport. |
| **tsort** | Topological sort of a dependency graph — a general graph algorithm. |
| **unicode-normalize** | Unicode NFC/NFD/NFKC/NFKD — a general Unicode algorithm. |
| **uri** | URI/URL parse/build/normalize — a general, RFC-defined format. |
| **zlib** | DEFLATE/gzip + crc32/adler32 — a general compression codec. |

## Tier 2 — general, but Ruby-exact semantics are the value

| Module | Rationale |
| --- | --- |
| **abbrev** | Computes unambiguous abbreviations of a set of strings — a general algorithm, but shaped to Ruby's `Abbrev` semantics. |
| **bigdecimal** | Arbitrary-precision decimals — general, but the point is MRI-exact rounding/precision/banker's rules. |
| **cmath** | Complex-valued math functions — general, mirroring Ruby's `CMath` branch-cut conventions. |
| **complex** | Complex-number arithmetic — general numeric type, value is Ruby-exact coercion/formatting. |
| **date** | Calendar date/`DateTime` — general, but the value is MRI-exact parsing/arithmetic/formatting. |
| **format** | `sprintf`/`%`/`format` engine — general string formatting with Ruby's exact directive set and edge cases. |
| **matrix** | Linear-algebra `Matrix`/`Vector` — general math, value is Ruby-exact API and rational-preserving results. |
| **prettyprint** | Wadler/`pp` pretty-printing algorithm — general, but tuned to Ruby's `PrettyPrint` output. |
| **prime** | Prime generation / factorization — a general number-theory library with Ruby's `Prime` API. |
| **rational** | Exact rational arithmetic — general numeric type, value is Ruby-exact reduction/coercion. |
| **scanf** | `scanf`-style parsing — general, matching Ruby's directive semantics. |
| **time** | `Time` with zones/parsing/formatting — general, value is MRI-exact `strftime`/parse behavior. |

## Tier 3 — Ruby-interop-only (needs a Ruby evaluator, or a Ruby-only artifact/toolchain)

| Module | Rationale |
| --- | --- |
| **benchmark** | Ruby `Benchmark` harness — reports timings of Ruby blocks; needs the evaluator to run them. |
| **did-you-mean** | Ruby error-hint plugin — hooks into Ruby's exception/`NameError` machinery. |
| **erb** | Compiles ERB templates **to Ruby source** — a Ruby-only artifact; eval stays in rbgo. |
| **marshal** | Ruby's `Marshal` binary wire format — a Ruby-only serialization artifact. |
| **observer** | The `Observable` mixin — a Ruby object-model construct (module included into classes). |
| **ostruct** | `OpenStruct` — a Ruby dynamic-attribute object; only meaningful with the object model. |
| **parser** | Ruby lexer/parser/AST — its whole job is consuming Ruby source; the front-end. |
| **pstore** | Transactional object store built **on Marshal** — persists Ruby objects only. |
| **racc** | LALR parser-generator that **emits Ruby source** — a Ruby toolchain component. |
| **rack** | The Ruby web-server/app interface — a Ruby framework contract; needs the runtime. |
| **rake** | Ruby build tool — Rakefiles are a Ruby DSL evaluated at runtime. |
| **regexp** | Onigmo-compatible engine — general matching, but exists to back Ruby `Regexp`/`MatchData` and the match globals inside rbgo. |
| **reline** | Line editor for `irb`/interactive Ruby — a Ruby REPL/toolchain component. |
| **rexml** | Pure-Ruby XML processor — general XML, but the artifact is the Ruby `REXML` API/DSL. |
| **rss** | RSS/Atom feed parser/maker — Ruby's `RSS` library API (a Ruby-shaped artifact over feeds). |
| **set** | `Set`/`SortedSet` — a Ruby core collection woven into the object model / Enumerable. |
| **sinatra** | Ruby web DSL — routes are a Ruby block DSL evaluated at runtime. |
| **stringio** | `StringIO` — an in-memory Ruby `IO` object; only meaningful with the IO object model. |
| **strscan** | `StringScanner` — a stateful Ruby scanner object driving the lexer/parsers. |
| **webrick** | Ruby's `WEBrick` HTTP server — a Ruby web framework/runtime component. |
| **yaml** | Psych-compatible YAML — general format, but the value/artifact is Ruby-object (`!ruby/…`) round-tripping and MRI-exact emission. |

---

## Notes on the tiering

- The line between Tier 1 and Tier 2 is **"would you reach for it in a non-Ruby
  Go program?"** Tier 1 modules (json, csv, uri, zlib, digest, …) answer *yes,
  freely*. Tier 2 modules (bigdecimal, rational, time, format, …) are equally
  importable, but you'd only prefer them over a Go-native equivalent when you
  specifically want **Ruby-identical numerics/formatting** — that Ruby-exactness
  *is* the product.
- Tier 3 splits into two shapes that share one property — **a Ruby runtime or a
  Ruby-only artifact is intrinsic:** (a) *evaluator-dependent* runtime pieces
  (benchmark, rack, rake, sinatra, webrick, set, stringio, strscan, ostruct,
  observer, did-you-mean) and (b) *Ruby-only-artifact* producers/consumers
  (erb → Ruby source, marshal → Ruby wire format, parser/racc → Ruby AST/Ruby
  source, pstore → Marshalled objects, reline → Ruby REPL, rexml/rss → Ruby-API
  documents, regexp/yaml → Ruby-semantics-defining back-ends bound into rbgo).
- **Binding status is orthogonal to tier.** 40 of the 53 are bound into rbgo
  today; the 13 not-yet-bound are `complex`, `net-http`, `racc`, `rack`, `rake`,
  `rational`, `reline`, `rss`, `sinatra`, `stringio`, `syslog`, `time`,
  `webrick` — a mix of all three tiers.
