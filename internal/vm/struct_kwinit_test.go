package vm_test

import (
	"strings"
	"testing"
)

// TestStructKeywordInit covers Struct.new(..., keyword_init: true), the positional
// default, and the argument errors. Asserted against MRI Ruby 4.0.5.
func TestStructKeywordInit(t *testing.T) {
	cases := []struct{ src, want string }{
		// keyword_init: members come from keyword arguments; absent members are nil.
		{`S = Struct.new(:a, :b, keyword_init: true); s = S.new(a: 1, b: 2); p [s.a, s.b]`, "[1, 2]\n"},
		{`S = Struct.new(:a, :b, keyword_init: true); p [S.new(a: 1).b, S.new.to_a, S.new(a: 1, b: 2).to_h]`, "[nil, [nil, nil], {a: 1, b: 2}]\n"},
		// keyword_init: false (and absent) keep the positional constructor.
		{`S = Struct.new(:a, :b, keyword_init: false); s = S.new(1, 2); p [s.a, s.b]`, "[1, 2]\n"},
		{`S = Struct.new(:a, :b); p [S.new(1).b, S.new(1, 2).a]`, "[nil, 1]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		{`S = Struct.new(:a, keyword_init: true); S.new(a: 1, c: 3)`, "unknown keyword"},  // unknown member key
		{`S = Struct.new(:a, keyword_init: true); S.new(5)`, "wrong number of arguments"}, // positional given to a kw struct
		{`S = Struct.new(:a, :b); S.new(1, 2, 3)`, "struct size differs"},                 // too many positionals
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q err=%v, want substring %q", c.src, err, c.want)
		}
	}
}
