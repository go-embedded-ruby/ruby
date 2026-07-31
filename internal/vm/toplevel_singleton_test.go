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
