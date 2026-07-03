// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-cgi/cgi"
)

// registerCGI installs the CGI module (require "cgi"), backed by the standalone
// github.com/go-ruby-cgi/cgi library so the escaping/parsing surface stays
// byte-for-byte compatible with MRI 4.0.5 (the cgi/escape default plus the cgi
// gem's CGI.parse) without re-implementing the entity tables here.
//
// Wired methods: escape / unescape (application/x-www-form-urlencoded, space ->
// '+'); escapeURIComponent / unescapeURIComponent (space -> "%20");
// escapeHTML / unescapeHTML (five named entities + numeric &#NN; / &#xHH;
// decoding); escapeElement / unescapeElement (tag-only HTML escaping for named
// elements); and parse (query string -> Hash of String -> Array<String>).
// Ruby 4.0 moved CGI.parse to the cgi gem; it is wired on the module regardless.
// CGI's live request/response/session and HTML-generation machinery is out of
// scope.
func (vm *VM) registerCGI() {
	mod := newClass("CGI", nil)
	mod.isModule = true
	vm.consts["CGI"] = object.Wrap(mod)
	sdef := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	sdef("escape", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(cgi.Escape(strArg(args[0]))))
	})
	sdef("unescape", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(cgi.Unescape(strArg(args[0]))))
	})
	sdef("escapeURIComponent", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(cgi.EscapeURIComponent(strArg(args[0]))))
	})
	sdef("unescapeURIComponent", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(cgi.UnescapeURIComponent(strArg(args[0]))))
	})
	sdef("escapeHTML", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(cgi.EscapeHTML(strArg(args[0]))))
	})
	sdef("unescapeHTML", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(cgi.UnescapeHTML(strArg(args[0]))))
	})
	sdef("escapeElement", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(cgi.EscapeElement(strArg(args[0]), elementNames(args[1:])...)))
	})
	sdef("unescapeElement", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Wrap(object.NewString(cgi.UnescapeElement(strArg(args[0]), elementNames(args[1:])...)))
	})
	sdef("parse", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return cgiParse(strArg(args[0]))
	})
}

// elementNames coerces the variadic element-name arguments of escapeElement /
// unescapeElement to a []string. Each must be a String, matching MRI.
func elementNames(args []object.Value) []string {
	names := make([]string, len(args))
	for i, a := range args {
		names[i] = strArg(a)
	}
	return names
}

// cgiParse implements CGI.parse: it returns a Ruby Hash mapping each query key
// (a String) to an Array of its decoded String values, in first-appearance key
// order (MRI inserts keys as it first encounters them). The library's
// ParseQuery yields an unordered map, so the key order is recovered by scanning
// the query the same way the gem does (split on '&' / ';', key before the first
// '='); a pair with no '=' contributes a key with an empty value array.
func cgiParse(query string) object.Value {
	parsed := cgi.ParseQuery(query)
	h := object.NewHash()
	for _, key := range parseKeyOrder(query) {
		arr := object.NewArrayFromSlice(make([]object.Value, 0, len(parsed[key])))
		for _, v := range parsed[key] {
			arr.Elems = append(arr.Elems, object.Wrap(object.NewString(v)))
		}
		h.Set(object.Wrap(object.NewString(key)), object.Wrap(arr))
	}
	return object.Wrap(h)
}

// parseKeyOrder returns the query's keys in first-appearance order. It mirrors
// CGI.parse's own splitting so the order matches MRI's Hash insertion order:
// the query is split on '&' and ';', each pair on its first '=', and the key
// part is form-unescaped before deduplication.
func parseKeyOrder(query string) []string {
	var order []string
	seen := map[string]bool{}
	for _, pair := range strings.FieldsFunc(query, func(r rune) bool { return r == '&' || r == ';' }) {
		rawKey, _, _ := strings.Cut(pair, "=")
		key := cgi.Unescape(rawKey)
		if !seen[key] {
			seen[key] = true
			order = append(order, key)
		}
	}
	return order
}
