# rbgo WebAssembly playground

The whole interpreter — lexer, parser, compiler, bytecode VM — plus the numeric
stack (`NDArray`, `FFT`, `Complex`, `Rational`, `Math`, `Set`) and the cgo-free
image pipeline (`Image`, backed by [go-images](https://github.com/go-images/images))
compiled to a single `GOOS=js GOARCH=wasm` module. There is **no server-side
code**: everything runs in the browser tab.

## Build & run

```sh
./web/build.sh serve     # build web/rbgo.wasm and serve http://localhost:8080
```

Or build only (e.g. for static hosting / GitHub Pages):

```sh
./web/build.sh           # produces web/rbgo.wasm + web/wasm_exec.js
```

`rbgo.wasm` and `wasm_exec.js` are build artifacts (git-ignored); deploy the
`web/` directory after running the build.

## What the page exposes

The wasm module ([`cmd/wasm`](../cmd/wasm)) publishes two functions on the JS
global object:

| function | returns | used by |
|----------|---------|---------|
| `rbgoEval(src)` | `{output, value, error}` | the Ruby REPL |
| `rbgoImage(src, bytes)` | `{output, value, error, bytes}` | the image demo |

`rbgoImage` binds the input image's raw bytes to the Ruby constant `INPUT` (a
`String`), runs `src`, and — when the program's result is a `String` (e.g. the
output of `Image#to_png`) — hands those bytes back as a `Uint8Array` for the page
to paint onto a `<canvas>`. The image demo's pipeline is plain Ruby:

```ruby
Image.decode(INPUT).gaussian_blur(2.0).sobel_mag.to_png
```

Arbitrary REPL input can never crash the interpreter: a native binding that
faults on bad arguments is converted to a rescuable Ruby `ArgumentError` (see
`callNative` in `internal/vm`).
