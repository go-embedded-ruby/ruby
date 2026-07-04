// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"

	libpuma "github.com/go-ruby-puma/puma"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// pumaExec runs src on an existing VM (REPL-style), so a test can start a server
// in one step and then drive requests against it in the next while the VM state
// (the $srv global) persists.
func pumaExec(t *testing.T, vm *VM, src string) {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := vm.Run(iseq); err != nil {
		t.Fatalf("run %q: %v", src, err)
	}
}

// pumaWithGVLReleased releases the VM's GVL around fn, modelling the main Ruby
// thread being parked at a blocking point (Kernel#sleep / Thread#join) while the
// server's request-handler goroutine enters the VM to run the Rack app. Without
// this the handler's pumaServe would block forever on the GVL the test goroutine
// holds, so it is exactly how a running server is driven from Ruby.
func pumaWithGVLReleased(vm *VM, fn func()) {
	vm.gvl.Unlock()
	defer vm.gvl.Lock()
	fn()
}

// pumaRecover asserts fn raises a Ruby exception of the given class.
func pumaRecover(t *testing.T, wantClass string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected raise %s, got none", wantClass)
		}
		re, ok := r.(RubyError)
		if !ok {
			t.Fatalf("expected a RubyError, got %#v", r)
		}
		if re.Class != wantClass {
			t.Fatalf("raised %s, want %s", re.Class, wantClass)
		}
	}()
	fn()
}

// TestPumaServeRoundTrip starts a real server on 127.0.0.1:0, issues an
// in-process HTTP request and asserts the Ruby Rack app's response — the status,
// the multi-valued header and a body built from PATH_INFO and the drained
// rack.input — flow back over the wire, then stops the server cleanly.
func TestPumaServeRoundTrip(t *testing.T) {
	vm := New(&bytes.Buffer{})
	pumaExec(t, vm, `
require "puma"
$app = ->(env) {
  data = env["rack.input"].read
  [200, {"Content-Type" => "text/plain", "X-Multi" => ["a", "b"]}, ["hi " + env["PATH_INFO"] + " " + data]]
}
$srv = Puma::Server.new($app)
$srv.run("127.0.0.1", 0)
`)
	addr := vm.globals["$srv"].(*PumaServer).address()

	var (
		status int
		body   string
		multi  []string
	)
	pumaWithGVLReleased(vm, func() {
		resp, err := http.Post("http://"+addr+"/world", "text/plain", strings.NewReader("payload"))
		if err != nil {
			t.Errorf("post: %v", err)
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		status, body, multi = resp.StatusCode, string(b), resp.Header["X-Multi"]
	})
	pumaExec(t, vm, `$srv.stop`)

	if status != 200 {
		t.Errorf("status = %d, want 200", status)
	}
	if body != "hi /world payload" {
		t.Errorf("body = %q, want %q", body, "hi /world payload")
	}
	if len(multi) != 2 || multi[0] != "a" || multi[1] != "b" {
		t.Errorf("X-Multi = %v, want [a b]", multi)
	}
}

// TestPumaServeStringBody covers the String (single-chunk) Rack body path and the
// scalar (single-value) header path through a real request.
func TestPumaServeStringBody(t *testing.T) {
	vm := New(&bytes.Buffer{})
	pumaExec(t, vm, `
require "puma"
$srv = Puma::Server.new(->(env){ [201, {"Content-Type" => "text/plain"}, "one-shot"] })
$srv.run("127.0.0.1", 0)
`)
	addr := vm.globals["$srv"].(*PumaServer).address()
	var status int
	var body string
	pumaWithGVLReleased(vm, func() {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			t.Errorf("get: %v", err)
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		status, body = resp.StatusCode, string(b)
	})
	pumaExec(t, vm, `$srv.stop`)
	if status != 201 || body != "one-shot" {
		t.Errorf("status=%d body=%q, want 201 %q", status, body, "one-shot")
	}
}

// TestPumaServeRaise covers the lowlevel-error path: a Rack app that raises
// unwinds to puma's 500 response, whose body carries the raised message.
func TestPumaServeRaise(t *testing.T) {
	vm := New(&bytes.Buffer{})
	pumaExec(t, vm, `
require "puma"
$srv = Puma::Server.new(->(env){ raise "boom" })
$srv.run("127.0.0.1", 0)
`)
	addr := vm.globals["$srv"].(*PumaServer).address()
	var status int
	var body string
	pumaWithGVLReleased(vm, func() {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			t.Errorf("get: %v", err)
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		status, body = resp.StatusCode, string(b)
	})
	pumaExec(t, vm, `$srv.stop`)
	if status != 500 {
		t.Errorf("status = %d, want 500", status)
	}
	if !strings.Contains(body, "Puma caught this error") || !strings.Contains(body, "boom") {
		t.Errorf("body = %q, want it to mention the caught error and \"boom\"", body)
	}
}

// TestPumaServeBadTriple covers the malformed-response path: a Rack app that does
// not return a [status, headers, body] Array raises Puma::Error, rendered as a
// 500 by the lowlevel-error handler.
func TestPumaServeBadTriple(t *testing.T) {
	vm := New(&bytes.Buffer{})
	pumaExec(t, vm, `
require "puma"
$srv = Puma::Server.new(->(env){ 42 })
$srv.run("127.0.0.1", 0)
`)
	addr := vm.globals["$srv"].(*PumaServer).address()
	var status int
	pumaWithGVLReleased(vm, func() {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			t.Errorf("get: %v", err)
			return
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		status = resp.StatusCode
	})
	pumaExec(t, vm, `$srv.stop`)
	if status != 500 {
		t.Errorf("status = %d, want 500", status)
	}
}

// TestPumaEnvValue covers pumaEnvValue / pumaReadAll across every branch,
// including the ones an ordinary request never produces (Integer / Float / nil
// env values and a non-reader rack.input), which are reachable only through a
// synthetic env.
func TestPumaEnvValue(t *testing.T) {
	vm := New(&bytes.Buffer{})
	for _, c := range []struct {
		key  string
		v    any
		want string
	}{
		{"REQUEST_METHOD", "GET", `"GET"`},
		{"rack.multithread", true, "true"},
		{"n", 7, "7"},
		{"n64", int64(9), "9"},
		{"f", 3.5, "3.5"},
		{"rack.version", []int{1, 6}, "[1, 6]"},
		{"absent", nil, "nil"},
		{"other", struct{}{}, `"{}"`},
	} {
		if got := vm.pumaEnvValue(c.key, c.v).Inspect(); got != c.want {
			t.Errorf("pumaEnvValue(%q, %#v) = %s, want %s", c.key, c.v, got, c.want)
		}
	}

	// rack.input drains a reader into the backing StringIO buffer.
	in := vm.pumaEnvValue("rack.input", strings.NewReader("body")).(*IOObj)
	if string(in.buf) != "body" {
		t.Errorf("rack.input buf = %q, want %q", in.buf, "body")
	}
	// A non-reader rack.input yields an empty StringIO rather than panicking.
	if in2 := vm.pumaEnvValue("rack.input", 123).(*IOObj); len(in2.buf) != 0 {
		t.Errorf("non-reader rack.input buf = %q, want empty", in2.buf)
	}
	// rack.errors maps to the VM's $stderr IO.
	if _, ok := vm.pumaEnvValue("rack.errors", nil).(*IOObj); !ok {
		t.Errorf("rack.errors did not map to an IO")
	}

	// pumaEnvHash wraps the whole map.
	h := vm.pumaEnvHash(map[string]any{"PATH_INFO": "/x"})
	if v, _ := h.Get(object.NewString("PATH_INFO")); v.Inspect() != `"/x"` {
		t.Errorf("pumaEnvHash PATH_INFO = %v", v)
	}
}

// TestPumaResultTuple covers the Rack response translation: a valid triple maps
// to the Go tuple (with scalar and Array-valued headers), and a non-triple raises
// Puma::Error.
func TestPumaResultTuple(t *testing.T) {
	headers := object.NewHash()
	headers.Set(object.NewString("A"), object.NewString("1"))
	headers.Set(object.NewString("B"), object.NewArrayFromSlice([]object.Value{object.NewString("x"), object.NewString("y")}))
	body := object.NewArrayFromSlice([]object.Value{object.NewString("hello")})
	triple := object.NewArrayFromSlice([]object.Value{object.IntValue(201), headers, body})

	st, h, b := pumaResultTuple(triple)
	if st != 201 {
		t.Errorf("status = %d, want 201", st)
	}
	if len(h["A"]) != 1 || h["A"][0] != "1" {
		t.Errorf("header A = %v", h["A"])
	}
	if len(h["B"]) != 2 || h["B"][0] != "x" || h["B"][1] != "y" {
		t.Errorf("header B = %v", h["B"])
	}
	if len(b) != 1 || string(b[0]) != "hello" {
		t.Errorf("body = %q", b)
	}

	pumaRecover(t, "Puma::Error", func() { pumaResultTuple(object.IntValue(5)) })
	pumaRecover(t, "Puma::Error", func() {
		pumaResultTuple(object.NewArrayFromSlice([]object.Value{object.IntValue(1)}))
	})
}

// TestPumaBodyAndHeaders covers the remaining pumaBody / pumaHeaders branches: a
// String body, a stringified fallback body, and a non-Hash headers value.
func TestPumaBodyAndHeaders(t *testing.T) {
	if b := pumaBody(object.NewString("s")); len(b) != 1 || string(b[0]) != "s" {
		t.Errorf("String body = %q", b)
	}
	if b := pumaBody(object.IntValue(9)); len(b) != 1 || string(b[0]) != "9" {
		t.Errorf("fallback body = %q", b)
	}
	if h := pumaHeaders(object.NewString("not-a-hash")); len(h) != 0 {
		t.Errorf("non-Hash headers = %v, want empty", h)
	}
}

// TestPumaOptions covers pumaOptions across a Hash (per-key overrides, plus a
// String key that matches nothing), a Configuration, and the TypeError tail; and
// pumaKey / pumaInt across their Symbol/String/other and Integer/Bignum/error
// branches.
func TestPumaOptions(t *testing.T) {
	h := object.NewHash()
	h.Set(object.Symbol("min_threads"), object.IntValue(2))
	h.Set(object.Symbol("max_threads"), object.IntValue(6))
	h.Set(object.Symbol("workers"), object.IntValue(4))
	h.Set(object.Symbol("environment"), object.NewString("prod"))
	h.Set(object.NewString("ignored"), object.IntValue(1)) // String key, unmatched
	o := pumaOptions(h)
	if o.MinThreads != 2 || o.MaxThreads != 6 || o.Workers != 4 || o.Environment != "prod" {
		t.Errorf("pumaOptions(Hash) = %+v", o)
	}

	cfg := &PumaConfiguration{cfg: libpuma.NewConfiguration()}
	if pumaOptions(cfg).MaxThreads != libpuma.DefaultOptions().MaxThreads {
		t.Errorf("pumaOptions(Configuration) did not return the config options")
	}

	pumaRecover(t, "TypeError", func() { pumaOptions(object.IntValue(3)) })

	// pumaKey.
	if pumaKey(object.Symbol("a")) != "a" || pumaKey(object.NewString("b")) != "b" || pumaKey(object.IntValue(5)) != "5" {
		t.Errorf("pumaKey mismatch")
	}
	// pumaInt.
	if pumaInt(object.IntValue(3)) != 3 {
		t.Errorf("pumaInt(Integer) mismatch")
	}
	if pumaInt(&object.Bignum{I: big.NewInt(11)}) != 11 {
		t.Errorf("pumaInt(Bignum) mismatch")
	}
	pumaRecover(t, "TypeError", func() { pumaInt(object.NewString("x")) })
}

// TestPumaWrapperStrings covers the object.Value ToS/Inspect/Truthy protocol on
// every Puma wrapper type.
func TestPumaWrapperStrings(t *testing.T) {
	for _, w := range []object.Value{
		&PumaServer{}, &PumaThreadPool{}, &PumaConfiguration{}, &PumaDSL{},
	} {
		if w.ToS() == "" || w.Inspect() == "" || !w.Truthy() {
			t.Errorf("%T: unexpected ToS/Inspect/Truthy", w)
		}
	}
}

// TestPumaErrMsg covers pumaErrMsg for a RubyError with a message, a RubyError
// with none (falls back to its class), and a non-RubyError recovered value.
func TestPumaErrMsg(t *testing.T) {
	if got := pumaErrMsg(RubyError{Class: "RuntimeError", Message: "boom"}); got != "boom" {
		t.Errorf("pumaErrMsg(msg) = %q", got)
	}
	if got := pumaErrMsg(RubyError{Class: "Puma::Error"}); got != "Puma::Error" {
		t.Errorf("pumaErrMsg(no msg) = %q", got)
	}
	if got := pumaErrMsg("plain"); got != "plain" {
		t.Errorf("pumaErrMsg(plain) = %q", got)
	}
}
