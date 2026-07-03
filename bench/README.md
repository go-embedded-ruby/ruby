# Benchmarks

Performance is tracked **differentially against the reference implementation
(MRI)** — every benchmark program is first checked for identical output under
each runtime, then timed under four:

- **rbgo** — this interpreter (`go build -ldflags="-s -w" -o rbgo ./cmd/rbgo`)
- **rbgo+AOT** — a native binary from `rbgo build`, which compiles the program's
  lowerable methods to Go and links them in (see
  [docs/aot-compiler.md](../docs/aot-compiler.md))
- **MRI** — the reference CRuby interpreter (oracle: Ruby 4.0.5)
- **MRI+YJIT** — CRuby with its JIT enabled (`ruby --yjit`)

The interpreter aims to **at least match** the reference; the AOT path aims to
**beat YJIT** on compute-bound code (and does — see below).

## Running

```bash
go build -ldflags="-s -w" -o rbgo ./cmd/rbgo
RUBY=ruby RBGO=./rbgo bash bench/run.sh 5      # best of 5 runs each
AOT=0 RUBY=ruby RBGO=./rbgo bash bench/run.sh 5 # skip the AOT column
```

`bench/run.sh` checks output parity first (a program whose output differs is
reported and skipped), then reports the best wall-clock time of N runs (best,
not mean, to suppress scheduler noise) and the AOT/MRI and AOT/YJIT ratios. The
AOT column builds a specialised native binary per program with `rbgo build`
(needs the Go toolchain + a module checkout); set `AOT=0` to skip it.

There is also a Go micro-benchmark suite for profiling the execution loop in
isolation (parse + compile + prelude excluded):

```bash
go test ./internal/vm/ -run=NONE -bench=. -benchmem
go test ./internal/vm/ -run=NONE -bench=Fib -cpuprofile=cpu.prof   # then: go tool pprof
```

## Workloads

| File | Exercises | AOT-eligible |
| --- | --- | --- |
| `fib.rb` | recursion + method dispatch (call-bound) | yes (L3 integer kernel) |
| `loop.rb` | tight integer `while` loop in a method | yes (L3 integer kernel) |
| `dispatch.rb` | monomorphic method calls into an object | no (call-bound) |
| `alloc.rb` | short-lived object allocation + GC pressure | no |
| `proc.rb` | `Proc#call` invocation in a loop | no (proc dispatch) |
| `blocks.rb` | block iteration (`Integer#times`) | yes (L2 top level + block) |
| `array.rb` | `map`/`select`/`reduce` pipeline | yes (L2 driver + native container kernels) |
| `hash.rb` | Hash insertion + lookup | yes (L2 top level + value-key fast path) |
| `strings.rb` | string interpolation + `join` | yes (L2 top level + block) |
| `wordcount.rb` | split + hash counting + sum (mixed) | yes (L2 top level + block) |
| `mandelbrot.rb` | benchmarks-game float kernel (compute-bound) | not yet (float) |

The formalized parity report — methodology, the full rbgo / MRI / MRI+YJIT
table, root-cause analysis and action items — lives in
[`../BENCHMARKS.md`](../BENCHMARKS.md).

## Current results (Apple M-series, Ruby 4.0.5, best of 5, 2026-07-03)

> **2026-07-03 — native container kernels landed.** The `array`/`hash`/`wordcount`
> rows below are *after* replacing their hot container paths with native kernels
> (see the analysis section): a value-key fast path in `Hash#[]`/`#[]=` and native
> `Array#select`/`#reduce`. Interpreter before→after, this host: **array 0.60→0.26s
> (2.3× faster)**, **hash 0.40→0.34s**; and the AOT binary — which calls the same
> runtime methods — improved with them: **array AOT/MRI 5.11×→1.00× — now at MRI
> parity** (0.98× at N=20), **hash AOT/MRI 3.00×→2.33×**, **wordcount 1.50×→1.33×**.
> All output stays byte-identical to MRI (`go test ./...` green with `-race`, TZ=UTC).

| Benchmark | rbgo | rbgo+AOT | MRI | MRI+YJIT | AOT/MRI | AOT/YJIT |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| **array** | 0.26s | **0.09s** | 0.09s | 0.06s | **1.00×** | 1.50× |
| **blocks** | 0.90s | **0.23s** | 0.25s | 0.22s | **0.92×** | **1.05×** |
| **fib** | 3.29s | **0.03s** | 0.48s | 0.10s | **0.06×** | **0.30×** |
| hash | 0.34s | 0.21s | 0.09s | 0.09s | 2.33× | 2.33× |
| **loop** | 1.77s | **0.02s** | 0.36s | 0.36s | **0.06×** | **0.06×** |
| strings | 0.05s | 0.05s | 0.04s | 0.04s | 1.25× | 1.25× |
| wordcount | 0.16s | 0.12s | 0.09s | 0.08s | 1.33× | 1.50× |

The two method-based integer workloads (`fib`, `loop`) compile to unboxed
`int64` kernels (level 3): **`fib` beats MRI ~16× and YJIT ~3×; `loop` beats both
~18×.**

The other rows are *interpreter-bound* — their top-level/block code defines no
hot method, so levels 1/3 never touched them and they ran at the interpreter
floor (`rbgo+AOT ≈ rbgo`). **Level 2 lowers that top-level + block code to Go**
(see [docs/aot-compiler.md](../docs/aot-compiler.md)), and the effect splits by
where each row spends its time (before → after this same machine):

- **`blocks` 3.6× → 0.9× MRI** (0.89s → 0.23s): its hot work is `t += i`,
  arithmetic in the block body, which Level 2 fully compiles — it now **beats the
  MRI interpreter and matches YJIT**.
- **`hash` 4.4× → 3.0× → 2.3×** (0.40 → 0.27 → 0.21s), **`array` 6.7× → 5.1× →
  1.0×** (0.60 → 0.46 → 0.09s, **now at MRI parity**), **`wordcount` 2.0× → 1.5× →
  1.3×** (0.16 → 0.12s): Level 2 first removed the driver/dispatch overhead
  (each ~25–33 % faster), but the rows then sat near the floor because their hot
  cost was the **native runtime methods themselves**. The 2026-07-03 container
  kernels close most of that remaining gap (see the analysis section below):
  `Array#select`/`#reduce`, previously routed through `Enumerable` (a splat-array
  allocation + a second block dispatch *per element* via `__each_packed`), are now
  native single-pass loops, and `Hash#[]`/`#[]=` short-circuit an Integer/Symbol
  key to its comparable value instead of resolving a `#hash` method per operation.
  The array pipeline (`map`/`select`/`reduce`) now runs **at MRI's C speed**; hash
  and wordcount narrow further but keep a residual gap (below).
- **`strings`** is unchanged: at 0.05s it is dominated by process start, so
  lowering its `<<` block body is in the noise.

The AOT-before column equalled `rbgo` for every interpreter-bound row (nothing
was lowered); AOT-after is the Level-2 binary, built and timed identically.

rbgo also starts faster than MRI (~0 vs ~30 ms: a single static binary with no
gem/`$LOAD_PATH` scan), which is why string/IO-bound scripts already match.

### Native container kernels (2026-07-03)

Profiling the `array`/`hash`/`wordcount` benches after Level 2 showed the residual
time was *inside* the container methods, not in dispatch — two specific costs:

- **`Hash#[]` / `#[]=` resolved a `#hash` method per operation.** Every key was
  routed through `object.hashKey`, whose fall-through called the VM's
  `CustomKeyHook` (for user objects with a Ruby-level `#hash`/`#eql?`), which ran
  a full `findMethod("hash")` ancestor walk *on every Integer key* only to
  discover it inherits the default `Object#hash` and fall back to the value
  itself. On the `hash` bench (1M Integer-keyed ops) that walk was ~10 % of the
  whole run. Fix: `hashKey` now returns immediate value types — `Integer`,
  `Float`, `Symbol`, `true`/`false`, `nil` — directly as their own comparable
  map key (none can be subclassed to override `#hash`, so this is byte-identical
  to the old result, just without the per-op resolution).
- **`Array#select` / `#reduce` ran interpreted, through `Enumerable`.** Only
  `#map`/`#each` were native; `#select`/`#reduce` came from the `Enumerable`
  prelude and iterated via `__each_packed`, which yields through a
  `each { |*a| … }` splat — **an Array allocation and a second block dispatch per
  element**. On the `array` pipeline (`.map.select.reduce`, 500 × 2000 elements)
  that doubled the block-call count. Fix: native `Array#select` (result pre-sized
  to the input length, single pass) and native `Array#reduce`/`#inject` (a direct
  fold mirroring every `Enumerable#reduce` form and error, so behaviour is
  identical). `#filter`/`#collect`/`#inject` delegate to these through the
  prelude and inherit the fast path.

Result on this host: **`array` AOT reaches MRI parity (0.98× at N=20)** — the
pipeline now runs at C speed — while `hash` drops 3.0×→2.3× and `wordcount`
1.5×→1.3×. The remaining `hash` gap is dispatch/allocation on the `h[k]=v` *send
itself* (arg-slice + interface boxing on `OpSend`, i.e. Level-2/interpreter
territory, not the Hash method), and `wordcount` is bounded by `String#split` +
the `strKey` `[]byte→string` copy inherent to a mutable-string key — neither is a
container-kernel cost, so they are left for the dispatch/AOT lever above.

## Where the gap is, and the plan to close it

Profiling (and a `GOGC=off` control, which makes things *slower*) shows the gap
is **not** garbage collection — it is per-instruction and per-call interpreter
overhead inherent to a clean, interface-based Go bytecode VM.

**What the experiments ruled out (measured, not assumed):**

- **Allocation / boxing is not the bottleneck.** A flyweight that removed 260k
  integer boxings from `loop` left its time unchanged (20.2 vs 20.6 ms), and
  `GOGC=off` makes things *slower*. So a tagged-`Value`/Fixnum refactor — a huge
  change — would **not** close the gap. This experiment was run *before*
  committing to that refactor, and saved it.
- **De-closuring the dispatch loop has no clean form.** A throwaway micro-test
  confirmed the closure+`defer` wrapper costs ~6× over plain locals, so the
  lever is real. But: a *duplicated* fast loop (local operand stack) does reach
  that speed (fib −24 %) yet leaves the handler loop's opcode cases uncovered —
  which the 100 %-coverage rule forbids — and duplicates ~250 lines. A *single*
  shared loop over a heap frame keeps coverage but its per-call frame allocation
  and method-call push/pop **regress** the call-heavy cases (fib/blocks) while
  only helping `loop`. Neither is acceptable, so the interpreter stays as is.

**Safe interpreter wins already landed:** Env-slot inlining (−33 % allocations)
and a monomorphic send fast path. Both are small (~5 %); the interpreter is at
its clean-design floor of ~3–6× MRI on compute-bound code.

## The real lever: build-time compilation (AOT) — shipped

Matching CRuby's C interpreter — let alone YJIT, which *is* a JIT — on
compute-bound code is not achievable for a clean Go interpreter. The lever that
*does* reach it, and a natural fit for a project that already links a single
static binary through the Go toolchain, is to **compile Ruby methods to Go at
`rbgo build` time** and let the Go compiler lower them to native code. This is
**implemented** (`internal/aot`, [docs/aot-compiler.md](../docs/aot-compiler.md)):

- **Level 1** lowers any method's bytecode to straight-line Go (locals as Go
  variables, a direct call for self-recursion); semantics stay identical because
  operators still go through the runtime. This alone beats the MRI interpreter.
- **Level 2** lowers a program's *top-level code and the blocks it passes* to Go
  — the `<main>` ISeq becomes `aotMain`, and each literal block an inline Go
  closure (a native `Proc`); outer locals a block closes over are captured
  lexically, and every send carries an inline method cache. This reaches the
  block-/string-/array-/hash-heavy "real app code" that defines no hot method:
  **`blocks` drops from 3.5× to 0.9× MRI** (now beats the interpreter, matches
  YJIT); `hash`/`array`/`wordcount` shed their driver overhead (~25–33 %) but
  stay near the floor, since their hot work is the native container methods MRI
  runs in C.
- **Level 3** specialises a pure-integer method (arithmetic/comparison on Integer
  parameters, recursion *and* `while` loops) to an **unboxed `int64` kernel**,
  with a type guard at the boundary and a **deopt** edge that recovers any
  overflow / divide-by-zero by re-running the sound interpreted body — so it
  stays correct for every input (overflow still promotes to the identical
  Bignum). This is what the `fib`/`loop` rows above measure: **~25× MRI / ~5.5×
  YJIT on `fib`, ~36× on `loop`.**

The dynamic cases (redefinition, `method_missing`, `eval`, non-integer or
polymorphic methods) fall back to the interpreter, so correctness is never at
risk. Every stage is gated by the 100 %-coverage + MRI-differential suite;
`BenchmarkAOTGeneratedL3Fib` pins the generated kernel to the YJIT-beating bar.

Still ahead for AOT: Float/mixed-type kernels, cross-method devirtualisation,
and the require-graph/closed-world *single-binary* half of `rbgo build`.

## Known issues surfaced by benchmarking

- _(none open)_ — `Hash.new(0)` / `Hash.new { … }` default-valued hashes, which
  previously crashed with a Go-level panic, now work (default value and default
  proc, with the MRI arity guards).
