// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"testing"
)

// A minimal identity-copy stylesheet plus a value-of stylesheet, kept on one
// line so the rbgo lexer takes them verbatim.
const xsltIdentity = `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform"><xsl:output method="xml" omit-xml-declaration="yes"/><xsl:template match="/"><out><xsl:value-of select="/root/item"/></out></xsl:template></xsl:stylesheet>`

// TestNokogiriXSLTConstants covers the Nokogiri::XSLT module, its Stylesheet
// class and its SyntaxError class (loaded by require "nokogiri").
func TestNokogiriXSLTConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "nokogiri"; p Nokogiri::XSLT.is_a?(Module)`, "true\n"},
		{`require "nokogiri"; p Nokogiri::XSLT::SyntaxError < Nokogiri::SyntaxError`, "true\n"},
		{`require "nokogiri"; xsl=%q{` + xsltIdentity + `}; p Nokogiri::XSLT(xsl).class`, "Nokogiri::XSLT::Stylesheet\n"},
		{`require "nokogiri"; xsl=%q{` + xsltIdentity + `}; p Nokogiri::XSLT.parse(xsl).class`, "Nokogiri::XSLT::Stylesheet\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestNokogiriXSLTTransform covers #transform (-> Nokogiri::XML::Document),
// #apply_to and #serialize (-> serialised String), including the params Hash.
func TestNokogiriXSLTTransform(t *testing.T) {
	cases := []struct{ src, want string }{
		// transform returns a Document.
		{`require "nokogiri"
xsl=%q{` + xsltIdentity + `}
doc = Nokogiri::XML("<root><item>hi</item></root>")
p Nokogiri::XSLT(xsl).transform(doc).class`, "Nokogiri::XML::Document\n"},
		// apply_to serialises the result tree to a String.
		{`require "nokogiri"
xsl=%q{` + xsltIdentity + `}
doc = Nokogiri::XML("<root><item>hi</item></root>")
puts Nokogiri::XSLT(xsl).apply_to(doc)`, "<out>hi</out>\n"},
		// serialize is an alias of apply_to.
		{`require "nokogiri"
xsl=%q{` + xsltIdentity + `}
doc = Nokogiri::XML("<root><item>x</item></root>")
puts Nokogiri::XSLT(xsl).serialize(doc)`, "<out>x</out>\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestNokogiriXSLTParams covers a stylesheet parameter passed as the transform /
// apply_to params Hash (Integer, String, Symbol, true, nil value arms).
func TestNokogiriXSLTParams(t *testing.T) {
	const withParam = `<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform"><xsl:output method="xml" omit-xml-declaration="yes"/><xsl:param name="who"/><xsl:template match="/"><out><xsl:value-of select="$who"/></out></xsl:template></xsl:stylesheet>`
	src := `require "nokogiri"
xsl=%q{` + withParam + `}
doc = Nokogiri::XML("<root/>")
puts Nokogiri::XSLT(xsl).apply_to(doc, {"who" => "bob", :n => 1, :flag => true, :s => :sym, :z => nil})`
	if got := eval(t, src); got != "<out>bob</out>\n" {
		t.Errorf("params: got %q", got)
	}
}

// TestNokogiriXSLTErrors covers the error arms: a malformed stylesheet raises
// Nokogiri::XSLT::SyntaxError, a missing / non-Document transform argument raises,
// and a transform failure surfaces as Nokogiri::SyntaxError.
func TestNokogiriXSLTErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		// Malformed stylesheet -> compile SyntaxError.
		{`require "nokogiri"; begin; Nokogiri::XSLT("<nope/>"); rescue Nokogiri::XSLT::SyntaxError; p :caught; end`, ":caught\n"},
		// No arguments to XSLT() -> ArgumentError.
		{`require "nokogiri"; begin; Nokogiri::XSLT(); rescue ArgumentError; p :argerr; end`, ":argerr\n"},
		// transform with no argument -> ArgumentError.
		{`require "nokogiri"
xsl=%q{` + xsltIdentity + `}
begin; Nokogiri::XSLT(xsl).transform; rescue ArgumentError; p :argerr; end`, ":argerr\n"},
		// transform with a non-Document argument -> TypeError.
		{`require "nokogiri"
xsl=%q{` + xsltIdentity + `}
begin; Nokogiri::XSLT(xsl).transform("nope"); rescue TypeError; p :typeerr; end`, ":typeerr\n"},
		// apply_to with a non-Document argument -> TypeError.
		{`require "nokogiri"
xsl=%q{` + xsltIdentity + `}
begin; Nokogiri::XSLT(xsl).apply_to(42); rescue TypeError; p :typeerr; end`, ":typeerr\n"},
		// A stylesheet that parses but errors at transform-time (bad XPath in a
		// value-of) surfaces as Nokogiri::SyntaxError from both transform and apply_to.
		{`require "nokogiri"
xsl=%q{<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform"><xsl:template match="/"><out><xsl:value-of select="///bad"/></out></xsl:template></xsl:stylesheet>}
doc = Nokogiri::XML("<root/>")
begin; Nokogiri::XSLT(xsl).transform(doc); rescue Nokogiri::SyntaxError; p :t; end
begin; Nokogiri::XSLT(xsl).apply_to(doc); rescue Nokogiri::SyntaxError; p :a; end`, ":t\n:a\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
