// Native build stub: the real entry point is wasm-only (see main.go). This keeps
// `go build ./...` and `go test ./...` green on every host architecture.
//
//go:build !js || !wasm

package main

func main() {}
