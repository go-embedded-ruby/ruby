package vm_test

import (
	"strings"
	"testing"
)

// TestCaseInHash covers hash patterns: the deconstruct_keys protocol, shorthand
// `{name:}` binding, value sub-patterns, key renaming, `**rest`, `**nil`, the
// empty `{}` pattern, nesting, and the brace-less top-level form.
func TestCaseInHash(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"shorthand", "case {name: \"A\", age: 1}\nin {name:, age:}\n puts \"#{name} #{age}\"\nend", "A 1\n"},
		{"value", "case {status: \"active\"}\nin {status: \"active\"}\n puts \"yes\"\nend", "yes\n"},
		{"value_no", "case {status: \"x\"}\nin {status: \"active\"}\n puts \"y\"\nelse\n puts \"no\"\nend", "no\n"},
		{"rename", "case {a: 1}\nin {a: x}\n puts x\nend", "1\n"},
		{"rest", "case {a: 1, b: 2, c: 3}\nin {a:, **rest}\n p [a, rest]\nend", "[1, {b: 2, c: 3}]\n"},
		{"rest_empty", "case {a: 1}\nin {a:, **rest}\n p rest\nend", "{}\n"},
		{"rest_nil", "case {a: 1}\nin {a:, **nil}\n puts \"exact\"\nend", "exact\n"},
		{"rest_nil_extra", "case {a: 1, b: 2}\nin {a:, **nil}\n puts \"y\"\nelse\n puts \"extra\"\nend", "extra\n"},
		{"empty", "case {}\nin {}\n puts \"empty\"\nend", "empty\n"},
		{"empty_nonempty", "case {x: 1}\nin {}\n puts \"y\"\nelse\n puts \"not empty\"\nend", "not empty\n"},
		{"missing_key", "case {a: 1}\nin {b:}\n puts \"y\"\nelse\n puts \"no b\"\nend", "no b\n"},
		{"non_hash", "case 5\nin {a:}\n puts \"y\"\nelse\n puts \"not hash\"\nend", "not hash\n"},
		{"mixed_value_bind", "case {name: \"Bob\", role: :admin}\nin {name: String => n, role: :admin}\n puts \"admin #{n}\"\nend", "admin Bob\n"},
		{"nested", "case {user: {name: \"X\", tags: [1, 2]}}\nin {user: {name:, tags: [f, *]}}\n puts \"#{name} #{f}\"\nend", "X 1\n"},
		{"braceless", "case {a: 1, b: 2}\nin a:, b:\n puts a + b\nend", "3\n"},
		{"braceless_rest", "case {a: 1, b: 2}\nin a:, **rest\n p [a, rest]\nend", "[1, {b: 2}]\n"},
		{"struct_keys", "S = Struct.new(:x, :y)\ncase S.new(1, 2)\nin {x:, y:}\n puts x + y\nend", "3\n"},
		{"custom_deconstruct", "class C\n def deconstruct_keys(k)\n  {host: \"h\", port: 80}\n end\nend\ncase C.new\nin {host:, port:}\n puts \"#{host}:#{port}\"\nend", "h:80\n"},
		{"guard", "case {age: 20}\nin {age:} if age >= 18\n puts \"adult\"\nelse\n puts \"minor\"\nend", "adult\n"},
		{"result", "x = case {a: 1}\nin {a:}\n  a * 10\nend\nputs x", "10\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestCaseInHashNoMatch covers NoMatchingPatternError from a failed hash match.
func TestCaseInHashNoMatch(t *testing.T) {
	if err := runErr(t, "case {a: 1}\nin {b:}\n puts \"x\"\nend"); err == nil || !strings.Contains(err.Error(), "NoMatchingPatternError") {
		t.Errorf("got %v", err)
	}
	if err := runErr(t, "case {a: 1}\nin {a:, **nil}\nend\ncase {a: 1, b: 2}\nin {a:, **nil}\nend"); err == nil || !strings.Contains(err.Error(), "NoMatchingPatternError") {
		t.Errorf("rest_nil extras: got %v", err)
	}
}

// TestCaseInHashParseErrors covers a doubled double-splat.
func TestCaseInHashParseErrors(t *testing.T) {
	if err := runErr(t, "case {a: 1}\nin {**rest, **nil}\nend"); err == nil {
		t.Error("expected error for two ** in hash pattern")
	}
}
