// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"testing"
)

// TestCGI covers the CGI module's URL- and HTML-encoding surface
// (require "cgi"), asserted against MRI Ruby 4.0.5.
func TestCGI(t *testing.T) {
	cases := []struct{ src, want string }{
		// CGI.escape is application/x-www-form-urlencoded: space -> '+'.
		{`require "cgi"; p CGI.escape("a b&c=d/e")`, "\"a+b%26c%3Dd%2Fe\"\n"},
		{`require "cgi"; p CGI.escape("")`, "\"\"\n"},
		{`require "cgi"; p CGI.escape("héllo wörld")`, "\"h%C3%A9llo+w%C3%B6rld\"\n"},
		// CGI.unescape reverses it ('+' -> space, %XX decoded).
		{`require "cgi"; p CGI.unescape("a+b%26c%3Dd%2Fe")`, "\"a b&c=d/e\"\n"},
		{`require "cgi"; p CGI.unescape("")`, "\"\"\n"},
		// Malformed escapes are left as-is (MRI never raises) -> the err arm.
		{`require "cgi"; p CGI.unescape("%ZZbad")`, "\"%ZZbad\"\n"},
		// Round-trip across the full byte range stays stable.
		{`require "cgi"; s="A z0_~ &%/=+"; p CGI.unescape(CGI.escape(s)) == s`, "true\n"},
		// escapeHTML encodes the five entities.
		{`require "cgi"; p CGI.escapeHTML(%q{<a href="x">&'</a>})`, "\"&lt;a href=&quot;x&quot;&gt;&amp;&#39;&lt;/a&gt;\"\n"},
		{`require "cgi"; p CGI.escapeHTML("")`, "\"\"\n"},
		// unescapeHTML reverses the same five entities.
		{`require "cgi"; p CGI.unescapeHTML("&lt;a&gt;&amp;&quot;&#39;")`, "\"<a>&\\\"'\"\n"},
		{`require "cgi"; p CGI.unescapeHTML("plain")`, "\"plain\"\n"},
		// escape/unescapeHTML round-trip.
		{`require "cgi"; s=%q{<b>&"'</b>}; p CGI.unescapeHTML(CGI.escapeHTML(s)) == s`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
