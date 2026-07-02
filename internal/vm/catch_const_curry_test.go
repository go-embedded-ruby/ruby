package vm_test

import (
	"strings"
	"testing"
)

// TestCatchConstCurry covers Kernel#catch/#throw, Module#const_get/#const_set/
// #const_defined?, and Proc#curry. Asserted against MRI Ruby 4.0.5.
func TestCatchConstCurry(t *testing.T) {
	cases := []struct{ src, want string }{
		// catch / throw: tag+value, tag only, no throw, fresh tag, nesting, across a call.
		{`p [catch(:x) { throw :x, 42 }, catch(:x) { throw :x }, catch(:x) { 7 }]`, "[42, nil, 7]\n"},
		{`p catch { |t| throw t, :ok }`, ":ok\n"},
		{`p catch(:a) { catch(:b) { throw :a, 1 }; 2 }`, "1\n"},
		{`def f; throw :done, 99; end; p catch(:done) { f; :never }`, "99\n"},
		// const_get / const_set / const_defined? on a class and on Object.
		{`class C; end; C.const_set(:X, 9); p [C::X, C.const_get(:X), C.const_get("X")]`, "[9, 9, 9]\n"},
		{`Object.const_set(:FOO, 42); p [FOO, Object.const_get(:Integer)]`, "[42, Integer]\n"},
		{`class C; Z = 1; end; C.const_set(:W, 2); p [C.const_defined?(:Z), C.const_defined?(:W), C.const_defined?(:Q)]`, "[true, true, false]\n"},
		{`p Math.const_get(:PI).round(2)`, "3.14\n"},
		// curry: stepwise, multi-arg steps, reuse of a partial, proc + explicit/optional arity.
		{`add = ->(a, b, c) { a + b + c }; p [add.curry[1][2][3], add.curry[1, 2][3], add.curry[1][2, 3]]`, "[6, 6, 6]\n"},
		{`f = ->(a, b, c) { [a, b, c] }; g = f.curry[1]; p [g[2][3], g[20][30]]`, "[[1, 2, 3], [1, 20, 30]]\n"},
		{`mul = proc { |a, b, c| a * b * c }; sub = ->(a, b) { a - b }; p [mul.curry[2][3][4], sub.curry(2)[10][3]]`, "[24, 7]\n"},
		{`p :upcase.to_proc.curry["hi"]`, "\"HI\"\n"}, // negative-arity proc -> required count
		{`c = ->(a, b) { a + b }.curry; p [c[1].lambda?, c.is_a?(Proc)]`, "[true, true]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`throw :nope, 1`, "uncaught throw :nope"},                // UncaughtThrowError
		{`catch(:x)`, "no block given"},                           // catch needs a block
		{`Object.const_get(:foo)`, "wrong constant name foo"},     // not uppercase
		{`Object.const_get(123)`, "is not a symbol nor a string"}, // bad type
		{`Object.const_get(:Nope)`, "uninitialized constant"},     // valid name, absent -> NameError
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}
