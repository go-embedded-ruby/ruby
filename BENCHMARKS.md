# Performance parity — go-embedded-ruby (rbgo) vs MRI/CRuby  (2026-06-22)

This is the standard performance-parity report for the pure-Go Ruby VM
(`rbgo`, a from-scratch mruby/YARV-style bytecode interpreter, CGO=0) measured
**differentially against the reference implementation, MRI/CRuby** — the same
`.rb` source run through every runtime, output checked for byte-identical
parity first, then wall-clock timed.

The honest bar is **"as good as MRI"**. CRuby is a C interpreter with three
decades of tuning, and `ruby --yjit` adds a production JIT — a very high bar.
A clean, interface-dispatched Go bytecode VM is expected to be several times
slower on compute-bound code; this report **quantifies how much, and where the
time goes**, and shows the one path (build-time AOT) that already beats both.

## Methodology

- **Host:** Apple M4 Max, macOS (darwin/arm64), 16 cores.
- **Go:** go1.26.4. **rbgo:** `go build -ldflags="-s -w" -o rbgo ./cmd/rbgo`.
- **Reference:** `ruby 4.0.5 (2026-05-20) +PRISM [arm64-darwin25]`, the MRI
  oracle. **YJIT:** `ruby --yjit` (`RubyVM::YJIT.enabled? == true`).
- **Fairness:** the **same** `bench/*.rb` is run by every runtime. Each
  program's stdout is checked for byte-identical parity against MRI **before**
  timing; a divergent program is reported and skipped. Single process, no
  server warm-up beyond the program's own loop; **best-of-N** wall time (best,
  not mean, to suppress scheduler noise) — N = 5 for the table below.
- **rbgo+AOT:** a specialised native binary from `rbgo build` (the program's
  lowerable methods compiled to Go and linked); shown because it is the lever
  that reaches CRuby. AOT builds need `GOWORK=off` on this host (a stray parent
  `go.work` otherwise shadows the module graph).
- Reproduce: `GOWORK=off AOT=1 RUBY=ruby RBGO=./rbgo bash bench/run.sh 5`.

## Results (best of 5)

| program | rbgo (ms) | ruby (ms) | ruby --yjit (ms) | rbgo+AOT (ms) | ratio rbgo/ruby | ratio rbgo/yjit | verdict |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| strings | 40 | 40 | 40 | 40 | 1.0× | 1.0× | **parity** (startup + I/O bound) |
| wordcount | 120 | 90 | 80 | 120 | 1.3× | 1.5× | competitive |
| hash | 280 | 100 | 100 | 300 | 2.8× | 2.8× | within ~3× |
| array | 340 | 90 | 60 | 340 | 3.8× | 5.7× | within ~4–6× |
| blocks | 850 | 250 | 230 | 880 | 3.4× | 3.7× | within ~4× |
| proc | 850 | 170 | 150 | 840 | 5.0× | 5.7× | within ~5–6× |
| alloc | 1330 | 240 | 210 | 1410 | 5.5× | 6.3× | allocation-bound, ~6× |
| dispatch | 1490 | 230 | 180 | 1550 | 6.5× | 8.3× | call-bound, the widest gap |
| loop | 1520 | 400 | 400 | **10** | 3.8× | 3.8× | AOT: **40× faster than both** |
| mandelbrot | 2650 | 860 | 830 | n/a* | 3.1× | 3.2× | float kernel, ~3× (not yet AOT-lowered) |
| fib | 3500 | 520 | 120 | **20** | 6.7× | 29× | AOT: **26× MRI / 6× YJIT** |

\* *`mandelbrot` is the benchmarks-game float kernel at `mandelbrot(600)`; it is
not yet AOT-lowered (the AOT path currently specialises only pure-integer
methods), so the AOT column is n/a and the row reflects the interpreter: rbgo
~2.65 s vs MRI ~0.78 s (≈3.1×), sitting with the other compute-bound rows.*

> Correctness gate: every row above produced **byte-identical** output under
> rbgo, MRI and MRI+YJIT (checked by `bench/run.sh` before timing). No program
> was skipped for divergence.

## Where rbgo stands

- **Competitive / at parity:** `strings`, `wordcount`. Anything dominated by
  process startup or string/IO throughput already matches MRI — rbgo is a
  single static binary with **no gem / `$LOAD_PATH` scan** (~0 ms vs MRI's
  ~30 ms startup), so short scripts win the startup back.
- **Within ~3–6× of MRI (the clean-interpreter floor):** `hash`, `array`,
  `blocks`, `proc`, `alloc`. These are interpreter-bound Enumerable / block /
  allocation pipelines with no user method to lower, so they run on the bytecode
  loop. ~3–6× the C interpreter is the expected floor for an interface-dispatched
  Go VM.
- **Widest gap — call-bound:** `dispatch` (6.5× MRI, 8.3× YJIT) and the
  interpreted `fib` (6.7× MRI, **29× YJIT**). Per-call overhead (frame setup +
  method lookup + interface dispatch) is rbgo's single most expensive primitive
  relative to CRuby's inline method cache + YJIT's call-site specialisation.
- **AOT beats both:** the two pure-integer **method** kernels (`fib`, `loop`)
  AOT-compile to unboxed `int64` Go and **beat CRuby and YJIT outright** —
  `fib` ~26× MRI / ~6× YJIT, `loop` ~40× both. This is the build-time lever a
  Go-toolchain project gets for free.

## Root cause — where the time goes

Profiling (`go test ./internal/vm -bench=Fib -cpuprofile`), plus controlled
experiments, locate the cost precisely. **Measured, not assumed:**

1. **Per-call / per-instruction interpreter overhead is the dominant cost.**
   The two call-heavy rows (`dispatch`, `fib`) have the worst ratios. Each
   bytecode step goes through an interface-typed handler; each method call
   builds a heap frame and walks a method table. This is the inherent tax of a
   clean, interface-based Go bytecode VM vs C's switch-threaded loop and
   inline caches.
2. **Allocation/boxing is *not* the bottleneck (ruled out).** A flyweight that
   removed 260k integer boxings from `loop` left its time unchanged
   (20.2 vs 20.6 ms), and `GOGC=off` makes things *slower*. So a
   tagged-`Value`/Fixnum refactor would **not** close the gap — this experiment
   was run before committing to that large refactor and saved it.
3. **GC is not the bottleneck either** — same `GOGC=off` control. The `alloc`
   row's ~6× is dominated by per-object frame/method-call cost, not collection.
4. **Method dispatch has no inline cache.** Each send re-resolves the target
   (a monomorphic send fast path landed, ~5%, but there is no per-call-site
   cache as in CRuby). `dispatch`'s 6.5–8.3× is the direct readout of this.

## Action items (ordered by expected leverage)

1. **Inline (polymorphic) method caches at the send site.** Cache the resolved
   method per call site keyed by receiver class; this is the single biggest
   lever for the call-bound rows (`dispatch`, interpreted `fib`, `proc`) and is
   exactly what CRuby/YJIT exploit. Highest ROI.
2. **Threaded-code / computed-goto-style dispatch.** Replace the interface
   handler indirection with a direct-threaded loop (jump table over opcode
   functions) to cut per-instruction overhead on every compute row. A
   *duplicated* fast loop already showed −24% on `fib` in a throwaway test — the
   lever is real; the task is a form that keeps the 100%-coverage gate.
3. **Cheaper call frames.** Pool / stack-allocate the per-call frame so method
   calls stop hitting the heap; targets `dispatch`/`fib`/`proc`/`alloc`.
4. **Extend AOT beyond integer kernels.** Float/mixed-type kernels (would pull
   `mandelbrot` into the AOT-wins column), cross-method devirtualisation, and
   the closed-world single-binary half of `rbgo build`. Already the only path
   that beats YJIT; widening its eligibility widens the win.
5. **Eventually, a JIT.** Matching YJIT on arbitrary dynamic code ultimately
   needs runtime specialisation. AOT covers the static/compute-bound case today;
   a tracing/method JIT is the long-horizon answer for the dynamic remainder.

## Reproducing

```bash
go build -ldflags="-s -w" -o rbgo ./cmd/rbgo
GOWORK=off AOT=1 RUBY=ruby RBGO=./rbgo bash bench/run.sh 5   # full table
AOT=0          RUBY=ruby RBGO=./rbgo bash bench/run.sh 5      # interpreter only

# Isolated execution-loop micro-benchmarks (parse/compile/prelude excluded):
go test ./internal/vm/ -run=NONE -bench=. -benchmem
```

The harness (`bench/run.sh`) and its programs live under `bench/` and are
**isolated from the coverage gate** (no `_test.go`, plain `.rb` + shell), so
they never affect the 100%-coverage CI requirement.
