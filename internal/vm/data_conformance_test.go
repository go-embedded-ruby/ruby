package vm_test

import (
	"strings"
	"testing"
)

// TestDataConformance exercises the MRI 3.2+/4.0 Data surface: Data.define,
// positional and keyword construction, the immutable value/query methods,
// #with, pattern-matching hooks and inspect formatting. Every expectation is
// asserted against MRI Ruby 4.0.5.
func TestDataConformance(t *testing.T) {
	cases := []struct{ src, want string }{
		// --- Data.define --------------------------------------------------------
		{`Foo = Data.define(:a, :b); p Foo.members`, "[:a, :b]\n"},
		{`Foo = Data.define(:a, :b); p Foo.ancestors.first(3)`, "[Foo, Data, Object]\n"},
		{`Foo = Data.define(:a); p Foo.superclass == Data`, "true\n"},
		{`p Data.superclass == Object`, "true\n"},
		{`p Data.define(:a).name`, "nil\n"},                // anonymous
		{`Foo = Data.define(:a); p Foo.name`, "\"Foo\"\n"}, // named on constant assignment
		{`p Data.define("a", "b").members`, "[:a, :b]\n"},  // String members
		{`p Data.define("a", :b, "c").members`, "[:a, :b, :c]\n"},
		{`p Data.define.members`, "[]\n"}, // no members
		// A block is class-evaluated (self = class) and can define reader-using methods.
		{`Foo = Data.define(:t, :y) { def label; "#{t} (#{y})"; end }; p Foo.new("M", 1999).label`, "\"M (1999)\"\n"},
		{`module M; Bar = Data.define(:v) { def dbl; v * 2; end }; end; p M::Bar.new(21).dbl`, "42\n"},

		// --- construction: positional -------------------------------------------
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2).a`, "1\n"},
		{`Foo = Data.define(:a, :b); f = Foo.new(1, 2); p [f.a, f.b]`, "[1, 2]\n"},
		{`Foo = Data.define(:a, :b); p Foo[1, 2].b`, "2\n"}, // Data[] == new
		{`One = Data.define(:v); p One.new(42).v`, "42\n"},
		{`One = Data.define(:v); p One.new(42, **{}).v`, "42\n"},      // positional + empty kwsplat
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2, **{}).a`, "1\n"}, // empty kwsplat dropped

		// --- construction: keyword ----------------------------------------------
		{`Foo = Data.define(:a, :b); p Foo.new(a: 1, b: 2).a`, "1\n"},
		{`Foo = Data.define(:a, :b); p Foo[a: 1, b: 2].b`, "2\n"},
		{`Foo = Data.define(:a, :b); p Foo.new("a" => 1, "b" => 2).a`, "1\n"}, // String keys
		{`One = Data.define(:v); p One.new("v" => -1, v: 42).v`, "42\n"},      // last of String/Symbol wins

		// --- frozen / immutability ---------------------------------------------
		{`Foo = Data.define(:a); p Foo.new(1).frozen?`, "true\n"},
		{`p Data.define.new.frozen?`, "true\n"},
		{`Foo = Data.define(:a); p Foo.new(1).respond_to?(:a)`, "true\n"},
		{`Foo = Data.define(:a); p Foo.new(1).respond_to?(:a=)`, "false\n"}, // no writer

		// --- members ------------------------------------------------------------
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2).members`, "[:a, :b]\n"},
		{`Foo = Data.define(:a, :b); p Foo.members`, "[:a, :b]\n"},

		// --- #with --------------------------------------------------------------
		{`Foo = Data.define(:a, :b); f = Foo.new(1, 2); p f.with(b: 99).b`, "99\n"},
		{`Foo = Data.define(:a, :b); f = Foo.new(1, 2); p f.with(a: 9).a`, "9\n"},
		{`Foo = Data.define(:a, :b); f = Foo.new(1, 2); p f.with.equal?(f)`, "true\n"}, // no args -> self
		{`Foo = Data.define(:a, :b); f = Foo.new(1, 2); p f.with(**{}).equal?(f)`, "true\n"},
		{`Foo = Data.define(:a, :b); f = Foo.new(1, 2); p f.with("a" => 9).a`, "9\n"}, // String key
		{`Foo = Data.define(:a, :b); f = Foo.new(1, 2); g = f.with(a: 5); p [f.a, g.a, g.frozen?]`, "[1, 5, true]\n"},

		// --- to_h ---------------------------------------------------------------
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2).to_h`, "{a: 1, b: 2}\n"},
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2).to_h { |k, v| [k, v * 10] }`, "{a: 10, b: 20}\n"},
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2).to_h { |k, v| [v, k] }`, "{1 => :a, 2 => :b}\n"},

		// --- deconstruct / deconstruct_keys -------------------------------------
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2).deconstruct`, "[1, 2]\n"},
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2).deconstruct_keys(nil)`, "{a: 1, b: 2}\n"},
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2).deconstruct_keys([:a])`, "{a: 1}\n"},
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2).deconstruct_keys([])`, "{}\n"},
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2).deconstruct_keys(["a", "b"])`, "{\"a\" => 1, \"b\" => 2}\n"},
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2).deconstruct_keys([:a, :b, :a])`, "{}\n"}, // more keys than members
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2).deconstruct_keys([:z, :a])`, "{}\n"},     // first not a member -> stop
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2).deconstruct_keys([:a, :z])`, "{a: 1}\n"}, // stop at first miss

		// --- pattern matching ---------------------------------------------------
		{`Foo = Data.define(:a, :b); case Foo.new(1, 2); in Foo(a:, b:); p [a, b]; end`, "[1, 2]\n"},
		{`Foo = Data.define(:a, :b); case Foo.new(1, 2); in Foo[x, y]; p [x, y]; end`, "[1, 2]\n"},

		// --- == / eql? / hash ---------------------------------------------------
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2) == Foo.new(1, 2)`, "true\n"},
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2) == Foo.new(1, 3)`, "false\n"},
		{`A = Data.define(:x); B = Data.define(:x); p A.new(1) == B.new(1)`, "false\n"}, // different class
		{`Foo = Data.define(:a); p Foo.new(1).eql?(Foo.new(1))`, "true\n"},
		{`Foo = Data.define(:a); p Foo.new(1).eql?(Foo.new(1.0))`, "false\n"}, // 1 vs 1.0
		{`Foo = Data.define(:a); p Foo.new(1) == Foo.new(1.0)`, "true\n"},
		{`Foo = Data.define(:a); p Foo.new(1) == 5`, "false\n"}, // non-Data
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2).hash == Foo.new(1, 2).hash`, "true\n"},
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2).hash.instance_of?(Integer)`, "true\n"},
		// a plain-object member exercises the "neither Struct nor Data" member path.
		{`Foo = Data.define(:a); o = Object.new; d = Foo.new(o); p d == Foo.new(o)`, "true\n"},
		{`Foo = Data.define(:a); p Foo.new(Object.new) == Foo.new(Object.new)`, "false\n"},
		{`Foo = Data.define(:a); p Foo.new(Object.new).hash.instance_of?(Integer)`, "true\n"},

		// --- inspect / to_s -----------------------------------------------------
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2).inspect`, "\"#<data Foo a=1, b=2>\"\n"},
		{`Foo = Data.define(:a, :b); p Foo.new(1, 2).to_s`, "\"#<data Foo a=1, b=2>\"\n"},
		{`Foo = Data.define(:a); p Foo.new("x").inspect`, "\"#<data Foo a=\\\"x\\\">\"\n"},
		{`p Data.define(:a).new(1).to_s`, "\"#<data a=1>\"\n"}, // anonymous class: no name
		{`p Data.define.new.inspect`, "\"#<data>\"\n"},         // anonymous, no members
		{`Foo = Data.define(:a, :b); p Foo.instance_method(:inspect) == Foo.instance_method(:to_s)`, "true\n"},
		// recursive value: the recursive member renders as #<data Name:...>.
		{`Foo = Data.define(:a, :b); x = Foo.allocate; x.send(:initialize, a: 42, b: x); p x.to_s`,
			"\"#<data Foo a=42, b=#<data Foo:...>>\"\n"},

		// --- override #initialize (positional -> keywords; super) ---------------
		{`Foo = Data.define(:w, :h, :area) { def initialize(w:, h:); super(w: w, h: h, area: w * h); end }; p Foo.new(w: 2, h: 3).area`, "6\n"},
		{`$log = []; Foo = Data.define(:a, :b) { def initialize(*r, **k); super; $log = [r, k]; end }; Foo.new(1, 2); p $log`, "[[], {a: 1, b: 2}]\n"},
		{`$log = []; Foo = Data.define(:a, :b) { def initialize(*r, **k); super; $log = [r, k]; end }; f = Foo.new(1, 2); f.with(a: 9); p $log`, "[[], {a: 9, b: 2}]\n"},
		// bare allocate + #initialize (bypasses .new, re-allocates structVals).
		{`Foo = Data.define(:a); f = Foo.allocate; f.send(:initialize, a: 7); p f.a`, "7\n"},
		// subclass construction inherits .new and shares members.
		{`Foo = Data.define(:a, :b); class Sub < Foo; end; p Sub.new(1, 2).to_s`, "\"#<data Sub a=1, b=2>\"\n"},
		{`Foo = Data.define(:a, :b); s = Class.new(Foo).new(a: 1, b: 2); p [s.a, s.b]`, "[1, 2]\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
		}
	}
}

// TestDataConformanceErrors covers Data's error classes and MRI-exact messages:
// arity, missing/unknown keywords, TypeError on bad members/keys, FrozenError on
// mutation, and the abstract Data.new.
func TestDataConformanceErrors(t *testing.T) {
	errs := []struct{ src, want string }{
		// Data is abstract.
		{`Data.new`, "undefined method 'new' for class Data"},
		{`class Bad < Data; end; Bad.new`, "undefined method 'new' for class Bad"},

		// Data.define bad members.
		{`Data.define(1)`, "1 is not a symbol nor a string"},
		{`Data.define(:a, :a)`, "duplicate member: a"},

		// positional arity.
		{`Foo = Data.define(:a, :b); Foo.new(1, 2, 3)`, "wrong number of arguments (given 3, expected 0..2)"},
		// missing (positional too few -> missing keywords).
		{`Foo = Data.define(:a, :b); Foo.new(1)`, "missing keyword: :b"},
		{`Foo = Data.define(:a, :b, :c); Foo.new(1)`, "missing keywords: :b, :c"},
		// missing (keyword).
		{`Foo = Data.define(:a, :b); Foo.new(a: 1)`, "missing keyword: :b"},
		{`Foo = Data.define(:a, :b); Foo.new`, "missing keywords: :a, :b"},
		// unknown keyword (singular + plural), missing wins over unknown.
		{`Foo = Data.define(:a, :b); Foo.new(a: 1, b: 2, c: 3)`, "unknown keyword: :c"},
		{`Foo = Data.define(:a, :b); Foo.new(a: 1, b: 2, c: 3, d: 4)`, "unknown keywords: :c, :d"},
		{`Foo = Data.define(:a, :b); Foo.new(a: 1, c: 3, d: 4)`, "missing keyword: :b"}, // missing before unknown
		// positional mixed with keyword.
		{`Foo = Data.define(:a, :b); Foo.new(1, b: 2)`, "wrong number of arguments (given 2, expected 0)"},
		{`Foo = Data.define(:a, :b); Foo.new(1, 2, c: 3)`, "wrong number of arguments (given 3, expected 0)"},
		// bad keyword key types.
		{`Foo = Data.define(:a); Foo.new(1 => 2)`, "1 is not a symbol nor a string"},
		{`Foo = Data.define(:a); k = Object.new; def k.to_str; 0; end; Foo.new(k => 2)`, "can't convert Object into String"},

		// #initialize called directly with bad arity.
		{`Foo = Data.define(:a); f = Foo.allocate; f.send(:initialize, 5)`, "wrong number of arguments (given 1, expected 0)"},
		{`Foo = Data.define(:a); f = Foo.allocate; f.send(:initialize, 1, 2)`, "wrong number of arguments (given 2, expected 0)"},

		// FrozenError on any mutation attempt.
		{`Foo = Data.define(:a); Foo.new(1).instance_variable_set(:@a, 5)`, "can't modify frozen Foo"},

		// #with errors.
		{`Foo = Data.define(:a, :b); Foo.new(1, 2).with(z: 9)`, "unknown keyword: :z"},
		{`Foo = Data.define(:a, :b); Foo.new(1, 2).with(4, "m")`, "wrong number of arguments (given 2, expected 0)"},
		{`Foo = Data.define(:a, :b); Foo.new(1, 2).with(4)`, "wrong number of arguments (given 1, expected 0)"},

		// #deconstruct_keys errors.
		{`Foo = Data.define(:a); Foo.new(1).deconstruct_keys`, "wrong number of arguments (given 0, expected 1)"},
		{`Foo = Data.define(:a); Foo.new(1).deconstruct_keys("x")`, "wrong argument type String (expected Array or nil)"},
		{`Foo = Data.define(:a, :b); Foo.new(1, 2).deconstruct_keys([0, 1])`, "0 is not a symbol nor a string"},
		{`Foo = Data.define(:a, :b); k = Object.new; def k.to_str; 0; end; Foo.new(1, 2).deconstruct_keys([k])`, "can't convert Object into String"},

		// #to_h block errors.
		{`Foo = Data.define(:a, :b); Foo.new(1, 2).to_h { |k, v| [k, v, 1] }`, "element has wrong array length (expected 2, was 3)"},
		{`Foo = Data.define(:a, :b); Foo.new(1, 2).to_h { |k, v| "x" }`, "wrong element type String"},
		{`Foo = Data.define(:a); Foo.new(1).to_h { |k, v| o = Object.new; def o.to_a; [1, 2]; end; o }`, "wrong element type Object"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}

// TestDataToHToAryCoercion checks that a #to_h block's returned pair is coerced
// with #to_ary (never #to_a), matching MRI.
func TestDataToHToAryCoercion(t *testing.T) {
	out := eval(t, `Foo = Data.define(:a)
o = Object.new
def o.to_ary; [:k, :v]; end
p Foo.new(1).to_h { |k, val| o }`)
	if out != "{k: :v}\n" {
		t.Errorf("to_ary coercion out=%q, want {k: :v}", out)
	}
}

// TestDataAnonymousRecursiveInspect exercises the recursion guard's anonymous-
// class branch (the class name is empty, so the sentinel uses Class#to_s).
func TestDataAnonymousRecursiveInspect(t *testing.T) {
	out := eval(t, `Base = Data.define(:a, :b)
k = Class.new(Base)
x = k.allocate
x.send(:initialize, a: 1, b: x)
puts x.to_s`)
	if !strings.Contains(out, "#<data a=1, b=#<data ") || !strings.Contains(out, ":...>>") {
		t.Errorf("anonymous recursive inspect out=%q", out)
	}
}
