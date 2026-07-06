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
// Two view render paths land: an inline-String template (needs no filesystem)
// and a :symbol template naming views/<sym>.erb (read through the File binding,
// with the views dir taken from settings :views, default "./views").
//
// Layouts wrap that view exactly as MRI Sinatra does. By default `erb :view`
// renders views/layout.erb (if present) with the view's rendered String exposed
// as `yield`, so the layout's `<%= yield %>` interpolates the inner content; a
// missing default layout is silently skipped (MRI's `catch(:layout_missing)`).
// `erb :view, layout: false` renders the view with no layout; `layout: :name`
// (or a template String) selects a custom layout, and a missing *named* layout
// raises Errno::ENOENT (MRI eats the error only for the default layout). Partials
// (`erb :part` composed inside a view via a helper) and the :erb escape_html /
// app-level `set :erb, layout:` engine options remain a scoped follow-up.

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
	names, vals := sinatraErbLocals(args)
	// Render the view itself. A plain view is evaluated with no block, so a bare
	// `yield` in a view (as opposed to a layout) raises LocalJumpError, matching MRI.
	view := vm.sinatraErbRender(sc, vm.sinatraErbTemplate(sc, args[0]), names, vals, nil)
	return vm.sinatraErbLayout(sc, args, view, names, vals)
}

// sinatraErbRender compiles one ERB template String through go-ruby-erb and
// evaluates it in the handler's binding, binding the render locals and, when
// non-nil, the yield block (used to expose the wrapped view to a layout's
// `<%= yield %>`). It returns the rendered String.
func (vm *VM) sinatraErbRender(sc *SinatraCtx, tmpl string, names []string, vals []object.Value, block *Proc) object.Value {
	src, _, err := erbCompile(tmpl, erb.Options{TrimMode: sinatraTrimMode, EOutVar: sinatraEOutVar})
	if err != nil {
		// go-ruby-erb never fails on a well-formed template (its err is reserved for
		// genuinely malformed options, unreachable through this fixed-option call);
		// the branch is surfaced as an ArgumentError so the contract stays total.
		raise("ArgumentError", "%s", err.Error())
	}
	return vm.sinatraErbEval(sc, src, names, vals, block)
}

// sinatraErbLayout applies Sinatra's layout rule to an already-rendered view: it
// reads the layout option from the `erb` options Hash (args[1]) and, if a layout
// applies, renders it with the same locals and a block whose `yield` returns the
// view's String, so the layout's `<%= yield %>` interpolates the inner content.
//
// The layout is chosen exactly as MRI Sinatra's render does:
//   - no :layout key      -> the default layout (views/layout.erb), missing = skipped
//   - :layout => false/nil -> no layout (the view is returned verbatim)
//   - :layout => true      -> the default layout, but a missing file now raises
//   - :layout => :name     -> views/name.erb, a missing file raises
//   - :layout => "src"     -> an inline template String used as the layout
//
// (MRI eats a "layout missing" error only for the implicit default layout — an
// explicitly requested layout that is absent surfaces the Errno::ENOENT.)
func (vm *VM) sinatraErbLayout(sc *SinatraCtx, args []object.Value, view object.Value, names []string, vals []object.Value) object.Value {
	val, present := sinatraLayoutOpt(args)
	var (
		name   string // a :symbol/true layout resolves to views/<name>.erb
		source string // a String layout is an inline template rendered directly
		inline bool
		eat    bool // eat a missing layout file (only the implicit default layout)
	)
	switch {
	case !present:
		name, eat = "layout", true
	default:
		switch v := val.(type) {
		case object.Symbol:
			name = string(v)
		case *object.String:
			source, inline = v.Str(), true
		default:
			// true selects the default layout (with the error surfaced, not eaten);
			// false / nil / any other falsy value means render the view with no layout.
			if !val.Truthy() {
				return view
			}
			name = "layout"
		}
	}

	// The layout's `yield` returns the wrapped view's rendered String.
	inner := view
	yield := &Proc{native: func(*VM, []object.Value) object.Value { return inner }}

	if inline {
		return vm.sinatraErbRender(sc, source, names, vals, yield)
	}
	tmpl, found := vm.sinatraReadLayout(sc, name, eat)
	if !found {
		// The implicit default layout is absent — return the un-wrapped view, exactly
		// as MRI's catch(:layout_missing) does.
		return view
	}
	return vm.sinatraErbRender(sc, tmpl, names, vals, yield)
}

// sinatraLayoutOpt reads the :layout entry from the `erb` options Hash (the second
// positional argument), reporting whether the key was present so an absent key
// (default layout) is distinguished from an explicit `layout: false`.
func sinatraLayoutOpt(args []object.Value) (val object.Value, present bool) {
	if len(args) >= 2 {
		if h, ok := args[1].(*object.Hash); ok {
			if v, ok := h.Get(object.Symbol("layout")); ok {
				return v, true
			}
		}
	}
	return nil, false
}

// sinatraReadLayout reads the layout template file views/<name>.erb through the
// File binding, the views dir resolved as for a view (settings :views, default
// "./views"). When eat is true (the implicit default layout) a missing file is
// reported as found=false rather than raising, so the caller can skip the layout —
// MRI's catch(:layout_missing). When eat is false the file is read unconditionally,
// so a missing named layout raises Errno::ENOENT.
func (vm *VM) sinatraReadLayout(sc *SinatraCtx, name string, eat bool) (tmpl string, found bool) {
	if eat && !vm.sinatraViewExists(sc, name) {
		return "", false
	}
	return vm.sinatraErbReadView(sc, name), true
}

// sinatraViewExists reports whether views/<name>.erb exists, through the File
// binding (File.exist?), so the default-layout lookup obeys the same host
// filesystem seam as a view read.
func (vm *VM) sinatraViewExists(sc *SinatraCtx, name string) bool {
	pathV := object.NewString(vm.sinatraViewsDir(sc) + "/" + name + ".erb")
	return vm.send(vm.consts["File"].(*RClass), "exist?", []object.Value{pathV}, nil).Truthy()
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
	pathV := object.NewString(vm.sinatraViewsDir(sc) + "/" + name + ".erb")
	// File.read returns the file's contents as a String (or raises Errno::ENOENT for
	// a missing view, which propagates exactly as under Sinatra).
	res := vm.send(vm.consts["File"].(*RClass), "read", []object.Value{pathV}, nil)
	return res.(*object.String).Str()
}

// sinatraViewsDir resolves the app's views directory: the :views setting (a
// String) or Sinatra's default "./views" when unset or set to a non-String value.
func (vm *VM) sinatraViewsDir(sc *SinatraCtx) string {
	if v, ok := sc.settings["views"]; ok {
		if s, ok := v.(*object.String); ok {
			return s.Str()
		}
	}
	return "./views"
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
