package vm_test

import "testing"

// TestInlineCacheInvalidation exercises every mutation that must bust the
// per-send-site inline method cache. Each program calls a method inside a loop
// (so the send site is warmed and its cache filled) and then performs a change
// that alters what that same send must resolve to; if the cache were not
// invalidated the post-change calls would wrongly keep returning the old method.
// Every expectation is the MRI Ruby result.
func TestInlineCacheInvalidation(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			// Redefining an instance method after the send site is warm.
			name: "method redefinition",
			src: `class C
  def f; 1; end
end
c = C.new
3.times { c.f }      # warm the cache on C#f
class C
  def f; 2; end       # redefine: must bust the cache
end
p c.f`,
			want: "2\n",
		},
		{
			// define_method installs a new body for an already-cached name.
			name: "define_method override",
			src: `class C
  def f; 1; end
end
c = C.new
3.times { c.f }
class C
  define_method(:f) { 2 }
end
p c.f`,
			want: "2\n",
		},
		{
			// A module include changes the ancestry the send resolves through.
			name: "module include",
			src: `module M
  def f; 2; end
end
class C
  def f; 1; end
end
c = C.new
3.times { c.f }       # resolves to C#f, cached
class C
  prepend M            # M now shadows C#f
end
p c.f`,
			want: "2\n",
		},
		{
			// include adds a method that did not previously exist on the class.
			name: "include adds method",
			src: `module M
  def g; 7; end
end
class C; end
c = C.new
# warm a *different* send so the iseq has caches, then add g via include
3.times { c.class }
class C
  include M
end
p c.g`,
			want: "7\n",
		},
		{
			// A singleton method defined on one object must override the cached
			// instance method *for that object only* — and the cache, keyed on the
			// shared class, must not serve the singleton body to siblings.
			name: "singleton overrides instance",
			src: `class C
  def f; 1; end
end
a = C.new
b = C.new
3.times { a.f; b.f }    # warm C#f for both
def a.f; 99; end        # singleton on a only
p a.f
p b.f`,
			want: "99\n1\n",
		},
		{
			// define_singleton_method form, same requirement.
			name: "define_singleton_method overrides instance",
			src: `class C
  def f; 1; end
end
a = C.new
3.times { a.f }
a.define_singleton_method(:f) { 42 }
p a.f`,
			want: "42\n",
		},
		{
			// extend mixes a module into a single object after the cache is warm.
			name: "extend after warm",
			src: `module M
  def f; :ext; end
end
class C
  def f; :base; end
end
o = C.new
3.times { o.f }
o.extend(M)
p o.f`,
			want: ":ext\n",
		},
		{
			// Polymorphic site: two receiver classes flow through the same send,
			// each must resolve to its own method (the cache is monomorphic, so the
			// class-pointer guard must reject the other class and refill).
			name: "polymorphic site",
			src: `class A; def f; "a"; end; end
class B; def f; "b"; end; end
def call(x); x.f; end
arr = [A.new, B.new, A.new, B.new]
arr.each { |o| print call(o) }
puts`,
			want: "abab\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.src); got != c.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
			}
		})
	}
}

// TestInlineCacheHitCorrectness verifies that a warm monomorphic cache returns
// the right method across many calls (the steady-state hit path), and that a
// recursive method (fib) — the canonical call-bound benchmark whose send site is
// re-entered through itself — still computes correctly with caching on.
func TestInlineCacheHitCorrectness(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class C; def add(a,b); a+b; end; end
c = C.new
s = 0
1000.times { |i| s = c.add(s, 1) }
p s`, "1000\n"},
		{`def fib(n); n < 2 ? n : fib(n-1) + fib(n-2); end
p fib(20)`, "6765\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestSingletonViaSend dispatches a per-object singleton method through
// Object#send, which routes via vm.send (not the OpSend fast path) and must
// consult the receiver's singleton class. Asserted against MRI.
func TestSingletonViaSend(t *testing.T) {
	if got := eval(t, "o = Object.new\ndef o.foo; :sing; end\np o.send(:foo)"); got != ":sing\n" {
		t.Errorf("singleton via send: got %q want %q", got, ":sing\n")
	}
}
