package vm_test

import "testing"

func TestDoEndBlocks(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{
			"each_with_params",
			"r = []\n[1, 2, 3].each do |x|\n  r << x * 10\nend\np r",
			"[10, 20, 30]\n",
		},
		{
			"map",
			"p([1, 2, 3].map do |x|\n  x + 1\nend)",
			"[2, 3, 4]\n",
		},
		{
			"no_params",
			"n = 0\n3.times do\n  n = n + 1\nend\np n",
			"3\n",
		},
		{
			"nested",
			"t = 0\n[1, 2].each do |a|\n  [10, 20].each do |b|\n    t = t + a * b\n  end\nend\np t",
			"90\n",
		},
		{
			"yield_through_do",
			"class C\n  def go\n    [1, 2].each do |x|\n      yield x\n    end\n  end\nend\nr = []\nC.new.go { |v| r << v }\np r",
			"[1, 2]\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// The `do` after a while/until condition is the loop's, not a block on a call
// inside the condition.
func TestWhileDoDisambiguation(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"while_do", "i = 0\nwhile i < 3 do\n  i = i + 1\nend\np i", "3\n"},
		{"while_no_do", "i = 0\nwhile i < 2\n  i = i + 1\nend\np i", "2\n"},
		{"until_do", "i = 0\nuntil i >= 2 do\n  i = i + 1\nend\np i", "2\n"},
		{"while_method_cond_do", "a = [1, 2, 3]\nn = 0\nwhile a.include?(n + 1) do\n  n = n + 1\nend\np n", "3\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
