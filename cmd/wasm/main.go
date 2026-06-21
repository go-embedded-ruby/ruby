// Command wasm is the browser front-end for the embedded-ruby interpreter.
//
// Built for GOOS=js GOARCH=wasm, it publishes two functions on the JS global
// object so a page (see web/index.html) can drive the interpreter:
//
//	rbgoEval(src)            → {output, value, error}
//	    Compile and run Ruby, returning captured stdout, the Inspect() of the
//	    program's result value, and any error message.
//
//	rbgoImage(src, bytes)    → {output, value, error, bytes}
//	    Bind INPUT to a String of the raw image bytes (a Uint8Array from the
//	    page), run src, and — when the program's result is a String (e.g. the
//	    output of Image#to_png) — return those bytes as a Uint8Array so the page
//	    can paint them onto a <canvas>. This is the cgo-free image pipeline
//	    (go-images) running entirely in the browser.
//
// A native build stub lives in stub.go so `go build ./...` stays green off-wasm.
//
//go:build js && wasm

package main

import (
	"bytes"
	"syscall/js"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-embedded-ruby/ruby/internal/vm"
	"github.com/go-ruby-parser/parser"
)

// evalRuby compiles and runs src against a fresh VM seeded with the given
// top-level constants. It returns captured stdout, the result value (nil on a
// parse/compile/runtime error) and an error string ("" on success).
func evalRuby(src string, seed map[string]object.Value) (string, object.Value, string) {
	prog, err := parser.Parse(src)
	if err != nil {
		return "", nil, err.Error()
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		return "", nil, err.Error()
	}
	var buf bytes.Buffer
	machine := vm.New(&buf)
	for k, v := range seed {
		machine.SetConst(k, v)
	}
	val, err := machine.Run(iseq)
	if err != nil {
		return buf.String(), nil, err.Error()
	}
	return buf.String(), val, ""
}

// result builds the common {output, value, error} JS object.
func result(out string, val object.Value, errStr string) map[string]any {
	res := map[string]any{"output": out, "error": errStr}
	if val != nil {
		res["value"] = val.Inspect()
	}
	return res
}

// rbgoEval is the JS entry point for the Ruby REPL.
func rbgoEval(_ js.Value, args []js.Value) any {
	out, val, errStr := evalRuby(args[0].String(), nil)
	return js.ValueOf(result(out, val, errStr))
}

// rbgoImage is the JS entry point for the image pipeline. args[1] is a
// Uint8Array of the input image's encoded bytes.
func rbgoImage(_ js.Value, args []js.Value) any {
	in := args[1]
	buf := make([]byte, in.Get("length").Int())
	js.CopyBytesToGo(buf, in)
	out, val, errStr := evalRuby(args[0].String(), map[string]object.Value{
		"INPUT": object.NewString(string(buf)),
	})
	res := result(out, val, errStr)
	if s, ok := val.(*object.String); ok {
		u8 := js.Global().Get("Uint8Array").New(len(s.B))
		js.CopyBytesToJS(u8, s.B)
		res["bytes"] = u8
	}
	return js.ValueOf(res)
}

func main() {
	js.Global().Set("rbgoEval", js.FuncOf(rbgoEval))
	js.Global().Set("rbgoImage", js.FuncOf(rbgoImage))
	js.Global().Set("rbgoReady", true)
	select {} // keep the Go runtime alive so the exported funcs stay callable
}
