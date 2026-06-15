package vm_test

import "testing"

func TestEndlessMethods(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"with_param", "def square(x) = x * x\np square(5)", "25\n"},
		{"no_params", "def greeting = \"hi\"\np greeting", "\"hi\"\n"},
		{"in_class", "class C\ndef double(n) = n * 2\nend\np C.new.double(21)", "42\n"},
		{"no_param_method", "class C\ndef name = \"C\"\nend\np C.new.name", "\"C\"\n"},
		{"singleton_endless", "class C\ndef self.make = 99\nend\np C.make", "99\n"},
		{"with_default", "def f(x, y = 10) = x + y\np f(5)\np f(5, 20)", "15\n25\n"},
		{"normal_def_still_works", "def f\n1 + 2\nend\np f", "3\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
