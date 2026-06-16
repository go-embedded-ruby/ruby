package vm_test

import (
	"strings"
	"testing"
)

func TestInstanceReflection(t *testing.T) {
	pre := "class C\ndef initialize\n@x = 1\n@y = 2\nend\nend\nc = C.new\n"
	tests := []struct{ name, src, want string }{
		{"ivar_get_sym", pre + "p c.instance_variable_get(:@x)", "1\n"},
		{"ivar_get_str", pre + "p c.instance_variable_get(\"@y\")", "2\n"},
		{"ivar_get_missing", pre + "p c.instance_variable_get(:@nope)", "nil\n"},
		{"ivar_set", pre + "c.instance_variable_set(:@z, 99)\np c.instance_variable_get(:@z)", "99\n"},
		{"ivar_defined_true", pre + "p c.instance_variable_defined?(:@x)", "true\n"},
		{"ivar_defined_false", pre + "p c.instance_variable_defined?(:@nope)", "false\n"},
		{"ivar_defined_no_table", "p 5.instance_variable_defined?(:@x)", "false\n"},
		{"ivar_get_no_table", "p 5.instance_variable_get(:@x)", "nil\n"},
		{"instance_eval", pre + "p c.instance_eval { @x + @y }", "3\n"},
		{"instance_exec", pre + "p c.instance_exec(10) { |n| @x + n }", "11\n"},
		{"sym_ivar", `p :@x`, ":@x\n"},
		{"sym_cvar", `p :@@cv`, ":@@cv\n"},
		{"sym_gvar", `p :$g`, ":$g\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestInstanceEvalNoBlock(t *testing.T) {
	for _, src := range []string{`5.instance_eval`, `5.instance_exec`} {
		if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "LocalJumpError") {
			t.Fatalf("src=%q got %v want LocalJumpError", src, err)
		}
	}
}
