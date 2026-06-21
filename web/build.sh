#!/usr/bin/env sh
# Build the rbgo WebAssembly playground.
#
#   ./web/build.sh          build web/rbgo.wasm + copy the JS glue
#   ./web/build.sh serve    build, then serve the playground on :8080
#
# The interpreter, the numeric stack (NDArray, FFT) and the cgo-free image
# pipeline (go-images) all run in the browser — there is no server-side code.
set -eu

cd "$(dirname "$0")/.."
export GOWORK=off

echo "building web/rbgo.wasm …"
GOOS=js GOARCH=wasm go build -o web/rbgo.wasm ./cmd/wasm

# Copy Go's wasm loader glue from the active toolchain.
GOROOT="$(go env GOROOT)"
if [ -f "$GOROOT/lib/wasm/wasm_exec.js" ]; then
	cp "$GOROOT/lib/wasm/wasm_exec.js" web/wasm_exec.js
else
	cp "$GOROOT/misc/wasm/wasm_exec.js" web/wasm_exec.js   # Go < 1.24 layout
fi

echo "done: web/rbgo.wasm ($(wc -c < web/rbgo.wasm | tr -d ' ') bytes), web/wasm_exec.js"

if [ "${1:-}" = "serve" ]; then
	echo "serving http://localhost:8080/ (Ctrl-C to stop)"
	cd web
	# wasm needs the application/wasm MIME type; Go's http.FileServer sets it.
	exec go run - <<'GO'
package main

import "net/http"

func main() { http.ListenAndServe(":8080", http.FileServer(http.Dir("."))) }
GO
fi
