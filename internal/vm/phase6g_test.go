package vm_test

import "testing"

func TestConstHashPattern(t *testing.T) {
	pre := "Point = Struct.new(:x, :y)\n"
	tests := []struct{ name, src, want string }{
		{"binds", pre + "case Point.new(1, 2)\nin Point(x:, y:)\np [x, y]\nend", "[1, 2]\n"},
		{"value_subpattern", pre + "case Point.new(5, 9)\nin Point(x: 5, y:)\np y\nend", "9\n"},
		{"class_mismatch", pre + "case {a: 1}\nin Point(x:)\np \"no\"\nin {a:}\np a\nend", "1\n"},
		{"value_mismatch", pre + "case Point.new(1, 2)\nin Point(x: 9)\np \"no\"\nelse\np \"else\"\nend", "\"else\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
