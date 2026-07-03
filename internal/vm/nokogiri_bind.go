// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	nokogiri "github.com/go-ruby-nokogiri/nokogiri"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph (object.Value)
// and the interpreter-independent DOM / query model of
// github.com/go-ruby-nokogiri/nokogiri (pure-Go, no cgo, no libxml2). The HTML
// and XML parsers, the CSS-to-XPath translator and the XPath engine live in that
// library; rbgo only maps a Ruby markup String onto Nokogiri.HTML / Nokogiri.XML
// and wraps the resulting Document / Node / NodeSet in the shells the Nokogiri
// module dispatches on.

// nokogiriParseHTML parses an HTML source String into a Document. The HTML
// parser (golang.org/x/net/html via the library) is lenient and only surfaces an
// error from the underlying reader, which a String reader never produces, so
// parsing a String cannot fail — no error branch is needed.
func nokogiriParseHTML(src string) object.Value {
	doc, _ := nokogiri.HTML(src)
	return &NokogiriDocument{doc: doc}
}

// nokogiriParseXML parses an XML source String into a Document, raising a Ruby
// Nokogiri::SyntaxError on a parse failure (an XML document is parsed strictly,
// so malformed markup surfaces here).
func nokogiriParseXML(src string) object.Value {
	doc, err := nokogiri.XML(src)
	if err != nil {
		raiseNokogiriError(err)
	}
	return &NokogiriDocument{doc: doc}
}

// raiseNokogiriError re-raises a library error as a Ruby Nokogiri::SyntaxError.
// It never returns (raise panics).
func raiseNokogiriError(err error) {
	raise("Nokogiri::SyntaxError", "%s", err.Error())
}

// nokogiriSourceArg coerces the Nokogiri.HTML / Nokogiri.XML argument to its
// markup source String: a String yields its contents, any other value its to_s.
func nokogiriSourceArg(args []object.Value) string {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..)")
	}
	return nokogiriStr(args[0])
}

// nokogiriSelectorArg coerces a #css / #xpath selector argument to its String.
func nokogiriSelectorArg(args []object.Value) string {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
	}
	return nokogiriStr(args[0])
}

// nokogiriStr coerces a value to its string: a String yields its contents, a
// Symbol its name, and any other value its to_s.
func nokogiriStr(v object.Value) string {
	{
		__sw105 := v
		switch {
		case object.IsKind[*object.String](__sw105):
			n := object.Kind[*object.String](__sw105)
			_ = n
			return n.Str()
		case object.IsKind[object.Symbol](__sw105):
			n := object.Kind[object.Symbol](__sw105)
			_ = n
			return string(n)
		}
	}
	return v.ToS()
}

// nokogiriNodeOrNil wraps a node, mapping a Go nil (no match) to Ruby nil so
// #at_css / #parent / #next etc. return nil when there is no node.
func nokogiriNodeOrNil(n *nokogiri.Node) object.Value {
	if n == nil {
		return object.NilV
	}
	return &NokogiriNode{n: n}
}

// nokogiriNodeArray maps a NodeSet to a Ruby Array of wrapped nodes (#to_a).
func nokogiriNodeArray(set *nokogiri.NodeSet) *object.Array {
	nodes := set.Nodes()
	arr := object.NewArrayFromSlice(make([]object.Value, len(nodes)))
	for i, n := range nodes {
		arr.Elems[i] = &NokogiriNode{n: n}
	}
	return arr
}
