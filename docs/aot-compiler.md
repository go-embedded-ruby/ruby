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
| rbgo, interpreted | 329 ms | the bytecode VM |
| **rbgo, AOT level 1** | **38 ms** | sound, every op via `binaryOp` — **beats MRI** |
| **rbgo, AOT level 2** | **8.3 ms** | typed + guarded inline — **matches YJIT** |
| MRI 4.0.5 interpreter | 44 ms | |
| MRI 4.0.5 + YJIT | 8.0 ms | |

The result is decisive:

- **Level 1** keeps full Ruby semantics (every operator still dispatches through
  `binaryOp`, so a redefined `Integer#+` is honoured identically) yet, just by
  replacing the dispatch loop with straight-line Go control flow + Go locals +
  direct calls, it is **8.6× faster than interpreting and already beats the MRI
  interpreter**. This is the floor available to *every* method, with no type
  analysis.
- **Level 2** adds a receiver type guard and inline integer arithmetic with a
  deopt fall-back to level 1 — YJIT's playbook — and **matches YJIT**.

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

1. **Codegen skeleton** — lower a restricted opcode subset (push/local/branch/
   integer-arith/known-send/return) to level-1 Go for a single method; verify it
   produces MRI-identical output and the measured speedup.
2. **Whole-method level-1** — cover every opcode (with interpreter fall-back for
   the few hard ones), so any method can be compiled soundly.
3. **`rbgo build` integration** — generate, compile and link emitted Go into the
   static binary; method dispatch prefers the compiled entry.
4. **Level-2 specialisation + deopt** — type guards, inline primitives, the
   method-state generation counter and deopt edges.

The prototype (`aot_proto_test.go`) stays as the regression that pins the
target: level-1 must keep beating the MRI interpreter, level-2 must keep matching
YJIT.
