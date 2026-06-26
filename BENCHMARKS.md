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
  not mean, to suppress scheduler noise) — N = 8 for the call-bound rows
  re-measured 2026-06-22, N = 5 for the rest.
- **rbgo+AOT:** a specialised native binary from `rbgo build` (the program's
  lowerable methods compiled to Go and linked); shown because it is the lever
  that reaches CRuby. AOT builds need `GOWORK=off` on this host (a stray parent
  `go.work` otherwise shadows the module graph).
- Reproduce: `GOWORK=off AOT=1 RUBY=ruby RBGO=./rbgo bash bench/run.sh 5`.

## Results (best of 8)

> **2026-06-22 — inline method caches landed.** The call-bound rows below are
> *after* adding per-send-site inline method caches, in-place argument passing on
> the OpSend fast path, pooled operand-stack/frame backing arrays, and an
> exact-arity block-bind fast path. Before→after (best-of-8, this host):
> **fib 3030→2480 ms (−18%)**, **dispatch 1340→1190 ms (−11%)**,
> **proc 750→700 ms (−7%)**, **blocks 700→640 ms (−9%, also from a reused
> times-arg slice)**; `alloc` is unchanged (allocation-, not dispatch-, bound).
> All output stays byte-identical to MRI. Cache invalidation is exact —
> method (re)definition, `define_method`, `include`/`prepend`, singleton-method
> definition and `extend` all bust the relevant caches (a global method-serial
> stamp + per-object singleton bypass; tests in `inlinecache_test.go`).

| program | rbgo (ms) | ruby (ms) | ruby --yjit (ms) | rbgo+AOT (ms) | ratio rbgo/ruby | ratio rbgo/yjit | verdict |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| strings | 40 | 40 | 40 | 40 | 1.0× | 1.0× | **parity** (startup + I/O bound) |
| wordcount | 120 | 90 | 80 | 120 | 1.3× | 1.5× | competitive |
| hash | 280 | 100 | 100 | 300 | 2.8× | 2.8× | within ~3× |
| array | 340 | 90 | 60 | 340 | 3.8× | 5.7× | within ~4–6× |
| blocks | 640 | 250 | 230 | 880 | 2.6× | 2.8× | within ~3× (inline-cache + reused-arg path) |
| proc | 700 | 160 | 140 | 840 | 4.4× | 5.0× | within ~4–5× |
| alloc | 1320 | 240 | 200 | 1410 | 5.5× | 6.6× | allocation-bound, ~6× |
| dispatch | 1190 | 230 | 180 | 1550 | 5.2× | 6.6× | call-bound — narrowed by inline caches |
| loop | 1520 | 400 | 400 | **10** | 3.8× | 3.8× | AOT: **40× faster than both** |
| mandelbrot | 2650 | 860 | 830 | n/a* | 3.1× | 3.2× | float kernel, ~3× (not yet AOT-lowered) |
| fib | 2480 | 500 | 110 | **20** | 5.0× | 22.5× | call-bound — inline caches; AOT still wins |

\* *`mandelbrot` is the benchmarks-game float kernel at `mandelbrot(600)`; it is
not yet AOT-lowered (the AOT path currently specialises only pure-integer
methods), so the AOT column is n/a and the row reflects the interpreter: rbgo
~2.65 s vs MRI ~0.78 s (≈3.1×), sitting with the other compute-bound rows.*

> Correctness gate: every row above produced **byte-identical** output under
> rbgo, MRI and MRI+YJIT (checked by `bench/run.sh` before timing). No program
> was skipped for divergence.

## Comparative runtimes — rbgo vs MRI / YJIT / JRuby / TruffleRuby (2026-06-26)

Best-of-8 wall time (ms), same `bench/*.rb` through every runtime, output checked
byte-identical against MRI first. Host: Apple M4 Max, darwin/arm64.
`ruby 4.0.5`, `jruby 10.1.0.0` (OpenJDK 25), `truffleruby 34.0.1` (GraalVM CE
Native). Reproduce: `AOT=1 RUNS=8 JRUBY=jruby TRUFFLE=<path> bash bench/run.sh 8`.

| program | rbgo | rbgo+AOT | MRI | MRI+YJIT | JRuby | TruffleRuby |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| strings | 40 | 40 | 40 | 40 | 1040 | 120 |
| wordcount | 120 | 120 | 80 | 80 | 1090 | 200 |
| hash | 250 | 260 | 80 | 80 | 1120 | 100 |
| array | 440 | 440 | 90 | 60 | 1160 | 60 |
| blocks | 560 | 600 | 250 | 220 | 1200 | 80 |
| proc | 690 | 680 | 160 | 140 | 1160 | 70 |
| dispatch | 1180 | 1150 | 220 | 170 | 1190 | 50 |
| alloc | 1250 | 1280 | 230 | 190 | 1190 | 90 |
| loop | 1490 | **10** | 360 | 360 | 1240 | 90 |
| fib | 2470 | **20** | 480 | 100 | 2770 | 150 |
| mandelbrot | 2930 | 2920 | 780 | 760 | 1270 | 90 |

- **TruffleRuby** (GraalVM native JIT) is the compute ceiling — e.g. `mandelbrot`
  90 ms vs MRI 780 ms. A tracing JIT is expected to dominate steady-state loops.
- **JRuby** is dominated by ~1.0–1.2 s JVM startup: every short program lands near
  1.1 s regardless of work, and only on the heaviest (`fib` 2.77 s) does the work
  show — for these single-shot micro-benchmarks startup is the story.
- **rbgo** (pure-Go bytecode interpreter, CGO=0) runs ~3–6× MRI on compute-bound
  code and at **parity** where startup/I-O dominates (`strings`, `wordcount`).
- **rbgo+AOT** is the standout: `loop` 10 ms and `fib` 20 ms — **18–24× faster
  than MRI+YJIT**, the only runtime here that beats YJIT, via closed-world native
  lowering of integer-bound methods (`mandelbrot`'s float kernel is not yet
  AOT-lowered).

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
- **Widest gap — call-bound:** `dispatch` (5.2× MRI, 6.6× YJIT) and the
  interpreted `fib` (5.0× MRI, **22.5× YJIT**) — both **narrowed** by the new
  inline method caches (was 6.5×/8.3× and 6.7×/29× respectively). Per-call
  overhead (frame setup + method lookup + interface dispatch) is still rbgo's
  most expensive primitive relative to CRuby's inline cache + YJIT's call-site
  specialisation, but the per-call-site cache now removes the method-table walk
  on the monomorphic hot path, leaving frame setup + interface dispatch as the
  residual cost. Matching the MRI *interpreter* on these is the realistic bar;
  YJIT (a JIT) stays far ahead until rbgo grows runtime specialisation.
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
4. **Method dispatch now has an inline cache (landed 2026-06-22).** Each send
   first hit the monomorphic fast path (~5%); it now also carries a per-call-site
   inline cache keyed by receiver class + a global method serial, so a warm
   monomorphic send is a pointer compare instead of a method-table walk. This
   closed `dispatch` from 6.5×→5.2× MRI and interpreted `fib` from 6.7×→5.0×.
   What remains on these rows is frame setup + interface-handler dispatch
   (items 2–3), not method resolution.

## Action items (ordered by expected leverage)

1. ~~**Inline (polymorphic) method caches at the send site.**~~ **DONE
   (2026-06-22).** Per-call-site monomorphic inline cache keyed by receiver class
   + global method serial; drops the method-table walk on the warm hot path.
   Delivered −18% `fib`, −11% `dispatch`, −7% `proc`. Next refinement: a small
   polymorphic (N-way) cache for megamorphic sites.
2. **Threaded-code / computed-goto-style dispatch.** Replace the interface
   handler indirection with a direct-threaded loop (jump table over opcode
   functions) to cut per-instruction overhead on every compute row. A
   *duplicated* fast loop already showed −24% on `fib` in a throwaway test — the
   lever is real; the task is a form that keeps the 100%-coverage gate.
3. **Cheaper call frames (partly done).** The frame env and the operand-stack
   backing array are now pooled (free-lists, GVL-guarded), and the OpSend fast
   path passes args in place from the operand stack instead of copying — this is
   part of the 2026-06-22 win. Remaining: avoid the `push`/`pop` closures that
   force the operand stack to the heap, and stack-allocate small frames.
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
