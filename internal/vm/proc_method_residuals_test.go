package vm_test

import (
	"strings"
	"testing"
)

// TestProcCallAliasIdentity covers that Proc#[], #yield and #=== are the same
// built-in definition as Proc#call, and Method#[] / #=== the same as Method#call
// — so #instance_method compares them equal (MRI 4.0.6 genuine aliases).
func TestProcCallAliasIdentity(t *testing.T) {
	cases := []struct{ name, src string }{
		{"proc_index", `p Proc.instance_method(:[]) == Proc.instance_method(:call)`},
		{"proc_yield", `p Proc.instance_method(:yield) == Proc.instance_method(:call)`},
		{"proc_case", `p Proc.instance_method(:===) == Proc.instance_method(:call)`},
		{"proc_eql", `p Proc.instance_method(:eql?) == Proc.instance_method(:==)`},
		{"method_index", `p Method.instance_method(:[]) == Method.instance_method(:call)`},
		{"method_case", `p Method.instance_method(:===) == Method.instance_method(:call)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != "true\n" {
				t.Errorf("%s = %q, want \"true\\n\"", tc.src, got)
			}
		})
	}
}

// TestProcCallAliasesBehave confirms the aliased names still invoke the proc.
func TestProcCallAliasesBehave(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"index", `add = ->(a, b) { a + b }; p add[2, 3]`, "5\n"},
		{"yield", `add = ->(a, b) { a + b }; p add.yield(2, 3)`, "5\n"},
		{"case", `even = ->(n) { n.even? }; p(even === 4)`, "true\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("%s = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestProcEquality covers Proc#== identity semantics and #eql? as its alias:
// same object (and its #dup) equal; distinct procs, and non-Procs, unequal.
func TestProcEquality(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"same", `p = proc { :foo }; p(p == p)`, "true\n"},
		{"dup", `p = proc { :foo }; p(p == p.dup)`, "true\n"},
		{"eql_same", `p = proc { :foo }; p(p.eql?(p))`, "true\n"},
		{"distinct", `p(proc { :foo } == proc { :foo })`, "false\n"},
		{"lambda_vs_proc", `p(-> { :foo } == proc { :foo })`, "false\n"},
		{"non_proc", `p(proc { :foo } == [])`, "false\n"},
		{"is_public", `p Proc.public_instance_methods(false).include?(:==)`, "true\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("%s = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestProcCurryLambdaness covers that Proc#curry keeps the receiver's lambda-ness
// and propagates it through partial application (proc{}.curry.lambda? is false;
// ->{}.curry.lambda? is true, even after a call that is short of the arity).
func TestProcCurryLambdaness(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"lambda", `p((->{}).curry.lambda?)`, "true\n"},
		{"proc", `p((proc{}).curry.lambda?)`, "false\n"},
		{"procnew", `p(Proc.new{}.curry.lambda?)`, "false\n"},
		{"lambda_partial", `p((-> x, y {}).curry.call(42).lambda?)`, "true\n"},
		{"proc_partial", `p((proc{|x,y|}).curry.call(42).lambda?)`, "false\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("%s = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestProcCurryApplies drives the gather-then-invoke path of a curried proc,
// through both one-at-a-time and grouped argument delivery.
func TestProcCurryApplies(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"one_at_a_time", `add = proc { |x, y, z| x + y + z }; p add.curry[1][2][3]`, "6\n"},
		{"grouped", `add = proc { |x, y, z| x + y + z }; p add.curry[1, 2][3]`, "6\n"},
		{"arity_arg", `add = ->(x, y, z) { x + y + z }; p add.curry(3)[1][2][3]`, "6\n"},
		{"native_lambda", `def foo(a, b); a + b; end; p method(:foo).to_proc.curry(2)[4][5]`, "9\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("%s = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestProcCurryArityErrors covers the lambda-only arity validation of
// Proc#curry(n): fewer than the required arity, or more than a splat-less lambda
// accepts, raises ArgumentError; a non-lambda proc never raises.
func TestProcCurryArityErrors(t *testing.T) {
	raises := []string{
		`(-> x, y, z {}).curry(2)`,        // fewer than required
		`(-> x, y, z {}).curry(4)`,        // more than accepted, no splat
		`(-> x, y, z, *more {}).curry(2)`, // fewer than required, with splat
		`(-> { true }).curry(1)`,          // more than accepted (zero params)
		`(-> a, b = nil {}).curry(5)`,     // more than optional-max
	}
	for _, src := range raises {
		t.Run("raises_"+src, func(t *testing.T) {
			err := runErr(t, src)
			if err == nil || !strings.Contains(err.Error(), "ArgumentError") {
				t.Errorf("%s: want ArgumentError, got %v", src, err)
			}
		})
	}

	ok := []string{
		`(-> a, b, c, d = nil, e = nil {}).curry(4)`, // within optional range
		`(-> a, b, c, *d {}).curry(4)`,               // splat absorbs the excess
		`(proc { |x, y, z| }).curry(2)`,              // a proc never enforces arity
		`(proc { |x, y, z| }).curry(9)`,              // even well past its width
	}
	for _, src := range ok {
		t.Run("ok_"+src, func(t *testing.T) {
			if err := runErr(t, src); err != nil {
				t.Errorf("%s: want no error, got %v", src, err)
			}
		})
	}
}
