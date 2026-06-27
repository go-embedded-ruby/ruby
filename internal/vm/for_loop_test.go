package vm_test

import "testing"

// TestForLoop exercises the `for VARS in ITER ... end` form, which the compiler
// lowers to `ITER.each { ... }` with the crucial difference that the loop
// variables live in the ENCLOSING scope (so they leak past the loop) rather than
// in a fresh block scope. Each case is a behaviour MRI also produces.
func TestForLoop(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// Single variable; it leaks with its last value after the loop.
		{"single_leaks", "for i in [1,2,3]; p i; end; p i", "1\n2\n3\n3\n"},
		// Destructuring multiple variables from each yielded element.
		{"destructure", "for a, b in [[1,2],[3,4]]; p a+b; end", "3\n7\n"},
		// Iterating a Range.
		{"range", "for x in 1..3; p x; end", "1\n2\n3\n"},
		// The body can read and write enclosing locals normally.
		{"accumulate", "s=0; for i in [1,2,3]; s+=i; end; p s", "6\n"},
		// break stops the loop; the loop var keeps the value it had at the break.
		{"break_leaks", "for i in [1,2,3]; break if i==2; end; p i", "2\n"},
		// next skips the rest of the iteration like in any loop.
		{"next_skips", "for i in [1,2,3,4]; next if i.even?; p i; end", "1\n3\n"},
		// `for` evaluates to the iterable (MRI returns the collection).
		{"value_is_iterable", "r = for x in 1..3; end; p r", "1..3\n"},
		{"value_is_array", "r = for x in [1,2]; end; p r", "[1, 2]\n"},
		// A loop variable that already exists in the enclosing scope is reused
		// (resolves to the existing local) rather than shadowed.
		{"reuse_existing", "i = 99; for i in [7,8]; end; p i", "8\n"},
		// A pre-existing variable destructured across stays a single enclosing
		// local too.
		{"reuse_existing_multi", "a = 0; b = 0; for a, b in [[1,2]]; end; p [a, b]", "[1, 2]\n"},
		// Empty iterable: the body never runs and the loop var stays untouched.
		{"empty_iter", "x = :sentinel; for x in []; end; p x", ":sentinel\n"},
		// next with no following body still iterates fully.
		{"next_plain", "n=0; for i in [1,2,3]; n+=1; next; end; p n", "3\n"},
		// Nested for loops; the inner var leaks but is overwritten each pass.
		{"nested", "for a in [1,2]; for b in [10,20]; end; end; p [a, b]", "[2, 20]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
