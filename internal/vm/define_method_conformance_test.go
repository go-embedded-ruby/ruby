package vm_test

import (
	"strings"
	"testing"
)

// TestDefineMethodArityConformance covers the lambda argument-arity semantics a
// method created by define_method enforces: a count mismatch raises ArgumentError
// and a lone Array argument is never auto-splat destructured (MRI 4.0.6).
func TestDefineMethodArityConformance(t *testing.T) {
	arityErr := []struct{ name, src string }{
		{"zero_arg_required", `Class.new { define_method(:m) { |a| a } }.new.m`},
		{"two_args_for_one", `Class.new { define_method(:m) { |a| a } }.new.m(1, 2)`},
		{"one_arg_for_zero", `Class.new { define_method(:m) { :x } }.new.m(1)`},
		{"opt_default_zero", `Class.new { define_method(:m) { |a, b = 1| a } }.new.m`},
		{"opt_default_three", `Class.new { define_method(:m) { |a, b = 1| a } }.new.m(1, 2, 3)`},
		{"proc_zero_param", `Class.new { p = Proc.new { || true }; define_method(:m, &p) }.new.m(:x)`},
	}
	for _, tc := range arityErr {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), "ArgumentError") {
				t.Errorf("src=%q: got err=%v, want ArgumentError", tc.src, err)
			}
		})
	}

	// A lone Array argument binds whole to the single parameter (no auto-splat).
	if got := eval(t, `p Class.new { define_method(:m) { |a,| a } }.new.m([1, 2])`); got != "[1, 2]\n" {
		t.Errorf("no-auto-splat: got %q, want %q", got, "[1, 2]\n")
	}
	// Optional and splat parameters still bind normally under the strict check.
	if got := eval(t, `p Class.new { define_method(:m) { |a, b = 9| [a, b] } }.new.m(1)`); got != "[1, 9]\n" {
		t.Errorf("optional default: got %q, want %q", got, "[1, 9]\n")
	}
	if got := eval(t, `p Class.new { define_method(:m) { |a, *b| [a, b] } }.new.m(1, 2, 3)`); got != "[1, [2, 3]]\n" {
		t.Errorf("splat: got %q, want %q", got, "[1, [2, 3]]\n")
	}
}

// TestDefineMethodVisibilityConformance covers the visibility a define_method-ed
// method receives: the current default when the definee is the receiver, public
// when defined from another module, and always-private for :initialize.
func TestDefineMethodVisibilityConformance(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"target_module_block", `
			k = Class.new do
				define_method(:pub) {}
				private
				define_method(:priv) {}
			end
			p k.public_instance_methods(false).include?(:pub)
			p k.private_instance_methods(false).include?(:priv)`, "true\ntrue\n"},
		{"another_module_public", `
			k = Class.new
			Class.new do
				k.send(:define_method, :a) {}
				private
				k.send(:define_method, :b) {}
			end
			p k.public_instance_methods(false).include?(:a)
			p k.public_instance_methods(false).include?(:b)`, "true\ntrue\n"},
		{"initialize_private_block", `
			k = Class.new { define_method(:initialize) {} }
			p k.private_instance_methods(false).include?(:initialize)`, "true\n"},
		{"initialize_private_unbound", `
			k = Class.new do
				def t; end
				define_method(:initialize, instance_method(:t))
			end
			p k.private_instance_methods(false).include?(:initialize)`, "true\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestDefineMethodErrorsConformance covers define_method's error paths: name
// coercion, the non-Proc/Method type error, frozen receivers, and the cross-class
// bind checks for a transplanted Method/UnboundMethod.
func TestDefineMethodErrorsConformance(t *testing.T) {
	cases := []struct{ name, src, wantClass, wantMsg string }{
		{"not_proc_string", `Class.new { define_method(:m, "s") }`,
			"TypeError", "wrong argument type String (expected Proc/Method/UnboundMethod)"},
		{"not_proc_nil", `Class.new { define_method(:m, nil) }`,
			"TypeError", "wrong argument type NilClass (expected Proc/Method/UnboundMethod)"},
		{"not_proc_int", `Class.new { define_method(:m, 1234) }`,
			"TypeError", "wrong argument type Integer (expected Proc/Method/UnboundMethod)"},
		{"no_block", `Class.new { define_method(:m) }`,
			"ArgumentError", ""},
		{"name_not_sym", `Class.new { define_method(1001, ->{}) }`,
			"TypeError", "is not a symbol nor a string"},
		{"to_str_non_string", `
			o = Object.new
			def o.to_str() [] end
			Class.new { define_method(o, ->{}) }`,
			"TypeError", "can't convert Object to String"},
		{"frozen", `Class.new { freeze; define_method(:m) {} }`,
			"FrozenError", ""},
		{"unbound_unrelated_class", `
			c = Class.new { def foo; :x; end }
			Class.new { define_method(:bar, c.instance_method(:foo)) }`,
			"TypeError", "bind argument must be a subclass of"},
		{"unbound_child_on_parent", `
			P = Class.new { define_method(:foo) { :bar } }
			C = Class.new(P) { define_method(:foo) { :baz } }
			P.send(:define_method, :foo, C.instance_method(:foo))`,
			"TypeError", "bind argument must be a subclass of"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil {
				t.Fatalf("src=%q: expected %s, got nil", tc.src, tc.wantClass)
			}
			if !strings.Contains(err.Error(), tc.wantClass) {
				t.Errorf("src=%q: got err=%v, want class %s", tc.src, err, tc.wantClass)
			}
			if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("src=%q: got err=%v, want message %q", tc.src, err, tc.wantMsg)
			}
		})
	}
}

// TestDefineMethodBodyConformance covers the successful body forms: name coercion
// via #to_str, a Method/UnboundMethod taking precedence over a passed block, and
// the method_added hook firing once after the method is installed.
func TestDefineMethodBodyConformance(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"proc_precedes_block", `
			k = Class.new do
				define_method(:m, -> { :from_method }) { :from_block }
			end
			p k.new.m`, ":from_method\n"},
		{"to_str_name", `
			o = Object.new
			def o.to_str() "foo" end
			k = Class.new { define_method(o, -> { :ok }) }
			p k.new.foo`, ":ok\n"},
		{"unbound_transplant", `
			k = Class.new do
				def orig(a, b); [a, b]; end
				define_method(:copy, instance_method(:orig))
			end
			p k.new.copy(1, 2)`, "[1, 2]\n"},
		{"method_added_fires", `
			$added = []
			k = Class.new do
				def self.method_added(n); $added << n; end
				define_method(:hooked) { true }
			end
			p $added`, "[:hooked]\n"},
		{"returns_symbol", `p Class.new { }.send(:define_method, "n") { true }`, ":n\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}
