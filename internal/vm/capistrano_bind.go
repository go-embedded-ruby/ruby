// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"

	capistrano "github.com/go-ruby-capistrano/capistrano"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent github.com/go-ruby-capistrano/capistrano
// library — the pure-Go (CGO=0) port of the core of Ruby's Capistrano (the
// deployment-automation framework), a sibling of the go-ruby-rake and
// go-ruby-thor bindings. The whole deterministic half of Capistrano lives in the
// library: the variable store and its lazy/memoized values, the server/role
// registry and its role filtering, the Rake-style task graph with before/after
// hooks and invoke-once / circular-dependency semantics, and the SSHKit-style
// execute/test/capture command semantics. rbgo re-expresses that surface as the
// Ruby DSL (see capistrano.go for the class + method registration) and converts
// values across the boundary here.
//
// Two seams cross into rbgo, and both run INLINE on the VM goroutine under the
// GVL:
//
//   - a task's action body and a before/after hook body (`task :deploy do … end`)
//     — captured as a *Proc, wrapped by capBody, and called by the library at the
//     point the invoke walk reaches the task (after its prerequisites);
//   - an on-block (`on(hosts) do execute :ls end`) — run once per matching host
//     with the Session bound as `self`, so execute/test/capture resolve as
//     methods on that Session.
//
// The effectful command backend is wired to the library's in-process FakeBackend
// (capBackend), so execute/capture/test are RECORDED and return scripted output
// rather than dialling a real host: the binding never opens a socket, spawns a
// goroutine, or leaks network state, and every run is reproducible. A host that
// wanted real deploys would inject an SSH backend over rbgo's socket layer, but
// the default keeps rbgo hermetic.

// The wrapper types. Each holds a pointer into the library and reports the
// matching Capistrano::* class (see classOf); the methods registered in
// capistrano.go operate on the held value.

// CapServerVal wraps a *capistrano.Server — a single deploy target
// (Capistrano::Server).
type CapServerVal struct{ s *capistrano.Server }

// CapSessionVal wraps a *capistrano.Session — the per-host execution context
// yielded to an on-block (Capistrano::Session).
type CapSessionVal struct{ s *capistrano.Session }

// CapTaskVal wraps a *capistrano.Task — one node in the task graph
// (Capistrano::Task).
type CapTaskVal struct{ t *capistrano.Task }

// CapBackendVal wraps the per-VM *capistrano.FakeBackend — the recording command
// backend (Capistrano::TestBackend), exposed so Ruby can script command output
// and inspect the recorded command log.
type CapBackendVal struct{ b *capistrano.FakeBackend }

func (v *CapServerVal) ToS() string     { return v.s.String() }
func (v *CapServerVal) Inspect() string { return "#<Capistrano::Server " + v.s.String() + ">" }
func (v *CapServerVal) Truthy() bool    { return true }

func (v *CapSessionVal) ToS() string     { return "#<Capistrano::Session " + v.s.Host().String() + ">" }
func (v *CapSessionVal) Inspect() string { return v.ToS() }
func (v *CapSessionVal) Truthy() bool    { return true }

func (v *CapTaskVal) ToS() string     { return v.t.Name() }
func (v *CapTaskVal) Inspect() string { return "#<Capistrano::Task " + v.t.Name() + ">" }
func (v *CapTaskVal) Truthy() bool    { return true }

func (v *CapBackendVal) ToS() string     { return "#<Capistrano::TestBackend>" }
func (v *CapBackendVal) Inspect() string { return "#<Capistrano::TestBackend>" }
func (v *CapBackendVal) Truthy() bool    { return true }

// capName coerces a Symbol / String (or any value via #to_s) to its plain string,
// matching Capistrano's Symbol-or-String variable / task / role names.
func capName(v object.Value) string {
	switch x := v.(type) {
	case object.Symbol:
		return string(x)
	case *object.String:
		return x.Str()
	}
	return v.ToS()
}

// capStrList maps a role / dependency / host value to a []string: an Array's
// elements by name, nil for nil, or a single scalar wrapped as one entry (the DSL
// accepts either a list or a bare name).
func capStrList(v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		out := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = capName(e)
		}
		return out
	}
	if object.IsNil(v) {
		return nil
	}
	return []string{capName(v)}
}

// capStringArgs maps positional command tokens (execute :git, "clone", url) to
// their string values.
func capStringArgs(args []object.Value) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = capName(a)
	}
	return out
}

// capProp maps a Ruby per-server / per-role property value to the Go form the
// library reads: a Ruby boolean becomes a Go bool (so `primary: true` /
// `no_release: true` drive Server#primary? / #no_release?), and any other value
// is kept as the Ruby object it is (Server#fetch hands it straight back).
func capProp(v object.Value) any {
	if b, ok := v.(object.Bool); ok {
		return bool(b)
	}
	return v
}

// capPropValue is the inverse of capProp for Server#fetch: a Go bool becomes a
// Ruby boolean, a kept Ruby value is returned unchanged, and an unset property
// (nil) reads as Ruby nil.
func capPropValue(v any) object.Value {
	if b, ok := v.(bool); ok {
		return object.Bool(b)
	}
	if ov, ok := v.(object.Value); ok {
		return ov
	}
	return object.NilV
}

// capOptions splits a trailing options Hash (`server host, roles: [...], **props`
// / `role name, hosts, **props`) into the :roles list and the remaining property
// bag. A nil / absent Hash yields no roles and an empty bag.
func capOptions(v object.Value) (roles []string, props map[string]any) {
	props = map[string]any{}
	h, ok := v.(*object.Hash)
	if !ok {
		return nil, props
	}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		if capName(k) == "roles" {
			roles = capStrList(val)
			continue
		}
		props[capName(k)] = capProp(val)
	}
	return roles, props
}

// capValue is the value stored by `set` / a `fetch` default: a Ruby block or Proc
// becomes a lazily-evaluated, memoized capistrano.Callable (its body run INLINE
// under the GVL on first fetch), and any other Ruby value is stored as-is. The
// Callable returns the block's Ruby result, so a later fetch reads it back
// unchanged.
func (vm *VM) capValue(v object.Value, blk *Proc) any {
	if blk != nil {
		return capistrano.Callable(func() any { return vm.callBlock(blk, nil) })
	}
	if p, ok := v.(*Proc); ok {
		return capistrano.Callable(func() any { return vm.callBlock(p, nil) })
	}
	return v
}

// capFetched renders a resolved configuration value back as a Ruby value: a
// missing key with no default is Go nil (Ruby nil), and every value the binding
// stores is a Ruby object.Value (a Callable is resolved by the library before it
// reaches here).
func capFetched(v any) object.Value {
	if v == nil {
		return object.NilV
	}
	return v.(object.Value)
}

// capBody wraps a task / hook Ruby block as a capistrano.TaskBody seam. The block
// is invoked INLINE (callBlock) when the library reaches the task in the invoke
// walk; a Ruby exception raised inside unwinds as a Go panic through the walk
// (matching MRI, where an exception escaping a task body aborts the run), so the
// body itself never returns an error. A nil block yields a nil body (a task with
// no action).
func (vm *VM) capBody(blk *Proc) capistrano.TaskBody {
	if blk == nil {
		return nil
	}
	return func() error {
		vm.callBlock(blk, nil)
		return nil
	}
}

// capHosts resolves the `on(hosts)` argument to the library's []*Server: a single
// Capistrano::Server (as primary(:db) returns), an Array of them (as roles(:web)
// returns), or anything else (nil / a non-server) as no hosts — which the library
// turns into a NoMatchingServersError.
func capHosts(v object.Value) []*capistrano.Server {
	switch x := v.(type) {
	case *CapServerVal:
		return []*capistrano.Server{x.s}
	case *object.Array:
		out := make([]*capistrano.Server, 0, len(x.Elems))
		for _, e := range x.Elems {
			if sv, ok := e.(*CapServerVal); ok {
				out = append(out, sv.s)
			}
		}
		return out
	}
	return nil
}

// wrapServers wraps a library server slice as a Ruby Array of Capistrano::Server.
func wrapServers(srvs []*capistrano.Server) object.Value {
	elems := make([]object.Value, len(srvs))
	for i, s := range srvs {
		elems[i] = &CapServerVal{s: s}
	}
	return object.NewArrayFromSlice(elems)
}

// wrapServer wraps a single library server (nil → Ruby nil, as primary(role)
// returns nil for an unknown role).
func wrapServer(s *capistrano.Server) object.Value {
	if s == nil {
		return object.NilV
	}
	return &CapServerVal{s: s}
}

// capRaise maps a library error onto its Ruby exception. Every error the library
// raises satisfies capistrano.CapistranoError; the binding switches on the
// concrete type to pick the matching Capistrano::* subclass, and falls back to
// the base Capistrano::Error for a plain transport error (a backend dial/IO
// failure, which is not part of the sealed tree).
func capRaise(err error) object.Value {
	switch err.(type) {
	case *capistrano.TaskNotFoundError:
		return raise("Capistrano::TaskNotFoundError", "%s", err.Error())
	case *capistrano.NoMatchingServersError:
		return raise("Capistrano::NoMatchingServersError", "%s", err.Error())
	case *capistrano.CommandError:
		return raise("Capistrano::CommandError", "%s", err.Error())
	case *capistrano.Error:
		return raise("Capistrano::Error", "%s", err.Error())
	}
	return raise("Capistrano::Error", "%s", err.Error())
}

// capErr builds a plain transport error from a Ruby message, for the
// TestBackend#fail_* failure-injection helpers.
func capErr(v object.Value) error { return errors.New(capName(v)) }
