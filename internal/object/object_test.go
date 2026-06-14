package object

import (
	"math"
	"testing"
)

func TestToSAndInspect(t *testing.T) {
	tests := []struct {
		v            Value
		toS, inspect string
		truthy       bool
	}{
		{Integer(42), "42", "42", true},
		{Integer(-7), "-7", "-7", true},
		{Float(1.0), "1.0", "1.0", true},
		{Float(3.5), "3.5", "3.5", true},
		{Float(math.Inf(1)), "Infinity", "Infinity", true},
		{Float(math.Inf(-1)), "-Infinity", "-Infinity", true},
		{Float(math.NaN()), "NaN", "NaN", true},
		{String("hi"), "hi", `"hi"`, true},
		{String("a\"b\\c\nd\te"), "a\"b\\c\nd\te", `"a\"b\\c\nd\te"`, true},
		{Bool(true), "true", "true", true},
		{Bool(false), "false", "false", false},
		{Nil{}, "", "nil", false},
		{Main{}, "main", "main", true},
	}
	for _, tc := range tests {
		if got := tc.v.ToS(); got != tc.toS {
			t.Errorf("%#v ToS = %q, want %q", tc.v, got, tc.toS)
		}
		if got := tc.v.Inspect(); got != tc.inspect {
			t.Errorf("%#v Inspect = %q, want %q", tc.v, got, tc.inspect)
		}
		if got := tc.v.Truthy(); got != tc.truthy {
			t.Errorf("%#v Truthy = %v, want %v", tc.v, got, tc.truthy)
		}
	}
}

func TestSingletons(t *testing.T) {
	if !True.Truthy() || False.Truthy() || NilV.Truthy() {
		t.Fatal("singleton truthiness wrong")
	}
}
