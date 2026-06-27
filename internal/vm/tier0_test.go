package vm_test

import (
	"strings"
	"testing"
)

// runCase is a (src, want-stdout) pair asserted against MRI Ruby 4.0.5 output.
type runCase struct{ src, want string }

func checkCases(t *testing.T, cases []runCase) {
	t.Helper()
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestDefinedOperator covers defined? for every form, against MRI semantics:
// it never raises and returns the right tag String or nil. (Oracle: MRI 4.0.5.)
func TestDefinedOperator(t *testing.T) {
	checkCases(t, []runCase{
		// The #1 bug: an undefined constant is nil, not a NameError.
		{`p defined?(Nope)`, "nil\n"},
		{`X = 1 unless defined?(X); p X`, "1\n"},
		{`X = 1; X = 2 unless defined?(X); p X`, "1\n"},
		// Constants, including scope-resolution paths.
		{`A = 1; p defined?(A)`, "\"constant\"\n"},
		{`p defined?(Float::INFINITY)`, "\"constant\"\n"},
		{`p defined?(Float::NOPE)`, "nil\n"},
		{`p defined?(Nope::Bar)`, "nil\n"},
		{`p defined?(::Object)`, "\"constant\"\n"},
		{`p defined?(::Nope)`, "nil\n"},
		// Methods: implicit-self, explicit receiver, with args.
		{`p defined?(puts)`, "\"method\"\n"},
		{`p defined?(no_such_method)`, "nil\n"},
		{`def foo(a); end; p defined?(foo(1))`, "\"method\"\n"},
		{`p defined?(undefmeth(1))`, "nil\n"},
		{`p defined?("s".upcase)`, "\"method\"\n"},
		{`p defined?(1.foobar)`, "nil\n"},
		// A defined receiver that is nil still reports method; an undefined
		// receiver reports nil (the receiver is itself subject to defined?).
		{`@x = nil; p defined?(@x.to_s)`, "\"method\"\n"},
		{`p defined?(@nope.to_s)`, "nil\n"},
		{`p defined?(Nope.foo)`, "nil\n"},
		// Operators are methods.
		{`p defined?(1 + 2)`, "\"method\"\n"},
		{`x = 1; p defined?(-x)`, "\"method\"\n"},
		{`p defined?(!true)`, "\"method\"\n"},
		{`p defined?(1 <=> 2)`, "\"method\"\n"},
		// && / || are expressions.
		{`p defined?(1 && 2)`, "\"expression\"\n"},
		{`p defined?(nil || 1)`, "\"expression\"\n"},
		// Local variables.
		{`y = 5; p defined?(y)`, "\"local-variable\"\n"},
		{`p defined?(undeclared_local)`, "nil\n"},
		// Instance / class / global variables.
		{`@iv = 2; p defined?(@iv)`, "\"instance-variable\"\n"},
		{`p defined?(@unset_iv)`, "nil\n"},
		{`$gv = 3; p defined?($gv)`, "\"global-variable\"\n"},
		{`p defined?($unset_gv)`, "nil\n"},
		{`class C; @@v = 1; def t; defined?(@@v); end; def u; defined?(@@nope); end; end; p [C.new.t, C.new.u]`,
			"[\"class variable\", nil]\n"},
		// yield.
		{`def m; defined?(yield); end; p [m { 1 }, m]`, "[\"yield\", nil]\n"},
		// self / nil / true / false.
		{`p defined?(self)`, "\"self\"\n"},
		{`p defined?(nil)`, "\"nil\"\n"},
		{`p defined?(true)`, "\"true\"\n"},
		{`p defined?(false)`, "\"false\"\n"},
		// Assignments.
		{`p defined?(x = 1)`, "\"assignment\"\n"},
		{`@z = 0; p defined?(@z += 1)`, "\"assignment\"\n"},
		{`p defined?(A2 = 1)`, "\"assignment\"\n"},
		{`@v = 0; p defined?(@v = 5)`, "\"assignment\"\n"},
		{`$g = 0; p defined?($g = 5)`, "\"assignment\"\n"},
		// Literals and other expressions.
		{`p defined?("str")`, "\"expression\"\n"},
		{`p defined?([1, 2])`, "\"expression\"\n"},
		{`p defined?(1)`, "\"expression\"\n"},
		{`p defined?(:sym)`, "\"expression\"\n"},
		{`p defined?({a: 1})`, "\"expression\"\n"},
		{`p defined?(1..3)`, "\"expression\"\n"},
		// Chained receiver where an inner part is undefined.
		{`p defined?(@x.foo.bar)`, "nil\n"},
	})
}

// TestModuleFunction covers Module#module_function (both forms).
func TestModuleFunction(t *testing.T) {
	checkCases(t, []runCase{
		{`module M; def foo; 42; end; module_function :foo; end; p M.foo`, "42\n"},
		{`module M; module_function; def foo; 7; end; def bar; foo * 2; end; end; p [M.foo, M.bar]`, "[7, 14]\n"},
		// The instance method still exists when included.
		{`module M; module_function; def foo; 9; end; end; class C; include M; def call; foo; end; end; p C.new.call`, "9\n"},
		{`module M; def a; 1; end; def b; 2; end; p module_function(:a, :b); end`, "[:a, :b]\n"},
		{`module M; def a; 1; end; p module_function :a; end`, ":a\n"},
	})
	if err := runErr(t, `module M; module_function :nope; end`); err == nil ||
		!strings.Contains(err.Error(), "undefined method 'nope'") {
		t.Errorf("module_function on missing method: err=%v", err)
	}
}

// TestVisibilityDirectives covers the visibility setters and the _class_method
// forms. Visibility is not enforced by this VM, but the directives must accept
// names and return the MRI value.
func TestVisibilityDirectives(t *testing.T) {
	checkCases(t, []runCase{
		{`class C; def f; 1; end; p private(:f); end`, ":f\n"},
		{`class C; def f; 1; end; def g; 2; end; p public(:f, :g); end`, "[:f, :g]\n"},
		{`class C; p private; end`, "nil\n"},
		{`class C; def f; end; protected(:f); end; p "ok"`, "\"ok\"\n"},
		{`module M; def self.sx; 1; end; p private_class_method(:sx) == M; end`, "true\n"},
		{`module M; def self.sx; 1; end; p public_class_method(:sx) == M; end`, "true\n"},
		// Visibility is not enforced: a "private" method stays callable.
		{`class C; def f; 5; end; private :f; end; p C.new.f`, "5\n"},
		// Constant-visibility directives are accepted (no-ops returning nil).
		{`class C; X = 1; p private_constant(:X) == C; end`, "true\n"},
		{`class C; X = 1; p public_constant(:X) == C; end`, "true\n"},
	})
	for _, src := range []string{
		`class C; private :nope; end`,
		`module M; private_class_method :nope; end`,
	} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "undefined method 'nope'") {
			t.Errorf("src=%q expected undefined-method error, got %v", src, err)
		}
	}
}

// TestExtendAndSingletonClass covers Object#extend on ordinary and
// builtin-backed receivers, plus Object#singleton_class.
func TestExtendAndSingletonClass(t *testing.T) {
	checkCases(t, []runCase{
		// extend on an ordinary object.
		{`module M; def a; "A"; end; end; o = Object.new; o.extend(M); p o.a`, "\"A\"\n"},
		// extend on builtin-backed values (the $LOAD_PATH case).
		{`module M; def shout; "HEY"; end; end; s = "x"; s.extend(M); p s.shout`, "\"HEY\"\n"},
		{`module M; def tag; :arr; end; end; a = [1]; a.extend(M); p [a.tag, a.length]`, "[:arr, 1]\n"},
		{`module M; def m; 1; end; end; h = {}; h.extend(M); p h.m`, "1\n"},
		// Only the extended instance gains the method.
		{`module M; def m; 1; end; end; a = []; a.extend(M); p [].respond_to?(:m)`, "false\n"},
		{`module M; def a; end; end; o = []; o.extend(M); p o.respond_to?(:a)`, "true\n"},
		// extend returns self and chains.
		{`module M; def m; 9; end; end; p [].extend(M).m`, "9\n"},
		// singleton_class.
		{`p "abc".singleton_class.class`, "Class\n"},
		{`o = Object.new; def o.x; 1; end; p o.singleton_class.instance_methods(false)`, "[:x]\n"},
		// define_singleton_method on a builtin-backed value.
		{`a = [1, 2]; a.define_singleton_method(:double) { length * 2 }; p a.double`, "4\n"},
	})
	for _, src := range []string{
		`[].extend(Object.new)`, // not a module
		`[].extend`,             // no args
		`5.singleton_class`,     // immediate value
	} {
		if err := runErr(t, src); err == nil {
			t.Errorf("src=%q expected error, got nil", src)
		}
	}
}

// TestReflection covers UnboundMethod / Method reflection and define_method
// from a (Unbound)Method.
func TestReflection(t *testing.T) {
	checkCases(t, []runCase{
		// instance_method → UnboundMethod, bind, bind_call.
		{`class C; def foo; 5; end; end; p C.instance_method(:foo).bind(C.new).call`, "5\n"},
		{`class C; def foo(x); x + 1; end; end; p C.instance_method(:foo).bind_call(C.new, 41)`, "42\n"},
		{`class C; def foo; 3; end; end; um = C.instance_method(:foo); p [um.name, um.owner, um.arity]`, "[:foo, C, 0]\n"},
		{`class C; def foo(a, b); end; end; p C.instance_method(:foo).arity`, "2\n"},
		// Inherited methods resolve.
		{`class A; def foo; 1; end; end; class B < A; end; p B.instance_method(:foo).owner`, "A\n"},
		// Method#unbind round-trips.
		{`class C; def foo; 11; end; end; m = C.new.method(:foo); p m.unbind.bind(C.new).call`, "11\n"},
		// define_method from an (Unbound)Method.
		{`class C; def foo; 3; end; end; class C; define_method(:bar, instance_method(:foo)); end; p C.new.bar`, "3\n"},
		{`class C; def foo; 7; end; end; m = C.new.method(:foo); class D; end; D.define_method(:g, m); p D.new.g`, "7\n"},
		// method_defined?.
		{`class C; def foo; end; end; p [C.method_defined?(:foo), C.method_defined?(:nope)]`, "[true, false]\n"},
	})
	for _, src := range []string{
		`class C; def foo; end; end; C.instance_method(:nope)`,         // missing method
		`class C; def foo; end; end; C.instance_method(:foo).bind(42)`, // incompatible bind
		`class C; def foo; end; end; C.instance_method(:foo).bind_call(42)`,
	} {
		if err := runErr(t, src); err == nil {
			t.Errorf("src=%q expected error, got nil", src)
		}
	}
}

// TestVersionConstants covers the RUBY_* version/platform constants.
func TestVersionConstants(t *testing.T) {
	checkCases(t, []runCase{
		{`p RUBY_VERSION >= "3.0"`, "true\n"},
		{`p RUBY_VERSION.class`, "String\n"},
		{`p RUBY_ENGINE`, "\"ruby\"\n"},
		{`p RUBY_ENGINE_VERSION == RUBY_VERSION`, "true\n"},
		{`p RUBY_PATCHLEVEL`, "0\n"},
		{`p RUBY_PLATFORM.class`, "String\n"},
		{`p RUBY_PLATFORM.include?("-")`, "true\n"},
		{`p RUBY_DESCRIPTION.start_with?("ruby ")`, "true\n"},
		{`p RUBY_COPYRIGHT.include?("Copyright")`, "true\n"},
	})
}

// TestFileConstants covers File path constants.
func TestFileConstants(t *testing.T) {
	checkCases(t, []runCase{
		{`p File::SEPARATOR`, "\"/\"\n"},
		{`p File::ALT_SEPARATOR`, "nil\n"},
		{`p File::PATH_SEPARATOR`, "\":\"\n"},
		{`p File::NULL`, "\"/dev/null\"\n"},
	})
}

// TestKernelIntrospection covers __method__, caller, at_exit, alias_method.
func TestKernelIntrospection(t *testing.T) {
	checkCases(t, []runCase{
		{`def m; __method__; end; p m`, ":m\n"},
		{`p __method__`, "nil\n"},
		{`def a; __method__; end; def b; a; end; p b`, ":a\n"},
		// caller is a best-effort String array; from the top level it is empty.
		{`p caller`, "[]\n"},
		{`def a; caller; end; def b; a; end; r = b; p [r.class, r.length >= 2]`, "[Array, true]\n"},
		{`def a; caller; end; r = a; p r.length`, "1\n"},
		// alias_method (method form of alias).
		{`class C; def foo; 1; end; alias_method :bar, :foo; end; p C.new.bar`, "1\n"},
		{`class C; def foo; 1; end; p alias_method(:bar, :foo); end`, ":bar\n"},
		// at_exit runs in LIFO at normal exit.
		{`at_exit { puts "a" }; at_exit { puts "b" }; puts "main"`, "main\nb\na\n"},
		{`x = at_exit { }; p x.class`, "Proc\n"},
	})
	if err := runErr(t, `class C; alias_method :x, :nope; end`); err == nil ||
		!strings.Contains(err.Error(), "undefined method") {
		t.Errorf("alias_method on missing method: err=%v", err)
	}
	if err := runErr(t, `at_exit`); err == nil {
		t.Errorf("at_exit without a block should raise")
	}
}

// TestDefinedGlobalSpecials covers defined? for the regexp special globals and
// numbered match groups, where the result depends on whether a match exists and
// whether the group participated. (Oracle: MRI 4.0.5.)
func TestDefinedGlobalSpecials(t *testing.T) {
	checkCases(t, []runCase{
		// $~ is always defined; the others need a match.
		{`p defined?($~)`, "\"global-variable\"\n"},
		{`p defined?($&)`, "nil\n"},
		{`p defined?($1)`, "nil\n"},
		// After a match: whole-match specials defined; a participating group
		// defined; a non-participating / out-of-range group nil.
		{`"ab" =~ /(a)/; p [defined?($~), defined?($&), defined?($1), defined?($2)]`,
			"[\"global-variable\", \"global-variable\", \"global-variable\", nil]\n"},
		{"\"abc\" =~ /b/; p [defined?($1), defined?($`)]", "[nil, \"global-variable\"]\n"},
		// A predefined stream global ($stdout) is defined.
		{`p defined?($stdout)`, "\"global-variable\"\n"},
	})
}

// TestDefinedControlFlow checks that a control-flow signal (throw) raised while
// evaluating a guarded receiver propagates through defined? rather than being
// swallowed, matching MRI.
func TestDefinedControlFlow(t *testing.T) {
	checkCases(t, []runCase{
		{`catch(:t) { p defined?(throw(:t).x) }; p "after"`, "\"after\"\n"},
	})
}

// TestReflectionInspect covers UnboundMethod's string representation. rbgo uses
// the "#<UnboundMethod: Owner#name>" core form (MRI 4.0.5 also appends the
// parameter signature and source location, which this VM does not track).
func TestReflectionInspect(t *testing.T) {
	checkCases(t, []runCase{
		{`class C; def foo; end; end; p C.instance_method(:foo).to_s`, "\"#<UnboundMethod: C#foo>\"\n"},
		{`class C; def foo; end; end; p C.instance_method(:foo).inspect`, "\"#<UnboundMethod: C#foo>\"\n"},
		{`class C; def foo; end; end; p(!!C.instance_method(:foo))`, "true\n"},
	})
}

// TestStringNameArgs checks the authoring directives accept String (not just
// Symbol) method names, and reject non-name arguments with TypeError.
func TestStringNameArgs(t *testing.T) {
	checkCases(t, []runCase{
		{`class C; def f; 1; end; alias_method "g", "f"; end; p C.new.g`, "1\n"},
		{`module M; def f; 3; end; module_function "f"; end; p M.f`, "3\n"},
		{`class C; def f; end; end; p C.instance_method("f").name`, ":f\n"},
	})
	if err := runErr(t, `class C; def f; end; private 1; end`); err == nil ||
		!strings.Contains(err.Error(), "not a symbol nor a string") {
		t.Errorf("private with a non-name arg: err=%v", err)
	}
}

// TestExtendIdempotent covers re-extending / re-defining on the same builtin
// receiver, exercising the existing-singleton branch of the side table.
func TestExtendIdempotent(t *testing.T) {
	checkCases(t, []runCase{
		{`module A; def a; 1; end; end; module B; def b; 2; end; end
s = "x"; s.extend(A); s.extend(B); p [s.a, s.b]`, "[1, 2]\n"},
		{`a = []
a.define_singleton_method(:one) { 1 }
a.define_singleton_method(:two) { 2 }
p [a.one, a.two]`, "[1, 2]\n"},
	})
}

// TestAtExitErrorSwallowed checks that a raise inside one at_exit hook does not
// prevent the other hooks (or the program's normal completion) from running.
func TestAtExitErrorSwallowed(t *testing.T) {
	got := eval(t, `at_exit { puts "first" }
at_exit { raise "boom" }
at_exit { puts "last" }
puts "main"`)
	want := "main\nlast\nfirst\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}
