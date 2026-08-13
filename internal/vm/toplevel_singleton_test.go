package vm_test

import (
	"strings"
	"testing"
)

// TestTopLevelSingletonDef covers `def self.foo` at the top level: it defines a
// singleton method on the main object (self), so a bare or `self.`-qualified
// call resolves it — while it does NOT leak as a method on other objects or as a
// class method of Object. Regression test for top-level singleton dispatch,
// which previously landed the method on Object's singleton-method table (making
// `Object.foo` work and bare `foo` fail). All outputs verified against MRI Ruby
// 4.0.5.
func TestTopLevelSingletonDef(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			"bare call finds main's singleton method",
			`def self.extract(file, line); [file, line]; end
p extract("a.rb", 1)`,
			"[\"a.rb\", 1]\n",
		},
		{
			"self.-qualified call finds it",
			`def self.extract(file, line); [file, line]; end
p self.extract("a.rb", 1)`,
			"[\"a.rb\", 1]\n",
		},
		{
			"splat param variant (not a splat-specific bug)",
			`def self.extract(file, line, *rest); [file, line, rest]; end
p extract("a.rb", 1, 2, 3)`,
			"[\"a.rb\", 1, [2, 3]]\n",
		},
		{
			"bare call from a later top-level statement",
			`def self.greet; "hi"; end
p greet`,
			"\"hi\"\n",
		},
		{
			"appears in self's singleton_methods",
			`def self.extract; end
p self.singleton_methods.include?(:extract)`,
			"true\n",
		},
		{
			"does NOT leak onto other Objects",
			`def self.extract; end
p Object.new.respond_to?(:extract)`,
			"false\n",
		},
		{
			"does NOT become a class method of Object",
			`def self.extract; 1; end
begin
  Object.extract
rescue NoMethodError
  p :raised
end`,
			":raised\n",
		},
		{
			"body sees main as self",
			`def self.who; self; end
p who.equal?(self)`,
			"true\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("src=%q\ngot=%q\nwant=%q", c.src, got, c.want)
			}
		})
	}

	// `def self.foo` where self is an immediate (reached via instance_eval on an
	// Integer) still raises the singleton-definition TypeError, mirroring MRI.
	if err := runErr(t, `5.instance_eval { def self.boom; end }`); err == nil ||
		!strings.Contains(err.Error(), "can't define singleton method") {
		t.Errorf("immediate self singleton def: got %v, want TypeError", err)
	}
}

// TestSingletonMethodsVisibilityAndScope covers the two MRI rules
// Kernel#singleton_methods obeys, beyond merely listing names: PRIVATE singleton
// methods are excluded (only public and protected are returned), and a false
// argument restricts the result to the receiver's OWN singleton methods,
// excluding those gained from `extend`ed modules (which live on the singleton
// class's ancestors and so count as inherited). All wants verified against ruby
// 4.0.6.
func TestSingletonMethodsVisibilityAndScope(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			"excludes private, keeps public and protected",
			`o = Object.new
def o.pub; end
def o.pro; end
def o.pri; end
class << o; protected :pro; private :pri; end
p o.singleton_methods.sort`,
			"[:pro, :pub]\n",
		},
		{
			"false excludes a class's extended-module methods, keeps own",
			`module MM; def mm_pub; end; def mm_pri; end; private :mm_pri; end
class CC; def self.own; end; extend MM; end
p CC.singleton_methods(false).sort`,
			"[:own]\n",
		},
		{
			"true includes a class's public extended-module methods",
			`module MM2; def mm_pub; end; end
class CC2; def self.own; end; extend MM2; end
p CC2.singleton_methods(true).include?(:mm_pub)`,
			"true\n",
		},
		{
			"true still excludes a private extended-module method",
			`module MM3; def mm_pri; end; private :mm_pri; end
class CC3; extend MM3; end
p CC3.singleton_methods(true).include?(:mm_pri)`,
			"false\n",
		},
		{
			"false excludes a plain object's extended-module methods, keeps own",
			`module MM4; def mm_pub; end; end
ob = Object.new
def ob.own2; end
ob.extend(MM4)
p ob.singleton_methods(false).sort`,
			"[:own2]\n",
		},
		{
			"true includes a plain object's extended-module methods",
			`module MM5; def mm_pub; end; end
ob = Object.new
ob.extend(MM5)
p ob.singleton_methods(true).include?(:mm_pub)`,
			"true\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("src=%q\ngot=%q\nwant=%q", c.src, got, c.want)
			}
		})
	}
}
