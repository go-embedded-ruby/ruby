package vm_test

import (
	"strings"
	"testing"
)

// TestHashNewDefault covers Hash.new with a default value, a default block, and
// no default, plus a plain literal — the missing-key paths through hashDefault.
func TestHashNewDefault(t *testing.T) {
	cases := []struct{ src, want string }{
		// Hash.new(default): a miss returns the default without storing it.
		{"h = Hash.new(0)\nh[:a] += 1\nh[:a] += 1\nputs h[:a]\nputs h[:x]\nputs h.size", "2\n0\n1\n"},
		// Hash.new { |hash, key| … }: the block runs on a miss and may store.
		{"h = Hash.new { |hash, k| hash[k] = k * 10 }\nputs h[5]\nputs h.size", "50\n1\n"},
		// Hash.new with no default: a miss reads as nil.
		{"h = Hash.new\np h[:x]", "nil\n"},
		// A literal has no default either.
		{"h = {a: 1}\np h[:z]", "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestHashNewErrors covers the arity guards: more than one default value, and a
// default value together with a block.
func TestHashNewErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"Hash.new(0, 1)", "ArgumentError"},
		{"Hash.new(0) { |h, k| k }", "ArgumentError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
