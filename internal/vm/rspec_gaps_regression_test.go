package vm_test

import "testing"

// TestRSpecGapsRegression locks in the RSpec-sweep parse/compile gaps that are
// closed on main (G1 leading `::`, G3 masgn to ivar/attr LHS, G6 bare anonymous
// `*`/`**`/`&` params, G8 explicit `super(*args)`) so they cannot silently
// regress. Each snippet is a tight representative of the gap documented in
// CONFORMANCE-RSPEC.md §4; every expected output is verified against MRI Ruby
// 4.0.5.
func TestRSpecGapsRegression(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// G1 — leading `::` top-level constant reference.
		{
			"G1 leading :: constant reference",
			`Foo = 1
module M; Bar = 2; end
p ::Foo
p ::M::Bar
p defined?(::Foo)`,
			"1\n2\n\"constant\"\n",
		},
		// G3 — multiple assignment with instance-variable LHS targets.
		{
			"G3 masgn to ivar targets",
			`class C
  def initialize; @a, @b = 1, 2; end
  def vals; [@a, @b]; end
end
p C.new.vals`,
			"[1, 2]\n",
		},
		// G3 — multiple assignment with an attribute-setter LHS target.
		{
			"G3 masgn to attribute target",
			`o = Object.new
def o.x=(v); @x = v; end
def o.x; @x; end
o.x, @z = 5, 6
p [o.x, @z]`,
			"[5, 6]\n",
		},
		// G6 — trailing bare anonymous splat parameter.
		{
			"G6 bare anonymous splat *",
			`def take(file, line, *)
  [file, line]
end
p take("a.rb", 1, 2, 3)`,
			"[\"a.rb\", 1]\n",
		},
		// G6 — bare anonymous double-splat and block parameters.
		{
			"G6 bare anonymous ** and &",
			`def kw(a, **)
  a
end
def blk(&)
  block_given?
end
p kw(1, k: 2)
p blk { 1 }`,
			"1\ntrue\n",
		},
		// G8 — explicit super with a splat argument.
		{
			"G8 super(*args)",
			`class A; def m(*a); a; end; end
class B < A; def m(*a); super(*a); end; end
p B.new.m(1, 2)`,
			"[1, 2]\n",
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
