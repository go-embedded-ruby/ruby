<p align="center"><img src="https://raw.githubusercontent.com/go-embedded-ruby/brand/main/social/go-embedded-ruby.png" alt="go-embedded-ruby/ruby" width="720"></p>

# ruby — go-embedded-ruby

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-9B1C2E)](https://go-embedded-ruby.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Phase](https://img.shields.io/badge/phases-1--6%20active-1a7f37)](https://go-embedded-ruby.github.io/docs/roadmap/)

**A pure-Go implementation of Ruby — one static binary, full dynamism, zero cgo.**

This repository is the interpreter: a lexer, parser and compiler that lower Ruby
to bytecode, and a stack VM (mruby/YARV lineage) that runs it. The front-end is
**embedded in the binary**, so `eval` and runtime `require` keep working. Ruby
objects are Go heap objects, so **Go's garbage collector is reused**. Dispatch
goes through **mutable per-class method tables**, which is what makes
monkey-patching, `define_method` and `method_missing` free.

> 🌐 [Website](https://go-embedded-ruby.github.io) · 📚 [Documentation](https://go-embedded-ruby.github.io/docs/) · 🧭 [Roadmap](docs/plan-rbgo.md)

## Status

Supported today (every feature **differential-tested against MRI Ruby 4.0.5**):

- **Values:** integers (`int64`, with automatic **Bignum** promotion on int64
  overflow and arbitrary-precision integer literals), floats, strings, symbols,
  arrays, hashes, ranges (incl. beginless/endless), `true`/`false`/`nil`,
  `self`, `Proc`/lambda, `Regexp`/`MatchData`, `Struct`.
- **Operators:** arithmetic (`+ - * / %`, **Ruby floor division**, `**`),
  comparison/`<=>`, `==`/`===`, `<<`, `&&`/`||`, ternary, ranges; correct
  negative-literal precedence (`-2.abs == 2`, `-2**2 == -4`).
- **Control flow:** `if`/`elsif`/`else`, `unless`, `while`/`until`,
  `case`/`when`, statement modifiers, `begin`/`rescue`/`else`/`ensure`/`retry`,
  `break`/`next`, `Kernel#loop`.
- **Pattern matching (`case`/`in`):** value, variable-binding, class/constant,
  array (incl. splat and nested), hash (`deconstruct_keys`, `**rest`/`**nil`),
  find (`[*pre, x, *post]`), pin (`^x`) and alternative (`a | b`) patterns;
  the `=> name` binding suffix, guards (`if`/`unless`), the one-line forms
  `expr => pattern` and `expr in pattern`, and `NoMatchingPatternError` — via
  the `deconstruct`/`deconstruct_keys` protocols.
- **Assignment:** multiple assignment / destructuring (`a, b = 1, 2`, swap,
  `x, *rest = …`, `*init, last = …`), compound assignment
  (`+= -= *= /= %= <<= ||= &&=`).
- **Methods:** required / optional / `*splat` / **keyword** (`a:`, `b: 2`) /
  `**rest` / `&block` parameters, setter defs (`def name=`), **endless methods**
  (`def foo = expr`), recursion, `return`, `super`.
- **Blocks / Procs / lambdas:** `{ }` / `do…end` closures, `yield`,
  `block_given?`, `&block` capture, `Proc`/`lambda`/**stabby `->(){}`**, `&proc`
  block-pass and `Symbol#to_proc` (the `&:sym` shorthand).
- **Classes & modules:** inheritance, `@ivars`, `new`/`initialize`, constants and
  constant assignment, **class methods** (`def self.foo`), modules + `include`
  (mixins), `super`, **`attr_accessor`/`reader`/`writer`**, **`Struct.new`**.
- **Metaprogramming:** dynamic dispatch via mutable method tables,
  `method_missing`, `send`/`public_send`, `respond_to?`, **`define_method`**,
  **`instance_eval`/`instance_exec`**, **`class_eval`/`module_eval`/`class_exec`**,
  `instance_variable_get`/`set`/`defined?`.
- **Strings:** mutable (reference semantics) with `<<`/concat/replace/prepend/
  insert/`[]=`/slice!/the bang methods and `freeze`/`FrozenError`;
  interpolation, `%w`/`%i` literals, `%`/`format`/`sprintf`,
  case/strip/`split`/`each_char`/`lines` and friends.
- **Regular expressions:** `/re/imx` literals, `Regexp`/`MatchData`, `=~` /
  `match` / `match?` / `scan` / `gsub` / `sub` / `split`, and the match globals
  `$~` / `$1`..`$N` / `$&` / `` $` `` / `$'` — running on the standalone pure-Go
  [go-onigmo](https://github.com/go-onigmo/regexp) engine, so the build stays
  **CGO=0**.
- **Collections:** Array / Hash / Range with `Enumerable` (map/select/reduce/…)
  and `Comparable`, both written once in embedded Ruby; Array **bang methods**
  (`map!`/`sort!`/`select!`/`reject!`/`compact!`/`uniq!`/`reverse!`).
- **Objects:** `dup`/`clone`/`freeze`/`frozen?`, `equal?`,
  `instance_variable_get`/`set`.

**100% coverage** is enforced in CI across all six 64-bit targets (amd64, arm64,
riscv64, loong64, ppc64le, s390x) and three OSes. See the
[roadmap](https://go-embedded-ruby.github.io/docs/roadmap/) for what's next
(Fiber/Enumerator/lazy, hooks and string `eval`, the `rbgo build`
toolchain).

## Quick start

Requires **Go 1.26.4+**.

```bash
# run a one-liner
go run ./cmd/rbgo run -e 'puts 1 + 2'        # => 3

# run a file
cat > fib.rb <<'RB'
def fib(n)
  if n < 2
    n
  else
    fib(n - 1) + fib(n - 2)
  end
end
puts fib(20)
RB
go run ./cmd/rbgo run fib.rb                  # => 6765

# build the CLI
go build -o rbgo ./cmd/rbgo
./rbgo fib.rb
```

## Layout

```
cmd/rbgo/            CLI: run (build | repl arrive later)
internal/
  token/             token kinds (carry SpaceBefore = MRI's spaceSeen)
  lexer/             stateful lexer (lexState seed, SpaceBefore)
  ast/               AST nodes
  parser/            recursive descent + Pratt; scope stack for locals
  compiler/          AST → bytecode (ISeq), local-slot resolution
  bytecode/          instruction set + ISeq
  vm/                stack-machine interpreter, arithmetic, builtins
  object/            Value interface + concrete value types
docs/                plan-rbgo.md (the roadmap)
```

## Testing

```bash
go test ./...
go test -coverpkg=./internal/... -coverprofile=cov.out ./internal/...
go tool cover -func=cov.out | tail -1
```

If a parent `go.work` is present, prefix commands with `GOWORK=off`.

## Design & roadmap

See **[docs/plan-rbgo.md](docs/plan-rbgo.md)** for the full architecture, the
9-phase plan (Phase 0 vertical slice → Phase 8 conformance & performance), the
risk register, and the decision journal. The regexp engine is developed
separately as a pure-Go reimplementation of Onigmo in
[go-onigmo/regexp](https://github.com/go-onigmo/regexp).

## License

BSD-3-Clause. See [LICENSE](LICENSE).
