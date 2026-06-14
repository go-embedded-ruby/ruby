# ruby — go-embedded-ruby

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-9B1C2E)](https://go-embedded-ruby.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Phase](https://img.shields.io/badge/phase-0%20vertical%20slice-1a7f37)](https://go-embedded-ruby.github.io/docs/phases/phase0/)

**A pure-Go implementation of Ruby — one static binary, full dynamism, zero cgo.**

This repository is the interpreter: a lexer, parser and compiler that lower Ruby
to bytecode, and a stack VM (mruby/YARV lineage) that runs it. The front-end is
**embedded in the binary**, so `eval` and runtime `require` keep working. Ruby
objects are Go heap objects, so **Go's garbage collector is reused**. Dispatch
goes through **mutable per-class method tables**, which is what makes
monkey-patching, `define_method` and `method_missing` free.

> 🌐 [Website](https://go-embedded-ruby.github.io) · 📚 [Documentation](https://go-embedded-ruby.github.io/docs/) · 🧭 [Roadmap](docs/plan-rbgo.md)

## Status — Phase 0 (vertical slice)

The whole chain exists, thin. Supported today:

- integers (`int64`), floats, strings; `true`/`false`/`nil`; `self`
- local variables; arithmetic (`+ - * / %`, **Ruby floor division**),
  comparison, equality, unary `-`/`!`
- `if`/`elsif`/`else`, `unless`, `while`/`until`, statement modifiers
  (`x if cond`)
- `def` with required parameters, recursion, implicit and explicit `return`
- `puts` / `print` / `p`

Behaviour is **differential-tested against MRI**; the `object` package is at
100% coverage and the suite covers the interpreter end to end.

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
