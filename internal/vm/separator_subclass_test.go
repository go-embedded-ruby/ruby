// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// ⛔ Every separator global refused an instance of a String SUBCLASS. The test
// was `v.(*object.String)`, an exact-type assertion, where MRI's are
// RB_TYPE_P(val, T_STRING) (rb_str_setter) and rb_check_string_type
// (rb_fs_check) — both true for a subclass. rbgo represents such an instance as
// an RObject wrapping a String, so the assertion missed every one of them.
//
// The five globals do NOT all do the same thing with it afterwards, and that is
// the point of the table: $, $\ and $; keep the object as handed over, while $/
// and $-0 replace it with a frozen plain String. Every row was measured against
// MRI 4.0.7 on this host.
func TestSeparatorGlobalsAcceptAStringSubclass(t *testing.T) {
	const mk = `
sc = Class.new(String)
str = sc.new("abc")
str.instance_variable_set(:@ivar, 1)
`
	for _, tc := range []struct{ name, src, want string }{
		// $/ and $-0: deprecated_rs_setter makes a frozen BARE String, so the
		// class becomes String and the ivar is gone. These two rows are
		// language/predefined_spec.rb's "makes a new frozen String from the
		// assigned String", one per global.
		{"$/ converts a subclass to a frozen bare String",
			mk + `$/ = str
p [$/.instance_of?(String), $/.frozen?, $/.instance_variable_defined?(:@ivar), $/ == str]`,
			`[true, true, false, true]`},
		{"$-0 converts a subclass to a frozen bare String",
			mk + `$-0 = str
p [$-0.instance_of?(String), $-0.frozen?, $-0.instance_variable_defined?(:@ivar), $-0 == str]`,
			`[true, true, false, true]`},

		// ⛔ $, $\ and $; must NOT convert: rb_str_setter stores what it was
		// handed. A fix that froze-and-copied everywhere would pass the two
		// rows above and silently rewrite these three.
		{"$, keeps the subclass object",
			mk + `$, = str
p [$,.equal?(str), $,.frozen?]`,
			`[true, false]`},
		{"$\\ keeps the subclass object",
			mk + `$\ = str
p [$\.equal?(str), $\.frozen?]`,
			`[true, false]`},
		{"$; keeps the subclass object",
			mk + `$; = str
p [$;.equal?(str), $;.frozen?]`,
			`[true, false]`},

		// ⛔ $; went down a DIFFERENT wrong path from the others: not a
		// TypeError but #to_str, which a String subclass answers — so the
		// assignment succeeded and quietly replaced the object with a plain
		// String. A wrong answer, where the others at least refused.
		{"$; no longer silently converts through #to_str",
			mk + `$; = str
p $;.class.equal?(sc)`,
			`true`},

		// Controls: the refusals that must survive.
		{"$/ still refuses a non-String",
			`begin; $/ = 1; rescue TypeError => e; p e.message; end`,
			`"value of $/ must be String"`},
		{"$, still refuses a non-String",
			`begin; $, = 1; rescue TypeError => e; p e.message; end`,
			`"value of $, must be String"`},
		{"$; still refuses a non-String non-Regexp",
			`begin; $; = 1; rescue TypeError => e; p e.message; end`,
			`"value of $; must be String or Regexp"`},
		{"$; still accepts a Regexp", `$; = /x/; p $;`, `/x/`},
		{"$; still accepts a #to_str duck",
			`class D; def to_str = "dd"; end
$; = D.new; p $;`, `"dd"`},
		{"nil is still accepted", `$/ = nil; p $/`, `nil`},

		// The frozen-bare short-circuit still applies to an exact String, and
		// the default separator still wins by identity over it.
		{"a frozen bare String is kept by identity",
			`s = "abc".freeze; $/ = s; p $/.equal?(s)`, `true`},
		{"the default separator wins over a frozen bare equal String",
			`was = $/; s = "\n".dup.freeze; $/ = s; p [$/.equal?(s), $/.equal?(was)]`,
			`[false, true]`},
		{"an exact String with an ivar is copied",
			`s = "abc".dup; s.instance_variable_set(:@i, 1); $/ = s
p [$/.equal?(s), $/.frozen?, $/.instance_variable_defined?(:@i)]`,
			`[false, true, false]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %s\nwant %s", tc.src, got, tc.want)
			}
		})
	}
}
