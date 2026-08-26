package vm_test

import (
	"strings"
	"testing"
)

// TestMarshal covers the Marshal module (dump/load/restore + version constants),
// asserted against MRI Ruby 4.0.5. Marshal is a core module (no require). The
// byte-exact expectations were captured with `ruby -e 'p Marshal.dump(x).bytes'`.
func TestMarshal(t *testing.T) {
	cases := []struct{ src, want string }{
		// --- byte-exact dumps vs MRI ---------------------------------------
		{`p Marshal.dump(nil).bytes`, "[4, 8, 48]\n"},
		{`p Marshal.dump(true).bytes`, "[4, 8, 84]\n"},
		{`p Marshal.dump(false).bytes`, "[4, 8, 70]\n"},
		{`p Marshal.dump(0).bytes`, "[4, 8, 105, 0]\n"},
		{`p Marshal.dump(123).bytes`, "[4, 8, 105, 1, 123]\n"},
		{`p Marshal.dump(-124).bytes`, "[4, 8, 105, 255, 132]\n"},
		{`p Marshal.dump(256).bytes`, "[4, 8, 105, 2, 0, 1]\n"},
		{`p Marshal.dump(2**30).bytes`, "[4, 8, 108, 43, 7, 0, 0, 0, 64]\n"},
		{`p Marshal.dump(-(2**70)).bytes`, "[4, 8, 108, 45, 10, 0, 0, 0, 0, 0, 0, 0, 0, 64, 0]\n"},
		{`p Marshal.dump(3.14).bytes`, "[4, 8, 102, 9, 51, 46, 49, 52]\n"},
		{`p Marshal.dump(100.0).bytes`, "[4, 8, 102, 8, 49, 101, 50]\n"},
		{`p Marshal.dump(0.0).bytes`, "[4, 8, 102, 6, 48]\n"},
		{`p Marshal.dump(-0.0).bytes`, "[4, 8, 102, 7, 45, 48]\n"},
		{`p Marshal.dump(1.0/0).bytes`, "[4, 8, 102, 8, 105, 110, 102]\n"},
		{`p Marshal.dump(-1.0/0).bytes`, "[4, 8, 102, 9, 45, 105, 110, 102]\n"},
		{`p Marshal.dump(0.0/0).bytes`, "[4, 8, 102, 8, 110, 97, 110]\n"},
		{`p Marshal.dump(:sym).bytes`, "[4, 8, 58, 8, 115, 121, 109]\n"},
		{`p Marshal.dump([:a, :a]).bytes`, "[4, 8, 91, 7, 58, 6, 97, 59, 0]\n"},
		{`p Marshal.dump("héllo").bytes`, "[4, 8, 73, 34, 11, 104, 195, 169, 108, 108, 111, 6, 58, 6, 69, 84]\n"},
		{`p Marshal.dump("abc".b).bytes`, "[4, 8, 34, 8, 97, 98, 99]\n"},
		{`p Marshal.dump("abc".encode("US-ASCII")).bytes`, "[4, 8, 73, 34, 8, 97, 98, 99, 6, 58, 6, 69, 70]\n"},
		{`p Marshal.dump([1, 2, 3]).bytes`, "[4, 8, 91, 8, 105, 6, 105, 7, 105, 8]\n"},
		{`p Marshal.dump({"a" => 1}).bytes`, "[4, 8, 123, 6, 73, 34, 6, 97, 6, 58, 6, 69, 84, 105, 6]\n"},
		{`p Marshal.dump(Hash.new(0)).bytes`, "[4, 8, 125, 0, 105, 0]\n"},
		{`p Marshal.dump(1..5).bytes`, "[4, 8, 111, 58, 10, 82, 97, 110, 103, 101, 8, 58, 9, 101, 120, 99, 108, 70, 58, 10, 98, 101, 103, 105, 110, 105, 6, 58, 8, 101, 110, 100, 105, 10]\n"},
		{`p Marshal.dump(/ab/i).bytes`, "[4, 8, 73, 47, 7, 97, 98, 1, 6, 58, 6, 69, 70]\n"},
		{`p Marshal.dump(/x/).bytes`, "[4, 8, 73, 47, 6, 120, 0, 6, 58, 6, 69, 70]\n"},
		{`p Marshal.dump(/café/).bytes`, "[4, 8, 73, 47, 10, 99, 97, 102, 195, 169, 16, 6, 58, 6, 69, 84]\n"},
		{`p Marshal.dump(Complex(1, 2)).bytes`, "[4, 8, 85, 58, 12, 67, 111, 109, 112, 108, 101, 120, 91, 7, 105, 6, 105, 7]\n"},
		{`p Marshal.dump(Rational(1, 3)).bytes`, "[4, 8, 85, 58, 13, 82, 97, 116, 105, 111, 110, 97, 108, 91, 7, 105, 6, 105, 8]\n"},
		{`p Marshal.dump(Time.at(0).utc).bytes`, "[4, 8, 73, 117, 58, 9, 84, 105, 109, 101, 13, 32, 128, 17, 192, 0, 0, 0, 0, 6, 58, 9, 122, 111, 110, 101, 73, 34, 8, 85, 84, 67, 6, 58, 6, 69, 70]\n"},
		{`p Marshal.dump(Time.at(1234567890).utc).bytes`, "[4, 8, 73, 117, 58, 9, 84, 105, 109, 101, 13, 183, 69, 27, 192, 0, 0, 224, 125, 6, 58, 9, 122, 111, 110, 101, 73, 34, 8, 85, 84, 67, 6, 58, 6, 69, 70]\n"},
		{`p Marshal.dump(String).bytes`, "[4, 8, 99, 11, 83, 116, 114, 105, 110, 103]\n"},
		{`p Marshal.dump(Comparable).bytes`, "[4, 8, 109, 15, 67, 111, 109, 112, 97, 114, 97, 98, 108, 101]\n"},
		{`class MPoint; def initialize; @x = 1; @y = 2; end; end; p Marshal.dump(MPoint.new).bytes`,
			"[4, 8, 111, 58, 11, 77, 80, 111, 105, 110, 116, 7, 58, 7, 64, 120, 105, 6, 58, 7, 64, 121, 105, 7]\n"},
		{`MS = Struct.new(:a, :b); p Marshal.dump(MS.new(1, 2)).bytes`,
			"[4, 8, 83, 58, 7, 77, 83, 7, 58, 6, 97, 105, 6, 58, 6, 98, 105, 7]\n"},
		{`class MHook; def initialize(v); @v = v; end; def marshal_dump; @v; end; def marshal_load(v); @v = v; end; end; p Marshal.dump(MHook.new([1, 2])).bytes`,
			"[4, 8, 85, 58, 10, 77, 72, 111, 111, 107, 91, 7, 105, 6, 105, 7]\n"},

		// --- round-trips across every value type ---------------------------
		{`p Marshal.load(Marshal.dump(123)) == 123`, "true\n"},
		{`p Marshal.load(Marshal.dump(-1000000000)) == -1000000000`, "true\n"},
		{`p Marshal.load(Marshal.dump(2**70)) == 2**70`, "true\n"},
		{`p Marshal.load(Marshal.dump(-(2**70))) == -(2**70)`, "true\n"},
		{`p Marshal.load(Marshal.dump(3.14)) == 3.14`, "true\n"},
		{`p Marshal.load(Marshal.dump(1.0/0)).infinite?`, "1\n"},
		{`p Marshal.load(Marshal.dump(0.0/0)).nan?`, "true\n"},
		{`p Marshal.load(Marshal.dump("héllo")) == "héllo"`, "true\n"},
		{`p Marshal.load(Marshal.dump("héllo")).encoding.name`, "\"UTF-8\"\n"},
		{`p Marshal.load(Marshal.dump("abc".b)).encoding.name`, "\"ASCII-8BIT\"\n"},
		{`p Marshal.load(Marshal.dump("abc".encode("US-ASCII"))).encoding.name`, "\"US-ASCII\"\n"},
		{`p Marshal.load(Marshal.dump(:sym)) == :sym`, "true\n"},
		{`p Marshal.load(Marshal.dump(true))`, "true\n"},
		{`p Marshal.load(Marshal.dump(false))`, "false\n"},
		{`p Marshal.load(Marshal.dump(nil)).nil?`, "true\n"},
		{`p Marshal.load(Marshal.dump([1, [2, 3], "x"])) == [1, [2, 3], "x"]`, "true\n"},
		{`p Marshal.load(Marshal.dump({"a" => 1, "b" => [true, nil]})) == {"a" => 1, "b" => [true, nil]}`, "true\n"},
		{`p Marshal.load(Marshal.dump({1 => 2, 3 => 4})) == {1 => 2, 3 => 4}`, "true\n"},
		{`p Marshal.load(Marshal.dump(Hash.new(0)))[:missing]`, "0\n"},
		{`r = Marshal.load(Marshal.dump(1...5)); p [r.begin, r.end, r.exclude_end?]`, "[1, 5, true]\n"},
		{`r = Marshal.load(Marshal.dump("a".."z")); p [r.begin, r.end, r.exclude_end?]`, "[\"a\", \"z\", false]\n"},
		{`r = Marshal.load(Marshal.dump(/ab/mix)); p [r.source, r.options]`, "[\"ab\", 7]\n"},
		{`p Marshal.load(Marshal.dump(/café/)) == /café/`, "true\n"},
		{`p Marshal.load(Marshal.dump(Complex(3, 4))) == Complex(3, 4)`, "true\n"},
		{`p Marshal.load(Marshal.dump(Rational(2, 4))) == Rational(1, 2)`, "true\n"},
		{`p Marshal.load(Marshal.dump(Time.at(1234567890).utc)) == Time.at(1234567890).utc`, "true\n"},
		{`p Marshal.load(Marshal.dump(String)) == String`, "true\n"},
		{`p Marshal.load(Marshal.dump(Comparable)) == Comparable`, "true\n"},
		{`class MP2; def initialize; @x = 7; end; def x; @x; end; end; p Marshal.load(Marshal.dump(MP2.new)).x`, "7\n"},
		{`MS2 = Struct.new(:a, :b); v = Marshal.load(Marshal.dump(MS2.new(4, 5))); p [v.a, v.b]`, "[4, 5]\n"},
		{`class MH2; def initialize(v); @v = v; end; def marshal_dump; @v; end; def marshal_load(v); @v = v; end; def v; @v; end; end; p Marshal.load(Marshal.dump(MH2.new([9, 8]))).v`, "[9, 8]\n"},

		// --- class _dump / _load protocol ----------------------------------
		{`class MDump; def initialize(n); @n = n; end; def _dump(l); @n.to_s; end; def self._load(s); new(s.to_i); end; def n; @n; end; end; p Marshal.load(Marshal.dump(MDump.new(42))).n`, "42\n"},

		// --- object identity: shared elements stay shared ------------------
		{`a = [1]; c = Marshal.load(Marshal.dump([a, a])); p c[0].equal?(c[1])`, "true\n"},
		{`s = "x"; c = Marshal.load(Marshal.dump([s, s])); p c[0].equal?(c[1])`, "true\n"},
		{`h = {1 => 2}; c = Marshal.load(Marshal.dump([h, h])); p c[0].equal?(c[1])`, "true\n"},
		{`a = []; a << a; b = Marshal.load(Marshal.dump(a)); p b[0].equal?(b)`, "true\n"},

		// --- freeze: kwarg + load proc -------------------------------------
		{`p Marshal.load(Marshal.dump("x"), freeze: true).frozen?`, "true\n"},
		{`class MFrz; end; p Marshal.load(Marshal.dump(MFrz.new), freeze: true).frozen?`, "true\n"},
		{`seen = []; Marshal.load(Marshal.dump([1, 2]), proc { |o| seen << o }); p seen`, "[1, 2, [1, 2]]\n"},
		{`seen = []; Marshal.load(Marshal.dump([7])) { |o| seen << o.class }; p seen`, "[Integer, Array]\n"},

		// --- IO source / destination ---------------------------------------
		{`require "stringio"; io = StringIO.new; Marshal.dump([1, 2], io); io.rewind; p Marshal.load(io)`, "[1, 2]\n"},

		// --- float formatting branches ------------------------------------
		{`p Marshal.dump(-2.5).bytes`, "[4, 8, 102, 9, 45, 50, 46, 53]\n"},
		{`p Marshal.dump(1200.0).bytes`, "[4, 8, 102, 10, 49, 46, 50, 101, 51]\n"},
		{`p Marshal.dump(12.0).bytes`, "[4, 8, 102, 7, 49, 50]\n"},
		{`p Marshal.dump(0.1).bytes`, "[4, 8, 102, 8, 48, 46, 49]\n"},
		{`p Marshal.dump(1e100).bytes`, "[4, 8, 102, 10, 49, 101, 49, 48, 48]\n"},
		{`p Marshal.dump(-1).bytes`, "[4, 8, 105, 250]\n"},
		{`p Marshal.load(Marshal.dump(-123)) == -123`, "true\n"},
		{`p Marshal.load(Marshal.dump(122)) == 122`, "true\n"},
		{`p Marshal.load(Marshal.dump(-1.0/0)).infinite?`, "-1\n"},
		{`p Marshal.load(Marshal.dump(1200.0)) == 1200.0`, "true\n"},

		// --- shared references across the full type set --------------------
		{`r = /x/; c = Marshal.load(Marshal.dump([r, r])); p c[0].equal?(c[1])`, "true\n"},
		{`z = Complex(1, 2); c = Marshal.load(Marshal.dump([z, z])); p c[0].equal?(c[1])`, "true\n"},
		{`z = Rational(1, 2); c = Marshal.load(Marshal.dump([z, z])); p c[0].equal?(c[1])`, "true\n"},
		{`z = 1..2; c = Marshal.load(Marshal.dump([z, z])); p c[0].equal?(c[1])`, "true\n"},
		{`c = Marshal.load(Marshal.dump([String, String])); p c[0].equal?(c[1])`, "true\n"},
		{`t = Time.at(5).utc; c = Marshal.load(Marshal.dump([t, t])); p c[0].equal?(c[1])`, "true\n"},
		{`class MShare; end; o = MShare.new; c = Marshal.load(Marshal.dump([o, o])); p c[0].equal?(c[1])`, "true\n"},

		// --- Bignum-backed Rational, nested class, named encoding ----------
		{`p Marshal.load(Marshal.dump(Rational(2**70, 3))) == Rational(2**70, 3)`, "true\n"},
		{`module MOuter; class MInner; end; end; p Marshal.load(Marshal.dump(MOuter::MInner)) == MOuter::MInner`, "true\n"},
		{`s = "abc".encode("EUC-JP"); p Marshal.load(Marshal.dump(s)).encoding.name`, "\"EUC-JP\"\n"},

		// --- freeze: on a Regexp ------------------------------------------
		{`p Marshal.load(Marshal.dump(/x/), freeze: true).frozen?`, "true\n"},

		// --- Marshal.dump arity: limit / nil / non-IO second argument -----
		{`p Marshal.dump(5, 3).bytes`, "[4, 8, 105, 10]\n"},
		{`p Marshal.dump(5, nil).bytes`, "[4, 8, 105, 10]\n"},
		{`p Marshal.dump(5, Object.new).bytes`, "[4, 8, 105, 10]\n"},

		// --- Marshal.dump always returns an ASCII-8BIT (BINARY) string -----
		{`p Marshal.dump(42).encoding.name`, "\"ASCII-8BIT\"\n"},
		{`p Marshal.dump("hi").encoding.name`, "\"ASCII-8BIT\"\n"},

		// --- 'C': instances of a user subclass of a built-in value type ----
		{`class UA < Array; end; p Marshal.dump(UA.new([1, 2])).bytes`, "[4, 8, 67, 58, 7, 85, 65, 91, 7, 105, 6, 105, 7]\n"},
		{`class UH < Hash; end; p Marshal.dump(UH.new).bytes`, "[4, 8, 67, 58, 7, 85, 72, 123, 0]\n"},
		{`class UA2 < Array; end; a = UA2.new([1]); a.instance_variable_set(:@x, 7); p Marshal.dump(a).bytes`, "[4, 8, 73, 67, 58, 8, 85, 65, 50, 91, 6, 105, 6, 6, 58, 7, 64, 120, 105, 12]\n"},
		{`class UA3 < Array; end; c = Marshal.load(Marshal.dump(UA3.new([4, 5]))); p [c.class.name, c.to_a]`, "[\"UA3\", [4, 5]]\n"},
		{`class UA4 < Array; end; a = UA4.new([1]); a.instance_variable_set(:@y, 9); c = Marshal.load(Marshal.dump(a)); p c.instance_variable_get(:@y)`, "9\n"},
		{`class UA5 < Array; end; o = UA5.new; c = Marshal.load(Marshal.dump([o, o])); p c[0].equal?(c[1])`, "true\n"},

		// --- 'e': a singleton-extended object ------------------------------
		{`module ME; end; o = Object.new; o.extend(ME); p Marshal.dump(o).bytes`, "[4, 8, 101, 58, 7, 77, 69, 111, 58, 11, 79, 98, 106, 101, 99, 116, 0]\n"},
		{`module ME2; def tag; :ok; end; end; o = Object.new; o.extend(ME2); c = Marshal.load(Marshal.dump(o)); p c.tag`, ":ok\n"},
		{`o = Object.new; o.extend(Module.new); begin; Marshal.dump(o); rescue TypeError => e; p e.message.start_with?("can't dump anonymous"); end`, "true\n"},

		// --- version constants + restore alias -----------------------------
		{`p Marshal::MAJOR_VERSION`, "4\n"},
		{`p Marshal::MINOR_VERSION`, "8\n"},
		{`p Marshal.restore(Marshal.dump([1, 2])) == [1, 2]`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`Marshal.load(Marshal.dump(123)[0..2])`, "marshal data too short"},
		{`Marshal.dump(proc { 1 })`, "no _dump_data is defined for class Proc"},
		{`Marshal.dump(Hash.new { |h, k| 0 })`, "default proc"},
		{`Marshal.load(42)`, "IO needed"},
		{`Marshal.load("\x05\bi\x06")`, "format version 4.8 required"},
		{`Marshal.load("\x04\bZ")`, "dump format error"},
		{"Marshal.load(\"\\x04\\bo:\\bFoo\\x00\")", "undefined class/module Foo"},
		{`Marshal.dump`, "wrong number of arguments"},
		{`Marshal.load`, "wrong number of arguments"},
		{`Marshal.dump(Class.new)`, "anonymous class"},
		{"Marshal.load(\"\\x04\\b@\\x06\")", "dump format error"},                // object link out of range
		{"Marshal.load(\"\\x04\\bo0\")", "dump format error"},                    // symbol expected, got tag '0'
		{"Marshal.load(\"\\x04\\bo;\\x06\")", "dump format error"},               // symbol back-ref out of range
		{"Marshal.load(\"\\x04\\b\\\"\\xfa\")", "marshal data too short"},        // negative byte length
		{"Marshal.load(\"\\x04\\bU:\\x06Zi\\x06\")", "undefined class/module Z"}, // unknown UserMarshal class
		{"Marshal.load(\"\\x04\\bU:\\fComplexi\\n\")", "marshal data too short"}, // Complex payload not a 2-array
		{"Marshal.load(\"\\x04\\bu:\\tTime\\x00\")", "marshal data too short"},   // Time payload shorter than 8 bytes
		{"Marshal.load(\"\\x04\\bS:\\vStringi\\x06\")", "is not a Struct"},       // Struct class isn't a Struct
		{`class BadD; def _dump(l); 5; end; end; Marshal.dump(BadD.new)`, "_dump() must return String"},
		{`Marshal.dump(Class.new.new)`, "anonymous class"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
		}
	}
}
