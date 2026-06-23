package vm_test

import "testing"

// TestInstanceMethodsAndSymbolCompare covers Module#instance_methods,
// Object#methods, and Symbol#<=> / Symbol being Comparable. Asserted against MRI
// Ruby 4.0.5 (method lists are sorted, since their order is implementation-defined).
func TestInstanceMethodsAndSymbolCompare(t *testing.T) {
	cases := []struct{ src, want string }{
		// instance_methods(false): own methods only; default: includes inherited.
		{`class A; def foo; end; def bar; end; end; p A.instance_methods(false).sort`, "[:bar, :foo]\n"},
		{`class A; def foo; end; end
class B < A; def baz; end; end
p [B.instance_methods(false).sort, B.instance_methods.include?(:foo)]`, "[[:baz], true]\n"},
		// Included module methods show up in the full list, not in (false).
		{`module M; def mm; end; end
class A; include M; def foo; end; end
p [A.instance_methods(false).sort, A.instance_methods.include?(:mm)]`, "[[:foo], true]\n"},
		// Object#methods includes inherited and singleton methods.
		{`class A; def foo; end; end; p [A.new.methods.include?(:foo), A.new.methods.include?(:to_s)]`, "[true, true]\n"},
		{`o = Object.new; def o.special; end; p o.methods.include?(:special)`, "true\n"},
		// Symbol#<=> and Comparable.
		{`p [(:a <=> :b), (:b <=> :a), (:a <=> :a), (:a <=> "x")]`, "[-1, 1, 0, nil]\n"},
		{`p [:a < :b, :c > :a, :a.between?(:a, :z)]`, "[true, true, true]\n"},
		{`p [:c, :a, :b].sort`, "[:a, :b, :c]\n"},
		{`p [:banana, :apple, :cherry].min`, ":apple\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
