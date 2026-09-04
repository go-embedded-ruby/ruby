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
