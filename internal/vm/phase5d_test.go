package vm_test

import "testing"

func TestStringIterationBatch(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"lines_empty", `p "".lines`, "[]\n"},
		{"lines_blank_line", `p "a\n\nb".lines`, "[\"a\\n\", \"\\n\", \"b\"]\n"},
		{"lines_trailing_nl", `p "abc\n".lines`, "[\"abc\\n\"]\n"},
		{"lines_no_nl", `p "x".lines`, "[\"x\"]\n"},
		{"each_char_returns_self", `p "hello".each_char { |c| }`, "\"hello\"\n"},
		{"each_char_collect", "r = []\n\"abc\".each_char { |c| r << c }\np r", "[\"a\", \"b\", \"c\"]\n"},
		{"each_line_collect", "r = []\n\"ab\\ncd\".each_line { |l| r << l }\np r", "[\"ab\\n\", \"cd\"]\n"},
		{"each_byte_collect", "r = []\n\"AB\".each_byte { |b| r << b }\np r", "[65, 66]\n"},
		{"each_byte_returns_self", `p "hi".each_byte { |b| }`, "\"hi\"\n"},
		{"each_line_returns_self", `p "ab\ncd".each_line { |l| }`, "\"ab\\ncd\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestStringIterationNoBlock(t *testing.T) {
	// String iterators with no block return an Enumerator (MRI semantics).
	cases := map[string]string{
		`p "xy".each_char.to_a`:   "[\"x\", \"y\"]\n",
		`p "xy".each_byte.to_a`:   "[120, 121]\n",
		`p "x\ny".each_line.to_a`: "[\"x\\n\", \"y\"]\n",
	}
	for src, want := range cases {
		if got := eval(t, src); got != want {
			t.Errorf("src=%q got %q want %q", src, got, want)
		}
	}
}
