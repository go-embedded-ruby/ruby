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
| `fib.rb` | recursion + method dispatch (call-bound) | yes (integer kernel) |
| `loop.rb` | tight integer `while` loop in a method | yes (integer kernel) |
| `dispatch.rb` | monomorphic method calls into an object | no (call-bound) |
| `alloc.rb` | short-lived object allocation + GC pressure | no |
| `proc.rb` | `Proc#call` invocation in a loop | no (proc dispatch) |
| `blocks.rb` | block iteration (`Integer#times`) | no (block dispatch) |
| `array.rb` | `map`/`select`/`reduce` pipeline | no (Enumerable) |
| `hash.rb` | Hash insertion + lookup | no |
| `strings.rb` | string interpolation + `join` | no |
| `wordcount.rb` | split + hash counting + sum (mixed) | no |
| `mandelbrot.rb` | benchmarks-game float kernel (compute-bound) | not yet (float) |

The formalized parity report — methodology, the full rbgo / MRI / MRI+YJIT
table, root-cause analysis and action items — lives in
[`../BENCHMARKS.md`](../BENCHMARKS.md).

## Current results (Apple M-series, Ruby 4.0.5, best of 3)

| Benchmark | rbgo | rbgo+AOT | MRI | MRI+YJIT | AOT/MRI | AOT/YJIT |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| array | 0.34s | 0.35s | 0.09s | 0.06s | 3.89× | 5.83× |
| blocks | 0.87s | 0.87s | 0.25s | 0.23s | 3.48× | 3.78× |
| **fib** | 3.48s | **0.02s** | 0.49s | 0.11s | **0.04×** | **0.18×** |
| hash | 0.26s | 0.25s | 0.09s | 0.09s | 2.78× | 2.78× |
| **loop** | 1.36s | **0.01s** | 0.37s | 0.36s | **0.03×** | **0.03×** |
| strings | 0.04s | 0.04s | 0.04s | 0.04s | 1.00× | 1.00× |
| wordcount | 0.12s | 0.12s | 0.09s | 0.08s | 1.33× | 1.50× |

The two method-based integer workloads (`fib`, `loop`) compile to unboxed
`int64` kernels: **`fib` is ~25× faster than MRI and ~5.5× faster than YJIT;
`loop` is ~36× faster than both.** The interpreter-bound rows (`array`/`blocks`/
`hash`/`strings`/`wordcount`) define no hot methods, so the AOT binary is the
same interpreter — `rbgo+AOT ≈ rbgo`, as expected (nothing was lowered). Those
rows still run on the interpreter at ~3–6× MRI, the clean-Go-interpreter floor.

rbgo also starts faster than MRI (~0 vs ~30 ms: a single static binary with no
gem/`$LOAD_PATH` scan), which is why string/IO-bound scripts already match.

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
