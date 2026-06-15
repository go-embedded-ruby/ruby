package vm_test

import "testing"

func TestAttrAccessors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"accessor", "class C\nattr_accessor :x\nend\nc = C.new\nc.x = 5\np c.x", "5\n"},
		{"accessor_multi", "class C\nattr_accessor :a, :b\nend\nc = C.new\nc.a = 1\nc.b = 2\np c.a + c.b", "3\n"},
		{"reader_unset", "class C\nattr_accessor :x\nend\np C.new.x", "nil\n"},
		{"reader", "class C\nattr_reader :r\ndef initialize\n@r = 99\nend\nend\np C.new.r", "99\n"},
		{"writer", "class C\nattr_writer :w\ndef val\n@w\nend\nend\nc = C.new\nc.w = 42\np c.val", "42\n"},
		{"compound_attr", "class C\nattr_accessor :n\ndef initialize\n@n = 0\nend\nend\nc = C.new\nc.n += 5\nc.n *= 2\np c.n", "10\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
