package vm_test

import "testing"

// The OpSend fast path and the general-send fallback hand callees the caller's
// live operand-stack region instead of a per-call copy. Correctness rests on an
// ISeq callee copying its args into env slots synchronously (exec) before control
// returns, and on the rarer native callee getting a defensive copy. These tests
// pin that no retained reference ever aliases the reused operand region: a proc
// (Proc#call, site #1) or a class method (general send, site #2) that captures
// its args must see correct, non-aliased values across repeated same-call-site
// invocations. Run under -race in CI.
func TestArgElideRetention(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// Splat proc that stashes its args array on every call, invoked from one hot
		// call site that reuses the operand-stack region. Each `a` must be a distinct
		// array of that call's args (exec's splat binding allocates a fresh array),
		// never an alias overwritten by the next call.
		{
			"splat_proc_capture",
			"stash = []\n" +
				"f = ->(*a) { stash << a }\n" +
				"i = 0\n" +
				"while i < 4\n  f.call(i, i + 1)\n  i += 1\nend\n" +
				"p stash",
			"[[0, 1], [1, 2], [2, 3], [3, 4]]\n",
		},
		// Fixed-arity proc that stashes an array built from its scalar params.
		{
			"scalar_proc_capture",
			"stash = []\n" +
				"f = ->(x, y) { stash << [x, y] }\n" +
				"i = 0\n" +
				"while i < 3\n  f.call(i, i * 10)\n  i += 1\nend\n" +
				"p stash",
			"[[0, 0], [1, 10], [2, 20]]\n",
		},
		// A proc returned from a method and invoked after that method's frame has
		// returned: it must still bind its args (and closed-over x) correctly.
		{
			"proc_outlives_frame",
			"def make\n  x = 100\n  ->(a, b) { [x, a, b] }\nend\n" +
				"f = make\n" +
				"p f.call(1, 2)\n" +
				"p f.call(3, 4)",
			"[100, 1, 2]\n[100, 3, 4]\n",
		},
		// Proc#[] and Proc#yield are the same nonRetaining native as Proc#call.
		{
			"proc_index_and_yield",
			"f = ->(a, b) { a * b }\n" +
				"p f[6, 7]\n" +
				"p f.yield(3, 4)",
			"42\n12\n",
		},
		// Native-block target of Proc#call (Symbol#to_proc): callBlockSelf copies the
		// live region before the native body reads it.
		{
			"native_proc_call",
			"up = :upcase.to_proc\n" +
				"p up.call(\"hi\")\n" +
				"p up.call(\"world\")",
			"\"HI\"\n\"WORLD\"\n",
		},
		// General-send fallback (site #2): a class method (class receiver, off the
		// monomorphic cache) with a splat that stashes its args each call, from one
		// reused call site. exec's splat binding must give each call a private array.
		{
			"class_method_splat_capture",
			"class Acc\n" +
				"  @@stash = []\n" +
				"  def self.add(*a)\n    @@stash << a\n  end\n" +
				"  def self.stash\n    @@stash\n  end\n" +
				"end\n" +
				"i = 0\n" +
				"while i < 3\n  Acc.add(i, i + 1)\n  i += 1\nend\n" +
				"p Acc.stash",
			"[[0, 1], [1, 2], [2, 3]]\n",
		},
		// General-send fallback via method_missing: the resolved args reach the
		// handler intact.
		{
			"method_missing_fallback",
			"class Ghost\n" +
				"  def method_missing(name, *a)\n    \"#{name}:#{a.inspect}\"\n  end\n" +
				"end\n" +
				"p Ghost.new.foo(1, 2, 3)",
			"\"foo:[1, 2, 3]\"\n",
		},
		// A native class method reached through the fallback still runs correctly
		// (invokeInPlace copies for a retaining native; a non-retaining one reads the
		// region directly).
		{
			"class_method_native",
			"p Array.new(3, 7)",
			"[7, 7, 7]\n",
		},
		// define_method body invoked as a method: the anchored-proc path
		// (callProcMethod) copies args into env via exec; a capturing splat stays
		// non-aliased across calls.
		{
			"define_method_splat_capture",
			"class D\n" +
				"  @@stash = []\n" +
				"  define_method(:add) { |*a| @@stash << a }\n" +
				"  def stash\n    @@stash\n  end\n" +
				"end\n" +
				"d = D.new\n" +
				"i = 0\n" +
				"while i < 3\n  d.add(i, i + 1)\n  i += 1\nend\n" +
				"p d.stash",
			"[[0, 1], [1, 2], [2, 3]]\n",
		},
		// define_method whose body is a native proc (Symbol#to_proc): callProcMethod
		// copies before the native body reads its args.
		{
			"define_method_native_body",
			"class E\n" +
				"  define_method(:shout, :upcase.to_proc)\n" +
				"end\n" +
				"p E.new.shout(\"hey\")",
			"\"HEY\"\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}
