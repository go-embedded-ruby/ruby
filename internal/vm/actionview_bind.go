// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"

	actionview "github.com/go-ruby-actionview/actionview"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-actionview/actionview library — the
// pure-Go, MRI-faithful reimplementation of ActionView's html-safe buffer and the
// tag / url / form / text / number view helpers. The observable output
// (tag_options rendering, form name/id conventions, currency/number formatting,
// link_to / content_tag markup, SafeBuffer escaping) is produced by that library;
// rbgo only maps Ruby arguments to the library's Go types and wraps its results.
//
// ActionView::Base is the view context: the stateful helpers (form_tag,
// form_with, button_to, current_page?, render) hang off it, while the pure
// helpers are library package functions the same class exposes. Its two seams —
// URLFor (the routes seam) and RenderTemplate (the template-evaluation seam) —
// are wired here to Ruby callables when the host supplies them, and otherwise to
// sensible defaults (url_for is the identity/path formatter, render evaluates an
// inline ERB template through the already-bound go-ruby-erb compiler so <%= %>
// runs Ruby). Because #render is an ordinary dispatchable method on
// ActionView::Base, a later actionpack/actionmailer binding renders a view by
// simply sending :render to a Base instance.

// SafeBufferVal is a Ruby ActiveSupport::SafeBuffer / ActionView::OutputBuffer:
// an html-safe string. It wraps the library's *actionview.SafeBuffer so the
// mutating concat operators (<< / concat / safe_concat) update in place while the
// escape-on-append policy stays the library's. The cls field lets classOf report
// the SafeBuffer class.
type SafeBufferVal struct {
	cls *RClass
	buf *actionview.SafeBuffer
}

// ToS returns the buffer's raw markup (ActiveSupport::SafeBuffer#to_s).
func (b *SafeBufferVal) ToS() string { return b.buf.String() }

// Inspect renders the buffer's contents quoted, matching how a String inspects.
func (b *SafeBufferVal) Inspect() string { return fmt.Sprintf("%q", b.buf.String()) }

// Truthy reports true: a SafeBuffer, like any String, is always truthy.
func (b *SafeBufferVal) Truthy() bool { return true }

// newSafeBuffer wraps a library SafeBuffer value as a Ruby SafeBufferVal of the
// bound class, copying it to a fresh pointer so the mutators operate on the
// wrapper's own storage.
func (vm *VM) newSafeBuffer(sb actionview.SafeBuffer) *SafeBufferVal {
	cp := sb
	return &SafeBufferVal{cls: vm.consts["ActiveSupport::SafeBuffer"].(*RClass), buf: &cp}
}

// ActionViewBase is a Ruby ActionView::Base instance: the view context carrying
// the request/routing/CSRF configuration and the two host seams (url_for and
// render_template). Its zero configuration mirrors a bare helper include with
// forgery protection off. A fresh actionview.Context is built per stateful call
// from these fields, so mutating the configuration between calls is honoured.
type ActionViewBase struct {
	cls   *RClass
	vm    *VM
	ivars map[string]object.Value

	authenticityToken     string
	protectAgainstForgery bool
	suppressUTF8          bool
	requestMethod         string
	requestPath           string
	requestFullpath       string

	// urlForProc, when set, is the Ruby routes callable url_for resolves through;
	// renderTemplateProc, when set, is the Ruby template resolver #render evaluates
	// identifiers with. Both nil selects the built-in defaults.
	urlForProc         *Proc
	renderTemplateProc *Proc
}

// ToS renders the default Object#to_s form; a view context has no meaningful
// string value of its own.
func (b *ActionViewBase) ToS() string { return "#<ActionView::Base>" }

// Inspect matches ToS.
func (b *ActionViewBase) Inspect() string { return b.ToS() }

// Truthy reports true, as every ordinary object does.
func (b *ActionViewBase) Truthy() bool { return true }

// newActionViewBase constructs a fresh ActionView::Base bound to vm. A later
// actionpack/actionmailer binding uses this to obtain a render context and then
// dispatches :render to it.
func (vm *VM) newActionViewBase() *ActionViewBase {
	return &ActionViewBase{cls: vm.consts["ActionView::Base"].(*RClass), vm: vm, ivars: map[string]object.Value{}}
}

// context builds the library Context from the current configuration, wiring the
// URLFor and RenderTemplate seams to this instance's methods (which dispatch to
// the Ruby callables or the defaults).
func (b *ActionViewBase) context() *actionview.Context {
	return &actionview.Context{
		URLFor:                b.urlFor,
		AuthenticityToken:     b.authenticityToken,
		ProtectAgainstForgery: b.protectAgainstForgery,
		SuppressUTF8Enforcer:  b.suppressUTF8,
		RequestMethod:         b.requestMethod,
		RequestPath:           b.requestPath,
		RequestFullpath:       b.requestFullpath,
		RenderTemplate:        b.renderTemplate,
	}
}

// urlFor is the routes seam: it resolves a routing argument to a URL string. When
// a Ruby url_for callable is configured it is invoked with the argument (a Ruby
// value); otherwise the argument is used as-is (a String passes through, anything
// else is stringified), matching the library's identity default.
func (b *ActionViewBase) urlFor(o any) string {
	// The library calls this seam with either a Go string (the form_with url) or a
	// Ruby value (the current_page? / url_for argument); those are the only two
	// producers, so a non-string is asserted to object.Value.
	var ov object.Value
	if s, ok := o.(string); ok {
		ov = object.NewString(s)
	} else {
		ov = o.(object.Value)
	}
	if b.urlForProc != nil {
		return avToS(b.vm.callBlock(b.urlForProc, []object.Value{ov}))
	}
	return avToS(ov)
}

// renderTemplate is the template-evaluation seam #render defers to. When a Ruby
// render_template callable is configured it is invoked with the resolved
// identifier and the locals (as a Ruby Hash) and its result is the rendered
// markup — this is the hook a later actionpack binding wires to its own template
// resolver. Otherwise the identifier is treated as an inline ERB template source
// and evaluated (through the bound go-ruby-erb compiler) with the locals bound as
// local variables, so <%= %> runs Ruby against this view context.
func (b *ActionViewBase) renderTemplate(identifier string, locals map[string]any) (string, error) {
	if b.renderTemplateProc != nil {
		h := object.NewHash()
		for k, v := range locals {
			h.Set(object.NewString(k), avLocalToRuby(v))
		}
		r := b.vm.callBlock(b.renderTemplateProc, []object.Value{object.NewString(identifier), h})
		return avToS(r), nil
	}
	return b.vm.actionViewInlineRender(b, identifier, locals), nil
}

// AVPartialIter is a Ruby ActionView::PartialIteration: the per-element iteration
// state a collection partial sees as its <name>_iteration local. It wraps the
// library's PartialIteration so #index / #size / #first? / #last? report the
// library's values.
type AVPartialIter struct {
	cls *RClass
	p   actionview.PartialIteration
}

// ToS renders the default object form; the iteration state has no string value.
func (p *AVPartialIter) ToS() string { return "#<ActionView::PartialIteration>" }

// Inspect matches ToS.
func (p *AVPartialIter) Inspect() string { return p.ToS() }

// Truthy reports true.
func (p *AVPartialIter) Truthy() bool { return true }

// avLocalToRuby maps a render local the library hands to the RenderTemplate seam
// back to a Ruby value: user locals are stored as their original object.Value and
// pass through; the library-injected <name>_counter is a Go int; and the
// <name>_iteration is a library PartialIteration wrapped as an AVPartialIter.
func avLocalToRuby(v any) object.Value {
	// The three producers are: a user local (its original object.Value), the
	// library-injected <name>_counter (a Go int) and the <name>_iteration (a
	// library PartialIteration). A non-int, non-PartialIteration value is therefore
	// always the object.Value of a user local.
	if i, ok := v.(int); ok {
		return object.IntValue(int64(i))
	}
	if p, ok := v.(actionview.PartialIteration); ok {
		return &AVPartialIter{cls: avPartialIterClass, p: p}
	}
	return v.(object.Value)
}

// avPartialIterClass is the ActionView::PartialIteration class, captured at
// registration so avLocalToRuby (called from the library seam, without a vm in
// scope) can stamp the wrapper.
var avPartialIterClass *RClass

// avValue maps a Ruby argument to the Go value the library helpers consume,
// preserving the type distinctions tag_options / safeString switch on: nil, bool,
// Integer (as Go int), Float, String, Symbol (as its name), a SafeBuffer (as the
// library's html-safe SafeBuffer so it is not re-escaped), an Array (as []any for
// class/token joining) and a Hash (as map[string]any for data/aria expansion and
// class hashes). Anything else falls back to its Ruby to_s.
func avValue(v object.Value) any {
	switch x := v.(type) {
	case nil, object.Nil:
		return nil
	case object.Bool:
		return bool(x)
	case object.Integer:
		return int(x)
	case object.Float:
		return float64(x)
	case *object.String:
		return x.Str()
	case object.Symbol:
		return string(x)
	case *SafeBufferVal:
		return *x.buf
	case *object.Array:
		out := make([]any, len(x.Elems))
		for i, e := range x.Elems {
			out[i] = avValue(e)
		}
		return out
	case *object.Hash:
		m := make(map[string]any, len(x.Keys))
		for _, k := range x.Keys {
			val, _ := x.Get(k)
			m[avKey(k)] = avValue(val)
		}
		return m
	}
	return v.ToS()
}

// avKey renders a Ruby Hash key (a Symbol or String, the attribute-name shapes)
// as its bare name; any other key falls back to its to_s.
func avKey(k object.Value) string {
	switch n := k.(type) {
	case object.Symbol:
		return string(n)
	case *object.String:
		return n.Str()
	}
	return k.ToS()
}

// avToS renders a Ruby value as a plain (unescaped) string the way #to_s would:
// nil is empty, a String yields its contents, a Symbol its name, and any other
// value its Ruby to_s. It is used for URL/label arguments where the library or a
// caller does any escaping.
func avToS(v object.Value) string {
	switch x := v.(type) {
	case nil, object.Nil:
		return ""
	case *object.String:
		return x.Str()
	case object.Symbol:
		return string(x)
	}
	return v.ToS()
}

// avOpts converts an optional options argument to the library's ordered Attrs: a
// Hash becomes one Attr per key in insertion order (matching Ruby hash ordering,
// which the byte-for-byte output depends on); nil / a missing / non-Hash argument
// yields no attributes.
func avOpts(v object.Value) actionview.Attrs {
	h, ok := v.(*object.Hash)
	if !ok {
		return nil
	}
	out := make(actionview.Attrs, 0, len(h.Keys))
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		out = append(out, actionview.Attr{Key: avKey(k), Val: avValue(val)})
	}
	return out
}

// avArg returns args[i] or nil when the index is past the end, so a helper can
// treat trailing arguments (value / content / options) as optional.
func avArg(args []object.Value, i int) object.Value {
	if i < len(args) {
		return args[i]
	}
	return nil
}

// avOptHash returns the trailing options Hash of a helper call (args[i] when it is
// a Hash), or nil. Number/text helpers read their keyword options from it.
func avOptHash(args []object.Value, i int) *object.Hash {
	if h, ok := avArg(args, i).(*object.Hash); ok {
		return h
	}
	return nil
}

// avHashVal looks a keyword option up in an options Hash by Symbol key then String
// key (Rails options use Symbol keys, but a String key is tolerated), reporting
// whether it was present. A nil Hash has no options.
func avHashVal(h *object.Hash, key string) (object.Value, bool) {
	if h == nil {
		return nil, false
	}
	if v, ok := h.Get(object.Symbol(key)); ok {
		return v, true
	}
	return h.Get(object.NewString(key))
}

// avHashStr returns the string value of option key, or def when it is absent.
func avHashStr(h *object.Hash, key, def string) string {
	if v, ok := avHashVal(h, key); ok {
		return avToS(v)
	}
	return def
}

// avHashInt returns the integer value of option key, or def when it is absent or
// not an Integer.
func avHashInt(h *object.Hash, key string, def int) int {
	if v, ok := avHashVal(h, key); ok {
		if i, ok := v.(object.Integer); ok {
			return int(i)
		}
	}
	return def
}

// avHashBool returns the truthiness of option key, or def when it is absent.
func avHashBool(h *object.Hash, key string, def bool) bool {
	if v, ok := avHashVal(h, key); ok {
		return v.Truthy()
	}
	return def
}
