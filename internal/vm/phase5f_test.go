package vm_test

import (
	"strings"
	"testing"
)

func TestConstantAssignment(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"int", "X = 5\np X", "5\n"},
		{"float", "PI = 3.14\np PI", "3.14\n"},
		{"expr", "X = 5\nY = X + 1\np Y", "6\n"},
		{"reassign", "X = 1\nX = 2\np X", "2\n"},
		{"string", "NAME = \"ruby\"\np NAME", "\"ruby\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestAttributeAssignment(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"setter", "class Box\ndef v; @v; end\ndef v=(x); @v = x * 2; end\nend\nb = Box.new\nb.v = 5\np b.v", "10\n"},
		{"reader_writer", "class P\ndef initialize(x); @x = x; end\ndef x; @x; end\ndef x=(v); @x = v; end\nend\no = P.new(1)\no.x = 7\np o.x", "7\n"},
		{"const_holds_obj", "class P\ndef x; @x; end\ndef x=(v); @x = v; end\nend\nO = P.new\nO.x = 9\np O.x", "9\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestUninitializedConstant(t *testing.T) {
	if err := runErr(t, `p NOPE`); err == nil || !strings.Contains(err.Error(), "uninitialized constant") {
		t.Fatalf("got %v want uninitialized constant", err)
	}
}
