// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	erubi "github.com/go-ruby-erubi/erubi"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent template-to-Ruby-source compiler of
// github.com/go-ruby-erubi/erubi (a pure-Go, no-cgo port of the erubi gem —
// Rails' default ERB engine). Like the ERB binding (erb.go), erubi compiles a
// template to the Ruby #src an interpreter then evals; the compile lives wholly
// in the library, and rbgo evals the emitted source (whose ::Erubi.h escape
// calls are answered by the Erubi.h module function wired in registerErubi).

// ErubiEngine is an instance of Erubi::Engine (or its Erubi::CaptureEndEngine
// subclass): the compiled result of one template. Its readers expose the frozen
// Ruby #src, the #filename it reports, and the #bufvar naming its output buffer,
// all produced by the go-ruby-erubi compiler at construction. The cls field lets
// classOf report the exact class (Engine vs CaptureEndEngine).
type ErubiEngine struct {
	cls      *RClass
	src      string
	filename string
	bufvar   string
}

// ToS renders the default Object#to_s form (#<Erubi::Engine>); the engine is an
// opaque compiled artifact with no meaningful string value of its own.
func (e *ErubiEngine) ToS() string { return "#<" + e.cls.name + ">" }

// Inspect matches ToS: there is no richer inspect for the compiled engine.
func (e *ErubiEngine) Inspect() string { return e.ToS() }

// Truthy reports true, as every non-nil, non-false object does.
func (e *ErubiEngine) Truthy() bool { return true }

// erubiEngineLike is the shared surface of the library's *Engine and its
// *CaptureEndEngine (which embeds *Engine): the three compiled outputs the
// binding copies into an ErubiEngine. The ctor seams below are typed to it so
// both engine kinds funnel through one build helper.
type erubiEngineLike interface {
	Src() string
	Filename() string
	BufVar() string
}

// erubiEngineCtor / erubiCaptureEndCtor are seams over the library constructors,
// letting a test drive the otherwise-unreachable InvalidIndicatorError panic
// path (only reachable via a custom scanning Regexp, which this binding never
// exposes) so buildErubiEngine's ArgumentError translation stays covered.
var erubiEngineCtor = func(input string, opts erubi.Options) erubiEngineLike {
	return erubi.NewEngine(input, opts)
}
var erubiCaptureEndCtor = func(input string, opts erubi.Options) erubiEngineLike {
	return erubi.NewCaptureEndEngine(input, opts)
}

// buildErubiEngine compiles input through ctor, wrapping the result in an
// ErubiEngine of class cls. The library raises invalid template configuration by
// panicking with *InvalidIndicatorError (mirroring Erubi::Engine#handle's
// ArgumentError); that panic is translated here to a Ruby ArgumentError, while
// any other panic (e.g. the TypeError strArg raises for a non-String template)
// is re-raised unchanged.
func buildErubiEngine(cls *RClass, ctor func(string, erubi.Options) erubiEngineLike, args []object.Value) (ret object.Value) {
	defer func() {
		if r := recover(); r != nil {
			if ie, ok := r.(*erubi.InvalidIndicatorError); ok {
				raise("ArgumentError", "%s", ie.Error())
			}
			panic(r)
		}
	}()
	input := strArg(args[0])
	e := ctor(input, erubiOptions(args[1:]))
	return &ErubiEngine{cls: cls, src: e.Src(), filename: e.Filename(), bufvar: e.BufVar()}
}

// erubiOptions maps the keyword Hash of Erubi::Engine.new / CaptureEndEngine.new
// to the library's Options, mirroring the erubi gem's keyword arguments. The
// tri-state booleans (escape / escape_html / trim / freeze_template_literals)
// stay unset when absent or nil — matching a missing Ruby hash key — so the
// library applies erubi's own defaults; the plain booleans (freeze /
// chain_appends / ensure) default to false; the string options fall back to the
// library's defaults when absent.
func erubiOptions(rest []object.Value) erubi.Options {
	var o erubi.Options
	h := erubiOptsHash(rest)
	if h == nil {
		return o
	}
	triBool := func(key string, set func(*bool)) {
		if v, ok := h.Get(object.Symbol(key)); ok {
			if _, isNil := v.(object.Nil); !isNil {
				set(erubi.Bool(v.Truthy()))
			}
		}
	}
	plainBool := func(key string, set func(bool)) {
		if v, ok := h.Get(object.Symbol(key)); ok {
			set(v.Truthy())
		}
	}
	strOpt := func(key string, set func(string)) {
		if v, ok := h.Get(object.Symbol(key)); ok {
			set(strArg(v))
		}
	}

	triBool("escape", func(p *bool) { o.Escape = p })
	triBool("escape_html", func(p *bool) { o.EscapeHTML = p })
	triBool("trim", func(p *bool) { o.Trim = p })
	triBool("freeze_template_literals", func(p *bool) { o.FreezeTemplateLiterals = p })

	plainBool("freeze", func(b bool) { o.Freeze = b })
	plainBool("chain_appends", func(b bool) { o.ChainAppends = b })
	plainBool("ensure", func(b bool) { o.Ensure = b })

	strOpt("escapefunc", func(s string) { o.EscapeFunc = s })
	strOpt("bufvar", func(s string) { o.BufVar = s })
	strOpt("bufval", func(s string) { o.BufVal = s })
	strOpt("filename", func(s string) { o.Filename = s })
	strOpt("preamble", func(s string) { o.Preamble = s })
	strOpt("postamble", func(s string) { o.Postamble = s })
	strOpt("literal_prefix", func(s string) { o.LiteralPrefix = s })
	strOpt("literal_postfix", func(s string) { o.LiteralPostfix = s })
	return o
}

// erubiOptsHash returns the trailing keyword Hash of an Erubi constructor call,
// or nil when the last argument is not a Hash (a bare template with no options).
func erubiOptsHash(rest []object.Value) *object.Hash {
	if len(rest) == 0 {
		return nil
	}
	h, ok := rest[len(rest)-1].(*object.Hash)
	if !ok {
		return nil
	}
	return h
}
