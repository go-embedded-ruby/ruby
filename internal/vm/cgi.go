// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"net/url"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerCGI installs the CGI module (require "cgi"). The URL-encoding surface
// Puppet uses (CGI.escape / CGI.unescape) is real over Go's net/url —
// CGI.escape is application/x-www-form-urlencoded (space -> '+'), matching MRI
// byte for byte — and the HTML-entity helpers (escapeHTML / unescapeHTML) are
// implemented directly. CGI's HTML-generation DSL is not part of this surface.
func (vm *VM) registerCGI() {
	mod := newClass("CGI", nil)
	mod.isModule = true
	vm.consts["CGI"] = mod
	sdef := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	sdef("escape", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(url.QueryEscape(strArg(args[0])))
	})
	sdef("unescape", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s, err := url.QueryUnescape(strArg(args[0]))
		if err != nil {
			// MRI's CGI.unescape never raises — it leaves malformed escapes as-is.
			return object.NewString(strArg(args[0]))
		}
		return object.NewString(s)
	})
	sdef("escapeHTML", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(escapeHTML(strArg(args[0])))
	})
	sdef("unescapeHTML", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(unescapeHTML(strArg(args[0])))
	})
}

// escapeHTML replaces the five characters CGI.escapeHTML encodes.
func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&#39;")
	return r.Replace(s)
}

// unescapeHTML reverses escapeHTML for the same five entities.
func unescapeHTML(s string) string {
	r := strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", "\"", "&#39;", "'")
	return r.Replace(s)
}
