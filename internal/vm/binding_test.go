package vm_test

import (
	"strings"
	"testing"
)

// TestBinding covers Binding capture, Binding#eval and the local-variable
// introspection/mutation API, asserted against MRI Ruby 4.0.5 semantics.
func TestBinding(t *testing.T) {
	cases := []struct{ src, want string }{
		// eval reads and writes the captured locals (write-back to the frame).
		{`x = 5; b = binding; puts b.eval("x + 1")`, "6\n"},
		{`x = 1; b = binding; b.eval("x = 99"); puts x`, "99\n"},
		{`x = 2; b = binding; b.eval("x = x + 10"); puts x`, "12\n"},
		{`n = 5; b = binding; puts b.eval("n * n")`, "25\n"},
		// eval(str, binding) is the Kernel form of the same thing.
		{`x = 10; puts eval("x * 2", binding)`, "20\n"},
		// local_variable_get / _set / _defined? against captured and injected locals.
		{`x = 7; puts binding.local_variable_get(:x)`, "7\n"},
		{`x = "s"; b = binding; puts b.local_variable_get("x")`, "s\n"}, // String name coerces
		{`b = binding; b.local_variable_set(:z, 42); puts b.local_variable_get(:z)`, "42\n"},
		{`puts binding.local_variable_defined?(:nope)`, "false\n"},
		{`x = 1; puts binding.local_variable_defined?(:x)`, "true\n"},
		// local_variables: declaration order, then MRI's injected-first ordering.
		{`x = 1; y = 2; p binding.local_variables`, "[:x, :y]\n"},
		{`b = binding; p b.local_variables`, "[:b]\n"},
		{`b = binding; b.local_variable_set(:w, "hi"); p b.local_variables`, "[:w, :b]\n"},
		{`b=binding; b.local_variable_set(:w,1); b.local_variable_set(:q,2); p b.local_variables`, "[:q, :w, :b]\n"},
		{`x=1;y=2;b=binding; b.local_variable_set(:z,3); p b.local_variables`, "[:z, :x, :y, :b]\n"},
		{`x=1;b=binding; b.local_variable_set(:x,9); p b.local_variables`, "[:x, :b]\n"}, // set existing: no reorder
		// receiver / class / inspect.
		{`puts binding.receiver == self`, "true\n"},
		{`p binding.class`, "Binding\n"},
		{`p binding.is_a?(Binding)`, "true\n"},
		// A binding captured inside a method sees that method's locals and self.
		{`def m; a = 3; binding; end; b = m; puts b.local_variable_get(:a)`, "3\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`binding.local_variable_get(:nope)`, "local variable 'nope' is not defined"},
		{`binding.local_variable_set(42, 1)`, "42 is not a symbol nor a string"},
		{`binding.local_variable_get(42)`, "42 is not a symbol nor a string"},
		{`binding.eval(42)`, "no implicit conversion of Integer into String"},
		{`binding.eval("(")`, "SyntaxError"},     // parse failure
		{`binding.eval("retry")`, "SyntaxError"}, // parses, fails to compile
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}
