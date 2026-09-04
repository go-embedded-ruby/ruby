// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"regexp"
	"testing"
)

// TestArrayLiveIteration covers the index-based iteration of Array#each and the
// collect/filter family: an element the block appends during iteration is
// visited too (MRI's "tolerates increasing an array size during iteration").
// Blocks avoid the != operator on purpose (an unrelated VM inline-cache defect
// mis-evaluates a block's first !=). Verified against MRI Ruby 4.0.
func TestArrayLiveIteration(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		// each visits appended tail.
		{`a=[1,2,3]; s=[]; i=0; a.each{|e| s<<e; a<<10 if i<1; i+=1}; p s`, "[1, 2, 3, 10]\n"},
		// map maps the appended tail.
		{`a=[1,2,3]; i=0; r=a.map{|e| a<<9 if i<1; i+=1; e*2}; p r`, "[2, 4, 6, 18]\n"},
		// select tests the appended tail.
		{`a=[1,2]; i=0; r=a.select{|e| a<<3 if i<1; i+=1; e>1}; p r`, "[2, 3]\n"},
		// sum folds the appended tail.
		{`a=[1,2]; i=0; r=a.sum{|e| a<<3 if i<1; i+=1; e}; p r`, "6\n"},
		// to_h processes the appended tail.
		{`a=[[1,:a]]; i=0; h=a.to_h{|p| a<<[2,:b] if i<1; i+=1; p}; p h`, "{1 => :a, 2 => :b}\n"},
		// take_while considers the appended tail.
		{`a=[1,2]; i=0; r=a.take_while{|e| a<<3 if i<1; i+=1; e<10}; p r`, "[1, 2, 3]\n"},
		// sort_by keys the appended tail.
		{`a=[3,1]; i=0; r=a.sort_by{|e| a<<2 if i<1; i+=1; e}; p r`, "[1, 2, 3]\n"},
		// map! maps the appended tail in place.
		{`a=[1,2]; i=0; a.map!{|e| a<<3 if i<1; i+=1; e*10}; p a`, "[10, 20, 30]\n"},
		// each_index yields the appended index.
		{`a=[1,2]; s=[]; i=0; a.each_index{|k| s<<k; a<<9 if i<1; i+=1}; p s`, "[0, 1, 2]\n"},
	})
}

// TestArrayKeepIfInPlace covers arrayKeepIf: normal removal, the "no change"
// nil return of select!, the panic-safe partial compaction when the block raises
// mid-iteration (the decided prefix plus the raising element and the rest), and
// the full-truncate path when the block shrinks the array below the current
// index before raising. Verified against MRI Ruby 4.0.
func TestArrayKeepIfInPlace(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`a=[5,6,7,8]; a.keep_if{|e| e==6 ? false : true}; p a`, "[5, 7, 8]\n"},
		{`p([1,2,3].select!{|e| e>0})`, "nil\n"}, // nothing removed → nil
		{`p([1,2,3].reject!{|e| e>0})`, "[]\n"},  // all removed
		// The block raises at 3: 1 kept, 2 dropped, then 3 and 4 retained.
		{`a=[1,2,3,4]; begin; a.keep_if{|e| next false if e==2; raise "x" if e==3; true}; rescue; end; p a`,
			"[1, 3, 4]\n"},
		{`a=[1,2,3,4]; begin; a.reject!{|e| next true if e==2; raise "x" if e==3; false}; rescue; end; p a`,
			"[1, 3, 4]\n"},
		// The block clears the array (shrinking below i) then raises → empty.
		{`b=[1,2,3]; begin; b.keep_if{|e| b.clear; raise "x"}; rescue; end; p b`, "[]\n"},
		// delete_if shares the same compaction.
		{`a=[1,2,3,4]; a.delete_if{|e| e==2 || e==4}; p a`, "[1, 3]\n"},
	})
}

// TestArrayInspectDispatch covers Array#inspect rendering each element through
// its own Ruby #inspect, the #to_s and #<Class:0x…> identity fall-backs of MRI's
// rb_obj_as_string, the recursion sentinel, a BasicObject element (no #inspect),
// and the #to_s alias identity. Verified against MRI Ruby 4.0.
func TestArrayInspectDispatch(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`class C; def inspect; "CUSTOM"; end; end; puts [C.new].inspect`, "[CUSTOM]\n"},
		{`class D; def inspect; self; end; def to_s; "dee"; end; end; puts [D.new].inspect`, "[dee]\n"},
		{`a=[1]; a<<a; puts a.inspect`, "[1, [...]]\n"},
		{`puts [].inspect`, "[]\n"},
		{`puts [1,[2,3]].inspect`, "[1, [2, 3]]\n"},
		// #to_s is a true alias of #inspect (shared UnboundMethod).
		{`p(Array.instance_method(:to_s) == Array.instance_method(:inspect))`, "true\n"},
	})
	// Identity fall-back: #inspect and #to_s both return non-strings → #<Class:0x…>.
	got := eval(t, `class OS3; def inspect; self; end; def to_s; self; end; end; puts [OS3.new].inspect`)
	if !regexp.MustCompile(`^\[#<OS3:0x[0-9a-f]+>\]\n$`).MatchString(got) {
		t.Errorf("identity fallback inspect = %q", got)
	}
	// A bare BasicObject element (no #inspect / #respond_to?) uses native rendering.
	got = eval(t, `class BO < BasicObject; end; puts [BO.new].inspect`)
	if !regexp.MustCompile(`^\[#<BO`).MatchString(got) {
		t.Errorf("BasicObject element inspect = %q", got)
	}
}

// TestArraySetOpCoercion covers #to_ary coercion of set-operation arguments
// (Array#|, #&, #-, #union, #intersection, #difference, #intersect?): an Array
// subclass is used directly, any other value converts through #to_ary, and a
// non-convertible argument raises TypeError. Verified against MRI Ruby 4.0.
func TestArraySetOpCoercion(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		// The #| and #& operator METHODS coerce via #to_ary; reach them through #send
		// (bare `a | b` in source compiles to an opcode that does not dispatch them).
		{`class TA; def to_ary; [7,8]; end; end; p([1].send(:|, TA.new))`, "[1, 7, 8]\n"},
		{`class TA; def to_ary; [2,3]; end; end; p([1,2,3].send(:&, TA.new))`, "[2, 3]\n"},
		{`class TA; def to_ary; [3,4]; end; end; p([1,2].union(TA.new))`, "[1, 2, 3, 4]\n"},
		{`class TA; def to_ary; [2,9]; end; end; p([1,2,3].intersection(TA.new))`, "[2]\n"},
		{`class TA; def to_ary; [2]; end; end; p([1,2].difference(TA.new))`, "[1]\n"},
		{`class TA; def to_ary; [3]; end; end; p([1,2].intersect?(TA.new))`, "false\n"},
		{`class Sub < Array; end; p([1,2,3].send(:&, Sub.new([2,3])))`, "[2, 3]\n"}, // subclass direct
		{`begin; [1].send(:|, 5); rescue TypeError=>e; puts e.message; end`,
			"no implicit conversion of Integer into Array\n"},
	})
}

// TestArrayInitialize covers Array.new / Array#initialize: copying another array
// through #to_ary, a #to_int size, the 3+-argument and negative/Bignum size
// errors, break-safe incremental build, and #initialize being private. Verified
// against MRI Ruby 4.0.
func TestArrayInitialize(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`class F; def to_ary; [7,8]; end; end; p Array.new(F.new)`, "[7, 8]\n"},
		{`class G; def to_int; 3; end; end; p Array.new(G.new, 0)`, "[0, 0, 0]\n"},
		{`p Array.new(3){|i| i*2}`, "[0, 2, 4]\n"},
		{`p Array.new([1,2,3])`, "[1, 2, 3]\n"}, // Array argument copied
		{`p Array.new`, "[]\n"},
		{`r=Array.new(5){|i| break "B" if i==3; i.to_s}; p r`, `"B"` + "\n"},
		{`begin; Array.new(1,2,3); rescue ArgumentError=>e; puts e.message; end`,
			"wrong number of arguments (given 3, expected 0..2)\n"},
		{`begin; Array.new(-1); rescue ArgumentError=>e; puts e.message; end`, "negative array size\n"},
		{`begin; Array.new(10**40); rescue ArgumentError=>e; puts e.message; end`, "array size too big\n"},
		{`p(Array.private_instance_methods.include?(:initialize))`, "true\n"},
	})
}

// TestArrayToHCoercion covers Array#to_h coercing a non-Array pair through
// #to_ary and rejecting a positional argument. Verified against MRI Ruby 4.0.
func TestArrayToHCoercion(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`class H; def to_ary; [:k,:v]; end; end; p [H.new].to_h`, "{k: :v}\n"},
		{`begin; [[1,2]].to_h(:x); rescue ArgumentError=>e; puts e.message; end`,
			"wrong number of arguments (given 1, expected 0)\n"},
	})
}

// TestArrayCountArgCoercion covers #first/#last/#pop/#shift/#drop/#permutation
// coercing a count through #to_int, the pop/shift over-argument error, and the
// first-with-Bignum RangeError. Verified against MRI Ruby 4.0.
func TestArrayCountArgCoercion(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`class N; def to_int; 2; end; end; p [1,2,3,4].first(N.new)`, "[1, 2]\n"},
		{`class N; def to_int; 2; end; end; p [1,2,3,4].last(N.new)`, "[3, 4]\n"},
		{`class N; def to_int; 2; end; end; a=[1,2,3,4]; p a.pop(N.new); p a`, "[3, 4]\n[1, 2]\n"},
		{`class N; def to_int; 2; end; end; a=[1,2,3,4]; p a.shift(N.new); p a`, "[1, 2]\n[3, 4]\n"},
		{`class N; def to_int; 2; end; end; p [1,2,3,4].drop(N.new)`, "[3, 4]\n"},
		{`p [1,2,3].permutation(2.0).to_a.length`, "6\n"}, // Float truncated via to_int
		{`begin; [1,2].pop(1,2); rescue ArgumentError=>e; puts e.message; end`,
			"wrong number of arguments (given 2, expected 0..1)\n"},
		{`begin; [1,2].shift(1,2); rescue ArgumentError=>e; puts e.message; end`,
			"wrong number of arguments (given 2, expected 0..1)\n"},
		{`begin; [1,2,3].first(10**30); rescue RangeError; puts "RE"; end`, "RE\n"},
	})
}

// TestArrayConcatReplaceDelete covers #concat / #replace coercing through
// #to_ary, concat snapshotting each argument (so concat(self) doubles once),
// #transpose coercing rows, #delete only checking frozen on an actual removal,
// and #rindex re-checking the live length when a block shrinks the array.
// Verified against MRI Ruby 4.0.
func TestArrayConcatReplaceDelete(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`class TA; def to_ary; [3,4]; end; end; a=[1,2]; a.concat(TA.new); p a`, "[1, 2, 3, 4]\n"},
		{`a=[1,2]; a.concat(a,a); p a`, "[1, 2, 1, 2, 1, 2]\n"},
		{`class TA; def to_ary; [9,9]; end; end; a=[1]; a.replace(TA.new); p a`, "[9, 9]\n"},
		{`class R; def to_ary; [3,4]; end; end; p [[1,2],R.new].transpose`, "[[1, 3], [2, 4]]\n"},
		{`a=[1,2,3].freeze; p a.delete(9)`, "nil\n"}, // absent → no frozen error
		{`begin; [1,2,3].freeze.delete(2); rescue FrozenError; puts "FE"; end`, "FE\n"},
		{`a=[4,5,6,7]; r=a.rindex{|e| a.clear; e==4}; p r`, "nil\n"}, // shrink mid-scan, no crash
	})
}

// TestArrayAliases covers the shared-record aliases so their UnboundMethods are
// identical (as MRI's aliases are), plus their functionality. Verified against
// MRI Ruby 4.0.
func TestArrayAliases(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`a=[1]; a.append(2,3); p a`, "[1, 2, 3]\n"},
		{`a=[1]; a.prepend(2,3); p a`, "[2, 3, 1]\n"},
		{`p [1,2].to_ary.equal?([1,2].to_ary)`, "false\n"}, // returns self (fresh arrays differ)
		{`x=[1,2]; p x.to_ary.equal?(x)`, "true\n"},        // to_ary returns self
		{`p [1,2,3].collect{|e| e*2}`, "[2, 4, 6]\n"},
		{`p [1,2,3].filter{|e| e>1}`, "[2, 3]\n"},
		{`a=[1,2,3]; a.collect!{|e| e+1}; p a`, "[2, 3, 4]\n"},
		{`a=[1,2,3]; a.filter!{|e| e>1}; p a`, "[2, 3]\n"},
		{`p(Array.instance_method(:append) == Array.instance_method(:push))`, "true\n"},
		{`p(Array.instance_method(:prepend) == Array.instance_method(:unshift))`, "true\n"},
		{`p(Array.instance_method(:collect) == Array.instance_method(:map))`, "true\n"},
		{`p(Array.instance_method(:filter) == Array.instance_method(:select))`, "true\n"},
		{`p(Array.instance_method(:collect!) == Array.instance_method(:map!))`, "true\n"},
		{`p(Array.instance_method(:filter!) == Array.instance_method(:select!))`, "true\n"},
	})
}

// TestArrayFlattenCoercion covers Array#flatten coercing elements through
// #to_ary (a real one, one reached via #method_missing behind
// respond_to_missing?, a nil result kept, and a non-Array result raising
// TypeError), depth limiting, and the recursion guard. Verified against MRI
// Ruby 4.0.
func TestArrayFlattenCoercion(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`class I; def to_ary; [1,2]; end; end; p [I.new,3].flatten`, "[1, 2, 3]\n"},
		{`class MM
  def respond_to_missing?(n,inc); n==:to_ary; end
  def method_missing(n,*a); n==:to_ary ? [4,5] : super; end
end
p [MM.new,6].flatten`, "[4, 5, 6]\n"},
		{`class FN; def to_ary; nil; end; end; p [FN.new,1].flatten.size`, "2\n"},
		{`class FT; def to_ary; 5; end; end; begin; [FT.new].flatten; rescue TypeError; puts "TE"; end`, "TE\n"},
		{`p [1,[2,[3,[4]]]].flatten(1)`, "[1, 2, [3, [4]]]\n"},
		{`a=[1]; a<<a; begin; a.flatten; rescue ArgumentError=>e; puts e.message; end`,
			"tried to flatten recursive array\n"},
	})
}

// TestArraySortBlockCmp covers the sort block comparator turning its result into
// a sign like MRI's rb_cmpint: a small Integer, a Bignum, a Float compared via
// #<=> 0, and a non-comparable result raising. Verified against MRI Ruby 4.0.
func TestArraySortBlockCmp(t *testing.T) {
	runCases(t, []struct{ src, want string }{
		{`p [3,1,2].sort{|a,b| a-b}`, "[1, 2, 3]\n"},            // small Integer sign
		{`p [3,1,2].sort{|a,b| (a-b)*(10**20)}`, "[1, 2, 3]\n"}, // Bignum sign
		{`p [3,1,2].sort{|a,b| (a-b).to_f}`, "[1, 2, 3]\n"},     // Float via <=> 0
		{`begin; [1,2].sort{|a,b| "x"}; rescue ArgumentError=>e; puts e.message; end`,
			"comparison of String with 0 failed\n"},
	})
}
