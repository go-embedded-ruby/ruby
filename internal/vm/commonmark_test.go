// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestCommonmarkModule covers the Commonmark/CommonMarker loadable module
// (require "commonmark"): both names refer to the same module, and require
// reports the usual once-only load semantics.
func TestCommonmarkModule(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "commonmark"; p Commonmark.equal?(CommonMarker)`, "true\n"},
		{`require "commonmark"; p Commonmark.is_a?(Module)`, "true\n"},
		{`p require "commonmark"`, "true\n"},
		{`require "commonmark"; p require "commonmark"`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCommonmarkRenderHTML covers render_html / to_html over the CommonMark core:
// headings, emphasis, and the strict-CommonMark default (raw HTML filtered).
func TestCommonmarkRenderHTML(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "commonmark"; print Commonmark.render_html("# Hi")`, "<h1>Hi</h1>\n"},
		{`require "commonmark"; print Commonmark.render_html("a *b* c")`, "<p>a <em>b</em> c</p>\n"},
		// to_html is the same entry point.
		{`require "commonmark"; print CommonMarker.to_html("**x**")`, "<p><strong>x</strong></p>\n"},
		// A non-String argument is coerced via to_s.
		{`require "commonmark"; print Commonmark.render_html(:hi)`, "<p>hi</p>\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCommonmarkOptions covers the option bridge: a Hash of extension flags (as
// Symbol or String keys), an Array of extension symbols, and the two spellings of
// each name, plus the renderer flags (unsafe, hardbreaks).
func TestCommonmarkOptions(t *testing.T) {
	cases := []struct{ src, want string }{
		// Strikethrough via a Hash with a Symbol key.
		{`require "commonmark"; print Commonmark.render_html("~~x~~", strikethrough: true)`, "<p><del>x</del></p>\n"},
		// The plural spelling is accepted too.
		{`require "commonmark"; print Commonmark.render_html("~~x~~", {"strikethroughs" => true})`, "<p><del>x</del></p>\n"},
		// An Array of extension symbols enables them.
		{`require "commonmark"; print Commonmark.render_html("~~x~~", [:strikethrough])`, "<p><del>x</del></p>\n"},
		// A String key works as well.
		{`require "commonmark"; print Commonmark.render_html("~~x~~", {"strikethrough" => true})`, "<p><del>x</del></p>\n"},
		// unsafe: passes raw HTML through instead of filtering it.
		{`require "commonmark"; print Commonmark.render_html("<div>hi</div>", unsafe: true)`, "<div>hi</div>\n"},
		// The default (no options) filters raw HTML.
		{`require "commonmark"; print Commonmark.render_html("<div>hi</div>")`, "<!-- raw HTML omitted -->\n"},
		// hard_breaks turns a soft break into <br />.
		{`require "commonmark"; print Commonmark.render_html("a\nb", hard_breaks: true)`, "<p>a<br />\nb</p>\n"},
		// An unrecognised key is ignored (strict CommonMark).
		{`require "commonmark"; print Commonmark.render_html("~~x~~", nope: true)`, "<p>~~x~~</p>\n"},
		// A non-Hash/Array/nil option value selects the strict default.
		{`require "commonmark"; print Commonmark.render_html("# H", 42)`, "<h1>H</h1>\n"},
		// An explicit nil options argument is the strict default.
		{`require "commonmark"; print Commonmark.render_html("# H", nil)`, "<h1>H</h1>\n"},
		// tables (both spellings) render a GFM pipe table.
		{`require "commonmark"; p Commonmark.render_html("|a|\n|-|\n|1|", table: true).include?("<table>")`, "true\n"},
		{`require "commonmark"; p Commonmark.render_html("|a|\n|-|\n|1|", tables: true).include?("<table>")`, "true\n"},
		// autolink (both spellings) links a bare URL.
		{`require "commonmark"; p Commonmark.render_html("http://x.co", autolink: true).include?("<a href")`, "true\n"},
		{`require "commonmark"; p Commonmark.render_html("http://x.co", autolinks: true).include?("<a href")`, "true\n"},
		// task list (all spellings) renders a checkbox.
		{`require "commonmark"; p Commonmark.render_html("- [x] done", tasklist: true).include?("checkbox")`, "true\n"},
		{`require "commonmark"; p Commonmark.render_html("- [x] done", task_list: true).include?("checkbox")`, "true\n"},
		{`require "commonmark"; p Commonmark.render_html("- [x] done", tasklists: true).include?("checkbox")`, "true\n"},
		// hardbreaks alternate spelling turns a soft break into <br />.
		{`require "commonmark"; p Commonmark.render_html("a\nb", hardbreaks: true).include?("<br />")`, "true\n"},
		// unsafe_ alternate spelling passes raw inline HTML through.
		{`require "commonmark"; p Commonmark.render_html("<b>x</b>", unsafe_: true).include?("<b>x</b>")`, "true\n"},
		// github_pre_lang (both spellings) emits the language on the <pre> element.
		{"require \"commonmark\"; p Commonmark.render_html(\"```rb\\nx\\n```\", github_pre_lang: true).include?(\"<pre lang=\\\"rb\\\">\")", "true\n"},
		{"require \"commonmark\"; p Commonmark.render_html(\"```rb\\nx\\n```\", githubpre_lang: true).include?(\"<pre lang=\\\"rb\\\">\")", "true\n"},
		// A Hash value that is falsey leaves the extension off.
		{`require "commonmark"; print Commonmark.render_html("~~x~~", strikethrough: false)`, "<p>~~x~~</p>\n"},
		// A non-Symbol/String key falls back to to_s (here an unrecognised name).
		{`require "commonmark"; print Commonmark.render_html("# H", {42 => true})`, "<h1>H</h1>\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCommonmarkStringToHTML covers the String#to_html shortcut, with and without
// an options argument.
func TestCommonmarkStringToHTML(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "commonmark"; print "# T".to_html`, "<h1>T</h1>\n"},
		{`require "commonmark"; print "~~z~~".to_html(strikethrough: true)`, "<p><del>z</del></p>\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestCommonmarkArity covers the wrong-number-of-arguments guard.
func TestCommonmarkArity(t *testing.T) {
	src := `require "commonmark"
begin
  Commonmark.render_html
rescue ArgumentError => e
  print "argerror"
end`
	if got := eval(t, src); got != "argerror" {
		t.Errorf("got=%q want=argerror", got)
	}
}
