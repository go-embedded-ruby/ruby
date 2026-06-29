// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestREXMLBinding exercises the REXML module backed by the go-ruby-rexml
// library (internal/vm/rexml.go): require, Document parse/build/serialise, the
// compact and Pretty formatters, Element navigation and mutation, Attributes,
// the Text / Comment / CData / Instruction / DocType node classes, the XPath
// subset (first / each / match, predicates, @attr) and REXML::ParseException.
// Every expectation is pinned against MRI 4.0.5 (ruby -rrexml/document -e).
func TestREXMLBinding(t *testing.T) {
	const req = `require "rexml/document"; `
	tests := []struct{ name, src, want string }{
		// --- require -------------------------------------------------------
		{"require_document_true", `p require "rexml/document"`, "true\n"},
		{"require_document_false", `require "rexml/document"; p require "rexml/document"`, "false\n"},
		{"require_rexml_true", `p require "rexml"`, "true\n"},

		// --- parse -> to_s round trip (single-quoted attrs, sorted) ---------
		{"roundtrip_basic", req + `p REXML::Document.new("<root a=\"1\" b=\"2\"><child>hi</child></root>").to_s`,
			"\"<root a='1' b='2'><child>hi</child></root>\"\n"},
		{"roundtrip_sorts_attrs", req + `p REXML::Document.new('<r b="2" a="1"/>').to_s`,
			"\"<r a='1' b='2'/>\"\n"},
		{"roundtrip_comment_cdata", req + `p REXML::Document.new("<r><!-- c --><![CDATA[x<y]]></r>").to_s`,
			"\"<r><!-- c --><![CDATA[x<y]]></r>\"\n"},
		{"empty_self_close", req + `p REXML::Document.new("<r></r>").to_s`, "\"<r/>\"\n"},

		// --- entity handling -----------------------------------------------
		{"entity_text_roundtrip", req + `p REXML::Document.new("<r>a&amp;b</r>").to_s`, "\"<r>a&amp;b</r>\"\n"},
		{"entity_text_decoded", req + `p REXML::Document.new("<r>a&amp;b</r>").root.text`, "\"a&b\"\n"},
		{"entity_attr_roundtrip", req + `p REXML::Document.new(%q{<r x="&amp;&lt;"/>}).to_s`, "\"<r x='&amp;&lt;'/>\"\n"},
		{"entity_attr_decoded", req + `p REXML::Document.new(%q{<r x="&amp;&lt;"/>}).root.attributes["x"]`, "\"&<\"\n"},

		// --- root / name ---------------------------------------------------
		{"root_name", req + `p REXML::Document.new("<root><c/></root>").root.name`, "\"root\"\n"},
		{"root_nil", req + `p REXML::Document.new.root`, "nil\n"},

		// --- attributes ----------------------------------------------------
		{"attr_read", req + `p REXML::Document.new(%q{<r a="1" b="2"/>}).root.attributes["a"]`, "\"1\"\n"},
		{"attr_missing", req + `p REXML::Document.new("<r/>").root.attributes["x"]`, "nil\n"},
		{"attr_size", req + `p REXML::Document.new(%q{<r a="1" b="2"/>}).root.attributes.size`, "2\n"},
		{"attr_length", req + `p REXML::Document.new(%q{<r a="1"/>}).root.attributes.length`, "1\n"},
		{"attr_each_source_order", req + `s=""; REXML::Document.new(%q{<r b="2" a="1"/>}).root.attributes.each{|k,v| s<<"#{k}=#{v} "}; p s`,
			"\"b=2 a=1 \"\n"},
		{"attr_each_attribute", req + `s=[]; REXML::Document.new(%q{<r b="2" a="1"/>}).root.attributes.each_attribute{|v| s<<v}; p s`,
			"[\"2\", \"1\"]\n"},
		{"attr_set_serialise", req + `d=REXML::Document.new("<r/>"); d.root.attributes["k"]="v"; p d.to_s`,
			"\"<r k='v'/>\"\n"},
		{"attr_delete", req + `d=REXML::Document.new(%q{<r a="1" b="2"/>}); d.root.attributes.delete("a"); p d.to_s`,
			"\"<r b='2'/>\"\n"},
		{"element_attr_bracket", req + `p REXML::Document.new(%q{<r a="1"/>}).root["a"]`, "\"1\"\n"},
		{"element_attr_bracket_sym", req + `p REXML::Document.new(%q{<r a="1"/>}).root[:a]`, "\"1\"\n"},
		{"element_index_child", req + `p REXML::Document.new("<r><a/><b/></r>").root[1].name`, "\"b\"\n"},

		// --- element navigation --------------------------------------------
		{"elements_path_text", req + `p REXML::Document.new("<r><c>hi</c></r>").root.elements["c"].text`, "\"hi\"\n"},
		{"elements_path_miss", req + `p REXML::Document.new("<r/>").root.elements["c"]`, "nil\n"},
		{"elements_index_1based", req + `p REXML::Document.new("<r><a/><b/></r>").root.elements[2].name`, "\"b\"\n"},
		{"elements_index_oob", req + `p REXML::Document.new("<r><a/></r>").root.elements[5]`, "nil\n"},
		{"elements_size", req + `p REXML::Document.new("<r><a/><b/><c/></r>").root.elements.size`, "3\n"},
		{"elements_each", req + `s=[]; REXML::Document.new("<r><a/><b/></r>").root.elements.each{|e| s<<e.name}; p s`,
			"[\"a\", \"b\"]\n"},
		{"elements_each_path", req + `s=[]; REXML::Document.new("<r><a/><a/><b/></r>").root.elements.each("a"){|e| s<<e.name}; p s`,
			"[\"a\", \"a\"]\n"},
		{"each_element", req + `s=[]; REXML::Document.new("<r><a/><b/></r>").root.each_element{|e| s<<e.name}; p s`,
			"[\"a\", \"b\"]\n"},
		{"each_element_path", req + `s=0; REXML::Document.new("<r><a/><a/><b/></r>").root.each_element("a"){|e| s+=1}; p s`, "2\n"},
		{"expanded_name", req + `p REXML::Document.new("<ns:r xmlns:ns='u'/>").root.expanded_name`, "\"ns:r\"\n"},
		{"text_nil_when_none", req + `p REXML::Document.new("<r><c/></r>").root.text`, "nil\n"},

		// --- building ------------------------------------------------------
		{"build_doc", req + `d=REXML::Document.new; r=d.add_element("html"); r.add_attribute("lang","en"); b=r.add_element("body"); b.add_text("hi & bye"); p d.to_s`,
			"\"<html lang='en'><body>hi &amp; bye</body></html>\"\n"},
		{"build_element_new", req + `e=REXML::Element.new("x"); e.add_text("y"); p e.to_s`, "\"<x>y</x>\"\n"},
		{"build_element_default_name", req + `p REXML::Element.new.to_s`, "\"<UNDEFINED/>\"\n"},
		{"elements_add_element", req + `d=REXML::Document.new("<r/>"); d.root.elements.add_element("c"); p d.to_s`,
			"\"<r><c/></r>\"\n"},

		// --- Pretty formatter (Formatters::Pretty over StringIO) -----------
		{"pretty_stringio", req + `require "stringio"; io=StringIO.new; REXML::Formatters::Pretty.new(2).write(REXML::Document.new("<r><c>hi</c></r>"), io); p io.string`,
			"\"<r>\\n  <c>\\n    hi\\n  </c>\\n</r>\"\n"},
		{"pretty_default_indent", req + `require "stringio"; io=StringIO.new; REXML::Formatters::Pretty.new.write(REXML::Document.new("<a><b/></a>"), io); p io.string`,
			"\"<a>\\n  <b/>\\n</a>\"\n"},
		// Pretty#write with no IO and Document#write with no IO are binding
		// conveniences (not MRI-shaped — MRI writes to $stdout / raises): they
		// return the rendered String. Asserted as the binding's own contract.
		{"pretty_write_returns_string", req + `p REXML::Formatters::Pretty.new(2).write(REXML::Document.new("<a><b/></a>")).class`, "String\n"},
		{"doc_write_indent", req + `require "stringio"; io=StringIO.new; REXML::Document.new("<a><b/></a>").write(io, 2); p io.string`,
			"\"<a>\\n  <b/>\\n</a>\"\n"},
		{"doc_write_returns_string", req + `p REXML::Document.new("<a/>").write`, "\"<a/>\"\n"},
		{"doc_write_pretty_string", req + `p REXML::Document.new("<a><b/></a>").write(2)`, "\"<a>\\n  <b/>\\n</a>\"\n"},

		// --- node classes --------------------------------------------------
		{"text_to_s_escapes", req + `p REXML::Text.new("a&b").to_s`, "\"a&amp;b\"\n"},
		{"text_value_decoded", req + `p REXML::Text.new("a&b").value`, "\"a&b\"\n"},
		{"comment_to_s", req + `p REXML::Comment.new("hi").to_s`, "\"hi\"\n"},
		{"cdata_to_s", req + `p REXML::CData.new("x<y").to_s`, "\"x<y\"\n"},
		{"instruction_to_s", req + `p REXML::Instruction.new("xml-stylesheet", %q{type="text/xsl"}).to_s`,
			"\"<?xml-stylesheet type=\\\"text/xsl\\\"?>\"\n"},
		{"instruction_no_content", req + `p REXML::Instruction.new("php").to_s`, "\"<?php?>\"\n"},
		{"instruction_target", req + `p REXML::Instruction.new("php","x").target`, "\"php\"\n"},
		{"doctype_to_s", req + `p REXML::Document.new(%q{<!DOCTYPE html><r/>}).root.name`, "\"r\"\n"},

		// --- class identity ------------------------------------------------
		{"document_class", req + `p REXML::Document.new("<r/>").class`, "REXML::Document\n"},
		{"element_class", req + `p REXML::Document.new("<r/>").root.class`, "REXML::Element\n"},
		{"attributes_class", req + `p REXML::Document.new("<r/>").root.attributes.class`, "REXML::Attributes\n"},
		{"elements_class", req + `p REXML::Document.new("<r/>").root.elements.class`, "REXML::Elements\n"},

		// --- XPath subset --------------------------------------------------
		{"xpath_first", req + `p REXML::XPath.first(REXML::Document.new("<r><c/></r>").root, "c").name`, "\"c\"\n"},
		{"xpath_first_nil", req + `p REXML::XPath.first(REXML::Document.new("<r/>").root, "c")`, "nil\n"},
		{"xpath_match_count", req + `p REXML::XPath.match(REXML::Document.new(%q{<r><a id="1"/><a id="2"/><b/></r>}).root, "a").length`, "2\n"},
		{"xpath_each", req + `s=[]; REXML::XPath.each(REXML::Document.new(%q{<r><a id="1"/><a id="2"/></r>}).root, "a"){|e| s<<e.attributes["id"]}; p s`,
			"[\"1\", \"2\"]\n"},
		{"xpath_pred_attr_eq", req + `p REXML::XPath.first(REXML::Document.new(%q{<r><a id="1"/><a id="2"/></r>}).root, %q{a[@id="2"]}).attributes["id"]`,
			"\"2\"\n"},
		{"xpath_pred_position", req + `p REXML::XPath.first(REXML::Document.new("<r><a/><a/></r>").root, "a[2]").class`, "REXML::Element\n"},
		{"xpath_attr_node_value", req + `p REXML::XPath.first(REXML::Document.new(%q{<r><a id="1"/></r>}).root, "a/@id")`, "\"1\"\n"},
		{"xpath_attr_node_decoded", req + `p REXML::XPath.first(REXML::Document.new(%q{<r><a id="&amp;"/></r>}).root, "a/@id")`, "\"&\"\n"},

		// --- ParseException ------------------------------------------------
		{"parse_error_class", req + `begin; REXML::Document.new("<unclosed>"); rescue REXML::ParseException => e; p e.class; end`,
			"REXML::ParseException\n"},
		{"parse_error_is_std", req + `p REXML::ParseException.ancestors.include?(StandardError)`, "true\n"},
		{"parse_error_rescued_std", req + `begin; REXML::Document.new("<x"); rescue StandardError; p :ok; end`, ":ok\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := eval(t, tt.src)
			if out != tt.want {
				t.Fatalf("src=%q\n got %q\nwant %q", tt.src, out, tt.want)
			}
		})
	}
}

// TestREXMLNodeRendering covers the Go-level ToS / Inspect / Truthy / node
// renderers of the REXML::* wrappers: puts goes through ToS, p through Inspect,
// truthiness through Truthy, and XPath context extraction through node(). These
// are the paths the VM takes for a non-RObject builtin value (io.displayStr /
// inspectStr call the wrapper's Go methods directly).
func TestREXMLNodeRendering(t *testing.T) {
	const req = `require "rexml/document"; `
	tests := []struct{ name, src, want string }{
		// ToS (puts) -------------------------------------------------------
		{"puts_document", req + `puts REXML::Document.new("<r a='1'/>")`, "<r a='1'/>\n"},
		{"puts_element", req + `puts REXML::Document.new("<r><c>hi</c></r>").root`, "<r><c>hi</c></r>\n"},
		{"puts_text", req + `puts REXML::Text.new("a&b")`, "a&amp;b\n"},
		{"puts_comment", req + `puts REXML::Comment.new("c")`, "c\n"},
		{"puts_cdata", req + `puts REXML::CData.new("x<y")`, "x<y\n"},
		{"puts_instruction", req + `puts REXML::Instruction.new("php","x")`, "<?php x?>\n"},
		{"puts_attributes", req + `puts REXML::Document.new("<r/>").root.attributes`, "attributes\n"},
		{"puts_elements", req + `puts REXML::Document.new("<r/>").root.elements`, "elements\n"},

		// Inspect (p) ------------------------------------------------------
		{"insp_document_root", req + `p REXML::Document.new("<r/>")`, "<UNDEFINED> ... </>\n"},
		{"insp_document_empty", req + `p REXML::Document.new`, "<UNDEFINED/>\n"},
		{"insp_element_empty", req + `p REXML::Document.new("<r a='1'/>").root`, "<r a='1'/>\n"},
		{"insp_element_children", req + `p REXML::Document.new("<r a='1'><c/></r>").root`, "<r a='1'> ... </>\n"},
		{"insp_text", req + `p REXML::Text.new("a&b")`, "#<REXML::Text: a&amp;b>\n"},
		{"insp_comment", req + `p REXML::Comment.new("c")`, "#<REXML::Comment: c>\n"},
		{"insp_cdata", req + `p REXML::CData.new("x")`, "#<REXML::CData: x>\n"},
		{"insp_instruction", req + `p REXML::Instruction.new("t")`, "<?t?>\n"},
		{"insp_attributes", req + `p REXML::Document.new("<r/>").root.attributes`, "#<REXML::Attributes>\n"},
		{"insp_elements", req + `p REXML::Document.new("<r/>").root.elements`, "#<REXML::Elements>\n"},

		// Truthy (a node is always truthy) ---------------------------------
		{"truthy_document", req + `puts(REXML::Document.new("<r/>") ? "t" : "f")`, "t\n"},
		{"truthy_element", req + `puts(REXML::Document.new("<r/>").root ? "t" : "f")`, "t\n"},
		{"truthy_text", req + `puts(REXML::Text.new("a") ? "t" : "f")`, "t\n"},
		{"truthy_comment", req + `puts(REXML::Comment.new("c") ? "t" : "f")`, "t\n"},
		{"truthy_cdata", req + `puts(REXML::CData.new("c") ? "t" : "f")`, "t\n"},
		{"truthy_instruction", req + `puts(REXML::Instruction.new("t") ? "t" : "f")`, "t\n"},
		{"truthy_attributes", req + `puts(REXML::Document.new("<r/>").root.attributes ? "t" : "f")`, "t\n"},
		{"truthy_elements", req + `puts(REXML::Document.new("<r/>").root.elements ? "t" : "f")`, "t\n"},

		// node() via XPath context on a Document and an Element ------------
		{"xpath_ctx_document", req + `p REXML::XPath.first(REXML::Document.new("<r><c/></r>"), "r/c").name`, "\"c\"\n"},
		{"xpath_ctx_element", req + `p REXML::XPath.first(REXML::Document.new("<r><c/></r>").root, "c").name`, "\"c\"\n"},
		// node() via XPath context on the leaf node types (the library yields
		// nil for a leaf context, which is what these assert) -------------
		{"xpath_ctx_text", req + `p REXML::XPath.first(REXML::Text.new("z"), ".")`, "nil\n"},
		{"xpath_ctx_comment", req + `p REXML::XPath.first(REXML::Comment.new("c"), ".")`, "nil\n"},
		{"xpath_ctx_cdata", req + `p REXML::XPath.first(REXML::CData.new("d"), ".")`, "nil\n"},
		{"xpath_ctx_instruction", req + `p REXML::XPath.first(REXML::Instruction.new("t"), ".")`, "nil\n"},

		// text() XPath yields a REXML::Text (wrapNode Text branch) ---------
		{"xpath_text_node_class", req + `p REXML::XPath.first(REXML::Document.new("<r>hi<c/></r>").root, "text()").class`, "REXML::Text\n"},
		{"xpath_text_node_value", req + `p REXML::XPath.first(REXML::Document.new("<r>hi<c/></r>").root, "text()").to_s`, "\"hi\"\n"},

		// doctype accessor (wrapNode DocType branch + DocType renderers) ---
		{"doctype_class", req + `p REXML::Document.new("<!DOCTYPE html><r/>").doctype.class`, "REXML::DocType\n"},
		{"doctype_puts", req + `puts REXML::Document.new("<!DOCTYPE html><r/>").doctype`, "<!DOCTYPE html>\n"},
		{"doctype_to_s_method", req + `p REXML::Document.new("<!DOCTYPE html><r/>").doctype.to_s`, "\"<!DOCTYPE html>\"\n"},
		{"doctype_truthy", req + `puts(REXML::Document.new("<!DOCTYPE html><r/>").doctype ? "t" : "f")`, "t\n"},
		{"doctype_none", req + `p REXML::Document.new("<r/>").doctype`, "nil\n"},
		// DocType#inspect is the binding's Go renderer (the wrapper is not an
		// RObject), so it shows the declaration rather than MRI's object-id form.
		{"doctype_inspect", req + `p REXML::Document.new("<!DOCTYPE html><r/>").doctype`, "<!DOCTYPE html>\n"},

		// XPath @attr fallback through wrapNode's nil result ---------------
		{"xpath_attr_fallback", req + `p REXML::XPath.first(REXML::Document.new(%q{<r><a id="9"/></r>}).root, "a/@id")`, "\"9\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := eval(t, tt.src)
			if out != tt.want {
				t.Fatalf("src=%q\n got %q\nwant %q", tt.src, out, tt.want)
			}
		})
	}
}

// TestREXMLErrorBranches covers the binding's defensive error and edge branches:
// the TypeError raised by a non-String/non-node Document.new source, the XPath
// context TypeError for a non-node, the doctype to_s renderer, and the wrapNode
// DocType / nil fallbacks reachable through parsing.
func TestREXMLErrorBranches(t *testing.T) {
	const req = `require "rexml/document"; `
	tests := []struct{ name, src, want string }{
		{"doc_new_type_error", req + `begin; REXML::Document.new(42); rescue TypeError => e; p :te; end`, ":te\n"},
		{"doc_new_from_document", req + `d=REXML::Document.new("<r><c/></r>"); p REXML::Document.new(d).to_s`, "\"<r><c/></r>\"\n"},
		{"doc_new_from_element", req + `e=REXML::Element.new("x"); p REXML::Document.new(e).to_s`, "\"<x/>\"\n"},
		{"doc_new_nil", req + `p REXML::Document.new(nil).root`, "nil\n"},
		{"xpath_ctx_type_error", req + `begin; REXML::XPath.first("notanode", "a"); rescue TypeError => e; p :te; end`, ":te\n"},
		{"doctype_to_s", req + `d=REXML::Document.new("<!DOCTYPE html SYSTEM 'x.dtd'><r/>"); p d.to_s.start_with?("<!DOCTYPE")`, "true\n"},

		// Element#[Integer] (0-based child) and a missing attribute name ----
		{"element_index_0", req + `p REXML::Document.new("<r><a/><b/></r>").root[0].name`, "\"a\"\n"},
		{"element_index_oob", req + `p REXML::Document.new("<r><a/></r>").root[9]`, "nil\n"},
		{"element_attr_miss", req + `p REXML::Document.new("<r/>").root["nope"]`, "nil\n"},

		// rexmlNameArg default (a non-name key reads as nil) ----------------
		{"element_float_key", req + `p REXML::Document.new(%q{<r a="1"/>}).root[1.0]`, "nil\n"},
		{"attributes_float_key", req + `p REXML::Document.new(%q{<r a="1"/>}).root.attributes[1.0]`, "nil\n"},

		// Elements#add (alias of add_element) ------------------------------
		{"elements_add", req + `d=REXML::Document.new("<r/>"); d.root.elements.add("c"); p d.to_s`, "\"<r><c/></r>\"\n"},

		// Instruction#content ----------------------------------------------
		{"instruction_content", req + `p REXML::Instruction.new("t","c").content`, "\"c\"\n"},
		{"instruction_content_empty", req + `p REXML::Instruction.new("t").content`, "\"\"\n"},

		// Pretty.new with a non-Integer indent defaults to 2 ---------------
		{"pretty_new_nonint", req + `require "stringio"; io=StringIO.new; REXML::Formatters::Pretty.new("x").write(REXML::Document.new("<a><b/></a>"), io); p io.string`,
			"\"<a>\\n  <b/>\\n</a>\"\n"},
		// Pretty#write on a non-Document argument raises ArgumentError ------
		{"pretty_write_bad_arg", req + `begin; REXML::Formatters::Pretty.new(2).write(42); rescue ArgumentError => e; p :ae; end`, ":ae\n"},

		// Document#write to a String output (appends via <<) ----------------
		{"write_to_string", req + `s=""; REXML::Document.new("<a><b/></a>").write(s, 2); p s`, "\"<a>\\n  <b/>\\n</a>\"\n"},
		// write(io) with no indent, and write(io, nil), both compact --------
		{"write_io_compact", req + `require "stringio"; io=StringIO.new; REXML::Document.new("<a><b/></a>").write(io); p io.string`, "\"<a><b/></a>\"\n"},
		{"write_io_nil_indent", req + `require "stringio"; io=StringIO.new; REXML::Document.new("<a><b/></a>").write(io, nil); p io.string`, "\"<a><b/></a>\"\n"},

		// A DocType is not a usable XPath context in the supported subset:
		// passing one raises TypeError (it does not implement node()).
		{"xpath_ctx_doctype_type_error", req + `begin; REXML::XPath.first(REXML::Document.new("<!DOCTYPE html><r/>").doctype, "."); rescue TypeError; p :te; end`, ":te\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := eval(t, tt.src)
			if out != tt.want {
				t.Fatalf("src=%q\n got %q\nwant %q", tt.src, out, tt.want)
			}
		})
	}
}
