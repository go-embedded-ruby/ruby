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
overhead. The reference interpreter wins because of two structural advantages
this VM does not yet have:

1. **Tagged Fixnums.** CRuby encodes small integers in the pointer, so integer
   arithmetic allocates nothing. Here every `object.Integer` result boxes into a
   Go interface (a heap word for values outside Go's 0–255 cache). This is the
   single biggest item for `fib`/`loop`.
2. **A register-resident dispatch loop + inline caches.** The execution loop
   keeps its operand stack and program counter on the heap (they must survive
   the panic/recover used for `raise`), so every push/pop/local access is a
   pointer indirection; and each call site re-resolves the method.

**Roadmap (ordered by expected impact):**

- [ ] A non-`raise` fast path for the execution loop (operand stack + `pc` as
      registers when the frame installs no exception handler), so the common
      method body runs without heap indirection.
- [ ] Monomorphic inline caches at call sites, with a global method-state
      generation counter for invalidation.
- [ ] A tagged `Value` representation (Fixnum without boxing) — the largest
      change, and what ultimately matches MRI on compute-bound code.
- [ ] Streamlined call path (fuse `dispatchSend`→`send`→`invoke`; avoid the
      per-send argument-slice allocation once retention is provably safe).

Each step is gated by the existing 100%-coverage + MRI-differential test suite,
so correctness never regresses for speed.

## Known issues surfaced by benchmarking

- `Hash.new(0)` (a default-valued hash) is not implemented and currently raises
  a Go-level panic instead of working or raising a Ruby error — to be fixed.
