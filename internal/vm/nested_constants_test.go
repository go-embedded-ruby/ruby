package vm_test

import (
	"strings"
	"testing"
)

// Ruby-level coverage for the nested-constant change and adjacent branches that
// the new namespacing touches.

func TestNestedConstantNamespacing(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		// Qualified .name for a class nested in a module.
		{"qualified_name", `module A; class B; end; end; p A::B.name`, "\"A::B\"\n"},
		// Same constant name in two namespaces is two distinct classes.
		{"distinct_namespaces", `module A; class Dup;end;end; module C; class Dup;end;end; p A::Dup.equal?(C::Dup)`, "false\n"},
		// A nested constant shadowing a top-level one stays distinct.
		{"shadows_toplevel", `module M; class File;end;end; p(M::File==File)`, "false\n"},
		// Bare constant inside a nested method resolves by lexical nesting.
		{"lexical_bare_const", `module Outer; X=1; module Inner; def self.get; X; end; end; end; p Outer::Inner.get`, "1\n"},
		// Deeper nesting qualifies the whole path.
		{"deep_qualified", `module A; module B; class C; end; end; end; p A::B::C.name`, "\"A::B::C\"\n"},
		// Reopening a nested class keeps its qualified name.
		{"reopen_keeps_name", `module A; class B; end; end; module A; class B; def x; 1; end; end; end; p A::B.name`, "\"A::B\"\n"},
		// A class nested in a class (not module) qualifies through it too.
		{"nested_in_class", `class A; class B; end; end; p A::B.name`, "\"A::B\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eval(t, tt.src); got != tt.want {
				t.Fatalf("eval(%q) = %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

// TestAnonymousClassToS covers RClass.ToS for an unnamed Class.new (the
// constant change qualifies named classes; an anonymous one falls through to the
// "#<Class>" placeholder rendering).
func TestAnonymousClassToS(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"anon_class", `p Class.new.to_s`, "\"#<Class>\"\n"},
		{"anon_class_inspect", `p Class.new.inspect`, "\"#<Class>\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eval(t, tt.src); got != tt.want {
				t.Fatalf("eval(%q) = %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

// TestBuiltinSubclassToS covers RObject.ToS's builtin-backed arm: binding the
// generic Object#to_s onto an instance of a String subclass routes through
// self.ToS(), which (because the object wraps a built-in value) renders the
// wrapped string. (A plain `.to_s` would dispatch to String#to_s instead.)
func TestBuiltinSubclassToS(t *testing.T) {
	src := `class S < String; end
s = S.new("hi")
m = Object.instance_method(:to_s).bind(s)
p m.call`
	if got := eval(t, src); got != "\"hi\"\n" {
		t.Fatalf("builtin-subclass Object#to_s = %q, want \"hi\"\\n", got)
	}
}

// TestTopLevelConstMissing covers OpGetConstTop's NameError arm (`::Name` where
// Name is not a top-level constant).
func TestTopLevelConstMissing(t *testing.T) {
	src := `begin; ::NoSuchTopLevelConst; rescue NameError => e; puts e.message; end`
	got := eval(t, src)
	if !strings.Contains(got, "uninitialized constant NoSuchTopLevelConst") {
		t.Fatalf("::missing message = %q", got)
	}
}

// TestLoadPathNonArray covers loadPathDirs' non-Array $LOAD_PATH arm: when
// $LOAD_PATH is not an array, the load path contributes no directories and a
// plain require simply fails to find the file.
func TestLoadPathNonArray(t *testing.T) {
	src := `$LOAD_PATH = 42
begin
  require "definitely_no_such_lib_xyz"
rescue LoadError
  puts "caught"
end`
	if got := eval(t, src); got != "caught\n" {
		t.Fatalf("non-array $LOAD_PATH require = %q, want \"caught\\n\"", got)
	}
}
