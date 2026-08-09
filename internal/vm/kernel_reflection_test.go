package vm_test

import "testing"

// TestKernelMethodAndCallee covers Kernel#__method__ and #__callee__ to ruby 4.x
// semantics: the current method name, the original-vs-called distinction for an
// aliased method, transparency through blocks / define_method / a block nested in
// a define_method body / send / eval, and nil in a class body and at the top
// level.
func TestKernelMethodAndCallee(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		// A plain method reports its own name for both.
		{"method_plain", `def m; __method__; end
p m`, ":m\n"},
		{"callee_plain", `def m; __callee__; end
p m`, ":m\n"},
		// An aliased method: __method__ is the original name, __callee__ the alias.
		{"method_aliased", `class C; def f; __method__; end; alias_method :g, :f; end
p C.new.g`, ":f\n"},
		{"callee_aliased", `class C; def f; __callee__; end; alias_method :g, :f; end
p C.new.g`, ":g\n"},
		// Calling through the original name still reports the original for both.
		{"method_alias_orig", `class C; def f; __method__; end; alias_method :g, :f; end
p C.new.f`, ":f\n"},
		{"callee_alias_orig", `class C; def f; __callee__; end; alias_method :g, :f; end
p C.new.f`, ":f\n"},
		// An alias of an alias keeps pointing at the very first original.
		{"method_alias_chain", `class C; def f; __method__; end; alias_method :g, :f; alias_method :h, :g; end
p C.new.h`, ":f\n"},
		{"callee_alias_chain", `class C; def f; __callee__; end; alias_method :g, :f; alias_method :h, :g; end
p C.new.h`, ":h\n"},
		// A block is transparent: __method__ reports the enclosing method.
		{"method_in_block", `def m; [1].map { __method__ }.first; end
p m`, ":m\n"},
		{"callee_in_block", `def m; [1].map { __callee__ }.first; end
p m`, ":m\n"},
		// define_method bodies report the defined method name.
		{"method_define_method", `class C; define_method(:dm) { __method__ }; end
p C.new.dm`, ":dm\n"},
		{"callee_define_method", `class C; define_method(:dm) { __callee__ }; end
p C.new.dm`, ":dm\n"},
		// A block nested in a define_method body inherits that method name.
		{"method_block_in_dm", `class C; define_method(:dm) { [1].map { __method__ }.first }; end
p C.new.dm`, ":dm\n"},
		{"callee_block_in_dm", `class C; define_method(:dm) { [1].map { __callee__ }.first }; end
p C.new.dm`, ":dm\n"},
		// send is transparent: the name is that of the sent method.
		{"method_from_send", `def m; send(:helper); end
def helper; __method__; end
p m`, ":helper\n"},
		// eval is transparent: eval'd code inherits the caller's method.
		{"method_from_eval", `def m; eval("__method__"); end
p m`, ":m\n"},
		{"callee_from_eval", `def m; eval("__callee__"); end
p m`, ":m\n"},
		// nil inside a class body (no enclosing method).
		{"method_class_body", `class C; $x = __method__; end
p $x`, "nil\n"},
		{"callee_class_body", `class C; $x = __callee__; end
p $x`, "nil\n"},
		// nil at the top level.
		{"method_top_level", `p __method__`, "nil\n"},
		{"callee_top_level", `p __callee__`, "nil\n"},
		// A block at the top level (no enclosing method) is still nil.
		{"method_top_block", `p([1].map { __method__ }.first)`, "nil\n"},
		// Kernel exposes them as a private instance method and a public module method.
		{"method_private_on_kernel", `p Kernel.private_instance_methods(false).include?(:__method__)`, "true\n"},
		{"callee_private_on_kernel", `p Kernel.private_instance_methods(false).include?(:__callee__)`, "true\n"},
		{"method_public_on_kernel", `p Kernel.public_methods(false).include?(:__method__)`, "true\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestKernelMethodIntrospection covers #methods / #public_methods /
// #private_methods / #protected_methods for both a class receiver (its singleton
// / class methods) and an instance receiver (its instance methods), the all-arg
// selecting inherited-vs-own, and #methods excluding private methods — matched to
// ruby 4.x.
func TestKernelMethodIntrospection(t *testing.T) {
	// A shared fixture with one method of each visibility on both the class and
	// instance level, plus a subclass, so the all-arg and class/instance splits
	// are all exercised.
	const fix = `class Base
  def ipub; end
  protected
  def ipro; end
  private
  def ipri; end
  public
  def self.spub; end
end
class Sub < Base
  def subpub; end
  private
  def subpri; end
end
`
	tests := []struct {
		name, src, want string
	}{
		// Instance #methods excludes private methods but keeps protected ones.
		{"methods_excludes_private", fix + `m = Sub.new.methods
p [m.include?(:ipub), m.include?(:ipro), m.include?(:ipri), m.include?(:subpri)]`,
			"[true, true, false, false]\n"},
		// #methods(false) on a plain instance with no singleton methods is empty.
		{"methods_false_empty", fix + `p Sub.new.methods(false)`, "[]\n"},
		// #methods(false) lists only the object's own singleton methods.
		{"methods_false_singleton", fix + `o = Sub.new
def o.only_here; end
p o.methods(false)`, "[:only_here]\n"},
		// A class's #methods(false) lists its own singleton (class) methods.
		{"methods_false_class", fix + `p Base.methods(false).include?(:spub)`, "true\n"},
		// #public_methods keeps public, drops protected and private.
		{"public_methods_instance", fix + `m = Sub.new.public_methods
p [m.include?(:ipub), m.include?(:ipro), m.include?(:ipri)]`,
			"[true, false, false]\n"},
		// #private_methods on an instance: own and inherited private methods.
		{"private_methods_instance", fix + `m = Sub.new.private_methods
p [m.include?(:subpri), m.include?(:ipri), m.include?(:ipub)]`,
			"[true, true, false]\n"},
		// The all=false form restricts private_methods to the receiver's own.
		{"private_methods_own", fix + `m = Sub.new.private_methods(false)
p [m.include?(:subpri), m.include?(:ipri)]`, "[true, false]\n"},
		// #protected_methods on an instance: the protected method, not public/private.
		{"protected_methods_instance", fix + `m = Sub.new.protected_methods
p [m.include?(:ipro), m.include?(:ipub), m.include?(:ipri)]`,
			"[true, false, false]\n"},
		// protected_methods(false) on the subclass (which defines none of its own)
		// is empty; the inherited one needs all=true.
		{"protected_methods_own_empty", fix + `p Sub.new.protected_methods(false)`, "[]\n"},
		// A private class method appears in the class's #private_methods.
		{"private_methods_class", fix + `class Base; class << self; private; def spri; end; end; end
p Base.private_methods(false).include?(:spri)`, "true\n"},
		// Symbols, and #methods is not sorted-guaranteed but contains the names —
		// assert membership rather than order, matching MRI's unspecified order.
		{"public_methods_class", fix + `p Base.public_methods.include?(:spub)`, "true\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestKernelInstanceVariableReflection covers the instance-variable reflection
// surface (#instance_variable_get / _set / _defined? / #remove_instance_variable
// / #instance_variables) including the NameError raised for a missing removal and
// the "not allowed as an instance variable name" validation, to ruby 4.x.
func TestKernelInstanceVariableReflection(t *testing.T) {
	valueTests := []struct {
		name, src, want string
	}{
		{"ivar_get_set", `o = Object.new
o.instance_variable_set(:@x, 42)
p o.instance_variable_get(:@x)`, "42\n"},
		{"ivar_get_missing_nil", `p Object.new.instance_variable_get(:@x)`, "nil\n"},
		{"ivar_defined_true", `o = Object.new
o.instance_variable_set(:@x, 1)
p o.instance_variable_defined?(:@x)`, "true\n"},
		{"ivar_defined_false", `p Object.new.instance_variable_defined?(:@x)`, "false\n"},
		{"ivar_remove_returns_value", `o = Object.new
o.instance_variable_set(:@x, 7)
p o.remove_instance_variable(:@x)`, "7\n"},
		{"ivar_remove_then_undefined", `o = Object.new
o.instance_variable_set(:@x, 7)
o.remove_instance_variable(:@x)
p o.instance_variable_defined?(:@x)`, "false\n"},
		{"ivars_list", `o = Object.new
o.instance_variable_set(:@a, 1)
o.instance_variable_set(:@b, 2)
p o.instance_variables`, "[:@a, :@b]\n"},
		{"ivar_set_string_name", `o = Object.new
o.instance_variable_set("@y", 9)
p o.instance_variable_get("@y")`, "9\n"},
	}
	for _, tc := range valueTests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}

	errTests := []struct {
		name, src, wantClass, wantMsg string
	}{
		{"remove_missing_ivar", `Object.new.remove_instance_variable(:@nope)`,
			"NameError", "instance variable @nope not defined"},
		{"get_invalid_name", `Object.new.instance_variable_get(:foo)`,
			"NameError", "not allowed as an instance variable name"},
		{"set_invalid_name", `Object.new.instance_variable_set(:foo, 1)`,
			"NameError", "not allowed as an instance variable name"},
	}
	for _, tc := range errTests {
		t.Run(tc.name, func(t *testing.T) {
			assertRaise(t, tc.src, tc.wantClass, tc.wantMsg)
		})
	}
}
