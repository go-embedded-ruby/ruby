package vm_test

import (
	"strings"
	"testing"
)

// TestArrayElementSetResiduals pins Array#[]= against MRI 4.0.6: single-index
// auto-grow, the (start,length) and Range forms (including a Range subclass and
// #to_int endpoints), nil-padding beyond the end, #to_ary coercion of a
// multi-element rhs, and the exact IndexError/RangeError diagnostics.
func TestArrayElementSetResiduals(t *testing.T) {
	cases := []struct{ src, want string }{
		// Single-index auto-grow (at the end and beyond it).
		{"a=[1,2]; a[2]=3; p a", "[1, 2, 3]\n"},
		{"a=[]; a[1]=14; p a", "[nil, 14]\n"},
		{"a=[1,2]; a[5]=6; p a", "[1, 2, nil, nil, nil, 6]\n"},
		// Negative single index resolves from the end, and returns the rhs.
		{"a=[1,2,3]; p(a[-1]=9); p a", "9\n[1, 2, 9]\n"},
		// (start,length) form pads beyond the end and splices an Array rhs.
		{"a=[1,2,3]; a[5,2]=['x']; p a", "[1, 2, 3, nil, nil, \"x\"]\n"},
		{"a=[1,2,3,4,5]; a[1,2]=['a','b','c']; p a", "[1, \"a\", \"b\", \"c\", 4, 5]\n"},
		// (start,length) with a non-Array rhs stores it as a single element.
		{"a=[1,2,3]; a[0,2]=9; p a", "[9, 3]\n"},
		// Range form: beyond-end pad, exclusive, endless, and a Range subclass.
		{"a=[1,2,3]; a[3..4]=['x']; p a", "[1, 2, 3, \"x\"]\n"},
		{"a=[1,2,3]; a[5..7]=['x']; p a", "[1, 2, 3, nil, nil, \"x\"]\n"},
		{"a=[1,2,3]; a[1...1]=['x']; p a", "[1, \"x\", 2, 3]\n"},
		{"a=[1,2,3,4]; a[1..]=['x']; p a", "[1, \"x\"]\n"},
		{"a=[1,2,3,4]; a[..1]=['x']; p a", "[\"x\", 3, 4]\n"},
		{"class RR<Range;end; a=[1,2,3,4,5]; a[RR.new(1,2)]=['x']; p a", "[1, \"x\", 4, 5]\n"},
		// #to_int coercion of index and start/length arguments.
		{"class C; def to_int; 1; end; end; a=[1,2,3]; a[C.new]=9; p a", "[1, 9, 3]\n"},
		{"class C; def to_int; 1; end; end; a=[1,2,3]; a[C.new,1]=9; p a", "[1, 9, 3]\n"},
		// #to_ary coercion of a multi-element rhs; an Array subclass is spliced.
		{"o=Object.new; def o.to_ary; [7,8]; end; a=[1,2,3]; a[0,2]=o; p a", "[7, 8, 3]\n"},
		{"class AS<Array;end; a=[1,2,3]; a[0,2]=AS.new([5,6,7]); p a", "[5, 6, 7, 3]\n"},
		// A multi-element rhs whose #to_ary does not return an Array is a single elem.
		{"o=Object.new; def o.to_ary; 42; end; a=[1,2,3]; a[0,1]=o; p(a[0].class)", "Object\n"},
		// Range self-assignment must not corrupt mid-splice (repl aliases a.Elems).
		{"a=[1,2,3]; a[0,2]=a; p a", "[1, 2, 3, 3]\n"},
		// Endless range with begin beyond the end (nil-Hi negative-length clamp).
		{"a=[1,2,3]; a[5..]=['x']; p a", "[1, 2, 3, nil, nil, \"x\"]\n"},
		// Inverted range implies a zero-width insert (non-nil-Hi negative-length clamp).
		{"a=[1,2,3]; a[3..1]=['x']; p a", "[1, 2, 3, \"x\"]\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}

func TestArrayElementSetErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{"a=[1,2,3]; a[-5]=9", "index -5 too small for array; minimum: -3"},
		{"a=[1,2,3]; a[-5,1]=9", "index -5 too small for array; minimum: -3"},
		{"a=[1,2,3]; a[0,-1]=9", "negative length (-1)"},
		{"a=[1,2,3]; a[-5..-1]=['x']", "-5..-1 out of range"},
		{"class M;end; a=[1]; a[M.new]=5", "no implicit conversion of M into Integer"},
	}
	for _, tc := range cases {
		err := runErr(t, tc.src)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("src=%q got %v want %q", tc.src, err, tc.want)
		}
	}
}

// TestArrayValuesAtRanges pins Array#values_at Range handling: spans, nil-fill
// for out-of-bounds positions, exclusive/endless/beginless bounds, #to_int
// indices, and the RangeError for a begin still negative after normalisation.
func TestArrayValuesAtRanges(t *testing.T) {
	cases := []struct{ src, want string }{
		{"p [1,2,3,4,5].values_at(0..2, 3..4)", "[1, 2, 3, 4, 5]\n"},
		{"p [1,2,3].values_at(1..5)", "[2, 3, nil, nil, nil]\n"},
		{"p [1,2,3].values_at(4..6)", "[nil, nil, nil]\n"},
		{"p [1,2,3].values_at(1..)", "[2, 3]\n"},
		{"p [1,2,3].values_at(..1)", "[1, 2]\n"},
		{"p [1,2,3,4,5].values_at(1...3)", "[2, 3]\n"},
		{"p [1,2,3,4,5].values_at(-3..-1)", "[3, 4, 5]\n"},
		{"p [1,2,3].values_at(3..1)", "[]\n"},
		{"p [1,2,3,4,5].values_at(0, 1..2, -1)", "[1, 2, 3, 5]\n"},
		{"class C; def to_int; 1; end; end; p [1,2,3].values_at(C.new)", "[2]\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
	if err := runErr(t, "[1,2,3].values_at(-10..-8)"); err == nil || !strings.Contains(err.Error(), "-10..-8 out of range") {
		t.Errorf("values_at neg-oob range: got %v", err)
	}
}

func TestArrayInsertRotateCoerce(t *testing.T) {
	cases := []struct{ src, want string }{
		{"class C; def to_int; 2; end; end; p [1,2,3,4].insert(C.new, :x)", "[1, 2, :x, 3, 4]\n"},
		{"p [1,2,3,4].insert(-2, :x)", "[1, 2, 3, :x, 4]\n"},
		{"p [1,2,3,4].rotate(2.6)", "[3, 4, 1, 2]\n"},
		{"class C; def to_int; 2; end; end; p [1,2,3,4].rotate(C.new)", "[3, 4, 1, 2]\n"},
		{"class C; def to_int; 2; end; end; a=[1,2,3,4]; a.rotate!(C.new); p a", "[3, 4, 1, 2]\n"},
		{"p [].rotate", "[]\n"},
		{"a=[]; a.rotate!; p a", "[]\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
	if err := runErr(t, "class M;end; [1,2].insert(M.new, 9)"); err == nil || !strings.Contains(err.Error(), "no implicit conversion of M into Integer") {
		t.Errorf("insert non-int: got %v", err)
	}
	if err := runErr(t, "[1,2,3,4].insert(-10, :x)"); err == nil || !strings.Contains(err.Error(), "too small for array") {
		t.Errorf("insert too-small: got %v", err)
	}
}

func TestArrayDigResiduals(t *testing.T) {
	cases := []struct{ src, want string }{
		{"p [[1,[2,3]]].dig(0, 1, 0)", "2\n"},
		{"p [1,2,3].dig(1)", "2\n"},
		{"p [1,2,3].dig(9)", "nil\n"},
		{"p [nil].dig(0, 1)", "nil\n"},
		{"p [[1,{a: 2}]].dig(0, 1, :a)", "2\n"},
		// A redefined/overridden #dig on the extracted value is honoured.
		{"o=Object.new; def o.dig(*a); a.sum; end; p [[o]].dig(0, 0, 10, 20)", "30\n"},
		// Hash#dig shares the same continuation (digRest): hit, miss, and nested.
		{"p({a: 1}.dig(:a))", "1\n"},
		{"p({a: 1}.dig(:z))", "nil\n"},
		{"p({a: {b: 2}}.dig(:a, :b))", "2\n"},
		{"p({a: nil}.dig(:a, :b))", "nil\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
	if err := runErr(t, "[1,2,3].dig"); err == nil || !strings.Contains(err.Error(), "ArgumentError") {
		t.Errorf("dig no-args: got %v", err)
	}
	if err := runErr(t, "{a: 1}.dig"); err == nil || !strings.Contains(err.Error(), "ArgumentError") {
		t.Errorf("hash dig no-args: got %v", err)
	}
	if err := runErr(t, "[1,2].dig(0, 0)"); err == nil || !strings.Contains(err.Error(), "does not have #dig method") {
		t.Errorf("dig into Integer: got %v", err)
	}
}

func TestArrayProductResiduals(t *testing.T) {
	cases := []struct{ src, want string }{
		{"p [1,2].product([3,4])", "[[1, 3], [1, 4], [2, 3], [2, 4]]\n"},
		// Block form yields each combination and returns the receiver.
		{"acc=[]; r=[1,2].product([3,4]){|c| acc<<c}; p acc; p r", "[[1, 3], [1, 4], [2, 3], [2, 4]]\n[1, 2]\n"},
		// A non-Array argument is coerced via #to_ary.
		{"o=Object.new; def o.to_ary; [3,4]; end; p [1,2].product(o)", "[[1, 3], [1, 4], [2, 3], [2, 4]]\n"},
		// An empty list makes an empty product.
		{"p [1,2].product([])", "[]\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
	if err := runErr(t, "[1,2].product(5)"); err == nil || !strings.Contains(err.Error(), "no implicit conversion of Integer into Array") {
		t.Errorf("product non-array: got %v", err)
	}
	if err := runErr(t, "[1,2].product(*Array.new(60){[1,2,3,4,5]})"); err == nil || !strings.Contains(err.Error(), "too big to product") {
		t.Errorf("product overflow: got %v", err)
	}
}

func TestArrayFlattenDepthCoerce(t *testing.T) {
	cases := []struct{ src, want string }{
		{"p [1,[2,[3]]].flatten(nil)", "[1, 2, 3]\n"},
		{"class C; def to_int; 1; end; end; p [1,[2,[3]]].flatten(C.new)", "[1, 2, [3]]\n"},
		{"a=[1,[2,[3]]]; a.flatten!(nil); p a", "[1, 2, 3]\n"},
		{"class C; def to_int; 1; end; end; a=[1,[2,[3]]]; a.flatten!(C.new); p a", "[1, 2, [3]]\n"},
	}
	for _, tc := range cases {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
		}
	}
}
