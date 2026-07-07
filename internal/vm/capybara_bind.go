// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"net/http"
	"strings"

	capybara "github.com/go-ruby-capybara/capybara"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires the Capybara App seam over github.com/go-ruby-capybara/capybara
// to a real Ruby Rack app. capybara's rack_test driver drives the app under test
// through an App func — App(*capybara.Request) *capybara.Response. The binding's
// App closure converts the Request into a Rack `env` Hash, sends #call(env) to the
// Ruby app inline under the GVL (the driver is invoked from a Session native
// method, so the VM already holds the GVL and runs on the current thread — no
// thread swap is needed), and converts the returned [status, headers, body]
// triple back into a *capybara.Response. It also holds the small value coercions
// the Session/Node methods share.

// capybaraApp builds the capybara.App seam for a Ruby Rack app: every request the
// driver makes becomes a Rack env Hash handed to app.call, whose [status,
// headers, body] triple is turned back into a Response. It runs inline — a raise
// in the Ruby app unwinds as a Ruby exception through the driver back to the
// Session method that started the navigation.
func (vm *VM) capybaraApp(app object.Value) capybara.App {
	return func(req *capybara.Request) *capybara.Response {
		result := vm.send(app, "call", []object.Value{vm.capybaraEnv(req)}, nil)
		return capybaraResponse(result)
	}
}

// capybaraEnv renders a capybara.Request as the Ruby Rack `env` Hash. The string
// CGI/rack keys come from the library's RackEnv (REQUEST_METHOD, PATH_INFO,
// QUERY_STRING, HTTP_* headers, CONTENT_TYPE/LENGTH, …); rack.input is upgraded
// from the raw body string into a StringIO so `env['rack.input'].read` works, and
// rack.errors / rack.version / a default SERVER_PORT are added so a real Rack app
// (Sinatra, a lambda) sees a complete env.
func (vm *VM) capybaraEnv(req *capybara.Request) *object.Hash {
	h := object.NewHash()
	for k, v := range req.RackEnv() {
		if k == "rack.input" {
			continue
		}
		h.Set(object.NewString(k), object.NewString(v))
	}
	h.Set(object.NewString("rack.input"), &IOObj{cls: vm.consts["StringIO"].(*RClass), isStr: true, buf: []byte(req.Body)})
	h.Set(object.NewString("rack.errors"), vm.curStderr())
	h.Set(object.NewString("rack.version"), object.NewArray(object.IntValue(1), object.IntValue(3)))
	if _, ok := h.Get(object.NewString("SERVER_PORT")); !ok {
		h.Set(object.NewString("SERVER_PORT"), object.NewString("80"))
	}
	return h
}

// capybaraResponse converts the Ruby Rack response — a [status, headers, body]
// Array — into a *capybara.Response. The status/headers/body decoding is shared
// with the Puma binding (pumaInt/pumaHeaders/pumaBody); a value that is not a
// three-element Array raises Capybara::CapybaraError, matching a Rack contract
// violation.
func capybaraResponse(v object.Value) *capybara.Response {
	arr, ok := v.(*object.Array)
	if !ok || len(arr.Elems) < 3 {
		raise("Capybara::CapybaraError", "Rack app must return a [status, headers, body] triple, got %s", v.Inspect())
	}
	header := http.Header{}
	for k, vals := range pumaHeaders(arr.Elems[1]) {
		for _, s := range vals {
			header.Add(k, s)
		}
	}
	var body strings.Builder
	for _, chunk := range pumaBody(arr.Elems[2]) {
		body.Write(chunk)
	}
	return &capybara.Response{Status: pumaInt(arr.Elems[0]), Header: header, Body: body.String()}
}

// capSession unwraps the receiver into its *capybara.Session.
func capSession(v object.Value) *capybara.Session { return v.(*CapybaraSession).sess }

// capNode unwraps the receiver into its *capybara.Node.
func capNode(v object.Value) *capybara.Node { return v.(*CapybaraNode).node }

// capNode1 wraps one *capybara.Node in its Ruby class.
func (vm *VM) capNode1(n *capybara.Node) object.Value {
	return &CapybaraNode{node: n, cls: vm.consts["Capybara::Node::Element"].(*RClass)}
}

// capNodeArray wraps a slice of nodes into a Ruby Array of Capybara::Node::Element.
func (vm *VM) capNodeArray(nodes []*capybara.Node) *object.Array {
	out := object.NewArrayFromSlice(make([]object.Value, len(nodes)))
	for i, n := range nodes {
		out.Elems[i] = vm.capNode1(n)
	}
	return out
}

// capStr coerces an argument to its String contents (a Symbol yields its name,
// any other value its to_s), mirroring how Capybara accepts String or Symbol
// locators/selectors.
func capStr(v object.Value) string {
	switch n := v.(type) {
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	}
	return v.ToS()
}

// capNil reports whether v is Ruby nil (object.NilV), used to fall back to
// Capybara.app / to detect a missing app. Every value the binding inspects is a
// real object.Value (never a Go-nil interface), so the Nil type assertion is
// sufficient.
func capNil(v object.Value) bool {
	_, isNil := v.(object.Nil)
	return isNil
}

// capKwarg reads a String keyword argument (fill_in's `with:` / select's `from:`)
// from the trailing options Hash of args, matching the key by name so a Symbol or
// String key both resolve. A missing key yields "".
func capKwarg(args []object.Value, key string) string {
	h, ok := lastHash(args)
	if !ok {
		return ""
	}
	for _, k := range h.Keys {
		if capStr(k) == key {
			val, _ := h.Get(k)
			return capStr(val)
		}
	}
	return ""
}

// capErrClass maps a Go error the capybara library returns onto the Ruby error
// class name to raise. Every finder/matcher/action reachable through this binding
// returns one of these types; the trailing ElementNotFound is the finder default
// (capybara's ParseError is unreachable here because the library's HTML-parser
// seam is a fixed strings.Reader that cannot error).
func capErrClass(err error) string {
	switch err.(type) {
	case *capybara.Ambiguous:
		return "Capybara::Ambiguous"
	case *capybara.ExpectationNotMet:
		return "Capybara::ExpectationNotMet"
	case *capybara.UnselectableError:
		return "Capybara::UnselectableError"
	case *capybara.InfiniteRedirect:
		return "Capybara::InfiniteRedirectError"
	}
	return "Capybara::ElementNotFound"
}

// capyRaise raises the mapped Ruby exception for a non-nil library error, and is a
// no-op for nil (so `capyRaise(sess.Visit(...))` reads cleanly).
func capyRaise(err error) {
	if err == nil {
		return
	}
	raise(capErrClass(err), "%s", err.Error())
}

// capSessionStr adapts a Session accessor returning a string into a native method
// returning a Ruby String.
func capSessionStr(fn func(*capybara.Session) string) NativeFn {
	return func(_ *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(fn(capSession(v)))
	}
}

// capSessionAct adapts a Session action taking one locator and returning an error
// (click_*/choose/check/uncheck) into a native method returning the receiver.
func capSessionAct(fn func(*capybara.Session, string) error) NativeFn {
	return func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		capyRaise(fn(capSession(v), capStr(rackArg(args))))
		return v
	}
}

// capSessionFind adapts a Session finder returning (*Node, error) into a native
// method returning a wrapped Capybara::Node::Element.
func capSessionFind(fn func(*capybara.Session, string) (*capybara.Node, error)) NativeFn {
	return func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		n, err := fn(capSession(v), capStr(rackArg(args)))
		capyRaise(err)
		return vm.capNode1(n)
	}
}

// capSessionHas adapts a Session predicate returning a bool (has_*? / has_no_*?)
// into a native method returning a Ruby boolean.
func capSessionHas(fn func(*capybara.Session, string) bool) NativeFn {
	return func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(fn(capSession(v), capStr(rackArg(args))))
	}
}

// capSessionAssert adapts a Session assertion returning an error (assert_*) into a
// native method returning true, raising Capybara::ExpectationNotMet on failure.
func capSessionAssert(fn func(*capybara.Session, string) error) NativeFn {
	return func(_ *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		capyRaise(fn(capSession(v), capStr(rackArg(args))))
		return object.Bool(true)
	}
}
