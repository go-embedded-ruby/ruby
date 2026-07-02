// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestBuilderModule covers the Builder module and Builder::XmlMarkup class
// (require "builder").
func TestBuilderModule(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "builder"; p Builder.is_a?(Module)`, "true\n"},
		{`require "builder"; p Builder::XmlMarkup.is_a?(Class)`, "true\n"},
		{`p require "builder"`, "true\n"},
		{`require "builder"; p require "builder"`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBuilderElements covers the method_missing element DSL: self-closing tags,
// text content, attributes (from a keyword Hash), and nested blocks.
func TestBuilderElements(t *testing.T) {
	cases := []struct{ src, want string }{
		// A text element.
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.name("Al")
			print xml.target!`, `<name>Al</name>`},
		// A self-closing (empty) element.
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.br
			print xml.target!`, `<br/>`},
		// Attributes from a keyword Hash, in order.
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.a("hi", href: "u", id: 1)
			print xml.target!`, `<a href="u" id="1">hi</a>`},
		// A nested block.
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.person { xml.name("Al"); xml.age(30) }
			print xml.target!`, `<person><name>Al</name><age>30</age></person>`},
		// A Symbol content value (rendered via to_s).
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.k(:v)
			print xml.target!`, `<k>v</k>`},
		// A nil content self-closes; nil attribute value is an empty attribute.
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.img(src: nil)
			print xml.target!`, `<img src=""/>`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBuilderTagBang covers the explicit tag! emitter (equivalent to
// method_missing but with the name given as the first argument).
func TestBuilderTagBang(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.tag!("name", "Al")
			print xml.target!`, `<name>Al</name>`},
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.tag!(:wrap) { xml.tag!(:inner, "x") }
			print xml.target!`, `<wrap><inner>x</inner></wrap>`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBuilderSpecials covers text!, <<, cdata!, comment! and instruct!.
func TestBuilderSpecials(t *testing.T) {
	cases := []struct{ src, want string }{
		// text! escapes character data.
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.text!("a & b")
			print xml.target!`, `a &amp; b`},
		// << inserts markup verbatim.
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml << "<raw/>"
			print xml.target!`, `<raw/>`},
		// cdata! wraps text in a CDATA section.
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.cdata!("x")
			print xml.target!`, `<![CDATA[x]]>`},
		// comment! emits an XML comment.
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.comment!("c")
			print xml.target!`, `<!-- c -->`},
		// text! / << / cdata! / comment! with a non-String argument coerce via to_s.
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.text!(42)
			print xml.target!`, `42`},
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml << 7
			print xml.target!`, `7`},
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.cdata!(9)
			print xml.target!`, `<![CDATA[9]]>`},
		// instruct! with no argument emits the XML declaration.
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.instruct!
			print xml.target!`, `<?xml version="1.0" encoding="UTF-8"?>`},
		// instruct! with an explicit directive and attributes.
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.instruct!(:xml, encoding: "US-ASCII")
			print xml.target!`, `<?xml version="1.0" encoding="US-ASCII"?>`},
		// A non-Hash extra argument to instruct! is ignored (skip arm in builderAttrs).
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.instruct!(:xml, 99)
			print xml.target!`, `<?xml version="1.0" encoding="UTF-8"?>`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBuilderDeclare covers declare! for a DOCTYPE with Symbol and String args
// and an internal-subset block.
func TestBuilderDeclare(t *testing.T) {
	cases := []struct{ src, want string }{
		// A Symbol arg prints bare; a String arg double-quoted.
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.declare!(:DOCTYPE, :html)
			print xml.target!`, `<!DOCTYPE html>`},
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.declare!(:DOCTYPE, :html, :PUBLIC, "-//W3C//DTD")
			print xml.target!`, `<!DOCTYPE html PUBLIC "-//W3C//DTD">`},
		// An Integer arg prints double-quoted via to_s (the default arm).
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.declare!(:X, 42)
			print xml.target!`, `<!X "42">`},
		// A block is the internal subset, emitted between " [" and "]".
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.declare!(:DOCTYPE, :html) { xml.declare!(:ENTITY, :nbsp) }
			print xml.target!`, "<!DOCTYPE html [<!ENTITY nbsp>]>"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBuilderIndent covers the indent: and margin: constructor options.
func TestBuilderIndent(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "builder"
			xml = Builder::XmlMarkup.new(indent: 2)
			xml.ul { xml.li "a" }
			print xml.target!`, "<ul>\n  <li>a</li>\n</ul>\n"},
		{`require "builder"
			xml = Builder::XmlMarkup.new(indent: 2, margin: 1)
			xml.a
			print xml.target!`, "  <a/>\n"},
		// A non-Hash constructor argument is ignored (the skip arm in builderOptions).
		{`require "builder"
			xml = Builder::XmlMarkup.new(42)
			xml.a
			print xml.target!`, `<a/>`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBuilderTargetAndRespond covers target!/to_s and respond_to_missing?.
func TestBuilderTargetAndRespond(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "builder"
			xml = Builder::XmlMarkup.new
			xml.a("x")
			print xml.to_s`, `<a>x</a>`},
		{`require "builder"; p Builder::XmlMarkup.new.respond_to?(:anything)`, "true\n"},
		{`require "builder"; p Builder::XmlMarkup.new.inspect`, "\"#<Builder::XmlMarkup>\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestBuilderErrors covers the argument guards on tag! and declare!.
func TestBuilderErrors(t *testing.T) {
	cases := []string{
		`Builder::XmlMarkup.new.tag!`,
		`Builder::XmlMarkup.new.declare!`,
	}
	for _, body := range cases {
		src := `require "builder"
begin
  ` + body + `
rescue ArgumentError
  print "err"
end`
		if got := eval(t, src); got != "err" {
			t.Errorf("body=%q got=%q want=err", body, got)
		}
	}
}
