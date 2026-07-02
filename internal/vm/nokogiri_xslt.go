// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	nokogiri "github.com/go-ruby-nokogiri/nokogiri"
	xslt "github.com/go-ruby-xslt/xslt"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// NokogiriXSLTStylesheet is the Ruby wrapper around a *xslt.Stylesheet — a
// compiled XSLT 1.0 stylesheet (Nokogiri::XSLT::Stylesheet). The real transform
// engine lives in the github.com/go-ruby-xslt/xslt library, a pure-Go XSLT 1.0
// processor over the go-ruby-nokogiri XML DOM + XPath engine (so the whole path
// stays CGO-free). This shell wires Nokogiri::XSLT(str) to a compiled stylesheet
// whose #transform(doc) returns a Nokogiri document and #apply_to(doc) returns
// the serialised output String, mirroring the gem's libxslt surface.
type NokogiriXSLTStylesheet struct {
	s *xslt.Stylesheet
}

func (s *NokogiriXSLTStylesheet) ToS() string     { return "#<Nokogiri::XSLT::Stylesheet>" }
func (s *NokogiriXSLTStylesheet) Inspect() string { return "#<Nokogiri::XSLT::Stylesheet>" }
func (s *NokogiriXSLTStylesheet) Truthy() bool    { return true }

// registerNokogiriXSLT installs the Nokogiri::XSLT module and its Stylesheet
// class onto the already-registered Nokogiri module (require "nokogiri" is what
// loads it — XSLT rides the same feature). Nokogiri::XSLT(str) / .parse(str)
// compile an XSLT source String into a Stylesheet; #transform(doc[, params])
// returns a transformed Nokogiri::XML::Document and #apply_to / #serialize the
// serialised output String. A malformed stylesheet raises Nokogiri::XSLT::SyntaxError
// (a subclass of Nokogiri::SyntaxError), matching how the gem surfaces a
// compilation failure.
func (vm *VM) registerNokogiriXSLT() {
	nok, ok := vm.consts["Nokogiri"].(*RClass)
	if !ok {
		return
	}

	xsltMod := newClass("Nokogiri::XSLT", nil)
	xsltMod.isModule = true
	nok.consts["XSLT"] = xsltMod
	vm.consts["Nokogiri::XSLT"] = xsltMod

	// Nokogiri::XSLT::SyntaxError < Nokogiri::SyntaxError for a compile failure.
	synErr := vm.consts["Nokogiri::SyntaxError"].(*RClass)
	xsltErr := newClass("Nokogiri::XSLT::SyntaxError", synErr)
	xsltMod.consts["SyntaxError"] = xsltErr
	vm.consts["Nokogiri::XSLT::SyntaxError"] = xsltErr

	stylesheet := newClass("Nokogiri::XSLT::Stylesheet", vm.cObject)
	xsltMod.consts["Stylesheet"] = stylesheet
	vm.consts["Nokogiri::XSLT::Stylesheet"] = stylesheet

	// Nokogiri::XSLT(str) is a method call on the Nokogiri module (Ruby's
	// Nokogiri::XSLT() constructor sugar), and Nokogiri::XSLT.parse(str) is the
	// same compilation on the nested module. Both compile the source into a
	// Stylesheet.
	parse := func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return nokogiriXSLTParse(nokogiriSourceArg(args))
	}
	nok.smethods["XSLT"] = &Method{name: "XSLT", owner: nok, native: parse}
	xsltMod.smethods["parse"] = &Method{name: "parse", owner: xsltMod, native: parse}

	vm.registerNokogiriXSLTStylesheet(stylesheet)
}

// registerNokogiriXSLTStylesheet installs the Stylesheet instance surface:
// #transform (returns a Nokogiri::XML::Document) and #apply_to / #serialize
// (returns the serialised output String), each accepting an optional params Hash.
func (vm *VM) registerNokogiriXSLTStylesheet(cls *RClass) {
	self := func(v object.Value) *xslt.Stylesheet { return v.(*NokogiriXSLTStylesheet).s }

	cls.define("transform", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		doc, params := nokogiriXSLTArgs(args)
		out, err := self(v).Transform(doc, params)
		if err != nil {
			raiseNokogiriError(err)
		}
		return &NokogiriDocument{doc: out}
	})

	apply := func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		doc, params := nokogiriXSLTArgs(args)
		out, err := self(v).Apply(doc, params)
		if err != nil {
			raiseNokogiriError(err)
		}
		return object.NewString(out)
	}
	cls.define("apply_to", apply)
	cls.define("serialize", apply)
}

// nokogiriXSLTParse compiles an XSLT source String into a Stylesheet, raising a
// Ruby Nokogiri::XSLT::SyntaxError on a malformed stylesheet.
func nokogiriXSLTParse(src string) object.Value {
	s, err := xslt.ParseString(src)
	if err != nil {
		raise("Nokogiri::XSLT::SyntaxError", "%s", err.Error())
	}
	return &NokogiriXSLTStylesheet{s: s}
}

// nokogiriXSLTArgs reads the transform / apply_to arguments: the first argument
// must be a Nokogiri::XML::Document (the source tree); a trailing Hash supplies
// stylesheet parameters (its keys' to_s names mapped to string/float/bool
// values). A missing or non-Document first argument raises TypeError.
func nokogiriXSLTArgs(args []object.Value) (*nokogiri.Document, map[string]any) {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..2)")
	}
	doc, ok := args[0].(*NokogiriDocument)
	if !ok {
		raise("TypeError", "no implicit conversion into Nokogiri::XML::Document")
	}
	var params map[string]any
	if len(args) > 1 {
		if h, ok := args[1].(*object.Hash); ok {
			params = nokogiriXSLTParams(h)
		}
	}
	return doc.doc, params
}

// nokogiriXSLTParams maps a Ruby params Hash to the map[string]any the xslt
// library consumes: each key's to_s name mapped to a string / float64 / bool /
// nil value (the value shapes the engine accepts as a stylesheet parameter).
func nokogiriXSLTParams(h *object.Hash) map[string]any {
	m := make(map[string]any, len(h.Keys))
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		m[nokogiriStr(k)] = nokogiriXSLTParamValue(val)
	}
	return m
}

// nokogiriXSLTParamValue maps one Ruby parameter value to the Go value the xslt
// engine accepts (string / float64 / bool / nil), defaulting to the value's to_s.
func nokogiriXSLTParamValue(v object.Value) any {
	switch n := v.(type) {
	case object.Nil:
		return nil
	case object.Bool:
		return bool(n)
	case object.Integer:
		return float64(n)
	case object.Float:
		return float64(n)
	case *object.String:
		return n.Str()
	case object.Symbol:
		return string(n)
	}
	return v.ToS()
}
