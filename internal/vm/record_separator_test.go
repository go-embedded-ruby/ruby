package vm

import "testing"

// Ruby 4.0 stops storing the object assigned to $/ and $-0
// (io.c v4.0.9 deprecated_rs_setter, language/predefined_spec.rb:698-722).
//
// Every expectation below was measured on a real
// `ruby 4.0.7 (2026-09-15 revision 229531a6cf) [arm64-darwin25]` before it was
// written here, because two of them are not in ruby/spec at all.
func TestRecordSeparatorAssignment(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"an unfrozen String is replaced by a frozen copy",
			`$VERBOSE = nil; s = +"abc"; $/ = s; p [$/.equal?(s), $/.frozen?, $/ == s]`,
			"[false, true, true]",
		},
		{
			"a frozen String with no instance variables is kept",
			`$VERBOSE = nil; s = "abc".freeze; $/ = s; p $/.equal?(s)`,
			"true",
		},

		// ⛔ NOT IN ruby/spec. The equality branch runs BEFORE the "frozen bare
		// string is kept" rule, so a frozen plain "\n" — which satisfies that
		// rule word for word — is still replaced, by the canonical separator.
		// Every example in predefined_spec.rb uses "abc" and so can only reach
		// the other branch; proposed upstream as ruby/spec#1410.
		{
			"a String equal to the default becomes the default object",
			`$VERBOSE = nil; d = $/; s = "\n".dup.freeze; $/ = s; p [$/.equal?(s), $/.equal?(d)]`,
			"[false, true]",
		},
		{
			"and so does an unfrozen one",
			`$VERBOSE = nil; d = $/; $/ = +"\n"; p $/.equal?(d)`,
			"true",
		},

		// $-0 shares the slot, so it takes the same setter.
		{
			"$-0 is replaced the same way",
			`$VERBOSE = nil; s = +"abc"; $-0 = s; p [$-0.equal?(s), $-0.frozen?]`,
			"[false, true]",
		},
		{
			"and still reads as the same object as $/",
			`$VERBOSE = nil; $/ = +"xyz"; p $-0.equal?($/)`,
			"true",
		},

		// ⛔ CONTROLS: $\ and $, take rb_deprecated_str_setter, NOT this one
		// (io.c v4.0.9 Init_IO). Carrying the freezing into them would be a
		// silent divergence in the opposite direction, and nothing else in the
		// suite asserts it.
		{
			`$\ keeps the object it is given`,
			`$VERBOSE = nil; s = +"abc"; $\ = s; p [$\.equal?(s), $\.frozen?]`,
			"[true, false]",
		},
		{
			`$, keeps the object it is given`,
			`$VERBOSE = nil; s = +"abc"; $, = s; p [$,.equal?(s), $,.frozen?]`,
			"[true, false]",
		},

		// Unchanged by this: nil, the TypeError, and the returned value.
		{"nil is accepted", `$/ = nil; p $/`, "nil"},
		{
			"a non-String is still refused without #to_str",
			`begin; $/ = 1; rescue TypeError => e; p e.message; end`,
			`"value of $/ must be String"`,
		},
		{
			"assignment returns the value given, not the value stored",
			`$VERBOSE = nil; s = +"xyz"; p((($/ = s)).equal?(s))`,
			"true",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %q\nwant %q", tc.src, got, tc.want)
			}
		})
	}
}
