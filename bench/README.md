# Benchmarks

Performance is tracked **differentially against the reference implementation
(MRI)** — every benchmark program is first checked for identical output under
`rbgo` and MRI, then timed under three runtimes:

- **rbgo** — this interpreter (`go build -ldflags="-s -w" -o rbgo ./cmd/rbgo`)
- **MRI** — the reference CRuby interpreter (oracle: Ruby 4.0.5)
- **MRI+YJIT** — CRuby with its JIT enabled (`ruby --yjit`)

The goal is to **at least match the reference interpreter** on every workload.

## Running

```bash
go build -ldflags="-s -w" -o rbgo ./cmd/rbgo
RUBY=ruby RBGO=./rbgo bash bench/run.sh 5      # best of 5 runs each
```

`bench/run.sh` checks output parity first (a program whose output differs is
reported and skipped), then reports the best wall-clock time of N runs (best,
not mean, to suppress scheduler noise) and the rbgo/MRI and rbgo/YJIT ratios.

There is also a Go micro-benchmark suite for profiling the execution loop in
isolation (parse + compile + prelude excluded):

```bash
go test ./internal/vm/ -run=NONE -bench=. -benchmem
go test ./internal/vm/ -run=NONE -bench=Fib -cpuprofile=cpu.prof   # then: go tool pprof
```

## Workloads

| File | Exercises |
| --- | --- |
| `fib.rb` | recursion + method dispatch (call-bound) |
| `loop.rb` | tight integer `while` loop (arithmetic + locals) |
| `blocks.rb` | block iteration (`Integer#times`) |
| `array.rb` | `map`/`select`/`reduce` pipeline |
| `hash.rb` | Hash insertion + lookup |
| `strings.rb` | string interpolation + `join` |
| `wordcount.rb` | split + hash counting + sum (mixed) |

## Current results (Apple M-series, Ruby 4.0.5)

| Benchmark | rbgo | MRI | MRI+YJIT | rbgo/MRI | rbgo/YJIT |
| --- | ---: | ---: | ---: | ---: | ---: |
| array | 0.35s | 0.09s | 0.06s | 3.89× | 5.83× |
| blocks | 0.84s | 0.25s | 0.23s | 3.36× | 3.65× |
| fib | 0.86s | 0.14s | 0.04s | 6.14× | 21.50× |
| hash | 0.26s | 0.09s | 0.09s | 2.89× | 2.89× |
| loop | 1.34s | 0.37s | 0.37s | 3.62× | 3.62× |
| strings | 0.04s | 0.04s | 0.04s | 1.00× | 1.00× |
| wordcount | 0.13s | 0.09s | 0.08s | 1.44× | 1.62× |

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

## The real lever: build-time compilation (AOT "JIT")

Matching CRuby's C interpreter — let alone YJIT, which *is* a JIT — on
compute-bound code is not achievable for a clean Go interpreter. The right
architecture, and a natural fit for this project (which already links a single
static binary through the Go toolchain), is to **compile Ruby methods to Go
source at `rbgo build` time** and let the Go compiler lower them to native code:

- Specialise hot methods to typed Go (e.g. `Integer#+`, loops, known sends),
  guarded by a method-state check, with a **deopt fall-back to the interpreter**
  for the dynamic cases (redefinition, `method_missing`, `eval`).
- This is the build-time analogue of YJIT's runtime specialisation, and is the
  path that can reach — and on tree-shaken static code, beat — MRI.

It is a major, multi-stage effort; the first concrete step is a **feasibility
prototype**: AOT-compile one constrained kernel (integer arithmetic + a counted
loop) end to end and measure it against MRI to validate the approach before
committing to the full compiler. Every stage stays gated by the 100 %-coverage +
MRI-differential suite.

## Known issues surfaced by benchmarking

- `Hash.new(0)` (a default-valued hash) is not implemented and currently raises
  a Go-level panic instead of working or raising a Ruby error — to be fixed.
