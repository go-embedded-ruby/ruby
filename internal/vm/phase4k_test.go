package vm_test

import (
	"strings"
	"testing"
)

func TestHashBatch(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"slice", `p({a: 1, b: 2, c: 3}.slice(:a, :c))`, "{a: 1, c: 3}\n"},
		{"slice_missing", `p({a: 1, b: 2}.slice(:z))`, "{}\n"},
		{"except", `p({a: 1, b: 2, c: 3}.except(:b))`, "{a: 1, c: 3}\n"},
		{"except_none", `p({a: 1}.except)`, "{a: 1}\n"},
		{"merge_bang", `p({a: 1, b: 2}.merge!({b: 9, c: 3}))`, "{a: 1, b: 9, c: 3}\n"},
		{"merge_bang_mutates", "h = {a: 1}\nh.merge!({b: 2})\np h", "{a: 1, b: 2}\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestMergeBangTypeError(t *testing.T) {
	if err := runErr(t, `({a: 1}).merge!(5)`); err == nil || !strings.Contains(err.Error(), "TypeError") {
		t.Fatalf("got %v want TypeError", err)
	}
}
