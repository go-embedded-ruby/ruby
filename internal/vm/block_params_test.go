package vm_test

import "testing"

// TestBlockOptionalSplatParams covers optional (name = default) and *splat
// parameters in block and stabby-lambda lists, the resulting Proc#arity, and the
// argument binding (defaults fill absent params, extras are dropped). Asserted
// against MRI Ruby 4.0.5.
func TestBlockOptionalSplatParams(t *testing.T) {
	cases := []struct{ src, want string }{
		// Optional params: default fills when absent, supplied value wins.
		{`p [proc { |a, b = 5| [a, b] }.call(1), proc { |a, b = 5| [a, b] }.call(1, 2)]`, "[[1, 5], [1, 2]]\n"},
		{`p [lambda { |a, b = 5| [a, b] }.call(9), ->(a, b = 5) { [a, b] }.call(7)]`, "[[9, 5], [7, 5]]\n"},
		// Multiple optionals; a later default may reference an earlier param.
		{`p proc { |a, b = a + 1, c = 9| [a, b, c] }.call(2)`, "[2, 3, 9]\n"},
		// Splat in a stabby lambda, with and without trailing required params.
		{`p [->(*a) { a }.call(1, 2, 3), ->(a, *b) { [a, b] }.call(1, 2, 3)]`, "[[1, 2, 3], [1, [2, 3]]]\n"},
		// Optional + splat combined.
		{`p [->(a, b = 5, *c) { [a, b, c] }.call(1), ->(a, b = 5, *c) { [a, b, c] }.call(1, 2, 3, 4)]`, "[[1, 5, []], [1, 2, [3, 4]]]\n"},
		// Binding edges: fewer than required pads with nil (then the default), more
		// than the parameter count drops the extras.
		{`p proc { |a, b, c = 1| [a, b, c] }.call(1)`, "[1, nil, 1]\n"},
		{`p proc { |a, b = 5| [a, b] }.call(1, 2, 3)`, "[1, 2]\n"},
		// Optional params in a real iterator.
		{`r = []; [10, 20].each { |a, b = 99| r << [a, b] }; p r`, "[[10, 99], [20, 99]]\n"},
		// Proc#arity: a non-lambda proc with optionals reports the positive required
		// count; a lambda (and any *splat) reports -(required + 1); a Symbol#to_proc
		// is a native -2.
		{`p [proc { |a, b = 5| }.arity, proc { |a, b, c = 1| }.arity, proc { |a, *b| }.arity]`, "[1, 2, -2]\n"},
		{`p [lambda { |a, b = 5| }.arity, ->(a, b = 1, *c) {}.arity, ->(*a) {}.arity, ->(a, b) {}.arity]`, "[-2, -2, -1, 2]\n"},
		{`p [proc { |a| }.arity, proc {}.arity, :upcase.to_proc.arity]`, "[1, 0, -2]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
