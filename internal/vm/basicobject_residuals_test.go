// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestSingletonMethodHooks covers the singleton_method_added / _removed /
// _undefined definition hooks: MRI calls them on the object that owns a singleton
// method whenever one is added to, removed from, or undefined on it. Each case
// drives a distinct definition path (def obj.x, def self.x, class << self,
// define_method, define_singleton_method, alias, remove_method, undef).
func TestSingletonMethodHooks(t *testing.T) {
	cases := []struct{ src, want string }{
		// singleton_method_added, per-object singleton (def obj.x). Defining the hook
		// itself fires it once (MRI), then the following def fires it again.
		{`o = Object.new
def o.singleton_method_added(n); puts "A #{n}"; end
def o.foo; end`, "A singleton_method_added\nA foo\n"},
		// A class receiver (def self.x lands on the class's singleton methods) and the
		// class << self / define_singleton_method singleton-class paths all fire on the
		// class.
		{`c = Class.new
def c.singleton_method_added(n); puts "C #{n}"; end
class << c; def bar; end; end
c.define_singleton_method(:baz){}`, "C singleton_method_added\nC bar\nC baz\n"},
		// A method defined into a plain object's singleton class (class << obj) fires on
		// the attached object.
		{`o = Object.new
def o.singleton_method_added(n); puts "S #{n}"; end
class << o; def m; end; end`, "S singleton_method_added\nS m\n"},
		// alias_method, syntax alias, and define_method inside a singleton class each
		// count as adding a singleton method.
		{`c = Class.new
def c.singleton_method_added(n); puts "C #{n}"; end
def c.orig; end
class << c; alias_method :al, :orig; end
class << c; alias sy orig; end
class << c; define_method(:dm){}; end`,
			"C singleton_method_added\nC orig\nC al\nC sy\nC dm\n"},
		// singleton_method_removed fires when a singleton method is removed.
		{`o = Object.new
def o.singleton_method_removed(n); puts "R #{n}"; end
def o.gone; end
class << o; remove_method :gone; end`, "R gone\n"},
		// singleton_method_undefined fires when a singleton method is undefined.
		{`o = Object.new
def o.singleton_method_undefined(n); puts "U #{n}"; end
def o.und; end
class << o; undef_method :und; end`, "U und\n"},
		// When the hook is undef'd, defining a singleton method routes to the object's
		// #method_missing (here recording the hook name and method).
		{`o = Object.new
class << o
  def method_missing(*a); puts "MM #{a.inspect}"; end
  undef_method :singleton_method_added
  def foo; end
end`, "MM [:singleton_method_added, :foo]\n"},
		// The default hooks are private no-op methods on BasicObject — a direct call
		// returns nil and is otherwise silent.
		{`p BasicObject.new.__send__(:singleton_method_added, :x)`, "nil\n"},
		{`p BasicObject.new.__send__(:singleton_method_removed, :x)`, "nil\n"},
		{`p BasicObject.new.__send__(:singleton_method_undefined, :x)`, "nil\n"},
		// A def into a normal class still fires method_added, not the singleton hook.
		{`class MAOnly
  def self.method_added(n); puts "MA #{n}"; end
  def inst; end
end`, "MA inst\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestInstanceEvalDefinee covers instance_eval / instance_exec running a block
// with self's singleton class as the method-def target while keeping the block's
// lexical scope for constants, class variables and class reopenings (MRI).
func TestInstanceEvalDefinee(t *testing.T) {
	cases := []struct{ src, want string }{
		// The block is yielded self.
		{`p "hola".instance_eval {|o| o }`, "\"hola\"\n"},
		// A def binds to the receiver only, not to every instance of its class.
		{`o = Object.new
o.instance_eval { def foo; 1; end }
p o.foo
p Object.new.respond_to?(:foo)`, "1\nfalse\n"},
		// instance_eval on a class defines a class (singleton) method.
		{`c = Class.new
c.instance_eval { def cm; 9; end }
p c.cm`, "9\n"},
		// A `class` reopening inside the block resolves lexically (top-level Bar),
		// not under the receiver's singleton class.
		{`class IEBar; end
ob = Object.new
ob.instance_eval { class IEBar; def z; 3; end; end }
p IEBar.new.z`, "3\n"},
		// A class variable inside the block resolves in the block's lexical class.
		{`class IECVar
  @@n = 41
  def blk; ->(*){ @@n } end
end
p Object.new.instance_eval(&IECVar.new.blk)`, "41\n"},
		// A read-only block runs on an immediate (which has no singleton class).
		{`p 5.instance_eval { self }`, "5\n"},
		// instance_exec passes its arguments to the block and runs against self.
		{`o = Object.new
o.instance_exec(7) {|x| @v = x }
p o.instance_variable_get(:@v)`, "7\n"},
		// A native block (Symbol#to_proc) is yielded self and runs against it.
		{`p "abc".instance_eval(&:upcase)`, "\"ABC\"\n"},
		// A def inside instance_eval fires singleton_method_added on the receiver.
		{`o = Object.new
def o.singleton_method_added(n); puts n; end
o.instance_eval { def m2; end }`, "singleton_method_added\nm2\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestMethodMissingVisibilityRouting covers a private/protected call reaching the
// receiver's #method_missing, an undefined class method reaching a singleton
// #method_missing, and — with no #method_missing — a NoMethodError whose
// receiver/name are stamped. All match MRI.
func TestMethodMissingVisibilityRouting(t *testing.T) {
	cases := []struct{ src, want string }{
		// A blocked private call reaches #method_missing (with the name and args).
		{`class MMC
  def method_missing(*a) [:mm, *a] end
  def priv; end
  private :priv
end
p MMC.new.priv
p MMC.new.priv(1, 2)`, "[:mm, :priv]\n[:mm, :priv, 1, 2]\n"},
		// A blocked protected call (from outside) reaches #method_missing.
		{`class MMP
  def method_missing(*a) :mm end
  def prot; end
  protected :prot
end
p MMP.new.prot`, ":mm\n"},
		// A literal block flows through to #method_missing.
		{`class MMB
  def method_missing(*a, &b) b.call end
  def priv; end
  private :priv
end
p MMB.new.priv { 42 }`, "42\n"},
		// An undefined CLASS method reaches a singleton #method_missing.
		{`class MMS
  def self.method_missing(*a) [:smm, *a] end
end
p MMS.undefined_thing(3)`, "[:smm, :undefined_thing, 3]\n"},
		// A private CLASS method reaches a singleton #method_missing when blocked.
		{`class MMPCM
  def self.method_missing(*a) [:g, *a] end
  def self.pcm; end
  private_class_method :pcm
end
p MMPCM.pcm`, "[:g, :pcm]\n"},
		// A protected call between two kin instances is still allowed (not blocked).
		{`class Kin
  def initialize(v) @v = v end
  def gt(o) val > o.val end
  protected
  def val; @v end
end
p Kin.new(5).gt(Kin.new(3))`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// TestBlockedCallNoMethodMissing covers the raise path: a private/protected call
// with no #method_missing raises NoMethodError stamped with receiver and name.
func TestBlockedCallNoMethodMissing(t *testing.T) {
	got := eval(t, `class NoMM
  def priv; end
  private :priv
end
o = NoMM.new
begin
  o.priv
rescue NoMethodError => e
  p e.receiver.equal?(o)
  p e.name
end`)
	if got != "true\n:priv\n" {
		t.Fatalf("got %q", got)
	}
	// A blocked protected call with no #method_missing raises the protected-method
	// NoMethodError (the visProtected raise branch).
	if err := runErr(t, `class ProtNoMM
  def prot; end
  protected :prot
end
ProtNoMM.new.prot`); err == nil || !strings.Contains(err.Error(), "protected method 'prot'") {
		t.Fatalf("want protected-method NoMethodError, got %v", err)
	}
	// The splat / block-argument send forms use the raise-only gate; a blocked
	// private call there still raises NoMethodError — for an object receiver
	// (dispatchClass branch)...
	if err := runErr(t, `class NoMM2
  def priv; end
  private :priv
end
NoMM2.new.priv(*[1])`); err == nil || !strings.Contains(err.Error(), "private method 'priv'") {
		t.Fatalf("want private-method NoMethodError, got %v", err)
	}
	// ...and for a class receiver's private class method (resolveClassMethod branch).
	if err := runErr(t, `class NoMM3
  def self.pcm; end
  private_class_method :pcm
end
NoMM3.pcm(*[])`); err == nil || !strings.Contains(err.Error(), "private method 'pcm'") {
		t.Fatalf("want private class-method NoMethodError, got %v", err)
	}
}

// TestSingletonHookUndefRaises covers the undef'd-hook path with no user
// method_missing: the definition routes to the default method_missing, which
// raises NoMethodError naming the hook.
func TestSingletonHookUndefRaises(t *testing.T) {
	err := runErr(t, `o = Object.new
class << o; undef_method :singleton_method_added; end
def o.bar; end`)
	if err == nil || !strings.Contains(err.Error(), "NoMethodError") ||
		!strings.Contains(err.Error(), "singleton_method_added") {
		t.Fatalf("want NoMethodError for singleton_method_added, got %v", err)
	}
}

// TestUndefHookOnMetaclass covers undef of a hook whose default lives on
// BasicObject and is reachable only through the attached object (a class's
// metaclass superclass chain does not bridge to BasicObject's instance methods):
// the undef must succeed, and a subsequent class-method def must then raise
// NoMethodError rather than the undef itself raising NameError.
func TestUndefHookOnMetaclass(t *testing.T) {
	err := runErr(t, `class MetaUndef
  class << self; undef_method :singleton_method_added; end
end
def MetaUndef.foo; end`)
	if err == nil || !strings.Contains(err.Error(), "NoMethodError") {
		t.Fatalf("want NoMethodError after undef on metaclass, got %v", err)
	}
}

// TestUndefMissingMethodStillRaises guards the non-singleton branch of the undef
// resolution: undef'ing a name defined nowhere is still a NameError.
func TestUndefMissingMethodStillRaises(t *testing.T) {
	err := runErr(t, `class UndefMissing; undef_method :never_defined; end`)
	if err == nil || !strings.Contains(err.Error(), "NameError") {
		t.Fatalf("want NameError for undef of undefined method, got %v", err)
	}
}

// TestDefaultInitializeRejectsArgs covers BasicObject#initialize taking no
// arguments: new with arguments (and a subclass forwarding them via super) is an
// ArgumentError, while a zero-arg new still works.
func TestDefaultInitializeRejectsArgs(t *testing.T) {
	if got := eval(t, `p Object.new.class`); got != "Object\n" {
		t.Fatalf("zero-arg new broke: %q", got)
	}
	for _, src := range []string{
		`BasicObject.new(1)`,
		`Object.new(1, 2)`,
		`class SuperFwd; def initialize(x); super; end; end
SuperFwd.new(1)`,
	} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "ArgumentError") {
			t.Errorf("src=%q want ArgumentError, got %v", src, err)
		}
	}
}

// TestClonePreservesSingletonHooks checks that a cloned object's copied singleton
// class still fires the hook for methods added to the clone (its attached object
// is the clone, not the source).
func TestClonePreservesSingletonHooks(t *testing.T) {
	got := eval(t, `o = Object.new
def o.singleton_method_added(n); puts n; end
c = o.clone
def c.after_clone; end`)
	// The clone copies singleton_method_added, so defining after_clone on the clone
	// fires it. (The def on the source before cloning also fired, printing the hook
	// name; the clone re-runs none of that.)
	if !strings.Contains(got, "after_clone") {
		t.Fatalf("want hook fired on clone, got %q", got)
	}
}
