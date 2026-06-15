package vm_test

import "testing"

func TestCompoundAssignment(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// arithmetic op-assign on a local
		{"add", "x = 5\nx += 3\np x", "8\n"},
		{"sub", "x = 5\nx -= 2\np x", "3\n"},
		{"mul", "x = 5\nx *= 4\np x", "20\n"},
		{"div", "x = 20\nx /= 3\np x", "6\n"},
		{"mod", "x = 17\nx %= 5\np x", "2\n"},
		// logical op-assign
		{"or_nil", "y = nil\ny ||= 10\np y", "10\n"},
		{"or_keeps", "y = 5\ny ||= 99\np y", "5\n"},
		{"and_truthy", "z = 1\nz &&= 5\np z", "5\n"},
		{"and_nil", "z = nil\nz &&= 5\np z", "nil\n"},
		// fresh local ||= must define the variable (reads nil first)
		{"fresh_or", "w ||= 42\np w", "42\n"},
		// << on a local array
		{"shovel", "list = [1]\nlist <<= 2\np list", "[1, 2]\n"},
		// index op-assign
		{"index_add", "h = {a: 1}\nh[:a] += 10\np h", "{a: 11}\n"},
		{"array_index_add", "arr = [1, 2, 3]\narr[0] += 100\np arr", "[101, 2, 3]\n"},
		{"index_or_chain", "c = {}\nc[:x] ||= 0\nc[:x] += 1\np c", "{x: 1}\n"},
		// ivar op-assign inside an object
		{
			"ivar",
			"class C\n  def initialize\n    @n = 0\n  end\n  def bump\n    @n += 2\n  end\n  def n\n    @n\n  end\nend\nc = C.new\nc.bump\nc.bump\np c.n",
			"4\n",
		},
		{
			"ivar_memo",
			"class C\n  def memo\n    @c ||= 7\n  end\nend\no = C.new\np o.memo\np o.memo",
			"7\n7\n",
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
