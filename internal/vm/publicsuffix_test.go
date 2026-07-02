// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestPublicSuffixConstants covers the PublicSuffix module, its Domain class and
// the error tree (require "public_suffix").
func TestPublicSuffixConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "public_suffix"; p PublicSuffix.is_a?(Module)`, "true\n"},
		{`p require "public_suffix"`, "true\n"},
		{`require "public_suffix"; p require "public_suffix"`, "false\n"},
		{`require "public_suffix"; p PublicSuffix::Domain.is_a?(Class)`, "true\n"},
		{`require "public_suffix"; p PublicSuffix::Error.ancestors.include?(StandardError)`, "true\n"},
		{`require "public_suffix"; p PublicSuffix::DomainInvalid.ancestors.include?(PublicSuffix::Error)`, "true\n"},
		{`require "public_suffix"; p PublicSuffix::DomainNotAllowed.ancestors.include?(PublicSuffix::DomainInvalid)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPublicSuffixParse covers PublicSuffix.parse and the Domain readers.
func TestPublicSuffixParse(t *testing.T) {
	pre := `require "public_suffix"; d = PublicSuffix.parse("www.example.com"); `
	cases := []struct{ src, want string }{
		{pre + `p d.tld`, "\"com\"\n"},
		{pre + `p d.sld`, "\"example\"\n"},
		{pre + `p d.trd`, "\"www\"\n"},
		{pre + `p d.name`, "\"www.example.com\"\n"},
		{pre + `p d.to_s`, "\"www.example.com\"\n"},
		{pre + `p d.domain`, "\"example.com\"\n"},
		{pre + `p d.subdomain`, "\"www.example.com\"\n"},
		{pre + `p d.domain?`, "true\n"},
		{pre + `p d.subdomain?`, "true\n"},
		{pre + `p d.class.name`, "\"PublicSuffix::Domain\"\n"},
		{pre + `p d.inspect.start_with?("#<PublicSuffix::Domain")`, "true\n"},
		// A registrable domain with no subdomain: trd absent, domain? true.
		{`require "public_suffix"; d = PublicSuffix.parse("example.com"); p d.trd`, "nil\n"},
		{`require "public_suffix"; d = PublicSuffix.parse("example.com"); p d.domain?`, "true\n"},
		{`require "public_suffix"; d = PublicSuffix.parse("example.com"); p d.subdomain?`, "false\n"},
		{`require "public_suffix"; d = PublicSuffix.parse("example.com"); p d.subdomain`, "nil\n"},
		// A multi-label public suffix.
		{`require "public_suffix"; d = PublicSuffix.parse("example.co.uk"); p d.tld`, "\"co.uk\"\n"},
		{`require "public_suffix"; d = PublicSuffix.parse("example.co.uk"); p d.sld`, "\"example\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPublicSuffixDomainValid covers PublicSuffix.domain, .valid? and options.
func TestPublicSuffixDomainValid(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "public_suffix"; p PublicSuffix.domain("www.example.com")`, "\"example.com\"\n"},
		{`require "public_suffix"; p PublicSuffix.valid?("www.example.com")`, "true\n"},
		{`require "public_suffix"; p PublicSuffix.valid?("nonexistent-tld-xyzzy")`, "false\n"},
		// domain of a bare public suffix is nil (no registrable domain).
		{`require "public_suffix"; p PublicSuffix.domain("com")`, "nil\n"},
		// default_rule: nil disables the fallback rule so an unlisted TLD is invalid.
		{`require "public_suffix"; p PublicSuffix.valid?("example.zzqq", default_rule: nil)`, "false\n"},
		// ignore_private: true is accepted and honoured.
		{`require "public_suffix"; p PublicSuffix.valid?("example.com", ignore_private: true)`, "true\n"},
		// A String options key works too.
		{`require "public_suffix"; p PublicSuffix.valid?("example.com", "ignore_private" => true)`, "true\n"},
		// A non-Hash trailing arg is ignored (falls through to default options).
		{`require "public_suffix"; p PublicSuffix.valid?("example.com", 42)`, "true\n"},
		// An unrecognised option key is ignored.
		{`require "public_suffix"; p PublicSuffix.valid?("example.com", bogus: true)`, "true\n"},
		// A non-Symbol / non-String option key (Integer) coerces via to_s and is
		// ignored (exercises the key-coercion default branch).
		{`require "public_suffix"; p PublicSuffix.valid?("example.com", { 1 => true })`, "true\n"},
		// A parsed Domain is truthy and renders via to_s in string interpolation.
		{`require "public_suffix"; d = PublicSuffix.parse("www.example.com"); puts(d ? "yes" : "no")`, "yes\n"},
		{`require "public_suffix"; d = PublicSuffix.parse("www.example.com"); puts "d=#{d}"`, "d=www.example.com\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestPublicSuffixErrors covers the arity and raise paths.
func TestPublicSuffixErrors(t *testing.T) {
	// No-argument calls raise ArgumentError.
	for _, call := range []string{
		`PublicSuffix.parse`,
		`PublicSuffix.domain`,
		`PublicSuffix.valid?`,
	} {
		src := `require "public_suffix"
begin
  ` + call + `
rescue ArgumentError
  puts "arity"
end`
		if got := eval(t, src); !strings.Contains(got, "arity") {
			t.Errorf("%s no-arg: got %q", call, got)
		}
	}
	// parse of an invalid name raises PublicSuffix::DomainInvalid.
	got := eval(t, `require "public_suffix"
begin
  PublicSuffix.parse("")
rescue PublicSuffix::DomainInvalid
  puts "invalid"
end`)
	if !strings.Contains(got, "invalid") {
		t.Errorf("parse invalid: got %q", got)
	}
	// parse of a bare public suffix raises PublicSuffix::DomainNotAllowed.
	got = eval(t, `require "public_suffix"
begin
  PublicSuffix.parse("com")
rescue PublicSuffix::DomainNotAllowed
  puts "notallowed"
end`)
	if !strings.Contains(got, "notallowed") {
		t.Errorf("parse bare suffix: got %q", got)
	}
}
