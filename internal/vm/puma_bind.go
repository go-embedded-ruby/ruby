// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"io"
	"net"

	libpuma "github.com/go-ruby-puma/puma"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the Rack seam between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-puma/puma server. The library owns
// the net/http listeners, the bounded thread pool and the HTTP<->Rack env
// translation; rbgo supplies the Rack application (a Ruby value answering #call)
// through the RackApp host seam and converts values at the boundary — an incoming
// request's Go env map into a Ruby Hash, and the app's returned
// [status, headers, body] triple back into the Go response tuple (see puma.go for
// the class/method registration).
//
// Threading model: rbgo runs bytecode under an emulated GVL (one Ruby thread at
// a time, see thread.go). The puma server invokes the Rack app from thread-pool
// goroutines, so pumaServe acquires the GVL before entering Ruby and installs a
// dedicated execution context for the request — every request is thereby
// serialized onto the VM, and the GVL is released again (via the deferred
// restore) the moment the app returns. A server is meant to be run while the main
// Ruby thread is parked at a blocking point (Kernel#sleep, Thread#join), which is
// when it has released the GVL for request handlers to acquire.

// PumaServer is the Ruby wrapper around a go-ruby-puma Server bound to a Ruby
// Rack app. addr is the listener address recorded by #run, nil before the server
// is started.
type PumaServer struct {
	vm   *VM
	app  object.Value
	srv  *libpuma.Server
	opts *libpuma.Options
	addr net.Addr
}

func (s *PumaServer) ToS() string     { return "#<Puma::Server>" }
func (s *PumaServer) Inspect() string { return "#<Puma::Server>" }
func (s *PumaServer) Truthy() bool    { return true }

// PumaThreadPool is the Ruby wrapper around a go-ruby-puma ThreadPool, the pool
// Server#thread_pool returns.
type PumaThreadPool struct{ tp *libpuma.ThreadPool }

func (p *PumaThreadPool) ToS() string     { return "#<Puma::ThreadPool>" }
func (p *PumaThreadPool) Inspect() string { return "#<Puma::ThreadPool>" }
func (p *PumaThreadPool) Truthy() bool    { return true }

// PumaConfiguration is the Ruby wrapper around a go-ruby-puma Configuration, the
// evaluated result of a Puma::Configuration.new config block.
type PumaConfiguration struct{ cfg *libpuma.Configuration }

func (c *PumaConfiguration) ToS() string     { return "#<Puma::Configuration>" }
func (c *PumaConfiguration) Inspect() string { return "#<Puma::Configuration>" }
func (c *PumaConfiguration) Truthy() bool    { return true }

// PumaDSL is the Ruby wrapper around a go-ruby-puma DSL, the receiver yielded to
// a Puma::Configuration.new block.
type PumaDSL struct{ d *libpuma.DSL }

func (d *PumaDSL) ToS() string     { return "#<Puma::DSL>" }
func (d *PumaDSL) Inspect() string { return "#<Puma::DSL>" }
func (d *PumaDSL) Truthy() bool    { return true }

// pumaRackApp adapts a Ruby Rack app (any value answering #call(env)) to the
// go-ruby-puma RackApp host seam. Call runs on a server thread-pool goroutine and
// hands off to pumaServe, which serializes onto the VM under the GVL.
type pumaRackApp struct {
	vm  *VM
	app object.Value
}

// Call implements libpuma.RackApp: it is the entry point the server calls, from a
// thread-pool goroutine, for each incoming request.
func (a *pumaRackApp) Call(env map[string]any) (int, map[string][]string, [][]byte) {
	return a.vm.pumaServe(a.app, env)
}

// newPumaServer builds a PumaServer for app with opts, wiring a lowlevel-error
// handler that renders a raise from the Ruby app as puma's 500 response (matching
// Puma::Server#lowlevel_error) rather than tearing down the connection.
func newPumaServer(vm *VM, app object.Value, opts *libpuma.Options) *PumaServer {
	opts.Lowlevel = func(err any, _ map[string]any) (int, map[string][]string, [][]byte) {
		return 500,
			map[string][]string{"Content-Type": {"text/plain"}},
			[][]byte{[]byte("Puma caught this error: " + pumaErrMsg(err))}
	}
	return &PumaServer{
		vm:   vm,
		app:  app,
		opts: opts,
		srv:  libpuma.NewServer(&pumaRackApp{vm: vm, app: app}, opts),
	}
}

// run binds a TCP listener and starts serving, mirroring Puma::Server#run. The
// host defaults to 127.0.0.1 and the port to 0 (an ephemeral port), so a test can
// start on 127.0.0.1:0 and read the bound address back via #port / #address.
func (s *PumaServer) run(args []object.Value) object.Value {
	host := "127.0.0.1"
	port := 0
	if len(args) >= 1 {
		host = rackStr(args[0])
	}
	if len(args) >= 2 {
		port = pumaInt(args[1])
	}
	addr, err := s.srv.AddTCPListener(host, port)
	if err != nil {
		raise("Puma::Error", "%s", err.Error())
	}
	s.addr = addr
	s.srv.Run()
	return s
}

// requireAddr returns the bound listener address, raising Puma::Error when the
// server has not been started (so #port / #url on an unstarted server is a clean
// Ruby error, not a nil dereference).
func (s *PumaServer) requireAddr() *net.TCPAddr {
	if s.addr == nil {
		raise("Puma::Error", "server is not running")
	}
	return s.addr.(*net.TCPAddr)
}

func (s *PumaServer) port() int       { return s.requireAddr().Port }
func (s *PumaServer) host() string    { return s.requireAddr().IP.String() }
func (s *PumaServer) address() string { return s.requireAddr().String() }
func (s *PumaServer) url() string     { return "http://" + s.address() }

// pumaServe runs the Rack app for one request, converting the Go env into a Ruby
// Hash, invoking the app's #call under the GVL and translating the returned
// [status, headers, body] triple back into the response tuple. It acquires the
// GVL and installs a fresh execution context so the request runs as its own VM
// thread; the deferred restore hands the GVL and the previous thread's context
// back, so a panic (a raise in the Ruby app) unwinds to the library's
// lowlevel-error handler with the VM left consistent.
func (vm *VM) pumaServe(app object.Value, goEnv map[string]any) (int, map[string][]string, [][]byte) {
	vm.gvl.Lock()
	prev := vm.currentThread
	prev.saveCtx(vm)
	thr := &RThread{status: "run", done: make(chan struct{}), locals: map[object.Value]object.Value{}, parked: true}
	thr.restoreCtx(vm)
	defer func() {
		prev.restoreCtx(vm)
		vm.gvl.Unlock()
	}()
	env := vm.pumaEnvHash(goEnv)
	result := vm.send(app, "call", []object.Value{env}, nil)
	return pumaResultTuple(result)
}

// pumaEnvHash builds the Ruby Rack `env` Hash from the Go env map the server
// produced for a request.
func (vm *VM) pumaEnvHash(env map[string]any) *object.Hash {
	h := object.NewHash()
	for k, v := range env {
		h.Set(object.NewString(k), vm.pumaEnvValue(k, v))
	}
	return h
}

// pumaEnvValue maps one Go env entry into its Ruby value. The two IO-typed SPEC
// keys are handled by name — rack.input becomes a StringIO over the request body
// (so `env['rack.input'].read` works) and rack.errors the VM's $stderr — and the
// remaining scalar keys map by Go type (string, bool, integer, float, the
// rack.version []int, nil), with any other value stringified.
func (vm *VM) pumaEnvValue(key string, v any) object.Value {
	switch key {
	case "rack.input":
		return &IOObj{cls: vm.consts["StringIO"].(*RClass), isStr: true, buf: pumaReadAll(v)}
	case "rack.errors":
		return vm.curStderr()
	}
	switch x := v.(type) {
	case string:
		return object.NewString(x)
	case bool:
		return object.Bool(x)
	case int:
		return object.IntValue(int64(x))
	case int64:
		return object.IntValue(x)
	case float64:
		return object.Float(x)
	case []int:
		out := make([]object.Value, len(x))
		for i, n := range x {
			out[i] = object.IntValue(int64(n))
		}
		return object.NewArrayFromSlice(out)
	case nil:
		return object.NilV
	}
	return object.NewString(fmt.Sprint(v))
}

// pumaReadAll drains the rack.input reader into the bytes backing its StringIO. A
// value that is not a reader (only reachable through a synthetic env) yields no
// bytes rather than panicking.
func pumaReadAll(v any) []byte {
	r, ok := v.(io.Reader)
	if !ok {
		return nil
	}
	data, _ := io.ReadAll(r)
	return data
}

// pumaResultTuple translates the Ruby Rack response — a [status, headers, body]
// Array — into the Go (status, headers, body) tuple the library writes back. A
// value that is not a three-element Array raises Puma::Error, which the server's
// lowlevel-error handler renders as a 500.
func pumaResultTuple(v object.Value) (int, map[string][]string, [][]byte) {
	arr, ok := v.(*object.Array)
	if !ok || len(arr.Elems) < 3 {
		raise("Puma::Error", "Rack app must return a [status, headers, body] triple, got %s", v.Inspect())
	}
	return pumaInt(arr.Elems[0]), pumaHeaders(arr.Elems[1]), pumaBody(arr.Elems[2])
}

// pumaHeaders maps the Rack headers Hash into the Go header map: each value is a
// String (one header value) or an Array of Strings (repeated header), mirroring
// Rack's header contract. A non-Hash headers value yields no headers.
func pumaHeaders(v object.Value) map[string][]string {
	out := map[string][]string{}
	h, ok := v.(*object.Hash)
	if !ok {
		return out
	}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		key := rackStr(k)
		if arr, ok := val.(*object.Array); ok {
			for _, e := range arr.Elems {
				out[key] = append(out[key], rackStr(e))
			}
			continue
		}
		out[key] = append(out[key], rackStr(val))
	}
	return out
}

// pumaBody maps the Rack response body into the Go [][]byte chunk sequence: an
// Array of parts (each stringified) models Rack's enumerable body, a String is a
// single chunk, and any other value is stringified into one chunk.
func pumaBody(v object.Value) [][]byte {
	switch b := v.(type) {
	case *object.Array:
		out := make([][]byte, len(b.Elems))
		for i, e := range b.Elems {
			out[i] = []byte(rackStr(e))
		}
		return out
	case *object.String:
		return [][]byte{b.Bytes()}
	}
	return [][]byte{[]byte(rackStr(v))}
}

// pumaOptions resolves Puma::Server.new's options argument into a go-ruby-puma
// Options: a Puma::Configuration contributes its evaluated options, and a Hash
// ({min_threads:, max_threads:, workers:, environment:}) overrides the puma
// defaults per key.
func pumaOptions(v object.Value) *libpuma.Options {
	switch o := v.(type) {
	case *PumaConfiguration:
		return o.cfg.Options()
	case *object.Hash:
		opts := libpuma.DefaultOptions()
		for _, k := range o.Keys {
			val, _ := o.Get(k)
			switch pumaKey(k) {
			case "min_threads":
				opts.MinThreads = pumaInt(val)
			case "max_threads":
				opts.MaxThreads = pumaInt(val)
			case "workers":
				opts.Workers = pumaInt(val)
			case "environment":
				opts.Environment = rackStr(val)
			}
		}
		return opts
	}
	raise("TypeError", "expected an options Hash or Puma::Configuration, got %s", v.Inspect())
	panic("unreachable")
}

// pumaOptionsHash renders a go-ruby-puma Options as the Ruby Hash
// Configuration#options returns, keyed by String in a stable order.
func pumaOptionsHash(opts *libpuma.Options) *object.Hash {
	h := object.NewHash()
	h.Set(object.NewString("min_threads"), object.IntValue(int64(opts.MinThreads)))
	h.Set(object.NewString("max_threads"), object.IntValue(int64(opts.MaxThreads)))
	h.Set(object.NewString("workers"), object.IntValue(int64(opts.Workers)))
	h.Set(object.NewString("environment"), object.NewString(opts.Environment))
	binds := make([]object.Value, len(opts.Binds))
	for i, b := range opts.Binds {
		binds[i] = object.NewString(b)
	}
	h.Set(object.NewString("binds"), object.NewArrayFromSlice(binds))
	return h
}

// pumaErrMsg renders a recovered lowlevel-error value for the 500 body: a Ruby
// raise (RubyError) contributes its message (or class name when it carries none),
// and any other recovered value is stringified.
func pumaErrMsg(err any) string {
	if re, ok := err.(RubyError); ok {
		if re.Message != "" {
			return re.Message
		}
		return re.Class
	}
	return fmt.Sprint(err)
}

// pumaFirst returns the single required argument of a DSL setter, raising
// ArgumentError when it was omitted.
func pumaFirst(args []object.Value, name string) object.Value {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1) for %s", name)
	}
	return args[0]
}

// pumaInt coerces a Ruby Integer argument to an int, raising TypeError for a
// non-Integer (a thread count / port / status must be an Integer).
func pumaInt(v object.Value) int {
	if n, ok := v.(object.Integer); ok {
		return int(n)
	}
	if n, ok := object.BigOf(v); ok {
		return int(n.Int64())
	}
	raise("TypeError", "expected an Integer, got %s", v.Inspect())
	panic("unreachable")
}

// pumaKey coerces a Ruby options key (Symbol or String) to its Go string name.
func pumaKey(v object.Value) string {
	switch k := v.(type) {
	case object.Symbol:
		return string(k)
	case *object.String:
		return k.Str()
	}
	return v.ToS()
}
