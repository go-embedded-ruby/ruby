package vm

import "testing"

// evalString runs a fragment and returns the value of its last expression, so a
// case reads as the Ruby it is testing rather than as a program that prints.
func evalString(t *testing.T, src string) string {
	t.Helper()
	return runSrc(t, "__v = begin\n"+src+"\nend\nputs __v")
}

// Ruby 4.0 adds Binding#implicit_parameters, #implicit_parameter_get and
// #implicit_parameter_defined? (proc.c v4.0.9:680-783). They are the other half
// of taking `_1` out of #local_variables: the implicit parameters do not
// vanish, they move to an API of their own.
//
// ⛔ WHY THIS FEATURE WAS INVISIBLE. The three ruby/spec files for it are
// `pass=0 fail=0 error=0` below 4.0 — every example behind a
// `ruby_version_is "4.0"` guard — and 37 failures above it. A version-move
// measurement that compared PASSING counts read zero on all three and a plan
// was published without them. The cases below are those specs' shapes, run here
// at whatever version rbgo reports, because a guard is not a reason to leave a
// method unimplemented.
func TestBindingImplicitParameters(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{{
		name: "it is reported alone",
		src:  `r = nil; [42].each { it; r = binding.implicit_parameters }; r.inspect`,
		want: `[:it]`,
	}, {
		name: "a numbered parameter is reported",
		src:  `r = nil; [42].each { _1; r = binding.implicit_parameters }; r.inspect`,
		want: `[:_1]`,
	}, {
		// ⛔ Every name UP TO the maximum referenced one, not just the one
		// written: `_3` alone makes the block take three.
		name: "every numbered parameter up to the maximum referenced one",
		src:  `proc { _3; binding.implicit_parameters }.call(:a, :b, :c, :d).inspect`,
		want: `[:_1, :_2, :_3]`,
	}, {
		name: "a scope with neither reports nothing",
		src:  `a = 1; binding.implicit_parameters.inspect`,
		want: `[]`,
	}, {
		// The parameter list is a property of the BLOCK, fixed when it is
		// compiled — not of the point the binding is taken at.
		name: "an implicit parameter referenced AFTER the call is still reported",
		src:  `proc { r = binding.implicit_parameters; a = _1; r }.call.inspect`,
		want: `[:_1]`,
	}, {
		name: "it from a parent scope is not this scope's",
		src:  `r = nil; [1].each { it; proc { r = binding.implicit_parameters }.call }; r.inspect`,
		want: `[]`,
	}, {
		name: "a numbered parameter from a parent scope is not this scope's",
		src:  `r = nil; [1].each { _1; proc { r = binding.implicit_parameters }.call }; r.inspect`,
		want: `[]`,
	}, {
		name: "one in a NESTED scope is not this scope's either",
		src:  `foo = -> { _1 }; binding.implicit_parameters.inspect`,
		want: `[]`,
	}, {
		name: "get reads the value",
		src:  `-> { _1; binding.implicit_parameter_get(:_1) }.call(:a).inspect`,
		want: `:a`,
	}, {
		name: "get reads every numbered parameter up to the maximum",
		src: `proc { _3; [binding.implicit_parameter_get(:_1), ` +
			`binding.implicit_parameter_get(:_2), binding.implicit_parameter_get(:_3)] }` +
			`.call(:a, :b, :c, :d).inspect`,
		want: `[:a, :b, :c]`,
	}, {
		name: "get reads it",
		src:  `-> { it; binding.implicit_parameter_get(:it) }.call(:a).inspect`,
		want: `:a`,
	}, {
		name: "get accepts a String",
		src:  `-> { _1; binding.implicit_parameter_get('_1') }.call(:a).inspect`,
		want: `:a`,
	}, {
		name: "defined? is false where there is none",
		src:  `binding.implicit_parameter_defined?(:_1).inspect`,
		want: `false`,
	}, {
		name: "defined? is true where there is one",
		src:  `proc { _1; binding.implicit_parameter_defined?(:_1) }.call.inspect`,
		want: `true`,
	}, {
		// ⛔ The boundary: `_3` defines three, and the fourth is false rather
		// than an error. A rule that answered "defined" for any `_N` would pass
		// every other case here.
		name: "defined? stops at the maximum referenced one",
		src: `proc { _3; [binding.implicit_parameter_defined?(:_3), ` +
			`binding.implicit_parameter_defined?(:_4)] }.call.inspect`,
		want: `[true, false]`,
	}, {
		name: "defined? is false for a parent scope's",
		src:  `r = nil; [1].each { _1; proc { r = binding.implicit_parameter_defined?(:_1) }.call }; r.inspect`,
		want: `false`,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalString(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %s\nwant %s", tc.src, got, tc.want)
			}
		})
	}
}

// ⛔ THE TWO REFUSALS ARE DIFFERENT AND BOTH ARE PINNED. MRI checks the SHAPE of
// the name before it looks for a value (proc.c v4.0.9:726, 775): a name that
// could never be an implicit parameter is refused differently from one that
// could be and is not. Collapsing them would answer "not defined" for `:a`.
func TestBindingImplicitParameterErrors(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{{
		name: "a name no scope could ever hold",
		src:  `begin; binding.implicit_parameter_get(:a); rescue NameError => e; e.message; end`,
		want: `'a' is not an implicit parameter`,
	}, {
		name: "the same refusal from defined?, which does NOT answer false",
		src:  `begin; binding.implicit_parameter_defined?(:a); rescue NameError => e; e.message; end`,
		want: `'a' is not an implicit parameter`,
	}, {
		name: "a well-formed name that this scope does not hold",
		src:  `begin; binding.implicit_parameter_get(:_1); rescue NameError => e; e.message; end`,
		want: `implicit parameter '_1' is not defined for #<Binding>`,
	}, {
		name: "it, when this scope has none",
		src:  `begin; binding.implicit_parameter_get(:it); rescue NameError => e; e.message; end`,
		want: `implicit parameter 'it' is not defined for #<Binding>`,
	}, {
		name: "a parent scope's is not defined HERE",
		src: `r = nil; [1].each { _1; proc { begin; binding.implicit_parameter_get(:_1); ` +
			`rescue NameError => e; r = e.message; end }.call }; r`,
		want: `implicit parameter '_1' is not defined for #<Binding>`,
	}, {
		name: "neither a String nor a Symbol",
		src:  `begin; binding.implicit_parameter_get(1); rescue TypeError => e; e.message; end`,
		want: `1 is not a symbol nor a string`,
	}, {
		name: "and the same from defined?",
		src:  `begin; binding.implicit_parameter_defined?(1); rescue TypeError => e; e.message; end`,
		want: `1 is not a symbol nor a string`,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalString(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %q\nwant %q", tc.src, got, tc.want)
			}
		})
	}
}

// The arity of all three is fixed in MRI (0, 1 and 1), so a wrong count is an
// ArgumentError and not a silently ignored argument.
//
// ⛔ Named by the coverage gate rather than thought of: the three refusals were
// written and never driven, which is the same as not having them.
func TestBindingImplicitParameterArity(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{{
		name: "implicit_parameters takes none",
		src:  `begin; binding.implicit_parameters(1); rescue ArgumentError => e; e.message; end`,
		want: `wrong number of arguments (given 1, expected 0)`,
	}, {
		name: "implicit_parameter_get takes one",
		src:  `begin; binding.implicit_parameter_get(:_1, 2); rescue ArgumentError => e; e.message; end`,
		want: `wrong number of arguments (given 2, expected 1)`,
	}, {
		name: "implicit_parameter_defined? takes one",
		src:  `begin; binding.implicit_parameter_defined?; rescue ArgumentError => e; e.message; end`,
		want: `wrong number of arguments (given 0, expected 1)`,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalString(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %q\nwant %q", tc.src, got, tc.want)
			}
		})
	}
}

// Binding#dup and #clone copy the implicit-parameter map rather than sharing
// it, the same way they copy the name list — so the copy answers for the same
// scope and a later change to one cannot reach the other.
//
// ⛔ Also named by the coverage gate: every dup in the suite happened to be of
// a binding with NO implicit parameters, so the copy was never exercised.
func TestADuplicatedBindingKeepsItsImplicitParameters(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{{
		name: "dup of a binding that has one",
		src:  `r = nil; [7].each { _1; r = binding.dup.implicit_parameter_get(:_1) }; r.inspect`,
		want: `7`,
	}, {
		name: "clone of a binding that has one",
		src:  `r = nil; [7].each { it; r = binding.clone.implicit_parameters }; r.inspect`,
		want: `[:it]`,
	}, {
		name: "dup of a binding that has none keeps having none",
		src:  `a = 1; binding.dup.implicit_parameters.inspect`,
		want: `[]`,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalString(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %s\nwant %s", tc.src, got, tc.want)
			}
		})
	}
}

// ⛔ THE CONTROL. An implicit parameter is not a local variable, and `it` has
// never been one — that part is already rbgo's behaviour and must stay. Without
// this, the change above could have made `it` visible to #local_variables and
// every test here would still pass.
//
// `_1` is a different case and deliberately untouched: ruby/spec pins it as
// VISIBLE under `ruby_version_is ""..."4.0"` and hidden only from 4.0, and rbgo
// reports 3.4.1. Hiding it belongs with the version bump, not here.
func TestImplicitParametersAreNotLocalVariables(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{{
		// Membership, not the whole list: local_variables inside a block also
		// names the enclosing scope's locals, which is correct and not what this
		// is about.
		name: "it is absent from local_variables",
		src:  `r = nil; [1].each { it; r = binding.local_variables.include?(:it) }; r.inspect`,
		want: `false`,
	}, {
		name: "and unreachable through local_variable_get",
		src: `r = nil; [1].each { it; begin; binding.local_variable_get(:it); ` +
			`rescue NameError => e; r = e.message; end }; r`,
		want: `local variable 'it' is not defined for #<Binding>`,
	}, {
		name: "while _1 stays visible at the version rbgo reports",
		src:  `r = nil; [1].each { _1; r = binding.local_variables.include?(:_1) }; r.inspect`,
		want: `true`,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalString(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %s\nwant %s", tc.src, got, tc.want)
			}
		})
	}
}
