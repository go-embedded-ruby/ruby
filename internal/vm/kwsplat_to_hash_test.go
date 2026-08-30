package vm_test

import (
	"strings"
	"testing"
)

// TestKeywordSplatToHash covers the `**operand` keyword splat when the operand
// is not already a Hash: nil contributes nothing, any other value is coerced via
// #to_hash, and a missing or non-Hash #to_hash is a TypeError — all matching MRI
// Ruby 4.0.6. A Hash operand and a hash-literal `**` are the unchanged fast path.
func TestKeywordSplatToHash(t *testing.T) {
	cases := []struct{ src, want string }{
		// An object with #to_hash is splatted into keyword arguments.
		{`o = Object.new; def o.to_hash; {a: 1, b: 2}; end
def f(**k); k; end
p f(**o)`, "{a: 1, b: 2}\n"},
		// The splat follows positional arguments and mingles with the rest hash.
		{`o = Object.new; def o.to_hash; {x: 9}; end
def g(m, **k); [m, k]; end
p g(1, **o)`, "[1, {x: 9}]\n"},
		// **nil contributes nothing (f(**nil) is f()).
		{`def f(**k); k; end; p f(**nil)`, "{}\n"},
		// A Hash operand splats unchanged (the fast path), and later keys win.
		{`def f(**k); k; end; p f(**{}, **{a: 1})`, "{a: 1}\n"},
		{`h = {a: 1}; o = Object.new; def o.to_hash; {b: 2}; end
def f(**k); k; end
p f(**h, **o)`, "{a: 1, b: 2}\n"},
		// The coercion also applies inside a hash literal's ** entry.
		{`o = Object.new; def o.to_hash; {c: 3}; end; p({x: 0, **o})`, "{x: 0, c: 3}\n"},
		{`p({**{a: 1}, **{b: 2}})`, "{a: 1, b: 2}\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errCases := []struct{ src, substr string }{
		// No #to_hash: the implicit-conversion TypeError.
		{`o = Object.new; def f(**k); k; end; f(**o)`, "no implicit conversion of Object into Hash"},
		// #to_hash returning a non-Hash: the "gives" TypeError.
		{`o = Object.new; def o.to_hash; 42; end; def f(**k); k; end; f(**o)`,
			"can't convert Object to Hash (Object#to_hash gives Integer)"},
	}
	for _, c := range errCases {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.substr) {
			t.Errorf("src=%q err=%v, want containing %q", c.src, err, c.substr)
		}
	}
}
