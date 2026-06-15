package vm_test

import (
	"strings"
	"testing"
)

func TestDefineMethod(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"with_param", "class C\ndefine_method(:double) { |x| x * 2 }\nend\np C.new.double(21)", "42\n"},
		{"no_param", "class C\ndefine_method(:greet) { \"hi\" }\nend\np C.new.greet", "\"hi\"\n"},
		{"closure_over_loop", "class C\n[:a, :b].each do |n|\ndefine_method(n) { n.to_s }\nend\nend\nc = C.new\np c.a\np c.b", "\"a\"\n\"b\"\n"},
		{"rebinds_self_ivar", "class D\ndef initialize\n@v = 10\nend\ndefine_method(:get_v) { @v }\ndefine_method(:add) { |n| @v + n }\nend\nd = D.new\np d.get_v\np d.add(5)", "10\n15\n"},
		{"returns_symbol", "class E\nm = define_method(:foo) { 1 }\np m\nend", ":foo\n"},
		{"proc_arg", "PR = proc { |x| x + 100 }\nclass F\ndefine_method(:viaproc, PR)\nend\np F.new.viaproc(1)", "101\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestDefineMethodErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"not_proc", "class G\ndefine_method(:x, 5)\nend", "expected Proc"},
		{"no_block", "class G\ndefine_method(:x)\nend", "without a block"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runErr(t, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("src=%q got %v want %q", tc.src, err, tc.want)
			}
		})
	}
}
