package vm_test

import (
	"strings"
	"testing"
)

// TestKernelArrayConversion covers Kernel#Array's MRI protocol: the real-Array
// shortcut, the #to_ary-then-#to_a chain (each may decline with nil), the
// single-element wrap when neither yields an Array, and the TypeError raised
// when a conversion returns a non-Array.
func TestKernelArrayConversion(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"real_array", `p Array([1, 2, 3])`, "[1, 2, 3]\n"},
		{"nil", `p Array(nil)`, "[]\n"},
		{"to_ary", `
class A1; def to_ary; [1, 2]; end; end
p Array(A1.new)`, "[1, 2]\n"},
		{"to_ary_nil_then_to_a", `
class A2; def to_ary; nil; end; def to_a; [3, 4]; end; end
p Array(A2.new)`, "[3, 4]\n"},
		{"to_a", `
class A3; def to_a; [5, 6]; end; end
p Array(A3.new)`, "[5, 6]\n"},
		{"to_a_nil_wraps", `
class A4; def to_a; nil; end; end
o = A4.new
p Array(o) == [o]`, "true\n"},
		{"private_to_ary", `
class A5; private def to_ary; [7, 8]; end; end
p Array(A5.new)`, "[7, 8]\n"},
		{"wrap_plain", `o = Object.new; p Array(o) == [o]`, "true\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestKernelArrayConversionErrors(t *testing.T) {
	cases := []struct{ name, src string }{
		{"to_ary_non_array", `
class B1; def to_ary; "x"; end; end
Array(B1.new)`},
		{"to_a_non_array", `
class B2; def to_a; "x"; end; end
Array(B2.new)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), "TypeError") {
				t.Errorf("src=%q: want TypeError, got %v", tc.src, err)
			}
		})
	}
}

// TestKernelHashConversion covers Kernel#Hash: the real-Hash shortcut, nil and
// empty-Array to {}, #to_hash conversion, and the TypeErrors for a missing or
// non-Hash-returning #to_hash.
func TestKernelHashConversion(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"real_hash", `p Hash({a: 1})`, "{a: 1}\n"},
		{"nil", `p Hash(nil)`, "{}\n"},
		{"empty_array", `p Hash([])`, "{}\n"},
		{"to_hash", `
class H1; def to_hash; {b: 2}; end; end
p Hash(H1.new)`, "{b: 2}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestKernelHashConversionErrors(t *testing.T) {
	cases := []struct{ name, src string }{
		{"no_to_hash", `Hash(Object.new)`},
		{"to_hash_non_hash", `
class H2; def to_hash; "x"; end; end
Hash(H2.new)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), "TypeError") {
				t.Errorf("src=%q: want TypeError, got %v", tc.src, err)
			}
		})
	}
}

// TestKernelStringConversion covers Kernel#String: the String / String-subclass
// shortcut (no #to_s call), the ordinary #to_s conversion, and the
// #method_missing path where #to_s is undef'd but answered dynamically.
func TestKernelStringConversion(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"nil", `p String(nil)`, "\"\"\n"},
		{"float", `p String(1.12)`, "\"1.12\"\n"},
		{"bool", `p String(false)`, "\"false\"\n"},
		{"constant", `p String(Object)`, "\"Object\"\n"},
		{"to_s", `
class S1; def to_s; "hi"; end; end
p String(S1.new)`, "\"hi\"\n"},
		{"same_object", `s = +"x"; p String(s).equal?(s)`, "true\n"},
		{"subclass", `
sub = Class.new(String)
s = sub.new("y")
p String(s).equal?(s)`, "true\n"},
		{"method_missing", `
o = Object.new
class << o
  undef_method :to_s
  def method_missing(m, *a); m == :to_s ? "mm" : super; end
end
p String(o)`, "\"mm\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestKernelStringConversionErrors(t *testing.T) {
	cases := []struct{ name, src, wantClass string }{
		{"undef_to_s", `
o = Object.new
class << o; undef_method :to_s; end
String(o)`, "TypeError"},
		{"respond_to_false", `
o = Object.new
class << o
  def respond_to?(m, ip = false); m == :to_s ? false : super; end
end
String(o)`, "TypeError"},
		{"respond_to_true_but_undef", `
o = Object.new
class << o
  undef_method :to_s
  def respond_to?(m, ip = false); m == :to_s ? true : super; end
end
String(o)`, "TypeError"},
		{"to_s_non_string", `
class S2; def to_s; 123; end; end
String(S2.new)`, "TypeError"},
		{"to_s_raises_other", `
class S3; def to_s; raise ArgumentError, "boom"; end; end
String(S3.new)`, "ArgumentError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.wantClass) {
				t.Errorf("src=%q: want %s, got %v", tc.src, tc.wantClass, err)
			}
		})
	}
}

// TestKernelDupClone covers Object#dup / Object#clone: the #initialize_copy hook
// (via #initialize_dup / #initialize_clone), clone's frozen-state and singleton
// copying, and the freeze: keyword handling.
func TestKernelDupClone(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"dup_immediate", `p 1.dup`, "1\n"},
		{"clone_immediate", `p 1.clone`, "1\n"},
		{"dup_calls_initialize_copy", `
class D1
  attr_accessor :obj
  def initialize; @obj = :orig; end
  def initialize_copy(o); @obj = :copied; end
end
p D1.new.dup.obj`, ":copied\n"},
		{"dup_not_frozen", `p [1].freeze.dup.frozen?`, "false\n"},
		{"clone_copies_frozen", `p [1].freeze.clone.frozen?`, "true\n"},
		{"clone_freeze_false", `p [1].freeze.clone(freeze: false).frozen?`, "false\n"},
		{"clone_freeze_true", `p [1].clone(freeze: true).frozen?`, "true\n"},
		{"clone_freeze_nil", `p [1].freeze.clone(freeze: nil).frozen?`, "true\n"},
		{"clone_singleton_method", `
o = Object.new
def o.special; :yes; end
p o.clone.special`, ":yes\n"},
		{"clone_singleton_on_array", `
a = [1, 2]
def a.tag; :t; end
p a.clone.tag`, ":t\n"},
		{"clone_no_singleton", `p Object.new.clone.class`, "Object\n"},
		{"initialize_clone_kwargs", `
class CF
  def initialize_clone(other, **kw); @rec = kw; end
  attr_reader :rec
end
p CF.new.clone(freeze: true).rec`, "{freeze: true}\n"},
		{"initialize_copy_same", `o = Object.new; p o.send(:initialize_copy, o).equal?(o)`, "true\n"},
		{"initialize_dup_returns_self", `a = Object.new; p a.send(:initialize_dup, Object.new).equal?(a)`, "true\n"},
		{"initialize_clone_returns_self", `a = Object.new; p a.send(:initialize_clone, Object.new).equal?(a)`, "true\n"},
		{"initialize_clone_freeze_kw", `a = Object.new; p a.send(:initialize_clone, Object.new, freeze: true).equal?(a)`, "true\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestKernelDupCloneErrors(t *testing.T) {
	cases := []struct{ name, src, wantClass string }{
		{"clone_freeze_bad", `Object.new.clone(freeze: 1)`, "ArgumentError"},
		{"clone_unknown_kw", `Object.new.clone(foo: 1)`, "ArgumentError"},
		{"initialize_copy_frozen", `Object.new.freeze.send(:initialize_copy, Object.new)`, "FrozenError"},
		{"initialize_copy_diff_class", `
klass = Class.new
sub = Class.new(klass)
klass.new.send(:initialize_copy, sub.new)`, "TypeError"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.wantClass) {
				t.Errorf("src=%q: want %s, got %v", tc.src, tc.wantClass, err)
			}
		})
	}
}

// TestKernelNumericFrozen covers that Bignum, Complex and Rational report as
// frozen (immutable value objects), alongside the singleton-class freezing that
// Object#freeze performs.
func TestKernelNumericFrozen(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"bignum", `p (2 ** 70).frozen?`, "true\n"},
		{"complex", `p Complex(1, 2).frozen?`, "true\n"},
		{"rational", `p Rational(1, 2).frozen?`, "true\n"},
		{"freeze_singleton", `
o = Object.new
c = o.singleton_class
before = c.frozen?
o.freeze
p [before, c.frozen?]`, "[false, true]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}
