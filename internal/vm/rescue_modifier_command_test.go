package vm_test

import "testing"

// Regression for the modifier-`rescue`-after-a-command-call precedence gap
// closed by go-ruby-parser v0.1.2. A command call (no parentheses around the
// arguments) followed by a modifier `rescue` must parse as `(cmd args) rescue
// x` — the rescue wrapping the whole call — rather than binding to the last
// argument. `def ... end rescue nil` must likewise wrap the whole definition.
//
// Every `want` below is verified against MRI ruby 4.0.5.
func TestRescueModifierCommandCall(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// `raise "boom"` is a bare command call; the modifier rescues the
		// whole call, so the begin block yields nil.
		{"command_raise_rescued_nil", `p(begin; raise "boom" rescue nil; end)`, "nil\n"},
		// Rescue supplies the fallback value of the whole raising call.
		{"command_raise_rescued_value", `x = (begin; raise "x" rescue 42; end); p x`, "42\n"},
		// Rescued call is a no-op statement; execution continues.
		{"command_raise_rescued_continues", `begin; raise "x" rescue nil; end; p :ok`, ":ok\n"},
		// Sanity: the parenthesized command form was always fine and still is.
		{"paren_command_still_ok", `puts("a") rescue nil`, "a\n"},
		// `def ... end rescue nil` wraps the whole definition, which succeeds,
		// so foo is defined and callable.
		{"def_end_rescue_defines", `def foo; 1; end rescue nil; p foo`, "1\n"},
		// Positive control: the bare modifier DOES catch a StandardError.
		{"standard_error_caught", `p(raise(StandardError, "x") rescue :caught)`, ":caught\n"},
		// Negative: a non-StandardError (bare Exception) is NOT caught by the
		// modifier rescue and propagates past it; the outer rescue Exception
		// clause catches it, proving `:caught` never bound.
		{"non_standard_error_propagates", "r = begin\n  (raise(Exception, \"x\") rescue :caught)\n  :never\nrescue Exception\n  :propagated\nend\np r", ":propagated\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
