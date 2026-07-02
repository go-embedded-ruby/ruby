// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"testing"
)

// TestNokogiriConstants covers the Nokogiri loadable module, its class tree and
// the SyntaxError class (require "nokogiri").
func TestNokogiriConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "nokogiri"; p Nokogiri.is_a?(Module)`, "true\n"},
		{`p require "nokogiri"`, "true\n"},
		{`require "nokogiri"; p require "nokogiri"`, "false\n"},
		{`require "nokogiri"; p Nokogiri::SyntaxError < StandardError`, "true\n"},
		// Nokogiri::XML::SyntaxError is the same class re-exposed.
		{`require "nokogiri"; p Nokogiri::XML::SyntaxError.equal?(Nokogiri::SyntaxError)`, "true\n"},
		// Class identity of parsed values.
		{`require "nokogiri"; p Nokogiri::HTML("<p>hi</p>").class`, "Nokogiri::XML::Document\n"},
		{`require "nokogiri"; doc = Nokogiri::HTML("<p>hi</p>"); p doc.at_css("p").class`, "Nokogiri::XML::Node\n"},
		{`require "nokogiri"; doc = Nokogiri::HTML("<p>hi</p>"); p doc.css("p").class`, "Nokogiri::XML::NodeSet\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestNokogiriQuery covers CSS / XPath queries, text extraction, attribute
// access, names and the NodeSet collection surface over a real parsed document.
func TestNokogiriQuery(t *testing.T) {
	cases := []struct{ src, want string }{
		// #css returns a NodeSet; #at_css the first match; #text the character data.
		{`require "nokogiri"
doc = Nokogiri::HTML("<ul><li>a</li><li>b</li></ul>")
p doc.css("li").length
p doc.at_css("li").text`, "2\n\"a\"\n"},
		// A CSS class selector and attribute access via #[].
		{`require "nokogiri"
doc = Nokogiri::HTML('<a class="x" href="/go">go</a>')
node = doc.at_css("a.x")
p node["href"]
p node["missing"]
p node.name`, "\"/go\"\n" + "nil\n" + "\"a\"\n"},
		// #xpath over the document.
		{`require "nokogiri"
doc = Nokogiri::XML("<root><item>1</item><item>2</item></root>")
p doc.xpath("//item").length
p doc.at_xpath("//item").text`, "2\n\"1\"\n"},
		// NodeSet #each / #map / #first / #last / #[] / #to_a / #text.
		{`require "nokogiri"
doc = Nokogiri::HTML("<ul><li>a</li><li>b</li><li>c</li></ul>")
set = doc.css("li")
p set.map { |n| n.text }
p set.first.text
p set.last.text
p set[1].text
p set[-1].text
p set.to_a.length
p set.text`, "[\"a\", \"b\", \"c\"]\n\"a\"\n\"c\"\n\"b\"\n\"c\"\n3\n\"abc\"\n"},
		// #at_css returning nil when nothing matches.
		{`require "nokogiri"
doc = Nokogiri::HTML("<p>hi</p>")
p doc.at_css("div")`, "nil\n"},
		// Tree navigation: children / parent.
		{`require "nokogiri"
doc = Nokogiri::XML("<root><a/><b/></root>")
root = doc.at_xpath("//root")
p root.children.length
p root.at_xpath("//a").parent.name`, "2\n\"root\"\n"},
		// #to_html round-trips a fragment.
		{`require "nokogiri"
doc = Nokogiri::XML("<a><b>x</b></a>")
p doc.at_xpath("//a").to_xml`, "\"<a><b>x</b></a>\"\n"},
		// Node-level #css (rooted at the node) and #[]= mutation.
		{`require "nokogiri"
doc = Nokogiri::HTML("<div><span>one</span><span>two</span></div>")
div = doc.at_css("div")
p div.css("span").length
node = doc.at_css("span")
node["data-x"] = "1"
p node["data-x"]`, "2\n\"1\"\n"},
		// #attributes returns a Hash keyed by attribute name.
		{`require "nokogiri"
doc = Nokogiri::HTML('<a id="i" class="c">t</a>')
p doc.at_css("a").attributes["id"]`, "\"i\"\n"},
		// Predicates.
		{`require "nokogiri"
doc = Nokogiri::HTML("<p>hi</p>")
p doc.at_css("p").element?`, "true\n"},
		// #empty? on an empty NodeSet.
		{`require "nokogiri"
doc = Nokogiri::HTML("<p>hi</p>")
p doc.css("div").empty?`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\ngot =%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestNokogiriSurface exercises the remaining node / nodeset / document methods
// so every bound method is covered: the serialisation and content aliases, the
// name aliases, the predicates, the full navigation set, the document-level
// accessors and the NodeSet size aliases.
func TestNokogiriSurface(t *testing.T) {
	cases := []struct{ src, want string }{
		// content / inner_text aliases and node_name.
		{`require "nokogiri"
doc = Nokogiri::HTML("<p>hi</p>")
n = doc.at_css("p")
p n.content
p n.inner_text
p n.node_name`, "\"hi\"\n\"hi\"\n\"p\"\n"},
		// inner_html / inner_xml / to_html / to_s.
		{`require "nokogiri"
doc = Nokogiri::XML("<a><b>x</b></a>")
n = doc.at_xpath("//a")
p n.inner_xml
p n.to_html.include?("<a>")
p n.to_s.include?("<a>")`, "\"<b>x</b>\"\n" + "true\n" + "true\n"},
		// node-level xpath / at_xpath / css / at_css.
		{`require "nokogiri"
doc = Nokogiri::XML("<r><a>1</a><a>2</a></r>")
r = doc.at_xpath("//r")
p r.xpath(".//a").length
p r.at_xpath(".//a").text
p r.css("a").length
p r.at_css("a").text`, "2\n\"1\"\n2\n\"1\"\n"},
		// #attribute accessor.
		{`require "nokogiri"
doc = Nokogiri::HTML('<a href="/x">t</a>')
p doc.at_css("a").attribute("href")
p doc.at_css("a").attribute("nope")`, "\"/x\"\nnil\n"},
		// predicates: text? / comment? / cdata?.
		{`require "nokogiri"
doc = Nokogiri::XML("<r>hi<!--c--></r>")
r = doc.at_xpath("//r")
kids = r.children
p kids.length >= 1
p doc.at_css("r").comment?`, "true\nfalse\n"},
		// navigation: next / previous / next_element / previous_element.
		{`require "nokogiri"
doc = Nokogiri::XML("<r><a/><b/><c/></r>")
b = doc.at_xpath("//b")
p b.next_element.name
p b.previous_element.name`, "\"c\"\n\"a\"\n"},
		// nil at the ends of navigation.
		{`require "nokogiri"
doc = Nokogiri::XML("<r><a/></r>")
a = doc.at_xpath("//a")
p a.next_element
p a.previous_element`, "nil\nnil\n"},
		// document #root / #text / #content / #to_xml / #to_html.
		{`require "nokogiri"
doc = Nokogiri::XML("<root>hi</root>")
p doc.root.name
p doc.text
p doc.content
p doc.to_xml.include?("root")
p doc.to_html.include?("root")
p doc.to_s.include?("root")`, "\"root\"\n\"hi\"\n\"hi\"\ntrue\ntrue\ntrue\n"},
		// NodeSet size / count aliases and #each without a block returns self.
		{`require "nokogiri"
doc = Nokogiri::HTML("<ul><li/><li/></ul>")
set = doc.css("li")
p set.size
p set.count
p set.each.equal?(set)`, "2\n2\ntrue\n"},
		// NodeSet #map without a block returns an empty Array; #[] out of range is nil.
		{`require "nokogiri"
doc = Nokogiri::HTML("<ul><li/></ul>")
set = doc.css("li")
p set.map
p set[5]
p set[-9]`, "[]\nnil\nnil\n"},
		// Display: to_s of a document / node / nodeset (via string interpolation).
		{`require "nokogiri"
doc = Nokogiri::XML("<a>x</a>")
p doc.to_s.include?("a")`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\ngot =%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestNokogiriErrors covers the SyntaxError raised for malformed XML and the
// argument-arity guards.
func TestNokogiriErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		// Malformed XML raises Nokogiri::SyntaxError (strict XML parse).
		{`require "nokogiri"
begin
  Nokogiri::XML("<root><unclosed>")
rescue Nokogiri::SyntaxError
  p :syntax
end`, ":syntax\n"},
		// A malformed CSS selector raises through the query path.
		{`require "nokogiri"
doc = Nokogiri::XML("<r/>")
begin
  doc.css("[[[bad")
rescue Nokogiri::SyntaxError
  p :badcss
end`, ":badcss\n"},
		// HTML / XML with no argument raise ArgumentError.
		{`require "nokogiri"; begin; Nokogiri::HTML(); rescue ArgumentError; p :h; end`, ":h\n"},
		{`require "nokogiri"; begin; Nokogiri::XML(); rescue ArgumentError; p :x; end`, ":x\n"},
		// Bad selectors raise through every query entry point: document #css /
		// #at_css / #xpath / #at_xpath and the node-level equivalents.
		{`require "nokogiri"
doc = Nokogiri::XML("<r><a/></r>")
node = doc.at_xpath("//a")
[
  -> { doc.css("[[[") }, -> { doc.at_css("[[[") },
  -> { doc.xpath("///") }, -> { doc.at_xpath("///") },
  -> { node.css("[[[") }, -> { node.at_css("[[[") },
  -> { node.xpath("///") }, -> { node.at_xpath("///") },
].each do |q|
  begin; q.call; rescue Nokogiri::SyntaxError; print "e"; end
end
puts`, "eeeeeeee\n"},
		// #css / #at_css / #xpath / #at_xpath and a NodeSet #[] with no argument
		// raise ArgumentError.
		{`require "nokogiri"
doc = Nokogiri::XML("<r/>")
node = doc.at_xpath("//r")
set = doc.css("r")
[
  -> { doc.css }, -> { doc.at_css }, -> { doc.xpath }, -> { doc.at_xpath },
  -> { node.css }, -> { node.xpath },
  -> { node[] rescue node.send(:[]) }, -> { node.send(:[]=, "a") },
  -> { node.attribute }, -> { set.send(:[]) },
].each do |q|
  begin; q.call; rescue ArgumentError; print "a"; end
end
puts`, "aaaaaaaaaa\n"},
		// #each with a block iterates each node.
		{`require "nokogiri"
doc = Nokogiri::HTML("<ul><li>a</li><li>b</li></ul>")
seen = []
doc.css("li").each { |n| seen << n.text }
p seen`, "[\"a\", \"b\"]\n"},
		// A Symbol selector is coerced via nokogiriStr (rare but accepted).
		{`require "nokogiri"
doc = Nokogiri::HTML("<p>hi</p>")
p doc.css(:p).length`, "1\n"},
		// node #inner_html.
		{`require "nokogiri"
doc = Nokogiri::HTML("<div><b>x</b></div>")
p doc.at_css("div").inner_html`, "\"<b>x</b>\"\n"},
		// #text? on a text child, #cdata? on a CDATA child.
		{`require "nokogiri"
doc = Nokogiri::XML("<r>hello<![CDATA[data]]></r>")
kids = doc.at_xpath("//r").children
p kids[0].text?
p kids[1].cdata?`, "true\ntrue\n"},
		// #next / #previous sibling navigation (including the text nodes).
		{`require "nokogiri"
doc = Nokogiri::XML("<r><a/><b/></r>")
a = doc.at_xpath("//a")
p a.next.name
p a.next.previous.name`, "\"b\"\n\"a\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\ngot =%q\nwant=%q", c.src, got, c.want)
		}
	}
}
