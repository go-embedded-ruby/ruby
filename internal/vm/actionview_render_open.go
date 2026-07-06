//go:build !rbgo_closed

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	erb "github.com/go-ruby-erb/erb"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-parser/parser"
)

// avParse and avCompileWithLocals are seams over the front-end used by the inline
// renderer, letting a fault-injection test drive the SyntaxError branches (a parse
// failure and a post-parse compile failure) deterministically rather than relying
// on a template that happens to break at each stage.
var (
	avParse             = parser.Parse
	avCompileWithLocals = compiler.CompileWithLocals
)

// actionViewInlineRender is the default RenderTemplate seam: it evaluates tmpl as
// an inline ERB template against the ActionView::Base context, so <%= %> runs Ruby
// with the render locals bound as local variables and helper methods dispatching
// on self. The template is compiled through the already-bound go-ruby-erb compiler
// in erubi mode (Rails' default engine, so a standalone <%= %> line keeps its
// trailing newline), then evaluated exactly as the Sinatra erb helper evaluates a
// view: a synthetic parent frame holds the locals so the compiled source resolves
// them at depth 1, and self is the Base (whose @ivars and helper methods resolve).
// The compiled template's final expression is its output buffer, so exec returns
// the rendered String. It uses the front-end directly (parser + CompileWithLocals),
// so a closed-world build replaces it with the stub in actionview_render_closed.go.
func (vm *VM) actionViewInlineRender(b *ActionViewBase, tmpl string, locals map[string]any) string {
	names, vals := avLocalSlots(locals)
	src, _, err := erbCompile(tmpl, erb.Options{Mode: erb.ModeErubi, EOutVar: "_erbout"})
	if err != nil {
		// go-ruby-erb never fails on a well-formed template (its err is reserved for
		// malformed options, unreachable through this fixed-option call, so the branch
		// is exercised by swapping erbCompile in a fault-injection test); it is
		// surfaced as an ArgumentError so the contract stays total.
		raise("ArgumentError", "%s", err.Error())
	}
	prog, perr := avParse(src)
	if perr != nil {
		// A parse error means the template's embedded Ruby (<% … %>) is itself
		// malformed — surfaced as a SyntaxError, matching MRI's eval of a broken
		// compiled template.
		raise("SyntaxError", "%s", perr.Error())
	}
	iseq, cerr := avCompileWithLocals(prog, names)
	if cerr != nil {
		// A compile error on source the front-end parsed (e.g. a construct illegal at
		// this position); surfaced as a SyntaxError.
		raise("SyntaxError", "%s", cerr.Error())
	}
	iseq.Name = "(actionview)"
	slots := make([]object.Value, len(vals))
	copy(slots, vals)
	parent := &Env{slots: slots}
	res := vm.exec(iseq, b, nil, b.cls, "", parent, nil, nil, nil)
	return avToS(res)
}

// avLocalSlots splits the render locals into a stable-ordered name list and the
// matching Ruby values (each mapped through avLocalToRuby), the shape
// CompileWithLocals and the synthetic parent env consume.
func avLocalSlots(locals map[string]any) (names []string, vals []object.Value) {
	for k, v := range locals {
		names = append(names, k)
		vals = append(vals, avLocalToRuby(v))
	}
	return names, vals
}
