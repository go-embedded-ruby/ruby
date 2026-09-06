package vm_test

import (
	"strings"
	"testing"
)

func TestClassEval(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"def_method", "class C\nend\nC.class_eval do\ndef hi\n\"hello\"\nend\nend\np C.new.hi", "\"hello\"\n"},
		{"def_with_args", "class C\nend\nC.class_eval { def add(a, b)\na + b\nend }\np C.new.add(2, 3)", "5\n"},
		{"module_eval", "module M\nend\nM.module_eval { def mt\n\"m\"\nend }\nclass D\ninclude M\nend\np D.new.mt", "\"m\"\n"},
		{"define_method_closure", "class C\nend\nn = 5\nC.class_eval { define_method(:n) { n } }\np C.new.n", "5\n"},
		{"class_exec_args", "class C\nend\nC.class_exec(7) { |v| define_method(:seven) { v } }\np C.new.seven", "7\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestClassEvalNoBlock(t *testing.T) {
	// class_eval accepts a string form, so with neither a block nor a string it
	// raises ArgumentError (MRI: "wrong number of arguments (given 0, expected
	// 1..3)"). class_exec has no string form, so it raises LocalJumpError. Verified
	// against ruby 4.0.5.
	cases := []struct{ src, want string }{
		{`class C\nend\nC.class_eval`, "ArgumentError"},
		{`class C\nend\nC.class_exec`, "LocalJumpError"},
	}
	for _, c := range cases {
		s := strings.ReplaceAll(c.src, `\n`, "\n")
		if err := runErr(t, s); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("src=%q got %v want %s", s, err, c.want)
		}
	}
}
