// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"

	actionview "github.com/go-ruby-actionview/actionview"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// avContains reports whether haystack contains needle, backing SafeBuffer#include?.
func avContains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

// registerActionView installs the native backbone of the ActionView view layer
// (require "action_view"): the ActionView::Base view context with the tag / url /
// form / text / number helpers and the render pipeline, the ActionView::Base
// FormBuilder yielded by form_with, ActionView::PartialIteration, and the
// html-safe ActiveSupport::SafeBuffer (plus String#html_safe). Every observable
// byte — tag_options rendering, form name/id conventions, currency/number
// formatting, SafeBuffer escaping — comes from the pure-Go
// github.com/go-ruby-actionview/actionview library; this is the wiring that maps
// Ruby arguments to it and back (see actionview_bind.go). It is registered after
// registerERB/registerErubi (it reuses the ERB escaping/compiler surface) and
// after registerActiveSupport (SafeBuffer nests under ActiveSupport, and the
// helpers reuse the inflector/core-ext through the library).
func (vm *VM) registerActionView() {
	mod := newClass("ActionView", nil)
	mod.isModule = true
	vm.consts["ActionView"] = mod

	vm.registerSafeBuffer()
	vm.registerActionViewPartialIteration(mod)
	vm.registerActionViewBase(mod)
	vm.registerActionViewFormBuilder(mod)
}

// registerSafeBuffer installs ActiveSupport::SafeBuffer (aliased as
// ActionView::OutputBuffer): the html-safe string whose concat operators escape
// unsafe input on append. It also teaches String#html_safe / #html_safe? so a
// plain String can be marked safe (returning a SafeBuffer) or queried (always
// false for a bare String).
func (vm *VM) registerSafeBuffer() {
	as, _ := vm.consts["ActiveSupport"].(*RClass)
	cSB := newClass("ActiveSupport::SafeBuffer", vm.cObject)
	vm.consts["ActiveSupport::SafeBuffer"] = cSB
	if as != nil {
		as.consts["SafeBuffer"] = cSB
	}
	if av, ok := vm.consts["ActionView"].(*RClass); ok {
		av.consts["OutputBuffer"] = cSB
	}

	cSB.smethods["new"] = &Method{name: "new", owner: cSB, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		s := ""
		if len(args) > 0 {
			s = avToS(args[0])
		}
		return vm.newSafeBuffer(actionview.Raw(s))
	}}

	cSB.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*SafeBufferVal).buf.String())
	})
	cSB.define("to_str", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*SafeBufferVal).buf.String())
	})
	cSB.define("html_safe?", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(true)
	})
	cSB.define("html_safe", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return self
	})
	cSB.define("length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self.(*SafeBufferVal).buf.Len()))
	})
	cSB.define("empty?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*SafeBufferVal).buf.Len() == 0)
	})
	concat := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*SafeBufferVal)
		if sb, ok := avArg(args, 0).(*SafeBufferVal); ok {
			b.buf.AppendSafe(*sb.buf)
		} else {
			b.buf.Concat(avToS(avArg(args, 0)))
		}
		return b
	}
	cSB.define("<<", concat)
	cSB.define("concat", concat)
	cSB.define("safe_concat", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*SafeBufferVal)
		b.buf.SafeConcat(avToS(avArg(args, 0)))
		return b
	})
	// + returns a fresh SafeBuffer of the concatenation, escaping the right operand
	// unless it is itself html-safe (ActiveSupport::SafeBuffer#+). This is what lets
	// a view build markup with `f.label + f.text_field + …` without XSS holes.
	cSB.define("+", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		out := vm.newSafeBuffer(actionview.Raw(self.(*SafeBufferVal).buf.String()))
		if sb, ok := avArg(args, 0).(*SafeBufferVal); ok {
			out.buf.AppendSafe(*sb.buf)
		} else {
			out.buf.Concat(avToS(avArg(args, 0)))
		}
		return out
	})
	// include? answers a substring query against the raw markup, so a view/spec can
	// assert on rendered output without an explicit #to_s (SafeBuffer is a String in
	// Rails and responds to it).
	cSB.define("include?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(avContains(self.(*SafeBufferVal).buf.String(), avToS(avArg(args, 0))))
	})

	vm.cString.define("html_safe", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.newSafeBuffer(actionview.Raw(self.(*object.String).Str()))
	})
	vm.cString.define("html_safe?", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(false)
	})
}

// registerActionViewPartialIteration installs ActionView::PartialIteration, the
// per-element iteration state a collection partial sees as its <name>_iteration
// local (index / size / first? / last?), backed by the library's PartialIteration.
func (vm *VM) registerActionViewPartialIteration(mod *RClass) {
	cPI := newClass("ActionView::PartialIteration", vm.cObject)
	vm.consts["ActionView::PartialIteration"] = cPI
	mod.consts["PartialIteration"] = cPI
	avPartialIterClass = cPI

	cPI.define("index", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self.(*AVPartialIter).p.Index))
	})
	cPI.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(self.(*AVPartialIter).p.Size))
	})
	cPI.define("first?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*AVPartialIter).p.First())
	})
	cPI.define("last?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(self.(*AVPartialIter).p.Last())
	})
}

// registerActionViewBase installs ActionView::Base and every helper the view
// context exposes. The pure helpers delegate straight to the library package
// functions; the stateful helpers (button_to, form_tag, form_with, current_page?,
// render) build a fresh library Context from the instance configuration; and the
// configuration writers (protect_against_forgery=, authenticity_token=, the
// request/CSRF fields, and the url_for= / render_template= seam callables) let a
// host opt into routing, forgery protection and template resolution.
func (vm *VM) registerActionViewBase(mod *RClass) {
	base := newClass("ActionView::Base", vm.cObject)
	vm.consts["ActionView::Base"] = base
	mod.consts["Base"] = base

	base.smethods["new"] = &Method{name: "new", owner: base, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.newActionViewBase()
	}}

	// --- output safety ---------------------------------------------------------
	base.define("raw", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.newSafeBuffer(actionview.Raw(avToS(avArg(args, 0))))
	})
	hEscape := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		v := avArg(args, 0)
		if sb, ok := v.(*SafeBufferVal); ok {
			return sb
		}
		return vm.newSafeBuffer(actionview.Raw(actionview.HTMLEscape(avToS(v))))
	}
	base.define("html_escape", hEscape)
	base.define("h", hEscape)

	// --- tag helpers -----------------------------------------------------------
	base.define("content_tag", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		name := avToS(avArg(args, 0))
		if blk != nil {
			content := avValue(vm.callBlock(blk, nil))
			return vm.newSafeBuffer(actionview.ContentTag(name, content, avOpts(avArg(args, 1))))
		}
		return vm.newSafeBuffer(actionview.ContentTag(name, avValue(avArg(args, 1)), avOpts(avArg(args, 2))))
	})
	base.define("tag", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.newSafeBuffer(actionview.Tag(avToS(avArg(args, 0)), avValue(avArg(args, 1)), avOpts(avArg(args, 2))))
	})
	tokenList := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vals := make([]any, len(args))
		for i, a := range args {
			vals[i] = avValue(a)
		}
		return vm.newSafeBuffer(actionview.TokenList(vals...))
	}
	base.define("token_list", tokenList)
	base.define("class_names", tokenList)
	base.define("cdata_section", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.newSafeBuffer(actionview.CDATASection(avToS(avArg(args, 0))))
	})

	// --- url helpers -----------------------------------------------------------
	base.define("link_to", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if blk != nil {
			url := avToS(avArg(args, 0))
			content := avValue(vm.callBlock(blk, nil))
			return vm.newSafeBuffer(actionview.LinkTo(content, url, avOpts(avArg(args, 1))))
		}
		return vm.newSafeBuffer(actionview.LinkTo(avValue(avArg(args, 0)), avToS(avArg(args, 1)), avOpts(avArg(args, 2))))
	})
	base.define("mail_to", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.newSafeBuffer(actionview.MailTo(avToS(avArg(args, 0)), avValue(avArg(args, 1)), avOpts(avArg(args, 2))))
	})
	base.define("button_to", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*ActionViewBase)
		opts := avOptHash(args, 2)
		method := avHashStr(opts, "method", "post")
		return vm.newSafeBuffer(b.context().ButtonTo(avValue(avArg(args, 0)), avToS(avArg(args, 1)), method, avOptsExcept(opts, "method")))
	})
	base.define("current_page?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*ActionViewBase)
		return object.Bool(b.context().CurrentPage(avArg(args, 0)))
	})
	base.define("url_for", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*ActionViewBase)
		return object.NewString(b.urlFor(avArg(args, 0)))
	})

	vm.registerActionViewFormTagHelpers(base)
	vm.registerActionViewTextHelpers(base)
	vm.registerActionViewNumberHelpers(base)
	vm.registerActionViewConfig(base)
	vm.registerActionViewRender(base)
}

// registerActionViewFormTagHelpers installs the object-independent form_tag-family
// helpers (the *_tag inputs, select_tag, submit/button, options_for_select and
// form_tag itself) on ActionView::Base.
func (vm *VM) registerActionViewFormTagHelpers(base *RClass) {
	base.define("text_field_tag", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.newSafeBuffer(actionview.TextFieldTag(avToS(avArg(args, 0)), avValue(avArg(args, 1)), avOpts(avArg(args, 2))))
	})
	base.define("password_field_tag", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.newSafeBuffer(actionview.PasswordFieldTag(avToS(avArg(args, 0)), avValue(avArg(args, 1)), avOpts(avArg(args, 2))))
	})
	base.define("hidden_field_tag", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.newSafeBuffer(actionview.HiddenFieldTag(avToS(avArg(args, 0)), avValue(avArg(args, 1)), avOpts(avArg(args, 2))))
	})
	base.define("text_area_tag", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.newSafeBuffer(actionview.TextAreaTag(avToS(avArg(args, 0)), avValue(avArg(args, 1)), avOpts(avArg(args, 2))))
	})
	base.define("check_box_tag", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		checked := false
		if v := avArg(args, 2); v != nil {
			checked = v.Truthy()
		}
		return vm.newSafeBuffer(actionview.CheckBoxTag(avToS(avArg(args, 0)), avToS(avArg(args, 1)), checked, avOpts(avArg(args, 3))))
	})
	base.define("radio_button_tag", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		checked := false
		if v := avArg(args, 2); v != nil {
			checked = v.Truthy()
		}
		return vm.newSafeBuffer(actionview.RadioButtonTag(avToS(avArg(args, 0)), avToS(avArg(args, 1)), checked, avOpts(avArg(args, 3))))
	})
	base.define("select_tag", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		opts := avOptHash(args, 2)
		includeBlank := avHashBool(opts, "include_blank", false)
		return vm.newSafeBuffer(actionview.SelectTag(avToS(avArg(args, 0)), avToSafeBuffer(avArg(args, 1)), includeBlank, avOptsExcept(opts, "include_blank")))
	})
	base.define("label_tag", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.newSafeBuffer(actionview.LabelTag(avToS(avArg(args, 0)), avToS(avArg(args, 1)), avOpts(avArg(args, 2))))
	})
	base.define("submit_tag", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.newSafeBuffer(actionview.SubmitTag(avToS(avArg(args, 0)), avOpts(avArg(args, 1))))
	})
	base.define("button_tag", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		if blk != nil {
			return vm.newSafeBuffer(actionview.ButtonTag(avValue(vm.callBlock(blk, nil)), avOpts(avArg(args, 0))))
		}
		return vm.newSafeBuffer(actionview.ButtonTag(avValue(avArg(args, 0)), avOpts(avArg(args, 1))))
	})
	base.define("options_for_select", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.newSafeBuffer(actionview.OptionsForSelect(avChoices(avArg(args, 0)), avToS(avArg(args, 1))))
	})
	base.define("form_tag", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		b := self.(*ActionViewBase)
		opts := avOptHash(args, 1)
		method := avHashStr(opts, "method", "post")
		url := avToS(avArg(args, 0))
		attrs := avOptsExcept(opts, "method")
		if blk != nil {
			body := avToSafeBuffer(vm.callBlock(blk, nil))
			return vm.newSafeBuffer(b.context().FormTag(url, method, attrs, body))
		}
		return vm.newSafeBuffer(b.context().FormTagOpen(url, method, attrs))
	})
}

// registerActionViewTextHelpers installs the TextHelper family (truncate,
// pluralize, simple_format, word_wrap, highlight, excerpt) on ActionView::Base.
func (vm *VM) registerActionViewTextHelpers(base *RClass) {
	base.define("truncate", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		o := avOptHash(args, 1)
		return vm.newSafeBuffer(actionview.Truncate(avToS(avArg(args, 0)),
			avHashInt(o, "length", 30), avHashStr(o, "omission", "..."),
			avHashStr(o, "separator", ""), avHashBool(o, "escape", true)))
	})
	base.define("pluralize", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(actionview.Pluralize(avValue(avArg(args, 0)), avToS(avArg(args, 1)), avToS(avArg(args, 2))))
	})
	base.define("simple_format", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.newSafeBuffer(actionview.SimpleFormat(avToS(avArg(args, 0)),
			avOpts(avArg(args, 1)), avHashStr(avOptHash(args, 2), "wrapper_tag", ""), nil))
	})
	base.define("word_wrap", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		o := avOptHash(args, 1)
		return object.NewString(actionview.WordWrap(avToS(avArg(args, 0)),
			avHashInt(o, "line_width", 80), avHashStr(o, "break_sequence", "\n")))
	})
	base.define("highlight", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		o := avOptHash(args, 2)
		return vm.newSafeBuffer(actionview.Highlight(avToS(avArg(args, 0)),
			avPhrases(avArg(args, 1)), avHashStr(o, "highlighter", ""), nil))
	})
	base.define("excerpt", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		o := avOptHash(args, 2)
		s, ok := actionview.Excerpt(avToS(avArg(args, 0)), avToS(avArg(args, 1)),
			avHashInt(o, "radius", 100), avHashStr(o, "omission", "..."), avHashStr(o, "separator", ""))
		if !ok {
			return object.NilV
		}
		return object.NewString(s)
	})
}

// registerActionViewNumberHelpers installs the NumberHelper family on
// ActionView::Base, each returning the library's formatted (plain) String.
func (vm *VM) registerActionViewNumberHelpers(base *RClass) {
	num := func(fn func(any, ...actionview.Option) string) NativeFn {
		return func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.NewString(fn(avValue(avArg(args, 0)), avNumberOpts(avOptHash(args, 1))...))
		}
	}
	base.define("number_to_currency", num(actionview.NumberToCurrency))
	base.define("number_with_delimiter", num(actionview.NumberWithDelimiter))
	base.define("number_to_delimited", num(actionview.NumberToDelimited))
	base.define("number_with_precision", num(actionview.NumberWithPrecision))
	base.define("number_to_rounded", num(actionview.NumberToRounded))
	base.define("number_to_percentage", num(actionview.NumberToPercentage))
	base.define("number_to_human_size", num(actionview.NumberToHumanSize))
	base.define("number_to_human", num(actionview.NumberToHuman))
}

// registerActionViewConfig installs the view-context configuration writers: the
// request/CSRF state and the two host seam callables (url_for= for routing,
// render_template= for the template resolver a later actionpack binding wires).
func (vm *VM) registerActionViewConfig(base *RClass) {
	base.define("authenticity_token=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*ActionViewBase).authenticityToken = avToS(avArg(args, 0))
		return avArg(args, 0)
	})
	base.define("protect_against_forgery=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*ActionViewBase).protectAgainstForgery = avArg(args, 0).Truthy()
		return avArg(args, 0)
	})
	base.define("suppress_utf8_enforcer=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*ActionViewBase).suppressUTF8 = avArg(args, 0).Truthy()
		return avArg(args, 0)
	})
	base.define("request_method=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*ActionViewBase).requestMethod = avToS(avArg(args, 0))
		return avArg(args, 0)
	})
	base.define("request_path=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*ActionViewBase).requestPath = avToS(avArg(args, 0))
		return avArg(args, 0)
	})
	base.define("request_fullpath=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*ActionViewBase).requestFullpath = avToS(avArg(args, 0))
		return avArg(args, 0)
	})
	base.define("url_for=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*ActionViewBase).urlForProc = avProc(avArg(args, 0))
		return avArg(args, 0)
	})
	base.define("render_template=", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		self.(*ActionViewBase).renderTemplateProc = avProc(avArg(args, 0))
		return avArg(args, 0)
	})
}

// registerActionViewRender installs ActionView::Base#render — the dispatchable
// render entry point. It maps a render options Hash (inline / template / partial /
// collection / locals / as / spacer_template) to the library's RenderOptions and
// defers template evaluation to the context's RenderTemplate seam. A later
// actionpack/actionmailer binding renders a view by sending :render to a Base with
// its own render_template callable wired, so the whole pipeline funnels through
// this one method.
func (vm *VM) registerActionViewRender(base *RClass) {
	base.define("render", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*ActionViewBase)
		h, ok := avArg(args, 0).(*object.Hash)
		if !ok {
			raise("ArgumentError", "render expects an options Hash")
		}
		ro := actionview.RenderOptions{
			Inline:   avHashStr(h, "inline", ""),
			Template: avHashStr(h, "template", ""),
			Partial:  avHashStr(h, "partial", ""),
			As:       avHashStr(h, "as", ""),
			Spacer:   avHashStr(h, "spacer_template", ""),
			Locals:   avLocalsMap(h),
		}
		if v, ok := avHashVal(h, "collection"); ok {
			if arr, ok := v.(*object.Array); ok {
				ro.Collection = avCollection(arr)
			}
		}
		out, err := b.context().Render(ro)
		if err != nil {
			raise("ArgumentError", "%s", err.Error())
		}
		return vm.newSafeBuffer(out)
	})
}

// avLocalsMap reads the render :locals option into a Go map keyed by the local
// name, holding each value as its original Ruby object.Value so the seam can bind
// it verbatim. A missing / non-Hash :locals yields no locals.
func avLocalsMap(h *object.Hash) map[string]any {
	lv, ok := avHashVal(h, "locals")
	if !ok {
		return nil
	}
	lh, ok := lv.(*object.Hash)
	if !ok {
		return nil
	}
	out := make(map[string]any, len(lh.Keys))
	for _, k := range lh.Keys {
		val, _ := lh.Get(k)
		out[avKey(k)] = val
	}
	return out
}

// avCollection maps a render :collection Array to a []any of the elements' Ruby
// values (kept as object.Value so the per-element local binds the real object).
func avCollection(arr *object.Array) []any {
	out := make([]any, len(arr.Elems))
	for i, e := range arr.Elems {
		out[i] = e
	}
	return out
}

// avOptsExcept converts an options Hash to Attrs while dropping the named keys
// (e.g. the :method / :include_blank keys a helper consumes itself rather than
// emitting as an HTML attribute).
func avOptsExcept(h *object.Hash, drop ...string) actionview.Attrs {
	if h == nil {
		return nil
	}
	skip := make(map[string]bool, len(drop))
	for _, d := range drop {
		skip[d] = true
	}
	out := make(actionview.Attrs, 0, len(h.Keys))
	for _, k := range h.Keys {
		name := avKey(k)
		if skip[name] {
			continue
		}
		val, _ := h.Get(k)
		out = append(out, actionview.Attr{Key: name, Val: avValue(val)})
	}
	return out
}

// avToSafeBuffer coerces an argument to a library SafeBuffer of already-safe
// markup: a Ruby SafeBuffer contributes its markup verbatim, any other value its
// to_s (used for a form/select body and the render result, which are assembled
// from helper output).
func avToSafeBuffer(v object.Value) actionview.SafeBuffer {
	if sb, ok := v.(*SafeBufferVal); ok {
		return *sb.buf
	}
	return actionview.Raw(avToS(v))
}

// avChoices maps an options_for_select container to the library's []Choice: a
// two-element Array element becomes Choice{text, value}, and any other element is
// used as both text and value.
func avChoices(v object.Value) []actionview.Choice {
	arr, ok := v.(*object.Array)
	if !ok {
		return nil
	}
	out := make([]actionview.Choice, len(arr.Elems))
	for i, e := range arr.Elems {
		if pair, ok := e.(*object.Array); ok && len(pair.Elems) == 2 {
			out[i] = actionview.Choice{Text: avToS(pair.Elems[0]), Value: avToS(pair.Elems[1])}
			continue
		}
		s := avToS(e)
		out[i] = actionview.Choice{Text: s, Value: s}
	}
	return out
}

// avPhrases maps the highlight phrases argument (a String or an Array of Strings)
// to a []string.
func avPhrases(v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		out := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = avToS(e)
		}
		return out
	}
	return []string{avToS(v)}
}

// avNumberOpts maps a number-helper options Hash to the library's functional
// Options: precision / significant / separator / delimiter / unit / format /
// negative_format / strip_insignificant_zeros. An absent key leaves the helper's
// own default in place.
func avNumberOpts(h *object.Hash) []actionview.Option {
	if h == nil {
		return nil
	}
	var o []actionview.Option
	if v, ok := avHashVal(h, "precision"); ok {
		if i, ok := v.(object.Integer); ok {
			o = append(o, actionview.Precision(int(i)))
		}
	}
	if v, ok := avHashVal(h, "significant"); ok {
		o = append(o, actionview.Significant(v.Truthy()))
	}
	if v, ok := avHashVal(h, "separator"); ok {
		o = append(o, actionview.Separator(avToS(v)))
	}
	if v, ok := avHashVal(h, "delimiter"); ok {
		o = append(o, actionview.Delimiter(avToS(v)))
	}
	if v, ok := avHashVal(h, "unit"); ok {
		o = append(o, actionview.Unit(avToS(v)))
	}
	if v, ok := avHashVal(h, "format"); ok {
		o = append(o, actionview.Format(avToS(v)))
	}
	if v, ok := avHashVal(h, "negative_format"); ok {
		o = append(o, actionview.NegativeFormat(avToS(v)))
	}
	if v, ok := avHashVal(h, "strip_insignificant_zeros"); ok {
		o = append(o, actionview.StripInsignificantZeros(v.Truthy()))
	}
	return o
}

// avProc returns the Proc value of v (a block/lambda passed to a seam writer), or
// nil when v is nil / not a Proc (clearing the seam).
func avProc(v object.Value) *Proc {
	if p, ok := v.(*Proc); ok {
		return p
	}
	return nil
}
