// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	liquid "github.com/go-ruby-liquid/liquid"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-liquid/liquid engine. The parser and
// renderer live in that library; rbgo only maps the template source String, an
// error-mode keyword and an assigns Hash to liquid.Parse / Template.Render, so the
// gem-faithful rendered output the Liquid module relies on is preserved by
// construction.

// liquidTemplate is the opaque native handle stored on a Liquid::Template instance
// (as @__tmpl) by Template.parse. It carries the parsed *liquid.Template so #render
// / #render! can render it without re-parsing. It satisfies object.Value only so it
// can live in an ivar; it is never exposed to Ruby as a first-class value.
type liquidTemplate struct{ t *liquid.Template }

func (h *liquidTemplate) ToS() string     { return "#<Liquid::Template>" }
func (h *liquidTemplate) Inspect() string { return h.ToS() }
func (h *liquidTemplate) Truthy() bool    { return true }

// liquidParse parses src in the given error mode into a *liquid.Template. A
// parse-time error (a genuine syntax error surfaced in :strict / :warn mode)
// raises the matching Liquid error so the failure rescues as the right Ruby class.
func liquidParse(src string, mode liquid.ErrorMode) *liquid.Template {
	t, err := liquid.Parse(src, liquid.WithErrorMode(mode))
	if err != nil {
		raise(liquidErrorClass(err), "%s", err.Error())
	}
	return t
}

// liquidRender renders the parsed template against assigns. In lax mode a runtime
// error renders inline ("Liquid error: …") and Render returns it as part of the
// string; in strict mode a runtime error is surfaced as the matching Liquid error
// via RenderStrict, so #render! raises where #render would embed.
func liquidRender(vm *VM, h *liquidTemplate, assigns map[string]any, strict bool) string {
	render := liquidRenderFn
	if strict {
		render = liquidRenderStrictFn
	}
	out, err := render(h.t, assigns)
	if err != nil {
		// A strict runtime error (render!) surfaces here; the lax Render never
		// returns an error in practice (it embeds "Liquid error: …" inline), so its
		// error arm is a defensive one, reached only by swapping the seam below.
		raise(liquidErrorClass(err), "%s", err.Error())
	}
	return out
}

// liquidRenderFn / liquidRenderStrictFn are the seams over Template.Render /
// Template.RenderStrict: swapping them lets a fault-injection test exercise
// liquidRender's error arm for the lax path, which the real Render never triggers.
var (
	liquidRenderFn       = func(t *liquid.Template, a map[string]any) (string, error) { return t.Render(a) }
	liquidRenderStrictFn = func(t *liquid.Template, a map[string]any) (string, error) { return t.RenderStrict(a) }
)

// liquidErrorMode reads the error_mode keyword from a parse call's trailing kwargs
// Hash. :lax (the gem default), :warn and :strict select the library's Lax / Warn /
// Strict modes; an unrecognised value falls back to Lax, matching the gem treating
// an unknown mode as lax.
func liquidErrorMode(args []object.Value) liquid.ErrorMode {
	h, ok := trailingHash(args)
	if !ok {
		return liquid.Lax
	}
	v, ok := h.Get(object.Symbol("error_mode"))
	if !ok {
		return liquid.Lax
	}
	switch liquidModeName(v) {
	case "strict":
		return liquid.Strict
	case "warn":
		return liquid.Warn
	default:
		return liquid.Lax
	}
}

// liquidModeName renders an error_mode value (a Symbol or String) as its bare name.
func liquidModeName(v object.Value) string {
	switch n := v.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	}
	return v.ToS()
}

// liquidErrorClass maps a library error to the qualified Ruby class name the
// re-raise should use, reading the error's gem class name off a *liquid.Error and
// falling back to the base Liquid::Error otherwise.
func liquidErrorClass(err error) string {
	if le, ok := err.(*liquid.Error); ok {
		switch le.Type {
		case "SyntaxError":
			return "Liquid::SyntaxError"
		case "ArgumentError":
			return "Liquid::ArgumentError"
		case "ZeroDivisionError":
			return "Liquid::ZeroDivisionError"
		case "StackLevelError":
			return "Liquid::StackLevelError"
		}
	}
	return "Liquid::Error"
}

// liquidAssignsArg reads the render argument as the assigns map. A missing or nil
// argument is the empty assigns ({}); a Hash is mapped key-by-key into the library
// value tree. A non-Hash argument raises a Liquid::ArgumentError, matching the gem
// requiring a Hash of assigns.
func liquidAssignsArg(vm *VM, args []object.Value) map[string]any {
	if len(args) == 0 {
		return map[string]any{}
	}
	switch a := args[0].(type) {
	case object.Nil:
		return map[string]any{}
	case *object.Hash:
		return liquidHashToMap(vm, a)
	}
	raise("Liquid::ArgumentError", "assigns must be a Hash")
	return nil
}

// liquidHashToMap maps a Ruby Hash to a map[string]any, rendering each key as its
// bare name (Symbol / String / to_s) so a template variable resolves whether the
// assigns key was written as a Symbol or a String.
func liquidHashToMap(vm *VM, h *object.Hash) map[string]any {
	m := make(map[string]any, len(h.Keys))
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		m[liquidKey(k)] = toLiquid(vm, val)
	}
	return m
}

// liquidKey renders a Hash key as its bare name for the assigns map.
func liquidKey(k object.Value) string {
	switch n := k.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	}
	return k.ToS()
}

// toLiquid maps a Ruby value into the go-ruby-liquid value tree (nil / bool / int64
// / float64 / string / []any / map[string]any). A Ruby object with no direct model
// shape is handed the engine its #to_s text, so a Drop-less custom object still
// renders as a string.
func toLiquid(vm *VM, v object.Value) any {
	switch n := v.(type) {
	case nil:
		return nil
	case object.Nil:
		return nil
	case object.Bool:
		return bool(n)
	case object.Integer:
		return int64(n)
	case *object.Bignum:
		return n.I
	case object.Float:
		return float64(n)
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	case *object.Array:
		out := make([]any, len(n.Elems))
		for i, el := range n.Elems {
			out[i] = toLiquid(vm, el)
		}
		return out
	case *object.Hash:
		return liquidHashToMap(vm, n)
	}
	return vm.send(v, "to_s", nil, nil).(*object.String).Str()
}
