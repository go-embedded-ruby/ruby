// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	erb "github.com/go-ruby-erb/erb"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file wires Sinatra's `erb` view helper to the already-bound go-ruby-erb
// compiler. A route/filter block runs against a SinatraCtx (self); `erb` there
// compiles a template to Ruby source (go-ruby-erb, mirroring MRI's ERB::Compiler
// byte for byte) and evaluates that source in the handler's binding — self = the
// SinatraCtx, so the action's @ivars and the request helpers (params, request,
// …) resolve, plus any `locals:` bound as local variables — capturing the
// _erbout buffer and returning the rendered String. That String becomes the Rack
// response body via sinatraResult, exactly as under MRI Sinatra + Tilt.
//
// Trim mode and the output-buffer name match Tilt::ERBTemplate's defaults, which
// Sinatra uses unchanged: trim "<>" (a code-only line's surrounding newlines are
// removed) and eoutvar "_erbout". <%= %> interpolates unescaped (Sinatra does not
// auto-escape unless :erb => { escape_html: true } is configured — a scoped
// follow-up), <% %> is control flow.
//
// Two render paths land: an inline-String template (needs no filesystem) and a
// :symbol template naming views/<sym>.erb (read through the File binding, with
// the views dir taken from settings :views, default "./views"). Layouts and
// partials are flagged as a scoped follow-up (see registerSinatraContext).

// sinatraTrimMode is the ERB trim mode Sinatra renders with: Tilt::ERBTemplate
// maps a nil/true :trim option (Sinatra's default) to "<>", so a line holding
// only a code tag has both its surrounding newlines trimmed.
const sinatraTrimMode = "<>"

// sinatraEOutVar names the output buffer the compiled template appends to, matching
// Tilt::ERBTemplate's default output variable.
const sinatraEOutVar = "_erbout"

// sinatraErb implements the `erb` view helper on a request context. template is
// either an inline template String or a :symbol naming views/<sym>.erb; options
// is the (largely reserved) options Hash whose :locals entry, if present, seeds
// locals; locals is the positional locals Hash. The template is compiled through
// go-ruby-erb and evaluated in the handler's binding, returning the rendered
// String.
func (vm *VM) sinatraErb(sc *SinatraCtx, args []object.Value) object.Value {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..3)")
	}
	tmpl := vm.sinatraErbTemplate(sc, args[0])
	src, _, err := erbCompile(tmpl, erb.Options{TrimMode: sinatraTrimMode, EOutVar: sinatraEOutVar})
	if err != nil {
		// go-ruby-erb never fails on a well-formed template (its err is reserved for
		// genuinely malformed options, unreachable through this fixed-option call);
		// the branch is surfaced as an ArgumentError so the contract stays total.
		raise("ArgumentError", "%s", err.Error())
	}
	names, vals := sinatraErbLocals(args)
	return vm.sinatraErbEval(sc, src, names, vals)
}

// sinatraErbTemplate resolves the first `erb` argument to the template source: an
// inline String is used verbatim; a :symbol is resolved to views/<sym>.erb and
// read from disk (through the File binding), the views dir coming from the app's
// :views setting (default "./views"). Any other type is a Sinatra-style error.
func (vm *VM) sinatraErbTemplate(sc *SinatraCtx, tmpl object.Value) string {
	switch t := tmpl.(type) {
	case *object.String:
		return t.Str()
	case object.Symbol:
		return vm.sinatraErbReadView(sc, string(t))
	}
	raise("ArgumentError", "erb: template must be a String or Symbol, got %s", classNameOf(tmpl))
	return ""
}

// sinatraErbReadView reads the ERB template file views/<name>.erb, the views dir
// taken from the app's :views setting (a String) or defaulting to "./views" as in
// Sinatra. Reading goes through the File binding so it obeys the same host
// filesystem seam as any File.read; a missing file raises Errno::ENOENT exactly as
// under Sinatra.
func (vm *VM) sinatraErbReadView(sc *SinatraCtx, name string) string {
	dir := "./views"
	if v, ok := sc.settings["views"]; ok {
		if s, ok := v.(*object.String); ok {
			dir = s.Str()
		}
	}
	pathV := object.NewString(dir + "/" + name + ".erb")
	// File.read returns the file's contents as a String (or raises Errno::ENOENT for
	// a missing view, which propagates exactly as under Sinatra).
	res := vm.send(vm.consts["File"].(*RClass), "read", []object.Value{pathV}, nil)
	return res.(*object.String).Str()
}

// sinatraErbLocals collects the local variables an `erb` render binds, in a stable
// order (options[:locals] first, then the positional locals Hash, positional
// entries overriding). Keys are Symbols or Strings; each becomes a local variable
// name visible to <%= %> / <% %> in the template.
func sinatraErbLocals(args []object.Value) (names []string, vals []object.Value) {
	seen := map[string]int{}
	put := func(name string, v object.Value) {
		if i, ok := seen[name]; ok {
			vals[i] = v
			return
		}
		seen[name] = len(names)
		names = append(names, name)
		vals = append(vals, v)
	}
	addHash := func(h *object.Hash) {
		for _, k := range h.Keys {
			v, _ := h.Get(k)
			put(sinatraStr(k), v)
		}
	}
	// options[:locals] — Sinatra also accepts locals passed inside the options Hash.
	if len(args) >= 2 {
		if h, ok := args[1].(*object.Hash); ok {
			if lv, ok := h.Get(object.Symbol("locals")); ok {
				if lh, ok := lv.(*object.Hash); ok {
					addHash(lh)
				}
			}
		}
	}
	// The positional locals Hash (3rd argument), which overrides options[:locals].
	if len(args) >= 3 {
		if h, ok := args[2].(*object.Hash); ok {
			addHash(h)
		}
	}
	return names, vals
}
