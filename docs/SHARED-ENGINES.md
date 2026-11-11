# Shared engines: what leaves `go-ruby-*`, and what each language owes the next

go-embedded-ruby, go-embedded-julia, go-embedded-r and go-embedded-c need many of
the same capabilities: regexes, Unicode normalisation, compression, YAML, time
zones, digests, random bit generators, linear algebra, and access to files and
the network under a sandbox. Today the most complete implementations sit in
`go-ruby-*` orgs and describe themselves as "Pure-Go MRI-compatible Ruby X".
Without this document each of the other three rewrites them.

Direction agreed by the maintainer on 2026-11-10, recorded here from
[#918](https://github.com/go-embedded-ruby/ruby/issues/918) so it is reviewable
and diffable rather than living in an issue thread.

## The rule

- If an engine's reference behaviour is a **C library or a standard that several
  languages share**, the engine lives in a **neutral org named for that
  reference**. `go-ruby-X` keeps only the layer faithful to MRI: API shape,
  messages, output formatting quirks.
- **What Ruby itself defines stays in `go-ruby-*`.**
- Extraction happens **on need**, when a second language needs the engine — not
  as one sweep.

Moving a module costs: a new module path and a release; updating its importers,
rbgo included; a `// Deprecated:` notice on the old path; and carrying the CI it
had — fuzz targets, the six architectures, the promises its documentation makes.

## ⛔ Two conditions this repository attaches to any extraction

Both come out of measurement done on 2026-11-10, and both were agreed with the
go-embedded-julia session.

### 1. An extraction carries its conformance baseline, not just its tests

A module's own green CI is **not** parity with the reference implementation. Two
of the first four engines proposed for extraction are measurably behind MRI
today — rbgo against MRI 4.0.7, pinned ruby/spec corpus, the same shim for both
engines:

| module | spec files that differ | MRI | rbgo | gap |
|---|---:|---:|---:|---:|
| `library/digest` | 31 | 86 | 44 | **42** |
| `library/securerandom` | 4 | 38 | 24 | **14** |

(Both counts come from `-quiet` reports, so the two score columns cover the files
that differ rather than the module totals; the gap is exact either way.)

"Extracted, CI green" would read as parity. It is not: it is a measured
56-example deficit against the reference, and the new home should start with the
number rather than inherit an unstated gap. `scripts/conformance/rubyspec/cmd/specdelta`
and `cmd/specroll` in this repository reproduce it for any module before it is
lifted — see the README's *"What the unswept 42 % holds"*.

### 2. A deadline belongs to the engine's API, not to its callers

`Net::FTP` is the worked example. rbgo's binding called `net.Dial` with no
timeout and set no read deadline anywhere, so a peer that accepted the
connection and then said nothing held the interpreter for ever: **39 of 56
`library/net-ftp` spec files had to be killed by the clock under rbgo, and none
under MRI** ([#916](https://github.com/go-embedded-ruby/ruby/issues/916)). A
`SIGQUIT` goroutine dump put the process in `net.(*conn).Read` beneath
`go-ruby-net-ftp`'s `GetMultiline`.

Most of that fix belongs to the consumer — but an engine that reads from an
unbounded `*bufio.Reader` is what makes it possible, and **an unbounded read in
an engine four languages share is a shared denial of service**, needing no
exploit, only silence. When `net-*` is extracted, the neutral engine takes a
deadline in its own API rather than trusting every caller to impose one.

⛔ `net-ftp` is **mid-change in this repository** and is not available for
extraction until #916 settles.

## First inventory of the 178 `go-ruby-*` modules rbgo binds

| engine (today) | shared reference | other languages that need it |
|---|---|---|
| `go-ruby-regexp` (Onigmo) | Onigmo | any Go program (the fleet already recommends it for PCRE/Ruby/JS regexes); a natural neighbour of `go-regexp/pcre2` |
| `go-ruby-unicode-normalize` | Unicode UAX #15 | julia now (Base's `normalize` calls utf8proc); go-embedded-julia also has its own tables (`internal/unitab`) |
| `go-ruby-digest`, `go-ruby-securerandom` | the standard algorithms; the OS entropy source | julia SHA and Random, R `digest`; entropy belongs to the shared host/sandbox layer |
| `go-ruby-zlib` | zlib (byte-identical streams) | R (`memCompress`, `gzcon`), julia |
| `go-ruby-yaml` | libyaml (Psych, and R's `yaml` package) | R |
| `go-ruby-tzinfo` | the IANA tz database | julia, R |
| `go-ruby-csv`, `-json`, `-toml`, `-msgpack`, `-protobuf`, `-arrow`, `-parquet` | the formats | all of them, each with its own output layer; Arrow also gives notebook cells one in-memory table format across languages |
| `go-ruby-pg`, `-mysql`, `-sqlite3`, `-redis`, `-http`, `-net-*`, `-resolv`, `-ldap`… | the protocols | all of them, once each VM has a host mode |
| `go-ruby-format`, `go-ruby-scanf` | C's `printf`/`scanf` core | C, R `sprintf`, julia `Printf`, each with its own variants on top |

**Stays Ruby:** `erb`, `haml`, `slim`, `erubi`, `marshal`, `ostruct`, `zeitwerk`,
`bundler`, `rubygems`, `irb`, and the Rails, RSpec and Puppet stacks.

## Also shared, outside `go-ruby-*`

- **Linear algebra:** `go-ndarray/ndarray/linalg` (BLAS, LU, QR, Cholesky, eig,
  SVD, lstsq) becomes a standalone library that julia's `LinearAlgebra` and R's
  `solve`/`qr`/`chol` use.
- **Random:** the bit generators in `go-ndarray/ndarray/random` (MT19937, PCG64,
  Philox) become a shared library, with Xoshiro256++ and dSFMT added for julia.
  Distributions stay per language, for bit parity.
- **libm:** a bit-exact libm (openlibm for julia, the system libm for R and C).

### go-ndarray's four constraints for `linalg` and `random`

Neither is mid-change (the only open PRs are Renovate and a loong64 float32 GEMM
blocked on hardware). There is no reason against the extraction, under four
constraints:

1. **GEMM first.** `linalg` imports `ndarray/internal/kernels`. Either the
   neutral library carries the GEMM too, or the kernels move down first into
   `go-simd/floats`, where the K1 kernels already go. One implementation, in the
   lowest shared library.
2. **A LAPACK-style API.** Today `linalg` works on `*ndarray.Array`; the neutral
   library takes raw slices with strides and leading dimensions, LAPACK-style,
   and ndarray keeps a thin wrapper. Its oracle is NumPy 2.5.3 on 1,040 cases —
   that corpus moves with it, as do the fuzz, govulncheck and cross-arch jobs.
3. **Random stays bit-exact.** The bit generators match NumPy bit for bit for the
   same seed, *including the order in which the samplers consume the stream*.
   Those golden tests move with the library; distributions stay per language.
4. **No break for ndarray users.** `ndarray/linalg` and `ndarray/random` stay as
   wrappers, so import paths do not change; below v1 this is a MINOR bump.

The allocation fixes land once, in the new library: SVD, Pinv, Lstsq, Eigh, QR
and Eig allocate 90–276× per 64×64 call (go-ndarray/ndarray#104), and `Choice`
allocates per draw (#103). `Wrap` — a zero-copy array with strides over a
caller's buffer (#76) — was filed for julia's column-major arrays, and the
LAPACK-style API of point 2 serves julia's `Matrix` directly, with no copy.

## What each language needs today

### julia — first extractions

`unicode-normalize`, `digest`, `securerandom`, and Onigmo moved to `go-regexp`.
None of the four is mid-change in this repository.

**The host/sandbox layer has three modes, not two.** Stated by the
go-embedded-julia session, with go-embedded-r's correction folded in:

| mode | who uses it |
|---|---|
| (a) **sandbox, no host** | go-embedded-r's default — a private in-memory writable tree per session, empty `Env`. go-embedded-julia's is an embedded **read-only** tree of julia's own sources. A shared sandbox can offer both at once: the language's tree read-only, plus a per-session in-memory scratch. |
| (b) **host confined to a root** | `os.OpenRoot`, go-embedded-r's `FileRoot`, opt-in |
| (c) **full host** | julia needs it, because the maintainer decided go-embedded-julia must also run as a standalone program. go-embedded-r deliberately has none. |

julia's first draft is go-embedded-julia/julia#32.

**⛔ `random` depends on `digest`, so the two cannot be extracted independently.**
julia 1.12's `Random` hashes a seed with **SHA-256** before seeding
Xoshiro256++: `hash_seed` feeds the seed — an `Integer`, a `UInt32`/`UInt64`
array, or a `String` — through `SHA.SHA2_256_CTX`
(julia 1.12.6, `stdlib/Random/src/RNGs.jl:296-340`; `Random.jl:16` imports
`SHA`). So julia's bit parity for `rand` after `Random.seed!` needs SHA-256
from the shared digest engine *and* Xoshiro256++/dSFMT from the shared
bit-generator library — digest is on the critical path of a property that looks
like it belongs entirely to random.

**What stands:** extract digest with its baseline stated, **before or with**
random, since julia's seeding depends on it.

⛔ **What does not, and the measurement that settles it.** An earlier revision of
this file said the 42-example `library/digest` gap "sits underneath julia's
`rand` reproducibility". That was an overreach, caught by the go-embedded-julia
session: julia's `Random` needs only SHA-256's **output bytes**, while the gap is
measured through **Ruby's `Digest` API**, which stays in `go-ruby-digest`.
Whether the two meet is a question about *which layer* the failing examples
exercise — so it was measured rather than argued:

| where the 42 missing examples are | count |
|---|---:|
| Ruby API with no algorithm at all (`hexencode` 5, `Digest::Instance` 4, `bubblebabble` 1) | 10 |
| the *same assertion* failing identically for md5, sha1, sha256, sha384, sha512 (`#digest`, `#hexdigest`, `#equal?`, `#size`, `#update`, `.file`) | 32 |
| an algorithm-specific divergence — one hash disagreeing where the others agree | **0** |

The failing descriptions are the tell: `Digest::MD5#size | is an alias of
Digest::MD5#length` and `Digest::SHA512#size | is an alias of
Digest::SHA512#length` are the same Ruby aliasing fact asserted five times, not
five wrong hashes. **Nothing in this gap implicates the hash bytes**, so it does
not reach julia's `rand`.

It would also not have been invisible if it did: go-embedded-julia holds `rand`
to julia 1.12.6 as an oracle, so wrong SHA-256 bytes would show up as
`rand` after `Random.seed!` diverging from julia, and its `SHA` stdlib is held
to julia's SHA outputs the same way.

**The lesson for every other extraction in this file:** a module-level
conformance number is measured at the *module's* API, and an extraction moves
only part of that module. State the baseline **broken down by algorithm and by
layer — engine bytes versus the language's API** — or the number will be read as
applying to the half that moved.

### R (go-embedded-r/r v0.14.1, R 4.6.2 as the oracle)

| engine | R use | status in go-embedded-r today | ask |
|---|---|---|---|
| `go-regexp/pcre2` | `grep`/`sub`/`regexpr`… with `perl = TRUE` | refused; the default (extended) engine is `go-embedded-r/tre`, a port of TRE with R's quirks behind `RCompat` | will import it when `perl = TRUE` is implemented; no fork |
| linear algebra (`go-ndarray` linalg, once extracted) | `solve`, `qr`, `chol`, `svd`, `eigen`, `det`; BLAS for `%*%`/`crossprod` | not implemented (M3 slice 13, planned; it was waiting for a numeric library, and will wait for this extraction instead) | needs the extracted module path, and R's LAPACK semantics (pivoting of `qr(LAPACK = FALSE)` is LINPACK's dqrdc2, which R ships itself, so it may stay R-specific) |
| zlib | `memCompress`/`memDecompress`, `gzfile`, `gzcon`, `saveRDS(compress = TRUE)` | `saveRDS`/`readRDS` use Go's `compress/gzip` (and `rds` reads bzip2); `memCompress`, `gzfile`, `gzcon` deferred; `saveRDS` refuses `bzip2`/`xz` | a shared zlib with byte-identical streams would let `saveRDS` output be compared with R's byte for byte |
| tzdata | `POSIXct`/`POSIXlt`, `strptime`, `format` | done on Go's embedded IANA database (`time/tzdata`) since v0.12.0 | would move to a shared module only if it pins a newer tz release than the Go toolchain does |
| `printf`/`scanf` core | `sprintf`, `formatC`, `format`, `scan` | `sprintf`/`formatC` are Go in `eval` (oracle-checked); `scan` deferred | a shared C-`printf` core is welcome; R's layer on top (vectorisation, `%s` of numbers via 15 significant digits) stays in R's repository |
| random bit generators | `set.seed`, `runif`, `sample` (Mersenne-Twister with R's seeding scrambling, `sample.kind = "Rejection"`) | not started (stats, M4) | MT19937 from the shared library, if its state can be seeded and read back as R's `.Random.seed` |
| libm | the math group | Go's `math` today; last-bit differences with R are recorded as divergences | a bit-exact system-libm (glibc) port would remove them |
| YAML | none in base (CRAN `yaml`, M8) | — | later |
| host/sandbox layer | files, connections, `Sys.getenv`, `Sys.time`, `Sys.sleep` | done in v0.13.0: a private in-memory file system by default, `Options.FileRoot` confined with `os.OpenRoot`, `Options.Env` (nil = empty; the host's environment is never read), bounds on path length 4096, depth 256, 65 536 entries, `MaxFileBytes` 256 MiB, a symlink-swap race test | go-embedded-r would adopt the shared layer instead of keeping its own; its design and tests (`eval/conn.go`, `eval/io_test.go`) are offered as input to go-embedded-julia/julia#32 |

Nothing in go-embedded-r's current slice (raw, complex and expression vectors,
v0.15.0) depends on these engines.

## Provenance

The rule and the inventory are from #918. go-ndarray's four constraints are from
[its comment on the same issue](https://github.com/go-embedded-ruby/ruby/issues/918#issuecomment-6772634483).
R's table was written by the go-embedded-r session and verified by it against
go-embedded-r/r v0.14.1. The two conditions above are this repository's, and are
backed by the measurements the README and the docs site now carry.
