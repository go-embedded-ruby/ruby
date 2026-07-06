// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	typhoeus "github.com/go-ruby-typhoeus/typhoeus"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// typhoeusTransport returns the terminal transport rbgo wires into every Typhoeus
// request — the library's net/http transport in production. Tests leave it at the
// default and drive an in-process httptest server over loopback, so the suite
// touches no external network (the Hydra parallelism included). The library owns
// the whole request/response/Hydra model around this seam; only the round-trip is
// host-provided.
var typhoeusTransport = func() typhoeus.Transport { return typhoeus.NetHTTP() }

// registerTyphoeus installs the Typhoeus module (require "typhoeus"): the
// module-level one-shot verbs (Typhoeus.get/post/…), the Typhoeus::Request
// (#on_complete/#run/#response), the Typhoeus::Response (#code/#body/#headers/
// #success?/#total_time/#return_code/#timed_out?), and the gem's signature
// Typhoeus::Hydra parallel runner (#queue/#run/#queued_count). The request model
// and the concurrent Hydra live in the github.com/go-ruby-typhoeus/typhoeus
// library, backed by net/http and goroutines instead of libcurl; this file is the
// class + method wiring (see typhoeus_bind.go for the wrappers and conversions).
func (vm *VM) registerTyphoeus() {
	mod := newClass("Typhoeus", nil)
	mod.isModule = true
	vm.consts["Typhoeus"] = mod

	cReq := newClass("Typhoeus::Request", vm.cObject)
	mod.consts["Request"] = cReq
	vm.consts["Typhoeus::Request"] = cReq
	cResp := newClass("Typhoeus::Response", vm.cObject)
	mod.consts["Response"] = cResp
	vm.consts["Typhoeus::Response"] = cResp
	cHydra := newClass("Typhoeus::Hydra", vm.cObject)
	mod.consts["Hydra"] = cHydra
	vm.consts["Typhoeus::Hydra"] = cHydra

	sm := func(name string, fn NativeFn) {
		mod.smethods[name] = &Method{name: name, owner: mod, native: fn}
	}
	for _, verb := range typhoeusVerbs {
		v := verb
		sm(v, func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			req := typhoeusBuild(args[0].ToS(), v, typhoeusHashAt(args, 1))
			return &TyphoeusResponse{req.Run()}
		})
	}

	vm.registerTyphoeusRequest(cReq)
	vm.registerTyphoeusResponse(cResp)
	vm.registerTyphoeusHydra(cHydra)
}

// typhoeusVerbs is the fixed set of HTTP verbs rbgo exposes on the module; the
// upper-cased name is threaded into the library Request.
var typhoeusVerbs = []string{"get", "post", "put", "delete", "head", "patch"}

// typhoeusBuild constructs a library Request bound to the seam transport from a
// URL, an upper-case-ready method and an optional Ruby options Hash.
func typhoeusBuild(url, method string, opts *object.Hash) *typhoeus.Request {
	req := typhoeus.NewRequest(url, typhoeusMethod(method), rubyHashToTyphoeusOptions(opts))
	req.Transport = typhoeusTransport()
	return req
}

// typhoeusMethod upper-cases a verb ("get" → "GET") the way libcurl/Typhoeus
// expects it on the wire.
func typhoeusMethod(m string) string {
	b := []byte(m)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 'a' - 'A'
		}
	}
	return string(b)
}

// registerTyphoeusRequest installs the Typhoeus::Request surface: the constructor
// (Typhoeus::Request.new(url, method:, …)), the on_complete callback, and run/
// response.
func (vm *VM) registerTyphoeusRequest(c *RClass) {
	reqOf := func(self object.Value) *typhoeus.Request { return self.(*TyphoeusRequest).r }

	c.smethods["new"] = &Method{name: "new", owner: c, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		h := typhoeusHashAt(args, 1)
		method := "get"
		if h != nil {
			if v, ok := h.Get(object.Symbol("method")); ok {
				method = typhoeusName(v)
			}
		}
		return &TyphoeusRequest{typhoeusBuild(args[0].ToS(), method, h)}
	}}
	c.define("on_complete", func(vm *VM, self object.Value, _ []object.Value, blk *Proc) object.Value {
		req := reqOf(self)
		req.OnComplete(func(r *typhoeus.Response) {
			vm.callBlock(blk, []object.Value{&TyphoeusResponse{r}})
		})
		return self
	})
	c.define("run", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &TyphoeusResponse{reqOf(self).Run()}
	})
	c.define("response", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return typhoeusResponseOrNil(reqOf(self).Response())
	})
	c.define("url", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(reqOf(self).URL)
	})
}

// typhoeusResponseOrNil wraps a response, or returns nil when the request has not
// run yet (Typhoeus::Request#response is nil before #run).
func typhoeusResponseOrNil(r *typhoeus.Response) object.Value {
	if r == nil {
		return object.NilV
	}
	return &TyphoeusResponse{r}
}

// registerTyphoeusResponse installs the read-only Typhoeus::Response surface.
func (vm *VM) registerTyphoeusResponse(c *RClass) {
	respOf := func(self object.Value) *typhoeus.Response { return self.(*TyphoeusResponse).r }

	code := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(respOf(self).Code))
	}
	c.define("code", code)
	c.define("response_code", code)
	c.define("body", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(respOf(self).Body)
	})
	c.define("headers", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return typhoeusHeadersHash(respOf(self).Headers)
	})
	c.define("total_time", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Float(respOf(self).TotalTime)
	})
	c.define("return_code", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Symbol(respOf(self).ReturnCode.String())
	})
	c.define("return_message", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(respOf(self).ReturnMessage())
	})
	c.define("success?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(respOf(self).Success())
	})
	c.define("timed_out?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(respOf(self).TimedOut())
	})
}

// registerTyphoeusHydra installs the Typhoeus::Hydra parallel runner surface: the
// constructor (Typhoeus::Hydra.new(max_concurrency:)), queue, run and
// queued_count.
func (vm *VM) registerTyphoeusHydra(c *RClass) {
	hydraOf := func(self object.Value) *typhoeus.Hydra { return self.(*TyphoeusHydra).h }

	c.smethods["new"] = &Method{name: "new", owner: c, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		var opts typhoeus.HydraOptions
		if h := typhoeusHashAt(args, 0); h != nil {
			if v, ok := h.Get(object.Symbol("max_concurrency")); ok {
				opts.MaxConcurrency = int(toInt(v))
			}
		}
		return &TyphoeusHydra{typhoeus.NewHydra(opts)}
	}}
	c.define("queue", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		hydraOf(self).Queue(args[0].(*TyphoeusRequest).r)
		return self
	})
	c.define("run", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		hydraOf(self).Run()
		return object.NilV
	})
	c.define("queued_count", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(hydraOf(self).QueuedCount()))
	})
}
