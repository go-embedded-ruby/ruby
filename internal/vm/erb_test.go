// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestERBRequire covers that `require "erb"` returns true on the first load and
// false thereafter, matching MRI's provided-feature contract.
func TestERBRequire(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "erb"`, "true\n"},
		{"require \"erb\"\np require \"erb\"", "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestERBBasic covers the three tag kinds, the <%% / %%> literals, comment tags
// and result_with_hash binding, asserted against MRI Ruby 4.0.5.
func TestERBBasic(t *testing.T) {
	cases := []struct{ src, want string }{
		// <%= expr %> with a local bound via result_with_hash.
		{`require "erb"; puts ERB.new("Hello <%= name %>!").result_with_hash(name: "x")`, "Hello x!\n"},
		// <%# comment %> emits nothing.
		{`require "erb"; puts ERB.new("a<%# c %>b").result_with_hash({})`, "ab\n"},
		// <%% / %%> literals: only <%% collapses to <%.
		{`require "erb"; puts ERB.new("<%% literal %%>").result_with_hash({})`, "<% literal %%>\n"},
		// <% code %> runs but emits nothing.
		{`require "erb"; puts ERB.new("a<% x = 1 %>b").result_with_hash({})`, "ab\n"},
		// %%> inside a tag body is a literal %> and does not close the tag.
		{`require "erb"; p ERB.new("<%= \"a%%>b\" %>").result_with_hash({})`, "\"a%>b\"\n"},
		// empty template renders the empty string.
		{`require "erb"; p ERB.new("").result_with_hash({})`, "\"\"\n"},
		// result with no argument uses a fresh top-level binding.
		{`require "erb"; p ERB.new("<%= 1 + 2 %>").result`, "\"3\"\n"},
		// multiple expressions and surrounding text.
		{`require "erb"; puts ERB.new("<%= a %>+<%= b %>=<%= a + b %>").result_with_hash(a: 2, b: 3)`, "2+3=5\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestERBTrimModes covers every trim mode against MRI's exact whitespace handling:
// "-" (-%>), ">" (tag ends line), "<>" (tag starts and ends line), "%" (percent
// code lines), and the default (no trimming).
func TestERBTrimModes(t *testing.T) {
	cases := []struct{ src, want string }{
		// default: newline after the tag is kept.
		{`require "erb"; p ERB.new("line1\n<% x=1 %>\nline2\n").result`, "\"line1\\n\\nline2\\n\"\n"},
		// dash mode: -%> strips one immediately-following newline.
		{`require "erb"; p ERB.new("line1\n<% x=1 -%>\nline2\n", trim_mode: "-").result`, "\"line1\\nline2\\n\"\n"},
		// dash: spaces between -%> and newline defeat the trim.
		{`require "erb"; p ERB.new("a<% x=1 -%>  \nb", trim_mode: "-").result`, "\"a  \\nb\"\n"},
		// dash: -%> with no newline after it strips nothing.
		{`require "erb"; p ERB.new("<% x=1 -%>X", trim_mode: "-").result`, "\"X\"\n"},
		// dash applies to <%= too.
		{`require "erb"; p ERB.new("<%= 1 -%>\nb", trim_mode: "-").result`, "\"1b\"\n"},
		// > mode: trims newline after any tag that ends its line.
		{`require "erb"; p ERB.new("a<% x=1 %>\nb").result`, "\"a\\nb\"\n"},
		{`require "erb"; p ERB.new("a<% x=1 %>\nb", trim_mode: ">").result`, "\"ab\"\n"},
		// > mode: spaces before the newline defeat the trim.
		{`require "erb"; p ERB.new("a<% x=1 %>  \nb", trim_mode: ">").result`, "\"a  \\nb\"\n"},
		// <> mode: trims only when the tag both starts and ends the line.
		{`require "erb"; p ERB.new("<% x=1 %>\nb", trim_mode: "<>").result`, "\"b\"\n"},
		{`require "erb"; p ERB.new("a<% x=1 %>\nb", trim_mode: "<>").result`, "\"a\\nb\"\n"},
		// percent mode: a leading % is a code line consuming its whole line.
		{`require "erb"; p ERB.new("% x=1\nhi\n", trim_mode: "%").result`, "\"hi\\n\"\n"},
		{`require "erb"; p ERB.new("a\n% x=1\nb\n", trim_mode: "%").result`, "\"a\\nb\\n\"\n"},
		// percent mode: %% at line start is a literal %.
		{`require "erb"; p ERB.new("%% literal\n", trim_mode: "%").result`, "\"% literal\\n\"\n"},
		// percent mode on a line without %: passed through verbatim.
		{`require "erb"; p ERB.new("plain\n", trim_mode: "%").result`, "\"plain\\n\"\n"},
		// percent code line without trailing newline.
		{`require "erb"; p ERB.new("% x=1", trim_mode: "%").result`, "\"\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestERBUtil covers ERB::Util.html_escape / .h and .url_encode / .u, matching
// MRI's byte-exact escaping output.
func TestERBUtil(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "erb"; puts ERB::Util.html_escape("<a>&\"")`, "&lt;a&gt;&amp;&quot;\n"},
		{`require "erb"; puts ERB::Util.h("a&b<c>d\"e'f")`, "a&amp;b&lt;c&gt;d&quot;e&#39;f\n"},
		// non-string coerced via to_s; nil -> "".
		{`require "erb"; p ERB::Util.h(nil)`, "\"\"\n"},
		{`require "erb"; p ERB::Util.h(123)`, "\"123\"\n"},
		{`require "erb"; puts ERB::Util.url_encode("a b/c?")`, "a%20b%2Fc%3F\n"},
		{`require "erb"; puts ERB::Util.u("aA0-_.~ /")`, "aA0-_.~%20%2F\n"},
		{`require "erb"; puts ERB::Util.url_encode("Hello World!")`, "Hello%20World%21\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestERBAccessors covers filename, lineno, location, src, run and result with an
// explicit binding capturing the caller's locals (the shape Puppet uses).
func TestERBAccessors(t *testing.T) {
	cases := []struct{ src, want string }{
		// result(binding) sees a caller local.
		{`require "erb"; greeting = "world"; puts ERB.new("hi <%= greeting %>").result(binding)`, "hi world\n"},
		// filename= / file readers round-trip; lineno defaults to 0.
		{`require "erb"; e = ERB.new("x"); e.filename = "t.erb"; p e.filename`, "\"t.erb\"\n"},
		{`require "erb"; e = ERB.new("x"); p e.lineno`, "0\n"},
		// the filename threads through to result so eval reports it.
		{`require "erb"; e = ERB.new("x"); e.filename = "t.erb"; p e.result`, "\"x\"\n"},
		// src exposes the compiled Ruby and includes the buffer name.
		{`require "erb"; e = ERB.new("a<%= 1 %>"); p e.src.include?("_erbout")`, "true\n"},
		// custom eoutvar threads through to src.
		{`require "erb"; e = ERB.new("x<%= y %>", eoutvar: "buf"); p e.src.include?("buf")`, "true\n"},
		// run prints the result to stdout.
		{`require "erb"; ERB.new("ran <%= 2*3 %>").run(binding)`, "ran 6"},
		// encoding reader.
		{`require "erb"; p ERB.new("x").encoding`, "\"UTF-8\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestERBUnterminated covers the unterminated-tag path: like MRI, the opening
// "<%" (and any marker) is dropped and the rest is rendered as literal text.
func TestERBUnterminated(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "erb"; p ERB.new("a <% x = 1 ").result`, "\"a  x = 1 \"\n"},
		{`require "erb"; p ERB.new("a <%= 1 ").result`, "\"a  1 \"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
