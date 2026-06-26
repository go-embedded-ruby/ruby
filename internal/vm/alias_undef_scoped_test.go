package vm_test

import (
	"strings"
	"testing"
)

// alias makes a new name resolve to an existing method, including symbol and
// operator items, and works for a method inherited from a superclass.
func TestAlias(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"bare", "class C; def a; 1; end; alias b a; end; p C.new.b", "1\n"},
		{"symbols", "class C; def a; 1; end; alias :b :a; end; p C.new.b", "1\n"},
		{"operator", "class C; def ==(o); true; end; alias eql? ==; end; p C.new.eql?(1)", "true\n"},
		{"inherited", "class A; def a; 7; end; end\nclass B < A; alias b a; end\np B.new.b", "7\n"},
		{"respond_to", "class C; def a; 1; end; alias b a; end; p C.new.respond_to?(:b)", "true\n"},
		{"redefine_original_keeps_alias", "class C; def a; 1; end; alias b a; def a; 2; end; end\nc = C.new\np [c.a, c.b]", "[2, 1]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// alias of a global variable copies the source global's current value.
func TestAliasGlobal(t *testing.T) {
	if got := eval(t, "$x = 5\nalias $y $x\np $y"); got != "5\n" {
		t.Errorf("global alias: got %q", got)
	}
	// The new-name-only `$` form (old name is a bare value source) also routes
	// through the global path.
	if got := eval(t, "$x = 9\nalias $z $x\n$z = 3\np [$x, $z]"); got != "[9, 3]\n" {
		t.Errorf("global alias independence: got %q", got)
	}
}

// alias of a name that resolves nowhere raises NameError.
func TestAliasMissing(t *testing.T) {
	err := runErr(t, "class C; alias b nope; end")
	if err == nil || !strings.Contains(err.Error(), "NameError") {
		t.Fatalf("got %v, want NameError", err)
	}
}

// undef removes a method: instance_methods no longer lists it, respond_to? is
// false, and a call raises NoMethodError — even for an inherited method.
func TestUndef(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"own", "class C; def a; 1; end; undef a; end; p C.instance_methods(false)", "[]\n"},
		{"multi", "class C; def a; 1; end; def b; 2; end; undef a, b; end; p C.instance_methods(false)", "[]\n"},
		{"respond_to", "class C; def a; 1; end; undef a; end; p C.new.respond_to?(:a)", "false\n"},
		{"hides_inherited_list", "class A; def a; 1; end; end\nclass B < A; undef a; end\np B.instance_methods(false)", "[]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// A call to an undef-ed method (own or inherited) raises NoMethodError.
func TestUndefRaisesOnCall(t *testing.T) {
	for _, src := range []string{
		"class C; def a; 1; end; undef a; end; C.new.a",
		"class A; def a; 1; end; end\nclass B < A; undef a; end\nB.new.a",
	} {
		err := runErr(t, src)
		if err == nil || !strings.Contains(err.Error(), "NoMethodError") {
			t.Fatalf("src=%q got %v, want NoMethodError", src, err)
		}
	}
}

// undef of a name that resolves nowhere raises NameError.
func TestUndefMissing(t *testing.T) {
	err := runErr(t, "class C; undef nope; end")
	if err == nil || !strings.Contains(err.Error(), "NameError") {
		t.Fatalf("got %v, want NameError", err)
	}
}

// An undef-ed method can be looked up again (Object#method) only after a fresh
// definition; before that it is treated as absent.
func TestUndefThenMethodLookup(t *testing.T) {
	err := runErr(t, "class C; def a; 1; end; undef a; end\nC.new.method(:a)")
	if err == nil || !strings.Contains(err.Error(), "NameError") {
		t.Fatalf("got %v, want NameError for method(:a) after undef", err)
	}
	if got := eval(t, "class C; def a; 1; end; undef a; def a; 2; end; end\np C.new.method(:a).call"); got != "2\n" {
		t.Errorf("redefine after undef: got %q", got)
	}
}

// Scope::NAME = value sets the constant on the named module/class and is read
// back through the scope; the leading ::NAME form sets a top-level constant.
func TestScopedConstAssign(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"module", "module M; end; M::X = 5; p M::X", "5\n"},
		{"class", "class C; end; C::Y = 7; p C::Y", "7\n"},
		{"value", "module M; end; p(M::X = 9)", "9\n"},
		{"toplevel", "::FOO = 7; p ::FOO", "7\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// Assigning a scoped constant whose scope is not a class/module raises TypeError.
func TestScopedConstAssignNonModule(t *testing.T) {
	err := runErr(t, "X = 5\nX::Y = 1")
	if err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("got %v, want TypeError", err)
	}
}

// A `*splat` on the right-hand side of a multiple assignment splat-expands into
// the value list (a bare splat, a splat followed by values, and a splat target).
func TestMultiAssignSplatRHS(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"bare", "a, b = *[1, 2]; p [a, b]", "[1, 2]\n"},
		{"splat_then_value", "a, b, c = *[1, 2], 3; p [a, b, c]", "[1, 2, 3]\n"},
		{"value_then_splat", "a, b, c = 1, *[2, 3]; p [a, b, c]", "[1, 2, 3]\n"},
		{"splat_target", "a, *b = *[1, 2, 3]; p [a, b]", "[1, [2, 3]]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
