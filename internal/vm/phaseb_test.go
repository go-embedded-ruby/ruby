package vm_test

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/vm"
)

// evalErr runs src expecting a runtime error, returning the RubyError class and
// message. It fails the test if no error is raised.
func evalErr(t *testing.T, src string) (class, msg string) {
	t.Helper()
	_, err := runProg(t, src, nil)
	if err == nil {
		t.Fatalf("src=%q: expected a runtime error, got none", src)
	}
	re, ok := err.(vm.RubyError)
	if !ok {
		t.Fatalf("src=%q: expected a RubyError, got %#v", src, err)
	}
	return re.Class, re.Message
}

// TestScopedClassModule covers P1: `::` constant-path class/module definitions,
// `::`-path superclasses, leading-`::` lookups and scoped names.
func TestScopedClassModule(t *testing.T) {
	tests := []struct{ src, want string }{
		{"class Foo; end; class Foo::Bar; end; p Foo::Bar", "Foo::Bar\n"},
		{"module Foo; end; class Foo::Baz; def hi; 41; end; end; p Foo::Baz.new.hi", "41\n"},
		{"class Outer; end; class Outer::Inner; end; p Outer::Inner.name", "\"Outer::Inner\"\n"},
		// `::`-path superclass and leading-`::` superclass.
		{"class E < StandardError; end; module M; class G < ::E; end; end; p M::G.ancestors.include?(E)", "true\n"},
		{"module N; end; class N::A; end; class B < N::A; end; p B.superclass.name", "\"N::A\"\n"},
		// Reopening a scoped class keeps it the same object.
		{"module W; end; class W::C; def a; 1; end; end; class W::C; def b; 2; end; end; o = W::C.new; p [o.a, o.b]", "[1, 2]\n"},
		// Reopening a scoped module.
		{"module Q; end; module Q::R; X = 7; end; module Q::R; Y = 8; end; p [Q::R::X, Q::R::Y]", "[7, 8]\n"},
		// Leading-`::` constant lookup as a value, and a leading-`::` class def.
		{"TOP = 9; class K; def v; ::TOP; end; end; p K.new.v", "9\n"},
		{"class ::Z; def hi; 5; end; end; p Z.new.hi", "5\n"},
		// A bare expression superclass (a `::`-path) used at the top level.
		{"module S; class Base; def k; 3; end; end; end; class Derived < S::Base; end; p Derived.new.k", "3\n"},
	}
	for _, tc := range tests {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

// TestScopedClassModuleErrors covers the type-mismatch and bad-parent branches.
func TestScopedClassModuleErrors(t *testing.T) {
	// A scoped definition whose parent is not a class/module.
	if cls, _ := evalErr(t, "X = 5; class X::Y; end"); cls != "TypeError" {
		t.Errorf("scoped class under a non-module: got %s, want TypeError", cls)
	}
	if cls, _ := evalErr(t, "X = 5; module X::Y; end"); cls != "TypeError" {
		t.Errorf("scoped module under a non-module: got %s, want TypeError", cls)
	}
	// Defining a class where a module already holds the name (and vice versa).
	if cls, _ := evalErr(t, "module Foo; end; class Foo; end"); cls != "TypeError" {
		t.Errorf("class over an existing module: got %s, want TypeError", cls)
	}
	if cls, _ := evalErr(t, "class Foo; end; module Foo; end"); cls != "TypeError" {
		t.Errorf("module over an existing class: got %s, want TypeError", cls)
	}
	// A `::`-path superclass that is not a Class.
	if cls, _ := evalErr(t, "module M; end; class C < ::M; end"); cls != "TypeError" {
		t.Errorf("module as superclass: got %s, want TypeError", cls)
	}
}

// TestArgumentForwarding covers P2: `def f(...)` / `g(...)`.
func TestArgumentForwarding(t *testing.T) {
	tests := []struct{ src, want string }{
		{"def f(...); g(...); end; def g(a,b); a+b; end; p f(2,3)", "5\n"},
		{"def f(a, ...); g(a, ...); end; def g(x,y,z); x+y+z; end; p f(1,2,3)", "6\n"},
		{"def g(a:, b:); a*b; end; def f(...); g(...); end; p f(a:3, b:4)", "12\n"},
		{"def g; yield 10; end; def f(...); g(...); end; p f{|x| x+1}", "11\n"},
		{"def g(*a, **k); [a,k]; end; def f(...); g(...); end; p f(1,2,x:3)", "[[1, 2], {x: 3}]\n"},
		{"def g(x); x; end; def f(pre, ...); g(...); end; p f(99, 7)", "7\n"},
		// No args forwarded at all (empty rest, empty kw, no block).
		{"def g; 1; end; def f(...); g(...); end; p f", "1\n"},
		// Forwarding leaves a trailing explicit arg after the `...`.
		{"def g(a,b,c); [a,b,c]; end; def f(...); g(...); end; p f(1,2,3)", "[1, 2, 3]\n"},
		// Forward to a method on an explicit receiver, with a leading explicit arg.
		{"class R; def g(a,b,c); a+b+c; end; end; def f(...); R.new.g(10, ...); end; p f(2,3)", "15\n"},
		// A trailing explicit arg after the `...`.
		{"def g(a,b,c); [a,b,c]; end; def f(...); g(..., 9); end; p f(1,2)", "[1, 2, 9]\n"},
	}
	for _, tc := range tests {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

// TestXStr covers P3: %x{…} command execution.
func TestXStr(t *testing.T) {
	if got := eval(t, "p(%x{echo hi}.strip)"); got != "\"hi\"\n" {
		t.Errorf("%%x got %q", got)
	}
	if got := eval(t, "print %x{printf abc}"); got != "abc" {
		t.Errorf("%%x printf got %q", got)
	}
}

// TestMultiAssignTargets covers P4: masgn into constants and other non-local
// targets, with and without a splat.
func TestMultiAssignTargets(t *testing.T) {
	tests := []struct{ src, want string }{
		{"A, B = 1, 2; p [A, B]", "[1, 2]\n"},
		{"A, *B = 1, 2, 3; p [A, B]", "[1, [2, 3]]\n"},
		{"*A, B = 1, 2, 3; p [A, B]", "[[1, 2], 3]\n"},
		{"a, B = 1, 2; p [a, B]", "[1, 2]\n"},
		{"a, b = 1, 2; p [a, b]", "[1, 2]\n"}, // all-locals path still works
		{"A, B, C = 1, 2, 3; p [A, B, C]", "[1, 2, 3]\n"},
		{"A, b = 1, 2; p [A, b]", "[1, 2]\n"}, // mixed const + local
	}
	for _, tc := range tests {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

// TestSendAliases covers __send__ / public_send.
func TestSendAliases(t *testing.T) {
	if got := eval(t, "p 1.__send__(:+, 2)"); got != "3\n" {
		t.Errorf("__send__ got %q", got)
	}
	if got := eval(t, "p \"ab\".public_send(:upcase)"); got != "\"AB\"\n" {
		t.Errorf("public_send got %q", got)
	}
}

// TestRespondToMissing covers respond_to? consulting respond_to_missing?.
func TestRespondToMissing(t *testing.T) {
	tests := []struct{ src, want string }{
		{"o=Object.new; def o.respond_to_missing?(m,p=false); m==:zap; end; p o.respond_to?(:zap)", "true\n"},
		{"o=Object.new; def o.respond_to_missing?(m,p=false); m==:zap; end; p o.respond_to?(:nope)", "false\n"},
		{"class C; def respond_to_missing?(m,ip=false); m==:dyn; end; end; p C.new.respond_to?(:dyn)", "true\n"},
		// No respond_to_missing? defined and the method is absent.
		{"p Object.new.respond_to?(:nope)", "false\n"},
		// A real method short-circuits before respond_to_missing?.
		{"p \"x\".respond_to?(:upcase)", "true\n"},
		// include_private flag is forwarded.
		{"o=Object.new; def o.respond_to_missing?(m,ip); ip; end; p o.respond_to?(:x, true)", "true\n"},
		// A class method is seen via the singleton chain.
		{"p Array.respond_to?(:new)", "true\n"},
	}
	for _, tc := range tests {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

// TestDefineSingletonMethodBlock covers a &block param inside a
// define_singleton_method / define_method body binding the passed block.
func TestDefineSingletonMethodBlock(t *testing.T) {
	tests := []struct{ src, want string }{
		{"o=Object.new; o.define_singleton_method(:run){|&b| b.call}; p o.run{42}", "42\n"},
		{"o=Object.new; o.define_singleton_method(:run){|&b| b ? b.call : :none}; p o.run", ":none\n"},
		{"class C; define_method(:m){|x, &b| b.call(x)}; end; p C.new.m(5){|v| v*2}", "10\n"},
		// A bare yield inside a define_method body uses the passed block too.
		{"class C; define_method(:m){ yield 3 }; end; p C.new.m{|v| v+1}", "4\n"},
		// A define_method body that is a native proc (a Method object) still runs.
		{"def double(x); x*2; end; class C; define_method(:d, &method(:double)); end; p C.new.d(8)", "16\n"},
		// define_singleton_method given an explicit Proc (not a block) argument.
		{"o=Object.new; pr=proc{|x| x+1}; o.define_singleton_method(:inc, pr); p o.inc(4)", "5\n"},
	}
	for _, tc := range tests {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

// TestSuperSplat covers super(*a), super(**k) and super(&b).
func TestSuperSplat(t *testing.T) {
	tests := []struct{ src, want string }{
		{"class A; def f(*a); a.sum; end; end; class B < A; def f(*a); super(*a); end; end; p B.new.f(1,2,3)", "6\n"},
		{"class A; def g(x,y); x+y; end; end; class B < A; def g(*a); super(*a); end; end; p B.new.g(4,5)", "9\n"},
		{"class A; def h(a:,b:); a*b; end; end; class B < A; def h(**k); super(**k); end; end; p B.new.h(a:3,b:4)", "12\n"},
		{"class A; def k; yield 7; end; end; class B < A; def k(&blk); super(&blk); end; end; p B.new.k{|v| v+1}", "8\n"},
	}
	for _, tc := range tests {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

// TestLoadPath covers $LOAD_PATH / $: as a real mutable array.
func TestLoadPath(t *testing.T) {
	tests := []struct{ src, want string }{
		{"p $LOAD_PATH.class", "Array\n"},
		{"p $LOAD_PATH", "[]\n"},
		{"$LOAD_PATH.unshift \"lib\"; p $LOAD_PATH.first", "\"lib\"\n"},
		{"$LOAD_PATH << \"vendor\"; p $LOAD_PATH", "[\"vendor\"]\n"},
	}
	for _, tc := range tests {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

// TestLoadPathRequire covers require searching $LOAD_PATH.
func TestLoadPathRequire(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/greet.rb", []byte("GREETING = \"hi from lib\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	q := strconv.Quote(dir)
	got := eval(t, "$LOAD_PATH.unshift "+q+"\nrequire \"greet\"\nputs GREETING\n")
	if !strings.Contains(got, "hi from lib") {
		t.Errorf("require via $LOAD_PATH got %q", got)
	}
	// A second require of the same feature returns false.
	if got := eval(t, "$LOAD_PATH.unshift "+q+"\nrequire \"greet\"\np require(\"greet\")\n"); !strings.HasSuffix(got, "false\n") {
		t.Errorf("second require should be false, got %q", got)
	}
	// A missing feature still raises LoadError after searching the path.
	if cls, _ := evalErr(t, "$LOAD_PATH.unshift "+q+"\nrequire \"nope\""); cls != "LoadError" {
		t.Errorf("missing require: got %s, want LoadError", cls)
	}
	// A non-string $LOAD_PATH entry is skipped, and a valid one after it works.
	got = eval(t, "$LOAD_PATH.unshift "+q+"\n$LOAD_PATH.unshift 123\nrequire \"greet\"\nputs GREETING\n")
	if !strings.Contains(got, "hi from lib") {
		t.Errorf("require skipping a non-string entry got %q", got)
	}
}
