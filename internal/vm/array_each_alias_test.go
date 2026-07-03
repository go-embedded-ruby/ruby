package vm_test

import "testing"

// TestArrayEachScratchAliasing pins down the correctness bar for Array#each's
// reused 1-element scratch slice: a block that retains what it is yielded (a
// captured splat Array, a captured closure over the parameter, or a value pushed
// into another collection) must observe DISTINCT per-iteration values, never the
// last element aliased across every capture. The interpreted-block path reuses
// the scratch slice; the native-block path (&method / &:sym) must fall back to a
// fresh slice, so both branches are exercised here.
func TestArrayEachScratchAliasing(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// Non-capturing: the canonical each-into copy.
		{"copy", `a=[1,2,3]; b=[]; a.each{|x| b<<x}; p b`, "[1, 2, 3]\n"},
		// Splat capture: each |*w| builds its own rest Array; stashing them must
		// not alias, so every captured w keeps its own element.
		{"splat_capture", `a=[1,2,3]; s=[]; a.each{|*w| s<<w}; p s`, "[[1], [2], [3]]\n"},
		// Closure capture: a lambda closing over the block parameter must see the
		// value from its own iteration, not the final one.
		{"closure_capture", `fs=[]; [10,20,30].each{|x| fs << ->{x}}; p fs.map{|f| f.call}`, "[10, 20, 30]\n"},
		// Auto-splat over pairs: scratch[0] is itself an Array each iteration.
		{"auto_splat_pairs", `s=[]; [[1,2],[3,4]].each{|a,b| s<<[a,b]}; p s`, "[[1, 2], [3, 4]]\n"},
		// Native block via a bound Method: exercises the fresh-slice fallback while
		// asserting each element is delivered.
		{"native_method_block", `out=[]; [1,2,3].each(&out.method(:push)); p out`, "[1, 2, 3]\n"},
		// Native block via Symbol#to_proc: no observable capture, but drives the
		// native branch to completion.
		{"native_sym_block", `[1,2,3].each(&:to_s); p :ok`, ":ok\n"},
		// Empty array: neither branch iterates.
		{"empty", `[].each{|x| raise "nope"}; p :ok`, ":ok\n"},
		// No block: returns an Enumerator.
		{"no_block_enum", `p [1,2,3].each.class`, "Enumerator\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestNonRetainingNativeElision covers the invokeInPlace copy-elision allow-list
// (Array/Hash #[] and #[]=). The h[w] += 1 tally and a[i] = a[i] + 1 in-place
// update both read and write index natives against the live operand-stack region
// with no defensive copy; a corrupted args backing would produce wrong counts or
// values.
func TestNonRetainingNativeElision(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"hash_tally", `h=Hash.new(0); %w[a b a c a b a].each{|w| h[w]+=1}; p [h["a"], h["b"], h["c"]]`, "[4, 2, 1]\n"},
		{"array_index_read", `a=[10,20,30]; p [a[0], a[1], a[-1]]`, "[10, 20, 30]\n"},
		{"array_index_write", `a=[1,2,3]; 3.times{|i| a[i]=a[i]+1}; p a`, "[2, 3, 4]\n"},
		{"hash_default_proc", `h=Hash.new{|hh,k| hh[k]=k.to_s}; h[7]; h[8]; p h`, "{7 => \"7\", 8 => \"8\"}\n"},
		{"array_slice", `a=[1,2,3,4,5]; p a[1,3]`, "[2, 3, 4]\n"},
		{"array_range", `a=[1,2,3,4,5]; p a[1..3]`, "[2, 3, 4]\n"},
		{"hash_set_returns_value", `h={}; x=(h[:k]=42); p x`, "42\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
