# Build-time AOT compiler (Ruby → Go → native)

## Why

A clean, interface-based Go bytecode interpreter is structurally ~3–6× slower
than CRuby's hand-tuned C interpreter on compute-bound code, and far slower than
YJIT (which *is* a JIT). Experiments ruled out the cheap levers — allocation is
not the bottleneck (a tagged-Fixnum refactor was measured useless before being
undertaken), and de-closuring the dispatch loop has no form that is both fast
and compatible with the 100 %-coverage rule (see `bench/README.md`).

The lever that *does* reach the reference is to stop interpreting hot methods and
instead **compile them to Go source at `rbgo build` time**, letting the Go
toolchain lower them to native code. This is a natural fit: rbgo already links a
single static binary through `go build`, and `rbgo build` already tree-shakes
the reached stdlib.

## Feasibility: proven

`internal/vm/aot_proto_test.go` hand-writes the Go a compiler would emit for
`def fib(n) = n < 2 ? n : fib(n-1) + fib(n-2)` at two specialisation levels and
benchmarks them. One `fib(30)`, lower is faster:

| Runtime | Time | |
| --- | ---: | --- |
| rbgo, interpreted | ~320 ms | the bytecode VM |
| **rbgo, AOT level 1** | **35 ms** | sound, every op via `binaryOp` — **beats MRI** |
| **rbgo, AOT level 2** | **7.4 ms** | typed + guarded, boxed — **matches YJIT** |
| **rbgo, AOT level 3** | **1.95 ms** | unboxed interior — **beats YJIT ~4×** |
| MRI 4.0.5 interpreter | 44 ms | |
| MRI 4.0.5 + YJIT | 8.0 ms | |

The result is decisive:

- **Level 1** keeps full Ruby semantics (every operator still dispatches through
  `binaryOp`, so a redefined `Integer#+` is honoured identically) yet, just by
  replacing the dispatch loop with straight-line Go control flow + Go locals +
  direct calls, it is **~9× faster than interpreting and already beats the MRI
  interpreter**. This is the floor available to *every* method, with no type
  analysis.
- **Level 2** adds a receiver type guard and inline integer arithmetic, boxing
  each result, with a deopt fall-back — YJIT's playbook — and **matches YJIT**.
- **Level 3** is the boundary form a whole-method type-inference pass enables:
  guard + box only at the method edge, the entire recursive interior on unboxed
  `int64`. It **beats YJIT by ~4×**.

### Can we beat the Ruby JIT? Yes — and here is why (measured)

Level 3 already does (1.95 ms vs YJIT's 8.0 ms), with a *naive* hand-compilation
(no PGO, no cross-method inlining beyond what Go does for free). The advantage is
structural, and compounds:

- **Whole-program analysis, unlimited compile budget.** A runtime JIT specialises
  a region at a time under a time budget and only sees the traces it has run. An
  AOT pass at `rbgo build` sees the entire call graph and can prove types,
  devirtualise sends, and constant-fold across method boundaries before emitting
  code.
- **Go's optimiser does the backend.** We emit Go; the Go compiler contributes
  decades of register allocation, inlining, escape analysis, SSA and dead-code
  elimination — and **PGO** (`go build -pgo`) feeds a representative profile back
  in, pushing hot paths further than a JIT's budgeted codegen.
- **Zero runtime cost, zero warmup.** A JIT spends execution time compiling,
  profiling and managing a code cache, and pays a cold start. AOT pays none of
  that: the binary runs at full speed from the first instruction — a decisive
  edge on short and bursty programs.
- **Tree-shaking + monomorphisation.** `rbgo build` already prunes unreached
  code; specialising each call site against the known callee removes dispatch a
  runtime JIT would still guard.

Where a JIT can still win is genuinely polymorphic or self-modifying code
(`eval`, `define_method`, runtime redefinition): there AOT falls back to the
interpreter. But those are rare in hot paths, and the fall-back is always correct.
On average, for the compute-bound code where speed matters, **AOT + Go's
optimiser + PGO beats YJIT**.

## Design

At `rbgo build`, for each compiled method ISeq:

1. **Emit a Go function** `m_<id>(vm *VM, self Value, args []Value) Value` whose
   body is the ISeq lowered to structured Go: branches → `if`/`for`, `OpSend` →
   a direct call (to another emitted function when the callee is known, else
   `vm.dispatchSend`), locals → Go variables, the operand stack → SSA temporaries
   (no runtime stack).
2. **Level-1 (always correct):** operators and sends go through the existing
   `vm.binaryOp` / `vm.send`, so semantics are identical to the interpreter.
   This alone clears the MRI-interpreter bar.
3. **Level-2 (opportunistic):** where a type can be assumed (profile or simple
   inference), emit a guarded fast path with inlined primitives and a **deopt**
   edge back to the level-1 body (or the interpreter) when the guard fails or a
   method involved has been redefined (checked via a method-state generation
   counter).
4. **Dynamic escape hatch:** `eval`, `define_method`, `method_missing`, and any
   method that is redefined at runtime fall back to the interpreter — the
   compiled binary always carries it, so correctness is never at risk.
5. **Link** the generated Go alongside the runtime; method tables point at the
   compiled function, with the interpreter as the universal fall-back.

## Staging

Each stage is gated by the 100 %-coverage + MRI-differential suite, and each is
shippable on its own:

1. **Codegen skeleton** — ✅ **done** (`internal/aot`). Lowers a method ISeq to
   level-1 Go using depth-indexed stack variables and `goto` for control flow,
   with a self-send to the method becoming a direct recursive call; an
   unsupported opcode/constant/parameter-shape leaves the method to the
   interpreter (`Compile` returns ok=false). The generated `fib` compiles, runs
   MRI-identically (`fib(30) = 832040`), and clocks the prototype's level-1 speed
   (~36 ms — beating the MRI interpreter). 100% covered.
2. **Whole-method level-1** — ✅ **done**. Covers arrays, hashes, ranges, ivar
   get/set, const get/set, global read, regexp, `block_given?`/`yield`, and
   splat/concat, each through a runtime helper (`internal/vm/aot_runtime.go`)
   when it needs interpreter state. A generated-method differential suite
   (`cmd/aotgen` → `aot_e2e_*_test.go`) diffs ten compiled methods against MRI.
3. **`rbgo build` integration** — ✅ **done**. `internal/aot.CompileProgram`
   lowers a program's top-level methods to one `package vm` Go file (a function
   per method + an `init()` registering them via the dispatch seam,
   `RegisterCompiled`); `rbgo build` injects it with `go build -overlay` and
   links a specialised binary. `invoke()` prefers a method's compiled entry; a
   redefinition deopts to the interpreter. `go tool nm` confirms the compiled
   symbols are linked.
4. **Level-3 integer-kernel specialisation + deopt** — ✅ **done**
   (`internal/aot/level3.go`). A method that is pure integer arithmetic /
   comparison on Integer parameters (with self-recursion) is lowered to an
   unboxed `int64` kernel; the boxed entry guards the args are Integer and a
   `recover` turns an overflow / divide-by-zero (`aotDeopt`) into the level-1
   result, so it stays correct for every input. The overflow conditions mirror
   `intOp` exactly. The **generated** `fib(30)` clocks **1.86 ms** — matching the
   hand-written level-3 prototype (1.77 ms) and **beating MRI+YJIT (7.5 ms) by
   ~4×**. The deopt edges are proven against MRI: integer overflow promotes to
   the identical Bignum, a non-Integer argument falls back through level-1, and
   divide-by-zero raises `ZeroDivisionError`. Iterative kernels qualify too: a
   `while`-loop integer method (e.g. factorial) lowers to an unboxed loop, and an
   overflow mid-loop deopts and re-runs from the original arguments into the
   identical Bignum (`25! = 15511210043330985984000000`, MRI-verified).

The prototype (`aot_proto_test.go`) stays as the regression that pins the
target: level-1 must keep beating the MRI interpreter, level-3 must keep beating
YJIT. `BenchmarkAOTGeneratedL3Fib` pins the *generated* kernel to that same bar.

Still interpreted (left to future stages): optional/keyword/splat parameters,
non-integer (Float / mixed) kernels, and calls to methods other than the one
being compiled (no cross-method devirtualisation yet).

## Closed-world builds (`rbgo build --closed`)

The AOT path above specialises *methods*; closed-world mode specialises the
*whole binary*. `rbgo build --closed app.rb` produces a self-contained executable
that runs with no source file and links no front-end.

Two pieces make that possible:

- **Frozen bytecode.** `internal/aot.FreezeISeq` serialises a compiled `ISeq`
  tree to Go source — a single `func() *bytecode.ISeq` rebuilding it as literals,
  with no lexer/parser/compiler involved. The constant pool only ever holds the
  five kinds the compiler emits (Integer, Bignum, Float, String, Symbol), so the
  emitter is small and total. The program is frozen at build time and injected as
  `embeddedProgram` via `go build -overlay`; the prelude is frozen once into the
  committed `embeddedPrelude` (regenerated by `cmd/freeze-prelude`, kept honest by
  a deep-equal drift test against a fresh parse of `prelude.rb`).

- **Front-end isolation.** Every runtime parse+compile goes through one seam,
  `parseCompileFn`. The open build (`frontend_open.go`) wires it to the real
  parser+compiler; the closed build (`frontend_closed.go`, `//go:build
  rbgo_closed`) replaces it with a stub that raises `NotImplementedError`. The
  prelude install and `Binding#eval` are likewise tag-split. With nothing left
  referencing the parser/compiler, the linker drops them — `go tool nm` confirms
  zero front-end symbols, and the binary shrinks (~18% on the `fib` sample).

A require-graph scan (`internal/aot.FrontendUses`) walks the program for
`eval`/`require`/`instance_eval`/… and `rbgo build --closed` reports them, since
those calls raise in the closed binary. Everything else — the AOT-compiled
methods, the frozen prelude's Comparable/Enumerable, the core types — runs
unchanged, byte-for-byte with MRI.

## WebAssembly target (`rbgo build --closed --target wasm`)

The closed-world path also cross-compiles to the browser.
`rbgo build --closed --target wasm app.rb` emits a `GOOS=js GOARCH=wasm` module
that runs the embedded program with no front-end linked — the wasm equivalent of
the native closed-world binary.

- `--target wasm` **requires `--closed`** (the wasm entry point runs the baked-in
  program; there is no command-line file to read in a browser tab).
- `buildEnv` appends `GOOS=js GOARCH=wasm` to the nested `go build`, so the build
  links `closed_main_wasm.go` instead of the native closed main. That entry runs
  the embedded program and then blocks on `select{}` — a Go wasm module that
  returns from `main()` is torn down, so it must stay parked for any JS event or
  animation callbacks the program registered to keep firing.
- The program can reach the page through the built-in **`JS` module**
  (`internal/vm/jsbridge_wasm.go`), which `vm.New` registers automatically on
  wasm: `JS.global`/`window`/`document`/`log`, `JS::Ref#get`/`set`/`call`/`[]`/`on`
  for DOM and Canvas, and `JS.raf { |t| … }` for an animation loop. So a
  closed-world wasm app can render and handle events with no hand-written
  JavaScript.

The same front-end isolation applies: `go tool nm` does not reliably read Go wasm
objects, so the integration test (`cmd/rbgo/wasm_build_test.go`,
`RBGO_BUILD_IT=1`) instead asserts the emitted module carries the `\0asm` magic
and that the linker dropped the front-end (no `go-ruby-parser/parser` or
`internal/compiler` symbols in the module bytes). Deploy the emitted `.wasm`
alongside Go's `wasm_exec.js` loader (see [`web/`](https://github.com/go-embedded-ruby/ruby/tree/main/web)).

For the *interactive REPL* flavour of wasm — the whole interpreter (front-end
included) compiled to the browser so the page can evaluate arbitrary Ruby — see
`cmd/wasm` and the playground in `web/`; that is a separate, open-world build,
not produced by `rbgo build`.
