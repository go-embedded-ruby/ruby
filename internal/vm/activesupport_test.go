// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// asCase drives one require + expression through the interpreter, asserting the
// printed output. Every case loads active_support so the Inflector module and the
// core extensions are exercised through their public Ruby surface.
func asCases(t *testing.T, cases []struct{ src, want string }) {
	t.Helper()
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

func TestActiveSupportInflector(t *testing.T) {
	asCases(t, []struct{ src, want string }{
		{`require "active_support"; p ActiveSupport::Inflector.pluralize("box")`, "\"boxes\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.singularize("boxes")`, "\"box\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.camelize("foo_bar")`, "\"FooBar\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.camelize("foo_bar", :lower)`, "\"fooBar\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.camelize("foo_bar", true)`, "\"FooBar\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.underscore("FooBar")`, "\"foo_bar\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.humanize("employee_salary")`, "\"Employee salary\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.titleize("man from the boondocks")`, "\"Man From The Boondocks\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.tableize("RawScaledScorer")`, "\"raw_scaled_scorers\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.classify("posts")`, "\"Post\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.dasherize("puni_puni")`, "\"puni-puni\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.demodulize("ActiveSupport::Inflector::Inflections")`, "\"Inflections\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.deconstantize("Net::HTTP")`, "\"Net\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.foreign_key("Message")`, "\"message_id\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.ordinal(1)`, "\"st\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.ordinalize(4)`, "\"4th\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.parameterize("Donald E. Knuth")`, "\"donald-e-knuth\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.transliterate("Ærøskøbing")`, "\"AEroskobing\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.transliterate("Ærø", "*")`, "\"AEro\"\n"},
		// constantize resolves a namespaced constant; safe_constantize never raises.
		{`require "active_support"; p ActiveSupport::Inflector.constantize("ActiveSupport::Inflector").name`, "\"ActiveSupport::Inflector\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.safe_constantize("Nope::Nope")`, "nil\n"},
		{`require "active_support"; p ActiveSupport::Inflector.safe_constantize("ActiveSupport::Inflector").name`, "\"ActiveSupport::Inflector\"\n"},
		// asResolver: an unresolvable middle segment (non-class) and an ancestor miss both fail safely.
		{`require "active_support"; p ActiveSupport::Inflector.safe_constantize("RUBY_VERSION::X")`, "nil\n"},
		{`require "active_support"; p ActiveSupport::Inflector.safe_constantize("ActiveSupport::Nope")`, "nil\n"},
	})
}

func TestActiveSupportInflectionsDSL(t *testing.T) {
	dsl := `require "active_support"
ActiveSupport::Inflector.inflections do |inflect|
  inflect.plural(/(quiz)$/i, '\1zes')
  inflect.singular(/(quiz)zes$/i, '\1')
  inflect.irregular("cow", "kine")
  inflect.uncountable("fish", "sheep")
  inflect.uncountable(%w[rice equipment])
  inflect.acronym("RESTful")
  inflect.human(/(.*)_cnt$/i, '\1_count')
  inflect.plural("ox", "oxen2")
end
`
	asCases(t, []struct{ src, want string }{
		{dsl + `p "quiz".pluralize`, "\"quizzes\"\n"},
		{dsl + `p ActiveSupport::Inflector.singularize("quizzes")`, "\"quiz\"\n"},
		{dsl + `p "cow".pluralize`, "\"kine\"\n"},
		{dsl + `p "fish".pluralize`, "\"fish\"\n"},
		{dsl + `p "rice".pluralize`, "\"rice\"\n"},
		{dsl + `p ActiveSupport::Inflector.camelize("restful_api")`, "\"RESTfulApi\"\n"},
		{dsl + `p ActiveSupport::Inflector.humanize("jobs_cnt")`, "\"Jobs count\"\n"},
		{dsl + `p "box".pluralize`, "\"boxen2\"\n"},
		// inflections with no block returns the ruleset object (its class, to_s, truthiness).
		{`require "active_support"; p ActiveSupport::Inflector.inflections.class.name`, "\"ActiveSupport::Inflector::Inflections\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.inflections.to_s`, "\"#<ActiveSupport::Inflector::Inflections>\"\n"},
		{`require "active_support"; p(ActiveSupport::Inflector.inflections ? "y" : "n")`, "\"y\"\n"},
		{`require "active_support"; p ActiveSupport::Inflector.inflections`, "#<ActiveSupport::Inflector::Inflections>\n"},
	})
}

func TestActiveSupportCoreExtString(t *testing.T) {
	r := `require "active_support/all"; `
	asCases(t, []struct{ src, want string }{
		{r + `p ["".blank?, "x".blank?, " ".present?, "x".present?, "x".presence, "  ".presence]`,
			"[true, false, false, true, \"x\", nil]\n"},
		{r + `p "  a  b   c ".squish`, "\"a b c\"\n"},
		{r + `p "Once upon a time".truncate(10)`, "\"Once up...\"\n"},
		{r + `p "product".pluralize`, "\"products\"\n"},
		{r + `p "ProductLine".underscore`, "\"product_line\"\n"},
		{r + `p "product_line".camelize`, "\"ProductLine\"\n"},
		{r + `p "product_line".camelize(:lower)`, "\"productLine\"\n"},
		{r + `p "raw_scaled_scorers".titleize`, "\"Raw Scaled Scorers\"\n"},
		{r + `p "Donald E. Knuth".parameterize`, "\"donald-e-knuth\"\n"},
		{r + `p "posts".classify`, "\"Post\"\n"},
		{r + `p "employee_salary".humanize`, "\"Employee salary\"\n"},
		{r + `p ["hello".starts_with?("he"), "hello".ends_with?("lo")]`, "[true, true]\n"},
	})
}

func TestActiveSupportCoreExtArray(t *testing.T) {
	r := `require "active_support/all"; `
	asCases(t, []struct{ src, want string }{
		{r + `p [[].blank?, [1].blank?]`, "[true, false]\n"},
		{r + `p [1,2,3,4,5,6,7].in_groups(3)`, "[[1, 2, 3], [4, 5, nil], [6, 7, nil]]\n"},
		{r + `p [1,2,3,4,5,6,7].in_groups(3, false)`, "[[1, 2, 3], [4, 5], [6, 7]]\n"},
		{r + `p [1,2,3,4,5,6,7].in_groups(3, "x")`, "[[1, 2, 3], [4, 5, \"x\"], [6, 7, \"x\"]]\n"},
		{r + `p [1,2,3,4,5,6,7].in_groups_of(3)`, "[[1, 2, 3], [4, 5, 6], [7, nil, nil]]\n"},
		{r + `p [1,2,3,4,5,6,7].in_groups_of(3, false)`, "[[1, 2, 3], [4, 5, 6], [7]]\n"},
		{r + `p ["a","b","c"].to_sentence`, "\"a, b, and c\"\n"},
		{r + `p [1,2,3].to_sentence`, "\"1, 2, and 3\"\n"},
		{r + `p [[1,2,3,4,5].second, [1,2,3,4,5].third, [1,2,3,4,5].fourth, [1,2,3,4,5].fifth]`, "[2, 3, 4, 5]\n"},
	})
}

func TestActiveSupportCoreExtHash(t *testing.T) {
	r := `require "active_support/all"; `
	asCases(t, []struct{ src, want string }{
		{r + `p [{}.blank?, {a: 1}.blank?]`, "[true, false]\n"},
		{r + `p({a: 1, b: 2}.deep_merge({b: {x: 1}, c: 3}))`, "{a: 1, b: {x: 1}, c: 3}\n"},
		{r + `p({a: 1, b: {y: 2}}.deep_merge({b: {z: 3}}))`, "{a: 1, b: {y: 2, z: 3}}\n"},
		{r + `p({"a" => 1, b: 2, 3 => 4}.symbolize_keys)`, "{a: 1, b: 2, 3 => 4}\n"},
		{r + `p({"a" => 1, b: 2, 3 => 4}.stringify_keys)`, "{\"a\" => 1, \"b\" => 2, \"3\" => 4}\n"},
		{r + `p({a: 1, b: 2}.reverse_merge({b: 9, c: 3}))`, "{b: 2, c: 3, a: 1}\n"},
		{r + `p({a: 1, b: {x: 1}, c: [1,2]}.deep_dup)`, "{a: 1, b: {x: 1}, c: [1, 2]}\n"},
		// deep_dup over every native value kind (nil/bool/string/symbol/int/float/array/nested hash/pass-through range).
		{r + `p({a: nil, b: true, c: "s", d: :sym, e: 1, f: 1.5, g: [1], h: {"k" => 1, 2 => 3}, i: (1..3)}.deep_dup)`,
			"{a: nil, b: true, c: \"s\", d: :sym, e: 1, f: 1.5, g: [1], h: {2 => 3, \"k\" => 1}, i: 1..3}\n"},
	})
}

func TestActiveSupportCoreExtIntegerObject(t *testing.T) {
	r := `require "active_support/all"; `
	asCases(t, []struct{ src, want string }{
		{r + `p [4.ordinalize, 3.multiple_of?(1), 4.multiple_of?(3)]`, "[\"4th\", true, false]\n"},
		{r + `p [nil.blank?, false.blank?, true.blank?, 0.blank?, Object.new.blank?]`, "[true, true, false, false, false]\n"},
		{r + `p [nil.present?, 5.present?]`, "[false, true]\n"},
		{r + `p [nil.presence, 5.presence]`, "[nil, 5]\n"},
		// blank? via the Blankable seam: an object responding to empty?.
		{r + `class E1; def empty?; true; end; end; class E2; def empty?; false; end; end; p [E1.new.blank?, E2.new.blank?]`, "[true, false]\n"},
		// try: nil-safe navigation, block form, no-arg form, unknown method, forwarded args, forwarded block.
		{r + `p nil.try(:upcase)`, "nil\n"},
		{r + `p "hi".try(:upcase)`, "\"HI\"\n"},
		{r + `p "hi".try("upcase")`, "\"HI\"\n"},
		{r + `p 5.try`, "nil\n"},
		{r + `p 5.try { |x| x + 1 }`, "6\n"},
		{r + `p nil.try { |x| x + 1 }`, "nil\n"},
		{r + `p "hi".try(:nonexistent_method)`, "nil\n"},
		{r + `p "hello".try(:sub, "h", "j")`, "\"jello\"\n"},
		{r + `p [1,2].try(:map) { |x| x * 2 }`, "[2, 4]\n"},
	})
}

func TestActiveSupportEnumerable(t *testing.T) {
	r := `require "active_support/all"; `
	asCases(t, []struct{ src, want string }{
		{r + `p ["a","bb","cc"].index_by { |s| s.length }`, "{1 => \"a\", 2 => \"cc\"}\n"},
		{r + `p (1..3).index_by { |x| x * 10 }`, "{10 => 1, 20 => 2, 30 => 3}\n"},
		{r + `p [[1].many?, [1,2].many?, [1,2,3].many? { |x| x > 1 }]`, "[false, true, true]\n"},
		{r + `p [[1,2,3].exclude?(2), [1,2,3].exclude?(9)]`, "[false, true]\n"},
		{r + `p [{id: 1, n: "a"}, {id: 2, n: "b"}].pluck(:id)`, "[1, 2]\n"},
		{r + `p [{id: 1, n: "a"}, {id: 2, n: "b"}].pluck(:id, :n)`, "[[1, \"a\"], [2, \"b\"]]\n"},
		{r + `p [{id: 1}, {id: 2}].pick(:id)`, "1\n"},
		{r + `p [].pick(:id)`, "nil\n"},
		{r + `p [{id: 1, n: "a"}].pick(:id, :n)`, "[1, \"a\"]\n"},
		{r + `p [].pick(:id, :n)`, "nil\n"},
	})
}

func TestActiveSupportErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "active_support/all"; ActiveSupport::Inflector.constantize("Nope::Missing")`, "uninitialized constant"},
		{`require "active_support/all"; ActiveSupport::Inflector.constantize("")`, "uninitialized constant"},
		{`require "active_support/all"; {a: 1}.deep_merge(5)`, "into Hash"},
		{`require "active_support/all"; {a: 1}.reverse_merge(5)`, "into Hash"},
		{`require "active_support"; ActiveSupport::Inflector.inflections { |i| i.plural(123, "x") }`, "expected Regexp"},
		{`require "active_support/all"; 5.try(123)`, "is not a symbol nor a string"},
		{`require "active_support/all"; [1,2].index_by`, "ArgumentError"},
	}
	for _, c := range cases {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q: got err=%v, want containing %q", c.src, err, c.want)
		}
	}
}

// TestActiveSupportRequire verifies the provided-feature contract: the first
// require reports true, a second reports false, and the aliases all load.
func TestActiveSupportRequire(t *testing.T) {
	asCases(t, []struct{ src, want string }{
		{`p require "active_support"`, "true\n"},
		{`p require "active_support/all"`, "true\n"},
		{`require "active_support"; p require "active_support"`, "false\n"},
		{`p require "active_support/core_ext/string"`, "true\n"},
	})
}
