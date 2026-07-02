// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"testing"

	nokogiri "github.com/go-ruby-nokogiri/nokogiri"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestNokogiriStr covers nokogiriStr's String / Symbol / default (to_s) arms.
func TestNokogiriStr(t *testing.T) {
	if s := nokogiriStr(object.NewString("x")); s != "x" {
		t.Errorf("string -> %q", s)
	}
	if s := nokogiriStr(object.Symbol("sym")); s != "sym" {
		t.Errorf("symbol -> %q", s)
	}
	if s := nokogiriStr(object.Integer(7)); s != "7" {
		t.Errorf("default to_s -> %q", s)
	}
}

// TestNokogiriNodeOrNil covers the nil (Go) -> Ruby nil arm and the wrap arm.
func TestNokogiriNodeOrNil(t *testing.T) {
	if v := nokogiriNodeOrNil(nil); v != object.NilV {
		t.Errorf("nil node -> %v", v)
	}
	doc, _ := nokogiri.XML("<a/>")
	if v := nokogiriNodeOrNil(doc.Root()); v == object.NilV {
		t.Error("real node should wrap, not nil")
	}
}

// TestNokogiriRaiseError covers raiseNokogiriError raising Nokogiri::SyntaxError.
func TestNokogiriRaiseError(t *testing.T) {
	defer func() {
		r := recover()
		re, ok := r.(RubyError)
		if !ok || re.Class != "Nokogiri::SyntaxError" {
			t.Errorf("want Nokogiri::SyntaxError, got %v", r)
		}
	}()
	raiseNokogiriError(errors.New("boom"))
}

// TestNokogiriDisplayMethods covers the ToS / Inspect / Truthy display methods on
// the Document / Node / NodeSet wrappers.
func TestNokogiriDisplayMethods(t *testing.T) {
	doc, _ := nokogiri.XML("<a>x</a>")
	d := &NokogiriDocument{doc: doc}
	if d.ToS() == "" || d.Inspect() != "#<Nokogiri::XML::Document>" || !d.Truthy() {
		t.Error("Document display methods")
	}
	n := &NokogiriNode{n: doc.Root()}
	if n.ToS() == "" || n.Inspect() != "#<Nokogiri::XML::Node>" || !n.Truthy() {
		t.Error("Node display methods")
	}
	set, _ := doc.CSS("a")
	s := &NokogiriNodeSet{set: set}
	if s.ToS() != "#<Nokogiri::XML::NodeSet>" || s.Inspect() != "#<Nokogiri::XML::NodeSet>" || !s.Truthy() {
		t.Error("NodeSet display methods")
	}
}

// TestNokogiriParseXMLError covers nokogiriParseXML's parse-error arm (malformed
// XML) directly, complementing the Ruby-level rescue test.
func TestNokogiriParseXMLError(t *testing.T) {
	defer func() {
		if _, ok := recover().(RubyError); !ok {
			t.Error("malformed XML should raise a RubyError")
		}
	}()
	nokogiriParseXML("<root><unclosed>")
}
