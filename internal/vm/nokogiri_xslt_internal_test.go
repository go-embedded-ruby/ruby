// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestRegisterNokogiriXSLTNoNokogiri covers the early return when the Nokogiri
// module is absent (registerNokogiriXSLT is a no-op rather than a panic).
func TestRegisterNokogiriXSLTNoNokogiri(t *testing.T) {
	vm := &VM{consts: map[string]object.Value{}}
	vm.registerNokogiriXSLT() // must not panic; nothing to register onto
	if _, ok := vm.consts["Nokogiri::XSLT"]; ok {
		t.Error("XSLT registered without a Nokogiri module")
	}
}

// TestNokogiriXSLTParamValue covers every arm of the parameter-value mapper,
// including the Float and default (to_s) arms not reached from the Ruby tests.
func TestNokogiriXSLTParamValue(t *testing.T) {
	cases := []struct {
		in   object.Value
		want any
	}{
		{object.NilV, nil},
		{object.Bool(true), true},
		{object.Integer(3), float64(3)},
		{object.Float(2.5), float64(2.5)},
		{object.NewString("s"), "s"},
		{object.Symbol("y"), "y"},
		{object.Integer(0), float64(0)},
	}
	for _, c := range cases {
		if got := nokogiriXSLTParamValue(c.in); got != c.want {
			t.Errorf("param(%v) = %v, want %v", c.in, got, c.want)
		}
	}
	// Default arm: a value that is none of the fast cases falls back to to_s.
	if got := nokogiriXSLTParamValue(&object.Array{Elems: []object.Value{object.Integer(1)}}); got != "[1]" {
		t.Errorf("default arm = %v, want [1]", got)
	}
}

// TestNokogiriXSLTStylesheetInspect covers the wrapper's ToS / Inspect / Truthy.
func TestNokogiriXSLTStylesheetInspect(t *testing.T) {
	s := object.Kind[*NokogiriXSLTStylesheet](nokogiriXSLTParse(`<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform"><xsl:template match="/"><o/></xsl:template></xsl:stylesheet>`))
	if s.ToS() != "#<Nokogiri::XSLT::Stylesheet>" {
		t.Errorf("ToS = %q", s.ToS())
	}
	if s.Inspect() != "#<Nokogiri::XSLT::Stylesheet>" {
		t.Errorf("Inspect = %q", s.Inspect())
	}
	if !s.Truthy() {
		t.Error("a Stylesheet is truthy")
	}
}

// TestNokogiriXSLTArgsNonHashParams covers nokogiriXSLTArgs's branch where the
// second argument is present but not a Hash (params stays nil).
func TestNokogiriXSLTArgsNonHashParams(t *testing.T) {
	doc := object.Kind[*NokogiriDocument](nokogiriParseXML("<root/>"))
	gotDoc, params := nokogiriXSLTArgs([]object.Value{doc, object.Integer(1)})
	if gotDoc == nil {
		t.Fatal("expected the document back")
	}
	if params != nil {
		t.Errorf("non-Hash second arg should yield nil params, got %v", params)
	}
}
