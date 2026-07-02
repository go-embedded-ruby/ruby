// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strconv"
	"strings"

	sinatra "github.com/go-ruby-sinatra/sinatra"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// sinatraClassOf resolves the Sinatra::Base subclass a DSL/#call invocation
// targets: the receiver itself when the DSL runs in a class body (self = the
// class), or the receiver's class when #call runs on an instance (App.new.call).
func sinatraClassOf(self object.Value) *RClass {
	switch v := self.(type) {
	case *RClass:
		return v
	case *RObject:
		return v.class
	}
	raise("TypeError", "not a Sinatra::Base")
	return nil
}

// sinatraVerbName maps an HTTP method to its lower-case DSL method name.
func sinatraVerbName(method string) string { return strings.ToLower(method) }

// sinatraPattern reads a route pattern argument, defaulting to "/" (Sinatra's
// empty-path default).
func sinatraPattern(args []object.Value) string {
	if len(args) == 0 {
		return "/"
	}
	return sinatraStr(args[0])
}

// sinatraOptPattern reads an optional filter pattern: absent yields "" (run on
// every request), matching before/after with no argument.
func sinatraOptPattern(args []object.Value) string {
	if len(args) == 0 {
		return ""
	}
	return sinatraStr(args[0])
}

// sinatraInt reads an argument as an int, falling back to def.
func sinatraInt(v object.Value, def int) int {
	switch n := v.(type) {
	case object.Integer:
		return int(n)
	case *object.String:
		if i, err := strconv.Atoi(n.Str()); err == nil {
			return i
		}
	}
	return def
}

// sinatraParamsHash snapshots the context params as a Ruby Hash keyed by String,
// preserving Sinatra's deterministic ordering. Scalar values are Strings; splat/
// captures are Arrays of Strings.
func sinatraParamsHash(c *sinatra.Context) *object.Hash {
	h := object.NewHash()
	for _, p := range c.Params() {
		h.Set(object.NewString(p.Key), rackFromGo(p.Value))
	}
	return h
}

// sinatraHaltArgs maps the arguments of a `halt` call into the []any the library
// consumes: an Integer sets the status, a String the body, an Array of Strings
// the body parts; anything else is ignored (Sinatra's halt is lenient).
func sinatraHaltArgs(args []object.Value) []any {
	out := make([]any, 0, len(args))
	for _, a := range args {
		switch n := a.(type) {
		case object.Integer:
			out = append(out, int(n))
		case *object.String:
			out = append(out, n.Str())
		case *object.Array:
			parts := make([]string, len(n.Elems))
			for i, el := range n.Elems {
				parts[i] = sinatraStr(el)
			}
			out = append(out, parts)
		}
	}
	return out
}

// sinatraSetContentType applies a `content_type` call: the first argument is the
// type (a Symbol such as :json, an extension, or a full media type); a trailing
// options Hash may carry charset:. An unknown media type raises, mirroring
// Sinatra — the library signals it by panicking *sinatra.UnknownMediaType, which
// this recovers and re-raises as a Ruby error (control-flow halts still unwind).
func sinatraSetContentType(c *sinatra.Context, args []object.Value) {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
	}
	typeArg := sinatraStr(args[0])
	charset := ""
	if len(args) > 1 {
		if h, ok := args[len(args)-1].(*object.Hash); ok {
			if v, ok := h.Get(object.Symbol("charset")); ok {
				charset = sinatraStr(v)
			}
		}
	}
	// ContentTypeCharset's only failure mode is a panic of *UnknownMediaType (an
	// unrecognised type), which is turned into a Ruby raise so application code
	// sees the error exactly as under Sinatra rather than an aborted process.
	defer func() {
		if r := recover(); r != nil {
			raise("RuntimeError", "%v", r)
		}
	}()
	c.ContentTypeCharset(typeArg, charset)
}

// sinatraSettingValue maps a Ruby settings value into the Go value the library's
// Settings registry holds (string / bool / int, else its to_s).
func sinatraSettingValue(v object.Value) any {
	switch n := v.(type) {
	case object.Bool:
		return bool(n)
	case object.Integer:
		return int(n)
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	}
	return v.ToS()
}

// sinatraResult maps a route/filter block's return value into Sinatra's body
// coercion: a String is the body, an Integer a status, an Array of Strings the
// body parts, nil leaves the body as helpers set it; any other value is rendered
// through its Ruby #to_s.
func sinatraResult(vm *VM, ret object.Value) any {
	switch n := ret.(type) {
	case nil, object.Nil:
		return nil
	case *object.String:
		return n.Str()
	case object.Integer:
		return int(n)
	case *object.Array:
		parts := make([]string, len(n.Elems))
		for i, el := range n.Elems {
			parts[i] = sinatraToS(vm, el)
		}
		return parts
	}
	return sinatraToS(vm, ret)
}

// sinatraToS renders a value through Ruby #to_s (so a custom object's body is
// what the app would render), used verbatim for a String.
func sinatraToS(vm *VM, v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	return vm.send(v, "to_s", nil, nil).ToS()
}
