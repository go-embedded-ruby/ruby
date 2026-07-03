// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestI18nFeature covers the require "i18n" feature probe and the module/error
// tree shape.
func TestI18nFeature(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "i18n"`, "true\n"},
		{`require "i18n"; p require "i18n"`, "false\n"},
		{`require "i18n"; p I18n.is_a?(Module)`, "true\n"},
		{`require "i18n"; p I18n::ArgumentError < ::ArgumentError`, "true\n"},
		{`require "i18n"; p I18n::MissingTranslationData < I18n::ArgumentError`, "true\n"},
		{`require "i18n"; p I18n::MissingTranslation == I18n::MissingTranslationData`, "true\n"},
		{`require "i18n"; p I18n::MissingInterpolationArgument < I18n::ArgumentError`, "true\n"},
		{`require "i18n"; p I18n::ReservedInterpolationKey < I18n::ArgumentError`, "true\n"},
		{`require "i18n"; p I18n::InvalidPluralizationData < I18n::ArgumentError`, "true\n"},
		{`require "i18n"; p I18n::Backend::Simple.is_a?(Class)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestI18nLocale covers the locale accessors.
func TestI18nLocale(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "i18n"; p I18n.locale`, ":en\n"},
		{`require "i18n"; p I18n.default_locale`, ":en\n"},
		{`require "i18n"; I18n.locale = :fr; p I18n.locale`, ":fr\n"},
		{`require "i18n"; I18n.locale = "de"; p I18n.locale`, ":de\n"},
		{`require "i18n"; I18n.default_locale = :es; p I18n.default_locale`, ":es\n"},
		{`require "i18n"; I18n.default_locale = "pt"; p I18n.default_locale`, ":pt\n"},
		{`require "i18n"; p I18n.available_locales`, "[]\n"},
		{`require "i18n"; I18n.backend.store_translations(:en, {a: "x"}); p I18n.available_locales`, "[:en]\n"},
		{`require "i18n"; I18n.backend.store_translations(:fr, {a: "x"}); p I18n.backend.available_locales`, "[:fr]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestI18nTranslate covers I18n.translate/#t: plain lookup, interpolation, count
// pluralization, scope, defaults, locale override, subtree return and missing.
func TestI18nTranslate(t *testing.T) {
	setup := `require "i18n"
I18n.backend.store_translations(:en, {
  hello: "Hello",
  greet: "Hi %{name}",
  items: {one: "1 item", other: "%{count} items"},
  nav: {home: "Home", about: "About"},
  colors: ["red", "green"]
})
I18n.backend.store_translations(:fr, {hello: "Bonjour"})
`
	cases := []struct{ src, want string }{
		{setup + `p I18n.t("hello")`, "\"Hello\"\n"},
		{setup + `p I18n.t(:hello)`, "\"Hello\"\n"},
		{setup + `p I18n.translate("greet", name: "Bob")`, "\"Hi Bob\"\n"},
		{setup + `p I18n.t("items", count: 1)`, "\"1 item\"\n"},
		{setup + `p I18n.t("items", count: 3)`, "\"3 items\"\n"},
		{setup + `p I18n.t("home", scope: "nav")`, "\"Home\"\n"},
		{setup + `p I18n.t("about", scope: [:nav])`, "\"About\"\n"},
		{setup + `p I18n.t("hello", locale: :fr)`, "\"Bonjour\"\n"},
		{setup + `p I18n.t("missing", default: "fallback")`, "\"fallback\"\n"},
		{setup + `p I18n.t("missing", default: :hello)`, "\"Hello\"\n"},
		{setup + `p I18n.t("missing", default: [:absent, "last"])`, "\"last\"\n"},
		// A subtree comes back as a Hash with Symbol keys; the library stores it in
		// a Go map, so read individual keys for a deterministic assertion.
		{setup + `h = I18n.t("nav"); p [h[:home], h[:about]]`, "[\"Home\", \"About\"]\n"},
		{setup + `p I18n.t("colors")`, "[\"red\", \"green\"]\n"},
		{setup + `p I18n.t("missing")`, "\"Translation missing: en.missing\"\n"},
		{setup + `p I18n.exists?("hello")`, "true\n"},
		{setup + `p I18n.exists?("missing")`, "false\n"},
		{setup + `p I18n.exists?("hello", :fr)`, "true\n"},
		{setup + `p I18n.exists?("hello", :de)`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestI18nTranslateErrors covers the raising forms and the argument checks.
func TestI18nTranslateErrors(t *testing.T) {
	setup := `require "i18n"
I18n.backend.store_translations(:en, {greet: "Hi %{name}"})
`
	if cls, _ := evalErr(t, setup+`I18n.t!("missing")`); cls != "I18n::MissingTranslationData" {
		t.Errorf("t! missing: got %s", cls)
	}
	if cls, _ := evalErr(t, setup+`I18n.t("missing", raise: true)`); cls != "I18n::MissingTranslationData" {
		t.Errorf("raise: true: got %s", cls)
	}
	// An absent %{name} raises once interpolation runs (the library interpolates
	// when at least one value is supplied), matching the gem's raising handler.
	if cls, _ := evalErr(t, setup+`I18n.t("greet", other: "x")`); cls != "I18n::MissingInterpolationArgument" {
		t.Errorf("missing interpolation: got %s", cls)
	}
	if cls, _ := evalErr(t, `require "i18n"; I18n.t`); cls != "ArgumentError" {
		t.Errorf("translate/0: got %s", cls)
	}
	if cls, _ := evalErr(t, `require "i18n"; I18n.exists?`); cls != "ArgumentError" {
		t.Errorf("exists?/0: got %s", cls)
	}
	if cls, _ := evalErr(t, `require "i18n"; I18n.backend.store_translations(:en)`); cls != "ArgumentError" {
		t.Errorf("store_translations/1: got %s", cls)
	}
	if cls, _ := evalErr(t, `require "i18n"; I18n.l`); cls != "ArgumentError" {
		t.Errorf("localize/0: got %s", cls)
	}
}

// TestI18nLocalize covers I18n.localize/#l over Time and Date, named and literal
// formats, and a locale-specific format tree.
func TestI18nLocalize(t *testing.T) {
	setup := `require "i18n"; require "date"
I18n.backend.store_translations(:en, {
  time: {formats: {default: "%Y-%m-%d %H:%M:%S", short: "%H:%M"}},
  date: {formats: {default: "%Y-%m-%d"}}
})
`
	// TZ-safe: parse an explicit-UTC instant so the broken-down fields do not
	// depend on the host time zone.
	tm := `Time.parse("2026-07-04T13:30:15+00:00")`
	cases := []struct{ src, want string }{
		{setup + `p I18n.l(` + tm + `)`, "\"2026-07-04 13:30:15\"\n"},
		{setup + `p I18n.l(` + tm + `, format: :short)`, "\"13:30\"\n"},
		{setup + `p I18n.l(` + tm + `, format: "%Y")`, "\"2026\"\n"},
		{setup + `p I18n.l(Date.new(2026, 7, 4))`, "\"2026-07-04\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// A named format missing from the store raises MissingTranslationData.
	if cls, _ := evalErr(t, setup+`I18n.l(`+tm+`, format: :nope)`); cls != "I18n::MissingTranslationData" {
		t.Errorf("unknown format: got %s", cls)
	}
}

// TestI18nLocalizeAccessors exercises the Temporal accessors the strftime layer
// reads for a literal format (Wday %w, Yday %j, Nsec %N, ZoneOffset %z, ZoneName
// %Z) over both a Time (clock present) and a Date (clock absent), covering the
// respond-to? true and false arms of the adapter.
func TestI18nLocalizeAccessors(t *testing.T) {
	setup := "require \"i18n\"; require \"date\"\n"
	// %w day-of-week, %j day-of-year, %N nsec, %z offset, %Z zone.
	fmt := "%w %j %N %z %Z"
	t1 := `Time.parse("2026-07-04T13:30:15+00:00")`
	// A literal (String) format bypasses the format store; both values format
	// without raising, which is enough to drive every accessor arm.
	if got := eval(t, setup+`p I18n.l(`+t1+`, format: "`+fmt+`").is_a?(String)`); got != "true\n" {
		t.Errorf("time literal format: got %q", got)
	}
	if got := eval(t, setup+`p I18n.l(Date.new(2026, 7, 4), format: "`+fmt+`").is_a?(String)`); got != "true\n" {
		t.Errorf("date literal format: got %q", got)
	}
	// A non-String, non-Symbol :format is coerced to its to_s and used literally;
	// an explicit :locale is honoured.
	if got := eval(t, setup+`p I18n.l(`+t1+`, format: 2026, locale: :en)`); got != "\"2026\"\n" {
		t.Errorf("integer format: got %q", got)
	}
}

// TestI18nExtraErrors covers the reserved-interpolation-key and
// invalid-pluralization-data error arms, plus odd key/value coercions.
func TestI18nExtraErrors(t *testing.T) {
	// A reserved placeholder (%{default}) raises once interpolation runs.
	res := `require "i18n"; I18n.backend.store_translations(:en, {r: "a %{default} b"}); I18n.t("r", x: 1)`
	if cls, _ := evalErr(t, res); cls != "I18n::ReservedInterpolationKey" {
		t.Errorf("reserved key: got %s", cls)
	}
	// A :count lookup whose plural subtree lacks the needed category is invalid.
	pl := `require "i18n"; I18n.backend.store_translations(:en, {items: {one: "one"}}); I18n.t("items", count: 5)`
	if cls, _ := evalErr(t, pl); cls != "I18n::InvalidPluralizationData" {
		t.Errorf("invalid pluralization: got %s", cls)
	}
	// A non-Symbol/String key coerces via to_s; a non-Hash store data is a no-op.
	if got := eval(t, `require "i18n"; I18n.backend.store_translations(:en, {1 => "one"}); p I18n.t("1")`); got != "\"one\"\n" {
		t.Errorf("integer key: got %q", got)
	}
	if got := eval(t, `require "i18n"; I18n.backend.store_translations(:en, 42); p true`); got != "true\n" {
		t.Errorf("non-hash data: got %q", got)
	}
	// A Symbol value round-trips to its name; an unmodelled value coerces via to_s.
	if got := eval(t, `require "i18n"; I18n.backend.store_translations(:en, {s: :sym, r: (1..3)}); p [I18n.t("s"), I18n.t("r")]`); got != "[\"sym\", \"1..3\"]\n" {
		t.Errorf("symbol/range value: got %q", got)
	}
}

// TestI18nValueRoundTrip covers the store/return value conversions for every
// leaf type (numbers, booleans, nil) exercised through translate.
func TestI18nValueRoundTrip(t *testing.T) {
	setup := `require "i18n"
I18n.backend.store_translations(:en, {n: 7, f: 1.5, yes: true, no: false, nada: nil, deep: {list: [1, true, nil]}})
`
	cases := []struct{ src, want string }{
		{setup + `p I18n.t("n")`, "7\n"},
		{setup + `p I18n.t("f")`, "1.5\n"},
		{setup + `p I18n.t("yes")`, "true\n"},
		{setup + `p I18n.t("no")`, "false\n"},
		{setup + `p I18n.t("deep")`, "{list: [1, true, nil]}\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}
