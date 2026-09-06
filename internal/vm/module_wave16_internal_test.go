// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"io"
	"testing"
)

// TestWave16NameCoercion covers coerceNameArg / coerceConstName / coerceCvarName
// through the Module methods that use them: Symbol and String taken directly, a
// #to_str-bearing object converted, and the two TypeError paths (no #to_str, and
// a #to_str returning a non-String). Verified against MRI (ruby 4.0.5).
func TestWave16NameCoercion(t *testing.T) {
	ok := []struct{ src, want string }{
		{"p String.method_defined?(:upcase)", "true\n"},
		{"p String.method_defined?(\"upcase\")", "true\n"},
		{"o = Object.new\ndef o.to_str; \"upcase\"; end\np String.method_defined?(o)", "true\n"},
		// coerceConstName / coerceCvarName happy paths.
		{"m = Module.new\nm.const_set(:Foo, 7)\np m.const_get(:Foo)", "7\n"},
		{"c = Class.new\nc.class_variable_set(:@@x, 5)\np c.class_variable_get(:@@x)", "5\n"},
	}
	for _, c := range ok {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("%q: got %q want %q", c.src, got, c.want)
		}
	}
	errs := []struct{ src, class, msg string }{
		{"String.method_defined?(123)", "TypeError", "123 is not a symbol nor a string"},
		{"o = Object.new\ndef o.to_str; 123; end\nString.method_defined?(o)", "TypeError",
			"can't convert Object to String (Object#to_str gives Integer)"},
		{"Module.new.const_set(\"x\", 1)", "NameError", "wrong constant name x"},
		{"Module.new.const_set(\"Name=\", 1)", "NameError", "wrong constant name Name="},
		{"Class.new.class_variable_get(:foo)", "NameError", "`foo' is not allowed as a class variable name"},
		{"Class.new.class_variable_get(:@@)", "NameError", "`@@' is not allowed as a class variable name"},
	}
	for _, c := range errs {
		if cls, msg := evalErr(t, c.src); cls != c.class || msg != c.msg {
			t.Errorf("%q: got %s/%q want %s/%q", c.src, cls, msg, c.class, c.msg)
		}
	}
}

// TestWave16RemoveClassVariable covers Module#remove_class_variable's name
// coercion (cvarNameArg): a non-Symbol/String argument raises TypeError and a
// malformed name raises NameError, while a valid name returns the removed value.
func TestWave16RemoveClassVariable(t *testing.T) {
	if got := eval(t, "c = Class.new\nc.class_variable_set(:@@x, 9)\np c.send(:remove_class_variable, :@@x)"); got != "9\n" {
		t.Errorf("remove_class_variable value: got %q", got)
	}
	if cls, _ := evalErr(t, "Class.new.send(:remove_class_variable, 123)"); cls != "TypeError" {
		t.Errorf("remove_class_variable non-name: got %s", cls)
	}
	if cls, _ := evalErr(t, "Class.new.send(:remove_class_variable, :foo)"); cls != "NameError" {
		t.Errorf("remove_class_variable malformed: got %s", cls)
	}
}

// TestWave16FrozenAndRemoveConst covers the frozen guards on Module#const_set and
// #class_variable_set and the autoload-entry removal path of #remove_const.
func TestWave16FrozenAndRemoveConst(t *testing.T) {
	errs := []struct{ src, class string }{
		{"m = Module.new.freeze\nm.const_set(:X, 1)", "FrozenError"},
		{"c = Class.new.freeze\nc.class_variable_set(:@@x, 1)", "FrozenError"},
		{"Module.new.send(:remove_const, :Nope)", "NameError"},
	}
	for _, c := range errs {
		if cls, _ := evalErr(t, c.src); cls != c.class {
			t.Errorf("%q: got %s want %s", c.src, cls, c.class)
		}
	}
	// remove_const drops a pending autoload and returns nil; a real constant comes
	// back as its value.
	if got := eval(t, "m = Module.new\nm.autoload(:AC, \"/no/such.rb\")\np m.send(:remove_const, :AC)"); got != "nil\n" {
		t.Errorf("remove_const autoload: got %q want nil", got)
	}
	if got := eval(t, "m = Module.new\nm.const_set(:V, 42)\np m.send(:remove_const, :V)"); got != "42\n" {
		t.Errorf("remove_const value: got %q want 42", got)
	}
}

// TestWave16IncludePrependValidation covers checkModuleArgs (no arguments, a
// non-Module, a Class, a refinement) for both Module#include and #prepend, and
// Module#include?'s Module-argument requirement.
func TestWave16IncludePrependValidation(t *testing.T) {
	errs := []struct{ src, class, msg string }{
		{"Module.new.include", "ArgumentError", "wrong number of arguments (given 0, expected 1+)"},
		{"Module.new.include(Object.new)", "TypeError", "wrong argument type Object (expected Module)"},
		{"Module.new.include(String)", "TypeError", "wrong argument type Class (expected Module)"},
		{"Module.new.prepend", "ArgumentError", "wrong number of arguments (given 0, expected 1+)"},
		{"Module.new.prepend(String)", "TypeError", "wrong argument type Class (expected Module)"},
		{"String.include?(1)", "TypeError", "wrong argument type Integer (expected Module)"},
		{"String.include?(Integer)", "TypeError", "wrong argument type Class (expected Module)"},
		{"c = String\nr = nil\nModule.new{ r = refine(c){} }\nModule.new.include(r)", "TypeError", "Cannot include refinement"},
		{"c = String\nr = nil\nModule.new{ r = refine(c){} }\nModule.new.prepend(r)", "TypeError", "Cannot prepend refinement"},
	}
	for _, c := range errs {
		if cls, msg := evalErr(t, c.src); cls != c.class || msg != c.msg {
			t.Errorf("%q: got %s/%q want %s/%q", c.src, cls, msg, c.class, c.msg)
		}
	}
	// include dispatches append_features + included in REVERSE argument order
	// (the overrides here record the call order without mixing in).
	src := `order = []
a = Module.new { define_singleton_method(:append_features) { |b| order << :af_a }; define_singleton_method(:included) { |b| order << :inc_a } }
z = Module.new { define_singleton_method(:append_features) { |b| order << :af_z }; define_singleton_method(:included) { |b| order << :inc_z } }
Class.new.include(a, z)
p order`
	if got := eval(t, src); got != "[:af_z, :inc_z, :af_a, :inc_a]\n" {
		t.Errorf("include reverse dispatch: got %q", got)
	}
}

// TestWave16FeatureMethods covers Module#append_features / #prepend_features: a
// direct mix-in, the cyclic-include guard, the frozen-target guard, a non-Module
// argument, and a non-Module self (an append_features rebound onto a Class).
func TestWave16FeatureMethods(t *testing.T) {
	if got := eval(t, "m = Module.new{ def hi; :hi; end }\nc = Class.new\nm.send(:append_features, c)\np c.new.hi"); got != ":hi\n" {
		t.Errorf("append_features direct: got %q", got)
	}
	if got := eval(t, "m = Module.new{ def hi; :hi; end }\nc = Class.new\nm.send(:prepend_features, c)\np c.new.hi"); got != ":hi\n" {
		t.Errorf("prepend_features direct: got %q", got)
	}
	errs := []struct{ src, class, msg string }{
		{"module CyA; end\nmodule CyB; include CyA; end\nCyA.include(CyB)", "ArgumentError", "cyclic include detected"},
		{"m = Module.new\nc = Class.new.freeze\nm.send(:append_features, c)", "FrozenError", ""},
		{"Module.new.send(:append_features, 5)", "TypeError", "wrong argument type Integer (expected Module)"},
		{"Module.new.send(:prepend_features, 5)", "TypeError", "wrong argument type Integer (expected Module)"},
		{"Module.instance_method(:append_features).bind(Class.new).call(Module.new)", "TypeError", "wrong argument type Class (expected Module)"},
	}
	for _, c := range errs {
		if cls, msg := evalErr(t, c.src); cls != c.class || (c.msg != "" && msg != c.msg) {
			t.Errorf("%q: got %s/%q want %s/%q", c.src, cls, msg, c.class, c.msg)
		}
	}
	// append_features / prepend_features are undefined on Class.
	if got := eval(t, "p Class.private_instance_methods(true).include?(:append_features)"); got != "false\n" {
		t.Errorf("Class append_features undefined: got %q", got)
	}
}

// TestWave16Hooks covers the default no-op mix-in / definition hooks, each of
// which returns nil, and confirms they are private instance methods of Module.
func TestWave16Hooks(t *testing.T) {
	for _, h := range []string{"included", "extended", "prepended", "method_added", "method_removed", "method_undefined", "const_added"} {
		if got := eval(t, "p Module.new.send(:"+h+", Object)"); got != "nil\n" {
			t.Errorf("%s default: got %q want nil", h, got)
		}
		if got := eval(t, "p Module.private_instance_methods(false).include?(:"+h+")"); got != "true\n" {
			t.Errorf("%s not private: got %q", h, got)
		}
	}
}

// TestWave16Comparison covers Module#<=> across equal, descendant, ancestor,
// unrelated and non-module arguments.
func TestWave16Comparison(t *testing.T) {
	cases := []struct{ src, want string }{
		{"p(Integer <=> Integer)", "0\n"},
		{"p(Integer <=> Numeric)", "-1\n"},
		{"p(Numeric <=> Integer)", "1\n"},
		{"p(Integer <=> String)", "nil\n"},
		{"p(Integer <=> 5)", "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("%q: got %q want %q", c.src, got, c.want)
		}
	}
}

// TestWave16ToSInspectName covers Module#to_s / #inspect / #name for named,
// anonymous, singleton-class and refinement receivers, and the inspect==to_s
// alias identity.
func TestWave16ToSInspectName(t *testing.T) {
	cases := []struct{ src, want string }{
		{"p String.to_s", "\"String\"\n"},
		{"p String.inspect", "\"String\"\n"},
		{"p String.singleton_class.to_s", "\"#<Class:String>\"\n"},
		{"p (Module.new.to_s =~ /\\A#<Module:0x\\h+>\\z/) != nil", "true\n"},
		{"p (Class.new.to_s =~ /\\A#<Class:0x\\h+>\\z/) != nil", "true\n"},
		{"p (\"x\".singleton_class.to_s =~ /\\A#<Class:#<String:0x\\h+>>\\z/) != nil", "true\n"},
		{"p (Class.new.singleton_class.to_s =~ /\\A#<Class:#<Class:0x\\h+>>\\z/) != nil", "true\n"},
		{"m = nil\nModule.new{ m = refine(String){} }\np m.to_s", "\"#<refinement:String@>\"\n"},
		{"p String.singleton_class.name", "nil\n"},
		{"p Module.new.name", "nil\n"},
		{"p(Module.instance_method(:inspect) == Module.instance_method(:to_s))", "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("%q: got %q want %q", c.src, got, c.want)
		}
	}
}

// TestWave16ModuleEval covers Module#module_eval / #class_eval argument
// validation (block plus args, count outside 1..3), the #to_str coercion of the
// eval-string and filename, and the class_eval/module_eval alias identity.
func TestWave16ModuleEval(t *testing.T) {
	if got := eval(t, "c = Class.new\nc.module_eval{ def hi; 1; end }\np c.new.hi"); got != "1\n" {
		t.Errorf("module_eval block: got %q", got)
	}
	if got := eval(t, "c = Class.new\nc.module_eval(\"def hi; 2; end\")\np c.new.hi"); got != "2\n" {
		t.Errorf("module_eval string: got %q", got)
	}
	if got := eval(t, "c = Class.new\nc.module_eval(\"def hi; 3; end\", \"f.rb\", 1)\np c.new.hi"); got != "3\n" {
		t.Errorf("module_eval string+file+line: got %q", got)
	}
	// filename coerced via #to_str.
	if got := eval(t, "c = Class.new\nfn = Object.new\ndef fn.to_str; \"f.rb\"; end\nc.module_eval(\"def hi; 4; end\", fn)\np c.new.hi"); got != "4\n" {
		t.Errorf("module_eval to_str filename: got %q", got)
	}
	// eval-string coerced via #to_str.
	if got := eval(t, "c = Class.new\nsrc = Object.new\ndef src.to_str; \"def hi; 5; end\"; end\nc.module_eval(src)\np c.new.hi"); got != "5\n" {
		t.Errorf("module_eval to_str source: got %q", got)
	}
	errs := []struct{ src, class, msg string }{
		{"Module.new.module_eval(\"1\"){ }", "ArgumentError", "wrong number of arguments (given 1, expected 0)"},
		{"Module.new.module_eval", "ArgumentError", "wrong number of arguments (given 0, expected 1..3)"},
		{"Module.new.module_eval(\"1\", \"f\", 1, 2)", "ArgumentError", "wrong number of arguments (given 4, expected 1..3)"},
		{"Module.new.module_eval(Object.new)", "TypeError", "no implicit conversion of Object into String"},
		{"Module.new.module_eval(\"1\", Object.new)", "TypeError", "no implicit conversion of Object into String"},
		// #to_str present but returning a non-String still raises (coerceToString
		// falls through the conversion to the TypeError).
		{"o = Object.new\ndef o.to_str; 1; end\nModule.new.module_eval(o)", "TypeError", "no implicit conversion of Object into String"},
	}
	for _, c := range errs {
		if cls, msg := evalErr(t, c.src); cls != c.class || msg != c.msg {
			t.Errorf("%q: got %s/%q want %s/%q", c.src, cls, msg, c.class, c.msg)
		}
	}
	if got := eval(t, "p(Module.instance_method(:class_eval) == Module.instance_method(:module_eval))"); got != "true\n" {
		t.Errorf("class_eval alias: got %q", got)
	}
	if got := eval(t, "p(Module.instance_method(:class_exec) == Module.instance_method(:module_exec))"); got != "true\n" {
		t.Errorf("class_exec alias: got %q", got)
	}
}

// TestWave16AliasMethodPrivacy covers alias_method forcing the always-private
// names private while leaving an ordinary name at its source visibility, and
// alwaysPrivateName's negative cases.
func TestWave16AliasMethodPrivacy(t *testing.T) {
	for _, n := range []string{"initialize", "initialize_copy", "initialize_clone", "initialize_dup", "respond_to_missing?"} {
		src := "c = Class.new{ def pub; end; alias_method :\"" + n + "\", :pub }\np c.private_instance_methods.include?(:\"" + n + "\")"
		if got := eval(t, src); got != "true\n" {
			t.Errorf("alias to %s: got %q want private", n, got)
		}
	}
	// A non-special alias of a public method stays public; method_missing is not
	// forced private.
	if got := eval(t, "c = Class.new{ def pub; end; alias_method :other, :pub }\np c.private_instance_methods.include?(:other)"); got != "false\n" {
		t.Errorf("non-special alias public: got %q", got)
	}
	if got := eval(t, "c = Class.new{ def pub; end; alias_method :method_missing, :pub }\np c.private_instance_methods.include?(:method_missing)"); got != "false\n" {
		t.Errorf("method_missing alias not forced private: got %q", got)
	}
	if alwaysPrivateName("foo") {
		t.Error("alwaysPrivateName(foo) = true")
	}
	if !alwaysPrivateName("initialize") {
		t.Error("alwaysPrivateName(initialize) = false")
	}
}

// TestWave16UndefMethodMessage covers undef_method's NameError receiver rendering
// for anonymous modules/classes and a class metaclass.
func TestWave16UndefMethodMessage(t *testing.T) {
	cases := []struct{ src, re string }{
		{"Module.new.send(:undef_method, :nope)", `\Aundefined method 'nope' for module '#<Module:0x\h+>'\z`},
		{"Class.new.send(:undef_method, :nope)", `\Aundefined method 'nope' for class '#<Class:0x\h+>'\z`},
	}
	for _, c := range cases {
		src := "begin; " + c.src + "; rescue NameError => e; p(e.message =~ /" + c.re + "/ ? :ok : e.message); end"
		if got := eval(t, src); got != ":ok\n" {
			t.Errorf("%q: got %q", c.src, got)
		}
	}
	// A metaclass names the class it belongs to.
	got := eval(t, "begin; String.singleton_class.send(:undef_method, :nope); rescue NameError => e; p(e.message =~ /for class 'String'/ ? :ok : e.message); end")
	if got != ":ok\n" {
		t.Errorf("metaclass undef message: got %q", got)
	}
}

// TestWave16MethodDefinedInheritAndVisibility covers Module#method_defined? and
// the public_/private_/protected_ variants across the inherit flag and the
// private-method exclusion.
func TestWave16MethodDefinedInheritAndVisibility(t *testing.T) {
	pre := "class MDBase; def pub; end; private def priv; end; end\nclass MDChild < MDBase; def own; end; end\n"
	cases := []struct{ src, want string }{
		{"p MDChild.method_defined?(:pub)", "true\n"},                  // inherited public
		{"p MDChild.method_defined?(:priv)", "false\n"},                // private excluded
		{"p MDChild.method_defined?(:pub, false)", "false\n"},          // own-only misses inherited
		{"p MDChild.method_defined?(:own, false)", "true\n"},           // own present
		{"p MDBase.private_method_defined?(:priv)", "true\n"},          // private variant
		{"p MDChild.private_method_defined?(:priv, false)", "false\n"}, // own-only misses inherited private
		{"p MDBase.public_method_defined?(:pub)", "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, pre+c.src); got != c.want {
			t.Errorf("%q: got %q want %q", c.src, got, c.want)
		}
	}
}

// TestWave16InstanceMethodNameError covers Module#instance_method /
// #public_instance_method on a missing name (NameError carrying #name) and the
// private/protected rejection of public_instance_method.
func TestWave16InstanceMethodNameError(t *testing.T) {
	got := eval(t, "begin; String.instance_method(:nope); rescue NameError => e; p e.name; end")
	if got != ":nope\n" {
		t.Errorf("instance_method NameError#name: got %q", got)
	}
	if cls, _ := evalErr(t, "c = Class.new{ private def sec; end }\nc.public_instance_method(:sec)"); cls != "NameError" {
		t.Errorf("public_instance_method private: got %s", cls)
	}
}

// TestWave16ModuleToSStrStruct drives moduleToSStr on the branches unreachable
// from Ruby: a refinement carrying no holder, a refinement carrying no target,
// and a singleton class with neither a metaOf nor an attached object.
func TestWave16ModuleToSStrStruct(t *testing.T) {
	vm := New(io.Discard)
	// Refinement with a target but no holder → holder renders empty.
	ref := &RClass{isModule: true, isRefinement: true, refinedClass: vm.cString}
	if got := vm.moduleToSStr(ref); got != "#<refinement:String@>" {
		t.Errorf("refinement no holder: got %q", got)
	}
	// A struct flagged isRefinement but with no target falls through to the
	// anonymous-module rendering.
	notRef := &RClass{isModule: true, isRefinement: true}
	if got := vm.moduleToSStr(notRef); got == "" {
		t.Errorf("refinement no target: got empty")
	}
	// A singleton class with neither metaOf nor attached uses the anonymous form.
	orphan := &RClass{isSingleton: true}
	got := vm.moduleToSStr(orphan)
	if len(got) < len("#<Class:#<Class:0x") || got[:9] != "#<Class:#" {
		t.Errorf("orphan singleton: got %q", got)
	}
}
