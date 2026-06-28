// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

// Coverage for the Puppet racc-parser batch: the four primitive divergences from
// MRI that blocked `require "puppet"` once the racc LALR runtime ran.
//
//   - builtins.go  Proc.new { ... }  returning the captured block (was the
//                  generic Class#new, producing a bare RObject whose .call failed).
//   - builtins.go  Array#[start, len]= and Array#[range]= slice assignment plus
//                  the arraySpliceAssign helper (was: only the single-index form,
//                  so the 3-arg/range forms corrupted the array — the exact
//                  miscompute that derailed racc's @racc_state stack).
//   - builtins.go  Module#constants (was undefined), with the inherit flag.
//   - builtins.go  Hash#to_h with a block (was ignored, returning self).
//   - vm.go        `super` invoked from inside a block resolving against the
//                  enclosing method (was: "super called outside of method").
//
// Each expectation is asserted against MRI 4.0.5 (ruby -e). Error paths are
// captured via begin/rescue inside the snippet so the tests stay Windows-portable.
// runSrc (aot_dispatch_test.go) runs a snippet on a fresh VM and trims stdout.

import "testing"

// TestProcNew proves Proc.new { ... } yields a real Proc whose #call runs the
// block, including the form where the block is implicit from an enclosing method.
func TestProcNew(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p Proc.new { |x| x + 1 }.class`, "Proc"},
		{`p Proc.new { |x| x + 1 }.call(5)`, "6"},
		{`def m; Proc.new { 42 }; end; p m.call`, "42"},
		// proc / lambda still match.
		{`p proc { |x| x * 2 }.call(5)`, "10"},
		{`p lambda { |x| x - 1 }.call(5)`, "4"},
		{`p lambda {}.lambda?`, "true"},
		// Proc.new with no block raises ArgumentError.
		{`p(begin; Proc.new; rescue => e; e.class.name; end)`, `"ArgumentError"`},
		{`p(begin; proc; rescue => e; e.class.name; end)`, `"ArgumentError"`},
		{`p(begin; lambda; rescue => e; e.class.name; end)`, `"ArgumentError"`},
	}
	for _, tc := range cases {
		if got := runSrc(t, tc.src); got != tc.want {
			t.Errorf("%s => %q, want %q", tc.src, got, tc.want)
		}
	}
}

// TestArraySliceAssign drives every Array slice-assignment shape against MRI: the
// start/length form (splice, single-element, delete, grow with nil padding,
// negative start) and the range form, plus the error branches.
func TestArraySliceAssign(t *testing.T) {
	cases := []struct{ src, want string }{
		// The original racc regression: dropping the top of the state stack.
		{`a=[0,110]; a[-1,1]=[]; p a`, "[0]"},
		{`a=[0,110,5]; a[-2,2]=[]; p a`, "[0]"},
		{`a=[9]; a[-1,1]=[]; p a`, "[]"},
		// start/length splicing.
		{`a=[1,2,3,4,5]; a[1,2]=[9,9,9]; p a`, "[1, 9, 9, 9, 4, 5]"},
		{`a=[1,2,3,4,5]; a[1,2]=99; p a`, "[1, 99, 4, 5]"},
		{`a=[1,2,3,4,5]; a[1,2]=[]; p a`, "[1, 4, 5]"},
		{`a=[1,2,3,4,5]; a[1,2]=nil; p a`, "[1, nil, 4, 5]"},
		// start beyond the end pads with nil.
		{`a=[1,2,3]; a[5,2]=[8]; p a`, "[1, 2, 3, nil, nil, 8]"},
		// negative start.
		{`a=[1,2,3]; a[-1,1]=[7,7]; p a`, "[1, 2, 7, 7]"},
		// zero length inserts.
		{`a=[1,2,3]; a[1,0]=[8]; p a`, "[1, 8, 2, 3]"},
		// range form, array and non-array RHS.
		{`a=[1,2,3]; a[1..2]=[5]; p a`, "[1, 5]"},
		{`a=[1,2,3]; a[1..2]=9; p a`, "[1, 9]"},
		{`a=[1,2,3,4]; a[1...3]=[0]; p a`, "[1, 0, 4]"},
		// single-index form is untouched.
		{`a=[1,2,3]; a[0]=9; p a`, "[9, 2, 3]"},
		{`a=[1,2,3]; a[-1]=9; p a`, "[1, 2, 9]"},
		{`a=[1,2,3]; a[3]=9; p a`, "[1, 2, 3, 9]"},
		// error branches.
		{`p(begin; [1,2,3][5]=9; [1][-9]=9; rescue => e; e.class.name; end)`, `"IndexError"`},
		{`p(begin; a=[1,2,3]; a[-9,1]=[]; rescue => e; e.class.name; end)`, `"IndexError"`},
		{`p(begin; a=[1,2,3]; a[1,-1]=[]; rescue => e; e.class.name; end)`, `"IndexError"`},
		{`p(begin; a=[1,2,3]; a[-9..1]=[]; rescue => e; e.class.name; end)`, `"RangeError"`},
	}
	for _, tc := range cases {
		if got := runSrc(t, tc.src); got != tc.want {
			t.Errorf("%s => %q, want %q", tc.src, got, tc.want)
		}
	}
}

// TestModuleConstants drives Module#constants with and without the inherit flag
// and an empty module, matching the (sorted) names MRI reports.
func TestModuleConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{"class Foo; BAR=1; BAZ=2; end; p Foo.constants", "[:BAR, :BAZ]"},
		{"module M; X=9; end; p M.constants", "[:X]"},
		{"module Empty; end; p Empty.constants", "[]"},
		// inherit=true (default) walks ancestors but skips Object's constants.
		{"module Mix; MIXED=1; end; class P; PARENT=1; end; class C < P; include Mix; OWN=1; end; p C.constants",
			"[:MIXED, :OWN, :PARENT]"},
		// inherit=false reports only the receiver's own constants.
		{"class P2; PARENT=1; end; class C2 < P2; OWN=1; end; p C2.constants(false)",
			"[:OWN]"},
	}
	for _, tc := range cases {
		if got := runSrc(t, tc.src); got != tc.want {
			t.Errorf("%s => %q, want %q", tc.src, got, tc.want)
		}
	}
}

// TestHashToHBlock proves Hash#to_h maps pairs through a block (returning self
// without one), including the MRI error branches for a bad block result.
func TestHashToHBlock(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p({a: 1, b: 2}.to_h)`, "{a: 1, b: 2}"},
		{`p({a: 1, b: 2}.to_h { |k, v| [k.to_s, v * 10] })`, `{"a" => 10, "b" => 20}`},
		// later pairs overwrite earlier ones on a key collision.
		{`p({a: 1, b: 2}.to_h { |k, v| [:same, v] })`, "{same: 2}"},
		{`p(begin; {a: 1}.to_h { |k, v| 5 }; rescue => e; e.class.name; end)`, `"TypeError"`},
		{`p(begin; {a: 1}.to_h { |k, v| [1, 2, 3] }; rescue => e; e.class.name; end)`, `"ArgumentError"`},
	}
	for _, tc := range cases {
		if got := runSrc(t, tc.src); got != tc.want {
			t.Errorf("%s => %q, want %q", tc.src, got, tc.want)
		}
	}
}

// TestSuperInsideBlock proves `super` written inside a block resolves against the
// enclosing method (a block is transparent to super as it is to yield): bare
// super forwards the home method's arguments, explicit super passes its own, the
// anchor survives nesting through several blocks, and super outside any method
// still raises.
func TestSuperInsideBlock(t *testing.T) {
	cases := []struct{ src, want string }{
		{`class A; def g(x); "A:#{x}"; end; end
class B < A; def h; yield 5; end; def g(x); h { |z| super(z) }; end; end
print B.new.g(99)`, "A:5"},
		// bare super forwards the home method's args, called repeatedly via a block.
		{`class A; def f(x, y); "A:#{x},#{y}"; end; end
class B < A; def each; yield 1; yield 2; end; def f(x, y); out=[]; each { |z| out << super }; out.join("|"); end; end
print B.new.f(10, 20)`, "A:10,20|A:10,20"},
		// nested blocks: the super anchor is inherited through both.
		{`class A; def f(x, y); "A:#{x},#{y}"; end; end
class C < A; def wrap; yield; end; def f(x, y); wrap { wrap { super(99, 88) } }; end; end
print C.new.f(1, 2)`, "A:99,88"},
		// keyword arguments forwarded by a bare super from the method's own frame.
		{`class A; def kw(a:); "A:#{a}"; end; end
class B < A; def kw(a:); super; end; end
print B.new.kw(a: 7)`, "A:7"},
		// super outside any method raises (the block has no enclosing method).
		{`p(begin; [1].each { super }; rescue Exception => e; e.message; end)`,
			`"super called outside of method"`},
	}
	for _, tc := range cases {
		if got := runSrc(t, tc.src); got != tc.want {
			t.Errorf("%s => %q, want %q", tc.src, got, tc.want)
		}
	}
}
