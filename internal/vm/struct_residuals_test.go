package vm_test

import (
	"strings"
	"testing"
)

// TestStructDataResiduals locks in the Struct/Data conformance residuals fixed in
// this change: keyword_init-unset auto-detect, #each_pair pair yielding, #dig on a
// missing member, #to_h block coercion, #inspect naming for anonymous-namespace
// classes, and the class-only hash of a recursive struct. Every expectation is
// byte-for-byte against MRI Ruby 4.0.
func TestStructDataResiduals(t *testing.T) {
	cases := []struct{ src, want string }{
		// Struct#initialize is private (MRI).
		{`S = Struct.new(:a); p S.private_instance_methods(true).include?(:initialize)`, "true\n"},

		// #each_pair yields ONE [name, value] Array per pair, so an Enumerator
		// consumer (to_a / single-parameter map) gets the pair whole.
		{`S = Struct.new(:a, :b); p S.new(1, 2).each_pair.to_a`, "[[:a, 1], [:b, 2]]\n"},
		{`S = Struct.new(:a, :b); p S.new(1, 2).each_pair.map { |v| v }`, "[[:a, 1], [:b, 2]]\n"},

		// #dig with a missing member name returns nil (unlike #[], which raises).
		{`S = Struct.new(:a); p S.new(1).dig(:zzz)`, "nil\n"},
		{`S = Struct.new(:a); p S.new(1).dig("zzz")`, "nil\n"},

		// keyword_init unset auto-detect (Ruby 3.2): a lone Hash whose keys all name
		// members is keyword init; any non-member or non-Symbol key keeps it
		// positional (matching MRI for an explicit-brace hash).
		{`S = Struct.new(:a, :b); p S.new(a: 1, b: 2).to_a`, "[1, 2]\n"},
		{`S = Struct.new(:a, :b); p S.new(a: 5).to_a`, "[5, nil]\n"},
		{`S = Struct.new(:a); p S.new({b: 1}).a`, "{b: 1}\n"},
		{`S = Struct.new(:a); p S.new({0 => 1}).a`, "{0 => 1}\n"},
		{`S = Struct.new(:a); p S.new({}).a`, "{}\n"},

		// #to_h block coerces the returned pair with #to_ary (never #to_a).
		{`S = Struct.new(:a); class Ary1; def to_ary; [:k, 9]; end; end; p S.new(1).to_h { |k, v| Ary1.new }`, "{k: 9}\n"},

		// #to_s / #inspect: name shown only for a permanent classpath — omitted for
		// an anonymous class or one nested in an anonymous namespace.
		{`puts Struct.new(:a).new("").to_s`, "#<struct a=\"\">\n"},
		{`S = Struct.new(:a); puts S.new("x").to_s`, "#<struct S a=\"x\">\n"},
		{`module NS1; B = Struct.new(:a); end; puts NS1::B.new("z").to_s`, "#<struct NS1::B a=\"z\">\n"},
		{`m = Module.new; m.module_eval "Foo = Struct.new(:a)"; puts m::Foo.new("").to_s`, "#<struct a=\"\">\n"},

		// Recursive struct hashes collapse to a class-only value (so #eql? recursive
		// structs hash alike); non-recursive nested structs still fold member hashes.
		{`R = Struct.new(:a, :b); x = R.new(1, 2); y = R.new(1, 2); x[:a] = x; y[:a] = x; p x.hash == y.hash`, "true\n"},
		{`R = Struct.new(:a); o1 = R.new(R.new(1)); o2 = R.new(R.new(2)); p o1.hash == o2.hash`, "false\n"},

		// Data#to_s naming and recursive sentinel.
		{`M = Data.define(:x); puts M.new(x: 1).to_s`, "#<data M x=1>\n"},
		{`M = Data.define(:x); k = Class.new(M); puts k.new(x: 1).to_s`, "#<data x=1>\n"},
		{`M = Data.define(:x); a = M.allocate; a.send(:initialize, x: a); puts a.to_s`, "#<data M x=#<data M:...>>\n"},
		// Recursive anonymous Data renders the member class as #<Class:0x…>.
		{`M = Data.define(:x); k = Class.new(M); b = k.allocate; b.send(:initialize, x: b); p !!(b.to_s =~ /\A#<data x=#<data #<Class:0x[0-9a-f]+>:\.\.\.>>\z/)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got %q\nwant %q", c.src, got, c.want)
		}
	}
}

// TestStructDataResidualErrors covers the TypeError paths whose message wording or
// class-naming this change corrected.
func TestStructDataResidualErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		// deconstruct_keys of a non-Array, non-nil argument names "Array or nil".
		{`Struct.new(:a).new(1).deconstruct_keys("x")`, "wrong argument type String (expected Array or nil)"},
		// A #to_int that returns a non-Integer names the key's real class.
		{`class KK2; def to_int; "x"; end; end; Struct.new(:a, :b).new(1, 2).deconstruct_keys([KK2.new])`, "can't convert KK2 into Integer"},
		// #to_h block pair that is neither Array nor #to_ary-able names the real class.
		{`Struct.new(:a).new(1).to_h { |k, v| Object.new }`, "wrong element type Object (expected array)"},
		{`Data.define(:a).new(a: 1).to_h { |k, v| Object.new }`, "wrong element type Object (expected array)"},
	}
	for _, c := range cases {
		err := runErr(t, c.src)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q: got err=%v, want containing %q", c.src, err, c.want)
		}
	}
}
