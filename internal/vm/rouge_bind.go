// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	rouge "github.com/go-ruby-rouge/rouge"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-rouge/rouge highlighter. The lexers,
// the regex-lexer engine and the HTML formatters live in that library; rbgo only
// maps the source String and lexer/formatter names to rouge.Highlight /
// rouge.FindLexer / rouge.FindFormatter, so the gem-faithful HTML the Rouge module
// relies on is preserved by construction.

// rougeLexer is the opaque native handle stored on a Rouge::Lexer instance by
// Lexer.find. It carries the found rouge.Lexer so #tag / #title / #aliases can read
// its metadata. It satisfies object.Value only so it can live in an ivar.
type rougeLexer struct{ lx rouge.Lexer }

func (h *rougeLexer) ToS() string     { return "#<Rouge::Lexer>" }
func (h *rougeLexer) Inspect() string { return h.ToS() }
func (h *rougeLexer) Truthy() bool    { return true }

// rougeHighlight tokenizes text with the named lexer and renders it with the named
// formatter. An unknown lexer or formatter name raises Rouge::Error (the gem raises
// on an unknown name), so the failure rescues as the right Ruby class.
func rougeHighlight(text, lexer, formatter string) string {
	out, err := rouge.Highlight(text, lexer, formatter)
	if err != nil {
		raise("Rouge::Error", "%s", err.Error())
	}
	return out
}

// rougeFindLexer returns a Rouge::Lexer instance wrapping the lexer registered
// under name, or Ruby nil when none matches (mirroring Rouge::Lexer.find returning
// nil). The found lexer handle is stored on the instance so its metadata accessors
// can read it back.
func rougeFindLexer(cls *RClass, name string) object.Value {
	lx := rouge.FindLexer(name)
	if lx == nil {
		return object.NilV
	}
	inst := &RObject{class: cls, ivars: map[string]object.Value{}}
	inst.ivars["@__lexer"] = &rougeLexer{lx: lx}
	return inst
}

// rougeLexerHandle returns the native lexer handle stored on a Rouge::Lexer
// instance. A receiver without one raises, matching a lexer object built outside
// Lexer.find.
func rougeLexerHandle(self object.Value) *rougeLexer {
	if h, ok := object.KindOK[*rougeLexer](getIvar(self, "@__lexer")); ok {
		return h
	}
	raise("Rouge::Error", "lexer was not found")
	return nil
}

// rougeFindFormatter returns the tag of the formatter registered under tag as a
// String, or Ruby nil when none matches (mirroring Rouge::Formatter.find resolving
// a formatter). rbgo models a formatter by its tag name, which is all
// Rouge.highlight needs to select one.
func rougeFindFormatter(tag string) object.Value {
	if f := rouge.FindFormatter(tag); f != nil {
		return object.NewString(tag)
	}
	return object.NilV
}
