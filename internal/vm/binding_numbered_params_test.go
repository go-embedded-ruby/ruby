package vm

import "testing"

// Ruby 4.0 takes the numbered parameters out of Binding's local-variable API
// (language/numbered_parameters_spec.rb:112-135, opened by `ruby_version_is
// "4.0"`; the block at :93-110 asserts the opposite below it and closes).
//
// ⛔ THE REFUSAL IS ABOUT THE NAME, NOT THE BINDING. MRI tests
// rb_numparam_id_p(lid) at the TOP of each of the three methods, before any
// lookup happens (proc.c v4.0.9:549, 602, 649), so `_1` raises in a binding
// that has no numbered parameter at all. `_1` is not a local variable that is
// being hidden; it is not a local variable name.
func TestNumberedParametersAreNotLocalVariables(t *testing.T) {
	const msg = "numbered parameter '_1' is not a local variable"
	for _, tc := range []struct{ name, src, want string }{
		{
			"local_variable_get raises",
			`begin; -> { _1; binding.local_variable_get(:_1) }.call("a"); rescue NameError => e; e.message; end`,
			msg,
		},
		{
			"local_variable_set raises",
			`begin; -> { _1; binding.local_variable_set(:_1, "b") }.call("a"); rescue NameError => e; e.message; end`,
			msg,
		},
		{
			// RAISES rather than answering false, which is the easy one to get
			// wrong: "not defined" is the natural reading and the wrong one.
			"local_variable_defined? raises rather than answering false",
			`begin; -> { _1; binding.local_variable_defined?(:_1) }.call("a"); rescue NameError => e; e.message; end`,
			msg,
		},

		// ⛔ Before the lookup: no numbered parameter is in scope anywhere here,
		// and it still raises. A guard placed after the lookup would answer
		// "local variable '_1' is not defined for ..." instead.
		{
			"it raises even where no numbered parameter exists",
			`begin; binding.local_variable_get(:_1); rescue NameError => e; e.message; end`,
			msg,
		},
		{
			"and for defined?, where false would otherwise be the answer",
			`begin; binding.local_variable_defined?(:_9); rescue NameError => e; e.message; end`,
			"numbered parameter '_9' is not a local variable",
		},

		// ⛔ CONTROL: `it` is a LEGAL local variable name, so the refusal must
		// NOT widen to rb_implicit_param_p. MRI uses the narrower
		// rb_numparam_id_p in all three methods for exactly this reason.
		{
			"it is an ordinary local variable name and is not refused",
			`it = 5; binding.local_variable_get(:it).inspect`,
			"5",
		},
		{
			"and can be set through a binding",
			`it = 5; binding.local_variable_set(:it, 6); it.inspect`,
			"6",
		},
		{
			"and answers defined?",
			`it = 5; binding.local_variable_defined?(:it).inspect`,
			"true",
		},

		// ⛔ CONTROL: the boundary of the name shape. `_0` and `_10` are
		// ordinary locals in MRI, so a refusal that tested "starts with _" or
		// "_ then digits" would reject names the program may legitimately use.
		{
			"_0 is an ordinary local",
			`_0 = 1; binding.local_variable_defined?(:_0).inspect`,
			"true",
		},
		{
			"_10 is an ordinary local",
			`_10 = 1; binding.local_variable_defined?(:_10).inspect`,
			"true",
		},
		{
			"a plain underscore is an ordinary local",
			`_ = 1; binding.local_variable_defined?(:_).inspect`,
			"true",
		},

		// ⛔ CONTROL, and the trade this change must NOT make: #parameters is
		// ungated (numbered_parameters_spec.rb:85) and still names `_1`. Only
		// the LOCAL TABLE loses the name, never the parameter list -- which is
		// why the compiler blanks b.locals and leaves b.params alone.
		{
			"a proc still names its numbered parameters",
			`proc { _1 }.parameters.inspect`,
			"[[:opt, :_1]]",
		},
		{
			"and a lambda does too",
			`-> { _2 }.parameters.inspect`,
			"[[:req, :_1], [:req, :_2]]",
		},

		// ⛔ CONTROL: they did not vanish, they moved. 4.0 gives them an API of
		// their own, and taking them out of one must not take them out of both.
		{
			"implicit_parameters still sees them",
			`-> { _1; binding.implicit_parameters }.call("a").inspect`,
			"[:_1]",
		},
		{
			"and implicit_parameter_get still reads them",
			`-> { _1; binding.implicit_parameter_get(:_1) }.call("a").inspect`,
			`"a"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalString(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %q\nwant %q", tc.src, got, tc.want)
			}
		})
	}
}

// #local_variables is tested through runSrc rather than evalString because
// evalString wraps the source in `__v = begin … end`, and that assignment makes
// `__v` a local of the ENCLOSING scope — which a binding taken inside a block
// lists as well. The first run of the cases above read `[:__v]` where it wanted
// `[]`: the numbered parameter was already gone and the harness was supplying
// the remaining entry. An assertion written against that output would have
// pinned my wrapper's name as Ruby behaviour.
func TestNumberedParametersAreNotInLocalVariables(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// The two examples numbered_parameters_spec.rb:113-116 opens at 4.0.
		{
			"one numbered parameter",
			`p(-> { _1; binding.local_variables }.call("a"))`,
			"[]",
		},
		{
			"several",
			`p(-> { _2; binding.local_variables }.call("a", "b"))`,
			"[]",
		},
		// ⛔ CONTROL: the list is not simply empty for every block. A real local
		// still appears, so the two cases above measure the hiding and not a
		// #local_variables that stopped working.
		{
			"a real local in the same shape still appears",
			`p(-> { x = 1; binding.local_variables }.call)`,
			"[:x]",
		},
		// ⛔ CONTROL: `it` is blanked from the local table the same way, and
		// this is the 4.0 behaviour rbgo already had before the version moved.
		{
			"it is not listed either",
			`p(-> { it; binding.local_variables }.call("a"))`,
			"[]",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %q\nwant %q", tc.src, got, tc.want)
			}
		})
	}
}
