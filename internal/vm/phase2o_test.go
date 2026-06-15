package vm_test

import "testing"

func TestTopLevelIvars(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"set_get", "@x = 5\np @x", "5\n"},
		{"undefined_nil", `p @nope`, "nil\n"},
		{"op_assign", "@x = 5\n@x += 3\np @x", "8\n"},
		{"or_assign_fresh", "@y ||= 10\np @y", "10\n"},
		{"or_assign_keeps", "@y = 1\n@y ||= 99\np @y", "1\n"},
		{"self_is_main", `puts self`, "main\n"},
		// top-level methods run with self = main, so they share its ivars
		{
			"shared_with_method",
			"@n = 0\ndef inc\n  @n += 1\nend\ninc\ninc\np @n",
			"2\n",
		},
		{
			"shared_array",
			"@items = []\ndef add(x)\n  @items << x\nend\nadd(1)\nadd(2)\np @items",
			"[1, 2]\n",
		},
		// value types still cannot hold ivars (no-op set, nil get)
		{
			"value_type_no_ivar",
			"class Integer\n  def stash\n    @v = 1\n    @v\n  end\nend\np 5.stash",
			"nil\n",
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
