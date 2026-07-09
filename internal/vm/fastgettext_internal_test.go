// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// fgRun runs a Ruby program with `require "fast_gettext"` prepended.
func fgRun(t *testing.T, body string) string {
	t.Helper()
	return runSrc(t, "require \"fast_gettext\"\n"+body)
}

// TestFastGettextFlow drives the headline flow: register an in-memory domain
// (singular + plural translations), select it and a locale, then translate
// through _ / n_ / s_ / p_ and switch locale.
func TestFastGettextFlow(t *testing.T) {
	got := fgRun(t, `
FastGettext.available_locales = ["en", "de"]
puts FastGettext.available_locales.inspect
FastGettext.add_text_domain("app",
  translations: {"en" => {"Hello" => "Hello!", "menu|File" => "File!"}, "de" => {"Hello" => "Hallo"}},
  plurals: {"en" => {["apple", "apples"] => ["apple", "apples"]}})
FastGettext.text_domain = "app"
puts FastGettext.text_domain
FastGettext.locale = "en"
puts FastGettext.locale
puts FastGettext._("Hello")
puts FastGettext._("Missing")
puts FastGettext.n_("apple", "apples", 1)
puts FastGettext.n_("apple", "apples", 2)
puts FastGettext.s_("menu|File")
puts FastGettext.s_("nomenu|Bar")
puts FastGettext.p_("ctx", "key")
puts FastGettext.key_exist?("Hello")
puts FastGettext.key_exist?("Nope")
FastGettext.locale = "de"
puts FastGettext._("Hello")
r = FastGettext.with_locale("en") { FastGettext::Translation._("Hello") }
puts r
puts FastGettext.locale
puts FastGettext.set_locale("en")
`)
	want := `["en", "de"]` + "\napp\nen\nHello!\nMissing\napple\napples\nFile!\nBar\nkey\ntrue\nfalse\nHallo\nHello!\nde\nen"
	if got != want {
		t.Fatalf("flow:\n got=%q\nwant=%q", got, want)
	}
}

// TestFastGettextDefaults covers the default_locale / default_text_domain
// setters and readers (nil when unset), an empty domain registration, and
// with_domain returning its block value.
func TestFastGettextDefaults(t *testing.T) {
	got := fgRun(t, `
puts FastGettext.text_domain.inspect
puts FastGettext.default_locale.inspect
puts FastGettext.default_text_domain.inspect
FastGettext.default_locale = "en"
puts FastGettext.default_locale
FastGettext.default_text_domain = "app"
puts FastGettext.default_text_domain
FastGettext.add_text_domain("empty")
puts FastGettext.with_domain("empty") { 42 }
`)
	want := "nil\nnil\nnil\nen\napp\n42"
	if got != want {
		t.Fatalf("defaults:\n got=%q\nwant=%q", got, want)
	}
}

// TestFastGettextStringKeys covers keyword Hashes with string keys (the fgKwGet
// string fallback), the s_ separator form, and the fgIntArg non-integer count
// (treated as zero, selecting the plural form under the default rule).
func TestFastGettextStringKeys(t *testing.T) {
	got := fgRun(t, `
FastGettext.available_locales = ["en"]
FastGettext.add_text_domain("d",
  "translations" => {"en" => {"menu/File" => "F2"}},
  "plurals" => {"en" => {["a", "b"] => ["a", "b"]}})
FastGettext.text_domain = "d"
FastGettext.locale = "en"
puts FastGettext.s_("menu/File", "/")
puts FastGettext.n_("a", "b", "x")
`)
	want := "F2\nb"
	if got != want {
		t.Fatalf("string keys:\n got=%q\nwant=%q", got, want)
	}
}

// TestFastGettextMalformedDomain covers the defensive skip branches in the
// translation / plural loaders: a non-Hash translations / plurals value, a
// non-Hash per-locale entry, and a plural entry whose key or forms is not an
// Array. None of these register anything, so the keys stay untranslated.
func TestFastGettextMalformedDomain(t *testing.T) {
	got := fgRun(t, `
FastGettext.available_locales = ["en"]
FastGettext.add_text_domain("bad1", translations: "nope", plurals: "nope")
FastGettext.add_text_domain("bad2", translations: {"en" => "nope"}, plurals: {"en" => "nope"})
FastGettext.add_text_domain("bad3", plurals: {"en" => {"k" => "v", ["s", "p"] => "notarray"}})
FastGettext.text_domain = "bad3"
FastGettext.locale = "en"
puts FastGettext._("Hello")
`)
	if got != "Hello" {
		t.Fatalf("malformed domain: got=%q want=Hello", got)
	}
}

// TestFastGettextArgErrors covers the ArgumentError paths: _ / n_ / p_ with too
// few arguments, available_locales= with a non-Array, and available_locales=
// invoked with no argument.
func TestFastGettextArgErrors(t *testing.T) {
	cases := []string{
		"FastGettext._",
		`FastGettext.n_("a", "b")`,
		`FastGettext.p_("ctx")`,
		`FastGettext.available_locales = 42`,
		`FastGettext.send(:available_locales=)`,
	}
	for _, expr := range cases {
		got := fgRun(t, "begin; "+expr+"; rescue => e; puts e.class; end")
		if got != "ArgumentError" {
			t.Fatalf("%s expected ArgumentError, got %q", expr, got)
		}
	}
}
