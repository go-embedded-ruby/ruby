package vm_test

import "testing"

// Splat (`*v`) coercion mirrors MRI: an Array passes through; an object that
// responds to #to_a is converted with it (MatchData, nil, …); anything else,
// including an object with only #to_ary, is wrapped in a one-element Array.
func TestSplatCoercion(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"array", `a = *[1, 2, 3]; p a`, "[1, 2, 3]\n"},
		{"nil", `a = *nil; p a`, "[]\n"},
		{"scalar", `a = *5; p a`, "[5]\n"},
		{"string", `a = *"x"; p a`, "[\"x\"]\n"},
		{"matchdata", `m = "1.2.3".match(/(\d)\.(\d)\.(\d)/); a = *m; p a`,
			"[\"1.2.3\", \"1\", \"2\", \"3\"]\n"},
		{"to_a_object", `class Foo; def to_a; [1, 2, 3]; end; end; a = *Foo.new; p a`,
			"[1, 2, 3]\n"},
		{"to_ary_only_wraps", `class Bar; def to_ary; [9, 9]; end; end; a = *Bar.new; p a.length`,
			"1\n"},
		{"destructure_matchdata", `m = "1.2.3".match(/(\d)\.(\d)\.(\d)/); _, x, y, z = *m; p [x, y, z]`,
			"[\"1\", \"2\", \"3\"]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("eval(%q) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// A #to_a that returns a non-Array raises TypeError, as in MRI.
func TestSplatToANonArray(t *testing.T) {
	src := `class Bad; def to_a; 42; end; end; a = *Bad.new; p a`
	class, _ := evalErr(t, src)
	if class != "TypeError" {
		t.Errorf("expected TypeError, got %q", class)
	}
}
