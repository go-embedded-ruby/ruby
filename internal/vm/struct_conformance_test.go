package vm_test

import (
	"strings"
	"testing"
)

// TestStructConformance exercises the MRI 4.0 Struct surface: both Struct.new
// forms, the value/query methods, pattern-matching hooks and inspect formatting.
// Every expectation is asserted against MRI Ruby 4.0.5.
func TestStructConformance(t *testing.T) {
	cases := []struct{ src, want string }{
		// --- Struct.new forms ---------------------------------------------------
		{`S = Struct.new(:a, :b); p S.new(1, 2).a`, "1\n"},
		{`S = Struct.new(:a, :b); p S.ancestors.include?(Enumerable)`, "true\n"},
		{`S = Struct.new(:a); p S.superclass == Struct`, "true\n"},
		{`p Struct.new(:a, :b).name`, "nil\n"},                                // anonymous
		{`S = Struct.new(:a); p S.name`, "\"S\"\n"},                           // named on constant assignment
		{`s = Struct.new(nil, :foo); p [s.name, s.new(9).foo]`, "[nil, 9]\n"}, // nil name slot (local var stays anonymous)
		// String members are accepted and reported as symbols.
		{`S = Struct.new(:a, "b"); p [S.members, S.new(1, 2).b]`, "[[:a, :b], 2]\n"},
		// Named struct: defines a constant under the receiver (Struct::Point).
		{`Struct.new("Point", :x, :y); p [Struct::Point.name, Struct::Point.new(3, 4).x]`, "[\"Struct::Point\", 3]\n"},
		{`k = Struct.new("Point2", :x); p k.equal?(Struct::Point2)`, "true\n"},
		// #to_str on the first argument supplies the name.
		{`o = Object.new; def o.to_str; "Named"; end; Struct.new(o, :a); p Struct::Named.new(5).a`, "5\n"},
		// A block is class-evaluated (self = class) and receives the class.
		{`S = Struct.new(:v) { def dbl; v * 2; end }; p S.new(21).dbl`, "42\n"},
		{`given = nil; S = Struct.new(:a) { |c| given = c }; p S.equal?(given)`, "true\n"},
		{`S = Struct.new(:a) { @seen = :yes }; p S.instance_variable_get(:@seen)`, ":yes\n"},

		// --- construction -------------------------------------------------------
		{`S = Struct.new(:a, :b); p S.new(1).to_a`, "[1, nil]\n"},
		{`S = Struct.new(:a, :b); p S.new.to_a`, "[nil, nil]\n"},
		{`S = Struct.new(:a, :b); p S[7, 8].to_a`, "[7, 8]\n"}, // Struct[] == new
		// keyword_init: true
		{`S = Struct.new(:a, :b, keyword_init: true); p S.new(a: 1, b: 2).to_a`, "[1, 2]\n"},
		{`S = Struct.new(:a, :b, keyword_init: true); p S.new(a: 1).to_a`, "[1, nil]\n"},
		{`S = Struct.new(:a, :b, keyword_init: true); p S.new.to_a`, "[nil, nil]\n"},
		{`S = Struct.new(:a, keyword_init: true); p S.new({a: 5}).a`, "5\n"}, // explicit hash
		// keyword_init: false / nil behave positionally.
		{`S = Struct.new(:a, :b, keyword_init: false); p S.new(1, 2).to_a`, "[1, 2]\n"},
		{`S = Struct.new(:a, keyword_init: nil); p S.new(3).a`, "3\n"},

		// --- members ------------------------------------------------------------
		{`S = Struct.new(:a, :b); p S.members`, "[:a, :b]\n"},
		{`S = Struct.new(:a, :b); p S.new.members`, "[:a, :b]\n"},

		// --- element access -----------------------------------------------------
		{`S = Struct.new(:a, :b); s = S.new(1, 2); p [s[0], s[1], s[-1], s[-2]]`, "[1, 2, 2, 1]\n"},
		{`S = Struct.new(:a, :b); s = S.new(1, 2); p [s[:a], s["b"]]`, "[1, 2]\n"},
		{`S = Struct.new(:a, :b); s = S.new(1, 2); s[0] = 9; s[:b] = 8; s["a"] = 7; p s.to_a`, "[7, 8]\n"},
		{`S = Struct.new(:a, :b); s = S.new(1, 2); s[-1] = 5; p s.b`, "5\n"},

		// --- to_a / values / deconstruct / to_h ---------------------------------
		{`S = Struct.new(:a, :b); p S.new(1, 2).values`, "[1, 2]\n"},
		{`S = Struct.new(:a, :b); p S.new(1, 2).deconstruct`, "[1, 2]\n"},
		{`S = Struct.new(:a, :b); p S.new(1, 2).to_h`, "{a: 1, b: 2}\n"},
		{`S = Struct.new(:a, :b); h = S.new(1, 2).to_h; h[:a] = 9; p S.new(1, 2).to_h`, "{a: 1, b: 2}\n"},
		{`S = Struct.new(:a, :b); p S.new(1, 2).to_h { |k, v| [k.to_s, v * 10] }`, "{\"a\" => 10, \"b\" => 20}\n"},
		{`S = Struct.new(:a); p S.new(1).to_a.frozen?`, "false\n"},

		// --- deconstruct_keys ---------------------------------------------------
		{`S = Struct.new(:x, :y); p S.new(1, 2).deconstruct_keys([:x, :y])`, "{x: 1, y: 2}\n"},
		{`S = Struct.new(:x, :y); p S.new(1, 2).deconstruct_keys(nil)`, "{x: 1, y: 2}\n"},
		{`S = Struct.new(:x, :y); p S.new(1, 2).deconstruct_keys([:x])`, "{x: 1}\n"},
		{`S = Struct.new(:x, :y); p S.new(1, 2).deconstruct_keys(["x", "y"])`, "{\"x\" => 1, \"y\" => 2}\n"},
		{`S = Struct.new(:x, :y, :z); p S.new(1, 2, 3).deconstruct_keys([0, 1, 2])`, "{0 => 1, 1 => 2, 2 => 3}\n"},
		{`S = Struct.new(:x, :y, :z); p S.new(1, 2, 3).deconstruct_keys([-1])`, "{-1 => 3}\n"},
		{`S = Struct.new(:x, :y); p S.new(1, 2).deconstruct_keys([:x, :y, :a])`, "{}\n"}, // more keys than members
		{`S = Struct.new(:x, :y); p S.new(1, 2).deconstruct_keys([:a, :x])`, "{}\n"},     // stop at first missing
		{`S = Struct.new(:x, :y); p S.new(1, 2).deconstruct_keys([:x, :a])`, "{x: 1}\n"},
		{`S = Struct.new(:x, :y); p S.new(1, 2).deconstruct_keys([0, 3])`, "{0 => 1}\n"},
		// #to_int coercion of a key.
		{`S = Struct.new(:x, :y); k = Object.new; def k.to_int; 1; end; h = S.new(1, 2).deconstruct_keys([k]); p h.values`, "[2]\n"},
		// pattern matching drives deconstruct_keys / deconstruct.
		{`S = Struct.new(:x, :y); case S.new(1, 2); in {x:, y:}; p [x, y]; end`, "[1, 2]\n"},
		{`S = Struct.new(:x, :y); case S.new(1, 2); in [a, b]; p [a, b]; end`, "[1, 2]\n"},

		// --- values_at ----------------------------------------------------------
		{`S = Struct.new(:a, :b, :c); p S.new(1, 2, 3).values_at(0, 2)`, "[1, 3]\n"},
		{`S = Struct.new(:a, :b, :c); p S.new(1, 2, 3).values_at(0..2)`, "[1, 2, 3]\n"},
		{`S = Struct.new(:a, :b, :c); p S.new(1, 2, 3).values_at(0..3)`, "[1, 2, 3, nil]\n"}, // fill nil past end
		{`S = Struct.new(:a, :b, :c); p S.new(1, 2, 3).values_at(0..)`, "[1, 2, 3]\n"},       // endless
		{`S = Struct.new(:a, :b, :c); p S.new(1, 2, 3).values_at(..1)`, "[1, 2]\n"},          // beginningless
		{`S = Struct.new(:a, :b, :c); p S.new(1, 2, 3).values_at(0...2)`, "[1, 2]\n"},        // exclusive
		{`S = Struct.new(:a, :b, :c); p S.new(1, 2, 3).values_at(0..2, 1..2)`, "[1, 2, 3, 2, 3]\n"},
		{`S = Struct.new(:a, :b, :c); p S.new(1, 2, 3).values_at`, "[]\n"},
		{`S = Struct.new(:a, :b); p S.new(1, 2).values_at(-1)`, "[2]\n"},
		{`S = Struct.new(:a, :b); p S.new(1, 2).values_at(2..1)`, "[]\n"}, // hi < lo

		// --- dig ----------------------------------------------------------------
		{`S = Struct.new(:a); p S.new(S.new(b: 1)).dig(:a)`, "#<struct S a={b: 1}>\n"},
		{`S = Struct.new(:a); T = Struct.new(:b); p S.new(T.new({z: 5})).dig(:a, :b, :z)`, "5\n"},
		{`S = Struct.new(:a, :b); p S.new(:one, :two).dig(0)`, ":one\n"},
		{`S = Struct.new(:a); T = Struct.new(:b); p S.new(T.new(5)).dig("a", "b")`, "5\n"}, // String keys
		{`S = Struct.new(:a); p S.new(nil).dig(:a, :x)`, "nil\n"},                          // nil intermediate
		{`S = Struct.new(:a); p S.new(1).dig(-1)`, "1\n"},
		{`S = Struct.new(:a); p S.new(1).dig(5)`, "nil\n"}, // index out of range → nil

		// --- size / length ------------------------------------------------------
		{`S = Struct.new(:a, :b, :c); p [S.new.size, S.new.length]`, "[3, 3]\n"},

		// --- each / each_pair ---------------------------------------------------
		{`S = Struct.new(:a, :b); r = []; S.new(1, 2).each { |v| r << v }; p r`, "[1, 2]\n"},
		{`S = Struct.new(:a, :b); r = []; S.new(1, 2).each_pair { |k, v| r << [k, v] }; p r`, "[[:a, 1], [:b, 2]]\n"},
		{`S = Struct.new(:a); p S.new(1).each.class`, "Enumerator\n"},
		{`S = Struct.new(:a); p S.new(1).each_pair.class`, "Enumerator\n"},
		{`S = Struct.new(:a, :b); s = S.new(1, 2); p s.each { |v| }.equal?(s)`, "true\n"},
		{`S = Struct.new(:a, :b); s = S.new(1, 2); p s.each_pair { |v| }.equal?(s)`, "true\n"},
		// Enumerable on top of #each.
		{`S = Struct.new(:a, :b); p S.new(1, 5).select { |v| v > 2 }`, "[5]\n"},
		{`S = Struct.new(:a, :b); p S.new(1, 5).map { |v| v + 1 }`, "[2, 6]\n"},

		// --- == / eql? / hash ---------------------------------------------------
		{`S = Struct.new(:a); p S.new(1) == S.new(1)`, "true\n"},
		{`S = Struct.new(:a); p S.new(1) == S.new(2)`, "false\n"},
		{`S = Struct.new(:a); s = S.new(1); p s == s`, "true\n"},
		{`S = Struct.new(:a); p S.new(1) == 42`, "false\n"},
		{`A = Struct.new(:a); B = Struct.new(:a); p A.new(1) == B.new(1)`, "false\n"},
		{`S = Struct.new(:a); p S.new(1).eql?(S.new(1))`, "true\n"},
		{`S = Struct.new(:a); p S.new(1).eql?(S.new(1.0))`, "false\n"}, // eql? has no numeric coercion
		{`S = Struct.new(:a); p S.new(1).hash == S.new(1).hash`, "true\n"},
		{`S = Struct.new(:a); p S.new(1).hash == S.new(1).dup.hash`, "true\n"},
		{`S = Struct.new(:a); p S.new(1).hash.is_a?(Integer)`, "true\n"},
		{`S = Struct.new(:a); p S.new(1).hash == S.new(2).hash`, "false\n"},
		{`p Struct.new(:x).new(1).hash == Struct.new(:y).new(1).hash`, "false\n"},
		// nested struct members compare/hash recursively.
		{`S = Struct.new(:a); p S.new(S.new(1)) == S.new(S.new(1))`, "true\n"},
		{`S = Struct.new(:a); p S.new(S.new(1)).hash == S.new(S.new(1)).hash`, "true\n"},
		{`S = Struct.new(:a); p S.new(S.new(1)) == S.new(S.new(2))`, "false\n"},
		// mutually-recursive structs terminate.
		{`S = Struct.new(:a, :b); x = S.new(nil, 1); x.a = x; y = S.new(nil, 1); y.a = y; p x == y`, "true\n"},
		// a non-Struct object member: compared/hashed as a plain value (identity).
		{`o = Object.new; S = Struct.new(:a); p S.new(o) == S.new(o)`, "true\n"},
		{`S = Struct.new(:a); p S.new(Object.new) == S.new(Object.new)`, "false\n"},
		{`S = Struct.new(:a); p S.new(Object.new).hash.is_a?(Integer)`, "true\n"},
		// self-recursive struct hashes without looping.
		{`S = Struct.new(:a); x = S.new(nil); x.a = x; p x.hash.is_a?(Integer)`, "true\n"},

		// values_at Range edges: negative end, and an empty (hi < lo) range.
		{`S = Struct.new(:a, :b, :c); p S.new(1, 2, 3).values_at(0..-1)`, "[1, 2, 3]\n"},
		{`S = Struct.new(:a, :b, :c); p S.new(1, 2, 3).values_at(2...1)`, "[]\n"},

		// --- inspect / to_s -----------------------------------------------------
		{`S = Struct.new(:a, :b); p S.new(1, 2).inspect`, "\"#<struct S a=1, b=2>\"\n"},
		{`S = Struct.new(:a, :b); p S.new(1, 2).to_s`, "\"#<struct S a=1, b=2>\"\n"},
		{`p Struct.new(:a).new("").to_s`, "\"#<struct a=\\\"\\\">\"\n"}, // anonymous, no class name
		{`E = Struct.new; p E.new.inspect`, "\"#<struct E>\"\n"},        // no members
		{`S = Struct.new(:a); p S.instance_method(:inspect) == S.instance_method(:to_s)`, "true\n"},
		{`p Struct.instance_method(:deconstruct) == Struct.instance_method(:to_a)`, "true\n"},

		// --- members are not ivars (MRI) ----------------------------------------
		{`S = Struct.new(:a); p S.new(1).instance_variables`, "[]\n"},
		{`S = Struct.new(:a); p S.new(1).instance_variable_get(:@a)`, "nil\n"},
		{`S = Struct.new(:a); s = S.new(1); s.instance_variable_set(:@a, 9); p [s.a, s.instance_variable_get(:@a)]`, "[1, 9]\n"},

		// --- subclassing --------------------------------------------------------
		{`C = Struct.new(:a, :b); class Sub < C; end; p Sub.new(1, 2).to_a`, "[1, 2]\n"},
		{`C = Struct.new(:a, :b); class Sub < C; end; p Sub.new(1, 2).inspect`, "\"#<struct Sub a=1, b=2>\"\n"},
		{`class Base < Struct.new(:a); def initialize(*); @k = 1; super; end; end; p Base.new(3).a`, "3\n"},

		// --- module override wins over Struct's shared method --------------------
		{`m = Module.new { def hash; :custom; end }; S = Struct.new(:a) { include m }; p S.new.hash`, ":custom\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestStructConformanceErrors covers Struct's error branches: argument/type
// errors, index errors and pattern-matching key coercion failures.
func TestStructConformanceErrors(t *testing.T) {
	errs := []struct{ src, want string }{
		// Struct.new member/name validation.
		{`Struct.new(:a, 5)`, "5 is not a symbol nor a string"},
		{`Struct.new(:a, 1.0)`, "1.0 is not a symbol nor a string"},
		{`Struct.new(:a, nil)`, "nil is not a symbol nor a string"},
		{`Struct.new(:a, {name: "x"})`, "is not a symbol nor a string"}, // trailing hash w/o keyword_init
		{`Struct.new(:foo, :foo)`, "duplicate member: foo"},
		{`Struct.new(:foo, :foo, keyword_init: true)`, "duplicate member: foo"},
		{`Struct.new("lower", :a)`, "identifier lower needs to be constant"},
		{`Struct.new("With Space", :a)`, "identifier With Space needs to be constant"},

		// construction errors.
		{`S = Struct.new(:a, :b); S.new(1, 2, 3)`, "struct size differs"},
		{`S = Struct.new(:a, keyword_init: true); S.new(5)`, "wrong number of arguments (given 1, expected 0)"},
		{`S = Struct.new(:a, keyword_init: true); S.new(1, 2)`, "wrong number of arguments (given 2, expected 0)"},
		{`S = Struct.new(:a, keyword_init: true); S.new(a: 1, c: 3)`, "unknown keywords: c"},
		{`S = Struct.new(:a, :b, keyword_init: true); S.new(c: 1, d: 2)`, "unknown keywords: c, d"},
		{`S = Struct.new(:a, keyword_init: true); S.new("a" => 1)`, "unknown keywords"}, // non-symbol key

		// element access errors.
		{`S = Struct.new(:a, :b); S.new(1, 2)[5]`, "offset 5 too large for struct(size:2)"},
		{`S = Struct.new(:a, :b); S.new(1, 2)[-5]`, "offset -5 too small for struct(size:2)"},
		{`S = Struct.new(:a, :b); S.new(1, 2)[:z]`, "no member 'z' in struct"},
		{`S = Struct.new(:a, :b); S.new(1, 2)["z"]`, "no member 'z' in struct"},
		{`S = Struct.new(:a, :b); S.new(1, 2)[Object.new]`, "no implicit conversion of Object into Integer"},
		{`S = Struct.new(:a, :b); S.new(1, 2)[:a, :b]`, "wrong number of arguments (given 2, expected 1)"},
		{`S = Struct.new(:a, :b); S.new(1, 2)[5] = 9`, "offset 5 too large for struct(size:2)"},
		{`S = Struct.new(:a, :b); S.new(1, 2)[:z] = 9`, "no member 'z' in struct"},
		{`S = Struct.new(:a, :b); S.new(1, 2)[Object.new] = 9`, "no implicit conversion of Object into Integer"},

		// frozen struct.
		{`S = Struct.new(:a); s = S.new(1); s.freeze; s.a = 2`, "can't modify frozen"},
		{`S = Struct.new(:a); s = S.new(1); s.freeze; s[0] = 2`, "can't modify frozen"},

		// to_h block errors.
		{`S = Struct.new(:a); S.new(1).to_h { |k, v| [k, v, 1] }`, "element has wrong array length (expected 2, was 3)"},
		{`S = Struct.new(:a); S.new(1).to_h { |k, v| "x" }`, "wrong element type String (expected array)"},

		// values_at / dig type errors.
		{`S = Struct.new(:a, :b); S.new(1, 2).values_at("x")`, "no implicit conversion of String into Integer"},
		{`S = Struct.new(:a, :b, :c); S.new(1, 2, 3).values_at(-4..3)`, "-4..3 out of range"},
		{`S = Struct.new(:a); S.new(1).dig`, "wrong number of arguments (given 0, expected 1+)"},
		{`S = Struct.new(:a); S.new(Object.new).dig(:a, 3)`, "does not have #dig method"},
		{`S = Struct.new(:a); S.new(1).dig(Object.new)`, "no implicit conversion of Object into Integer"},

		// deconstruct_keys errors.
		{`S = Struct.new(:a); S.new(1).deconstruct_keys`, "wrong number of arguments (given 0, expected 1)"},
		{`S = Struct.new(:a); S.new(1).deconstruct_keys(5)`, "wrong argument type Integer (expected Array)"},
		{`S = Struct.new(:a, :b); S.new(1, 2).deconstruct_keys([0, []])`, "no implicit conversion of Array into Integer"},
		{`S = Struct.new(:a, :b); k = Object.new; def k.to_int; "x"; end; S.new(1, 2).deconstruct_keys([k])`, "can't convert Object into Integer"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}

// TestStructNamedRedefineWarns checks that Struct.new(name, …) overwriting an
// existing constant emits the "already initialized constant" warning (to $stderr,
// which the harness folds into the captured stream) and rebinds the constant.
func TestStructNamedRedefineWarns(t *testing.T) {
	out := eval(t, `Struct.new("Dup1", :a); Struct.new("Dup1", :b); p Struct::Dup1.members`)
	if !strings.Contains(out, "already initialized constant Struct::Dup1") {
		t.Errorf("out=%q, want redefine warning", out)
	}
	if !strings.Contains(out, "[:b]") {
		t.Errorf("out=%q, want rebound members [:b]", out)
	}
}
