package vm_test

import (
	"strings"
	"testing"
)

// TestMethodInspectHeads covers the receiver/owner head rendering of
// Method#inspect (vm.methodHead) across MRI's cases: a genuine class method
// (Recv.name / Child(Parent).name), a module method reaching a class object via
// its metaclass (#<Class:Recv>(Owner)#name), an ordinary instance method
// (Recv#name and Recv(Owner)#name), an anonymous class instance, and a
// per-object singleton method.
func TestMethodInspectHeads(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{
			"def_self_class_method",
			`class WHClsP; def self.cm; end; end
puts WHClsP.method(:cm).inspect.start_with?("#<Method: WHClsP.cm")`,
			"true\n",
		},
		{
			"inherited_class_method_shows_owner",
			`class WHClsP2; def self.cm; end; end
class WHClsC2 < WHClsP2; end
puts WHClsC2.method(:cm).inspect.start_with?("#<Method: WHClsC2(WHClsP2).cm")`,
			"true\n",
		},
		{
			"module_instance_method_on_class_via_metaclass",
			`puts String.method(:include).inspect.start_with?("#<Method: #<Class:String>(Module)#include")`,
			"true\n",
		},
		{
			"extended_module_method_on_class_via_metaclass",
			`m = Module.new { def whbar; end }
c = Class.new
c.extend(m)
s = c.method(:whbar).inspect
puts s.start_with?("#<Method: #<Class:#{c.inspect}>(#{m.inspect})#whbar")`,
			"true\n",
		},
		{
			"ordinary_instance_method_same_class",
			`class WHOrd; def foo; end; end
puts WHOrd.new.method(:foo).inspect.start_with?("#<Method: WHOrd#foo")`,
			"true\n",
		},
		{
			"ordinary_instance_method_from_module_owner",
			`module WHMod; def bar; end; end
class WHInc; include WHMod; end
puts WHInc.new.method(:bar).inspect.start_with?("#<Method: WHInc(WHMod)#bar")`,
			"true\n",
		},
		{
			"anonymous_class_instance_uses_hash_separator",
			`k = Class.new { def orig; end; alias_method :ren, :orig }
puts k.new.method(:ren).inspect.include?("#ren(orig)")`,
			"true\n",
		},
		{
			"per_object_singleton_method_uses_dot",
			`o = Object.new
def o.foo; end
puts o.method(:foo).inspect.start_with?("#<Method: #<Object")
puts o.method(:foo).inspect.include?(">.foo")`,
			"true\ntrue\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestUnboundMethodInspectHead covers UnboundMethod#inspect head rendering: a
// named module owner inspects to its name, a per-object singleton owner inspects
// to its #<Class> form.
func TestUnboundMethodInspectHead(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{
			"named_module_owner",
			`module WHUMod; def bar; end; end
class WHUInc; include WHUMod; end
puts WHUInc.instance_method(:bar).inspect.start_with?("#<UnboundMethod: WHUMod#bar")`,
			"true\n",
		},
		{
			"per_object_singleton_owner",
			`o = Object.new
def o.foo; end
u = o.method(:foo).unbind
puts u.inspect.start_with?("#<UnboundMethod: #{o.singleton_class}#foo")`,
			"true\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// inheritedModulesSrc builds two private modules whose #derp forms a super chain
// (B#derp calls super into A#derp) both included into a class C, with a public
// visibility change and an alias — the JRuby #7240 scenario. super_method must
// follow the ORIGINAL definition across the two modules (reaching A, skipping B)
// and must not stop at the alias name.
const inheritedModulesSrc = `module WHA; private; def derp(m); "A"; end; end
module WHB; private; def derp; "B" + super("s"); end; end
class WHC; include WHA; include WHB; public :derp; alias_method :meow, :derp; end
`

func TestSuperMethodFollowsOriginalName(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{
			"bound_visibility_changed_super",
			inheritedModulesSrc + `puts WHC.new.method(:derp).super_method.owner`,
			"WHA\n",
		},
		{
			"bound_aliased_super_owner",
			inheritedModulesSrc + `puts WHC.new.method(:meow).super_method.owner`,
			"WHA\n",
		},
		{
			"unbound_visibility_changed_super",
			inheritedModulesSrc + `puts WHC.instance_method(:derp).super_method.owner`,
			"WHA\n",
		},
		{
			"unbound_aliased_super_owner",
			inheritedModulesSrc + `puts WHC.instance_method(:meow).super_method.owner`,
			"WHA\n",
		},
		{
			"unbound_super_method_nil_at_top_returns_nil",
			inheritedModulesSrc + `puts WHC.instance_method(:derp).super_method.super_method.inspect`,
			"nil\n",
		},
		{
			"bound_super_method_nil_at_top_returns_nil",
			inheritedModulesSrc + `puts WHC.new.method(:derp).super_method.super_method.inspect`,
			"nil\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestMethodValueStateCopyAndFreeze covers instance variables, freeze/frozen?
// and #dup/#clone copy semantics for BoundMethod and UnboundMethod: #dup and
// #clone copy instance variables, #dup resets the frozen flag while #clone
// preserves it, and setting an ivar on a frozen method raises FrozenError.
func TestMethodValueStateCopyAndFreeze(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{
			"bound_ivars_set_and_read",
			`m = Object.new.method(:method)
m.instance_variable_set(:@a, 1)
m.instance_variable_set(:@b, 2)
puts m.instance_variables.inspect
puts m.instance_variable_get(:@a)`,
			"[:@a, :@b]\n1\n",
		},
		{
			"unbound_ivars_set_and_read",
			`u = Object.instance_method(:method)
u.instance_variable_set(:@x, 9)
puts u.instance_variables.inspect
puts u.instance_variable_get(:@x)`,
			"[:@x]\n9\n",
		},
		{
			"bound_dup_copies_ivars_resets_frozen",
			`m = Object.new.method(:method)
m.instance_variable_set(:@ivar, 1)
m.freeze
d = m.dup
puts d.instance_variables.inspect
puts d.frozen?`,
			"[:@ivar]\nfalse\n",
		},
		{
			"bound_clone_preserves_frozen",
			`m = Object.new.method(:method)
m.freeze
puts m.frozen?
puts m.clone.frozen?`,
			"true\ntrue\n",
		},
		{
			"unbound_clone_preserves_frozen",
			`u = Object.instance_method(:method)
u.freeze
puts u.frozen?
puts u.clone.frozen?
puts u.dup.frozen?`,
			"true\ntrue\nfalse\n",
		},
		{
			"dup_without_ivars_is_distinct",
			`m = Object.new.method(:method)
d = m.dup
puts m.equal?(d)
puts m == d
puts d.instance_variables.inspect`,
			"false\ntrue\n[]\n",
		},
		{
			// A non-boxed, non-listed value (a Proc) still travels the freeze/frozen?
			// default fall-through without being affected, so the added branches stay
			// scoped to Bound/UnboundMethod.
			"proc_freeze_is_untracked_fall_through",
			`p = proc { 1 }
p.freeze
puts p.frozen?`,
			"false\n",
		},
		{
			"set_ivar_on_frozen_method_raises",
			`m = Object.new.method(:method)
m.freeze
begin
  m.instance_variable_set(:@z, 1)
  puts "no-raise"
rescue FrozenError
  puts "frozen"
end`,
			"frozen\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestSuperMethodPlainInheritance keeps the ordinary class-inheritance chain
// working (no alias, no module owners): super_method walks class supers.
func TestSuperMethodPlainInheritance(t *testing.T) {
	src := `class WHP; def m; end; end
class WHQ < WHP; def m; end; end
class WHR < WHQ; def m; end; end
puts WHR.new.method(:m).super_method.owner
puts WHR.instance_method(:m).super_method.owner
puts WHR.instance_method(:m).super_method.super_method.owner`
	got := eval(t, src)
	want := "WHQ\nWHQ\nWHP\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
	if strings.Contains(got, "nil") {
		t.Errorf("unexpected nil in super_method chain: %q", got)
	}
}
