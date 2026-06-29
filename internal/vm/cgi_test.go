// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"testing"
)

// TestCGI covers the CGI module's URL-, HTML- and element-encoding surface plus
// CGI.parse (require "cgi"), each output asserted against MRI Ruby 4.0.5
// (cgi/escape default + the cgi gem's CGI.parse).
func TestCGI(t *testing.T) {
	cases := []struct{ src, want string }{
		// --- escape / unescape: application/x-www-form-urlencoded (space -> '+').
		{`require "cgi"; p CGI.escape("a b&c=d/e")`, "\"a+b%26c%3Dd%2Fe\"\n"},
		{`require "cgi"; p CGI.escape("")`, "\"\"\n"},
		{`require "cgi"; p CGI.escape("héllo wörld")`, "\"h%C3%A9llo+w%C3%B6rld\"\n"},
		{`require "cgi"; p CGI.unescape("a+b%26c%3Dd%2Fe")`, "\"a b&c=d/e\"\n"},
		{`require "cgi"; p CGI.unescape("")`, "\"\"\n"},
		// Malformed escapes are left as-is (MRI never raises).
		{`require "cgi"; p CGI.unescape("%ZZbad")`, "\"%ZZbad\"\n"},
		{`require "cgi"; s="A z0_~ &%/=+"; p CGI.unescape(CGI.escape(s)) == s`, "true\n"},

		// --- escapeURIComponent / unescapeURIComponent: space -> "%20".
		{`require "cgi"; p CGI.escapeURIComponent("a b&c=d/e")`, "\"a%20b%26c%3Dd%2Fe\"\n"},
		{`require "cgi"; p CGI.escapeURIComponent("héllo wörld")`, "\"h%C3%A9llo%20w%C3%B6rld\"\n"},
		{`require "cgi"; p CGI.escapeURIComponent("")`, "\"\"\n"},
		{`require "cgi"; p CGI.unescapeURIComponent("a%20b%26c")`, "\"a b&c\"\n"},
		{`require "cgi"; p CGI.unescapeURIComponent("%ZZbad")`, "\"%ZZbad\"\n"},
		{`require "cgi"; s="A z0_~ &%/=+"; p CGI.unescapeURIComponent(CGI.escapeURIComponent(s)) == s`, "true\n"},

		// --- escapeHTML / unescapeHTML: five named entities.
		{`require "cgi"; p CGI.escapeHTML(%q{<a href="x">&'</a>})`, "\"&lt;a href=&quot;x&quot;&gt;&amp;&#39;&lt;/a&gt;\"\n"},
		{`require "cgi"; p CGI.escapeHTML("")`, "\"\"\n"},
		{`require "cgi"; p CGI.unescapeHTML("&lt;a&gt;&amp;&quot;&#39;")`, "\"<a>&\\\"'\"\n"},
		{`require "cgi"; p CGI.unescapeHTML("plain")`, "\"plain\"\n"},
		// Numeric (decimal / hex, both x and X) entities decode; unknowns stay.
		{`require "cgi"; p CGI.unescapeHTML("&amp;&#65;&#x42;&#X43;")`, "\"&ABC\"\n"},
		{`require "cgi"; p CGI.unescapeHTML("&unknown; &#; &#999999999999;")`, "\"&unknown; &#; &#999999999999;\"\n"},
		{`require "cgi"; s=%q{<b>&"'</b>}; p CGI.unescapeHTML(CGI.escapeHTML(s)) == s`, "true\n"},

		// --- escapeElement / unescapeElement: only the named tags are escaped.
		{`require "cgi"; p CGI.escapeElement(%q{<BR><A HREF="url"></A>}, "A")`, "\"<BR>&lt;A HREF=&quot;url&quot;&gt;&lt;/A&gt;\"\n"},
		// Case-insensitive word-boundary match: <A> hits, <AB> does not.
		{`require "cgi"; p CGI.escapeElement("<A><AB>", "A")`, "\"&lt;A&gt;<AB>\"\n"},
		// No element names -> string returned verbatim.
		{`require "cgi"; p CGI.escapeElement(%q{<BR><A></A>})`, "\"<BR><A></A>\"\n"},
		{`require "cgi"; p CGI.unescapeElement(CGI.escapeHTML(%q{<BR><A HREF="url"></A>}), "A")`, "\"&lt;BR&gt;<A HREF=\\\"url\\\"></A>\"\n"},
		{`require "cgi"; p CGI.unescapeElement(CGI.escapeHTML(%q{<BR><A></A>}))`, "\"&lt;BR&gt;&lt;A&gt;&lt;/A&gt;\"\n"},

		// --- parse: query -> Hash of String -> Array<String>, first-seen key order.
		{`require "cgi"; p CGI.parse("a=1&b=2&a=3")`, "{\"a\" => [\"1\", \"3\"], \"b\" => [\"2\"]}\n"},
		// Brackets are part of the key name verbatim (no [] special-casing).
		{`require "cgi"; p CGI.parse("x[]=1&x[]=2")`, "{\"x[]\" => [\"1\", \"2\"]}\n"},
		// A pair with no '=' yields an empty value array.
		{`require "cgi"; p CGI.parse("k")`, "{\"k\" => []}\n"},
		{`require "cgi"; p CGI.parse("")`, "{}\n"},
		// Values are form-decoded (%XX and '+').
		{`require "cgi"; p CGI.parse("a=h%C3%A9llo&q=a+b")`, "{\"a\" => [\"héllo\"], \"q\" => [\"a b\"]}\n"},
		// ';' is an alternate separator; empty pairs are skipped.
		{`require "cgi"; p CGI.parse("a=1;b=2&&c=3")`, "{\"a\" => [\"1\"], \"b\" => [\"2\"], \"c\" => [\"3\"]}\n"},
		// An empty key ("=v") is kept.
		{`require "cgi"; p CGI.parse("=v")`, "{\"\" => [\"v\"]}\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
