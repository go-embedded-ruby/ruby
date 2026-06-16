package vm_test

import (
	"strings"
	"testing"
)

func TestStructEnumerable(t *testing.T) {
	pre := "Point = Struct.new(:x, :y)\npt = Point.new(3, 7)\n"
	tests := []struct{ name, src, want string }{
		{"each", pre + "r = []\npt.each { |v| r << v }\np r", "[3, 7]\n"},
		{"map", pre + "p pt.map { |v| v * 2 }", "[6, 14]\n"},
		{"select", pre + "p pt.select { |v| v > 5 }", "[7]\n"},
		{"min", pre + "p pt.min", "3\n"},
		{"max", pre + "p pt.max", "7\n"},
		{"sum", pre + "p pt.sum", "10\n"},
		{"include", pre + "p pt.include?(3)", "true\n"},
		{"find", pre + "p pt.find { |v| v > 5 }", "7\n"},
		{"each_returns_self", pre + "p pt.each { |v| v }.to_a", "[3, 7]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestStructEachNoBlock(t *testing.T) {
	src := "Point = Struct.new(:x)\nPoint.new(1).each"
	if err := runErr(t, src); err == nil || !strings.Contains(err.Error(), "LocalJumpError") {
		t.Fatalf("got %v want LocalJumpError", err)
	}
}
