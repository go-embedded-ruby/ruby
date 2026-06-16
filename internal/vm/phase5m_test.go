package vm_test

import "testing"

func TestSafeNavigation(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"nil_recv", "a = nil\np a&.upcase", "nil\n"},
		{"non_nil_recv", "b = \"hi\"\np b&.upcase", "\"HI\"\n"},
		{"chain_nil", "a = nil\np a&.foo&.bar", "nil\n"},
		{"chain_through", "p \"abc\"&.upcase&.reverse", "\"CBA\"\n"},
		{"with_args", "arr = [1, 2, 3]\np arr&.join(\"-\")", "\"1-2-3\"\n"},
		{"array_method", "arr = [1, 2, 3]\np arr&.length", "3\n"},
		{"nil_with_args", "x = nil\np x&.foo(1, 2)", "nil\n"},
		{"short_circuit_args", "def side\n@hit = true\n1\nend\n@hit = false\nx = nil\nx&.foo(side)\np @hit", "false\n"},
		{"with_block", "p [1, 2, 3]&.map { |n| n * 2 }", "[2, 4, 6]\n"},
		{"nil_with_block", "x = nil\np x&.each { |n| n }", "nil\n"},
		{"false_is_not_nil", "p false&.to_s", "\"false\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
