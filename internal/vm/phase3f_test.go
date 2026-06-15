package vm_test

import "testing"

func TestCaseEquality(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"class_true", `p(Integer === 5)`, "true\n"},
		{"class_false", `p(String === 5)`, "false\n"},
		{"range_in", `p((1..5) === 3)`, "true\n"},
		{"range_out", `p((1..5) === 9)`, "false\n"},
		{"value", `p(5 === 5)`, "true\n"},
		{"value_false", `p(5 === 6)`, "false\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestCaseWhen(t *testing.T) {
	const kindFn = "def kind(x)\n  case x\n  when Integer then \"int\"\n  when String then \"str\"\n  when Array then \"arr\"\n  else \"other\"\n  end\nend\n"
	tests := []struct{ name, src, want string }{
		{"value_match", "r = case 2\nwhen 1 then \"one\"\nwhen 2 then \"two\"\nelse \"other\"\nend\np r", "\"two\"\n"},
		{"class_int", kindFn + `p kind(5)`, "\"int\"\n"},
		{"class_str", kindFn + `p kind("hi")`, "\"str\"\n"},
		{"class_arr", kindFn + `p kind([1])`, "\"arr\"\n"},
		{"class_else", kindFn + `p kind(3.5)`, "\"other\"\n"},
		{"range", "g = case 85\nwhen 90..100 then \"A\"\nwhen 80..89 then \"B\"\nelse \"F\"\nend\np g", "\"B\"\n"},
		{"no_subject", "n = 7\nr = case\nwhen n < 5 then \"low\"\nwhen n < 10 then \"mid\"\nelse \"high\"\nend\np r", "\"mid\"\n"},
		{"multi_value", `p(case 3 when 1, 2, 3 then "small" else "big" end)`, "\"small\"\n"},
		{"no_match_nil", `p(case 99 when 1 then "a" end)`, "nil\n"},
		{"multiline_body", "r = case 1\nwhen 1\n  x = 10\n  x * 2\nelse\n  0\nend\np r", "20\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
